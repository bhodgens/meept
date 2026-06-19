# Vision Model Implementation Review

**Date:** 2026-06-19
**Reviewer:** subagent (general-purpose)
**Commit reviewed:** d219cba6 + post-commit gap fixes (Gap A: chat_service.go Parts forwarding; Gap B: dispatcher Parts threading)
**Plan:** `docs/superpowers/plans/2026-06-18-vision-model.md`

## Plan Task Verification

| Task | Status | Evidence |
|------|--------|----------|
| 1. ContentPart/ImageRef types | IMPLEMENTED | `internal/llm/content_parts.go:14-29` (struct defs), `:36-80` (helpers); tests in `internal/llm/multimodal_test.go:10-90` |
| 2. ChatMessage.Parts + OpenAI serialization | IMPLEMENTED | `internal/llm/models.go:25` (Parts field), `:48-76` (ToOpenAIDict multimodal branch); tests `multimodal_test.go:93-159` |
| 3. UploadStore interface + Client/Anthropic integration | IMPLEMENTED | `internal/llm/interface.go:79-81`, `internal/llm/client.go:104,215-222` (WithUploadStore), `internal/llm/content_parts.go:85-105` (resolveImageURL) |
| 4. Anthropic multimodal serialization | IMPLEMENTED | `internal/llm/anthropic.go:459-467` (anthropicImageSource), `:686-727` (partsToAnthropicContent), `:590-603` (buildRequest Parts branch) |
| 5. Session Message Parts + SQLite migration | IMPLEMENTED | `internal/session/store.go:18` (Parts field), `internal/session/store_sqlite.go:126` (migration), `:720-754` (SaveMessages serializes parts), `:760-818` (GetMessages deserializes) |
| 6. UploadService | IMPLEMENTED | `internal/services/upload_service.go` (full file, 312 lines), tests in `internal/services/upload_service_test.go` |
| 7. HTTP upload endpoints | IMPLEMENTED | `internal/comm/http/upload_handlers.go` (166 lines), routes at `internal/comm/http/server.go:941-945` |
| 8. ChatRequest Parts | IMPLEMENTED | `internal/services/chat_service.go:31` (Parts field), `:119-121` (bus payload forwarding) |
| 9. Vision pre-flight | IMPLEMENTED | `internal/agent/vision_preflight.go` (117 lines), tests in `internal/agent/vision_preflight_test.go` |
| 10. Vision pre-flight integration into AgentLoop | IMPLEMENTED | `internal/agent/loop.go:462` (uploadStore field), `:1800-1814` (call site), `:3658-3667` (SetUploadStore) |
| 11. Flutter API models | IMPLEMENTED | `ui/flutter_ui/lib/models/api_models.dart:180-218` (ChatMessagePart), `:221-256` (ImageRefData), `:282` (ChatMessage.parts), `:257-269` (Attachment) |
| 12. Flutter SDK client | IMPLEMENTED | `ui/flutter_ui/lib/services/sdk_client.dart:317-330` (sendChatMessageWithParts), `:340-374` (uploadFile) |
| 13. Flutter chat input | IMPLEMENTED | `ui/flutter_ui/lib/features/chat/chat_input.dart:144` (_attachments), `:283-316` (_uploadDetectedImage), `:406-426` (_buildParts), `:492-506` (send via parts), `:708-721` (chips UI) |
| 14. Flutter message bubble | IMPLEMENTED | `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart:37-40` (imageParts snapshot), `:149-160` (chip rendering), `:239-252` (label helper) |
| 15. TUI multimodal | IMPLEMENTED | `internal/tui/models/chat.go:267-289` (attachmentEntry + isImageFile), `:1614-1693` (doSendMessage with parts), `:2798-2822` (image upload in detectAndAttachFile); `internal/tui/rpc.go:306-339` (ChatWithParts), `:341-380` (UploadFile) |
| 16. Config schema for uploads | IMPLEMENTED | `internal/config/schema.go:265` (Uploads field), `:268-275` (UploadsConfig struct), `:1169-1175` (default config) |
| 17. Upload RPC handler | IMPLEMENTED | `internal/daemon/upload_rpc.go` (97 lines), wired in `internal/daemon/daemon.go:579-584` |

**Task 18 (regression test run)** was a plan-only checklist item (run `go test ./...`); not separately tracked here.

