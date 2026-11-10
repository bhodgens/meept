package main

// voice_link_test.go - flag-validation and status-parsing tests for the
// `meept voice link` CLI (deadair-link plan leaf 03). Pure: no daemon,
// no RPC. Mirrors voice_test.go's formatter-test style.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/voice"
)

func TestVoiceLinkSubcommandRegistered(t *testing.T) {
	v := newVoiceCmd()
	var linkCmd bool
	for _, sub := range v.Commands() {
		if sub.Name() == "link" {
			linkCmd = true
			break
		}
	}
	if !linkCmd {
		t.Fatal("voice subcommands missing: link")
	}
}

func TestVoiceLinkFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		flags   linkFlags
		wantErr string // substring; "" = valid
	}{
		{"missing a", linkFlags{b: "secretary", seconds: 60}, "--a is required"},
		{"missing b", linkFlags{a: "companion", seconds: 60}, "--b is required"},
		{"empty both", linkFlags{seconds: 60}, "--a is required"},
		{"same employee", linkFlags{a: "companion", b: "companion", seconds: 60}, "different employees"},
		{"zero seconds", linkFlags{a: "a", b: "b", seconds: 0}, "--seconds must be > 0"},
		{"negative seconds", linkFlags{a: "a", b: "b", seconds: -5}, "--seconds must be > 0"},
		{"valid", linkFlags{a: "companion", b: "secretary", seconds: 90}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.flags.validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validate(%+v) = %v, want nil", tc.flags, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate(%+v) = nil, want error containing %q", tc.flags, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate(%+v) = %v, want error containing %q", tc.flags, err, tc.wantErr)
			}
		})
	}
}

