// gatecapture_wiring.go adapts the leaf-02 capture store onto the two
// chokepoint sink seams (jev-command-gate leaf 02, Contract C4):
//
//   - *builtin.DecisionGateHook.SetGateCapture (chokepoint 1:
//     decision-gate deny -> human resolves) - satisfied directly by
//     *gatecapture.Store's methods.
//   - *intsecurity.Engine.SetGateAskCapture (chokepoint 2: shell
//     permission-table ask -> confirmation resolution) - arm side
//     satisfied directly (memory-only, safe under the engine's RLock);
//     the ask RESOLUTION rides the ACP permission-relay resolve path,
//     where the relay's pending Summary carries the command via this
//     adapter's bound engine.
//
// internal/daemon is the only package that imports both sides, so the
// adaptation lives here (mirrors acp_notifier.go).
package daemon

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/gatecapture"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security"
)

// gateAskResolver converts an ACP permission-resolve decision verb
// ("allow"/"always"/"deny") into the C4 human-decision vocabulary.
func gateAskDecision(verb string) string {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case "allow", "always":
		return gatecapture.DecisionAllowed
	default:
		return gatecapture.DecisionDenied
	}
}

// gateCaptureStore wraps *gatecapture.Store with the daemon-side ask
// RESOLUTION lane. The arm lanes (deny, permission-table ask) are the
// store's own methods, called at the chokepoints; the resolution lane is
// wired here because only the daemon knows which confirmation-flow answer
// corresponds to which armed pending.
type gateCaptureStore struct {
	store *gatecapture.Store

	// askArmed tracks permission-table asks awaiting resolution, keyed by
	// command text. The store keeps its own pending map for prior-score
	// pairing; this set gates which resolutions CONSUME an ask (so a
	// decision-gate approval never lands a permission_table_ask row).
	askArmed sync.Map // map[string]struct{}

	logger *slog.Logger
}

// newGateCaptureStore opens the capture store at path (beside the shadow
// store) when capture is enabled. A nil store + nil error means "disabled"
// - callers skip wiring entirely.
func newGateCaptureStore(path string, logger *slog.Logger) (*gateCaptureStore, error) {
	store, err := gatecapture.OpenStore(path)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &gateCaptureStore{store: store, logger: logger.With("component", "gatecapture")}, nil
}

// DecisionGateDeny arms the chokepoint-1 pending (satisfies the
// builtin.gateCapture seam together with DecisionGateResolved).
func (g *gateCaptureStore) DecisionGateDeny(ctx context.Context, command string) {
	g.store.DecisionGateDeny(ctx, command)
}

// DecisionGateResolved lands the chokepoint-1 record.
func (g *gateCaptureStore) DecisionGateResolved(ctx context.Context, command, decision, approver string) {
	g.store.DecisionGateResolved(ctx, command, decision, approver)
}

// RecordScore arms the jev score-gate lane: satisfies the jevgate.Recorder
// seam (leaf-01 capture). The underlying store's RecordScore is
// nil-tolerant and only arms on escalations; a clean allow records nothing.
func (g *gateCaptureStore) RecordScore(ctx context.Context, command, serializedContext string, result *llm.CommandScoreResult, escalated bool, reason string) {
	g.store.RecordScore(ctx, command, serializedContext, result, escalated, reason)
}

// Escalated satisfies the jevgate.ConfirmFlow seam: the score gate's deny
// is NOT a terminal deny - the agent loop converts Block=true into the
// phase-1 confirmation shape - so the only notification needed here is the
// escalation marker, and the arm already happened via RecordScore. Kept as
// a debug log so the capture store can be passed directly as the hook's
// ConfirmFlow without a separate stub type.
func (g *gateCaptureStore) Escalated(ctx context.Context, command, reason string) {
	if g == nil || g.logger == nil {
		return
	}
	g.logger.Debug("gatecapture: jev score gate escalated", "reason", reason, "command", command)
}