## Findings

### Critical

#### C1: OpenAI-compatible providers receive unresolved `file://` image URLs
**File:** `internal/llm/models.go:67-72` (ToOpenAIDict), `internal/llm/client.go:275-278` (call site)

The OpenAI `Client.Chat` path calls `msg.ToOpenAIDict()` per message and ships the dict to the provider without resolving `file://<sha>` references into `data:` URLs. The result: any OpenAI-compatible provider (OpenAI, Gemini-via-OpenAI, local Llama.cpp, etc.) receives `"url": "file://abc123..."` which it cannot fetch. The image is silently dropped by the provider (or rejected as malformed).

The Anthropic path handles this correctly via `partsToAnthropicContent` which calls `resolveImageURL(p.ImageURL.URL, store)` at `internal/llm/anthropic.go:705`. The OpenAI path has no equivalent.

Note: `Client.uploadStore` is declared at `client.go:104` and populated by `WithUploadStore` at `:215-222`, but is never read anywhere in `client.go`. The field is dead code on the OpenAI Client (it is only useful on the Anthropic client). The `visionClient := llm.NewClient(visionModels[0], llm.WithUploadStore(l.uploadStore))` call at `loop.go:1807` therefore passes the store into a void for OpenAI providers — but vision pre-flight itself only mutates `ref.Description`, which then goes through ToOpenAIDict's described-image branch (text substitution at `models.go:61-65`), so the pre-flight output survives. The bug only affects the main agent turn when the main model is OpenAI-compatible AND the image description is still empty (e.g., no vision-capable model is registered, or the pre-flight errored).

This is "Critical" rather than "High" because it breaks the headline feature (multimodal image input to OpenAI providers) whenever vision pre-flight cannot run. The vision pre-flight fallback assigns `ref.Description = "[image analysis failed: ...]"` (vision_preflight.go:99), which triggers the text-substitution branch and hides the bug in the common case — but a user with no configured vision model will still produce broken requests if they somehow bypass the warning path.

### High

#### H1: `UploadService.Upload` race window allows lost dimensions record
**File:** `internal/services/upload_service.go:114-158`

The two-phase upload pattern (reserve slot under lock at :123, write to disk without lock at :132, then re-acquire lock at :140 to record dimensions) has a correctness gap: if a concurrent dedup-style `Upload` call for identical content arrives between the reservation at :123 and the final write at :140-149, the second caller will hit the dedup branch at :102-111 and return `existing` (the reserved record with `Width=0, Height=0`). The first caller's later update at :140-149 will then overwrite the record with dimensions — but the second caller's returned `*Upload` already has zero dimensions.

This is not a data-corruption bug (the on-disk JSON ends up correct after the first caller finishes), but it means clients that race on identical content can observe a `Width/Height=0` upload descriptor that the actual record does not have. Given that the second caller also bumps RefCount at :104 before the first caller's final save, there is also a small window where the refcount can be lost if the first caller's `saveRecords` at :147 overwrites with a snapshot taken before the increment.

Practical impact: low (requires identical-content concurrent uploads, which the SHA dedup is specifically trying to collapse), but the pattern is non-trivially wrong. A cleaner fix would hold the lock across disk write OR write the file before reserving the record.

#### H2: `runVisionPreflight` shares one 30-second timeout across all images
**File:** `internal/agent/vision_preflight.go:77-78`

```go
preflightCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
defer cancel()
```

is created once outside the per-image loop at :80. With N undescribed images and two attempts per image (initial + retry at :97), the total budget is 30s regardless of N. A message with 4 images where the first image's vision model takes 25s leaves only 5s for the remaining 3. Worse, the retry at :97 reuses the same context — if the first attempt timed out, the retry starts with a near-deadline context and will also time out immediately.

This is "High" rather than "Medium" because it silently degrades the cache-hit rate for multi-image messages and the retry path is effectively dead code once any single image burns through the budget.

#### H3: Image content is not indexed in FTS5 search
**File:** `internal/session/store_sqlite.go:166-177` (FTS triggers), `internal/session/store_sqlite.go:749` (SaveMessages INSERT)

