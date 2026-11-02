// Package hitl is the HITL regime evaluation harness
// (docs/plans/20261031-hitl-notifications/04-regime-evaluation.md, contract
// C7): it runs the SAME hermetic scripted scenario under meept's three
// autonomy regimes and produces a HitlRegimeReport comparing them on
// objective metrics computed ONLY from ground truth.
//
// Regimes (C7 values):
//   - per_action          [acp] permission_mode "human": every gated action
//     relays through the human permission registry; a scripted human answers
//     deterministically over the RPC surface.
//   - plan_gate_lead      plans.mode "always" + plans.approval
//     .require_approval: the turn parks at pending_approval and the scripted
//     human approves via plan.approve (one interrupt up front).
//   - autonomous_sampled  no relay, no plan gate; post-hoc the harness
//     samples N% of the recorded actions (seeded, deterministic) and
//     simulates spot-check verdicts against the audit trail.
//
// Ground truth (C7 pins ONLY these three sources; the metrics layer in
// metrics.go reads nothing else):
//  1. the auditlog hash chain (internal/auditlog) - tool.execution records
//     carry tool, args_hash and outcome for every gated execution;
//  2. the permission table RuleSource column (the security engine's
//     decision_log; values include base_rule, command_pattern, path_rule,
//     override, immutable, confirmation_gate, fail_closed,
//     shell_permission_table, employee_pre_exec, fence) - the risk-class
//     taxonomy for escape rates;
//  3. acp.permission.decision bus events - the harness records the
//     requested/resolved transitions through the pending registry RPC
//     (acp.permissions.list / acp.permissions.resolve), whose values are the
//     exact decision/reason/approver set the bus notifier publishes on
//     acp.permission.resolved (internal/daemon/acp_notifier.go); leaf 05's
//     e2e suite joins the WS frames against this timeline.
//
// Escape-rate oracle (documented choice): the security engine's deny
// decisions are the would-have-blocked oracle. A recorded autonomous action
// is replayed through a DEDICATED security engine instance (same seed
// rules); a hard deny (Allowed=false, RequiresConfirmation=false) means the
// spot-check would have blocked it (basis oracle_deny). A confirmation
// escalation (RequiresConfirmation=true) is NOT an escape - it is exactly
// the per-action regime's intervention surface. When the per-action
// reference run's scripted human DENIED the step, the R0 spot-check rule
// wins (basis reference_deny): any step the human-in-the-loop run denies
// that the autonomous run executed is an escape. Replay runs against its
// own engine instance, so oracle decisions append to that instance's
// decision_log only, never the daemon's.
//
// Boot choice (leaf T1): the runner reuses the existing e2e/harness scratch
// daemon builder. It is importable (no build tag on its non-test files) and
// cycle-free from here (e2e/harness imports internal/eval, the PARENT
// package - hitl is a sibling - so internal/eval/hitl -> e2e/harness is not
// an import cycle). The scripted-human RPC client, by contrast, is
// deliberately REIMPLEMENTED in this package (leaf consume note: "not
// imported from e2e; reimplement the small client in internal/eval/hitl").
//
// Metric provenance caveats (research-notes.md C.2/C.3, carried in the
// rendered report): intervention_precision and escape_rate are meept-defined
// syntheses - no canonical literature term pins either name; the nearest
// analogues are HiL-Bench question precision / FabricLoop outcome-change and
// blocker recall + Intervention Rate inversion respectively.
//
// The package never touches the chat path: it observes and drives existing
// surfaces only (AGENTS.md e2e policy; leaf Notes).
package hitl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/auditlog"
)

// Regime is one autonomy regime under evaluation. Values are frozen by C7's
// regime field ("per_action" | "plan_gate_lead" | "autonomous_sampled").
type Regime string

// The three regimes (C7).
const (
	RegimePerAction         Regime = "per_action"
	RegimePlanGateLead      Regime = "plan_gate_lead"
	RegimeAutonomousSampled Regime = "autonomous_sampled"
)

