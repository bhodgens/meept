// Package builtin — delegate_workflow.go implements the model-facing
// "workflow" (orchestrate) tool (dsh-convergence leaf
// docs/plans/20261102-dsh-convergence/01-delegation/05-workflow-tool.md,
// contract C3): a confined JavaScript program whose agent()/pipeline()/
// parallel()/phase()/log()/args helpers fan work out across delegated
// subagents (via the C1 DelegateManager Spawn+Join seam) and return a JSON
// value, or a background job id.
//
// Confinement: the script body runs inside meept's codemode goja VM
// (internal/codemode/sandbox.go — no fs/network/timer/Node APIs; the no-fs
// gate is a hard invariant and this leaf never loosens it). The ONLY Go
// bridge is `tools.agent`, which maps to DelegateManager.Spawn+Join. The
// composition helpers (parallel/pipeline/phase/log/args) are confined JS
// spliced ahead of the user script — control-flow and narration only, no new
// privileged globals.
//
// Caps are FAIL-CLOSE: an explicit value over the engine ceiling is
// INVALID_ARGUMENT (never clamped), and a cap trip at runtime
// (max_total_agents, max_items_per_call) or a misused hook ends the run as a
// fatal script error — never a silently narrower configuration. stop reasons
// completed|cancelled|error map to ok / isError, and the rendered result is
// capped (default 50000 chars, truncated with a notice).
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/codemode"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
	"github.com/dop251/goja"
)

// Engine/contract constants.
const (
	// workflowDefaultMaxTotal is the default ceiling on the total number of
	// agents a single run may spawn (dsh workflow-ptc Config default).
	workflowDefaultMaxTotal = 1000
	// workflowDefaultMaxItemsPerCall bounds a single pipeline/parallel call's
	// item/thunk count (dsh workflow-ptc default).
	workflowDefaultMaxItemsPerCall = 4096
	// workflowDefaultSyncTimeoutMs bounds each agent() Spawn+Join wait.
	workflowDefaultSyncTimeoutMs = 5000
	// workflowDefaultEngineTimeout bounds the whole script execution (wall
	// clock) so a runaway body is hard-killed, never a hang.
	workflowDefaultEngineTimeout = 30 * time.Minute
	// workflowRenderCap is the maximum rendered result length (dsh renders
	// foreground results capped; truncation carries a notice).
	workflowRenderCap = 50000
	// engine ceilings: an explicit value above any of these is
	// INVALID_ARGUMENT (fail-close, never clamped).
	workflowMaxConcurrentCeiling = 64
	workflowMaxTotalCeiling      = 100000
	workflowMaxItemsCeiling      = 65536
)

// Stop reason vocabulary (dsh: completed | cancelled | error).
const (
	workflowStopCompleted = "completed"
	workflowStopCancelled = "cancelled"
	workflowStopError     = "error"
)

// Error classes surfaced verbatim to the model (fail-close surface).
const (
	workflowClassScriptParse = "SCRIPT_PARSE"
	workflowClassMetaInvalid = "META_INVALID"
	workflowClassInvalidArg  = "INVALID_ARGUMENT"
	workflowClassAgentCap    = "AGENT_CAP"
	workflowClassItemCap     = "ITEM_CAP"
)

// metaStatementRe rejects a Claude-Code-style `export const meta` statement
// in the script body (dsh workflow-ptc META_STATEMENT); meta belongs in the
// tool's meta argument.
var metaStatementRe = regexp.MustCompile(`(?m)\bexport\s+const\s+meta\b`)

// kebabCaseRe validates meta.name is kebab-case.
var kebabCaseRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ---------------------------------------------------------------------------
// Parsed tool arguments, meta and config
// ---------------------------------------------------------------------------

// workflowParams is the decoded parameter object of the workflow tool.
type workflowParams struct {
	Script          string        `json:"script"`
	Meta            *workflowMeta `json:"meta"`
	AgentID         string        `json:"agent_id"`
	MaxConcurrent   int           `json:"max_concurrent_agents"`
	MaxTotal        int           `json:"max_total_agents"`
	MaxItemsPerCall int           `json:"max_items_per_call"`
	SyncTimeoutMS   int           `json:"sync_timeout_ms"`
	RunInBackground bool          `json:"run_in_background"`
}

type workflowMeta struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	WhenToUse   string          `json:"when_to_use"`
	Phases      []workflowPhase `json:"phases"`
}

