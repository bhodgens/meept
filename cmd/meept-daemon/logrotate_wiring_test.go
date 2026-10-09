package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewRotatingLogWriterCapsTheFile is the regression pin for the 2026-10-08
// incident: meept.log reached 145 GB because it was opened with a bare
// O_APPEND and nothing ever capped it. The writer must now bound the file.
func TestNewRotatingLogWriterCapsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.log")
	const cap int64 = 2048

	w, closeLog := newRotatingLogWriter(path, cap, nil)
	defer func() { _ = closeLog() }()

	chunk := bytes.Repeat([]byte("l"), 255)
	chunk = append(chunk, '\n')
	for i := 0; i < 256; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := closeLog(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Every generation must respect the cap, so total disk cost is bounded.
	var total int64
	generations := 0
	for _, p := range []string{path, path + ".1", path + ".2"} {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		generations++
		if fi.Size() > cap+int64(len(chunk)) {
			t.Fatalf("%s exceeds cap: %d > %d", p, fi.Size(), cap)
		}
		total += fi.Size()
	}
	if generations < 2 {
		t.Fatalf("expected rotation to produce multiple generations, got %d", generations)
	}
	if bound := int64(3) * cap; total > bound {
		t.Fatalf("unbounded disk cost: %d > %d", total, bound)
	}
}

// TestNewRotatingLogWriterTeesConsole proves foreground runs still print, which
// is why the writer is a MultiWriter rather than a replacement.
func TestNewRotatingLogWriterTeesConsole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.log")
	var console bytes.Buffer

	w, closeLog := newRotatingLogWriter(path, 1<<20, &console)
	defer func() { _ = closeLog() }()

	if _, err := w.Write([]byte("visible\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := closeLog(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if !strings.Contains(console.String(), "visible") {
		t.Fatalf("console output lost: %q", console.String())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file: %v", err)
	}
	if !strings.Contains(string(body), "visible") {
		t.Fatalf("log file missing the line: %q", body)
	}
}

// TestNewRotatingLogWriterFallsBackToConsole proves an unopenable log file
// degrades to console output instead of dropping every line — losing the
// terminal is worse than losing the file.
func TestNewRotatingLogWriterFallsBackToConsole(t *testing.T) {
	var console bytes.Buffer
	// Parent directory does not exist, so logrotate.New must fail.
	w, closeLog := newRotatingLogWriter(filepath.Join(t.TempDir(), "nope", "meept.log"), 1024, &console)
	defer func() { _ = closeLog() }()

	if _, err := w.Write([]byte("still visible\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(console.String(), "still visible") {
		t.Fatalf("fallback lost output: %q", console.String())
	}
}