// AllRegimes is the canonical report ordering (deterministic; map iteration
// would scramble the markdown table).
var AllRegimes = []Regime{RegimePerAction, RegimePlanGateLead, RegimeAutonomousSampled}

// Valid reports whether r is one of the three pinned regimes.
func (r Regime) Valid() bool {
	switch r {
	case RegimePerAction, RegimePlanGateLead, RegimeAutonomousSampled:
		return true
	}
	return false
}

// String implements fmt.Stringer (the value IS the C7 string).
func (r Regime) String() string { return string(r) }

// RegimeOverlay returns the hermetic daemon config overlay that arms the
// regime's interrupt surface. The mapping is deliberately thin:
//   - per_action: [acp] permission_mode "human" with the scenario's
//     fail-closed relay budget (internal/acp/relay.go stays the only
//     timeout authority), websocket on so the bus frames stay observable,
//     and require_confirmation_high off (the relay itself is the
//     confirmation layer; same posture as the acp-wire suite);
//   - plan_gate_lead: plans.mode "always" + require_approval, so the
//     dispatch path parks the turn at pending_approval (the interrupt);
//   - autonomous_sampled: explicitly no relay, no plan gate.
func RegimeOverlay(sc Scenario, regime Regime) map[string]any {
	sc = sc.WithDefaults()
	switch regime {
	case RegimePerAction:
		return map[string]any{
			"transport.http.websocket":           true,
			"security.require_confirmation_high": false,
			"acp.enabled":                        true,
			"acp.permission_mode":                "human",
			"acp.relay_timeout":                  sc.RelayTimeoutSeconds,
			"acp.dial_timeout":                   30,
			"acp.call_timeout":                   20,
			"acp.max_agents":                     2,
		}
	case RegimePlanGateLead:
		return map[string]any{
			"plans.mode":                      "always",
			"plans.approval.require_approval": true,
		}
	case RegimeAutonomousSampled:
		return map[string]any{
			"plans.approval.require_approval": false,
		}
	}
	return nil
}

// Scenario is the identical scripted scenario every regime executes.
type Scenario struct {
	// ID labels the scenario in the report (C7 scenario_id).
	ID string
	// ChatMessage is the identical chat turn submitted in every regime.
	ChatMessage string
	// ProjectName / SessionName are sandbox labels (derived when empty).
	ProjectName string
	SessionName string
	// RelayTimeoutSeconds is the per-action regime's fail-closed relay
	// budget ([acp] relay_timeout; default 5 - fast fail-closed keeps the
	// hermetic timeout leg cheap).
	RelayTimeoutSeconds int
	// AgentBin is the fakeagent binary path for the per-action catalog
	// ("" = caller composes the catalog itself). AgentID is the catalog
	// entry id (default "perm-agent").
	AgentBin string
	AgentID  string
	// Actions declares the scripted actions the oracle replays (escape
	// classification). Key joins chain records to declarations.
	Actions []ScenarioAction
	// SampleRate / SampleSeed drive the autonomous regime's deterministic
	// post-hoc spot-check (defaults 0.2 / 1).
	SampleRate float64
	SampleSeed uint64
}

// ScenarioAction declares one scripted action for the escape oracle.
type ScenarioAction struct {
	// Key is the stable join key (scripted humans match it via the
	// request summary; chain records are joined by AssignActionKeys).
	Key string
	// Action / Tool / Details replay through the security engine
	// (engine.Check(action, tool, details, "")).
	Action  string
	Tool    string
	Details map[string]string
}

// WithDefaults fills derived labels and sampling defaults. Deterministic:
// the same Scenario always yields the same defaults.
func (s Scenario) WithDefaults() Scenario {
	if s.ID == "" {
		s.ID = "scenario"
	}
	if s.ProjectName == "" {
		s.ProjectName = "hitl-project"
	}
	if s.SessionName == "" {
		s.SessionName = "hitl-" + s.ID
	}
	if s.RelayTimeoutSeconds <= 0 {
		s.RelayTimeoutSeconds = 5
	}
	if s.AgentID == "" {
		s.AgentID = "perm-agent"
	}
	if s.SampleRate <= 0 {
		s.SampleRate = 0.2
	}
	if s.SampleSeed == 0 {
		s.SampleSeed = 1
	}
	return s
}

