package l0

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

func newTestRecorder(t *testing.T, loc *time.Location) (*Recorder, string) {
	t.Helper()
	dir := t.TempDir()
	return NewRecorder(dir, loc), dir
}

// TestAppendReadRoundtrip writes records and reads them back, verifying the
// frozen §2.1 JSON shape field-by-field on the raw line.
func TestAppendReadRoundtrip(t *testing.T) {
	loc := time.UTC
	rec, _ := newTestRecorder(t, loc)

	if err := rec.Append("sess-1", "user", "hello <memory-context>injected</memory-context> world", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := rec.Append("sess-1", "assistant", "hi there", nil); err != nil {
		t.Fatalf("append: %v", err)
	}

	date := time.Now().In(loc).Format("2006-01-02")
	path := rec.DatePath(date)
	if filepath.Base(path) != date+".jsonl" {
		t.Fatalf("unexpected path layout: %s", path)
	}

	raw, err := os.ReadFile(path) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}

	// Frozen schema: exact key set per line.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &probe); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	for _, key := range []string{"id", "ts", "session_id", "role", "content", "meta"} {
		if _, ok := probe[key]; !ok {
			t.Fatalf("line missing frozen key %q: %s", key, lines[0])
		}
	}
	if len(probe) != 6 {
		t.Fatalf("line has extra keys beyond frozen schema: %s", lines[0])
	}

	got, err := ReadSince(path, "", 0)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 records, got %d", len(got))
	}
	if got[0].SessionID != "sess-1" || got[0].Role != "user" {
		t.Errorf("record 0 mismatch: %+v", got[0])
	}
	if got[0].Content != "helloworld" {
		t.Errorf("tag span not stripped: %q", got[0].Content)
	}
	if got[0].Meta["k"] != "v" {
		t.Errorf("meta not preserved: %+v", got[0].Meta)
	}
	if got[1].Role != "assistant" || got[1].Content != "hi there" {
		t.Errorf("record 1 mismatch: %+v", got[1])
	}
	if _, err := time.Parse(time.RFC3339, got[0].TS); err != nil {
		t.Errorf("ts not RFC3339: %v", err)
	}
}

// TestSanitizeForStoreVariants covers the stripping contract the recorder
// relies on (logic owned by internal/memory.SanitizeForStore).
func TestSanitizeForStoreVariants(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no tags", "plain text", "plain text"},
		{"whole span", "<memory-context>abc</memory-context>", ""},
		{"embedded", "before <memory-context>x\ny</memory-context> after", "beforeafter"},
		{"leading whitespace", "\n\n<memory-context>x</memory-context>tail", "tail"},
		{"unclosed tag kept", "<memory-context>no close", "<memory-context>no close"},
		{"multiline span", "a <memory-context>\nline1\nline2\n</memory-context> b", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memory.SanitizeForStore(tc.in); got != tc.want {
				t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestReadSinceMarkerAndLimit verifies sinceID skips through the marker and
// limit caps the result.
func TestReadSinceMarkerAndLimit(t *testing.T) {
	rec, dir := newTestRecorder(t, time.UTC)
	for i := range 5 {
		if err := rec.Append("s", "user", "m", nil); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	path := rec.DatePath(time.Now().UTC().Format("2006-01-02"))

	all, err := ReadSince(path, "", 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("ReadSince all: %d records, err %v", len(all), err)
	}

	since := all[1].ID
	got, err := ReadSince(path, since, 0)
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(got) != 3 || got[0].ID != all[2].ID {
		t.Errorf("sinceID not honored: got %d records", len(got))
	}

	limited, err := ReadSince(path, "", 2)
	if err != nil {
		t.Fatalf("ReadSince limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limit not honored: got %d", len(limited))
	}

	missing, err := ReadSince(filepath.Join(dir, "1999-01-01.jsonl"), "", 0)
	if err != nil || len(missing) != 0 {
		t.Errorf("missing file: want empty/nil error, got %d, %v", len(missing), err)
	}
}

// TestReadSessionFiltersBySession verifies per-session filtering across one
// day file.
func TestReadSessionFiltersBySession(t *testing.T) {
	rec, dir := newTestRecorder(t, time.UTC)
	_ = rec.Append("a", "user", "one", nil)
	_ = rec.Append("b", "user", "two", nil)
	_ = rec.Append("a", "assistant", "three", nil)

	date := time.Now().UTC().Format("2006-01-02")
	got, err := ReadSession(filepath.Join(dir, "l0"), date, "a")
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(got) != 2 || got[0].Content != "one" || got[1].Content != "three" {
		t.Errorf("session filter wrong: %+v", got)
	}
}

// TestIDUniquenessAcrossAppends: pkg/id.Generate must never collide across
// 100 appends (predictable-ID rule: crypto/rand, not time/rand).
func TestIDUniquenessAcrossAppends(t *testing.T) {
	rec, _ := newTestRecorder(t, time.UTC)
	date := time.Now().UTC().Format("2006-01-02")
	path := rec.DatePath(date)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if err := rec.Append("s", "user", "x", nil); err != nil {
				t.Errorf("append: %v", err)
			}
		})
	}
	wg.Wait()

	got, err := ReadSince(path, "", 0)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("want 100 records, got %d", len(got))
	}
	seen := make(map[string]bool, len(got))
	for _, r := range got {
		if seen[r.ID] {
			t.Fatalf("duplicate ID %q", r.ID)
		}
		seen[r.ID] = true
	}
}

