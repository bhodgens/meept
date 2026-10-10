package skills

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureLogger returns a logger writing into buf so a test can count lines.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// writeSkillDir creates n frontmatter-less skill files under a temp tier.
func writeSkillDir(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		// No YAML frontmatter: ParseSkillFile falls back to the slug as the name.
		// Distinct slug per file so the count assertion is exact; the earlier
		// version reused a letter and collided, loading fewer skills than n.
		body := "# Skill " + itoa(i) + "\n\nSome guidance.\n"
		name := "skill-number-" + itoa(i)
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600); err != nil {
			t.Fatalf("write skill %d: %v", i, err)
		}
	}
	return dir
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestFrontmatterWarningsAreAggregatedPerScan is the regression pin.
//
// Measured 2026-10-10 on the operator's live daemon: this condition was 40.9%
// of the entire log (~7,500 of 18,000 sampled lines), because
// FileSource.Discover runs ~253 times per daemon boot and each run logged one
// WARN per skill. The fix records the paths during a scan and emits ONE
// aggregated WARN at the end of that scan.
//
// This test asserts the aggregation holds for N skills, not just that some
// warning is produced.
func TestFrontmatterWarningsAreAggregatedPerScan(t *testing.T) {
	const n = 25
	dir := writeSkillDir(t, n)

	var buf bytes.Buffer
	src := NewFileSource([]DiscoveryTier{{Path: dir, Priority: 1}}, captureLogger(&buf))

	// NOTE: ParseSkillFile returns a valid slug-derived entry ALONGSIDE
	// ErrNoFrontmatter, but loadSkillFile treats the error branch as fatal and
	// skips the file. So a frontmatter-less file yields a WARNING here, not a
	// loaded skill — the skill is skipped, which is exactly why the condition
	// is worth one aggregated line rather than N.
	skills, err := src.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("frontmatter-less files should be skipped, got %d skills loaded", len(skills))
	}

	out := buf.String()
	warns := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "no frontmatter") {
			warns++
		}
	}
	// Exactly one aggregated line per scan, regardless of n.
	if warns != 1 {
		t.Fatalf("expected 1 aggregated frontmatter warning for %d skills, got %d\n%s",
			n, warns, out)
	}
	// The count must be reported so the information is not lost.
	if !strings.Contains(out, "count=25") && !strings.Contains(out, "count=\"25\"") {
		t.Fatalf("aggregated line does not report count=25:\n%s", out)
	}
	// And it must name the scan it belongs to.
	if !strings.Contains(out, "scan=discover") {
		t.Fatalf("aggregated line does not identify its scan:\n%s", out)
	}
}

// TestFrontmatterWarningSamplesPathsBounded proves the summary stays bounded
// when many skills are missing frontmatter: a sample plus a truncation count,
// never one path per skill.
func TestFrontmatterWarningSamplesPathsBounded(t *testing.T) {
	const n = 40
	dir := writeSkillDir(t, n)

	var buf bytes.Buffer
	src := NewFileSource([]DiscoveryTier{{Path: dir, Priority: 1}}, captureLogger(&buf))
	if _, err := src.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "paths_truncated") {
		t.Fatalf("expected a bounded sample with paths_truncated for %d skills:\n%s", n, out)
	}
	// At most 5 paths are listed, so the line stays short.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "paths=") && strings.Count(line, ".md") > 5 {
			t.Fatalf("aggregated line lists every path (%d); it must sample:\n%s",
				strings.Count(line, ".md"), line)
		}
	}
}

// TestNoFrontmatterWarningWhenAllSkillsAreWellFormed proves the aggregator does
// not emit a spurious warning for the normal case.
func TestNoFrontmatterWarningWhenAllSkillsAreWellFormed(t *testing.T) {
	dir := t.TempDir()
	good := "---\nname: proper-skill\ndescription: has frontmatter\n---\n\nBody.\n"
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, "good"+itoa(i)+".md"),
			[]byte(good), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	var buf bytes.Buffer
	src := NewFileSource([]DiscoveryTier{{Path: dir, Priority: 1}}, captureLogger(&buf))
	if _, err := src.Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if strings.Contains(buf.String(), "no frontmatter") {
		t.Fatalf("spurious frontmatter warning for well-formed skills:\n%s", buf.String())
	}
}

// TestMetadataScanAlsoAggregates proves the metadata-only path (which has no
// summary line of its own) flushes too, so it cannot regress into per-skill
// spam on its own.
func TestMetadataScanAlsoAggregates(t *testing.T) {
	const n = 12
	dir := writeSkillDir(t, n)

	var buf bytes.Buffer
	src := NewFileSource([]DiscoveryTier{{Path: dir, Priority: 1}}, captureLogger(&buf))
	if _, err := src.DiscoverMetadata(context.Background()); err != nil {
		t.Fatalf("DiscoverMetadata: %v", err)
	}
	out := buf.String()
	warns := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "no frontmatter") {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("metadata scan emitted %d warnings for %d skills, want 1:\n%s", warns, n, out)
	}
	if !strings.Contains(out, "scan=metadata") {
		t.Fatalf("metadata aggregate does not identify its scan:\n%s", out)
	}
}

// TestAccumulatorResetsBetweenScans proves two consecutive scans each emit
// their own line: a stale accumulator would either duplicate or suppress.
func TestAccumulatorResetsBetweenScans(t *testing.T) {
	dir := writeSkillDir(t, 6)

	var buf bytes.Buffer
	src := NewFileSource([]DiscoveryTier{{Path: dir, Priority: 1}}, captureLogger(&buf))
	for i := 0; i < 3; i++ {
		if _, err := src.Discover(context.Background()); err != nil {
			t.Fatalf("Discover %d: %v", i, err)
		}
	}
	warns := 0
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "no frontmatter") {
			warns++
		}
	}
	if warns != 3 {
		t.Fatalf("3 scans produced %d aggregated warnings, want 3 (accumulator leaked or was not reset)", warns)
	}
}