// ---------------------------------------------------------------------------
// Event timeline
// ---------------------------------------------------------------------------

// EventKind classifies one recorded timeline observation.
type EventKind string

// Timeline event kinds. permission_decision / plan_gate are the interrupt
// and intervention surfaces; terminal closes an attempted turn.
const (
	EventPermissionRequest  EventKind = "permission_request"
	EventPermissionDecision EventKind = "permission_decision"
	EventPlanGate           EventKind = "plan_gate"
	EventToolAction         EventKind = "tool_action"
	EventTerminal           EventKind = "terminal"
)

// TimelineEvent is one timestamped observation in a run's event stream.
// Permission events mirror the acp.permission.* payload values (ground
// truth 3); tool actions mirror the audit chain records (ground truth 1)
// with RuleSource joined from the decision_log (ground truth 2).
type TimelineEvent struct {
	At         time.Time `json:"at"`
	Kind       EventKind `json:"kind"`
	RequestID  string    `json:"request_id,omitempty"`
	Decision   string    `json:"decision,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Approver   string    `json:"approver,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	StepID     string    `json:"step_id,omitempty"`
	TaskID     string    `json:"task_id,omitempty"`
	Status     string    `json:"status,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	ArgsHash   string    `json:"args_hash,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
	RuleSource string    `json:"rule_source,omitempty"`
	Seq        uint64    `json:"seq,omitempty"`
}

// ActionRecord is one autonomous action observed in the audit chain
// (ground truth 1). Key/RiskClass/Basis are filled by the scenario join and
// the escape oracle.
type ActionRecord struct {
	Seq       uint64    `json:"seq"`
	At        time.Time `json:"at"`
	Tool      string    `json:"tool"`
	Category  string    `json:"category,omitempty"`
	ArgsHash  string    `json:"args_hash,omitempty"`
	Outcome   string    `json:"outcome"`
	Key       string    `json:"key,omitempty"`
	RiskClass string    `json:"risk_class,omitempty"`
	Basis     string    `json:"basis,omitempty"`
}

// RunResult is one scenario execution under one regime: the recorded event
// timeline plus the chain-collected action stream. SampledIdx marks the
// seeded spot-check sample; marking NEVER rewrites Events or Actions.
type RunResult struct {
	ScenarioID string          `json:"scenario_id"`
	Regime     string          `json:"regime"`
	StartedAt  time.Time       `json:"started_at"`
	Attempts   int             `json:"attempts"`
	Events     []TimelineEvent `json:"events"`
	Actions    []ActionRecord  `json:"actions,omitempty"`
	SampledIdx []int           `json:"sampled_idx,omitempty"`
}

// MarkSampled records the seeded sample indices. The action stream is left
// byte-identical (T2: the metric stream is marked, history is never
// rewritten).
func (r *RunResult) MarkSampled(idx []int) {
	r.SampledIdx = append([]int(nil), idx...)
}

// CollectChainActions reads the audit chain (ground truth 1) and extracts
// the tool.execution records as ActionRecords in ascending seq order.
func CollectChainActions(ctx context.Context, store *auditlog.Store) ([]ActionRecord, error) {
	if store == nil {
		return nil, fmt.Errorf("hitl: nil audit chain store")
	}
	recs, err := store.List(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("hitl: list audit chain: %w", err)
	}
	out := make([]ActionRecord, 0, len(recs))
	for _, rec := range recs {
		if rec.Type != "tool.execution" {
			continue
		}
		ar := ActionRecord{Seq: rec.Seq, At: rec.At}
		ar.Tool, _ = rec.Payload["tool"].(string)
		ar.Category, _ = rec.Payload["category"].(string)
		ar.ArgsHash, _ = rec.Payload["args_hash"].(string)
		ar.Outcome, _ = rec.Payload["outcome"].(string)
		if ar.Outcome == "" {
			ar.Outcome = "unknown"
		}
		out = append(out, ar)
	}
	return out, nil
}

