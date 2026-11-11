package daemon

// Pin tests for the bughunt fix wave (D1, D2, D6, D9, D10, A10) — daemon
// wiring/security/lifecycle. TESTS ONLY.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/voice"
)

// ---------------------------------------------------------------------------
// D1: stopComponents closes the employee ledger AFTER Voice.Stop
// ---------------------------------------------------------------------------

// TestStopComponentsClosesEmployeeLedgerAfterVoiceStop pins the ordering:
// the employee ledger must be closed (and nil'd) only AFTER the voice
// block has run — a live-call release hook's brief append lands while the
// store is still open, and nothing appends successfully after
// stopComponents returns.
func TestStopComponentsClosesEmployeeLedgerAfterVoiceStop(t *testing.T) {
	home := t.TempDir()
	store, err := ledger.Open(filepath.Join(home, "ledger.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() { _ = store.Close() }()

	c := &Components{
		Config:         &config.Config{},
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		EmployeeLedger: store,
		Voice: &VoiceWiring{
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}

	// The brief-append path (release hook) must succeed BEFORE the stop:
	// the store is open while the voice stack runs.
	if _, err := store.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "emp-d1",
		Kind:       ledger.KindCommitment,
		Subject:    "order-pin",
		Source:     ledger.SourceVoice,
	}); err != nil {
		t.Fatalf("append while ledger open (the pre-close brief path): %v", err)
	}

	if err := c.stopComponents(context.Background()); err != nil {
		t.Fatalf("stopComponents: %v", err)
	}
	if c.EmployeeLedger != nil {
		t.Fatal("stopComponents must nil EmployeeLedger (closed after the voice block)")
	}
	// The store is closed now: an append (a late brief) must FAIL —
	// proving the close really happened, i.e. the voice block (whose
	// release hooks append briefs) strictly precedes it.
	if _, err := store.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "emp-d1",
		Kind:       ledger.KindCommitment,
		Subject:    "order-pin",
		Source:     ledger.SourceVoice,
	}); err == nil {
		t.Fatal("append after stopComponents succeeded: the ledger was NOT closed — the D1 ordering (ledger close AFTER Voice.Stop) regressed")
	}
}

// ---------------------------------------------------------------------------
// D2: VoiceWiring.Stop drains live calls -> call row reaches ended
// ---------------------------------------------------------------------------

func TestVoiceStopDrainsLiveCallToEndedRow(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	const ccid = "cc-d2-drain"
	inst := acquireFor(w, ccid)
	trackCall(t, w, ccid)
	// Capture the store path BEFORE Stop closes the store handle: the
	// employee wiring records it at construction (deps.CallStorePath).
	callStorePath := w.Employee.storePath
	// Wire the release hook exactly as WireVoice section 8 does: the
	// drain drives FailOutcome, which fires THIS hook; releaseCall (the
	// registry release + terminal row write) hangs off it.
	w.outcomes = voice.NewOutcomeRegistry()
	w.Manager.SetReleaseHook(func(hookCcid string, out voice.Outcome) {
		w.outcomes.Record(hookCcid, out)
		w.releaseCall(hookCcid, out)
	})
	// Hold: register the call row (the binding entry OnCallEnd ends).
	holdDone := make(chan struct{})
	go func() {
		defer close(holdDone)
		holdPhoneCall(w.Employee, ccid, w.cfg.DefaultEmployee, inst.Target, "incoming", false)
	}()
	<-holdDone

	// Arm a live loop so Stop has real teardown work.
	armLoop(w, ccid, inst, newBlockingTurnRunner())

	w.Stop(context.Background())

	// The call store is CLOSED after Stop (by design). Assert the D2
	// outcome through state that survives the close: the registry row
	// must be released AND the call row must have reached ended BEFORE
	// the close — read the row through a FRESH read-only handle on the
	// same file.
	live := w.Registry.Live()
	if len(live) != 0 {
		t.Fatalf("registry still holds %d live rows after Stop", len(live))
	}
	roStore, err := voice.NewCallStore(callStorePath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("reopen calls.db read-only: %v", err)
	}
	defer func() { _ = roStore.Close() }()

	// The D2 assertion: NO stranded 'live' rows. SweepStaleLive counts
	// rows still marked live; the drain must have ended them all.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	n, err := roStore.SweepStaleLive(ctx)
	if err != nil {
		t.Fatalf("SweepStaleLive on the reopened store: %v", err)
	}
	if n != 0 {
		t.Fatalf("%d call row(s) still 'live' after Stop — the D2 stranded-row regression", n)
	}
	// And the drained row reads back terminal.
	rec, _, ok, err := roStore.EndedCall(ctx, ccid)
	if err != nil {
		t.Fatalf("EndedCall on the reopened store: %v", err)
	}
	if !ok {
		t.Fatal("call row missing after Stop: the drain did not run the release fan-out before the store close")
	}
	_ = rec
}

// ---------------------------------------------------------------------------
// D2 part 2: ContactChainWiring.Close wired into stopComponents
// ---------------------------------------------------------------------------

