package daemon

// Shipped-behavior acceptance matrix for plan 20261110-caller-authz
// (leaf 02-enforcement/02). This is the tree's acceptance artifact: it
// drives the REAL composed chain that the daemon ships, at the layer
// where a regression can actually fail:
//
//	contact.SQLiteStore (real sqlite, tmpdir)
//	  -> daemonCallerResolver + voice.ResolveCallerOrFailSoft  (O1/O2:
//	     unknown number fail-soft to Info{Number}, call proceeds)
//	  -> voice.Binding + turnRunnerFor stamp site              (real
//	     callerid.With on the turn ctx, via Binding.RunTurn)
//	  -> agent.Executor (real checkPermission -> details injection:
//	     caller_approved/caller_known presence-only keys)
//	  -> pkg/security.PermissionChecker routed by details["agent_id"]
//	     == employee id (the O5 voice-loop routing fix; the loop-level
//	     half — loop.SetAgentID on voice clones — is pinned by
//	     TestVoiceTurnRunnerStampsEmployeeAgentID in
//	     caller_resolve_test.go §4)
//	  -> employee.PreExecChecker step 1a/1b/1c (real RegisterPreExecCheckers
//	     construction shape: NewPreExecChecker + WithUnapprovedCallerAllowed-
//	     Tools(cfg.Voice...), adapter via NewPreExecAdapter)
//	  -> ExecutionResult.Error byte-check (the string the model sees)
//	  -> real employee.AuditStore row count (voice.unapproved_action)
//	  -> SetAutoPause spy (no-pause contract, with a never-rule contrast
//	     row proving the spy fires on paths that DO pause)
//
// WIRING CAVEAT pinned by this frame: the production boot site
// (manager.RegisterPreExecCheckers) does NOT call SetAuditStore/
// SetAutoPause on the per-employee checkers it builds, so a stock
// daemon's step-1c audit row is a nil-seam no-op. This matrix arms both
// seams explicitly (the checker's documented setters) to pin the
// FROZEN contract behavior; the wiring gap is reported to the owner.
//
// What is deliberately NOT in the frame: the Telnyx webhook/transport
// layer and the loop/child sockets — those legs are pinned by branch-01
// gate tests and the voice e2e suites. Everything caller-AUTHZ relevant
// runs for real here.
//
// Case map (02-docs-proof.md task 2):
//	(a) unknown caller + shell_execute -> denied; deny text byte-checked
//	    in the model-facing error; reason token caller_not_approved; NO
//	    auto-pause; exactly one voice.unapproved_action audit row (with
//	    tool + ccid evidence); an allow writes no further row.
//	(b) unknown caller + send_agent_message and + ledger_append -> the
//	    step-1c gate ALLOWS both (O3 message lane). The ledger_append leg
//	    additionally pins the shipped end-to-end outcome: 1c never denies
//	    it even with a state-setting set_status payload (O4 pinned —
//	    owner Q1 pending, see the case comment), and today the executor's
//	    base-rule layer refuses it with "Unknown action" (a pkg/security
//	    ToolActionMap/BuiltinRules gap from the employee-ledger tree,
//	    H7 class — NOT a caller-gate deny).
//	(c) approved caller + a tool the employee constitution forbids ->
//	    denied by step 1b, not 1c (Reason carries tools_allowed, never
//	    caller_not_approved; composed CheckResult.RuleSource ==
//	    "employee_pre_exec" proves the gate was reached at all) — the
//	    O5 proof that voice-stamped turns route the employee constitution.
//	(d) non-voice ctx (no caller keys) -> 1c inert: shell_execute
//	    succeeds byte-clean, while the same call with a stamped
//	    unapproved caller denies.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/callerid"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/security"
)