// AssignActionKeys joins chain-collected actions to the scenario's declared
// actions: exact key match first, then tool-name match in declaration order
// (a single-turn hermetic scenario executes its actions in declaration
// order). Unmatched actions keep Key "". Joins in place; the action stream
// is marked, never reordered.
func AssignActionKeys(acts []ActionRecord, declared []ScenarioAction) {
	used := map[int]bool{}
	for i := range acts {
		for j := range declared {
			if declared[j].Key != "" && declared[j].Key == acts[i].Key && !used[j] {
				used[j] = true
				goto next
			}
		}
		for j := range declared {
			if declared[j].Tool == acts[i].Tool && !used[j] {
				acts[i].Key = declared[j].Key
				used[j] = true
				goto next
			}
		}
	next:
	}
}

// ---------------------------------------------------------------------------
// Scripted human (step-pipeline precedent, reimplemented per the leaf)
// ---------------------------------------------------------------------------

// PendingPermission is one answerable human permission request (the
// acp.permissions.list row shape).
type PendingPermission struct {
	ID        string
	AgentID   string
	SessionID string
	Summary   string
	CreatedAt time.Time
}

// PermissionSource supplies pending permission requests and resolves them.
// The production implementation rides acp.permissions.list /
// acp.permissions.resolve over RPCPermissionSource.
type PermissionSource interface {
	Pending() ([]PendingPermission, error)
	Resolve(id, decision string) error
}

// PlanGateSource supplies pending plan approvals and answers them
// (plan.list_by_session filtered to pending_approval; plan.approve /
// plan.reject). decision is the human verb: "allow" approves, "deny"
// rejects ("always" approves, mirroring the relay verb set).
type PlanGateSource interface {
	PendingPlan(sessionID string) (planID, title string, ok bool, err error)
	DecidePlan(planID, sessionID, decision string) error
}

// HumanBehavior is the scripted human: a deterministic verdict function
// over one pending request. Returns the decision verb ("allow" | "deny" |
// "always") and the matched scenario step id ("" when no rule matched).
type HumanBehavior interface {
	Decide(p PendingPermission) (decision, stepID string)
}

// ScriptedHuman answers by summary-substring rules: the LONGEST matching
// rule key wins (deterministic; scenarios declare unambiguous substrings).
// Unmatched requests get fallback. This is the step-pipeline
// scripted-human pattern lifted into the harness: the small RPC client is
// reimplemented in this package (leaf consume note), never imported from
// e2e.
type ScriptedHuman struct {
	rules    map[string]string
	order    []string // rule keys, longest first
	fallback string
}

// NewScriptedHuman builds a deterministic scripted human. rules maps a
// summary substring to the verdict verb; fallback answers everything else.
func NewScriptedHuman(rules map[string]string, fallback string) *ScriptedHuman {
	h := &ScriptedHuman{rules: make(map[string]string, len(rules)), fallback: fallback}
	for k, v := range rules {
		h.rules[k] = v
		h.order = append(h.order, k)
	}
	sort.Slice(h.order, func(i, j int) bool { return len(h.order[i]) > len(h.order[j]) })
	return h
}

// Decide implements HumanBehavior.
func (h *ScriptedHuman) Decide(p PendingPermission) (decision, stepID string) {
	for _, k := range h.order {
		if strings.Contains(p.Summary, k) {
			return h.rules[k], k
		}
	}
	return h.fallback, ""
}

// ---------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------

// TerminalStatus is one terminal poll observation.
type TerminalStatus struct {
	Done      bool
	Completed bool
	TaskID    string
}

// RunConfig wires one Drive execution to its observation surfaces. All
// sources are seams so unit tests drive the full loop with fakes and the
// e2e tier (leaf 05) wires the real RPC-backed implementations over the
// scratch daemon.
type RunConfig struct {
	Scenario Scenario
	Regime   Regime
	// SessionID is the chat session the scenario runs in (plan gates are
	// discovered per session).
	SessionID string
	// Perms is nil for regimes without the relay (plan_gate_lead,
	// autonomous_sampled).
	Perms PermissionSource
	// Gates is nil except for plan_gate_lead.
	Gates PlanGateSource
	// Human is the scripted verdict function (required when Perms or
	// Gates is set).
	Human HumanBehavior
	// Terminal polls the turn's terminal state; Done=true closes the run.
	Terminal func() TerminalStatus
	// Clock is the timestamp seam (tests pin it; default time.Now UTC).
	Clock func() time.Time
}

