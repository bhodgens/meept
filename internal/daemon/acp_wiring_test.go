package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// stubACPDecisioner is the daemon-side Decisioner stub for wiring tests
// (acp-permission-decision leaf 03). Ask is never called through this seam
// here — wiring assertions check WHETHER the setter was applied, not lane
// behavior (lane behavior is pinned in internal/acp + e2e/suites/acp-wire).
type stubACPDecisioner struct{ available bool }

func (s stubACPDecisioner) Available() bool { return s.available }

func (s stubACPDecisioner) Ask(_ context.Context, _ llm.DecisionQuestion) (*llm.DecisionResult, error) {
	return &llm.DecisionResult{Answer: "allow", Confidence: 1.0}, nil
}

var _ acp.Decisioner = stubACPDecisioner{}

func TestApplyACPFromConfig_DisabledDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.ACP.Enabled {
		t.Fatal("DefaultConfig ACP.Enabled = true, want false")
	}

	mgr, err := applyACPFromConfig(cfg.ACP, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(default) error = %v, want nil", err)
	}
	if mgr != nil {
		t.Fatal("applyACPFromConfig(default) manager = non-nil, want nil")
	}
}

func TestApplyACPFromConfig_DisabledEmitsNoACPLogs(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP, stubACPDecisioner{available: true})
	if err != nil || mgr != nil {
		t.Fatalf("applyACPFromConfig(default) = (%v, %v), want (nil, nil)", mgr, err)
	}
	if strings.Contains(strings.ToLower(buf.String()), "acp") {
		t.Fatalf("disabled path logged acp: %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// Decision-lane wiring (acp-permission-decision leaf 03, Contract C)
// ---------------------------------------------------------------------------

// acpEnabledConfig is an enabled [acp] section with an isolated (missing)
// catalog file — empty catalog, no subprocess, no side effects.
func acpEnabledConfig(t *testing.T, mode string) config.ACPConfig {
	t.Helper()
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")
	cfg.PermissionMode = mode
	return cfg
}

// TestApplyACPFromConfig_DecisionModeAppliesDecider pins Contract C:
// permission_mode "decision" + non-nil Decisioner → the manager carries
// the decider (setter applied).
func TestApplyACPFromConfig_DecisionModeAppliesDecider(t *testing.T) {
	cfg := acpEnabledConfig(t, "decision")
	dec := stubACPDecisioner{available: true}

	mgr, err := applyACPFromConfig(cfg, dec)
	if err != nil {
		t.Fatalf("applyACPFromConfig(decision) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(decision) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if !mgr.HasDecisionClient() {
		t.Fatal("decision mode: manager.HasDecisionClient() = false, want true (SetDecisionClient not applied)")
	}
}

// TestApplyACPFromConfig_PermissiveModeIgnoresDecider pins the mode gate:
// a Decisioner passed with mode "permissive" must NOT arm the lane —
// permissive/deny behavior stays byte-identical (master.md decision 1).
func TestApplyACPFromConfig_PermissiveModeIgnoresDecider(t *testing.T) {
	cfg := acpEnabledConfig(t, "permissive")

	mgr, err := applyACPFromConfig(cfg, stubACPDecisioner{available: true})
	if err != nil {
		t.Fatalf("applyACPFromConfig(permissive) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(permissive) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if mgr.HasDecisionClient() {
		t.Fatal("permissive mode: manager has a decision client, want none")
	}
}

// TestApplyACPFromConfig_DecisionModeNilDeciderAllowed pins fail-closed
// layering: decision mode with a nil decider is NOT an error at the daemon
// wiring layer — the manager keeps no client and sessions fail closed with
// no_decisioner (pinned by leaf 02 tests).
func TestApplyACPFromConfig_DecisionModeNilDeciderAllowed(t *testing.T) {
	cfg := acpEnabledConfig(t, "decision")

	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(decision, nil dec) error = %v, want nil (manager fails closed downstream)", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(decision, nil dec) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if mgr.HasDecisionClient() {
		t.Fatal("nil decider must leave the manager without a decision client")
	}
}

func TestApplyACPFromConfig_EnabledMissingCatalog(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")

	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(enabled, missing catalog) error = %v, want nil (empty catalog)", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(enabled, missing catalog) manager = nil, want non-nil")
	}
	t.Cleanup(mgr.StopAll)

	if !mgr.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}
	if agents := mgr.Agents(); len(agents) != 0 {
		t.Fatalf("Agents() = %+v, want empty", agents)
	}
}

func TestRegisterACPTools_NilManagerNoPanic(t *testing.T) {
	reg := tools.NewRegistry(slog.Default())
	RegisterACPTools(reg, nil)
	if names := reg.Names(); len(names) != 0 {
		t.Fatalf("RegisterACPTools(nil mgr) registered %v", names)
	}
}