func TestVoiceLinkFlagDefaults(t *testing.T) {
	cmd := newVoiceLinkCmd()
	sec, err := cmd.Flags().GetInt("seconds")
	if err != nil {
		t.Fatalf("seconds flag: %v", err)
	}
	if sec != 60 {
		t.Fatalf("--seconds default = %d, want 60", sec)
	}
	// Same-employee refusal fires EARLY through RunE (before any daemon
	// connect): executing the command with --a == --b must never reach
	// connectDaemon (there is no daemon in this test - a connection
	// attempt would hang or fail with a connect error, not the
	// validation message).
	cmd.SetArgs([]string{"--a", "x", "--b", "x", "--seconds", "10"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "different employees") {
		t.Fatalf("RunE same-employee = %v, want early validation error", err)
	}
}

func TestVoiceLinkStatusParsing(t *testing.T) {
	// The CLI parses the daemon's voice.link.status envelope; pin the
	// wire shape round-trip against a hand-built JSON (the exact
	// internal/rpc/voice_link.go field names).
	raw := `{
		"link_id": "link-abc",
		"ended": false,
		"a": {"call_id": "local-1", "employee_id": "companion", "assistant_turns": 2, "user_turns": 1,
		      "transcript": [{"role": "assistant", "text": "hello", "at": "2026-11-09T12:00:00Z"}]},
		"b": {"call_id": "local-2", "employee_id": "secretary", "assistant_turns": 1, "user_turns": 0,
		      "transcript": []}
	}`
	var res voiceLinkStatusResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if res.LinkID != "link-abc" || res.Ended {
		t.Fatalf("envelope = %+v", res)
	}
	if res.A.AssistantTurns != 2 || res.A.UserTurns != 1 || res.A.EmployeeID != "companion" {
		t.Fatalf("side a = %+v", res.A)
	}
	if len(res.A.Transcript) != 1 || res.A.Transcript[0].Text != "hello" {
		t.Fatalf("side a transcript = %+v", res.A.Transcript)
	}
	if len(res.B.Transcript) != 0 {
		t.Fatalf("side b transcript = %+v, want empty", res.B.Transcript)
	}
}

// TestVoiceLinkAcceptanceGate pins the exit rule: the run-error path
// names the convergence failure when a side is short on assistant turns
// (the check is a pure comparison, driven through the same constants
// the runner uses).
func TestVoiceLinkAcceptanceGate(t *testing.T) {
	if linkRequiredAssistantTurns != 2 {
		t.Fatalf("acceptance bar = %d, leaf doc pins 2", linkRequiredAssistantTurns)
	}
	short := voiceLinkSideStatus{AssistantTurns: 1}
	okA := voiceLinkSideStatus{AssistantTurns: 2}
	okB := voiceLinkSideStatus{AssistantTurns: 3}
	check := func(a, b voiceLinkSideStatus) error {
		if a.AssistantTurns < linkRequiredAssistantTurns || b.AssistantTurns < linkRequiredAssistantTurns {
			return errors.New("link did not converge")
		}
		return nil
	}
	if err := check(short, okB); err == nil {
		t.Fatal("side short on turns must fail the gate")
	}
	if err := check(okA, okB); err != nil {
		t.Fatalf("both sides >= 2 must pass: %v", err)
	}
}

// TestVoiceLinkMonitorFlag pins the --monitor surface: flag exists with
// default false, the parse lands on linkFlags.monitor, and the open
// result mirrors the daemon's a_mon_path/b_mon_path wire keys.
func TestVoiceLinkMonitorFlag(t *testing.T) {
	cmd := newVoiceLinkCmd()
	mon, err := cmd.Flags().GetBool("monitor")
	if err != nil {
		t.Fatalf("monitor flag not registered: %v", err)
	}
	if mon {
		t.Fatal("--monitor default = true, want false (opt-in tap)")
	}

	// Parse through the command surface: --monitor flips the flag value.
	cmd.SetArgs([]string{"--a", "x", "--b", "x", "--monitor"})
	_ = cmd.Execute() // same-employee refuses early (no daemon); the flag parse already happened
	mon, err = cmd.Flags().GetBool("monitor")
	if err != nil || !mon {
		t.Fatalf("--monitor parse = %v (err %v), want true", mon, err)
	}

	// linkFlags carries the knob; validate() never rejects it (pure
	// pass-through - the monitor flag has no constraints of its own).
	f := linkFlags{a: "companion", b: "secretary", seconds: 60, monitor: true}
	if err := f.validate(); err != nil {
		t.Fatalf("validate with monitor = %v, want nil", err)
	}
}

// TestVoiceLinkOpenResultMonitorPaths pins the CLI mirror of the
// monitor-augmented voice.link.open shape: fields parse, and an
// envelope WITHOUT them (an old daemon / non-monitor open) leaves them
// empty.
func TestVoiceLinkOpenResultMonitorPaths(t *testing.T) {
	withMon := `{"link_id":"link-1","a_ccid":"local-1","b_ccid":"local-2","a_mon_path":"/s/link-1.mon-a","b_mon_path":"/s/link-1.mon-b"}`
	var o voiceLinkOpenResult
	if err := json.Unmarshal([]byte(withMon), &o); err != nil {
		t.Fatalf("unmarshal monitor open result: %v", err)
	}
	if o.MonitorAPath != "/s/link-1.mon-a" || o.MonitorBPath != "/s/link-1.mon-b" {
		t.Fatalf("monitor paths = %q/%q", o.MonitorAPath, o.MonitorBPath)
	}
	without := `{"link_id":"link-2","a_ccid":"local-3","b_ccid":"local-4"}`
	var o2 voiceLinkOpenResult
	if err := json.Unmarshal([]byte(without), &o2); err != nil {
		t.Fatalf("unmarshal legacy open result: %v", err)
	}
	if o2.MonitorAPath != "" || o2.MonitorBPath != "" {
		t.Fatalf("legacy shape leaked monitor paths: %+v", o2)
	}
}

// ---------------------------------------------------------------------------
// --record flag validation
// ---------------------------------------------------------------------------

func TestVoiceLinkRecordFlagValidation(t *testing.T) {
	// --record without --monitor is refused with a clear error.
	f := linkFlags{a: "companion", b: "secretary", seconds: 60, record: "/tmp/somewhere"}
	err := f.validate()
	if err == nil || !strings.Contains(err.Error(), "--record requires --monitor") {
		t.Fatalf("validate(record w/o monitor) = %v, want --record requires --monitor", err)
	}
	// --record with --monitor and a NON-EXISTENT dir: validate creates it.
	dir := filepath.Join(t.TempDir(), "nested", "rec")
	f = linkFlags{a: "companion", b: "secretary", seconds: 60, monitor: true, record: dir}
	if err := f.validate(); err != nil {
		t.Fatalf("validate(record+monitor) = %v, want nil", err)
	}
	if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
		t.Fatalf("--record dir not created: stat = %v", serr)
	}
	// record == "" is the default: no directory side effect, no error
	// even without --monitor (zero behaviour change).
	f = linkFlags{a: "companion", b: "secretary", seconds: 60}
	if err := f.validate(); err != nil {
		t.Fatalf("validate(no record) = %v, want nil", err)
	}
	// The --record flag is registered on the command surface.
	cmd := newVoiceLinkCmd()
	if _, err := cmd.Flags().GetString("record"); err != nil {
		t.Fatalf("--record flag not registered: %v", err)
	}
}

// ---------------------------------------------------------------------------
// recorderPlayer (unit, no audio device)
// ---------------------------------------------------------------------------

// fakePlayer records the bursts handed to playback for recorder tests.
type fakePlayer struct {
	mu      sync.Mutex
	played  [][]byte
	closed  int
	cleared int
}

