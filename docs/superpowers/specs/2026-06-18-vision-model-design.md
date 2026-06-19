# Vision Model & Multimodal Image Support Design

**Date:** 2026-06-18
**Status:** Approved
**Author:** brainstorming session

## Overview

Add a vision model specialist and full multimodal image support to Meept. Images flow from client (TUI or Flutter GUI) through the daemon to vision-capable LLMs, with description caching to bound token cost. Both UIs gain attachment support via drag-and-drop, paste, and explicit attachment button (GUI only).

## Design Decisions

| Decision | Choice | Alternatives Considered |
|----------|--------|------------------------|
| Routing model | Hybrid: explicit override > specialist > capability fallback | A (specialist only), B (capability swap only) |
| Image transport | File-store with SHA dedup + reference IDs | A (inline base64), C (hybrid size threshold) |
| Description caching | Description-as-cache: first call sends bytes, subsequent turns use cached text | B (parallel metadata), C (search-only) |
| Message representation | `Parts []ContentPart` on `ChatMessage`, precedence over `Content` string | B (sidecar attachments), C (inline references) |
| GUI scope | Redesign input composition (a), attachment button (c), message bubbles (d), drag/paste (f). Strict-to-spec chip display (b). No session badges (e). | — |

## Architecture

### Data Model

#### ContentPart (new, `internal/llm/models.go`)

```go
// ContentPart is one block of a multimodal message. At least one of Text or
// ImageURL is non-empty. Parts with a populated Description have already been
// analyzed by a vision model; downstream code MAY substitute the description
// text for the image bytes (see vision cache policy).
type ContentPart struct {
    Type     string    `json:"type"`               // "text" | "image_url"
    Text     string    `json:"text,omitempty"`
    ImageURL *ImageRef `json:"image_url,omitempty"`
}

// ImageRef references a stored upload. URL is always populated; the daemon
// rewrites it to a data URL before sending to the LLM. Description is the
// cached vision-model description (populated lazily after first analysis).
type ImageRef struct {
    URL         string `json:"url"`                 // "file://<sha256>.<ext>" or "data:..."
    Description string `json:"description,omitempty"`
    MIMEType    string `json:"mime_type,omitempty"`
    Width       int    `json:"width,omitempty"`
    Height      int    `json:"height,omitempty"`
}
```

#### ChatMessage extension

```go
type ChatMessage struct {
    Role       Role          `json:"role"`
    Content    string        `json:"content"`
    Parts      []ContentPart `json:"parts,omitempty"` // NEW. Non-empty => takes precedence for LLM serialization
    Name       string        `json:"name,omitempty"`
    ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
    ToolCallID string        `json:"tool_call_id,omitempty"`
    IsToolError bool         `json:"-"`
    SummaryLevel int         `json:"-"`
    Critical bool            `json:"-"`
}
```

**Backward compatibility rule:** when `Parts` is empty, `Content` (the existing string) is the canonical message. When `Parts` is non-empty, provider serializers emit the parts and ignore `Content`.

A helper `ContentFromParts(parts []ContentPart, useDescription bool) string` synthesizes a text-only string by concatenating text parts and substituting `[image: <description or url>]` for image parts. Used by FTS5 search, summarization, context compaction, memory injection, and any legacy consumer that wants a flat string.

- `useDescription=true`: substitutes `[image: <description>]` for image parts. Used by all consumers after pre-flight has cached the description.
- `useDescription=false`: substitutes `[image: <url>]`. Used only by the pre-flight vision call.

#### Session Message extension (`internal/session/store.go`)

```go
type Message struct {
    // ... existing fields unchanged
    Parts []llm.ContentPart `json:"parts,omitempty"` // NEW
}
```

SQLite schema migration adds `parts TEXT` column (default NULL) to `session_messages`. On read, if `parts` parses to a non-empty slice, populate the field; otherwise the row is text-only as today.

#### Upload table (new)

```sql
CREATE TABLE IF NOT EXISTS uploads (
    id          TEXT PRIMARY KEY,          -- sha256 hex
    path        TEXT NOT NULL,             -- ~/.meept/uploads/<id>.<ext>
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    width       INTEGER,
    height      INTEGER,
    created_at  TEXT NOT NULL,
    refcount    INTEGER DEFAULT 0
);
```