type workflowPhase struct {
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// WorkflowConfig is the exported engine configuration. Caps are enforced
// fail-close: an out-of-range value is a validation error, never a clamp.
type WorkflowConfig struct {
	MaxConcurrent   int
	MaxTotal        int
	MaxItemsPerCall int
	SyncTimeout     time.Duration
	EngineTimeout   time.Duration
	MemoryLimit     uint64
	// DefaultAgent is the agent_id an agent() helper with no explicit
	// agent_id (opts or per-parameter default) falls back to. An empty
	// DefaultAgent with no explicit selection makes agent() fail closed.
	DefaultAgent string
}

// defaultWorkflowConfig applies the dsh engine defaults.
func defaultWorkflowConfig(cfg *WorkflowConfig) {
	if cfg == nil {
		*cfg = WorkflowConfig{}
		return
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = workflowDefaultMaxConcurrent()
	}
	if cfg.MaxTotal <= 0 {
		cfg.MaxTotal = workflowDefaultMaxTotal
	}
	if cfg.MaxItemsPerCall <= 0 {
		cfg.MaxItemsPerCall = workflowDefaultMaxItemsPerCall
	}
	if cfg.SyncTimeout <= 0 {
		cfg.SyncTimeout = time.Duration(workflowDefaultSyncTimeoutMs) * time.Millisecond
	}
	if cfg.EngineTimeout <= 0 {
		cfg.EngineTimeout = workflowDefaultEngineTimeout
	}
}

// workflowDefaultMaxConcurrent mirrors dsh's default: min(16, max(1, cores-2)).
func workflowDefaultMaxConcurrent() int {
	return min(16, max(1, runtime.NumCPU()-2))
}

// ---------------------------------------------------------------------------
// Host-side validation (run before ANY child spawn)
// ---------------------------------------------------------------------------

// validateScript parse-checks the body host-side and rejects a
// Claude-Code-style meta statement. A failure is workflowClassScriptParse —
// fatal, before any child runs.
func validateScript(script string) error {
	if strings.TrimSpace(script) == "" {
		return fmt.Errorf("workflow: %s: script is required", workflowClassScriptParse)
	}
	if metaStatementRe.MatchString(script) {
		return fmt.Errorf("workflow: %s: script may not carry an `export const meta` statement; pass meta as the tool's meta argument instead", workflowClassScriptParse)
	}
	if _, err := goja.Compile("workflow.js", "(async()=>{\n"+script+"\n})()", false); err != nil {
		return fmt.Errorf("workflow: %s: %w", workflowClassScriptParse, err)
	}
	return nil
}

// validateMeta checks the meta vocabulary. A failure is
// workflowClassMetaInvalid — fatal.
func validateMeta(m *workflowMeta) error {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("workflow: %s: meta.name is required", workflowClassMetaInvalid)
	}
	if !kebabCaseRe.MatchString(m.Name) {
		return fmt.Errorf("workflow: %s: meta.name %q must be kebab-case", workflowClassMetaInvalid, m.Name)
	}
	if strings.TrimSpace(m.Description) == "" {
		return fmt.Errorf("workflow: %s: meta.description is required", workflowClassMetaInvalid)
	}
	for i, ph := range m.Phases {
		if strings.TrimSpace(ph.Title) == "" {
			return fmt.Errorf("workflow: %s: meta.phases[%d].title is required", workflowClassMetaInvalid, i)
		}
	}
	return nil
}

// validateConfig applies defaults and rejects an explicit value outside the
// engine ceiling as workflowClassInvalidArg (never clamped).
func validateConfig(p workflowParams, callerDefault string) (WorkflowConfig, error) {
	cfg := WorkflowConfig{DefaultAgent: callerDefault}
	if p.AgentID != "" {
		cfg.DefaultAgent = p.AgentID
	}
	cfg.MaxConcurrent = workflowDefaultMaxConcurrent()
	if p.MaxConcurrent > 0 {
		if p.MaxConcurrent < 1 || p.MaxConcurrent > workflowMaxConcurrentCeiling {
			return cfg, fmt.Errorf("workflow: %s: max_concurrent_agents %d exceeds the engine ceiling (%d..%d)", workflowClassInvalidArg, p.MaxConcurrent, 1, workflowMaxConcurrentCeiling)
		}
		cfg.MaxConcurrent = p.MaxConcurrent
	}
	cfg.MaxTotal = workflowDefaultMaxTotal
	if p.MaxTotal > 0 {
		if p.MaxTotal > workflowMaxTotalCeiling {
			return cfg, fmt.Errorf("workflow: %s: max_total_agents %d exceeds the engine ceiling (%d)", workflowClassInvalidArg, p.MaxTotal, workflowMaxTotalCeiling)
		}
		cfg.MaxTotal = p.MaxTotal
	}
	cfg.MaxItemsPerCall = workflowDefaultMaxItemsPerCall
	if p.MaxItemsPerCall > 0 {
		if p.MaxItemsPerCall > workflowMaxItemsCeiling {
			return cfg, fmt.Errorf("workflow: %s: max_items_per_call %d exceeds the engine ceiling (%d)", workflowClassInvalidArg, p.MaxItemsPerCall, workflowMaxItemsCeiling)
		}
		cfg.MaxItemsPerCall = p.MaxItemsPerCall
	}
	if p.SyncTimeoutMS > 0 {
		if p.SyncTimeoutMS < 1 {
			return cfg, fmt.Errorf("workflow: %s: sync_timeout_ms must be positive", workflowClassInvalidArg)
		}
		cfg.SyncTimeout = time.Duration(p.SyncTimeoutMS) * time.Millisecond
	} else {
		cfg.SyncTimeout = time.Duration(workflowDefaultSyncTimeoutMs) * time.Millisecond
	}
	cfg.EngineTimeout = workflowDefaultEngineTimeout
	return cfg, nil
}