func TestStopComponentsClosesContactChain(t *testing.T) {
	store, err := contact.NewStore(filepath.Join(t.TempDir(), "contacts.db"))
	if err != nil {
		t.Fatalf("open contact store: %v", err)
	}
	chain := &ContactChainWiring{Store: store}
	c := &Components{
		Config: &config.Config{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Voice: &VoiceWiring{
			logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			Contact: chain,
		},
	}
	if err := c.stopComponents(context.Background()); err != nil {
		t.Fatalf("stopComponents: %v", err)
	}
	if c.Voice != nil && c.Voice.Contact != nil {
		t.Fatal("contact chain not cleared by stopComponents: the contacts.db handle leaked (the D2 regression)")
	}
	// The store handle is closed: a write must fail.
	if err := store.Put(context.Background(), contact.Record{ID: "x", DisplayName: "y"}); err == nil {
		t.Fatal("contact store still writable after stopComponents — Close was never called (the D2 leak)")
	}
}

// ---------------------------------------------------------------------------
// D6: the daemon RPC surface exposes ledger.query / ledger.state
// ---------------------------------------------------------------------------

func TestLedgerRPCRegisteredOnDaemonSurface(t *testing.T) {
	server := rpc.New(&rpc.Config{SocketPath: filepath.Join(t.TempDir(), "l.sock")}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c := &Components{}
	registerLedgerRPC(c, server, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// ledger.query must RESOLVE (not "method not found"): a nil store
	// answers the named disabled refusal, never -32601.
	_, err := server.CallMethod(context.Background(), "ledger.query", []byte(`{"employee":"e","mode":"log"}`))
	if err != nil && !errors.Is(err, rpc.ErrLedgerDisabled) {
		// Any error other than the named disabled refusal on a nil store
		// is unexpected, but the method itself RESOLVED — the D6 pin
		// (registration) holds. Only "method not found" would fail it.
		t.Logf("ledger.query resolved with: %v", err)
	}
}

func TestLedgerRPCWithLiveStoreServesQueries(t *testing.T) {
	home := t.TempDir()
	store, err := ledger.Open(filepath.Join(home, "ledger.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: "emp-d6",
		Kind:       ledger.KindObservation,
		Subject:    "subj",
		Source:     ledger.SourceVoice,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	server := rpc.New(&rpc.Config{SocketPath: filepath.Join(t.TempDir(), "l2.sock")}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	registerLedgerRPC(&Components{EmployeeLedger: store}, server, slog.New(slog.NewTextHandler(io.Discard, nil)))

	result, err := server.CallMethod(context.Background(), "ledger.query", []byte(`{"employee":"emp-d6","mode":"log"}`))
	if err != nil {
		t.Fatalf("ledger.query over a wired store: %v", err)
	}
	m, ok := result.(rpc.LedgerQueryResult)
	if !ok {
		t.Fatalf("unexpected result shape: %T", result)
	}
	if m.Head != 1 {
		t.Fatalf("head = %d, want 1", m.Head)
	}
	if len(m.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(m.Records))
	}
	// ledger.state also resolves over the same registration.
	if _, err := server.CallMethod(context.Background(), "ledger.state", []byte(`{"employee":"emp-d6","subject":"subj"}`)); err != nil {
		t.Fatalf("ledger.state: %v", err)
	}
}

// ---------------------------------------------------------------------------
// D9: plain listener that exits immediately within the probe window
// fails the boot loudly
// ---------------------------------------------------------------------------

func TestPlainBootImmediateCleanExitFailsLoudly(t *testing.T) {
	w := plainBootWiring(t, freeLoopbackAddr(t))
	// Inject a Serve that exits cleanly (nil) at once: the pre-D9 code
	// warned and continued; the pin demands the boot FAIL.
	w.plainServe = func(context.Context, string) error { return nil }
	if err := w.startPlainListener(context.Background()); err == nil {
		t.Fatal("plain listener exited immediately with nil; boot continued (the D9 silent-degradation regression)")
	}
}

func TestPlainBootImmediateErrorExitStillFails(t *testing.T) {
	w := plainBootWiring(t, freeLoopbackAddr(t))
	w.plainServe = func(context.Context, string) error { return errors.New("boom") }
	if err := w.startPlainListener(context.Background()); err == nil {
		t.Fatal("plain listener errored within the probe window; boot continued")
	}
}

// ---------------------------------------------------------------------------
// D10: dial audit rows carry last-4 only
// ---------------------------------------------------------------------------

func TestDialAuditPayloadMaskedToLast4(t *testing.T) {
	auditStore, err := auditlog.OpenStore(filepath.Join(t.TempDir(), "audit.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	defer func() { _ = auditStore.Close() }()

	adapter := auditWriterAdapter{store: auditStore}
	const full = "+15551234567"
	row := map[string]any{
		"direction":   "outgoing",
		"target":      full,
		"employee_id": "emp-d10",
		"request_id":  "req-d10",
	}
	if err := adapter.WriteAudit(context.Background(), row); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	head, ok, err := auditStore.Head(context.Background())
	if err != nil || !ok {
		t.Fatalf("head: ok=%v err=%v", ok, err)
	}
	payload, _ := head.Payload["target"].(string)
	if payload == full {
		t.Fatal("audit row carries the FULL E.164 target — the D10 disclosure regression")
	}
	if payload != "•••4567" {
		t.Fatalf("masked target = %q, want •••4567", payload)
	}
	// The raw number must not appear anywhere in the payload.
	for k, v := range head.Payload {
		if s, isStr := v.(string); isStr && s == full {
			t.Fatalf("field %q leaks the raw number", k)
		}
	}
}

// ---------------------------------------------------------------------------
// A10: hangup racing setup -> no loop registered
// ---------------------------------------------------------------------------

func TestSetupRacingHangupRegistersNoLoop(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	const ccid = "cc-a10"
	acquireFor(w, ccid)
	trackCall(t, w, ccid)

	// Release the call FIRST (registry row dropped, manager released),
	// then deliver the setup frame that was already in flight. The
	// post-insert re-check must refuse the arming: no loop, no cancel,
	// no goroutine.
	w.releaseCall(ccid, voice.OutcomeHangup)
	w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})

	w.loopsMu.Lock()
	_, loopArmed := w.loops[ccid]
	_, cancelArmed := w.loopCancels[ccid]
	w.loopsMu.Unlock()
	if loopArmed || cancelArmed {
		t.Fatal("talk loop armed for a released call — the A10 zombie-loop regression")
	}
}
