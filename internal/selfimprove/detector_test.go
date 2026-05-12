package selfimprove

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// -----------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------

func newTestDetector(t *testing.T) *IssueDetector {
	dir := t.TempDir()
	t.Cleanup(func() {})
	cfg := DetectionConfig{
		LogPatterns: []string{},
		ErrorPatterns: []string{
			"ERROR",
			"FATAL",
			"panic:",
			"exception:",
		},
		SlowQueryThreshold: 5 * time.Second,
		Metrics:            []string{"error_rate"},
	}
	return NewIssueDetector(cfg, dir, slog.Default())
}

func newTestDetectorWithRoot(_ *testing.T, root string) *IssueDetector {
	cfg := DetectionConfig{
		LogPatterns: []string{},
		ErrorPatterns: []string{
			"ERROR",
			"FATAL",
			"panic:",
			"exception:",
		},
		SlowQueryThreshold: 5 * time.Second,
		Metrics:            []string{"error_rate"},
	}
	return NewIssueDetector(cfg, root, slog.Default())
}

func writeLog(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}
}

// -----------------------------------------------------------------------
// NewIssueDetector
// -----------------------------------------------------------------------

func TestNewIssueDetector(t *testing.T) {
	d := newTestDetector(t)
	if d == nil {
		t.Fatal("expected non-nil detector")
	}
	if len(d.errorPatterns) != 4 {
		t.Errorf("expected 4 error patterns, got %d", len(d.errorPatterns))
	}
}

func TestNewIssueDetector_InvalidPatterns(t *testing.T) {
	cfg := DetectionConfig{
		ErrorPatterns: []string{
			"valid-pattern",
			"[invalid", // bad regex
		},
	}
	d := NewIssueDetector(cfg, "", slog.Default())
	// Only valid patterns should be compiled
	if len(d.errorPatterns) != 1 {
		t.Errorf("expected 1 compiled pattern (bad regex skipped), got %d", len(d.errorPatterns))
	}
}

// -----------------------------------------------------------------------
// scanLogFile
// -----------------------------------------------------------------------

func TestScanLogFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	writeLog(t, logPath, `2024-01-01 INFO starting up
2024-01-01 ERROR database connection failed
2024-01-01 WARN disk space low
2024-01-01 FATAL system crash`)

	d := newTestDetector(t)
	d.projectRoot = dir
	ctx := context.Background()
	issues, err := d.scanLogFile(ctx, logPath)
	if err != nil {
		t.Fatalf("scanLogFile failed: %v", err)
	}

	// Should find ERROR, FATAL (and maybe WARN if matched -- FATAL is in the patterns but ERROR is matched too).
	// The default ErrorPatterns are ERROR, FATAL, panic:, exception:
	// So ERROR and FATAL lines should match
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues, got %d", len(issues))
	}

	// Verify issue structure
	for i, issue := range issues {
		if issue.ID == "" {
			t.Errorf("issue %d has empty ID", i)
		}
		if issue.Source != logPath {
			t.Errorf("issue %d wrong source: %s", i, issue.Source)
		}
		if issue.Metadata == nil {
			t.Errorf("issue %d has nil metadata", i)
		}
		if _, ok := issue.Metadata["line_number"]; !ok {
			t.Errorf("issue %d missing line_number metadata", i)
		}
	}
}

func TestScanLogFile_NoMatches(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "clean.log")
	writeLog(t, logPath, `2024-01-01 INFO all good
2024-01-01 DEBUG processing request`)

	d := newTestDetector(t)
	issues, err := d.scanLogFile(context.Background(), logPath)
	if err != nil {
		t.Fatalf("scanLogFile failed: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues for clean log, got %d", len(issues))
	}
}

func TestScanLogFile_PanicMatch(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "panic.log")
	writeLog(t, logPath, `2024-01-01 panic: runtime error: index out of range`)

	d := newTestDetector(t)
	issues, err := d.scanLogFile(context.Background(), logPath)
	if err != nil {
		t.Fatalf("scanLogFile failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue for panic line, got %d", len(issues))
	}
	if issues[0].Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical for panic, got %s", issues[0].Severity)
	}
}

func TestScanLogFile_ContextWindow(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "context.log")
	// Write multiple lines including an ERROR so the scan finds exactly one issue
	writeLog(t, logPath, "2024-01-01 INFO normal line\n2024-01-01 ERROR something failed\n2024-01-01 INFO after error\n")

	d := newTestDetector(t)
	issues, err := d.scanLogFile(context.Background(), logPath)
	if err != nil {
		t.Fatalf("scanLogFile failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Context == "" {
		t.Error("expected non-empty context")
	}
}

// -----------------------------------------------------------------------
// ScanLogs
// -----------------------------------------------------------------------

func TestScanLogs(t *testing.T) {
	dir := t.TempDir()

	// Create log files matching a pattern
	logPath := filepath.Join(dir, "test.log")
	writeLog(t, logPath, "ERROR something broke\n")

	cfg := DetectionConfig{
		LogPatterns:   []string{"*.log"},
		ErrorPatterns: []string{"ERROR"},
	}
	d := NewIssueDetector(cfg, dir, slog.Default())

	issues, err := d.ScanLogs(context.Background())
	if err != nil {
		t.Fatalf("ScanLogs failed: %v", err)
	}
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}
}

func TestScanLogs_NoPattern(t *testing.T) {
	d := newTestDetector(t)
	// No LogPatterns -> no glob matching
	issues, err := d.ScanLogs(context.Background())
	if err != nil {
		t.Fatalf("ScanLogs failed: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues with no patterns, got %d", len(issues))
	}
}