// ---------------------------------------------------------------------------
// workflowSpawner: the C1 seam the engine fans out through (exportable so
// branch 02 can reuse the engine against the real DelegateManager).
// ---------------------------------------------------------------------------

// workflowSpawner is the subset of the C1 DelegateManager the workflow engine
// depends on: spawn an async child and join it. *agent.DelegateManager
// satisfies it; tests inject a fake.
type workflowSpawner interface {
	Spawn(ctx context.Context, req agent.DelegateRequest, parentSessionID string) (agent.DelegateResult, error)
	Join(ctx context.Context, parentSessionID, childID string, timeout time.Duration) (agent.DelegateResult, error)
}

// ---------------------------------------------------------------------------
// Workflow engine
// ---------------------------------------------------------------------------

// WorkflowRun is the settled outcome of one engine run.
type WorkflowRun struct {
	RunID         string
	AgentsStarted int64
	StopReason    string // completed | cancelled | error
	StopMessage   string
	Value         string // JSON-serialized script return value (clamped)
}

// WorkflowEngine executes a confined workflow script against a C1 spawner.
// It is safe for a single foreground Run (one execution owns the codemode
// sandbox); RunBackground runs the engine in a detached goroutine.
type WorkflowEngine struct {
	mu            sync.Mutex
	mgr           workflowSpawner
	parentSession string
	cfg           WorkflowConfig
	runID         string
	sem           chan struct{}
	started       atomic.Int64
	completion    chan WorkflowRun
	deliver       func(WorkflowRun)
	// completedRun is the sticky settled run of a background execution
	// (set once by the deliver closure under mu; nil until settled).
	completedRun *WorkflowRun
	bus          workflowBus
}

// NewWorkflowEngine builds an engine. cfg defaults are applied; an out-of-range
// explicit value is rejected via cfgValid (callers surface it as isError).
func NewWorkflowEngine(cfg WorkflowConfig, mgr workflowSpawner) *WorkflowEngine {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = workflowDefaultMaxConcurrent()
	}
	defaultWorkflowConfig(&cfg)
	var sem chan struct{}
	if cfg.MaxConcurrent > 0 {
		sem = make(chan struct{}, cfg.MaxConcurrent)
	}
	return &WorkflowEngine{
		mgr:   mgr,
		cfg:   cfg,
		runID: id.Generate("wf-run-"),
		sem:   sem,
	}
}

// RunID returns the engine's run identifier.
func (e *WorkflowEngine) RunID() string { return e.runID }

// AgentsStarted reports how many agents the run has spawned so far.
func (e *WorkflowEngine) AgentsStarted() int64 { return e.started.Load() }

// Run executes the script and returns the settled run. meta is only
// validated for the tool surface; the engine revalidates the script host-side
// before any child spawn.
func (e *WorkflowEngine) Run(ctx context.Context, script string, args map[string]any) WorkflowRun {
	if err := validateScript(script); err != nil {
		return e.failedRun(workflowStopError, err.Error())
	}
	argsJSON := "{}"
	if args != nil {
		if b, err := json.Marshal(args); err == nil {
			argsJSON = string(b)
		}
	}
	full := buildWorkflowPrelude(e.cfg.MaxItemsPerCall, argsJSON) + "\n" + script

	sb, err := codemode.New(codemode.Options{
		Timeout:     e.cfg.EngineTimeout,
		MemoryLimit: e.cfg.MemoryLimit,
		Tools: []codemode.ToolBridge{{
			Name:        "agent",
			Description: "Spawn a delegated subagent, wait for its completed result (resolves its value), or null if the child failed; fatal errors reject the run.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt":   map[string]any{"type": "string"},
					"schema":   map[string]any{"type": "object"},
					"label":    map[string]any{"type": "string"},
					"phase":    map[string]any{"type": "string"},
					"agent_id": map[string]any{"type": "string"},
					"provider": map[string]any{"type": "string"},
					"model":    map[string]any{"type": "string"},
				},
			},
			Call: func(ctx context.Context, argsJSON string) (string, error) {
				return e.agentBridge(ctx, argsJSON), nil
			},
		}},
	})
	if err != nil {
		return e.failedRun(workflowStopError, "workflow: sandbox init failed: "+err.Error())
	}
	defer func() { _ = sb.Close() }()

	res, err := sb.Execute(ctx, full)
	if err != nil {
		// Host-side failure (dead sandbox / loop): a fatal error.
		return e.failedRun(workflowStopError, "workflow: "+err.Error())
	}
	if !res.OK {
		if ctx != nil && ctx.Err() != nil {
			return e.failedRun(workflowStopCancelled, "run was cancelled: "+ctx.Err().Error())
		}
		return e.failedRun(workflowStopError, res.Err)
	}
	return WorkflowRun{
		RunID:         e.runID,
		AgentsStarted: e.started.Load(),
		StopReason:    workflowStopCompleted,
		Value:         renderWorkflowResult(res.Value),
	}
}

