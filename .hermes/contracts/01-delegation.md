## Interface Contracts

Cross-leaf contracts live here once. Leaves consume the exact signatures
below; they never restate full definitions. Master C1/C2/C3 (restated here
for the executing agent) are the authoritative surface.

### M-C1: Delegation seam (from master; Owner: this branch)

```go
// internal/agent/delegate.go (NEW seam; keeps byte-compatible legacy delegate_task)
type DelegateRequest struct {
    AgentID          string            `json:"agent_id"`
    Message          string            `json:"message"`
    Context          string            `json:"context,omitempty"`
    OutputSchema     json.RawMessage    `json:"output_schema,omitempty"`
    Mode             string            `json:"mode,omitempty"`   // "spawn"|"fork"|"acp" (default spawn)
    Model            string            `json:"model,omitempty"`  // alias or model ref
    Provider         string            `json:"provider,omitempty"`
    ReasoningEffort  string            `json:"reasoning_effort,omitempty"`
    Depth            int               `json:"depth,omitempty"`  // max nesting, default 1
}
type DelegateResult struct {  // EXPANDS today's platform.go:309-314
    AgentID string `json:"agent_id"`
    Success bool   `json:"success"`
    Response string `json:"response,omitempty"`
    Error    string `json:"error,omitempty"`
    ChildID  string `json:"child_id,omitempty"`   // NEW: steer/interrupt key
    Status   string `json:"status,omitempty"`     // running|completed|interrupted
}
```

### B1: Child registry + async spawn core (Owner: 01-spawn-async.md)

```go
// internal/agent/delegate.go
type ChildState string // "pending"|"running"|"completed"|"interrupted"|"failed"
type ChildRecord struct {
    ChildID         string        `json:"child_id"`
    AgentID         string        `json:"agent_id"`
    ParentSessionID string        `json:"parent_session_id"`
    Mode            string        `json:"mode"`             // spawn|fork|acp
    State           ChildState    `json:"state"`
    Model           string        `json:"model,omitempty"`
    Provider        string        `json:"provider,omitempty"`
    ReasoningEffort string        `json:"reasoning_effort,omitempty"`
    CreatedAt       time.Time     `json:"created_at"`
    Result          string        `json:"result,omitempty"`  // set on completed
    Err             string        `json:"err,omitempty"`     // set on failed
    Descriptor      ChildDescriptor `json:"descriptor,omitempty"` // B6 durable record
}
type ChildOptions struct { Provider, Model, ReasoningEffort string }

type DelegateManager struct { /* reg *AgentRegistry */ }
func NewDelegateManager(reg *AgentRegistry, publisher func(topic string, payload any), cancelHook func(childID string) error) *DelegateManager
func (m *DelegateManager) Spawn(ctx context.Context, req DelegateRequest, parentSessionID string) (DelegateResult, error)
    // async: returns CHILDID + Status="running" immediately; a goroutine
    // runs registry.RunAgent (spawn/fork) or the ACP provider (Mode acp)
    // and on settlement publishes bus topic "agent.delegate.result"
    // payload {child_id, status, response, error} and updates the record.
    // routeResolver field (default identity) lets 04-model-selection pin
    // the per-child route before runChild; see B4.
func (m *DelegateManager) Children(parentSessionID string) []ChildRecord
func (m *DelegateManager) Lookup(childID string) (ChildRecord, bool)
func (m *DelegateManager) Join(ctx context.Context, parentSessionID, childID string, timeout time.Duration) (DelegateResult, error)
    // bounded wait for the child to settle; polls the record / wedges on the
    // settlement channel; returns completed/interrupted/failed result.
func (m *DelegateManager) RouteResolver func(route ChildOptions, req DelegateRequest) (ChildOptions, error) // 04 sets via SetRouteResolver
// Consumers: 02-fork-seed (fork seed hook), 03-child-control (tools),
//   04-model-selection (route resolver), 05-workflow-tool (fan-out), 06-descriptor-resume (persist).
```

### B2: History-seeded fork (Owner: 02-fork-seed.md)

```go
// internal/agent/delegate_fork.go
// completedTurnPrefix mirrors dsh subagent-fork-in-process/index.ts:46-53:
// the seed is the parent session's messages up to and including its LAST
// completed turn; the in-flight turn is excluded. Returns empty when no turn
// has completed (child then starts fresh). Capture ONCE at spawn and store
// in the child's transcript (the seed rides the record's Descriptor).
func completedTurnPrefix(conversation *Conversation) []llm.ChatMessage
// DelegateManager.Spawn routes Mode=="fork" through this hook (a package var
// `forkSeedFunc`, default nil -> fresh), seeding the child via the existing
// BuildSpawnContext(IsolationSharedTranscript, ...) path (isolation.go:65).
// Consumers: 01-spawn-async (routing), B6 (seed durability in descriptor).
```

### B3: Child control tools (Owner: 03-child-control.md)