func TestRegisterACPTools_NilRegistryNoPanic(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")
	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	if mgr != nil {
		t.Cleanup(mgr.StopAll)
	}
	RegisterACPTools(nil, mgr)
}

func TestRegisterACPTools_DisabledNoTools(t *testing.T) {
	reg := tools.NewRegistry(slog.Default())
	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	RegisterACPTools(reg, mgr)
	if names := reg.Names(); len(names) != 0 {
		t.Fatalf("RegisterACPTools(disabled) registered %v, want none", names)
	}
}

func TestRegisterACPTools_EnabledRegistersACPAgent(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")
	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	t.Cleanup(mgr.StopAll)

	reg := tools.NewRegistry(slog.Default())
	RegisterACPTools(reg, mgr)
	names := reg.Names()
	if len(names) != 1 || names[0] != "acp_agent" {
		t.Fatalf("RegisterACPTools(enabled) names = %v, want [acp_agent]", names)
	}
}

func TestApplyACPFromConfig_StopAllIdempotent(t *testing.T) {
	cfg := config.DefaultConfig().ACP
	cfg.Enabled = true
	cfg.AgentsFile = filepath.Join(t.TempDir(), "missing-acp-agents.json5")

	mgr, err := applyACPFromConfig(cfg, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig error = %v", err)
	}
	if mgr == nil {
		t.Fatal("manager is nil")
	}
	mgr.StopAll()
	mgr.StopAll()
}

// ---------------------------------------------------------------------------
// Human relay notifier wiring (acp-human-permission-relay leaf 03,
// Contract C). The bus-backed notifier arms the relay seam for mode
// "human" and for decision mode with decision_escalate_to_human; every
// other config leaves the manager byte-identical (no relay, zero logs).
// ---------------------------------------------------------------------------

