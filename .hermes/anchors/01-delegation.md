# Verified Anchors - branch 01-delegation (verified at 40f6fa5a)

Grep-verified. Use these exact shapes; do not invent APIs.

- `internal/tools/builtin/platform.go:257` — `delegateRegistry` (subset of AgentRegistry used by the tool)
- `internal/tools/builtin/platform.go:266` — `type DelegateTaskTool struct`; `:272` `NewDelegateTaskTool(registry *agent.AgentRegistry)`; `:276` Name()="delegate_task"; `:317` `Execute(ctx, args map[string]any) (any, error)` — TODAY it is SYNCHRONOUS: args agent_id/message/context, returns `DelegateResult{Success, Error, ...}`. The branch makes spawn ASYNC (C1) — your tests pin the new async contract from the leaf doc, not this old shape.
- `internal/agent/registry.go:35` — `type AgentRegistry struct` (specs map; loops keyed agentID->taskID->loop, "_default")
- `internal/agent/isolation.go:23` — `type ContextIsolation string`; `:65` `BuildSpawnContext(iso ContextIsolation, brief string, artifacts []ArtifactRef, memoryIDs []string, parent []llm.ChatMessage) SpawnContext` — the fork-seed seam leaf 02 extends
- `internal/agent/conversation.go:165` — `type Conversation struct` (messages []llm.ChatMessage + messageTypes parallel array) — fork seeding copies this
- `internal/agent/queue.go` — `QueueType/QueuedMessage/QueueConfig/QueueEventPayload` (queue plumbing the async spawn rides)
- `pkg/id/id.go:18` — `func Generate(prefix string) string` (child/task ids; NEVER time.Now().UnixNano())
- `internal/tools/interface.go:24` — `type Tool interface` (Name/Description/Parameters/Execute...) — new control tools implement it
- `internal/tools/registry.go:81` — `func (r *Registry) Register(tool Tool)`

New symbols your tests reference (contracts C1-C4 in .hermes/contracts/01-delegation.md — undefined today, that IS the RED): ChildRecord/ChildState/DelegateManager (internal/agent), modelselect.ResolveChildRoute/PreflightRoute, subagent_send_message / subagent_steer / subagent_stop tools, workflow tool, fork seeding + descriptor resume.

RED-gate convention (repo precedent commit 537c92fd): if a test file's
undefined-symbol errors break `go vet`/`go build` of the NON-test package,
park it as `<name>_test.go.staged` and say so in your report. If
`go build ./internal/agent` stays green and only `go test` fails on undefined
symbols, keep the file live.

e2e: suite dir `e2e/suites/delegate-seam/` (NEW). Build tag `//go:build e2e`,
import `github.com/caimlas/meept/e2e/harness` (correct path — NOT
"meept/e2e/harness"). `harness.Start(t)` boots a real daemon + FakeLLM;
`s.Fake.SetClassifierOutput(...)`, `s.Fake.SetChatText(...)`,
`s.Fake.EnqueueToolCalls(...)` script the model. Register the suite in
`e2e/manifest.json` (path_map + suites entry).

ANTI-VACUOUS BAR: assert values, error identity, state transitions, payload
fields. t.Log-only or commented-out bodies = rejection. Async tests must
assert the ack returns BEFORE completion and the completion event/record
arrives after (timing via polling with deadline, not sleeps).