// Frozen strings — byte-mirrors of internal/employee/enforcement.go
// (unapprovedCallerDenyText / unapprovedCallerDenyReason /
// unapprovedCallerAuditType) and the executor's deny rendering
// ("permission denied: <Reason>"). Kept literal here on purpose: an
// accidental retarget of the shipped strings must fail THIS matrix,
// not ride a renamed constant. Same mirroring posture as
// internal/agent/caller_details_test.go and
// internal/employee/caller_gate_test.go.
const (
	matrixCallerDenyText   = "This caller is not approved for that action. Take a message instead."
	matrixCallerDenyReason = "caller_not_approved: " + matrixCallerDenyText
	matrixCallerDenyModel  = "permission denied: " + matrixCallerDenyReason
	matrixCallerAuditType  = "voice.unapproved_action"
)

// matrixE164 builds a test E.164 (digit runs are fmt-built, not literal
// source strings — agent tooling redacts long digit runs on display;
// same posture as callerGateE164 in caller_resolve_test.go).
func matrixE164(n int) string { return fmt.Sprintf("+1555%07d", n) }

// ---------------------------------------------------------------------------
// Fakes (matrix-prefixed; the caller-authz gate fakes in
// caller_resolve_test.go are structurally identical but package-namespaced
// to that file's gate — no reuse, no redefinition collision).
// ---------------------------------------------------------------------------

type matrixFakeCalls struct{}

func (matrixFakeCalls) Start(_ context.Context, _ voice.CallRecord) error { return nil }
func (matrixFakeCalls) End(_ context.Context, _ string, _ string) error   { return nil }
func (matrixFakeCalls) AppendTranscript(_ context.Context, _ string, _ voice.TranscriptLine) error {
	return nil
}
func (matrixFakeCalls) TodayTranscripts(_ context.Context, _ string) ([]voice.CallTranscript, error) {
	return nil, nil
}
func (matrixFakeCalls) SessionIDFor(_, _ string) (string, bool) { return "", false }

type matrixFakeSessions struct{}

func (matrixFakeSessions) Create(name string) (voice.SessionHandle, error) {
	return voice.SessionHandle{ID: "sess-" + name, ConversationID: "conv-1"}, nil
}
func (matrixFakeSessions) List() ([]voice.NamedSession, error)              { return nil, nil }
func (matrixFakeSessions) UpdateName(sessionID, name string) error          { return nil }
func (matrixFakeSessions) SetEmployeeID(sessionID, employeeID string) error { return nil }

type matrixFakeProgress struct{}

func (matrixFakeProgress) PublishProgress(voice.ProgressEvent) {}

// matrixCaptureTurns is the voice.TurnRunner fake that captures the FULL
// onward context of each turn — i.e. exactly the ctx bindingTurnRunner.Run
// stamped with callerid.With + the employee id — and hands back a canned
// reply. The captured ctx is what Execute runs the matrix on.
type matrixCaptureTurns struct {
	mu  sync.Mutex
	ctx []context.Context
}

func (c *matrixCaptureTurns) Run(ctx context.Context, _ voice.SessionHandle, _ string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx = append(c.ctx, ctx)
	return "ok", nil
}

func (c *matrixCaptureTurns) captured(t *testing.T) context.Context {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.ctx) != 1 {
		t.Fatalf("captured %d turn ctxs, want exactly 1", len(c.ctx))
	}
	return c.ctx[0]
}

// matrixPauseSpy records every SetAutoPause invocation.
type matrixPauseSpy struct {
	mu     sync.Mutex
	calls  []string
	called int
}

func (s *matrixPauseSpy) fn() employee.AutoPauseFunc {
	return func(employeeID, reason string) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.called++
		s.calls = append(s.calls, employeeID+": "+reason)
		return nil
	}
}

func (s *matrixPauseSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.called
}

// ---------------------------------------------------------------------------
// Composition harness — the daemon's RegisterPreExecCheckers shape,
// driven in-package.
// ---------------------------------------------------------------------------