// RunBackground starts the run in a detached goroutine and returns the job id
// immediately. The caller's context is NOT the run's lifetime: the run
// continues in the background and its children settle via the DelegateManager
// registry (interruptible by childID through the B3 child-control tools).
//
// DEVIATION (recorded per the leaf rule against a second job registry): the
// wf-job id is NOT a queue job — queue integration would need the daemon-side
// job store wired through the toolprov provider seam (a cross-package change
// out of scope for this pass). Instead the settled WorkflowRun is delivered on
// completion: it is stored on the engine (WorkflowCompletion, readable via
// WaitCompletion/Completion) and published on the EXISTING
// agent.delegate.result bus topic with kind "workflow" — the same
// publisher-adaptor lane the child settlements already ride, so no new
// registry, topic, or reaper is introduced.
func (e *WorkflowEngine) RunBackground(script string, args map[string]any) string {
	jobID := id.Generate("wf-job-")
	e.mu.Lock()
	if e.deliver == nil {
		ch := make(chan WorkflowRun, 1)
		e.completion = ch
		e.deliver = func(run WorkflowRun) {
			ch <- run
			// Keep the settled run on the engine (leaf 07): the channel is
			// single-consumption, but completion is a sticky read so a
			// later status surface (workflow_status) reports the settled
			// outcome idempotently instead of degrading to running.
			e.mu.Lock()
			e.completedRun = &run
			e.mu.Unlock()
		}
	}
	deliver := e.deliver
	e.mu.Unlock()
	go func() {
		runCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		run := e.Run(runCtx, script, args)
		// Publish BEFORE delivering on the channel: delivery is the final
		// signal, so a caller waking on WaitCompletion always sees the bus
		// event already published.
		e.publishCompletion(run, jobID)
		deliver(run)
	}()
	return jobID
}

// settledRun returns the settled background run (sticky once delivered),
// or ok=false until the run settles. Read by the workflow_status surface;
// unlike the single-consumption channel seam this never degrades.
func (e *WorkflowEngine) settledRun() (WorkflowRun, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.completedRun == nil {
		return WorkflowRun{}, false
	}
	return *e.completedRun, true
}

// workflowBus is the minimal publish surface RunBackground delivery rides.
// It matches the handoffBus adaptor pattern (agent_message.go / handoff.go):
// a tool-level interface so tests can inject a fake without a live bus.
type workflowBus interface {
	Publish(topic string, msg *models.BusMessage) int
}

// SetCompletionBus wires the bus the background run's completion event is
// published on (agent.delegate.result with kind "workflow"). Nil (the default,
// and the test wiring) skips the publish; the completion stays readable via
// WaitCompletion either way.
func (e *WorkflowEngine) SetCompletionBus(b workflowBus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bus = b
}

// Completion returns the settled run of a background RunBackground execution
// without blocking. ok is false until the run settles (and false forever for
// engines never run in the background).
func (e *WorkflowEngine) Completion() (WorkflowRun, bool) {
	e.mu.Lock()
	ch := e.completion
	e.mu.Unlock()
	if ch == nil {
		return WorkflowRun{}, false
	}
	select {
	case run := <-ch:
		return run, true
	default:
		return WorkflowRun{}, false
	}
}

// WaitCompletion blocks until the background run settles (or ctx is done) and
// returns the settled run. A foreground-only engine (no RunBackground call)
// never settles: WaitCompletion returns the ctx error wrapped.
func (e *WorkflowEngine) WaitCompletion(ctx context.Context) (WorkflowRun, error) {
	e.mu.Lock()
	ch := e.completion
	e.mu.Unlock()
	if ch == nil {
		return WorkflowRun{}, fmt.Errorf("workflow: no background run on engine %s", e.runID)
	}
	select {
	case run := <-ch:
		return run, nil
	case <-ctx.Done():
		return WorkflowRun{}, fmt.Errorf("workflow: wait for run %s: %w", e.runID, ctx.Err())
	}
}

// publishCompletion publishes the background run's settlement on the existing
// agent.delegate.result topic (kind "workflow"), via the daemon-wired bus
// adaptor. Best-effort: a nil bus or a marshal failure skips the event (the
// completion itself is still delivered via the channel seam).
func (e *WorkflowEngine) publishCompletion(run WorkflowRun, jobID string) {
	e.mu.Lock()
	b := e.bus
	parent := e.parentSession
	e.mu.Unlock()
	if b == nil {
		return
	}
	payload := map[string]any{
		"kind":              "workflow",
		"job_id":            jobID,
		"run_id":            run.RunID,
		"status":            run.StopReason,
		"result":            run.Value,
		"error":             run.StopMessage,
		"agents":            run.AgentsStarted,
		"parent_session_id": parent,
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "workflow-engine", payload)
	if err != nil {
		return
	}
	_ = b.Publish("agent.delegate.result", msg)
}

// failedRun builds a WorkflowRun terminal without a value.
func (e *WorkflowEngine) failedRun(reason, msg string) WorkflowRun {
	return WorkflowRun{
		RunID:         e.runID,
		AgentsStarted: e.started.Load(),
		StopReason:    reason,
		StopMessage:   msg,
	}
}

