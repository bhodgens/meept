package employee

// Test gate for plan 20261110-caller-authz, leaf 02-enforcement/00T (§1-§4).
// Gates impl leaf 02-enforcement/01 (enforcement step 1c).
//
// ---------------------------------------------------------------------------
// PINNED DESIGN FOR THE IMPL LEAF (compile-RED anchor)
// ---------------------------------------------------------------------------
// Step 1c gets the unapproved-caller allowed-tool list via a FUNCTIONAL
// OPTION on the existing constructor — the constructor keeps its current
// shape (manager.go RegisterPreExecCheckers / preexec_adapter.go call sites
// do not break; components.go passes cfg.Voice.UnapprovedCallerAllowedTools):
//
//	func NewPreExecChecker(employeeID string, c *Constitution, opts ...PreExecOption) *PreExecChecker
//	func WithUnapprovedCallerAllowedTools(tools []string) PreExecOption
//
// The caller identity reaches the checker through the details map ONLY —
// frozen contract §5 (internal/agent executor injection): when a caller is
// stamped on the turn context, checkPermission adds the presence-only keys
// caller_approved / caller_known. Step 1c therefore fires iff BOTH keys are
// present (a turn with no keys is non-voice: 1c inert). Approved==false ->
// deny any tool NOT in the allowed list. This needs no signature change to
// security.PreExecChecker / pkg/security.PreExecDecision and no engine
// plumbing — the adapter already forwards details unchanged.
//
// Ordering: 1c runs after 1a (tools_forbidden) and 1b (tools_allowed),
// before 2/never — so a constitution deny wins over the caller gate, and a
// constitution allow does NOT rescue an unapproved caller. The enforcement
// order comment (enforcement.go ~:540) must list 1c.
//
// Deny shape (FROZEN): Decision.Allowed=false,
// Reason == "caller_not_approved: " + the pinned model-facing text
// verbatim — §6 requires BOTH the reason token and the text to reach the
// model, and Reason is the only field the security pipeline propagates
// (pkg/security stage 0 copies preDecision.Reason into CheckResult.Reason,
// executor renders "permission denied: <Reason>"). Severity "warning",
// RequiresPlan false, EscalateTo nil. NO auto-pause (contrast never[]).
// One audit row per deny via the existing recordDenial/AuditStore seam
// (1a/1b precedent: ViolatedRule carries the marker) with
// ViolatedRule == "voice.unapproved_action" and Evidence carrying the tool
// name and the conversation id (details["conversation_id"], ccid).
//
// Caller-key encoding (pinned by this gate): details["caller_approved"] /
// details["caller_known"] are strconv.FormatBool strings ("true"/"false"),
// present ONLY when callerid.From(ctx) reports ok. Step 1c fires iff
// caller_approved is present and != "true".
// ---------------------------------------------------------------------------