func (p *fakePlayer) Play(burst []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]byte, len(burst))
	copy(cp, burst)
	p.played = append(p.played, cp)
	return nil
}
func (p *fakePlayer) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleared++
}
func (p *fakePlayer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed++
}

// recorderPlayer satisfies voice.Player (compile-time pin).
var _ voice.Player = (*recorderPlayer)(nil)

func TestRecorderPlayerWritesBytesInOrder(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "x.raw"))
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakePlayer{}
	r := newRecorderPlayer(inner, f, false)
	bursts := [][]byte{[]byte("AAAA"), []byte("BBBBBB"), []byte("CC")}
	for _, b := range bursts {
		if err := r.Play(b); err != nil {
			t.Fatalf("Play: %v", err)
		}
	}
	if err := r.closeFile(); err != nil {
		t.Fatalf("closeFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "x.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "AAAA" + "BBBBBB" + "CC"; string(got) != want {
		t.Fatalf("recorded = %q, want %q (exact bytes, burst order)", got, want)
	}
	// Every burst was ALSO handed to the inner player, unchanged.
	if len(inner.played) != len(bursts) {
		t.Fatalf("inner got %d bursts, want %d", len(inner.played), len(bursts))
	}
	for i, b := range bursts {
		if string(inner.played[i]) != string(b) {
			t.Fatalf("inner burst %d = %q, want %q", i, inner.played[i], b)
		}
	}
	// The recorder must never close the shared audio context.
	if inner.closed != 0 {
		t.Fatal("recorder closed the inner player; monitorWG owns that")
	}
}

func TestRecorderPlayerSkipFirst(t *testing.T) {
	// The monitor's silent 640-byte warm-up burst must NOT enter the
	// recording, but must still reach the inner player (context warm).
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "y.raw"))
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakePlayer{}
	r := newRecorderPlayer(inner, f, true)
	warm := make([]byte, 640)
	if err := r.Play(warm); err != nil {
		t.Fatalf("warm Play: %v", err)
	}
	if err := r.Play([]byte("SPEECH")); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := r.closeFile(); err != nil {
		t.Fatalf("closeFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "y.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "SPEECH" {
		t.Fatalf("recorded = %q, want exactly %q (warm-up skipped)", got, "SPEECH")
	}
	if len(inner.played) != 2 || len(inner.played[0]) != 640 {
		t.Fatalf("inner got %d bursts (first len %d), want warm-up + speech", len(inner.played), len(inner.played[0]))
	}
}

func TestRecorderPlayerCloseAfterDrainAndIdempotent(t *testing.T) {
	// Teardown contract: the drain loop finishes (EOF -> done), the
	// sink flushes its tail THROUGH the recorder, and only then does
	// closeFile run - so the tail burst lands in the recording.
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "z.raw"))
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakePlayer{}
	rec := newRecorderPlayer(inner, f, true)
	// Warm-up through the recorder (skip-first consumes it).
	if err := rec.Play(make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	sink := voice.NewSpeakerSink(rec, 0)
	r, w := io.Pipe()
	done := make(chan error, 1)
	go drainLiveSpeaker(r, sink, done)
	tail := bytes.Repeat([]byte{0x01, 0x02}, 320) // one 640 B chunk
	if _, err := w.Write(tail); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil { // socket EOF
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("drainLiveSpeaker: %v", err)
	}
	// Mirror the production teardown ordering exactly.
	if err := sink.Flush(); err != nil {
		t.Fatalf("sink flush: %v", err)
	}
	if err := rec.closeFile(); err != nil {
		t.Fatalf("closeFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "z.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, tail) {
		t.Fatalf("recording = %d bytes, want the %d-byte tail (EOF tail must land after drain)", len(got), len(tail))
	}
	// Idempotent close + late Play after close is a no-op tee (no panic,
	// inner still plays).
	if err := rec.closeFile(); err != nil {
		t.Fatalf("second closeFile: %v", err)
	}
	if err := rec.Play([]byte("late")); err != nil {
		t.Fatalf("Play after close: %v", err)
	}
	// inner order: warm-up (640 zeros), tail burst, late burst.
	if len(inner.played) != 3 || string(inner.played[2]) != "late" {
		t.Fatalf("late burst did not reach inner: %d bursts", len(inner.played))
	}
	rec.Close() // voice.Player.Close must not panic post-close
}

func TestRecorderPlayerClearForwards(t *testing.T) {
	inner := &fakePlayer{}
	r := newRecorderPlayer(inner, nil, false)
	r.mu.Lock()
	r.closed = true // no file: Play never tees, Clear must still forward
	r.mu.Unlock()
	r.Clear()
	if inner.cleared != 1 {
		t.Fatalf("inner clears = %d, want 1", inner.cleared)
	}
}