// Runner drives scenarios and records ground-truth timelines.
type Runner struct {
	// PollInterval is the pending/terminal poll cadence (default 100ms;
	// tests set 1ms).
	PollInterval time.Duration
	// DriveTimeout bounds one Drive execution (default 120s).
	DriveTimeout time.Duration
}

// Drive executes the scenario under the configured regime and records the
// event timeline. Every pending permission request is recorded (request +
// decision with timestamps), every plan gate is recorded once with its
// scripted verdict, and the terminal event closes the attempted turn. A
// DriveTimeout expiry ends the run WITHOUT a terminal event: the attempt
// stays attempted-but-never-completed and the completion rate reads the
// gap honestly (no fabricated terminal).
func (r *Runner) Drive(cfg RunConfig) *RunResult {
	sc := cfg.Scenario.WithDefaults()
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	poll := r.PollInterval
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	budget := r.DriveTimeout
	if budget <= 0 {
		budget = 120 * time.Second
	}

	res := &RunResult{
		ScenarioID: sc.ID,
		Regime:     cfg.Regime.String(),
		StartedAt:  clock(),
		Attempts:   1,
	}
	deadline := clock().Add(budget)
	seenReq := map[string]bool{}
	seenPlan := map[string]bool{}

	for {
		if clock().After(deadline) {
			return res
		}
		if cfg.Perms != nil {
			r.pollPermissions(cfg, res, seenReq, clock)
		}
		if cfg.Gates != nil && cfg.Human != nil {
			r.pollPlanGates(cfg, res, seenPlan, clock)
		}
		if cfg.Terminal != nil {
			if st := cfg.Terminal(); st.Done {
				status := "failed"
				if st.Completed {
					status = "completed"
				}
				res.Events = append(res.Events, TimelineEvent{
					At:     clock(),
					Kind:   EventTerminal,
					Status: status,
					TaskID: st.TaskID,
				})
				return res
			}
		}
		time.Sleep(poll)
	}
}

// pollPermissions drains newly seen pending requests: record the request,
// take the scripted verdict, resolve, record the decision. Resolve
// failures record reason "resolve_error" (the stream stays honest; the
// next poll retries the list). Pending list errors are transient and
// retried on the next tick.
func (r *Runner) pollPermissions(cfg RunConfig, res *RunResult, seen map[string]bool, clock func() time.Time) {
	pending, err := cfg.Perms.Pending()
	if err != nil {
		return
	}
	for _, p := range pending {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		res.Events = append(res.Events, TimelineEvent{
			At:        clock(),
			Kind:      EventPermissionRequest,
			RequestID: p.ID,
			Summary:   p.Summary,
		})
		decision, step := "", ""
		if cfg.Human != nil {
			decision, step = cfg.Human.Decide(p)
		}
		if decision == "" {
			decision = "deny" // an unscripted request fails closed
		}
		reason := "answered"
		if err := cfg.Perms.Resolve(p.ID, decision); err != nil {
			reason = "resolve_error"
		}
		res.Events = append(res.Events, TimelineEvent{
			At:        clock(),
			Kind:      EventPermissionDecision,
			RequestID: p.ID,
			Decision:  decision,
			Reason:    reason,
			StepID:    step,
			// Approver stays empty here: the scripted client's resolve
			// echo carries no approver; the daemon-side attribution
			// arrives on the resolved bus frames leaf 05 joins.
		})
	}
}