```go
// Model-facing tools registered in internal/tools/builtin (snake_case):
//   subagent_send_message{child_id, message} -> controller.SendMessage:
//       running child  -> Dispatcher.SteerActiveAgent(child.ConversationID,
//                        msg, source="subagent_send_message") (dispatcher.go:4758)
//       idle/finished  -> new turn via DelegateManager.Join + seed resume (B6)
//       acp child      -> *acp.Session.Send (acp/session.go:262)
//   subagent_interrupt{child_id} -> controller.Interrupt: mark interrupted +
//       cancel the child's run ctx (registry/queue path) or acp.Session.Cancel (acp/session.go:303).
//   subagent_list_children{parent_session_id?} -> controller.ListChildren
//       derived from the C1 child registry (DelegateManager.Children).
// Mirrors dsh tool-subagent-control/index.ts:29-112.
// Consumers: 05-workflow-tool (join), 02-goals branch (round driver).
```

### B4: Per-child model / route selection (Owner: 04-model-selection.md)

```go
// internal/agent/modelselect/modelselect.go
type ModelSelectionPolicy struct { Routes []AllowedModelRoute } // {Provider, Model string}
func ResolveChildRoute(parent ChildOptions, configured *ChildOptions, req DelegateRequest,
        policy *ModelSelectionPolicy, enabled bool) (*ChildOptions, error)
    // mirrors dsh model-selection.ts:99-128 requestedAgentOptions + :139-159
    // assertAllowedModelSelection. Provider+model must be supplied together
    // (delegation_model_request rule); empty string values rejected;
    // policy nil permits only the parent route; route changed + no effort ->
    // configured route-owned effort cleared. When no selection is requested,
    // returns configured (may be nil -> parent route).
func PreflightRoute(ctx context.Context, resolver *llm.Resolver, opts ChildOptions) error
    // dsh preflightChildLlmRoute (model-selection.ts:182-202): the route must
    // resolve through meept's resolver (HasAlias / provider reachable) BEFORE
    // the child spawns; errors visibly at the tool boundary.
// DelegateManager.SetRouteResolver wires this into Spawn (B1).
// Consumers: 01-spawn-async.
```

### B5: Workflow orchestration tool (Owner: 05-workflow-tool.md)

```json
// Model-facing "workflow" tool executing a confined program (master C3):
//   { engine: "dsh-archetype",
//     script: "<js>",  // top-level await; body hooks:
//       agent(prompt,{schema?,label?,phase?,provider?,model?}) -> child result|validate-object|null
//       pipeline(items, ...stages)  // per-item, no barrier
//       parallel(thunks)            // concurrency barrier
//       phase(title), log(message), args
//     max_concurrent_agents, max_total_agents, max_items_per_call,
//     sync_timeout_ms, run_in_background }
// Confinement: reuse meept's goja codemode VM (internal/codemode/sandbox.go:
//   sec note 3-14, New at :124) - NO fs/network/timer/Node APIs; "the agents
//   do the work". Caps FAIL-CLOSE (fatal, never narrower): mirror dsh
//   workflow-ptc/index.ts:106-112 defaults maxConcurrentAgents=0->
//   min(16,max(1,cores-2)), maxTotalAgents=1000, maxItemsPerCall=4096,
//   syncTimeoutMs=5000; an explicit value over the ceiling is INVALID_ARGUMENT.
// Fan-out via B1 DelegateManager.Spawn; join via B1 Join.
// Returns {kind:"foreground"|"background", runId, agentsStarted, result} or
//   {kind:"background", jobId, runId} respectively (tool-workflow/src/index.ts:378-408).
// stop reasons: completed | cancelled | error map to isError (index.ts:197-210).
// Result rendering capped (default 50000 chars, truncated with notice).
// Consumers: 02-goals branch MAY reuse the fan-out engine.
```

### B6: Durable child descriptor + cold resume (Owner: 06-descriptor-resume.md)

```go
// internal/agent/delegate_descriptor.go
type ChildDescriptor struct {
    Version          int    `json:"version"`           // 1
    Mode             string `json:"mode"`
    Provider         string `json:"provider,omitempty"`
    Label            string `json:"label,omitempty"`
    AgentProvider    string `json:"agent_provider,omitempty"` // local|acp
    AgentModel       string `json:"agent_model,omitempty"`
    Reasoning        string `json:"reasoning,omitempty"`
    ParentSessionID  string `json:"parent_session_id"`
    SeedPayload      string `json:"seed_payload,omitempty"`   // fork seed (B2), durable
    CreatedAt        time.Time `json:"created_at"`
}
// The descriptor is stamped onto ChildRecord.Descriptor at spawn (B1 field),
// persisted with the child record so an idle/restarted daemon can cold-resume
// an unfinished delegation (mode/provider/model/seed from the descriptor, no
// live Agent load needed). Durable store: child records persisted under the
// parent session (project into session storage + a DelegateManager.Persist()
// commit on the store the daemon already owns). Cold-resume entry:
//   DelegateManager.Resume(ctx, childID) -> re-spawns the child from its
//   descriptor (re-running the seed/fresh spawn, re-pinning model/route).
// Mirrors dsh subagent/descriptor (descriptor.ts:48-86,230-269) + manager.coldResume
// (manager.ts:1349-1404).
// Consumers: 01-spawn-async (record/descriptor fields), B3 (resume on send_message to idle child).
```