import (
	"context"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Frozen strings — keep byte-identical with master.md §Interface Contracts
// and internal/agent/caller_details_test.go (agent cannot import employee,
// the constant is mirrored there with a cross-reference comment).
const (
	callerDenyText        = "This caller is not approved for that action. Take a message instead."
	callerNotApprovedMark = "caller_not_approved"
	callerAuditType       = "voice.unapproved_action"
	// callerDenyReason is the exact Decision.Reason the executor renders
	// to the model as "permission denied: <callerDenyReason>" (§6).
	callerDenyReason = callerNotApprovedMark + ": " + callerDenyText
)

// Test pin of the [voice] unapproved_caller_allowed_tools default
// (config.DefaultUnapprovedCallerAllowedTools — real builtin names pinned
// by leaf 01-identity/01). Inlined to avoid an employee->config import edge.
var callerDefaultAllowedTools = []string{"ledger_append", "send_agent_message"}

// callerInfoDetails builds the details map the agent executor injects for a
// stamped caller (§5 contract: presence-only caller_approved/caller_known,
// strconv.FormatBool values).
func callerInfoDetails(approved, known bool, extra map[string]string) map[string]string {
	d := map[string]string{
		"caller_approved": strconv.FormatBool(approved),
		"caller_known":    strconv.FormatBool(known),
	}
	for k, v := range extra {
		d[k] = v
	}
	return d
}

// permissiveConstitution has no tool gating, no never rules, no escalation,
// no budget thresholds, and an effectively infinite risk ceiling — so in the
// §1 tables the ONLY thing that can deny is step 1c.
func permissiveConstitution() *Constitution {
	return &Constitution{
		Purpose:      "voice companion",
		Role:         "companion",
		AutonomyTier: Tier2Propose,
		Constraints: ConstitutionalConstraints{
			RiskCeiling: "critical",
		},
	}
}

// voiceEmployeeConstitution mirrors the real wired shape (master.md
// grounding): a voice employee whose constitution allows only the
// message-taking lane; step 1b must win over step 1c.
func voiceEmployeeConstitution() *Constitution {
	c := permissiveConstitution()
	c.Constraints.ToolsAllowed = []string{"send_agent_message", "ledger_append"}
	return c
}

func newCallerGateChecker(t *testing.T, c *Constitution, allowed []string) *PreExecChecker {
	t.Helper()
	// RED ANCHOR: WithUnapprovedCallerAllowedTools does not exist until
	// impl leaf 02-01 — this file fails to compile before it lands.
	return NewPreExecChecker("emp-caller", c, WithUnapprovedCallerAllowedTools(allowed))
}

// ---------------------------------------------------------------------------
// §1 — deny/allow truth table.
// ---------------------------------------------------------------------------

func TestCallerGate_Check(t *testing.T) {
	type tc struct {
		name         string
		constitution *Constitution
		allowed      []string
		tool         string
		details      map[string]string
		wantAllow    bool
		wantReason   string // exact (pin) or "" (don't care)
		wantContains string // substring of Reason, "" = none
	}

	permissive := permissiveConstitution()
	voiceEmp := voiceEmployeeConstitution()

	shell := func(extra map[string]string) map[string]string {
		d := callerInfoDetails(false, false, extra)
		d["tool_name"] = "shell_execute"
		return d
	}

	tests := []tc{
		// approved caller: 1c inert, everything else proceeds normally.
		{
			name:         "approved caller unknown tool passes 1c",
			constitution: permissive,
			allowed:      callerDefaultAllowedTools,
			tool:         "shell_execute",
			details: func() map[string]string {
				d := callerInfoDetails(true, true, nil)
				d["tool_name"] = "shell_execute"
				return d
			}(),
			wantAllow:  true,
			wantReason: "",
		},
		// unapproved + tool in the allowed list: pass. Both default-allowed
		// tools MUST pass under the shipped defaults (O3 message lane).
		{
			name:         "unapproved caller ledger_append allowed by defaults",
			constitution: permissive,
			allowed:      callerDefaultAllowedTools,
			tool:         "ledger_append",
			details:      callerInfoDetails(false, false, map[string]string{"tool_name": "ledger_append"}),
			wantAllow:    true,
		},
		{
			name:         "unapproved caller send_agent_message allowed by defaults",
			constitution: permissive,
			allowed:      callerDefaultAllowedTools,
			tool:         "send_agent_message",
			details:      callerInfoDetails(false, true, map[string]string{"tool_name": "send_agent_message"}),
			wantAllow:    true,
		},
		// unapproved + tool absent from allowed list: deny with pinned shape.
		{
			name:         "unapproved caller shell_execute denied pinned text",
			constitution: permissive,
			allowed:      callerDefaultAllowedTools,
			tool:         "shell_execute",
			details:      shell(nil),
			wantAllow:    false,
			wantReason:   callerDenyReason,
		},
		{
			name:         "unapproved known caller file_write denied",
			constitution: permissive,
			allowed:      callerDefaultAllowedTools,
			tool:         "file_write",
			details:      callerInfoDetails(false, true, map[string]string{"tool_name": "file_write"}),
			wantAllow:    false,
			wantReason:   callerDenyReason,
		},
		// empty allowed list = no caller message rights at all (config
		// normalizes empty->default at load; the gate itself honors the
		// constructed list verbatim).
		{
			name:         "empty allowed list denies message tool too",
			constitution: permissive,
			allowed:      []string{},
			tool:         "send_agent_message",
			details:      callerInfoDetails(false, false, map[string]string{"tool_name": "send_agent_message"}),
			wantAllow:    false,
			wantReason:   callerDenyReason,
		},
		// custom list override passes through untouched.
		{
			name:         "custom allowed list admits web_fetch",
			constitution: permissive,
			allowed:      []string{"web_fetch"},
			tool:         "web_fetch",
			details:      callerInfoDetails(false, false, map[string]string{"tool_name": "web_fetch"}),
			wantAllow:    true,
		},
		// Unknown caller (zero Info stamp: approved=false, known=false) is
		// gated exactly like an unapproved contact (decision O2).
		{
			name:         "zero-Info unknown caller denied shell",
			constitution: permissive,
			allowed:      callerDefaultAllowedTools,
			tool:         "shell_execute",
			details:      callerInfoDetails(false, false, nil),
			wantAllow:    false,
			wantReason:   callerDenyReason,
		},
		// Ordering: 1b (constitution tools_allowed) runs BEFORE 1c and its
		// deny text must win — the caller gate must not rewrite an
		// existing constitution deny.
		{
			name:         "constitution 1b deny wins over caller gate",
			constitution: voiceEmp,
			allowed:      []string{"shell_execute"},
			tool:         "shell_execute",
			details:      callerInfoDetails(false, false, map[string]string{"tool_name": "shell_execute"}),
			wantAllow:    false,
			wantContains: "tools_allowed",
		},
		// Constitution allow + caller allowed-list pass.
		{
			name:         "voice employee message tool unapproved caller passes",
			constitution: voiceEmp,
			allowed:      callerDefaultAllowedTools,
			tool:         "send_agent_message",
			details:      callerInfoDetails(false, false, map[string]string{"tool_name": "send_agent_message"}),
			wantAllow:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newCallerGateChecker(t, tt.constitution, tt.allowed)
			dec := p.Check(tt.tool, tt.tool, tt.details)
			if dec.Allowed != tt.wantAllow {
				t.Fatalf("Allowed = %v, want %v (reason: %q)", dec.Allowed, tt.wantAllow, dec.Reason)
			}
			if tt.wantReason != "" && dec.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want exactly %q", dec.Reason, tt.wantReason)
			}
			if !tt.wantAllow && tt.wantReason != "" && !strings.Contains(dec.Reason, callerDenyText) {
				t.Errorf("deny Reason %q must contain the pinned model-facing text verbatim", dec.Reason)
			}
			if tt.wantContains != "" && !strings.Contains(dec.Reason, tt.wantContains) {
				t.Errorf("Reason = %q, want substring %q", dec.Reason, tt.wantContains)
			}
			if !tt.wantAllow && tt.wantReason != "" {
				// deny shape: warning severity, no plan routing.
				if dec.Severity != string(SeverityWarning) {
					t.Errorf("Severity = %q, want %q", dec.Severity, SeverityWarning)
				}
				if dec.RequiresPlan {
					t.Error("caller deny must not RequiresPlan")
				}
				if len(dec.EscalateTo) != 0 {
					t.Errorf("EscalateTo = %v, want empty", dec.EscalateTo)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// §2 — golden wrapper: with NO caller keys in details, results must be
// byte-identical to the pre-1c behavior. Re-runs the existing
// TestPreExecChecker_Check fixture rows (testConstitution()/budgetStub/
// pauseTracker from enforcement_test.go — rows NOT edited, only replayed)
// twice: once through the legacy no-options constructor (the 1c-free
// baseline), once through the caller-gate constructor with no caller keys
// plus one approved-caller variant. All three must agree, decision-for-
// decision, via reflect.DeepEqual on the full Decision.
// ---------------------------------------------------------------------------

// goldenRow replays a representative slice of the enforcement truth table
// — one row per rule branch (1a, 1b, allow, never, escalation, risk,
// budget, nil-constitution, inherit-all).
type goldenRow struct {
	name    string
	action  string
	tool    string
	details map[string]string
}

func callerGoldenRows() []goldenRow {
	return []goldenRow{
		{"forbidden tool", "execute", "git_push", nil},
		{"allowed tool", "fetch", "web_fetch", nil},
		{"not in tools_allowed", "custom", "unknown_tool", nil},
		{"never rule", "run", "shell_execute", map[string]string{"command": "merge to main"}},
		{"risk over ceiling", "delete", "file_read", map[string]string{"risk_level": "critical"}},
		{"budget exhausted", "fetch", "web_fetch", nil},
		{"turn tokens", "fetch", "web_fetch", map[string]string{"turn_tokens": "9000"}},
		{"plain allow", "fetch", "web_fetch", map[string]string{"note": "hi"}},
	}
}

func TestCallerGate_NoCallerGoldenIdentical(t *testing.T) {
	constitutionFor := func() *Constitution { return testConstitution() }

	decisionsFor := func(newChecker func() *PreExecChecker, decorate func(map[string]string) map[string]string) map[string]Decision {
		out := map[string]Decision{}
		for _, row := range callerGoldenRows() {
			p := newChecker()
			// budget row needs the stub wired like the existing table test.
			if row.name == "budget exhausted" {
				p.SetBudgetChecker(budgetStub{cents: 50})
			}
			details := row.details
			if details == nil {
				details = map[string]string{}
			}
			if decorate != nil {
				details = decorate(details)
			}
			out[row.name] = p.Check(row.action, row.tool, details)
		}
		return out
	}

	// Baseline: the constructor as it exists TODAY (no caller gate possible —
	// pre-1c behavior).
	baseline := decisionsFor(func() *PreExecChecker {
		return NewPreExecChecker("emp-golden", constitutionFor())
	}, nil)

	// Variant A: caller-gate constructor, NO caller keys in details (the
	// non-voice path). Must be byte-identical to baseline.
	withGate := decisionsFor(func() *PreExecChecker {
		return newCallerGateChecker(t, constitutionFor(), callerDefaultAllowedTools)
	}, func(d map[string]string) map[string]string { return d })

	if !reflect.DeepEqual(baseline, withGate) {
		for name, want := range baseline {
			got := withGate[name]
			if !reflect.DeepEqual(got, want) {
				t.Errorf("no-caller golden drift %q: got %+v want %+v", name, got, want)
			}
		}
	}

	// Variant B: approved caller stamped — also byte-identical (1c inert
	// when approved).
	approved := decisionsFor(func() *PreExecChecker {
		return newCallerGateChecker(t, constitutionFor(), callerDefaultAllowedTools)
	}, func(d map[string]string) map[string]string {
		return callerInfoDetails(true, true, d)
	})
	if !reflect.DeepEqual(baseline, approved) {
		for name, want := range baseline {
			got := approved[name]
			if !reflect.DeepEqual(got, want) {
				t.Errorf("approved-caller golden drift %q: got %+v want %+v", name, got, want)
			}
		}
	}
}

// TestCallerGate_NilConstitutionStaysInert guards the c==nil early-allow:
// no constitution, caller present, deny impossible (answer-all + no
// constraints).
func TestCallerGate_NilConstitutionStaysInert(t *testing.T) {
	p := newCallerGateChecker(t, nil, callerDefaultAllowedTools)
	dec := p.Check("run", "shell_execute", callerInfoDetails(false, false, nil))
	if !dec.Allowed {
		t.Errorf("nil constitution must allow-all regardless of caller: %+v", dec)
	}
}

// ---------------------------------------------------------------------------
// §3 — a caller deny does NOT auto-pause (contrast never[] which does).
// Asserts against the same state the Never step mutates: the AutoPauseFunc
// (pauseTracker from enforcement_test.go) — the exact seam the existing
// wantPause rows use.
// ---------------------------------------------------------------------------

func TestCallerGate_DenyDoesNotAutoPause(t *testing.T) {
	pause := &pauseTracker{}
	p := newCallerGateChecker(t, permissiveConstitution(), callerDefaultAllowedTools)
	p.SetAutoPause(pause.fn())

	dec := p.Check("run", "shell_execute", callerInfoDetails(false, false, nil))
	if dec.Allowed {
		t.Fatal("expected caller deny")
	}
	if pause.called {
		t.Errorf("caller deny must NOT auto-pause (pause called: emp=%q reason=%q)", pause.empID, pause.reason)
	}

	// Control: with the SAME wiring, a never-rule violation DOES pause —
	// proving the seam would have caught a 1c auto-pause (mirrors the
	// existing wantPause=true rows).
	p2 := newCallerGateChecker(t, permissiveConstitution(), callerDefaultAllowedTools)
	p2.SetConstitution(func() *Constitution {
		c := permissiveConstitution()
		c.Constraints.Never = []string{"merge to main"}
		return c
	}())
	pause2 := &pauseTracker{}
	p2.SetAutoPause(pause2.fn())
	dec2 := p2.Check("run", "shell_execute", map[string]string{"command": "merge to main"})
	if dec2.Allowed || !pause2.called {
		t.Fatalf("never-rule control broken: allowed=%v pause=%v", dec2.Allowed, pause2.called)
	}
}

// ---------------------------------------------------------------------------
// §4 — audit seam: exactly one voice.unapproved_action finding per deny,
// none per allow, nil audit writer survives (no panic, deny still returned).
// ---------------------------------------------------------------------------

func TestCallerGate_AuditOneRowPerDeny(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	p := newCallerGateChecker(t, permissiveConstitution(), callerDefaultAllowedTools)
	p.SetAuditStore(store)

	// allow: no row.
	if dec := p.Check("note", "ledger_append", callerInfoDetails(false, false,
		map[string]string{"conversation_id": "cc-allow"})); !dec.Allowed {
		t.Fatalf("expected allow under defaults, got %+v", dec)
	}
	findings, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-caller"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("allow must not write audit rows, got %d", len(findings))
	}

	// deny #1: exactly one row.
	details := callerInfoDetails(false, false, map[string]string{"conversation_id": "cc-99"})
	if dec := p.Check("run", "shell_execute", details); dec.Allowed {
		t.Fatalf("expected deny, got %+v", dec)
	}
	findings, err = store.List(context.Background(), AuditListFilter{EmployeeID: "emp-caller"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("one deny must write exactly one finding, got %d", len(findings))
	}
	f := findings[0]
	if f.ViolatedRule != callerNotApprovedMark && f.ViolatedRule != callerAuditType {
		// The marker must be identifiable as the unapproved-caller event:
		// either the reason token or the frozen audit type string.
		t.Errorf("ViolatedRule = %q, want %q or %q", f.ViolatedRule, callerNotApprovedMark, callerAuditType)
	}
	if !strings.Contains(f.Evidence, "shell_execute") {
		t.Errorf("Evidence %q must carry the tool name", f.Evidence)
	}
	if !strings.Contains(f.Evidence, "cc-99") {
		t.Errorf("Evidence %q must carry the conversation id (ccid)", f.Evidence)
	}

	// deny #2: second row (one per deny, no dedupe/coalescing).
	if dec := p.Check("delete", "file_write", callerInfoDetails(false, true, map[string]string{"conversation_id": "cc-100"})); dec.Allowed {
		t.Fatalf("expected deny #2, got %+v", dec)
	}
	findings, err = store.List(context.Background(), AuditListFilter{EmployeeID: "emp-caller"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(findings) != 2 {
		t.Errorf("two denies must write two findings, got %d", len(findings))
	}
}

func TestCallerGate_NilAuditWriterSurvives(t *testing.T) {
	// No SetAuditStore call at all: auditStore stays nil (the checker's
	// existing nil-writer posture via recordDenial). Deny must still be
	// returned and nothing may panic.
	p := newCallerGateChecker(t, permissiveConstitution(), callerDefaultAllowedTools)
	dec := p.Check("run", "shell_execute", callerInfoDetails(false, false, nil))
	if dec.Allowed {
		t.Fatalf("expected deny with nil audit writer, got %+v", dec)
	}
	if dec.Reason != callerDenyReason {
		t.Errorf("Reason = %q, want pinned %q", dec.Reason, callerDenyReason)
	}
}

func TestCallerGate_OptionNilListNormalizesToDefault(t *testing.T) {
	// The gate must fall back to the shipped defaults when constructed
	// without the option, so a wiring miss never revokes message rights
	// (O3 intent: unknown callers keep the message lane).
	p := NewPreExecChecker("emp-caller", permissiveConstitution())
	dec := p.Check("note", "ledger_append", callerInfoDetails(false, false, nil))
	if !dec.Allowed {
		t.Errorf("built-in default allow-list must admit ledger_append when option omitted: %+v", dec)
	}
	dec = p.Check("run", "shell_execute", callerInfoDetails(false, false, nil))
	if dec.Allowed {
		t.Error("built-in defaults must still deny shell_execute")
	}
}