The description cache does NOT live on the upload row — it lives on `ImageRef.Description` inside message `Parts`, because different messages referencing the same upload may have different descriptions (re-analysis, different prompts). `ImageRef.URL` is the join key to `uploads.id`.

### Upload Storage & HTTP Endpoints

**Storage layout:**
- Directory: `~/.meept/uploads/`
- Filename: `<sha256-hex>.<ext>` (e.g., `a1b2c3...f0.png`)
- Dedup: SHA-based. Upload of identical bytes returns existing ID, increments refcount.

**Endpoints** (`internal/comm/http/api_handlers.go`):

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/v1/uploads` | Multipart upload. Accepts `file` field. Returns `{uploads: [{id, path, mime_type, size_bytes, width, height}]}`. |
| `GET` | `/api/v1/uploads/{id}` | Returns raw file bytes with correct `Content-Type`. |
| `GET` | `/api/v1/uploads/{id}/metadata` | Returns JSON metadata without transferring bytes. |
| `DELETE` | `/api/v1/uploads/{id}` | Decrements refcount. File deletion handled by GC. |

**UploadService** (`internal/services/upload_service.go`):
- `Upload(ctx, reader, filename, mimeType) (*Upload, error)` — reads stream, hashes, stores, extracts metadata, inserts/uploads row.
- `Get(ctx, id) (*Upload, error)` — returns metadata + file path.
- `Serve(ctx, id) (io.ReadCloser, mimeType string, error)` — opens file for streaming.
- `Release(ctx, id) error` — decrements refcount.
- `Acquire(ctx, id) error` — increments refcount (called when a message references an upload).

**Config** (`meept.json5` under `daemon.uploads`):
```json5
uploads: {
  enabled: true,
  max_size_mb: 20,
  allowed_types: ["image/png", "image/jpeg", "image/gif", "image/webp"],
  gc_retention_days: 7,
  gc_interval_hours: 24,
}
```

**Security:** all upload endpoints require auth. MIME validated against allowlist on upload and serve. `X-Content-Type-Options: nosniff` set on serve. Filename from upload is sanitized and discarded — only SHA hash and original extension used.

**GC:** background sweep on daemon start and every 24h via `internal/scheduler`. Deletes files where `refcount = 0 AND created_at < now - gc_retention_days`.

**RPC for TUI:** new `upload.upload` bus topic accepts base64-encoded file data over Unix socket JSON-RPC (TUI lacks HTTP multipart). Returns upload descriptor.

### Vision Routing & Model Selection

**Capability:** `CapImages = "images"` already exists (`internal/llm/provider_registry.go:36`). Models declare it in `models.json5`.

**Routing precedence** (implemented in `Dispatcher.ClassifyAndRoute()`):

1. **Explicit model override** — user message contains a model reassignment directive (`@model:glm-4.7-vision`, "use gemini-flash for this", existing dispatcher parser). That model is used regardless of image content.
2. **Explicit specialist** — user addresses `@vision` or dispatcher detects vision-heavy intent (>=1 image AND text requests analysis: "describe", "what's in", "transcribe", "read this", "OCR"). Route to the `vision` specialist agent.
3. **Capability fallback** — image attached, no override, no specialist. Current agent runs, but `AgentLoop.resolveInferenceParams()` calls `Resolver.FindByCapabilities(["images"])` to swap in a vision-capable model for the turn.
4. **No vision model** — `Resolver.FindByCapabilities(["images"])` returns nil. Return structured error: `"no model with 'images' capability is configured; either add one to models.json5 or specify with @model"`.

**Vision specialist agent** — new `vision` executor agent:
- `requires: [images, reasoning]`
- Default model resolved at startup via `Resolver.FindByCapabilities(["images", "reasoning"])`
- System prompt tuned for image analysis
- Standard tool set; produces text descriptions

**Override parser** — extended model-reassignment parser accepts `@vision` (resolves to vision specialist) and `@vision:<modelref>` (specialist with explicit model).

**Model swap location** — extends existing `AgentLoop` model-switching block (`internal/agent/loop.go:1796-1829`) with pre-check: if assembled messages contain `Parts` with `Type: "image_url"` and nil/empty `Description`, and current model lacks `images` capability, run capability fallback before LLM call.

### Vision Description Cache Flow

**First turn with an image:**

1. `AgentLoop` assembles `messages` with `Parts` containing an `ImageRef` where `Description` is empty.
2. Pre-flight check (`visionPreflight()` in `internal/agent/`): for each `ImageRef` with empty `Description`:
   a. Resolve vision-capable model (following routing precedence).
   b. Send single-turn request: system prompt "Describe this image in detail. Include any text visible in the image (OCR), key objects, layout, colors, and any notable features. Be concise but thorough." + user message with image part (real bytes via data URL from upload store) + user text.
   c. Store description on `ImageRef.Description`.
   d. Persist updated `Parts` back to `session_messages.parts`.
3. Main agent loop proceeds using **description text** (not bytes), since description is now cached.

**Subsequent turns:** description is cached, pre-flight skips, main turn uses description text.

**Re-examination:** user says "look again", "re-examine", "look closer", or re-attaches — clears `Description`, forces fresh vision call.

**Pre-flight is the ONLY time real image bytes go to a model.** Once description is cached, everything uses text. The pre-flight call is a separate single-turn LLM request, not part of the main conversation.

**Concurrency:** if the same image appears in multiple messages in the same turn, pre-flight deduplicates by upload ID — one vision call per unique image per session. Concurrent turns in different sessions attaching the same upload get independent descriptions.

**Failure handling:** if pre-flight vision call fails (model error, timeout, rate limit):
- Retry up to 2 times with backoff.
- On final failure, set `Description = "[image analysis failed: <error>]"` and proceed.
- Does NOT block the main turn.

**Non-streaming for pre-flight:** the vision description call is non-streaming (full description needed before proceeding). Main turn retains existing streaming behavior.

### Provider Serialization

**Anthropic** (`internal/llm/anthropic.go`, modify `buildRequest`):

When `msg.Parts` is non-empty, call `partsToAnthropicContent()` instead of the single-text-block path:

```go
func (c *AnthropicClient) partsToAnthropicContent(parts []ContentPart) []anthropicContent {
    var out []anthropicContent
    for _, p := range parts {
        switch p.Type {
        case "text":
            out = append(out, anthropicContent{Type: "text", Text: p.Text})
        case "image_url":
            if p.ImageURL.Description != "" {
                out = append(out, anthropicContent{
                    Type: "text",
                    Text: fmt.Sprintf("[image: %s]", p.ImageURL.Description),
                })
            } else {
                dataURL := c.resolveImageURL(p.ImageURL.URL)
                mimeType, data := parseDataURL(dataURL)
                out = append(out, anthropicContent{
                    Type: "image",
                    Source: &anthropicImageSource{
                        Type:      "base64",
                        MediaType: mimeType,
                        Data:      data,
                    },
                })
            }
        }
    }
    return out
}
```

Requires extending `anthropicContent`:
```go
type anthropicContent struct {
    Type   string               `json:"type"`
    Text   string               `json:"text,omitempty"`
    Source *anthropicImageSource `json:"source,omitempty"` // NEW
    // ... existing fields unchanged
}

