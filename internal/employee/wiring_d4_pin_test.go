package employee

// Pin test for the bughunt D4 fix: NewManagerFromConfig must return a
// StopAnchor cancel that ends the periodic anchor job (previously the
// job ran on the raw wiring ctx, which no shutdown path cancelled — one
// leaked goroutine per restart). TESTS ONLY.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
)

func TestNewManagerFromConfigReturnsStopAnchor(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "bots.db")
	cfg := &config.Config{
		Employees: config.EmployeesConfig{Enabled: true},
		Bots:      config.BotsConfig{Enabled: false},
		Daemon:    config.DaemonConfig{DataDir: tmp},
	}
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	defer func() { _ = botStore.Close() }()
	botMgr := bot.NewManager(botStore, nil)

	result, err := NewManagerFromConfig(
		context.Background(), cfg, botMgr, botStore,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	// The anchor job started iff the chain store built; StopAnchor must
	// be non-nil exactly then, and cancelling it must end the job. We
	// cannot observe the goroutine directly, but StopAnchor cancel +
	// the Run contract (exits on ctx done) is the pinned shape: verify
	// the cancel is idempotent-safe and the wiring handed it back.
	if result.StopAnchor == nil {
		t.Fatal("StopAnchor is nil: the anchor job runs on an uncancellable ctx (the D4 leak)")
	}
	// Cancel twice: a second call must not panic (context.CancelFunc is
	// idempotent, but pin the shape the stop path relies on).
	result.StopAnchor()
	result.StopAnchor()
	// The exit-on-cancel contract of the job itself is pinned by
	// auditlog's TestAnchorJob_RunExportsAndStopsOnCancel. Here the
	// WIRING pin is: StopAnchor non-nil and callable (the daemon's
	// stopComponents path can end the job).
}