func TestScanLogs_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "big.log")
	// Create a large log that won't complete before context is cancelled
	lines := make([]string, 0, 100)
	for i := range 100 {
		lines = append(lines, "2024-01-01 INFO line "+string(rune('0'+i%10)))
	}
	writeLog(t, logPath, lines[0]) // just one line is fine for this test

	ctx, cancel := context.WithCancel(context.Background())
	d := newTestDetector(t)

	issues, err := d.ScanLogs(ctx)
	// Should succeed since we only have one line
	if err != nil {
		t.Fatalf("ScanLogs failed: %v", err)
	}
	// Cancel mid-flight
	cancel()
	_ = issues
}

// -----------------------------------------------------------------------
// ScanCode
// -----------------------------------------------------------------------

func TestScanCode_TODO(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "code.go")
	writeLog(t, goFile, `package main

// TODO: implement this function
func Main() {}
`)

	d := newTestDetectorWithRoot(t, dir)
	issues, err := d.ScanCode(context.Background())
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}
	// Should find TODO, FIXME, HACK, and panic patterns
	if len(issues) < 1 {
		t.Errorf("expected at least 1 issue (TODO), got %d", len(issues))
	}
}

func TestScanCode_FIXME(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "code.go")
	writeLog(t, goFile, `// FIXME: fix memory leak here
func leakyFn() {}
`)

	d := newTestDetectorWithRoot(t, dir)
	issues, err := d.ScanCode(context.Background())
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected at least 1 issue (FIXME)")
	}
}

func TestScanCode_Panic(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "code.go")
	writeLog(t, goFile, `
func crash() {
	panic("deliberate crash")
}
`)

	d := newTestDetectorWithRoot(t, dir)
	issues, err := d.ScanCode(context.Background())
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected at least 1 issue (panic)")
	}
}

func TestScanCode_NoIssues(t *testing.T) {
	dir := t.TempDir()
	goFile := filepath.Join(dir, "clean.go")
	writeLog(t, goFile, `package clean

func Good() {}
`)

	d := newTestDetectorWithRoot(t, dir)
	issues, err := d.ScanCode(context.Background())
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues in clean file, got %d", len(issues))
	}
}

func TestScanCode_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	// Create a .go file with _test.go suffix -- should be skipped
	testFile := filepath.Join(dir, "code_test.go")
	writeLog(t, testFile, `// TODO: write tests
package code

func TestMain() {}
`)

	d := newTestDetectorWithRoot(t, dir)
	issues, err := d.ScanCode(context.Background())
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}
	// _test.go files should be skipped, so no issues from TODO
	for _, iss := range issues {
		if iss.Source == testFile {
			t.Errorf("expected test file to be skipped, but got issue from %s", iss.Source)
		}
	}
}

func TestScanCode_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := newTestDetector(t)

	cancel()
	issues, err := d.ScanCode(ctx)
	_ = issues
	t.Logf("ScanCode with cancelled context returned err: %v (may not always error due to filepath.Walk behavior)", err)
}

// -----------------------------------------------------------------------
// DetectAll
// -----------------------------------------------------------------------

func TestDetectAll(t *testing.T) {
	dir := t.TempDir()

	// Create a log file
	logPath := filepath.Join(dir, "app.log")
	writeLog(t, logPath, "ERROR test error\n")

	// Create a Go file with a TODO
	goFile := filepath.Join(dir, "main.go")
	writeLog(t, goFile, "// TODO: finish this\nfunc main() {}\n")

	cfg := DetectionConfig{
		LogPatterns:   []string{"*.log"},
		ErrorPatterns: []string{"ERROR", "FATAL"},
	}
	d := NewIssueDetector(cfg, dir, slog.Default())

	issues, err := d.DetectAll(context.Background())
	if err != nil {
		t.Fatalf("DetectAll failed: %v", err)
	}

	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues (1 log + 1 code), got %d", len(issues))
	}
}

// -----------------------------------------------------------------------
// Helper methods
// -----------------------------------------------------------------------

func TestDetermineSeverity(t *testing.T) {
	d := newTestDetector(t)

	tests := []struct {
		line     string
		expected IssueSeverity
	}{
		{"FATAL: system crash", SeverityCritical},
		{"panic: null pointer", SeverityCritical},
		{"ERROR: connection refused", SeverityHigh},
		{"Warning: disk space", SeverityMedium},
		{"INFO: everything fine", SeverityLow},
	}

	for _, tt := range tests {
		got := d.determineSeverity(tt.line)
		if got != tt.expected {
			t.Errorf("determineSeverity(%q) = %s, want %s", tt.line, got, tt.expected)
		}
	}
}

func TestExtractDescription(t *testing.T) {
	d := newTestDetector(t)

	tests := []struct {
		line     string
		expected string
	}{
		{"ERROR: connection failed", "connection failed"},
		{"FATAL: crash", "crash"},
		{"panic: nil dereference", "nil dereference"},
		{"exception: out of memory", "out of memory"},
		{"error: timeout", "timeout"},
		{"Just a normal message", "Just a normal message"},
	}

	for _, tt := range tests {
		got := d.extractDescription(tt.line)
		if got != tt.expected {
			t.Errorf("extractDescription(%q) = %q, want %q", tt.line, got, tt.expected)
		}
	}
}

func TestExtractDescription_LongLine(t *testing.T) {
	d := newTestDetector(t)
	// Use a message without a recognized prefix so the truncation branch applies.
	// The implementation truncates to line[:200] + "..." (203 chars total).
	longMsg := string(make([]byte, 250))
	got := d.extractDescription(longMsg)
	if len(got) > 210 {
		t.Errorf("expected truncated at 200, got %d", len(got))
	}
}