// PermissionTableAsk arms the chokepoint-2 pending. MEMORY-ONLY (the
// security engine calls this under its RLock; the store's arm path is a
// mutex-guarded map insert that never touches sqlite).
func (g *gateCaptureStore) PermissionTableAsk(command string) {
	g.askArmed.Store(command, struct{}{})
	g.store.PermissionTableAsk(command)
}

// PermissionTableResolved lands the chokepoint-2 record (called from the
// ACP permission resolve path, OUTSIDE any engine lock). Exactly one
// record per armed ask: the armed mark is consumed first; an unmarked
// resolution (store restarted, ask raced an eviction) is dropped rather
// than fabricating a context.
func (g *gateCaptureStore) PermissionTableResolved(ctx context.Context, command, decision, approver string) {
	if _, ok := g.askArmed.LoadAndDelete(command); !ok {
		g.logger.Debug("gatecapture: ask resolution without an armed ask; dropped",
			"decision", decision)
		return
	}
	g.store.PermissionTableResolved(ctx, command, decision, approver)
}

// Close closes the underlying store.
func (g *gateCaptureStore) Close() error {
	if g == nil || g.store == nil {
		return nil
	}
	return g.store.Close()
}

// wireGateAskResolution resolves the chokepoint-2 lane: called on the ACP
// permission-resolve path with the pending request's rendered summary, it
// matches armed asks whose command text appears in the summary and lands
// the record. Best-effort and bounded (no locks held across store I/O;
// never fails the resolve).
func (g *gateCaptureStore) wireGateAskResolution(ctx context.Context, summary, decisionVerb, approver string) {
	if g == nil || g.store == nil || summary == "" {
		return
	}
	decision := gateAskDecision(decisionVerb)
	matched := 0
	g.askArmed.Range(func(key, _ any) bool {
		cmd, _ := key.(string)
		if cmd == "" || !strings.Contains(summary, cmd) {
			return true
		}
		matched++
		g.PermissionTableResolved(ctx, cmd, decision, approver)
		return true
	})
	if matched > 0 {
		g.logger.Debug("gatecapture: permission-table ask resolved",
			"decision", decision, "matched", matched)
	}
}

// wireGateCaptureResolve connects the chokepoint-2 resolution lane to the
// ACP permission-resolve path: the manager wraps every human relay resolve
// through this notifier seam, so the wrapped notifier sees each pending
// request (whose Summary carries the command text) plus the terminal
// decision. Matching is best-effort and bounded; a miss drops silently.
func wireGateCaptureResolve(mgr *acp.Manager, gc *gateCaptureStore) {
	if mgr == nil || gc == nil {
		return
	}
	mgr.AppendPermissionNotifier(gateResolveNotifier{gc: gc})
}

// gateResolveNotifier rides the relay's notifier fanout purely to OBSERVE
// resolutions (it never publishes anything).
type gateResolveNotifier struct {
	gc *gateCaptureStore
}

func (n gateResolveNotifier) NotifyPermissionRequested(p acp.PendingPermission) bool {
	return false // observe-only
}

func (n gateResolveNotifier) NotifyPermissionResolved(p acp.PendingPermission, decision, reason string, approver acp.Approver) {
	// Only HUMAN answers carry an approver identity (the relay stamps a
	// zero Approver on timeout/no-relay paths, audit M5); those are the
	// resolutions worth capturing.
	if approver.Kind == "" {
		return
	}
	if reason != "answered" {
		return
	}
	summary := p.Summary
	if summary == "" && len(p.Params) > 0 {
		summary = string(p.Params)
	}
	n.gc.wireGateAskResolution(context.Background(), summary, decision, approver.String())
}

// compile-time seams. builtinGateCapture is the unexported
// builtin.gateCapture interface (decision_gate.go); declared locally to the
// same shape so the assertion compiles without exporting it.
type builtinGateCapture interface {
	DecisionGateDeny(ctx context.Context, command string)
	DecisionGateResolved(ctx context.Context, command, decision, approver string)
}

var (
	_ builtinGateCapture      = (*gateCaptureStore)(nil)
	_ security.GateAskCapture = (*gateCaptureStore)(nil)
	_ acp.PermissionNotifier  = gateResolveNotifier{}
)
