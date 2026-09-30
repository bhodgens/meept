package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Golden render harness (docs/plans/2026-09-29-coverage-completion/02-tui-e2e/
// 01-phase1-foundations.md): render View() after a scripted message
// sequence at a FIXED size, compare against
// internal/tui/testdata/golden/<name>.txt.
//
// Regenerate with:  go test ./internal/tui/ -run TestGolden -update-golden
//
// Determinism rules (tui-e2e-plan.md §3.3 hazards):
//   - Every golden renders at fixed WindowSizeMsg (80x24 unless noted).
//   - No transient status messages: a statusMessage set with
//     time.Now() expires via wall-clock comparison and tea.Tick. Goldens
//     either avoid triggering status messages or deliver the
//     StatusMessageClearMsg before snapshotting (resetStatus clears the
//     line without the wall-clock race).
//   - Relative-time cells (sessions "activity" column via
//     formatRelativeTime) are wall-clock derived: golden fixtures seed
//     last_activity far enough in the past that "Xd ago" is stable for
//     the life of the repo (>= 30d renders the fixed "Jan 2" date, but
//     that is still wall-clock; so fixtures use the >=30d branch AND
//     stripRows is unnecessary — the date only changes if the machine
//     clock is years off; accepted hazard, documented here).
//   - Chat message headers carry HH:MM timestamps (time.Now() at
//     addMessage). Golden chat fixtures therefore assert content via the
//     post-processor instead of exact-match: the loaded-chat golden is
//     captured with the stripRules below applied (timestamp headers
//     stripped) so the body is stable.
//
// Strip rules applied BEFORE compare (documented per the leaf contract):
//   - ANSI escape sequences are stripped (styles are asserted by the
//     parity test, not by goldens).
//   - Chat timestamp headers match `\d\d:\d\d` at line start after the
//     bullet pointer and are replaced with "HH:MM".
var updateGolden = flag.Bool("update-golden", false, "rewrite golden files in internal/tui/testdata/golden/")

const goldenDir = "testdata/golden"

// goldenName is the fixture file for one golden case.
func goldenPath(name string) string {
	return filepath.Join(goldenDir, name+".txt")
}

// normalizeGoldenView applies the documented strip rules to a raw
// View() content string.
func normalizeGoldenView(t *testing.T, raw string) string {
	t.Helper()
	stripped := ansi.Strip(raw)
	// Chat HH:MM timestamps: replace any 2-digit-colon-2-digit token so
	// wall-clock message headers never break the golden.
	stripped = replaceClockTokens(stripped)
	// Trim trailing whitespace per line + trailing blank lines: lipgloss
	// padding varies with terminal quirks but content must not.
	lines := strings.Split(stripped, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// replaceClockTokens rewrites HH:MM tokens to a fixed placeholder.
func replaceClockTokens(s string) string {
	var b strings.Builder
	runes := []rune(s)
	isDigit := func(r rune) bool { return r >= '0' && r <= '9' }
	for i := 0; i < len(runes); i++ {
		if i+4 < len(runes)+1 && i+5 <= len(runes) &&
			isDigit(runes[i]) && isDigit(runes[i+1]) &&
			runes[i+2] == ':' &&
			isDigit(runes[i+3]) && isDigit(runes[i+4]) {
			// Boundary guard: not part of a longer digit/colon run
			// (e.g. 12:34:56 or 112:34).
			prevOK := i == 0 || !isDigit(runes[i-1])
			nextOK := i+5 >= len(runes) || !isDigit(runes[i+5])
			if prevOK && nextOK {
				b.WriteString("HH:MM")
				i += 4
				// Swallow a trailing :SS too (HH:MM:SS renders appear
				// in plan/detail timestamps) so seconds never leak
				// into a golden.
				if i+2 < len(runes) && runes[i+1] == ':' &&
					isDigit(runes[i+2]) && isDigit(runes[i+3]) &&
					(i+4 >= len(runes) || !isDigit(runes[i+4])) {
					b.WriteString(":SS")
					i += 3
				}
				continue
			}
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// assertGolden compares normalized content against the fixture,
// writing it when -update-golden is set. Lowercase enforcement lives
// here (AGENTS.md UI rule): every captured golden must be lowercase.
func assertGolden(t *testing.T, name, content string) {
	t.Helper()
	normalized := normalizeGoldenView(t, content)
	// Lowercase-text check (AGENTS.md: all UI text must be lowercase).
	// The rule governs authored UI TEXT; existing shipped labels that
	// predate the rule and keyboard-combo hints are checked against a
	// fixed allowlist so NEW uppercase text still fails the golden.
	// Keyboard combos (^X, ^S, ^P, ^C), the "Task:"/"Steps:" detail
	// labels, and the "Make sure the meept daemon is running:" error
	// line are current shipped strings — a deviation to fix in product
	// code, not in the golden checker (documented per the parity rule:
	// deviations must be explicit, not silent).
	lowercaseExemptions := []string{
		"^X ", "^S ", "^P ", "^C ", "^X y", "^B ", "^V ",
		"Task: ", "Steps: ",
		"Make sure the meept daemon is running:",
		"Aug ", "Jan ", // month abbreviations in date cells (time.Format)
		"HH:MM", ":SS", // clock placeholders from replaceClockTokens
		"D: delete", "R: retry",
	}
	for i, line := range strings.Split(normalized, "\n") {
		probe := line
		for _, ex := range lowercaseExemptions {
			probe = strings.ReplaceAll(probe, ex, "")
		}
		if idx := strings.IndexFunc(probe, func(r rune) bool { return r >= 'A' && r <= 'Z' }); idx >= 0 {
			t.Errorf("golden %s line %d contains uppercase UI text (AGENTS.md rule): %q",
				name, i+1, line)
		}
	}

	path := goldenPath(name)
	if *updateGolden {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(normalized+"\n"), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing (%s): run `go test ./internal/tui/ -run TestGolden -update-golden` and commit the fixture: %v",
			name, path, err)
	}
	wantStr := strings.TrimRight(string(want), "\n")
	if wantStr != normalized {
		// Debug aid: persist the actual render next to the fixture
		// (gitignored) so a flake can be diffed offline.
		_ = os.WriteFile(goldenPath(name)+".got.txt", []byte(normalized+"\n"), 0o644)
		t.Errorf("golden %s mismatch against %s\n--- want ---\n%s\n--- got ---\n%s\n(regenerate with -update-golden if intentional)",
			name, path, wantStr, normalized)
	}
}

// resetStatusApp clears any transient status message without racing the
// wall-clock: it writes both state fields directly on the (finished) app.
func resetStatusApp(app *App) {
	app.statusMessage = ""
	app.statusMessageTime = time.Now().Add(-time.Hour)
}


// settleAsync gives async cmd goroutines (fetch results that land outside
// any event-loop ordering — e.g. the session plans panel's "loading..." →
// "no plans" flip) time to deliver before finish()+assert. A bounded sleep
// is deliberately used INSTEAD of polling View() on the live model: reads
// while the loop runs would race it (bubbletea's own startup resize can
// land after any send barrier). The stub RPC server answers in
// microseconds, so 100ms is generous.
const settleAsyncDelay = 100 * time.Millisecond

func settleAsync() { time.Sleep(settleAsyncDelay) }

// captureView clears the transient status message (a wall-clock line —
// see the strip rules above) and returns the normalized View() content.
// app must be finished (loop stopped) — see headlessProgram.finish.
func captureView(t *testing.T, app *App) string {
	t.Helper()
	resetStatusApp(app)
	return normalizeGoldenView(t, app.View().Content)
}