type matrixHarness struct {
	turnCtx    context.Context // the stamped voice turn ctx (real stamp site)
	executor   *agent.Executor
	checker    *security.PermissionChecker // composed CheckResult assertions
	preExec    *employee.PreExecChecker
	audit      *employee.AuditStore
	pause      *matrixPauseSpy
	toolRan    map[string]int
	ranMu      *sync.Mutex
	employeeID string
}

// matrixVoiceTools are the tool names the matrix registers; all three are
// REAL builtin registry names (agent_message.go / ledger.go / shell).
var matrixVoiceTools = []string{"shell_execute", "send_agent_message", "ledger_append"}

// newMatrixHarness composes the shipped chain for one case:
//   - constitution + [voice] allowed-tools list through the daemon's
//     RegisterPreExecCheckers construction shape (NewPreExecChecker +
//     WithUnapprovedCallerAllowedTools + NewPreExecAdapter),
//   - a REAL contact store resolved through the REAL resolver + fail-soft
//     seam at the answer-site shape,
//   - the REAL bindingTurnRunner stamp site producing the turn ctx,
//   - a REAL agent.Executor over a REAL pkg/security.PermissionChecker,
//     routed by executor agent id == employee id (the O5 shipped shape).
//
// contactApproved pins the contact row for `number` with the given
// voice_approved bit; empty number means "store stays empty" (the unknown
// caller case). constitution may be nil for the fully-permissive default.
func newMatrixHarness(t *testing.T, employeeID, number string, contactApproved bool, constitution *employee.Constitution) *matrixHarness {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)

	// -- real contact store (O2 system of record).
	cstore, err := contact.NewStore(t.TempDir() + "/contacts.db")
	if err != nil {
		t.Fatalf("contact.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = cstore.Close() })
	if number != "" {
		if err := cstore.Put(context.Background(), contact.Record{
			ID: "c-" + employeeID, DisplayName: "Matrix Contact", Phone: number, VoiceApproved: contactApproved,
		}); err != nil {
			t.Fatalf("contact Put: %v", err)
		}
	}

	// -- answer-site resolution, exactly the WireVoice spawn-seam shape:
	// daemonCallerResolver + ResolveCallerOrFailSoft (fail-soft is the
	// O1 posture: the call ALWAYS proceeds; unknown -> unapproved per O2).
	resolver := daemonCallerResolver(cstore, logger)
	caller := voice.ResolveCallerOrFailSoft(context.Background(), resolver, number, logger)

	// -- the REAL stamp site: bindingTurnRunner over a REAL Binding.
	turns := &matrixCaptureTurns{}
	b, err := voice.NewBinding(voice.BindingDeps{
		Calls:    matrixFakeCalls{},
		Sessions: matrixFakeSessions{},
		Turns:    turns,
		Progress: matrixFakeProgress{},
	})
	if err != nil {
		t.Fatalf("voice.NewBinding: %v", err)
	}
	const ccid = "cc-matrix-1"
	if _, err := b.OnCallStart(context.Background(), ccid, number, employeeID, "incoming", voice.DayBrief{}); err != nil {
		t.Fatalf("Binding.OnCallStart: %v", err)
	}
	inst := voice.Instance{CallControlID: ccid, EmployeeID: employeeID, Target: number, Caller: caller}
	if _, err := turnRunnerFor(b, inst).Run(context.Background(), voice.SessionHandle{}, "hello"); err != nil {
		t.Fatalf("bindingTurnRunner.Run: %v", err)
	}
	turnCtx := turns.captured(t)
	// The stamp must be on the ctx the executor will read (sanity — the
	// gate test owns the full stamping proof).
	if _, ok := callerid.From(turnCtx); !ok {
		t.Fatal("turn ctx carries no callerid stamp — matrix frame broken")
	}

	// -- the checker, built in the daemon's construction shape.
	if constitution == nil {
		constitution = matrixPermissiveConstitution()
	}
	allowed := config.DefaultConfig().Voice.UnapprovedCallerAllowedTools
	if len(allowed) == 0 {
		t.Fatal("DefaultConfig().Voice.UnapprovedCallerAllowedTools empty — O3 message lane must ship by default")
	}
	preExec := employee.NewPreExecChecker(employeeID, constitution,
		employee.WithUnapprovedCallerAllowedTools(allowed))
	// The audit + auto-pause seams are the checker's production setters
	// (RegisterPreExecCheckers itself does NOT call SetAuditStore — see
	// the leaf report note; the matrix wires it explicitly so the
	// frozen voice.unapproved_action row is assertable).
	audit, err := employee.NewAuditStore(t.TempDir() + "/audit.db")
	if err != nil {
		t.Fatalf("employee.NewAuditStore: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	preExec.SetAuditStore(audit)
	spy := &matrixPauseSpy{}
	preExec.SetAutoPause(spy.fn())

	pc := security.NewPermissionChecker(security.Config{})
	pc.SetPreExecChecker(employeeID, employee.NewPreExecAdapter(preExec))

	// -- the executor (loop-restamped-to-employee-id shape, O5).
	ran := map[string]int{}
	var ranMu sync.Mutex
	registry := agent.NewPlaceholderToolRegistry()
	for _, name := range matrixVoiceTools {
		registry.Register(agent.NewMockTool(name, "matrix probe "+name,
			func(ctx context.Context, args map[string]any) (any, error) {
				ranMu.Lock()
				defer ranMu.Unlock()
				ran[name]++
				return map[string]any{"ran": name}, nil
			}))
	}
	exec := agent.NewExecutor(registry, pc,
		agent.WithExecutorAgentID(employeeID),
		agent.WithExecutorLogger(logger))

	return &matrixHarness{
		turnCtx:    turnCtx,
		executor:   exec,
		checker:    pc,
		preExec:    preExec,
		audit:      audit,
		pause:      spy,
		toolRan:    ran,
		ranMu:      &ranMu,
		employeeID: employeeID,
	}
}

func (h *matrixHarness) runCount(tool string) int {
	h.ranMu.Lock()
	defer h.ranMu.Unlock()
	return h.toolRan[tool]
}

// matrixPermissiveConstitution constrains nothing: every matrix deny in
// the permissive frame can only come from step 1c.
func matrixPermissiveConstitution() *employee.Constitution {
	return &employee.Constitution{
		Purpose:      "voice companion",
		Role:         "companion",
		AutonomyTier: employee.Tier2Propose,
		Constraints: employee.ConstitutionalConstraints{
			RiskCeiling: employee.RiskCeilingCritical,
		},
	}
}

// matrixMessageLaneConstitution mirrors the shipped voice-employee shape:
// the constitution itself allows ONLY the message lane.
func matrixMessageLaneConstitution() *employee.Constitution {
	c := matrixPermissiveConstitution()
	c.Constraints.ToolsAllowed = []string{"ledger_append", "send_agent_message"}
	return c
}

// matrixToolCall builds a model-shaped tool call.
func matrixToolCall(name, argsJSON string) llm.ToolCall {
	return llm.ToolCall{
		ID:       "call-" + name,
		Type:     "function",
		Function: llm.ToolCallFunction{Name: name, Arguments: argsJSON},
	}
}

// matrixCallerAuditRows returns voice.unapproved_action findings for the
// harness employee via the REAL store List path.
func matrixCallerAuditRows(t *testing.T, h *matrixHarness) []employee.AuditFinding {
	t.Helper()
	rows, err := h.audit.List(context.Background(), employee.AuditListFilter{EmployeeID: h.employeeID})
	if err != nil {
		t.Fatalf("AuditStore.List: %v", err)
	}
	var out []employee.AuditFinding
	for _, r := range rows {
		if r.ViolatedRule == matrixCallerAuditType {
			out = append(out, r)
		}
	}
	return out
}

// TestMain-free note: the mock increments under harness.ranMu; cases run
// serially per harness (one Execute per assertion path).

// ---------------------------------------------------------------------------
// (a) unknown caller + shell_execute -> denied, text byte-checked, no
//     pause, exactly one voice.unapproved_action audit row.
// ---------------------------------------------------------------------------

func TestCallerMatrix_UnknownCallerShellDenied(t *testing.T) {
	h := newMatrixHarness(t, "emp-m-a", "", false, nil) // empty store -> unknown number

	res := h.executor.Execute(h.turnCtx, matrixToolCall("shell_execute", `{"command":"ls","conversation_id":"cc-matrix-1"}`))
	if res == nil {
		t.Fatal("nil ExecutionResult")
	}
	if res.Success {
		t.Fatalf("unknown caller executed shell_execute: %+v", res)
	}
	// Byte-check the full model-facing error: the frozen prefix + reason
	// token + deny text, verbatim (frozen contract).
	if res.Error != matrixCallerDenyModel {
		t.Errorf("model-facing Error =\n  %q\nwant exactly\n  %q", res.Error, matrixCallerDenyModel)
	}
	if !strings.HasPrefix(res.Error, "permission denied: caller_not_approved: ") {
		t.Errorf("Error must carry the caller_not_approved reason prefix; got %q", res.Error)
	}
	// The tool never ran.
	if got := h.runCount("shell_execute"); got != 0 {
		t.Errorf("shell_execute mock ran %d times, want 0", got)
	}
	// NO auto-pause (contrast: never[] pauses — pinned in the contrast
	// row of case (c)); the spy must not have fired at all.
	if n := h.pause.count(); n != 0 {
		t.Errorf("caller deny must NOT auto-pause; pause fired %d times: %v", n, h.pause.calls)
	}
	// Exactly one voice.unapproved_action audit row (tool + ccid evidence).
	rows := matrixCallerAuditRows(t, h)
	if len(rows) != 1 {
		t.Fatalf("voice.unapproved_action rows = %d, want exactly 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.EmployeeID != h.employeeID {
		t.Errorf("audit row EmployeeID = %q, want %q", row.EmployeeID, h.employeeID)
	}
	if !strings.Contains(row.Evidence, "shell_execute") || !strings.Contains(row.Evidence, "ccid=cc-matrix-1") {
		t.Errorf("audit Evidence = %q, want tool name + ccid", row.Evidence)
	}
	if row.Severity != employee.SeverityWarning {
		t.Errorf("audit Severity = %q, want %q (warning, not critical)", row.Severity, employee.SeverityWarning)
	}

	// An ALLOW call must not append further rows (one row per deny).
	allow := h.executor.Execute(h.turnCtx, matrixToolCall("send_agent_message", `{"to":"boss","message":"hi","conversation_id":"cc-matrix-1"}`))
	if !allow.Success {
		t.Fatalf("case-(b) send_agent_message unexpectedly denied here: %q", allow.Error)
	}
	if rows := matrixCallerAuditRows(t, h); len(rows) != 1 {
		t.Errorf("allow call added audit rows: %d, want still 1", len(rows))
	}
	if n := h.pause.count(); n != 0 {
		t.Errorf("still no pause after allow; fired %d", n)
	}
}

// ---------------------------------------------------------------------------
// (b) unknown caller + message lane: send_agent_message executes for
//     real; ledger_append passes step 1c even with a state-setting
//     payload.
//
//     O4 PINNED (owner Open Question Q1 pending): step 1c adjudicates on
//     the TOOL NAME only — it cannot distinguish a note-taking
//     ledger_append from one carrying set_status, so a state-setting
//     ledger_append from an unknown caller is NOT denied by the caller
//     gate TODAY. This is the accepted residual risk of the tree; if Q1
//     lands a payload-kind check in 1c, this subcase is the one that
//     must flip. Do not silently narrow.
//
//     Additionally pinned, as shipped reality: past the 1c gate, the
//     executor's base-rule layer currently refuses ledger_append with
//     "Unknown action: ledger_append" — the tool has no ToolActionMap/
//     BuiltinRules row (H7-class gap left by the employee-ledger tree;
//     internal/agent/tool_action_completeness_test.go does not enumerate
//     it either). That deny is NOT a caller-gate deny (same outcome for
//     an approved caller — asserted below). When the H7 row lands,
//     update this base-layer pin; the 1c pin (no caller_not_approved on
//     ledger_append, any payload) must hold either way.
// ---------------------------------------------------------------------------

func TestCallerMatrix_UnknownCallerMessageLaneAllowed(t *testing.T) {
	h := newMatrixHarness(t, "emp-m-b", "", false, nil)

	// send_agent_message: full Execute pass — the tool actually ran.
	res := h.executor.Execute(h.turnCtx, matrixToolCall("send_agent_message", `{"to":"boss","message":"hold my call","conversation_id":"cc-matrix-1"}`))
	if !res.Success {
		t.Fatalf("unknown caller send_agent_message must pass the message lane (O3); got %+v", res)
	}
	if got := h.runCount("send_agent_message"); got != 1 {
		t.Errorf("send_agent_message mock ran %d times, want 1", got)
	}

	// ledger_append with a state-setting payload: step 1c must NOT deny
	// it (O4). See the case header — owner Q1 pending; the shipped
	// end-to-end outcome is the base-rule Unknown-action pin below.
	statePayload := `{"kind":"observation","subject":"acme-roof","payload":{"note":"caller demands refund"},` +
		`"set_status":"waiting_on_me","summary":"angry unknown caller, refund demanded","conversation_id":"cc-matrix-1"}`
	led := h.executor.Execute(h.turnCtx, matrixToolCall("ledger_append", statePayload))
	if led.Success {
		// If this ever goes green, the H7 base-rule gap was closed — the
		// full O4 residual risk is then live end-to-end; re-pin below.
		t.Log("ledger_append executed end-to-end: employee-ledger H7 base-rule gap appears CLOSED")
	} else {
		if strings.Contains(led.Error, "caller_not_approved") {
			t.Errorf("step 1c must NEVER deny ledger_append (O3 message lane): %q", led.Error)
		}
		if want := "permission denied: Unknown action: ledger_append"; led.Error != want {
			t.Errorf("ledger_append end-to-end Error = %q, want %q (1c allows; base rule H7-denies)", led.Error, want)
		}
	}
	// The 1c gate itself, byte-exact: state-setting payload, Allowed.
	details := matrixDetails("ledger_append", h.employeeID, false, false, "cc-matrix-1")
	if dec := h.preExec.Check("platform_read", "ledger_append", details); !dec.Allowed {
		t.Errorf("step 1c denied ledger_append(set_status) for unknown caller: %+v (O4 pin: gate passes TODAY)", dec)
	}

	// No caller-gate audit rows anywhere on the message lane: the two
	// above calls passed 1c, so voice.unapproved_action stays at zero.
	if rows := matrixCallerAuditRows(t, h); len(rows) != 0 {
		t.Errorf("message-lane allows wrote %d voice.unapproved_action rows, want 0", len(rows))
	}
}

// matrixDetails builds the details map the executor's checkPermission
// produces for a stamped caller (tool_name + agent_id + presence-only
// caller keys, strconv.FormatBool values — the frozen §5 shape).
func matrixDetails(toolName, employeeID string, approved, known bool, ccid string) map[string]string {
	return map[string]string{
		"tool_name":       toolName,
		"agent_id":        employeeID,
		"caller_approved": boolStr(approved),
		"caller_known":    boolStr(known),
		"conversation_id": ccid,
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// (c) approved caller + constitution-forbidden tool -> denied by step 1b
//     (not 1c), proving the O5 employee routing fires on a voice-stamped
//     chain: the checker is only reached because the executor injects
//     agent_id == employee id (voice clones restamped) AND the permission
//     checker routes on it. A caller-gate deny here would be a
//     contract violation (approved caller: 1c inert).
//
//     Contrast rows in the same frame pin: (i) the composed CheckResult
//     stamps RuleSource "employee_pre_exec" (the constitution deny came
//     FROM the employee checker stage), (ii) the never[] step DOES
//     auto-pause while the 1c/1b caller-family denies do NOT, and
//     (iii) the identical call from a non-approved caller is denied by
//     1a/1b FIRST — 1b wins the ordering (constitution deny outranks
//     the caller gate; caller allow never rescues a constitution deny).
// ---------------------------------------------------------------------------

func TestCallerMatrix_ApprovedCallerConstitutionDeny(t *testing.T) {
	c := matrixMessageLaneConstitution()
	// A never rule the message lane can still trip — for the pause
	// contrast below.
	c.Constraints.Never = []string{"delete the roof ledger"}

	number := matrixE164(301)
	h := newMatrixHarness(t, "emp-m-c", number, true, c)

	// Precondition: the resolver approved THIS caller (contact row).
	info, _ := callerid.From(h.turnCtx)
	if !info.Approved || !info.Known {
		t.Fatalf("turn ctx caller = %+v, want approved+known contact (fixture precondition)", info)
	}

	// shell_execute is outside the employee's tools_allowed.
	res := h.executor.Execute(h.turnCtx, matrixToolCall("shell_execute", `{"command":"ls","conversation_id":"cc-matrix-1"}`))
	if res.Success {
		t.Fatal("approved caller executed a constitution-forbidden tool — O5 routing dead")
	}
	if strings.Contains(res.Error, "caller_not_approved") {
		t.Errorf("deny must come from step 1b, NOT 1c; Error = %q", res.Error)
	}
	if want := `is not in tools_allowed list`; !strings.Contains(res.Error, want) {
		t.Errorf("Error = %q, want the 1b deny (%q)", res.Error, want)
	}
	// Composed stage proof: the deny carries the employee_pre_exec
	// RuleSource (the CheckResult was produced by the registered
	// PreExecChecker, not a later pipeline stage).
	cr := h.checker.CheckPermissionForAgent("shell_execute", matrixDetails("shell_execute", h.employeeID, true, true, "cc-matrix-1"), h.employeeID)
	if cr.Allowed {
		t.Error("composed CheckPermissionForAgent allowed a constitution-forbidden tool")
	}
	if cr.RuleSource != "employee_pre_exec" {
		t.Errorf("CheckResult.RuleSource = %q, want %q", cr.RuleSource, "employee_pre_exec")
	}
	// Ordering pin: an UNAPPROVED caller reaching the same
	// constitution-forbidden tool still gets the 1b deny (1a/1b run
	// before 1c; the caller gate never rewrites a constitution deny).
	unapproved := h.checker.CheckPermissionForAgent("shell_execute", matrixDetails("shell_execute", h.employeeID, false, false, "cc-matrix-1"), h.employeeID)
	if unapproved.Allowed || !strings.Contains(unapproved.Reason, "tools_allowed") || strings.Contains(unapproved.Reason, "caller_not_approved") {
		t.Errorf("unapproved caller on a 1b-forbidden tool = %+v, want the tools_allowed deny (1b before 1c)", unapproved)
	}

	// Contrast (pause semantics): the never[] step DOES auto-pause and
	// the caller/tool-gate denies do NOT. An approved caller's
	// message-lane call whose payload trips the never rule: 1c inert
	// (approved), step 2 fires -> critical deny + auto-pause.
	before := h.pause.count()
	nv := h.executor.Execute(h.turnCtx, matrixToolCall("send_agent_message",
		`{"to":"boss","message":"delete the roof ledger","conversation_id":"cc-matrix-1"}`))
	if nv.Success {
		t.Fatal("never-rule payload executed")
	}
	if h.pause.count() != before+1 {
		t.Errorf("never-rule deny must auto-pause once; spy count %d -> %d", before, h.pause.count())
	}
	// The earlier caller-adjacent denies (1b shell_execute above) added
	// no pause and no voice.unapproved_action rows.
	if rows := matrixCallerAuditRows(t, h); len(rows) != 0 {
		t.Errorf("approved-caller frame wrote %d voice.unapproved_action rows, want 0 (1c never fired)", len(rows))
	}
}

// ---------------------------------------------------------------------------
// (d) non-voice ctx (no callerid stamp -> no caller keys in details):
//     step 1c is inert and the call is byte-clean. Contrast pair: the
//     SAME executor/registry/ctx-without-the-binding, directly stamped
//     with an unapproved caller (the shape a caller-carrying turn takes),
//     denies with the frozen text. Also pins the executor's injection
//     absence: no stamp -> no caller_approved/caller_known keys reach
//     the checker (the §5 golden, asserted at the composed decision
//     layer).
// ---------------------------------------------------------------------------

func TestCallerMatrix_NonVoiceTurnGateInert(t *testing.T) {
	h := newMatrixHarness(t, "emp-m-d", "", false, nil)

	// A plain (non-voice) turn context: never passed through
	// bindingTurnRunner. shell_execute is OUTSIDE the shipped 1c
	// message lane, so if 1c were wrongly keyed on something other than
	// the caller keys, this call would deny.
	plainCtx := context.Background()
	res := h.executor.Execute(plainCtx, matrixToolCall("shell_execute", `{"command":"ls"}`))
	if !res.Success {
		t.Fatalf("non-voice turn denied by caller gate: %+v (1c must be inert without caller keys)", res)
	}
	if res.Error != "" {
		t.Errorf("non-voice turn Error = %q, want empty", res.Error)
	}
	if strings.Contains(fmt.Sprint(res.Result), "caller_not_approved") {
		t.Errorf("caller deny leaked into a non-voice turn: %+v", res)
	}
	// No audit rows, no pause on the inert path.
	if rows := matrixCallerAuditRows(t, h); len(rows) != 0 {
		t.Errorf("inert non-voice turn wrote %d audit rows, want 0", len(rows))
	}
	if n := h.pause.count(); n != 0 {
		t.Errorf("inert non-voice turn fired pause %d times, want 0", n)
	}

	// Contrast at the composed checker layer: a details map WITHOUT the
	// caller keys allows shell_execute (a permissive constitution),
	// while the identical map WITH caller_approved=false denies —
	// i.e. the keys' presence, not the employee id, arms 1c.
	noKeys := map[string]string{"tool_name": "shell_execute", "agent_id": h.employeeID}
	if dec := h.preExec.Check("shell_execute", "shell_execute", noKeys); !dec.Allowed {
		t.Errorf("no-caller-keys details denied on a permissive constitution: %+v", dec)
	}
	withKeys := matrixDetails("shell_execute", h.employeeID, false, false, "cc-x")
	if dec := h.preExec.Check("shell_execute", "shell_execute", withKeys); dec.Allowed {
		t.Error("stamped unapproved caller allowed shell_execute — 1c not firing")
	} else if !strings.HasPrefix(dec.Reason, "caller_not_approved") {
		t.Errorf("1c Reason = %q, want caller_not_approved prefix", dec.Reason)
	}

	// And the same ctx with a direct stamp rides the frozen model text.
	stamped := callerid.With(plainCtx, callerid.Info{Number: matrixE164(999)})
	res2 := h.executor.Execute(stamped, matrixToolCall("shell_execute", `{"command":"ls"}`))
	if res2.Error != matrixCallerDenyModel {
		t.Errorf("stamped non-binding ctx Error = %q, want %q", res2.Error, matrixCallerDenyModel)
	}
}
