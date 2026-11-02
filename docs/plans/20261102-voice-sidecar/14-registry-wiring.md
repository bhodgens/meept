# Registry + daemon wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate tests ALREADY EXIST
> (parked .staged from leaf 00T T8) - make them pass; do NOT modify gate
> tests. Do NOT commit. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** C8 instance registry + the whole-daemon wiring: components.go voice block, talk loop (prompt -> employee turn -> supervisor -> say, with pipelining), release-hook fan-out, secrets Broker.Value caller, bus topics
- **Dependencies:** leaves 05, 06, 09, 10, 11, 12, 13 (ALL of wave B/C)
- **Estimated Context:** 110K (largest leaf; serial dispatch; if the agent reports context pressure, the orchestrator splits wiring from talk-loop in-session)
- **Concurrency Group:** D (SERIAL - components.go is the hottest shared file)
- **Files this leaf owns:** internal/voice/registry.go, internal/voice/talkloop.go, internal/daemon/components.go (voice block only), internal/daemon/voice_wiring.go, scripts/audit-secrets-value-callers.py (the real Value caller row)

## Goal

Two things. (1) The registry: Acquire/Release/Live per C8 - one live call
per target, max_concurrent_calls cap, per-instance endpoint pin. (2) The
talk loop: the per-call goroutine that consumes final prompts, runs the
employee turn on the pinned endpoint, splits sentences, pipelines
supervisor checks (N+1 checked while N plays), sends allowed say frames,
handles interrupt (cancel turn context + splice clear), and ends the call
on max duration / silence / hangup.

## Context

Talk loop is the integration point for every contract: C1 frames (leaf
03), C6 supervisor (leaf 09), C7 brief via binding (leaf 12), splice
(leaf 06) as the audio pump, registry slot held for the call. Per-call
context: cancel on interrupt (draft cancel), on end, on max_call_minutes,
on silence_prompt_seconds silence (prompt once via filler-class say, then
hangup on repeat).
Pipelining: sentences flow through a small chan; supervisor Check runs in
a worker goroutine one sentence ahead of playback. Barge-in drops the chan
and cancels in-flight Check.
Endpoint pinning: the employee's model field / voice.voice_model alias
resolves through the EXISTING llm resolver (do not construct clients
here; wiring injects a ChatterLike per pin - registry-loop chatter lesson:
dispatched loops built by the agent registry use RegistryConfig.LLMClient;
verify the voice loop gets the pinned client via WithLLMChatter, not the
single default - see internal/llm/AGENTS.md session-affinity leaf-07
lesson 1).
components.go wiring (guarded by cfg.Voice.Enabled, ordered AFTER
msgBus/push/employee/session/loop manager exist):
1. Broker.Value("telnyx_api_key") -> telnyx.Client (ALLOWLIST ROW in the
   audit script, justification: outbound API token source, in-process
   only).
2. CallStore open ($MEEPT_HOME/voice/calls.db).
3. Registry, Manager (Codec L16/16000, ReleaseHook fan-out: registry
   Release + binding OnCallEnd + childproc NoteCallEnd + effect complete).
4. ChildProc, TextServer, Supervisor, Binding (WireVoiceEmployee).
5. VoiceRoutes registered on the HTTP mux (leaf 10) + media route
   SpliceRunner closure (leaf 06 RunSplice with per-call AudioPair).
6. Dialer exposed to the employee side: a builtin tool voice_call
   (category platform, permission rule in pkg/security BuiltinRules -
   unknown actions DENY by default, so the rule is mandatory) OR an
   employee-only RPC. Choose the tool path: NewVoiceCallTool(dialer) with
   the employee tool-grant gate; register via the toolprov provider seam
   (voice group), not direct registration.
7. Shutdown: ChildProc.Stop, TextServer close, CallStore close - in
   Daemon.Stop order (children before stores).
Bus topics (typed, internal/voice/topics.go): voice.call.progress (leaf
12), voice.call.started, voice.call.ended (payload {call_id, employee_id,
number, direction, outcome, duration_ms}). WS class "event"; no relay
subscription added.
Pointer-capture order lesson (repo defect class): every New* wiring call's
fields must be assigned BEFORE the constructor captures them - check line
order in the voice block.

## Verified Anchors

