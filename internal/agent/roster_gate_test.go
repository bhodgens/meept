package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/runtime"
)

// recordingBackend is a stub runtime.ExecutionBackend that returns canned
// results and records every executed command.
type recordingBackend struct {
	mu       sync.Mutex
	results  []*runtime.CommandResult
	commands []string
}

func (b *recordingBackend) Execute(_ context.Context, cmd runtime.Command) (*runtime.CommandResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commands = append(b.commands, cmd.Cmd)
	res := b.results[0]
	b.results = b.results[1:]
	return res, nil
}

func (b *recordingBackend) Name() string { return "recording" }

func (b *recordingBackend) Close() error { return nil }

// execCount reports how many gate commands ran.
func (b *recordingBackend) execCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.commands)
}

func TestRosterGateEvaluateTable(t *testing.T) {
	type rosterGateTestCase struct {
		name            string
		cfg             RosterGateConfig
		nilGate         bool // evaluate via nil receiver (no gate on spec)
		turnHadMutating bool
		exitCode        int
		gateOutput      string
		wantPassed      bool
		wantSkipped     bool
		wantErr         bool
		wantCommandRun  bool // gate command handed to backend
	}
	tests := []rosterGateTestCase{
		{
			name:            "no gate config skips",
			cfg:             RosterGateConfig{},
			turnHadMutating: true,
			wantSkipped:     true,
			wantCommandRun:  false,
		},
		{
			name:            "nil gate skips",
			cfg:             RosterGateConfig{Command: "true"},
			nilGate:         true,
			turnHadMutating: true,
			wantSkipped:     true,
			wantCommandRun:  false,
		},
		{
			name:            "read-only turn skips",
			cfg:             RosterGateConfig{Command: "true"},
			turnHadMutating: false,
			wantSkipped:     true,
			wantCommandRun:  false,
		},
		{
			name:            "mutating turn runs and passes",
			cfg:             RosterGateConfig{Command: "true"},
			turnHadMutating: true,
			exitCode:        0,
			wantPassed:      true,
			wantCommandRun:  true,
		},
		{
			name:            "mutating turn gate fail blocks",
			cfg:             RosterGateConfig{Command: "false"},
			turnHadMutating: true,
			exitCode:        1,
			gateOutput:      "FAIL: tests failed",
			wantPassed:      false,
			wantCommandRun:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g *RosterGate
			if !tt.nilGate {
				g = NewRosterGate(tt.cfg)
			}
			if tt.cfg.Command == "" && g != nil {
				t.Fatalf("NewRosterGate(empty) = non-nil, want nil")
			}
			if g == nil && !tt.nilGate && tt.cfg.Command != "" {
				t.Fatalf("NewRosterGate(%q) = nil, want non-nil", tt.cfg.Command)
			}

			var (
				passed, skipped bool
				output          string
				err             error
			)
			if g != nil {
				be := &recordingBackend{results: []*runtime.CommandResult{
					{Output: "gitstatus\x00HEAD\x00", ExitCode: 0}, // git status
					{Output: "head\x00", ExitCode: 0},              // git rev-parse
					{Output: tt.gateOutput, ExitCode: tt.exitCode}, // the gate command
				}}
				g.SetBackend(be)
				passed, output, skipped, err = g.Evaluate(context.Background(), t.TempDir(), tt.turnHadMutating)
				if got := be.execCount(); (got > 0) != tt.wantCommandRun {
					t.Fatalf("gate command executed %d times, wantCommandRun=%v", got, tt.wantCommandRun)
				}
			} else {
				// nil gate: Evaluate must be callable and skip.
				var gnil *RosterGate
				passed, output, skipped, err = gnil.Evaluate(context.Background(), t.TempDir(), tt.turnHadMutating)
			}

			if (err != nil) != tt.wantErr {
				t.Fatalf("Evaluate err = %v, wantErr %v", err, tt.wantErr)
			}
			if passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", passed, tt.wantPassed)
			}
			if skipped != tt.wantSkipped {
				t.Errorf("skipped = %v, want %v", skipped, tt.wantSkipped)
			}
			if !tt.wantPassed && !tt.wantSkipped && !strings.Contains(output, tt.gateOutput) {
				t.Errorf("output = %q, want to contain %q", output, tt.gateOutput)
			}
		})
	}
}

func TestRosterGateSkipWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	hashBackend := func() *recordingBackend {
		return &recordingBackend{results: []*runtime.CommandResult{
			{Output: "", ExitCode: 0},       // git status --porcelain (clean)
			{Output: "abc123", ExitCode: 0}, // git rev-parse HEAD
		}}
	}

	g := NewRosterGate(RosterGateConfig{Command: "false", SkipWhenUnchanged: true})

	// First run: gate command fails.
	be1 := hashBackend()
	be1.results = append(be1.results, &runtime.CommandResult{Output: "boom", ExitCode: 1})
	g.SetBackend(be1)
	passed1, out1, skipped1, err := g.Evaluate(context.Background(), dir, true)
	if err != nil || passed1 || skipped1 {
		t.Fatalf("first evaluate: passed=%v skipped=%v err=%v out=%q", passed1, skipped1, err, out1)
	}

	// Second identical failure, unchanged workspace: RunGate must skip the
	// re-run (only the two hash commands run, NOT the gate command).
	be2 := hashBackend()
	g.SetBackend(be2)
	passed2, out2, skipped2, err := g.Evaluate(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("second evaluate: %v", err)
	}
	if !skipped2 {
		t.Fatalf("second evaluate skipped = false, want true (out=%q)", out2)
	}
	if passed2 {
		t.Fatalf("second evaluate passed = true, want false")
	}
	if be2.execCount() != 2 {
		t.Fatalf("second evaluate ran %d commands, want 2 (hash only, gate skipped)", be2.execCount())
	}
}

func TestRosterGateTruncatesOutput(t *testing.T) {
	long := strings.Repeat("x", rosterGateMaxOutput+1000)
	got := truncateRosterGateOutput(long)
	if len(got) > rosterGateMaxOutput+100 {
		t.Fatalf("output not truncated: %d bytes", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncation marker missing")
	}
	if short := truncateRosterGateOutput("short"); short != "short" {
		t.Errorf("short output mangled: %q", short)
	}
}

func TestRosterGateFailureMessage(t *testing.T) {
	msg := rosterGateFailureMessage("go test ./...", "FAIL pkg 0.5s")
	if !strings.Contains(msg, "go test ./...") || !strings.Contains(msg, "FAIL pkg 0.5s") {
		t.Errorf("failure message missing command/output:\n%s", msg)
	}
	if len(msg) > rosterGateMaxOutput+200 {
		t.Errorf("failure message exceeds cap: %d bytes", len(msg))
	}
}

// TestRosterGateWorkdirIsSessionDir asserts the workdir handed to the gate
// backend is the one passed to Evaluate (the loop's session workdir), never
// derived from os.Getwd.
func TestRosterGateWorkdirIsSessionDir(t *testing.T) {
	dir := t.TempDir()
	g := NewRosterGate(RosterGateConfig{Command: "true"})
	be := &workdirRecorder{dir: dir}
	g.SetBackend(be)
	if _, _, _, err := g.Evaluate(context.Background(), dir, true); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(be.seenDirs) == 0 {
		t.Fatal("backend saw no commands")
	}
	for _, d := range be.seenDirs {
		if d != dir {
			t.Errorf("command ran in %q, want session dir %q", d, dir)
		}
	}
}

type workdirRecorder struct {
	dir      string
	seenDirs []string
}

func (w *workdirRecorder) Execute(_ context.Context, cmd runtime.Command) (*runtime.CommandResult, error) {
	w.seenDirs = append(w.seenDirs, cmd.Dir)
	return &runtime.CommandResult{Output: "", ExitCode: 0}, nil
}

func (w *workdirRecorder) Name() string { return "workdir-recorder" }

func (w *workdirRecorder) Close() error { return nil }