// pollPlanGates answers newly seen pending_approval plans: the decision
// event is recorded once per plan with the scripted verdict (the pause
// itself is derivable from StartedAt). "allow"/"always" approve, "deny"
// rejects; an unscripted gate fails closed (rejected).
func (r *Runner) pollPlanGates(cfg RunConfig, res *RunResult, seen map[string]bool, clock func() time.Time) {
	planID, title, ok, err := cfg.Gates.PendingPlan(cfg.SessionID)
	if err != nil || !ok || seen[planID] {
		return
	}
	seen[planID] = true
	p := PendingPermission{ID: planID, Summary: title}
	decision, step := "deny", ""
	if cfg.Human != nil {
		decision, step = cfg.Human.Decide(p)
	}
	reason := "answered"
	if err := cfg.Gates.DecidePlan(planID, cfg.SessionID, decision); err != nil {
		reason = "resolve_error"
	}
	res.Events = append(res.Events, TimelineEvent{
		At:        clock(),
		Kind:      EventPlanGate,
		RequestID: planID,
		Decision:  decision,
		Reason:    reason,
		StepID:    step,
		Summary:   title,
	})
}

// ---------------------------------------------------------------------------
// Boot composition (reuses the e2e/harness builder - see package doc)
// ---------------------------------------------------------------------------

// BootScenarioStack boots the hermetic scratch daemon for one regime run:
// the regime's config overlay plus, for per_action scenarios with a
// fakeagent binary, the ACP catalog seed + chat-roster grant (composition
// lifted from the acp-wire suite's stack builder; hermetic - everything
// stays in the sandbox MEEPT_HOME). Scenarios needing extra hooks call
// harness.Start themselves with RegimeOverlay.
func BootScenarioStack(t testing.TB, sc Scenario, regime Regime) *harness.Stack {
	t.Helper()
	if !regime.Valid() {
		t.Fatalf("hitl: invalid regime %q", string(regime))
	}
	sc = sc.WithDefaults()
	opts := []harness.StartOption{harness.WithConfigOverlay(RegimeOverlay(sc, regime))}
	if regime == RegimePerAction && sc.AgentBin != "" {
		opts = append(opts, harness.WithPreBootHook(func(s *harness.Stack) error {
			return seedACPCatalog(s, sc)
		}))
	}
	return harness.Start(t, opts...)
}

// seedACPCatalog writes the fakeagent catalog and grants acp_agent to the
// sandbox chat roster (acp-wire pattern; the harness seeds config/agents
// into MEEPT_HOME BEFORE pre-boot hooks run, so the copy is patched, never
// the repo file).
func seedACPCatalog(s *harness.Stack, sc Scenario) error {
	catalog := `{"agents":[{"id":"` + sc.AgentID + `","command":["` + sc.AgentBin +
		`","-mode","permission"],"enabled":true}]}`
	if err := os.WriteFile(filepath.Join(s.MeeptHome, "acp_agents.json5"), []byte(catalog), 0o600); err != nil {
		return fmt.Errorf("hitl: write acp catalog: %w", err)
	}
	chatSpec := filepath.Join(s.MeeptHome, "agents", "chat", "AGENT.md")
	data, err := os.ReadFile(chatSpec)
	if err != nil {
		return fmt.Errorf("hitl: read chat roster: %w", err)
	}
	patched := strings.Replace(string(data),
		"additional_tools:\n", "additional_tools:\n  - acp_agent\n", 1)
	if patched == string(data) {
		return fmt.Errorf("hitl: chat AGENT.md has no additional_tools block to extend")
	}
	return os.WriteFile(chatSpec, []byte(patched), 0o644)
}

// ---------------------------------------------------------------------------
// Seeded spot-check (autonomous regime, T2)
// ---------------------------------------------------------------------------

// SpotCheckVerdict is one action's simulated spot-check outcome. WouldBlock
// is honored ONLY for sampled actions; unsampled actions carry class
// evidence with basis "not_sampled" (the sample bounds the spot-check
// procedure, not the classification).
type SpotCheckVerdict struct {
	Index      int          `json:"index"`
	Action     ActionRecord `json:"action"`
	WouldBlock bool         `json:"would_block"`
	RiskClass  string       `json:"risk_class"`
	Basis      string       `json:"basis"` // oracle_deny | reference_deny | clear | not_sampled
}

