//go:build e2e

// Issue #165 follow-up (durable daily caps), voice-lifecycle hermetic gate.
// Two properties are pinned here with NO live network and NO daemon:
//
//  1. config binding: voice.DialConfig.DailyDialCap = 1 refuses the second
//     outbound dial (ErrDailyCapReached) before any Telnyx POST, audit row
//     or effect reservation;
//  2. restart persistence: a fresh Dialer and a fresh DailyCounter built
//     over the SAME counter file see the spent allowance, so the next dial
//     is still refused. The old in-process counter would reopen the cap.
//
// The counter file is a temp path; 0600 perms and atomic-rename durability
// are asserted directly so a future regression to a plain in-process map
// or a world-readable file fails this suite.
package voicelifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/internal/voice/telnyx"
)

// ccDialController records every DialOut (the Telnyx POST /calls seam).
type ccDialController struct{ dials int }

func (c *ccDialController) DialOut(_ context.Context, _, _, _ string, _ int) (string, error) {
	c.dials++
	return "cc-cap-e2e", nil
}
func (c *ccDialController) AnswerCall(_ context.Context, _ string) error { return nil }
func (c *ccDialController) StartStreaming(_ context.Context, _, _ string, _ telnyx.Codec) error {
	return nil
}
func (c *ccDialController) HangupCall(_ context.Context, _ string) error { return nil }

// capAudit counts audit rows; a refused/capped dial must write none.
type capAudit struct{ rows int }

func (a *capAudit) WriteAudit(_ context.Context, _ map[string]any) error {
	a.rows++
	return nil
}

const (
	capFrom   = "+15551230009"
	capTarget = "+15551230001"
)

// newCapDialer builds a real voice.Dialer over a fresh controller/audit and
// the supplied counter, with a cap of 1. The allowlist admits capTarget and
// every other gate is valid, so the ONLY refusal reason is the daily cap.
func newCapDialer(counter *voice.DailyCounter) (*voice.Dialer, *ccDialController, *capAudit) {
	ctl := &ccDialController{}
	audit := &capAudit{}
	d := voice.NewDialer(voice.DialConfig{
		Enabled:         true,
		AllowedNumbers:  []string{capTarget},
		PublicStreamURL: "wss://media.example/stream",
		ConnectionID:    "conn-cap-e2e",
		FromNumber:      capFrom,
		DailyDialCap:    1,
		Counter:         counter,
	}, ctl, nil, voice.NewRegistry(4), effects.NewMemoryLedger(), audit, nil)
	return d, ctl, audit
}

func dialOnceTarget(t *testing.T, d *voice.Dialer, requestID, target string) error {
	t.Helper()
	_, err := d.Dial(context.Background(), voice.DialRequest{
		EmployeeID: "emp-cap-e2e",
		Target:     target,
		RequestID:  requestID,
	})
	return err
}

func dialOnce(t *testing.T, d *voice.Dialer, requestID string) error {
	return dialOnceTarget(t, d, requestID, capTarget)
}

// TestDailyDialCapConfigBindsAndPersistsAcrossRestart pins both required
// properties in one lifecycle: dial once (admitted), confirm the file was
// written 0600, then build a FRESH counter+dialer over the same path and
// confirm the second dial is refused with zero additional POSTs/audit rows.
func TestDailyDialCapConfigBindsAndPersistsAcrossRestart(t *testing.T) {
	counterPath := filepath.Join(t.TempDir(), "voice", "daily_counters.json")
	counter := voice.NewDailyCounter(counterPath)

	// First dial admitted.
	d1, ctl1, audit1 := newCapDialer(counter)
	if err := dialOnce(t, d1, "req-cap-first"); err != nil {
		t.Fatalf("first dial: %v", err)
	}
	if ctl1.dials != 1 || audit1.rows != 1 {
		t.Fatalf("first dial: dials=%d audit=%d, want 1/1", ctl1.dials, audit1.rows)
	}

	// The counter file exists, is 0600, and carries the day/scope count.
	info, err := os.Stat(counterPath)
	if err != nil {
		t.Fatalf("stat counter file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("counter file perms = %o, want 600", perm)
	}

	// Simulate a daemon restart: a brand-new counter + dialer over the
	// SAME path. The spent allowance must still be visible.
	counter2 := voice.NewDailyCounter(counterPath)
	d2, ctl2, audit2 := newCapDialer(counter2)
	start := time.Now()
	err = dialOnce(t, d2, "req-cap-second")
	if !errors.Is(err, voice.ErrDailyCapReached) {
		t.Fatalf("post-restart dial error = %v, want errors.Is(ErrDailyCapReached)", err)
	}
	if ctl2.dials != 0 || audit2.rows != 0 {
		t.Errorf("capped post-restart dial: dials=%d audit=%d, want 0/0 (gate before side effects)", ctl2.dials, audit2.rows)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("capped dial took too long: %s", time.Since(start))
	}
}

// TestDailyDialCapUnlimitedIsByteIdentical pins the backward-compatible
// default: with cap 0, repeated dials all go through and NO counter file is
// written, even when a counter path is supplied (it must stay inert).
func TestDailyDialCapUnlimitedIsByteIdentical(t *testing.T) {
	dir := t.TempDir()
	counterPath := filepath.Join(dir, "daily_counters.json")
	ctl := &ccDialController{}
	audit := &capAudit{}
	targets := []string{"+15551230011", "+15551230012", "+15551230013"}
	for i, target := range targets {
		// Fresh registry per dial: with mgr=nil the slot never auto-releases,
		// and the one-live-call-per-employee rule is unrelated to the cap.
		d := voice.NewDialer(voice.DialConfig{
			Enabled:         true,
			AllowedNumbers:  targets,
			PublicStreamURL: "wss://media.example/stream",
			ConnectionID:    "conn-cap-e2e",
			FromNumber:      capFrom,
			DailyDialCap:    0,
			Counter:         voice.NewDailyCounter(counterPath),
		}, ctl, nil, voice.NewRegistry(4), effects.NewMemoryLedger(), audit, nil)
		if err := dialOnceTarget(t, d, "req-unlimited-"+string(rune('a'+i)), target); err != nil {
			t.Fatalf("unlimited dial %d: %v", i, err)
		}
	}
	if ctl.dials != 3 || audit.rows != 3 {
		t.Errorf("unlimited dials: dials=%d audit=%d, want 3/3", ctl.dials, audit.rows)
	}
	if _, err := os.Stat(counterPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("counter file created with cap 0 (want inert): stat err = %v", err)
	}
}

// TestDailyDialCapConcurrentTryIncAdmitsExactlyOne pins the counter's
// cross-call atomicity: 8 goroutines racing at cap=1 admit exactly ONE.
// A read-check-write split around an in-process mutex fails this test:
// multiple callers can read the same pre-increment count and all be
// admitted. The OS file lock must make the whole sequence atomic.
func TestDailyDialCapConcurrentTryIncAdmitsExactlyOne(t *testing.T) {
	counterPath := filepath.Join(t.TempDir(), "voice", "daily_counters.json")
	counter := voice.NewDailyCounter(counterPath)

	const n = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	errs := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, ok, err := counter.TryInc("2026-11-22", "voice.dial", 1)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs++
			}
			if ok {
				admitted++
			}
		}()
	}
	close(start)
	wg.Wait()

	if errs != 0 {
		t.Errorf("concurrent TryInc errors = %d, want 0", errs)
	}
	if admitted != 1 {
		t.Errorf("concurrent TryInc admissions = %d, want exactly 1", admitted)
	}
}