// TestAppendWithFixedClock: the injected clock controls both the ts value
// and the daily file chosen, without sleeping.
func TestAppendWithFixedClock(t *testing.T) {
	dir := t.TempDir()
	r := NewRecorder(dir, time.UTC)
	fixed := time.Date(2026, 10, 12, 23, 30, 0, 0, time.UTC)
	r.clock = func() time.Time { return fixed }

	if err := r.Append("s", "user", "c", nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := ReadSince(r.DatePath("2026-10-12"), "", 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("fixed-clock read: %d records, err %v", len(got), err)
	}
	if want := "2026-10-12T23:30:00Z"; got[0].TS != want {
		t.Errorf("ts = %q, want %q", got[0].TS, want)
	}
}

// TestPruneBoundaryAtCutoff: a file dated exactly at the cutoff day
// (today - maxAgeDays) survives; one day older is deleted. Retention is
// deterministic regardless of the host clock's distance from the fixture
// dates — the fixture is built relative to today.
func TestPruneBoundaryAtCutoff(t *testing.T) {
	dir := t.TempDir()
	// Build the fixture in the same LOCAL day frame Prune computes its cutoff
	// in (l0 files are named by local date — audit H4).
	today := memory.LocalDayStart(time.Now())

	mk := func(day time.Time) string {
		name := day.Format("2006-01-02") + ".jsonl"
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{\"id\":\"x\"}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return name
	}
	cutoffDay := mk(today.AddDate(0, 0, -7)) // exactly at cutoff: survives
	olderDay := mk(today.AddDate(0, 0, -8))  // one past cutoff: deleted
	recentDay := mk(today.AddDate(0, 0, -1)) // survives
	_, _ = cutoffDay, recentDay

	// Unrelated files are never touched.
	stray := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(stray, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	removed, err := Prune(dir, 7)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(dir, olderDay)); !os.IsNotExist(err) {
		t.Errorf("older-than-cutoff file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cutoffDay)); err != nil {
		t.Errorf("cutoff-day file should survive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, recentDay)); err != nil {
		t.Errorf("recent file should survive: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray file should survive: %v", err)
	}

	// Missing dir is a no-op, not an error.
	if n, err := Prune(filepath.Join(dir, "nope"), 7); err != nil || n != 0 {
		t.Errorf("missing dir: got (%d, %v)", n, err)
	}
}