// SpotCheckResult is the autonomous regime's post-hoc spot-check.
// EscapeCandidates counts sampled would-block actions per risk class.
type SpotCheckResult struct {
	Rate             float64            `json:"rate"`
	Seed             uint64             `json:"seed"`
	Sampled          []int              `json:"sampled"`
	Verdicts         []SpotCheckVerdict `json:"verdicts"`
	EscapeCandidates map[string]int     `json:"escape_candidates"`
}

// sampleStreamConst is the fixed second PCG word of the splitmix64 stream
// so the seed alone pins the sample.
const sampleStreamConst = 0x6869746c // "hitl"

// SampleIndices picks the deterministic spot-check sample: count =
// round(n*rate) (bounded to [0, n]), Fisher-Yates shuffled by a splitmix64
// stream keyed by (seed, sampleStreamConst), indices returned sorted.
// Deliberately NOT math/rand: sampling order is a published, reproducible
// function of the seed, and the repo's predid analyzer bans math/rand for
// identifier generation - this is neither an identifier nor an ID fallback
// path.
func SampleIndices(n int, rate float64, seed uint64) []int {
	if n <= 0 || rate <= 0 {
		return []int{}
	}
	count := int(math.Round(float64(n) * rate))
	if count > n {
		count = n
	}
	if count < 0 {
		count = 0
	}
	state := seed ^ sampleStreamConst
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := int(splitmix64(&state) % uint64(i+1))
		perm[i], perm[j] = perm[j], perm[i]
	}
	out := append([]int(nil), perm[:count]...)
	sort.Ints(out)
	return out
}

// splitmix64 advances the stream and returns the next word.
func splitmix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// RunSpotCheck runs the autonomous regime's sampled post-hoc spot-check.
// classify is the ground-truth oracle seam (metrics.ScenarioOracle builds
// it from the security engine's deny replay + the per-action reference
// denials); it is invoked for EVERY action so risk-class denominators are
// fully classified, while would-block verdicts count only inside the
// seeded sample (unsampled actions keep basis "not_sampled"). The action
// stream is never rewritten: verdicts live on the result.
func RunSpotCheck(acts []ActionRecord, rate float64, seed uint64, classify func(ActionRecord) (block bool, class, basis string)) *SpotCheckResult {
	sampled := SampleIndices(len(acts), rate, seed)
	sampledSet := make(map[int]bool, len(sampled))
	for _, i := range sampled {
		sampledSet[i] = true
	}
	res := &SpotCheckResult{
		Rate:             rate,
		Seed:             seed,
		Sampled:          sampled,
		Verdicts:         make([]SpotCheckVerdict, 0, len(acts)),
		EscapeCandidates: map[string]int{},
	}
	for i, a := range acts {
		block, class, basis := false, "unknown", "clear"
		if classify != nil {
			block, class, basis = classify(a)
		}
		if class == "" {
			class = "unknown"
		}
		v := SpotCheckVerdict{Index: i, Action: a, WouldBlock: block, RiskClass: class, Basis: basis}
		if !sampledSet[i] {
			v.WouldBlock = false
			v.Basis = "not_sampled"
		} else if block {
			res.EscapeCandidates[class]++
		}
		res.Verdicts = append(res.Verdicts, v)
	}
	return res
}

// ---------------------------------------------------------------------------
// Scripted-human RPC client (REIMPLEMENTED per the leaf consume note)
// ---------------------------------------------------------------------------

// RPCClient is a minimal length-prefixed JSON-RPC unix-socket client for
// the scripted human. Reimplemented in-package per leaf 04's consume note
// ("not imported from e2e; reimplement the small client in
// internal/eval/hitl"); the wire format is the daemon's canonical
// length-prefixed JSON-RPC (same shape the e2e harness client speaks).
// Not concurrency-safe: one client per scripted human.
type RPCClient struct {
	conn net.Conn
	r    *bufio.Reader
	next int64
}

// DialDaemonRPC dials the daemon's Unix RPC socket.
func DialDaemonRPC(socketPath string) (*RPCClient, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("hitl: dial rpc socket: %w", err)
	}
	return &RPCClient{conn: conn, r: bufio.NewReader(conn)}, nil
}

