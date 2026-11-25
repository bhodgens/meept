package employee

// Pins for bughunt 2026-11-24 finding L12 (cron fire-time GetBot error
// class split):
//
//   - TestCronTransientGetBotErrorWarnsAndRetriesNextTick (L12 pin iii): a
//     NON-not-found GetBot failure logs a Warn naming bot id + error, does
//     NOT fire, leaves the schedule claims and cron entries INTACT, and the
//     next tick (store healthy again) fires naturally. Failing revert:
//     restore `if err != nil || def == nil { logCronSkip("bot gone") }` —
//     the blip is then logged at DEBUG as "bot gone" with no error text and
//     the Warn assertions fail.
//
//   - TestCronNotfoundStillTakesGonePath (L12 pin iv): a genuine not-found
//     (wrapped sql.ErrNoRows) keeps today's gone path exactly — DEBUG
//     "bot gone", no Warn, no fire — and the fresh-prompt re-read plus the
//     captured-prompt fallback (TestCronFireUsesFreshPromptAfterUpdate) are
//     untouched by the split.

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bot"
)

// newL12Harness builds the cron fixture on an explicit store path so the
// test can create a SECOND bot.Manager over a closed store: same-package
// access to Manager.botManager lets a pin swap the backend to simulate a
// transient store blip without touching any production seam.
func newL12Harness(t *testing.T, botID string) (*bot.Manager, *Manager, *cronFireProbe, *webhookLogCapture) {
	t.Helper()
	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	botMgr := bot.NewManager(store, nil)
	result, err := NewManagerFromConfig(context.Background(), cronTestConfig(t), botMgr, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewManagerFromConfig: %v", err)
	}
	t.Cleanup(result.Manager.StopAll)
	logs := &webhookLogCapture{}
	result.Manager.logger = slog.New(logs)

	probe := &cronFireProbe{}
	loop := NewGoalLoop(botID, &Constitution{Purpose: "sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(probe)
	result.Manager.RegisterGoalLoop(botID, loop)

	ctx := context.Background()
	if err := botMgr.CreateBot(ctx, cronDef(botID, "0 4 * * *", true)); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	return botMgr, result.Manager, probe, logs
}

// TestCronTransientGetBotErrorWarnsAndRetriesNextTick pins L12(iii): a
// store blip is not a deleted bot.
func TestCronTransientGetBotErrorWarnsAndRetriesNextTick(t *testing.T) {
	const botID = "blip-bot"
	healthy, mgr, probe, logs := newL12Harness(t, botID)

	stale := mgr.Cron.Entries()
	if len(stale) != 1 {
		t.Fatalf("cron entries = %d, want 1", len(stale))
	}

	// Simulate the blip: swap in a bot.Manager whose store is closed, so
	// GetBot fails with a NON-sql.ErrNoRows error (transient class).
	blipStore, err := bot.NewStore(filepath.Join(t.TempDir(), "blip.db"))
	if err != nil {
		t.Fatalf("NewStore blip: %v", err)
	}
	if err := blipStore.Close(); err != nil {
		t.Fatalf("close blip store: %v", err)
	}
	mgr.botManager = bot.NewManager(blipStore, nil)

	stale[0].Job.Run()

	if probe.count() != 0 {
		t.Fatalf("transient GetBot failure still fired Trigger (%d calls)", probe.count())
	}
	logged := logs.String()
	if !strings.Contains(logged, "transient bot lookup failure") {
		t.Fatalf("transient blip not logged as a retry-class skip; logs:\n%s", logged)
	}
	if !strings.Contains(logged, "bot_id="+botID) {
		t.Fatalf("transient Warn missing bot id; logs:\n%s", logged)
	}
	if !strings.Contains(logged, "error=") {
		t.Fatalf("transient Warn missing the error text; logs:\n%s", logged)
	}
	if strings.Contains(logged, "bot gone") {
		t.Fatalf("transient blip misclassified as 'bot gone'; logs:\n%s", logged)
	}

	// Schedule claims must be INTACT: no destructive cleanup on a blip.
	if n := len(mgr.Cron.Entries()); n != 1 {
		t.Fatalf("cron entries after blip = %d, want 1 (blip must not release jobs)", n)
	}
	if c := claimsFor(mgr, botID); len(c) != 1 || c[0].schedule != "0 4 * * *" {
		t.Fatalf("cron claims after blip = %+v, want the 0 4 * * * claim intact", c)
	}
	mgr.cronRegsMu.Lock()
	_, regHeld := mgr.cronRegs[cronRegKey(botID, "0 4 * * *")]
	mgr.cronRegsMu.Unlock()
	if !regHeld {
		t.Fatal("dedupe registration claim dropped by a transient blip")
	}

	// Store recovers: the NEXT tick fires naturally — the blip skipped one
	// fire, it did not silently retire the schedule.
	mgr.botManager = healthy
	stale[0].Job.Run()
	if probe.count() != 1 {
		t.Fatalf("after recovery, fires = %d, want 1 (next tick must retry)", probe.count())
	}
}

// TestCronNotfoundStillTakesGonePath pins L12(iv): a real not-found keeps
// the pre-fix gone path byte-for-byte in behavior — DEBUG skip, no Warn, no
// fire, no second registration churn.
func TestCronNotfoundStillTakesGonePath(t *testing.T) {
	const botID = "deleted-l12"
	botMgr, mgr, probe, logs := newL12Harness(t, botID)

	stale := mgr.Cron.Entries()
	if len(stale) != 1 {
		t.Fatalf("cron entries = %d, want 1", len(stale))
	}
	// Delete through the real lifecycle so the row (not the store) is gone:
	// GetBot now returns a wrapped sql.ErrNoRows, the not-found class.
	if err := botMgr.DeleteBot(context.Background(), botID); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	stale[0].Job.Run()

	if probe.count() != 0 {
		t.Fatalf("not-found bot still fired Trigger (%d calls)", probe.count())
	}
	logged := logs.String()
	if !strings.Contains(logged, "cron trigger skipped: bot gone") {
		t.Fatalf("not-found did not take the gone path; logs:\n%s", logged)
	}
	if strings.Contains(logged, "transient bot lookup failure") {
		t.Fatalf("not-found misclassified as transient; logs:\n%s", logged)
	}
	// Gone path claims were already released by the delete hook — unchanged.
	if c := claimsFor(mgr, botID); len(c) != 0 {
		t.Fatalf("claims after delete = %+v, want none (delete hook behavior unchanged)", c)
	}
}
