package daemon

// A6-12 pin: the contact chain seam honors the CALLER's context.
// ContactChainWiring.RunCtx layers the 10-minute budget over the caller
// ctx (the caller's earlier deadline wins); Run keeps the historical
// background-ctx shim. Cancellation mid-chain must stop further channel
// attempts (the ChainRunner pre-checks ctx.Err() per channel and the hop
// delay is sleepCtx-bound — verified by reading internal/contact/chain.go).

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/contact"
)

// TestContactRunCtxCancelStopsChain pins the forward direction: a ctx
// cancelled during the FIRST channel attempt stops the chain — channels
// 2 and 3 are never attempted.
func TestContactRunCtxCancelStopsChain(t *testing.T) {
	store, err := contact.NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("contact store: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Put(context.Background(), contact.Record{
		ID: "p-1", DisplayName: "probe", Phone: "+15550000111",
		Policy: []contact.Channel{contact.ChannelTelnyxVoice, contact.ChannelSignalMsg, contact.ChannelTelnyxSMS},
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var calls [3]int
	var firstDone bool
	mk := func(i int, ch contact.Channel) contact.ChannelAttempt {
		return &cancelProbe{idx: i, calls: &calls, cancel: cancel, firstDone: &firstDone, ch: ch}
	}
	attempts := map[contact.Channel]contact.ChannelAttempt{
		contact.ChannelTelnyxVoice: mk(0, contact.ChannelTelnyxVoice),
		contact.ChannelSignalMsg:   mk(1, contact.ChannelSignalMsg),
		contact.ChannelTelnyxSMS:   mk(2, contact.ChannelTelnyxSMS),
	}
	runner := contact.NewChainRunner(attempts, nil).WithLogger(slog.New(slog.DiscardHandler))
	w := &ContactChainWiring{Store: store, Runner: runner}
	w.SetAllowedNumbersForTest([]string{"+15550000111"})

	report := w.RunCtx(ctx, "p-1", "hi", nil)
	// Exactly the first channel attempted; 2 and 3 stopped by the cancel.
	if calls[0] != 1 {
		t.Fatalf("channel 1 attempts = %d, want 1", calls[0])
	}
	if calls[1] != 0 || calls[2] != 0 {
		t.Fatalf("A6-12 regression: channels after the mid-chain cancel were attempted: %v", calls)
	}
	if strings.Contains(report, string(contact.ChannelSignalMsg)) || strings.Contains(report, string(contact.ChannelTelnyxSMS)) {
		t.Fatalf("cancelled chain rendered later channels: %q", report)
	}
}

// cancelProbe counts attempts and cancels the shared ctx once (during
// the first attempt on channel index 0).
type cancelProbe struct {
	ch        contact.Channel
	idx       int
	calls     *[3]int
	cancel    context.CancelFunc
	firstDone *bool
}

func (a *cancelProbe) Channel() contact.Channel { return a.ch }

func (a *cancelProbe) Attempt(_ context.Context, _ contact.Record, _ string) (string, error) {
	a.calls[a.idx]++
	if a.idx == 0 && !*a.firstDone {
		*a.firstDone = true
		a.cancel()
		time.Sleep(20 * time.Millisecond)
	}
	return "no_answer", nil
}

// TestContactRunKeepsBudgetCap pins both directions of the cap rule:
//   - plain Run mints its own 10-minute cap (the historical shim): the
//     attempt sees a deadline ~10 minutes out and the FULL chain runs
//     (no caller ctx exists to cancel it).
//   - RunCtx over a caller ctx with a SHORTER deadline keeps the caller's
//     earlier bound (min, not 10m).
func TestContactRunKeepsBudgetCap(t *testing.T) {
	mkWiring := func(t *testing.T, deadline *time.Time) (*ContactChainWiring, *[3]int) {
		t.Helper()
		store, err := contact.NewStore(filepath.Join(t.TempDir(), "contacts.db"))
		if err != nil {
			t.Fatalf("contact store: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if err := store.Put(context.Background(), contact.Record{
			ID: "p-1", DisplayName: "probe", Phone: "+15550000111",
			Policy: []contact.Channel{contact.ChannelTelnyxVoice, contact.ChannelSignalMsg},
		}); err != nil {
			t.Fatalf("seed record: %v", err)
		}
		var calls [3]int
		attempts := map[contact.Channel]contact.ChannelAttempt{
			contact.ChannelTelnyxVoice: &deadlineProbe{ch: contact.ChannelTelnyxVoice, calls: &calls, deadline: deadline, idx: 0},
			contact.ChannelSignalMsg:   &deadlineProbe{ch: contact.ChannelSignalMsg, calls: &calls, idx: 1},
		}
		runner := contact.NewChainRunner(attempts, nil).WithLogger(slog.New(slog.DiscardHandler))
		w := &ContactChainWiring{Store: store, Runner: runner}
		w.SetAllowedNumbersForTest([]string{"+15550000111"})
		return w, &calls
	}

	// Direction 1: plain Run — background ctx, ~10-minute cap, full chain.
	var dl time.Time
	w, calls := mkWiring(t, &dl)
	report := w.Run("p-1", "hi", nil)
	if !dl.IsZero() {
		until := time.Until(dl)
		if until < 9*time.Minute || until > 10*time.Minute+time.Second {
			t.Fatalf("plain Run ctx deadline = %v out, want the 10-minute cap", until)
		}
	} else {
		t.Fatal("plain Run attempt saw no deadline: Run no longer caps its own background ctx (A6-12 shim regression)")
	}
	if (*calls)[0] != 1 || (*calls)[1] != 1 {
		t.Fatalf("plain Run chain attempts = %v, want both channels (full fallback under the cap)", *calls)
	}
	if !strings.Contains(report, "no_answer") {
		t.Fatalf("plain Run report missing per-channel outcomes: %q", report)
	}

	// Direction 2: RunCtx with a shorter caller deadline wins.
	w2, _ := mkWiring(t, &dl)
	dl = time.Time{}
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	w2.RunCtx(cctx, "p-1", "hi", nil)
	if dl.IsZero() {
		t.Fatal("RunCtx attempt saw no deadline")
	}
	if until := time.Until(dl); until > 5*time.Minute+time.Second || until < 4*time.Minute {
		t.Fatalf("RunCtx deadline = %v out, want the caller's EARLIER 5m bound (10m cap must not widen it)", until)
	}
}

// deadlineProbe records the first attempt's ctx deadline and completes.
type deadlineProbe struct {
	ch       contact.Channel
	calls    *[3]int
	deadline *time.Time
	idx      int
}

func (a *deadlineProbe) Channel() contact.Channel { return a.ch }

func (a *deadlineProbe) Attempt(ctx context.Context, _ contact.Record, _ string) (string, error) {
	a.calls[a.idx]++
	if a.idx == 0 && a.deadline != nil {
		if dl, ok := ctx.Deadline(); ok {
			*a.deadline = dl
		} else {
			*a.deadline = time.Time{}
		}
	}
	return "no_answer", nil
}
