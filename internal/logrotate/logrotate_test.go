package logrotate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCapSizeRotatesAndBoundsDisk is the regression pin for the 2026-10-08
// incident: an unbounded append-only daemon log reached 145 GB. It writes far
// more than the cap and asserts the live file never exceeds it and that the
// total on-disk cost stays bounded.
func TestCapSizeRotatesAndBoundsDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.log")
	const cap int64 = 4096

	c, err := New(path, cap)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	// 64 KiB of 128-byte lines: 16x the cap.
	line := bytes.Repeat([]byte("x"), 127)
	line = append(line, '\n')
	for i := 0; i < 512; i++ {
		if _, err := c.Write(line); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	// A write may overshoot the cap by at most one write's worth.
	if size > cap+int64(len(line)) {
		t.Fatalf("live log grew past the cap: size=%d cap=%d", size, cap)
	}

	// The backup generation must exist and must itself be capped, which is what
	// makes total disk cost <= 2x cap instead of unbounded.
	fi, err := os.Stat(c.RotatedBackup())
	if err != nil {
		t.Fatalf("expected a rotated backup at %s: %v", c.RotatedBackup(), err)
	}
	if fi.Size() > cap+int64(len(line)) {
		t.Fatalf("backup generation is unbounded: size=%d cap=%d", fi.Size(), cap)
	}

	live, err := os.Stat(path)
	if err != nil {
		t.Fatalf("live log missing after rotate: %v", err)
	}
	total := live.Size() + fi.Size()
	if total > 3*cap {
		t.Fatalf("on-disk cost not bounded: live=%d backup=%d total=%d cap=%d",
			live.Size(), fi.Size(), total, cap)
	}
}

// TestCapSizePreservesContentAcrossRotate proves rotation is lossless for
// already-written bytes: everything written must still be readable across the
// live file and the single backup.
// TestCapSizeRetainedBytesAreAnExactSuffix pins the honest rotation contract.
// Retention is bounded (maxBackups+1 generations), so older bytes ARE dropped by
// design — but what survives must be a byte-exact SUFFIX of the original stream,
// with no corruption, truncation mid-line, or reordering. It must also be
// non-empty, i.e. rotation preserves recent history rather than erasing the log.
//
// The first version of this test demanded that ALL bytes survive, which the
// bounded design cannot promise; it also caught the real bug that rotate()
// overwrote .1 on every rotation (656 of 2624 bytes kept).
func TestCapSizeRetainedBytesAreAnExactSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.log")
	const cap int64 = 512

	c, err := New(path, cap)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var all bytes.Buffer
	for i := 0; i < 64; i++ {
		msg := []byte(strings.Repeat("a", 40) + "\n")
		if _, err := c.Write(msg); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		all.Write(msg)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Oldest generation first: .2, then .1, then the live file.
	got := readAll(t, path+".2") + readAll(t, c.RotatedBackup()) + readAll(t, path)
	want := all.String()

	if got == "" {
		t.Fatal("rotation erased the entire log")
	}
	if len(got) > len(want) {
		t.Fatalf("retained %d bytes, more than the %d written", len(got), len(want))
	}
	if !strings.HasSuffix(want, got) {
		t.Fatalf("retained bytes are not an exact suffix of the stream:\n got tail=%q\nwant tail=%q",
			tail(got, 80), tail(want, 80))
	}
	// The retained window must be substantial, not a token fragment.
	if bound := int64(maxBackups+1) * cap; int64(len(got)) < bound/2 {
		t.Fatalf("retained only %d bytes, want roughly %d (the rotation window)",
			len(got), bound/2)
	}
}

// TestCapSizeBoundsTotalDiskFootprint proves the steady-state disk cost is
// bounded by (maxBackups+1) x cap no matter how much is written — the property
// whose absence let a single runaway fill a 927 GB disk.
func TestCapSizeBoundsTotalDiskFootprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.log")
	const cap int64 = 1024

	c, err := New(path, cap)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	// 256 KiB against a 1 KiB cap: hundreds of rotations.
	chunk := bytes.Repeat([]byte("y"), 255)
	chunk = append(chunk, '\n')
	for i := 0; i < 1024; i++ {
		if _, err := c.Write(chunk); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	var total int64
	for _, p := range []string{path, path + ".1", path + ".2"} {
		if fi, err := os.Stat(p); err == nil {
			if fi.Size() > cap+int64(len(chunk)) {
				t.Fatalf("%s exceeds cap: %d > %d", p, fi.Size(), cap)
			}
			total += fi.Size()
		}
	}
	if bound := int64(maxBackups+1) * cap; total > bound {
		t.Fatalf("total disk footprint unbounded: %d bytes > %d", total, bound)
	}
}

// TestCapSizeAppendsToExisting proves the wrapper preserves the O_APPEND
// semantics the call sites rely on: a log written by a previous run must be
// extended, not truncated.
func TestCapSizeAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.log")
	if err := os.WriteFile(path, []byte("previous run\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	c, err := New(path, 1<<20)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("new run\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	body := readAll(t, path)
	if !strings.Contains(body, "previous run") || !strings.Contains(body, "new run") {
		t.Fatalf("append semantics broken: %q", body)
	}
}

// TestNewRejectsUnwritablePath proves the constructor surfaces an error rather
// than silently handing back a writer that drops every line.
func TestNewRejectsUnwritablePath(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "nope", "meept.log"), 1024); err == nil {
		t.Fatal("expected an error for a missing parent directory")
	}
}

// TestCapSizeCloseIsIdempotent proves the deferred Close in the call sites
// cannot panic on a double close.
func TestCapSizeCloseIsIdempotent(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "meept.log"), 1024)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should be nil, got %v", err)
	}
}

// TestCapSizeWriteAfterCloseFails proves a write on a closed writer is an error
// rather than a silent drop — the difference between "log lost" and "log
// reported broken".
func TestCapSizeWriteAfterCloseFails(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "meept.log"), 1024)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.Close()
	if _, err := c.Write([]byte("x")); err == nil {
		t.Fatal("expected an error writing to a closed CapSize")
	}
}

// TestDefaultMaxBytesApplies proves a non-positive cap falls back to the
// documented default instead of disabling rotation entirely, which is the
// failure mode that let the original log grow unbounded.
func TestDefaultMaxBytesApplies(t *testing.T) {
	c, err := New(filepath.Join(t.TempDir(), "meept.log"), 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.maxBytes != DefaultMaxBytes {
		t.Fatalf("maxBytes=%d, want DefaultMaxBytes=%d", c.maxBytes, DefaultMaxBytes)
	}
	// A negative cap must behave identically to zero.
	c2, err := New(filepath.Join(t.TempDir(), "meept.log"), -1)
	if err != nil {
		t.Fatalf("New(negative): %v", err)
	}
	defer c2.Close()
	if c2.maxBytes != DefaultMaxBytes {
		t.Fatalf("negative cap: maxBytes=%d, want %d", c2.maxBytes, DefaultMaxBytes)
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