type anthropicImageSource struct {
    Type      string `json:"type"`       // "base64"
    MediaType string `json:"media_type"` // "image/png", etc.
    Data      string `json:"data"`       // base64-encoded
}
```

**OpenAI-compatible** (`internal/llm/models.go`, modify `ToOpenAIDict`):

When `m.Parts` is non-empty, emit content as an array of typed blocks:

```go
if len(m.Parts) > 0 {
    content := make([]map[string]any, 0, len(m.Parts))
    for _, p := range m.Parts {
        switch p.Type {
        case "text":
            content = append(content, map[string]any{
                "type": "text",
                "text": p.Text,
            })
        case "image_url":
            if p.ImageURL.Description != "" {
                content = append(content, map[string]any{
                    "type": "text",
                    "text": fmt.Sprintf("[image: %s]", p.ImageURL.Description),
                })
            } else {
                dataURL := resolveImageURL(p.ImageURL.URL)
                content = append(content, map[string]any{
                    "type": "image_url",
                    "image_url": map[string]any{"url": dataURL},
                })
            }
        }
    }
    msg["content"] = content
} else {
    msg["content"] = m.Content
}
```

**Upload store injection** — LLM client needs access to upload store to resolve `file://sha.png` to bytes. New `WithUploadStore(UploadStore)` option on both `AnthropicClient` and `Client`:

```go
type UploadStore interface {
    Load(ctx context.Context, id string) (data []byte, mimeType string, err error)
}
```

**Description substitution is enforced at the serializer level:** both providers check `Description != ""` before loading bytes. Only pre-flight (which passes parts with empty Description) sends real image data.

### GUI (Flutter) Redesign

Scope: redesign input composition (a), attachment button (c), message bubbles (d), drag/paste (f). Strict-to-spec chip display (b). No session badges (e).

#### a. Input Area Composition (`chat_input.dart`)

Replace `_preparePayload(String text) -> String` with `_preparePayload(String text) -> List<ChatMessagePart>`. The send method passes structured parts.

New API models (`api_models.dart`):
```dart
class ChatMessagePart {
  final String type;       // 'text' | 'image_url'
  final String? text;
  final ImageRef? imageUrl;
  // ...
}

class ImageRef {
  final String url;
  final String? description;
  final String? mimeType;
  // ...
}
```

`ChatMessage` freezed model gains `List<ChatMessagePart>? parts`. Regenerated via build_runner.

`ChatRequest` (in `sdk_client.dart`) gains optional `parts` field alongside `message`. When `parts` is non-empty, daemon uses them; when absent, `message` string is used (backward compat).

Attachment state:
```dart
class Attachment {
  final String uploadId;
  final String filename;
  final String mimeType;
  final int sizeBytes;
}
```

`_attachments` becomes `List<Attachment>` instead of `List<String>`.

#### b. Attachment Chip Display (strict-to-spec)

Above the text input, a single-line horizontal scroll of chips. Each chip: `[filename.ext]` in smaller green text. No thumbnails, no click handler, no hover. A small `x` on each chip allows removal before sending. Minimal interactivity — strictly display otherwise.

#### c. Attachment Button

Layout in the input row, left-to-right: `[attachment button] [text field] [send button]`.

Single paperclip icon (`Icons.attach_file`) positioned LEFT of the text field. Tapping opens native file picker (`file_picker` package) filtered to allowed image types. Single button, type detection server-side.

#### d. Message Bubble Rendering (`chat_message_bubble.dart`)

User messages with image parts render as:
1. Text content (if any) in normal style.
2. Below text: row of `[filename.ext]` chips in green text — same visual as input chips, maintaining consistency. No inline thumbnails.

Assistant responses need no changes — always text.

#### f. Drag-and-Drop & Paste

- **Drag-and-drop:** entire chat tab is a drop target. Dropped files uploaded via uploads endpoint, added to `_attachments`. Dropped text inserted into textarea (existing behavior).
- **Paste image bytes from clipboard:** detected via `super_clipboard` or `pasteboard` package. Bytes uploaded via uploads endpoint, added to `_attachments`. Not pasted as text.
- **Paste file path string:** existing `_detectFilePaths()` behavior — if path has image MIME extension, file is uploaded via endpoint and added as attachment.
- **Plain text paste:** existing `_detectPaste()` behavior preserved.

Both paths funnel through the upload endpoint — no inline base64 in the client.

### TUI Parity (`internal/tui/models/chat.go`)

Per spec: "no explicit UI elements will be added except in how attached images will be represented — the same as pasted text, except it will be `[file:name.ext]`."

Changes:
1. `detectAndAttachFile` extended: when detected path has image MIME extension (`.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`), file uploaded via RPC (`upload.upload` bus topic) and attachment tagged as image upload ID.
2. `[filename.ext]` chip rendering unchanged.
3. `doSendMessage()` builds `Parts` instead of prepending text lines. RPC payload includes structured parts.
4. Paste detection extended to recognize base64 image data URLs.