The FTS5 mirror table only indexes `content`, not `parts`. When a user sends an image with no text body (just an image_url ContentPart), the `content` column is the empty string (`Content: ""` at `loop.go:1242` flows from `AddUserMessageWithParts` at `conversation.go:415-421` which stores `content` as the empty `userMessage`). The FTS insert trigger writes an empty row. The image description (populated post-preflight) is stored on `ImageRef.Description` inside the `parts` JSON column and is never searchable.

The result: multimodal messages cannot be found via `SearchMessages`. A user who says "find the message with the red square image" gets nothing even after pre-flight has cached "A red square" as the description.

The plan does call out that `ContentFromParts` should be used by "FTS5 search, summarization, context compaction, memory injection" (`content_parts.go:34-35` comment), but no call site in `internal/session/` or `internal/services/search_service.go` references `ContentFromParts` or `parts` at all (verified with grep). The helper exists but is unwired for FTS.

### Medium

#### M1: `description` collision with cached failure markers breaks pre-flight re-runs
**File:** `internal/agent/vision_preflight.go:99,111`

When pre-flight fails twice, the code writes `ref.Description = "[image analysis failed: ...]"` or `"[image analysis returned empty]"`. These are sentinel strings that populate `Description`, which `HasUndescribedImages` treats as "already described" (`content_parts.go:75` — the check is `Description == ""`). Subsequent turns therefore skip the image entirely — the user can never trigger a re-analysis even if the vision model comes back online.

Fix would use a separate flag (or check for the `[image analysis` prefix), but at minimum the sentinels should be documented as terminal so reviewers know.

#### M2: No test covers the vision pre-flight cache-write path
**File:** `internal/agent/vision_preflight_test.go`

The existing tests only cover `needsVisionPreflight` and `collectUndescribedImageRefs`. There is no test for `runVisionPreflight` itself — specifically, no test verifies that after a successful call, `ref.Description` is populated with the vision model's response, or that the in-place mutation propagates to the messages slice used by the main LLM turn. Given that this is the correctness lynchpin of the entire vision cache design (and is easy to break by changing `descMsg` to take a value instead of a pointer), the lack of coverage is a real gap.

#### M3: Plan-vs-implementation divergence on "runVisionPreflight collects unique by URL"
**File:** `internal/agent/vision_preflight.go:57-71`

The plan's corrected code at line ~1959 (plan doc) collects unique-by-URL refs and only describes each URL once. The implementation matches this. However, if two distinct ContentParts in the same message reference the same URL (e.g., the user pastes the same image twice), only the first `ImageRef` pointer is added to `toDescribe` — the second ContentPart's ImageRef never has its Description set, because the `seen` map skips it. Then when the main LLM turn runs, the second part still has `Description=""` and hits the unresolved-image path in the provider serializer.

Practical impact: low (users rarely paste the same image twice in one message), but the asymmetry is a real bug.

#### M4: TUI attachment upload blocks the TUI Update goroutine
**File:** `internal/tui/models/chat.go:2804-2819`

`detectAndAttachFile` runs synchronously inside the Bubble Tea `Update` goroutine and calls `m.rpc.UploadFile(context.Background(), candidate)` at :2809. The comment at :2806 acknowledges this and claims "Upload latency for typical image sizes (<5MB) is well under the 120s RPC timeout". For a 5MB image over a slow link (or local LLM with a saturated queue), this can freeze the TUI for tens of seconds. The TUI's own render loop is blocked. This is UX-jarring and violates the implicit "Update returns quickly" contract of Bubble Tea programs.

#### M5: Missing nil guard on `result.Intent` in recursive RouteToAgent handoff
**File:** `internal/agent/dispatcher.go:1293-1300`

When building `nextResult` for the report-router handoff, `Intent: result.Intent` is copied. If the dispatcher's classification produced a `nil` Intent (which can happen on certain fallback paths), the next hop's `DispatchResult.Intent` is nil. Downstream code that dereferences `result.Intent.Type` without a nil check will panic. The Parts propagation itself is fine here, but the new `Parts` field makes this path more reachable (every multimodal request that routes through a pair/collaboration handoff exercises it).

### Low

#### L1: `var _ = io.EOF` / `var _ = json.Marshal` / `var _ = slog.Default` cleanup leftover
**File:** Not present in final implementation — the plan at line ~1640 specified these dead imports, but the implemented `internal/comm/http/upload_handlers.go` imports only `encoding/base64`, `net/http`, `strings`. No issue; flagging that the plan text was not followed verbatim, which is correct.