- internal/daemon/components.go:5605-5610 - speakRouter wiring block (placement precedent for the voice block)
- internal/daemon/components.go:2307 - wireSlackChannel (optional-channel wiring precedent)
- internal/toolprov - provider seam (Register/Factory; tool gate applies to every execution)
- pkg/security/permissions.go BuiltinRules - mandatory tool action rule
- scripts/audit-secrets-value-callers.py - ALLOWED_VALUE_CALLERS
- master.md C8 - Registry interface

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/voice/registry.go - C8 verbatim
type Instance struct{ CallControlID, EmployeeID, Target, EndpointPin string }
func NewRegistry(maxConcurrent int) *Registry
// Acquire/Release/Live per C8; ErrBusy, ErrDuplicateTarget
// internal/voice/talkloop.go
type TalkLoop struct{ ... }
func NewTalkLoop(deps TalkDeps) *TalkLoop
func (t *TalkLoop) Run(ctx context.Context, call Instance) error  // lives for the call
```

### What This Leaf Consumes

- ALL prior contracts C1-C9 (this leaf is the assembly point)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate tests; confirm RED | mv internal/voice/registry_test.go.staged -> registry_test.go | go test -p 2 ./internal/voice/ -run TestRegistry | undefined: NewRegistry | RED confirmed | - |
| T2 | Registry: Acquire/Release/Live, cap + duplicate-target, -race under concurrent acquire | Create: internal/voice/registry.go | go test -p 2 -race ./internal/voice/ -run TestRegistry | - | PASS | Given cap 2, 3 acquires; When third; Then ErrBusy |
| T3 | TalkLoop: prompt->turn->sentences->pipelined supervisor->say; interrupt cancels turn + drops pipeline; max-duration and silence hangup with reasons | Create: internal/voice/talkloop.go, talkloop_test.go | go test -p 2 -race ./internal/voice/ -run TestTalkLoop | - | PASS | Given 2-sentence draft, deny on sentence 2; When run; Then only sentence 1 in say frames |
| T4 | Pipelining pin: supervisor Check for sentence N+1 starts before say(N) completes (fake clock/order recorder) | (same) | go test -p 2 ./internal/voice/ -run TestTalkLoopPipelining | - | PASS | Given slow playback; When pipelined; Then Check(N+1) start < say(N) end |
| T5 | voice_call tool + toolprov registration + BuiltinRules action row | Create: internal/tools/builtin/voice_call.go (or voice group under toolprov), Modify: pkg/security/permissions.go | go test -p 2 ./internal/tools/... -run TestVoiceCall; go build ./... | - | PASS | Given employee without grant; When tool called; Then denied by gate; Given grant; Then Dialer invoked |
| T6 | components.go voice block + voice_wiring.go + shutdown order + audit-script allowlist row | Create: internal/daemon/voice_wiring.go; Modify: internal/daemon/components.go, scripts/audit-secrets-value-callers.py | go build ./cmd/meept-daemon/; python3 scripts/audit-secrets-value-callers.py | - | exit 0 | Given voice.enabled=true scratch boot; When started; Then log line names voice routes armed + child path |
| T7 | Scratch-daemon live gate (master Integration Test Plan step 4): scratch MEEPT_HOME, fake telnyx base URL, simulated call.initiated reaches Manager, refused dial logs named reason; capture log evidence into the completion report | - | (manual recipe in report) | - | evidence cited | Given scratch boot; When fake webhook POST; Then manager log line with ccid |
| T8 | Format/vet/race + make graphs (new routes/topics) | - | gofmt -l internal/voice internal/daemon internal/tools/builtin; go vet ./internal/voice/ ./internal/daemon/; make graphs && make graphs-check | - | clean+fresh | - |

## Self-Verification Checklist

- [ ] Gate tests passing untouched
- [ ] Release hook fan-out covers ALL four sinks (registry, binding, childproc, effect) on EVERY outcome
- [ ] Broker.Value caller in the allowlist with justification; token never logged
- [ ] voice_call tool goes through the tool gate (G1-G4), not around it
- [ ] Pointer-capture order checked line-by-line in the wiring block
- [ ] Pinned endpoint actually reaches the voice loop (WithLLMChatter verified, not the default client)
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Live gate evidence:** boot log lines + fake-webhook trace
4. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] C8 exact; concurrency cap and duplicate-target proven under -race
- [ ] Wiring order: dependencies constructed before capture (line numbers cited)
- [ ] Shutdown order: child stopped before stores closed
- [ ] graphs regenerated; ws-event classification untouched
- [ ] Live gate evidence present (hermetic tests alone do not prove components.go wiring - repo rule)

Output: APPROVED or specific gaps.

## Notes

If context pressure hits mid-leaf: commit-ready split is (a) registry +
talkloop (internal/voice only) and (b) wiring + tool + live gate. The
orchestrator dispatches (a) then (b); do not self-split.