// Close releases the socket.
func (c *RPCClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Call sends one request and unwraps the response. An RPC error envelope
// returns a non-nil error; a valid result returns the object.
func (c *RPCClient) Call(method string, params any) (map[string]any, error) {
	if c == nil || c.conn == nil {
		return nil, fmt.Errorf("hitl: rpc client closed")
	}
	c.next++
	req := map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("hitl: rpc marshal %s: %w", method, err)
	}
	if _, err := fmt.Fprintf(c.conn, "%d\n", len(payload)); err != nil {
		return nil, fmt.Errorf("hitl: rpc write length %s: %w", method, err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		return nil, fmt.Errorf("hitl: rpc write payload %s: %w", method, err)
	}
	lengthLine, err := c.r.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("hitl: rpc read length %s: %w", method, err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		return nil, fmt.Errorf("hitl: rpc %s: bad length line %q", method, lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, fmt.Errorf("hitl: rpc read payload %s: %w", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		return nil, fmt.Errorf("hitl: rpc decode %s: %w", method, err)
	}
	if e, ok := resp["error"]; ok && e != nil {
		return nil, fmt.Errorf("hitl: rpc %s error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hitl: rpc %s result is not an object: %v", method, resp["result"])
	}
	return result, nil
}

// RPCPermissionSource adapts an RPCClient to the permission registry
// surface (acp.permissions.list / acp.permissions.resolve).
type RPCPermissionSource struct {
	C *RPCClient
}

// Pending implements PermissionSource.
func (s RPCPermissionSource) Pending() ([]PendingPermission, error) {
	res, err := s.C.Call("acp.permissions.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	if enabled, _ := res["enabled"].(bool); !enabled {
		return nil, nil
	}
	raw, _ := res["pending"].([]any)
	out := make([]PendingPermission, 0, len(raw))
	for _, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		pp := PendingPermission{ID: id}
		pp.AgentID, _ = m["agent_id"].(string)
		pp.SessionID, _ = m["session_id"].(string)
		pp.Summary, _ = m["summary"].(string)
		if ca, ok := m["created_at"].(string); ok {
			pp.CreatedAt, _ = time.Parse(time.RFC3339, ca)
		}
		out = append(out, pp)
	}
	return out, nil
}

// Resolve implements PermissionSource.
func (s RPCPermissionSource) Resolve(id, decision string) error {
	_, err := s.C.Call("acp.permissions.resolve", map[string]any{"id": id, "decision": decision})
	return err
}

// RPCPlanGateSource adapts an RPCClient to the plan approval surface
// (plan.list_by_session filtered to pending_approval; plan.approve /
// plan.reject).
type RPCPlanGateSource struct {
	C *RPCClient
}

// PendingPlan implements PlanGateSource.
func (s RPCPlanGateSource) PendingPlan(sessionID string) (planID, title string, ok bool, err error) {
	res, err := s.C.Call("plan.list_by_session", map[string]any{"session_id": sessionID})
	if err != nil {
		return "", "", false, err
	}
	raw, _ := res["plans"].([]any)
	for _, e := range raw {
		m, okMap := e.(map[string]any)
		if !okMap {
			continue
		}
		state, _ := m["state"].(string)
		if state != "pending_approval" {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		title, _ = m["title"].(string)
		return id, title, true, nil
	}
	return "", "", false, nil
}

// DecidePlan implements PlanGateSource ("allow"/"always" approve, "deny"
// rejects; anything else is refused client-side rather than normalized
// into a silent verdict).
func (s RPCPlanGateSource) DecidePlan(planID, sessionID, decision string) error {
	switch decision {
	case "allow", "always":
		_, err := s.C.Call("plan.approve", map[string]any{"plan_id": planID, "session_id": sessionID, "by": "hitl-scripted-human"})
		return err
	case "deny":
		_, err := s.C.Call("plan.reject", map[string]any{"plan_id": planID, "session_id": sessionID, "by": "hitl-scripted-human"})
		return err
	}
	return fmt.Errorf("hitl: invalid plan gate decision %q", decision)
}