// agentOutcome is the settled JSON object the tools.agent bridge resolves to.
// Success -> {ok:true,value}; child failure -> {ok:false,fatal:false}; a
// fatal workflow error -> {ok:false,fatal:true,reason}. Fatals are resolved
// (not rejected) so the JS agent() shim can re-throw them as tagged
// WorkflowFatal errors that parallel/pipeline propagate instead of nulling.
type agentOutcome struct {
	OK     bool   `json:"ok"`
	Fatal  bool   `json:"fatal,omitempty"`
	Reason string `json:"reason,omitempty"`
	Value  any    `json:"value,omitempty"`
}

// agentBridge implements the tools.agent ToolBridge: Spawn a child via the C1
// seam and Join it, enforcing the concurrency and total-agent caps fail-close.
// It always settles a JSON outcome (never a host error), so misused hooks and
// cap trips surface as fatal script errors, not silently-narrower configs.
func (e *WorkflowEngine) agentBridge(ctx context.Context, argsJSON string) string {
	var a struct {
		AgentID  string          `json:"agent_id"`
		Prompt   string          `json:"prompt"`
		Schema   json.RawMessage `json:"schema"`
		Label    string          `json:"label"`
		Phase    string          `json:"phase"`
		Provider string          `json:"provider"`
		Model    string          `json:"model"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return e.agentOutcome(fatalOutcome("agent: invalid arguments: " + err.Error()))
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return e.agentOutcome(fatalOutcome("agent(prompt): a non-empty prompt is required (misused hook)"))
	}
	if len(a.Schema) > 0 && a.Schema[0] != '{' {
		return e.agentOutcome(fatalOutcome("agent: schema must be a JSON object (misused hook)"))
	}
	agentID := a.AgentID
	if agentID == "" {
		agentID = e.cfg.DefaultAgent
	}
	if agentID == "" {
		return e.agentOutcome(fatalOutcome("agent(prompt): no agent selected; pass agent_id or configure a workflow default (misused hook)"))
	}

	// Concurrency cap: bound the number of in-flight Spawn+Join waits.
	if e.sem != nil {
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			return e.agentOutcome(fatalOutcome("agent: run cancelled while waiting for a concurrency slot"))
		}
	}
	// Total-agent cap: fail closed (AGENT_CAP) once max_total_agents is hit.
	for {
		cur := e.started.Load()
		if cur >= int64(e.cfg.MaxTotal) {
			return e.agentOutcome(fatalOutcome(fmt.Sprintf("%s: max_total_agents (%d) reached; refusing to spawn more agents", workflowClassAgentCap, e.cfg.MaxTotal)))
		}
		if e.started.CompareAndSwap(cur, cur+1) {
			break
		}
	}

	req := agent.DelegateRequest{
		AgentID:  agentID,
		Message:  a.Prompt,
		Mode:     "spawn",
		Provider: a.Provider,
		Model:    a.Model,
	}
	res, err := e.mgr.Spawn(ctx, req, e.parentSession)
	if err != nil {
		return e.agentOutcome(fatalOutcome("agent: spawn failed: " + err.Error()))
	}
	joinCtx, cancel := context.WithTimeout(ctx, e.cfg.SyncTimeout)
	defer cancel()
	joined, err := e.mgr.Join(joinCtx, e.parentSession, res.ChildID, e.cfg.SyncTimeout)
	if err != nil {
		// A child that did not settle in the sync window (or a cancelled ctx)
		// resolves null — not a script abort.
		return e.agentOutcome(childFailOutcome())
	}
	if !joined.Success {
		return e.agentOutcome(childFailOutcome())
	}
	return e.agentOutcome(successOutcome(parseChildValue(joined.Response)))
}

// parseChildValue returns the JSON value of a child's response: parsed when it
// is valid JSON, otherwise the raw response string.
func parseChildValue(resp string) any {
	var out any
	if err := json.Unmarshal([]byte(resp), &out); err == nil {
		return out
	}
	return resp
}

func fatalOutcome(reason string) agentOutcome { return agentOutcome{Fatal: true, Reason: reason} }
func childFailOutcome() agentOutcome          { return agentOutcome{} }
func successOutcome(v any) agentOutcome       { return agentOutcome{OK: true, Value: v} }

// agentOutcome serializes an outcome to the JSON string the bridge resolves.
func (e *WorkflowEngine) agentOutcome(o agentOutcome) string {
	b, err := json.Marshal(o)
	if err != nil {
		return `{"ok":false,"fatal":true,"reason":"workflow: internal outcome marshal failure"}`
	}
	return string(b)
}

// IsCancelled reports a "cancelled" stop reason.
func (r WorkflowRun) IsCancelled() bool { return r.StopReason == workflowStopCancelled }

// ---------------------------------------------------------------------------
// JS prelude: confined composition/narration helpers spliced ahead of the
// script. Only tools.agent is a Go bridge; these are pure JS building blocks
// that fan out through it. No fs/network/timer/Node leaks through.
// ---------------------------------------------------------------------------

func buildWorkflowPrelude(maxItemsPerCall int, argsJSON string) string {
	return strings.TrimSpace(fmt.Sprintf(`
const __max_items_per_call = %d;
const __args = %s;
const args = __args;
function __wf_mkfatal(msg){ const e = new Error(msg); e.name = "WorkflowFatal"; return e; }
function __wf_isfatal(e){ return !!(e && e.name === "WorkflowFatal"); }
async function agent(prompt, opts){
  if (typeof prompt !== "string" || prompt.trim() === "") throw __wf_mkfatal("workflow: agent(prompt) requires a non-empty prompt string");
  const o = (opts && typeof opts === "object") ? opts : {};
  const a = { prompt: prompt };
  const keys = ["schema","label","phase","provider","model","agent_id","context"];
  for (const k of keys){ if (o[k] !== undefined) a[k] = o[k]; }
  let raw = await tools.agent(a);
  let r = raw;
  if (typeof raw === "string"){ try { r = JSON.parse(raw); } catch(e){ r = {ok:false, fatal:true, reason:"workflow: agent bridge returned unparseable output"}; } }
  if (r && typeof r === "object" && r.ok === false){
    if (r.fatal) throw __wf_mkfatal(r.reason !== undefined ? r.reason : "workflow: run failed");
    return null;
  }
  if (r && typeof r === "object" && Object.prototype.hasOwnProperty.call(r, "value")) return r.value;
  return r;
}
async function pipeline(items, ...stages){
  if (items === undefined || items === null) throw __wf_mkfatal("workflow: pipeline requires items");
  const arr = Array.isArray(items) ? items : [items];
  if (arr.length > __max_items_per_call) throw __wf_mkfatal("workflow: ITEM_CAP exceeded (max_items_per_call " + __max_items_per_call + ")");
  const out = [];
  for (let i = 0; i < arr.length; i++){
    let cur = arr[i]; let okFlag = true;
    for (const s of stages){
      if (typeof s !== "function") throw __wf_mkfatal("workflow: pipeline stages must be functions");
      try { cur = await s(cur, i); }
      catch(e){ if (__wf_isfatal(e)) throw e; okFlag = false; break; }
    }
    out.push(okFlag ? cur : null);
  }
  return out;
}
async function parallel(thunks){
  if (!Array.isArray(thunks)) throw __wf_mkfatal("workflow: parallel requires an array of thunks");
  if (thunks.length > __max_items_per_call) throw __wf_mkfatal("workflow: ITEM_CAP exceeded (max_items_per_call " + __max_items_per_call + ")");
  return await Promise.all(thunks.map(function(t){
    if (typeof t !== "function") throw __wf_mkfatal("workflow: parallel thunks must be functions");
    return Promise.resolve().then(t).catch(function(e){ if (__wf_isfatal(e)) throw e; return null; });
  }));
}
function phase(title){ text("phase: " + String(title)); }
function log(msg){ text("workflow: " + String(msg)); }
`, maxItemsPerCall, argsJSON))
}

// renderWorkflowResult clamps the rendered result to workflowRenderCap with a
// truncation notice (dsh renders foreground results capped at 50000 chars).
func renderWorkflowResult(s string) string {
	if len(s) <= workflowRenderCap {
		return s
	}
	return s[:workflowRenderCap] + fmt.Sprintf(" ...[result truncated: %d of %d chars]", workflowRenderCap, len(s))
}

// ---------------------------------------------------------------------------
// The model-facing workflow tool.
// ---------------------------------------------------------------------------

// WorkflowTool executes a confined delegation workflow.
type WorkflowTool struct {
	tools.ToolDefaults
	ctrl *DelegateController
}

// NewWorkflowTool constructs the workflow tool bound to the controller whose
// C1 DelegateManager backs the agent() fan-out.
func NewWorkflowTool(c *DelegateController) *WorkflowTool {
	return &WorkflowTool{ctrl: c}
}

func (t *WorkflowTool) Name() string { return "workflow" }

func (t *WorkflowTool) Category() string { return "delegation" }

func (t *WorkflowTool) Description() string {
	return "Execute a confined JavaScript delegation program. The script runs in an isolated sandbox " +
		"with NO filesystem, network, timer or Node.js APIs — the agents do the work (via the agent() helper). " +
		"Available body hooks: agent(prompt,{schema,label,phase,provider,model,agent_id}) (spawn a subagent, " +
		"await its value, or null on child failure), pipeline(items,...stages), parallel(thunks), phase(title), " +
		"log(msg), and args. The script may be async and should `return <value>` (a JSON value). " +
		"Caps FAIL-CLOSE: max_total_agents, max_items_per_call, max_concurrent_agents (explicit over-ceiling is " +
		"INVALID_ARGUMENT, never clamped; a cap trip or misused hook ends the run as a fatal error). " +
		"run_in_background returns a job id immediately; otherwise the run blocks for the JSON result."
}

func (t *WorkflowTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"script": {
				Type:        schemaTypeString,
				Description: "The confined JavaScript program to run (async, returns a JSON value). Uses agent()/pipeline()/parallel()/phase()/log()/args.",
			},
			"meta": {
				Type:        schemaTypeObject,
				Description: "Optional workflow metadata: {name (kebab-case, required), description (required), when_to_use?, phases?: [{title, detail?, provider?, model?}]}.",
			},
			"agent_id": {
				Type:        schemaTypeString,
				Description: "Default delegated agent_id for agent() calls that pass no agent_id.",
			},
			"max_concurrent_agents": {
				Type:        schemaTypeInteger,
				Description: "Max concurrently in-flight agents (default min(16, cores-2)); over the engine ceiling is INVALID_ARGUMENT.",
			},
			"max_total_agents": {
				Type:        schemaTypeInteger,
				Description: "Max total agents a run may spawn (default 1000); a trip is AGENT_CAP, over-ceiling INVALID_ARGUMENT.",
			},
			"max_items_per_call": {
				Type:        schemaTypeInteger,
				Description: "Max items/thunks per pipeline/parallel call (default 4096); a trip is ITEM_CAP.",
			},
			"sync_timeout_ms": {
				Type:        schemaTypeInteger,
				Description: "Per-agent() join timeout in ms (default 5000).",
			},
			"run_in_background": {
				Type:        schemaTypeBoolean,
				Description: "When true, return {kind:\"background\", jobId, runId} immediately; the run continues in the background.",
			},
		},
		Required: []string{"script"},
	}
}

func (t *WorkflowTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.ctrl == nil {
		return tools.NewErrorResult("workflow: controller not bound"), nil
	}
	var p workflowParams
	if b, err := json.Marshal(args); err != nil {
		return tools.NewErrorResult("workflow: INVALID_ARGUMENT: could not read arguments"), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	} else if err := json.Unmarshal(b, &p); err != nil {
		return tools.NewErrorResult("workflow: INVALID_ARGUMENT: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}

	// Host-side validation before any child: script + meta. Fail-close.
	if err := validateScript(p.Script); err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}
	if err := validateMeta(p.Meta); err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}
	cfg, err := validateConfig(p, p.AgentID)
	if err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}
	if t.ctrl.mgr == nil {
		return tools.NewErrorResult("workflow: delegate manager not available"), nil
	}
	parent := t.ctrl.resolveCallerSession(ctx)
	engine := NewWorkflowEngine(cfg, t.ctrl.mgr)
	engine.parentSession = parent

	if p.RunInBackground {
		engine.SetCompletionBus(t.ctrl.completionBus)
		jobID := engine.RunBackground(p.Script, args)
		// Leaf 07-workflow-status: keep the job answerable to the model via
		// workflow_status(job_id) — bounded per-controller registry.
		t.ctrl.registerWorkflowJob(jobID, engine)
		return map[string]any{
			"kind":  "background",
			"jobId": jobID,
			"runId": engine.RunID(),
		}, nil
	}

	run := engine.Run(ctx, p.Script, args)
	switch run.StopReason {
	case workflowStopCompleted:
		return map[string]any{
			"kind":          "foreground",
			"runId":         run.RunID,
			"agentsStarted": run.AgentsStarted,
			"result":        run.Value,
		}, nil
	case workflowStopCancelled:
		return tools.NewErrorResult("workflow: run was cancelled (" + run.RunID + "): " + run.StopMessage), nil
	default:
		return tools.NewErrorResult("workflow: run failed (" + run.RunID + "): " + run.StopMessage), nil
	}
}

// Compile-time interface assertion.
var _ tools.Tool = (*WorkflowTool)(nil)

// ---------------------------------------------------------------------------
// workflow_status: the model-facing background-job status surface
// (dsh-convergence leaf docs/plans/20261102-dsh-convergence/04-tool-surface/
// 07-workflow-status.md). DSH-8 made a background run deliver its settled
// WorkflowRun; this leaf makes the job id ANSWERABLE to the model that
// received it: query by wf-job id, get running / completed / failed plus the
// settled run summary.
// ---------------------------------------------------------------------------

// workflowJobRegistryCap bounds the per-controller wf-job registry (FIFO
// eviction of the oldest job). The engine caps a single run at
// max_total_agents = 100000 spawns, so a registry of 256 jobs keeps the
// controller's status surface O(1)-bounded without ever evicting a live
// model-recent job in practice.
const workflowJobRegistryCap = 256

// Status vocabulary returned in the status field (the run's stop reasons
// completed|cancelled|error map to the settled classes; unsettled is
// running).
const (
	workflowStatusRunning   = "running"
	workflowStatusCompleted = "completed"
	workflowStatusFailed    = "failed"
	workflowStatusCancelled = "cancelled"
)

// jobStatusView is the sanitized status projection workflow_status returns —
// never a live engine reference or the settled channel seam.
type jobStatusView struct {
	JobID         string `json:"job_id"`
	RunID         string `json:"run_id,omitempty"`
	Status        string `json:"status"`
	StopReason    string `json:"stop_reason,omitempty"`
	Error         string `json:"error,omitempty"`
	Result        string `json:"result,omitempty"`
	AgentsStarted int64  `json:"agents_started,omitempty"`
}

// registerWorkflowJob records a background job's engine so a later
// workflow_status(job_id) can read it. Bounded: registering over the cap
// evicts the OLDEST job (a model-recent id stays answerable; the evicted
// status becomes the clean unknown-job error).
func (c *DelegateController) registerWorkflowJob(jobID string, e *WorkflowEngine) {
	if c == nil || jobID == "" || e == nil {
		return
	}
	c.jobMu.Lock()
	defer c.jobMu.Unlock()
	if c.jobReg == nil {
		c.jobReg = make(map[string]*WorkflowEngine)
	}
	if _, exists := c.jobReg[jobID]; !exists {
		c.jobOrder = append(c.jobOrder, jobID)
	}
	c.jobReg[jobID] = e
	for len(c.jobOrder) > workflowJobRegistryCap {
		oldest := c.jobOrder[0]
		c.jobOrder = c.jobOrder[1:]
		delete(c.jobReg, oldest)
		delete(c.settledCache, oldest)
	}
}

// cacheSettled stores a job's first-observed settlement and returns its
// view. Idempotent: the first settlement wins for the job's lifetime —
// a second call (e.g. after another reader drained the engine's channel)
// keeps the cached view rather than degrading to running.
func (c *DelegateController) cacheSettled(jobID string, run WorkflowRun) jobStatusView {
	c.jobMu.Lock()
	defer c.jobMu.Unlock()
	if v, ok := c.settledCache[jobID]; ok {
		return v
	}
	view := jobStatusView{
		JobID:         jobID,
		RunID:         run.RunID,
		StopReason:    run.StopReason,
		Error:         run.StopMessage,
		Result:        run.Value,
		AgentsStarted: run.AgentsStarted,
	}
	switch run.StopReason {
	case workflowStopCompleted:
		view.Status = workflowStatusCompleted
	case workflowStopCancelled:
		view.Status = workflowStatusCancelled
	default:
		view.Status = workflowStatusFailed
	}
	if c.settledCache == nil {
		c.settledCache = make(map[string]jobStatusView)
	}
	c.settledCache[jobID] = view
	return view
}

// workflowJobStatus reports a job's status WITHOUT blocking. The engine's
// Completion seam is single-consumption, so the first observed settlement is
// cached on the controller: repeated queries keep reporting the settled
// outcome (idempotent), never flipping a completed job back to running.
// The settled cache is the concurrency seam: WaitCompletion's delivery is
// consumed by whoever wakes on it, but the controller's cache is populated
// exactly once (settledCache), so every reader sees the same final view.
func (c *DelegateController) workflowJobStatus(jobID string) (jobStatusView, bool) {
	if c == nil {
		return jobStatusView{}, false
	}
	c.jobMu.Lock()
	e, ok := c.jobReg[jobID]
	c.jobMu.Unlock()
	if !ok || e == nil {
		return jobStatusView{}, false
	}
	// A previously-observed settlement wins (idempotent across readers).
	c.jobMu.Lock()
	cached, haveCached := c.settledCache[jobID]
	c.jobMu.Unlock()
	if haveCached {
		return cached, true
	}
	view := jobStatusView{JobID: jobID, Status: workflowStatusRunning, RunID: e.RunID()}
	run, settled := e.settledRun()
	if !settled {
		return view, true
	}
	return c.cacheSettled(jobID, run), true
}

// WorkflowStatus reports a background workflow job's status to the model.
// An unknown job id is a clean error envelope (never a panic, never a hang);
// the id namespace is the wf-job-* ids RunBackground returned.
func (c *DelegateController) WorkflowStatus(_ context.Context, jobID string) (any, error) {
	if strings.TrimSpace(jobID) == "" {
		return tools.NewErrorResult("workflow_status: job_id is required"), nil
	}
	view, ok := c.workflowJobStatus(jobID)
	if !ok {
		return tools.NewErrorResult(
			"workflow_status: unknown job_id: "+jobID+" (background job ids come from the workflow tool's run_in_background result)"), nil
	}
	return view, nil
}

// WorkflowStatusTool exposes background workflow job status to the model.
type WorkflowStatusTool struct {
	tools.ToolDefaults
	ctrl *DelegateController
}

// NewWorkflowStatusTool constructs the workflow_status tool bound to the
// controller that owns the wf-job registry.
func NewWorkflowStatusTool(c *DelegateController) *WorkflowStatusTool {
	return &WorkflowStatusTool{ctrl: c}
}

func (t *WorkflowStatusTool) Name() string     { return "workflow_status" }
func (t *WorkflowStatusTool) Category() string { return "delegation" }

func (t *WorkflowStatusTool) Description() string {
	return "Report the status of a background workflow run started with the workflow tool " +
		"(run_in_background:true). Returns running until the run settles, then the settled " +
		"summary: stop reason (completed/cancelled/error), agents started, result value, or " +
		"the error. Idempotent — a settled job keeps reporting its outcome. Never blocks."
}

func (t *WorkflowStatusTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"job_id": {
				Type:        schemaTypeString,
				Description: "The jobId returned by a workflow call with run_in_background:true.",
			},
		},
		Required: []string{"job_id"},
	}
}

func (t *WorkflowStatusTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.ctrl == nil {
		return tools.NewErrorResult("workflow_status: controller not bound"), nil
	}
	return t.ctrl.WorkflowStatus(ctx, asString(args["job_id"]))
}

// Compile-time interface assertion.
var _ tools.Tool = (*WorkflowStatusTool)(nil)
