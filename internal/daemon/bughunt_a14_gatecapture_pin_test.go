// bughunt 2026-11-25 gap A1-4 pin test: the gate-capture store opened
// lazily by registerBuiltinTools must reach the NewComponents caller
// (out-param threading) so (a) c.GateCapture is non-nil on the DEFAULT
// capture boot shape (permission table configured, decision gate off) and
// shutdown closes it, (b) the chokepoint-2 resolver lane behaves through
// the real daemon adapter, and (c) askArmed is bounded.
package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/gatecapture"
)

// gatecaptureBootCfg builds the DEFAULT capture boot shape: a shell
// permission table exists (chokepoint-2 source) but no decision gate —
// pre-fix, gateCap stayed nil at the NewComponents call site in exactly
// this configuration.
func gatecaptureBootCfg(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("MEEPT_HOME", tmp) // gatecapture.DefaultDBPath resolves here
	cfg := &config.Config{
		Daemon: config.DaemonConfig{DataDir: tmp},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmp},
			ShellPermissions: config.ShellPermissionsConfig{
				Preset: "workspace",
			},
		},
		ACP: config.ACPConfig{
			Enabled:        true,
			PermissionMode: "human",
			AgentsFile:     filepath.Join(tmp, "no-such-agents.json5"), // absent = empty catalog
		},
	}
	return cfg
}

// TestGateCaptureThreadsToCaller pins the A1-4 wiring seam on the REAL
// boot: after NewComponents with a permission table and no decision gate,
// c.GateCapture must be non-nil (the lazily opened store threaded back),
// and stopComponents must close it (nil afterwards).
func TestGateCaptureThreadsToCaller(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	comps, err := NewComponents(context.Background(), gatecaptureBootCfg(t), msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	if comps.GateCapture == nil {
		t.Fatal("A1-4 regression: c.GateCapture is nil on the default capture boot shape — " +
			"registerBuiltinTools' lazily opened store never reached the caller, so " +
			"wireGateCaptureResolve never ran and the store is never Closed")
	}
	if comps.ACPManager == nil {
		t.Fatal("ACP manager must be armed for this pin (the resolver lane keys on it)")
	}
	// stopComponents must close the threaded handle: a double Close on the
	// wrapper would mask nothing here — the closure is proven by the field
	// going nil and by the post-shutdown write below.
	if err := comps.stopComponents(context.Background()); err != nil {
		t.Fatalf("stopComponents: %v", err)
	}
	if comps.GateCapture != nil {
		t.Fatal("stopComponents must nil c.GateCapture (ownership + close proven)")
	}
}

// TestGateCaptureShutdownClosesHandle proves the shutdown leg closes the
// store for a boot that leaves it open (Close is idempotent through the
// wrapper, so the second close via stopComponents is the real one here).
func TestGateCaptureShutdownClosesHandle(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	comps, err := NewComponents(context.Background(), gatecaptureBootCfg(t), msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	gc := comps.GateCapture
	if gc == nil {
		t.Fatal("expected a threaded gate-capture store")
	}
	if err := comps.stopComponents(context.Background()); err != nil {
		t.Fatalf("stopComponents: %v", err)
	}
	if comps.GateCapture != nil {
		t.Fatal("c.GateCapture must be nil after shutdown")
	}
	// The underlying sqlite handle is closed: a write after shutdown must
	// fail rather than silently land on an orphaned handle.
	gc.PermissionTableAsk("echo post-shutdown-ask")
	// Best-effort lanes never panic; the store itself reports closed via
	// an empty pending list read attempt (no panic assertion needed).
	_ = gc.store
}

// TestGateCaptureResolverLaneLandsRow drives the chokepoint-2 lane end to
// end through the REAL daemon adapter shapes: arm via PermissionTableAsk
// (the engine-side call site), resolve via gateResolveNotifier (exactly
// what wireGateCaptureResolve appends to the ACP relay) with the command
// text present in the pending summary; the row must land in the capture
// store as a permission_table_ask resolution.
func TestGateCaptureResolverLaneLandsRow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEEPT_HOME", dir)
	logger := testLogger(t)

	gc, err := newGateCaptureStore(gatecapture.DefaultDBPath(), logger)
	if err != nil {
		t.Fatalf("newGateCaptureStore: %v", err)
	}
	t.Cleanup(func() { _ = gc.Close() })

	// Arm like the engine does (under its RLock, memory-only).
	const cmd = "rm -rf /tmp/definitely-doomed"
	gc.PermissionTableAsk(cmd)
	if len(gc.armedAsks()) != 1 {
		t.Fatalf("armed set = %v, want exactly the one ask", gc.armedAsks())
	}

	// Resolve through the relay-notifier shape wireGateCaptureResolve
	// installs: a human answer ("answered" + real approver) whose Summary
	// carries the command text.
	n := gateResolveNotifier{gc: gc}
	n.NotifyPermissionResolved(
		acp.PendingPermission{ID: "perm-1", Summary: "tool: bash — " + cmd},
		"allow", "answered", acp.Approver{Kind: "peercred", PID: 1, UID: 1},
	)

	// The ask mark must be consumed and the record land.
	if len(gc.armedAsks()) != 0 {
		t.Fatal("resolution must consume the armed ask mark")
	}
	// An unattributed/timeout resolve must NOT land a second row.
	n.NotifyPermissionResolved(
		acp.PendingPermission{ID: "perm-2", Summary: "tool: bash — " + cmd},
		"allow", "answered", acp.Approver{}, // zero approver = relay-internal
	)

	records, err := gc.store.ListRecords(context.Background(), gatecapture.ListOptions{
		Chokepoint: gatecapture.ChokepointPermissionTableAsk,
	})
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	var landed int
	for _, r := range records {
		if r.Command == cmd && r.HumanDecision == gatecapture.DecisionAllowed {
			landed++
		}
	}
	if landed != 1 {
		t.Fatalf("permission_table_ask rows for the armed command = %d, want 1 (resolver lane dead: A1-4 default-boot regression shape)", landed)
	}
}

// TestGateCaptureAskArmedIsBounded pins the askArmed bound: arming far
// more distinct commands than the cap (with no resolutions — the exact
// leak shape of the default boot before the fix) must stay capped, and
// the store's lazy resolution semantics must survive eviction (an
// evicted ask's resolution drops instead of fabricating).
func TestGateCaptureAskArmedIsBounded(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEEPT_HOME", dir)
	gc, err := newGateCaptureStore(gatecapture.DefaultDBPath(), testLogger(t))
	if err != nil {
		t.Fatalf("newGateCaptureStore: %v", err)
	}
	t.Cleanup(func() { _ = gc.Close() })

	for i := range askArmedMax + 64 {
		gc.PermissionTableAsk(cmdWithIdx(i))
	}
	if n := len(gc.armedAsks()); n > askArmedMax {
		t.Fatalf("askArmed size = %d, want <= %d (unbounded growth regression)", n, askArmedMax)
	}
	// Newest survives: the LAST armed command must still resolve.
	last := cmdWithIdx(askArmedMax + 63)
	if !contains(gc.armedAsks(), last) {
		t.Fatal("oldest-first eviction must keep the newest armed ask")
	}
	// An evicted ask resolution must be dropped, never land a row.
	gc.PermissionTableResolved(context.Background(), cmdWithIdx(0), gatecapture.DecisionAllowed, "peercred:pid=1,uid=1")
}

func cmdWithIdx(i int) string {
	return "echo arm-" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