#### L2: `Logging` quality — pre-flight cache hit has no log entry
**File:** `internal/agent/vision_preflight.go`

The cache-miss path logs at Debug level (:106-109). The cache-hit path (early return from `needsVisionPreflight` at :50) logs nothing. Operators have no visibility into how often the cache is being hit versus missed. A single Debug log on the early-return path would help.

#### L3: `dbPath: filepath.Join(dir, "uploads.json")` race with directory creation
**File:** `internal/services/upload_service.go:58,97`

`NewUploadService` constructs `dbPath = filepath.Join(dir, "uploads.json")` at construction time, but `s.dir` is only created lazily inside `Upload` at :97. The first `loadRecords` call (triggered from any `Upload`/`Load`/`Get`/`Release`/`Acquire`/`GCSweep`) calls `os.ReadFile(s.dbPath)` at :256, which returns an error (handled — returns an empty map). This is correct behavior, but the lazy-directory pattern means that `Load` on an ID before any upload has happened produces an opaque "upload not found" error rather than a clearer "upload directory does not exist yet".

#### L4: `UploadsConfig.Enabled` field is declared but never checked
**File:** `internal/config/schema.go:270`, `internal/services/service.go:184`, `internal/daemon/daemon.go:499-532`

The config has an `Enabled bool` field at schema.go:270 with default `true` (schema.go:1170), but no code reads it. The UploadService is created unconditionally whenever `UploadsDir != ""` (service.go:184). Setting `uploads.enabled = false` in the config has no effect — uploads remain enabled. Either remove the field or wire the check.

#### L5: `GCSweep` uses `time.Now()` cutoff comparing against potentially timezone-mixed timestamps
**File:** `internal/services/upload_service.go:227,247`

`CreatedAt` is stored as `time.Now().UTC()` at :120, but the cutoff at :227 is also UTC, so this is correct. However, if an old records file is imported from a non-UTC source, comparison may misbehave. Low-severity defensive note.

#### L6: `_ = accumulatedContext` dead code in RouteToAgent
**File:** `internal/agent/dispatcher.go:1301`

`accumulatedContext := d.buildAccumulatedContext(...)` is computed but immediately discarded with `_ = accumulatedContext // used for context enrichment in recursive call`. The comment claims future use; in the current code it is wasted work on every multimodal handoff.

## False-positives to ignore

### FP1: "I/O under mutex in `Upload`"
`internal/services/upload_service.go:97` (`os.MkdirAll`) and `:103` (`os.Stat`) and `:106` (`saveRecords` => `os.WriteFile`) inside the locked section look like CLAUDE.md mutex-scope violations. However, the author was clearly aware: disk I/O for the actual upload bytes was moved outside the lock at :132. The remaining in-lock I/O (MkdirAll, Stat, JSON persist) is operating on tiny records-metadata paths where the latency is negligible, and the code is explicitly structured as "reserve under lock, write bytes without lock, re-acquire to finalize". This is an acceptable pragmatic deviation; flagging as a style note rather than a bug. The `//nolint:mutexio` comments in store_sqlite.go follow the same convention.

### FP2: "ChatMessage.Parts is omitempty so it won't roundtrip through JSON"
The `Parts []ContentPart `json:"parts,omitempty"`` tag at `models.go:25` means an empty slice is omitted on marshal. On unmarshal, a missing field yields a nil slice, which `len(m.Parts) > 0` correctly treats as "no parts". Round-trip is correct.

### FP3: "`dbPath` field shadows directory-creation"
See L3 above — the behavior is correct, just slightly opaque.

### FP4: "Anthropic `anthropicContent` struct has both `Content string` and `Source *anthropicImageSource`"
The struct at `anthropic.go:447-460` mixes tool_result fields (`Content`, `ToolUseID`, `IsError`) with image fields (`Source`) on one struct. This is intentional — Anthropic's API uses one content-block shape with discriminated fields per `Type`, and the JSON tags are all `omitempty`. No serialization bug.

### FP5: "Loop variable capture in vision preflight"
`for _, ref := range toDescribe` at `vision_preflight.go:80` — `ref` is `*llm.ImageRef`, so taking its address implicitly is fine. There is no closure capture here (the loop body is synchronous). Not a bug.