// TestApplyACPFromConfig_HumanModeArmsRelay pins Contract C: human mode +
// a bus → the manager carries BOTH relay halves (HasPermissionRelay true).
func TestApplyACPFromConfig_HumanModeArmsRelay(t *testing.T) {
	cfg := acpEnabledConfig(t, "human")
	cfg.RelayTimeout = 30
	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(msgBus.Close)

	mgr, err := applyACPFromConfig(cfg, nil, msgBus)
	if err != nil {
		t.Fatalf("applyACPFromConfig(human) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("applyACPFromConfig(human) manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if !mgr.HasPermissionRelay() {
		t.Fatal("human mode: manager.HasPermissionRelay() = false, want true (relay + notifier not wired)")
	}
}

// TestApplyACPFromConfig_DecisionEscalationArmsRelay pins the escalation
// lane: decision mode + decision_escalate_to_human + a bus → relay armed.
func TestApplyACPFromConfig_DecisionEscalationArmsRelay(t *testing.T) {
	cfg := acpEnabledConfig(t, "decision")
	cfg.DecisionThreshold = 0.5
	cfg.DecisionEscalateToHuman = true
	cfg.RelayTimeout = 30
	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(msgBus.Close)

	mgr, err := applyACPFromConfig(cfg, stubACPDecisioner{available: true}, msgBus)
	if err != nil {
		t.Fatalf("applyACPFromConfig(decision+escalate) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if !mgr.HasPermissionRelay() {
		t.Fatal("decision_escalate_to_human: relay not armed")
	}
	if !mgr.HasDecisionClient() {
		t.Fatal("decision mode must still carry the decider")
	}
}

// TestApplyACPFromConfig_PermissiveModeNoRelay pins the mode gate: the
// same notifier-eligible bus passed with mode "permissive" must NOT arm
// the relay — permissive/deny behavior stays byte-identical.
func TestApplyACPFromConfig_PermissiveModeNoRelay(t *testing.T) {
	cfg := acpEnabledConfig(t, "permissive")
	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(msgBus.Close)

	mgr, err := applyACPFromConfig(cfg, nil, msgBus)
	if err != nil {
		t.Fatalf("applyACPFromConfig(permissive) error = %v", err)
	}
	if mgr == nil {
		t.Fatal("manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if mgr.HasPermissionRelay() {
		t.Fatal("permissive mode: relay armed, want none")
	}
}

// TestApplyACPFromConfig_HumanModeNilBusFailsClosed pins fail-closed
// layering: human mode with no bus (nil msgBus at construction) is NOT an
// error — the manager simply keeps no relay and sessions cancel
// immediately (internal/acp TestSession_HumanNilRelayFailsClosed).
func TestApplyACPFromConfig_HumanModeNilBusFailsClosed(t *testing.T) {
	cfg := acpEnabledConfig(t, "human")

	mgr, err := applyACPFromConfig(cfg, nil, nil)
	if err != nil {
		t.Fatalf("applyACPFromConfig(human, nil bus) error = %v, want nil", err)
	}
	if mgr == nil {
		t.Fatal("manager = nil")
	}
	t.Cleanup(mgr.StopAll)

	if mgr.HasPermissionRelay() {
		t.Fatal("nil bus must leave the manager without a relay (fails closed)")
	}
}

// TestApplyACPFromConfig_DisabledNoRelayZeroLogs pins the zero-log
// disabled contract THROUGH the new bus parameter: nothing wired, nothing
// logged, (nil, nil) returned.
func TestApplyACPFromConfig_DisabledNoRelayZeroLogs(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(msgBus.Close)

	mgr, err := applyACPFromConfig(config.DefaultConfig().ACP, nil, msgBus)
	if err != nil || mgr != nil {
		t.Fatalf("applyACPFromConfig(disabled) = (%v, %v), want (nil, nil)", mgr, err)
	}
	if strings.Contains(strings.ToLower(buf.String()), "acp") {
		t.Fatalf("disabled path logged acp: %q", buf.String())
	}
	if total := msgBus.Stats()["_total"]; total != 0 {
		t.Fatalf("disabled path left %d bus subscriptions", total)
	}
}

// TestACPBusNotifierPublishes pins the notifier itself: requested returns
// true iff a subscriber is listening (PublishT receiver count), and both
// frames carry the frozen payload keys on their topics.
func TestACPBusNotifierPublishes(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(msgBus.Close)

	var gotRequested, gotResolved bus.ACPPermissionEvent
	subA := bus.SubscribeT(msgBus, "test-req", bus.TopicACPPermissionRequested, func(p bus.ACPPermissionEvent) {
		gotRequested = p
	})
	subB := bus.SubscribeT(msgBus, "test-res", bus.TopicACPPermissionResolved, func(p bus.ACPPermissionEvent) {
		gotResolved = p
	})
	t.Cleanup(func() {
		msgBus.Unsubscribe(subA)
		msgBus.Unsubscribe(subB)
	})

	n := &acpBusNotifier{bus: msgBus, logger: slog.Default()}
	p := acp.PendingPermission{
		ID:        "perm-1",
		AgentID:   "codex",
		SessionID: "sess-a",
		Summary:   "tool: bash",
		CreatedAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	}
	if !n.NotifyPermissionRequested(p) {
		t.Fatal("NotifyPermissionRequested with listeners = false, want true")
	}
	n.NotifyPermissionResolved(p, "allow", "answered")

	// SubscribeT delivers on a reader goroutine: poll with a deadline.
	deadline := time.Now().Add(2 * time.Second)
	for (gotRequested.ID == "" || gotResolved.ID == "") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if gotRequested.ID != "perm-1" || gotRequested.AgentID != "codex" ||
		gotRequested.SessionID != "sess-a" || gotRequested.Summary != "tool: bash" {
		t.Fatalf("requested frame = %+v", gotRequested)
	}
	if gotRequested.CreatedAt == "" {
		t.Error("requested frame missing created_at")
	}
	if gotResolved.Decision != "allow" || gotResolved.Reason != "answered" {
		t.Fatalf("resolved frame = %+v", gotResolved)
	}
}

// TestACPBusNotifierNoListenerReturnsFalse pins the reconnect contract's
// notifier half: with no subscribers, NotifyPermissionRequested reports
// false (the session keeps waiting until timeout; a late subscriber can
// still answer via RPC).
func TestACPBusNotifierNoListenerReturnsFalse(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(msgBus.Close)

	n := &acpBusNotifier{bus: msgBus, logger: slog.Default()}
	if n.NotifyPermissionRequested(acp.PendingPermission{ID: "perm-x"}) {
		t.Fatal("no-listener NotifyPermissionRequested = true, want false")
	}
	// Resolved is fire-and-forget (no return); must not panic.
	n.NotifyPermissionResolved(acp.PendingPermission{ID: "perm-x"}, "deny", "timeout")
}

// TestACPBusNotifierNilSafe pins the repo setter/nil guard rule: a zero
// notifier must degrade (false / no panic), never crash the Serve
// goroutine the calls run on.
func TestACPBusNotifierNilSafe(t *testing.T) {
	var n *acpBusNotifier
	if n.NotifyPermissionRequested(acp.PendingPermission{}) {
		t.Error("nil notifier reported a listener")
	}
	n.NotifyPermissionResolved(acp.PendingPermission{}, "deny", "no_relay")
	zero := &acpBusNotifier{}
	if zero.NotifyPermissionRequested(acp.PendingPermission{}) {
		t.Error("zero-value notifier reported a listener")
	}
}