New RPC method: `upload.upload` (base64-encoded file data over Unix socket JSON-RPC). Returns upload descriptor.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Upload exceeds max_size_mb | HTTP 413, clear error message |
| Disallowed MIME type | HTTP 415, lists allowed types |
| Vision model not configured | Structured error to client with guidance |
| Pre-flight vision call fails | Retry 2x, then stub description, proceed |
| Upload file missing on disk | HTTP 404, log warning |
| Image dimensions exceed model limits | Warning in logs, send anyway (model may reject) |

## Testing Strategy

- **Unit:** `ContentPart` serialization, `ContentFromParts` text synthesis, `partsToAnthropicContent` image+text mixing, `ToOpenAIDict` with parts, upload store CRUD, refcount semantics.
- **Integration:** upload -> attach -> send -> pre-flight -> main turn flow; session reload with parts; GC sweep.
- **Provider mock:** mock vision model returns canned description, verify bytes sent on first call and description substituted on second.
- **TUI:** file path detection for image extensions triggers upload RPC.
- **Flutter:** widget tests for attachment button, chip display, drag-drop, paste handling, message bubble with image parts.

## Defaults & Assumptions

| Parameter | Default | Configurable |
|-----------|---------|--------------|
| Max upload size | 20MB | `daemon.uploads.max_size_mb` |
| Allowed MIME types | PNG, JPEG, GIF, WebP | `daemon.uploads.allowed_types` |
| GC retention | 7 days | `daemon.uploads.gc_retention_days` |
| GC sweep interval | 24h | `daemon.uploads.gc_interval_hours` |
| Vision pre-flight timeout | 30s | hardcoded (future: config) |
| Vision pre-flight retries | 2 | hardcoded |
| Vision pre-flight system prompt | "Describe this image in detail..." | hardcoded |
| Re-examination triggers | "look again", "re-examine", "look closer", re-attach | hardcoded |
| Upload ID format | SHA-256 hex | N/A |
| MIME extraction | Go `image.DecodeConfig`, fallback `http.DetectContentType` | N/A |
| Thumbnail generation | None (GUI fetches full image, Flutter caches) | N/A |
| Pre-flight streaming | Non-streaming | N/A |

## Files Touched (summary)

### Go — new files
- `internal/services/upload_service.go` — UploadService
- `internal/agent/vision_preflight.go` — vision pre-flight logic

### Go — modified files
- `internal/llm/models.go` — ContentPart, ImageRef, ChatMessage.Parts, ToOpenAIDict, ContentFromParts
- `internal/llm/anthropic.go` — anthropicContent/anthropicImageSource, partsToAnthropicContent, buildRequest
- `internal/llm/provider_registry.go` — (CapImages already exists, no change)
- `internal/llm/interface.go` — UploadStore interface
- `internal/session/store.go` — Message.Parts field
- `internal/session/store_sqlite.go` — schema migration, parts CRUD
- `internal/session/session.go` — SaveMessages/GetMessages parts handling
- `internal/services/chat_service.go` — ChatRequest.Parts, forward to agent loop
- `internal/comm/http/api_handlers.go` — upload endpoints
- `internal/comm/http/server.go` — route registration for uploads
- `internal/agent/loop.go` — vision pre-flight integration, model swap for images
- `internal/agent/dispatcher.go` (or equivalent) — @vision routing, image-aware intent classification
- `internal/daemon/components.go` — UploadService wiring
- `internal/config/schema.go` — uploads config block
- `internal/tui/models/chat.go` — image path detection, upload RPC, Parts building on send

### Flutter — new files
- (possibly) `ui/flutter_ui/lib/models/chat_message_part.dart` — if not inlined into api_models.dart

### Flutter — modified files
- `ui/flutter_ui/lib/models/api_models.dart` — ChatMessagePart, ImageRef, ChatMessage.parts
- `ui/flutter_ui/lib/models/api_models.freezed.dart` — regenerated
- `ui/flutter_ui/lib/models/api_models.g.dart` — regenerated
- `ui/flutter_ui/lib/services/sdk_client.dart` — ChatRequest.parts, upload method
- `ui/flutter_ui/lib/features/chat/chat_input.dart` — full input area redesign
- `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart` — image part rendering
- `ui/flutter_ui/lib/features/chat/chat_view.dart` — drop target wrapper

### Config
- `config/meept.json5` — `daemon.uploads` section template