## Security Notes

### S1: Path traversal in upload filename (LOW — mitigated by SHA-256 ID)
**File:** `internal/services/upload_service.go:88-93`

`ext := filepath.Ext(filename)` extracts the extension, then `path := filepath.Join(s.dir, id+ext)` builds the storage path. Since `id` is the hex SHA-256 hash (fixed charset), the only attacker-controlled component is `ext`. `filepath.Ext("foo/../../../etc/passwd")` returns `.passwd` (everything after the last dot) or empty depending on placement, and `filepath.Join` would normalize any remaining slashes in the extension into the path. Worst-case the attacker creates a file like `<sha>.evil` in the uploads dir — they cannot escape the directory because `id` dominates the path. The `mimeToExt` fallback at :91 is fully controlled by the server. No path-traversal vulnerability.

### S2: MIME-type confusion
**File:** `internal/services/upload_service.go:71-73`, `internal/comm/http/upload_handlers.go:49-52`

The MIME type is taken from the client's `Content-Type` header (`upload_handlers.go:49`) and validated against `allowedTypes` at `upload_service.go:71`. The allowed list (`image/png`, `image/jpeg`, `image/gif`, `image/webp`) is strict. The on-disk file extension comes from the original filename or the MIME type — no execution path interprets the bytes by extension. The `GET /api/v1/uploads/{id}` handler at `upload_handlers.go:118-121` sends back the stored MIME type with `X-Content-Type-Options: nosniff`, preventing browser MIME sniffing. No issue.

### S3: Upload endpoints inherit server-wide auth
**File:** `internal/comm/http/server.go:1109-1147`

The upload routes at server.go:942-945 are registered on the same mux as all other `/api/v1/*` routes and are wrapped by the server's `middleware` chain at :1109 which applies `authMiddleware` when `RequireAuth && len(APIKeys) > 0` at :1112. The default config has `RequireAuth: true` at :80. Auth coverage is correct.

### S4: `MaxBytesReader` correctly limits multipart upload size
**File:** `internal/comm/http/upload_handlers.go:35`

`http.MaxBytesReader(w, r.Body, s.services.Upload.MaxSizeBytes())` caps the body at the configured limit (default 20MB). The `Upload` method also re-checks via `io.LimitReader(reader, s.maxSizeBytes+1)` at `upload_service.go:76` — defense in depth.

## Conclusion

**Status:** needs work

**Ship-readiness:** The implementation is ~95% feature-complete (all 17 plan tasks IMPLEMENTED) and well-tested for the core types (ContentPart round-trip, Anthropic parts serialization, UploadService dedup/GC). The wiring through dispatcher (Gap B), chat service (Gap A), session SQLite column, HTTP handlers, RPC, TUI, and Flutter is all present and internally consistent.

However, **C1 (OpenAI providers receive unresolved `file://` URLs)** is a real break of the headline feature on the OpenAI path. It is partially masked by vision pre-flight's text-substitution fallback, but any deployment that (a) uses an OpenAI-compatible provider for the main agent AND (b) lacks a vision-capable model for pre-flight will produce broken requests. This should be fixed before shipping vision as a user-facing feature.

**H1** (upload race) and **H2** (shared vision pre-flight timeout) are correct-but-fragile patterns that will cause real-world flakiness under load.

**H3** (FTS does not index image descriptions) is a feature-completeness gap versus the plan's stated design (`content_parts.go:34-35` calls out FTS as a consumer of `ContentFromParts`, but the call site was never wired).

**Recommended pre-ship actions:**
1. Fix C1: thread `c.uploadStore` through `ToOpenAIDict` (or pre-resolve `ImageRef.URL` to a data URL before the Client.Chat call).
2. Fix H2: per-image timeout OR an overall budget that scales with image count.
3. Decide on H3: either wire `ContentFromParts` into the FTS trigger / SaveMessages path, or update the `content_parts.go:34` comment to remove FTS from the list of consumers.
4. Add a `runVisionPreflight` integration test (M2).
5. Wire `UploadsConfig.Enabled` (L4) or remove the field.

---

## Resolution Status (post-review fixes, 2026-06-19)

| Finding | Severity | Status | Evidence |
|---------|----------|--------|----------|
| Gap A: ChatService.Chat forwards Parts to bus payload | (pre-review) | FIXED | `internal/services/chat_service.go:119-121` — `payload["parts"] = req.Parts` |
| Gap B: dispatcher Parts threading | (pre-review) | FIXED | `internal/agent/dispatcher.go:166,340,470,1256-1257,1299`; `internal/agent/handler.go:529`; non-RouteToAgent branches log warnings at `:549,563,578` |
| C1: OpenAI unresolved file:// URLs | Critical | FIXED | `internal/llm/models.go:48,55,80` — `ToOpenAIDictWithStore(store)` calls `resolveImageURL`; wired at `internal/llm/client.go:277`; 4 new tests in `internal/llm/multimodal_test.go` |
| H1: Upload race window | High | DEFERRED | Requires restructure of two-phase reserve/write/finalize. Low real-world impact (requires concurrent identical-content uploads). Tracked as follow-up. |
| H2: Vision preflight shared timeout | High | FIXED | `internal/agent/vision_preflight.go:95` — `context.WithTimeout` now inside per-image loop |
| H3: FTS does not index image descriptions | High | FIXED | `internal/session/store_sqlite.go:743-764` — `searchContent` computed via `llm.ContentFromParts(msg.Parts, true)` when Parts present; used as the FTS-indexed `content` column value |
| M1: Failure sentinels block re-analysis | Medium | FIXED | Added `AnalysisFailed bool` field on `ImageRef` at `internal/llm/content_parts.go:31`; `HasUndescribedImages` checks it at `:84`; failure path sets flag instead of sentinel string at `vision_preflight.go:107,126-127,138` |
| M2: Missing runVisionPreflight test | Medium | FIXED | 6 new tests added in `internal/agent/vision_preflight_test.go` covering retry-after-failure, dup-URL propagation, empty-response handling, failed-ref inclusion |
| M3: Duplicate-URL refs not propagated | Medium | FIXED | `setImageRefStateByURL` helper at `vision_preflight.go:147-164` updates all same-URL refs on success and failure |
| M4: TUI attachment upload blocks Update goroutine | Medium | DEFERRED | UX issue, not correctness. Tracked as follow-up. |
| M5: Missing nil guard on result.Intent in recursive RouteToAgent | Medium | ACCEPTABLE | Pre-existing pattern; the new Parts field does not materially increase reachability. The dispatcher's classification paths always populate Intent before reaching RouteToAgent. |
| L1: Dead imports cleanup | Low | N/A | False-positive per reviewer; the plan's dead-import list was correctly NOT followed. |
| L2: No log on pre-flight cache hit | Low | DEFERRED | Cosmetic. |
| L3: dbPath race with directory creation | Low | ACCEPTABLE | Behavior is correct; just slightly opaque error message. |
| L4: UploadsConfig.Enabled never checked | Low | FIXED | `internal/daemon/daemon.go:506-509` — `if !uploadCfg.Enabled { uploadDataDir = "" }` skips UploadService creation |
| L5: GCSweep timezone mixing | Low | ACCEPTABLE | Both cutoff and CreatedAt use UTC. Defensive note only. |
| L6: Dead accumulatedContext in RouteToAgent | Low | ACCEPTABLE | Pre-existing pattern, unrelated to vision work. |

### Verification (post-fix)

- `go build ./...` — clean
- `go test ./...` — all packages pass, zero failures
- `go run ./tools/analyzers/mutexio/ ./internal/...` — clean (no I/O-under-mutex violations)
- C1 grep evidence: `ToOpenAIDictWithStore` at `models.go:55`, `client.go:277`, `resolveImageURL` at `models.go:80`
- H2 grep evidence: `context.WithTimeout` at `vision_preflight.go:95`, inside `for _, ref := range toDescribe` at `:89`
- H3 grep evidence: `searchContent := msg.Content` at `store_sqlite.go:750`, `llm.ContentFromParts(msg.Parts, true)` at `:756`, used in `stmt.Exec(... searchContent ...)` at `:764`
- L4 grep evidence: `if !uploadCfg.Enabled` at `daemon.go:506`

### Deferred items follow-up

H1 (upload race), M4 (TUI blocking), L2 (cache-hit log) are tracked as low-priority follow-ups. None block shipping the vision feature. See `docs/plans/vision-deferred-implementation.md` (to be created if not present).
