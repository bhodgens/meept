package mutation

// Regression tests for the RunFileMutations repair (observation 3).
//
// The pre-repair runner accepted `testFn func() error` and invoked the
// callback with no argument, while writing mutants to shared os.TempDir
// paths (mutation_%d.go) with the os.WriteFile error ignored — so the
// callback could only ever re-run against the ORIGINAL code and the report
// silently measured nothing. These tests pin the repaired contract:
//
//   - testFn is func(sourcePath string) error
//   - the baseline callback receives the ORIGINAL file and must pass
//   - each mutant is written to a fresh file under t.TempDir with the write
//     error checked; the original file is never modified
//   - a mutant the suite misses is reported as survived, a caught one as
//     killed
//   - zero applicable mutations yields a zero-count report (score 0, no NaN)
//
// The runner is invoked through reflection (callRunFileMutations) so that a
// regression against the pre-repair signature fails at runtime with a clear
// message instead of failing to compile.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// gateSource is the mutation target: one invertible condition (`if ok {`).
const gateSource = `package mutt

// Gate returns "open" only when ok is true.
func Gate(ok bool) string {
	if ok {
		return "open"
	}
	return "closed"
}
`

// gateTestSource is the suite that validates Gate and catches the inverted
// condition at runtime (assertion failure, not a compile failure).
const gateTestSource = `package mutt

import "testing"

func TestGate(t *testing.T) {
	if got := Gate(true); got != "open" {
		t.Fatalf("Gate(true) = %q, want %q", got, "open")
	}
	if got := Gate(false); got != "closed" {
		t.Fatalf("Gate(false) = %q, want %q", got, "closed")
	}
}
`

// compileAndTest returns a repaired-signature callback: it materializes the
// Go source file it is GIVEN into an isolated throwaway module and runs
// `go test` on it fully offline. A non-nil error means the module failed to
// build or a test assertion failed; the error carries the `go test` output.
func compileAndTest(t *testing.T) func(sourcePath string) error {
	t.Helper()
	return func(sourcePath string) error {
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", sourcePath, err)
		}
		dir := t.TempDir()
		files := map[string]string{
			"go.mod":       "module mutt\n\ngo 1.26\n",
			"gate.go":      string(data),
			"gate_test.go": gateTestSource,
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "test", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("go test for %s failed: %w\n%s", sourcePath, err, out)
		}
		return nil
	}
}

// callRunFileMutations invokes RunFileMutations via reflection, validating
// that its third parameter is func(string) error. Against the pre-repair
// `func() error` signature this returns a descriptive error instead of a
// compile failure, so the regression has an observable RED state.
func callRunFileMutations(t *testing.T, filePath string, fn func(sourcePath string) error) (report *MutationReport, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			report, err = nil, fmt.Errorf("RunFileMutations panicked: %v", r)
		}
	}()

	fv := reflect.ValueOf(RunFileMutations)
	ft := fv.Type()

	switch {
	case ft.Kind() != reflect.Func:
		return nil, fmt.Errorf("RunFileMutations is %s, want func", ft.Kind())
	case ft.NumIn() != 3:
		return nil, fmt.Errorf("RunFileMutations has %d params, want 3 (t, filePath, testFn)", ft.NumIn())
	case ft.In(2).Kind() != reflect.Func:
		return nil, fmt.Errorf("RunFileMutations param 3 is %s, want func", ft.In(2))
	}

	cb := ft.In(2)
	errType := reflect.TypeOf((*error)(nil)).Elem()
	if cb.NumIn() != 1 || cb.In(0).Kind() != reflect.String ||
		cb.NumOut() != 1 || cb.Out(0) != errType {
		return nil, fmt.Errorf("RunFileMutations testFn param is %s, want func(sourcePath string) error", cb)
	}

	out := fv.Call([]reflect.Value{reflect.ValueOf(t), reflect.ValueOf(filePath), reflect.ValueOf(fn)})
	if len(out) != 1 {
		return nil, fmt.Errorf("RunFileMutations returned %d values, want 1", len(out))
	}
	rep, _ := out[0].Interface().(*MutationReport)
	if rep == nil {
		return nil, fmt.Errorf("RunFileMutations returned a nil report")
	}
	return rep, nil
}

// TestRunFileMutationsRepairedCallback pins the full repaired contract:
// baseline on the original file, exactly one condition mutant exercised in
// a separate temp file, the mutant killed by an actual test assertion
// failure (not a compile failure), and the original file untouched.
func TestRunFileMutationsRepairedCallback(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "gate.go")
	if err := os.WriteFile(srcPath, []byte(gateSource), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	var gotPaths []string
	base := compileAndTest(t)
	testFn := func(sourcePath string) error {
		gotPaths = append(gotPaths, sourcePath)
		return base(sourcePath)
	}

	report, err := callRunFileMutations(t, srcPath, testFn)
	if err != nil {
		t.Fatalf("RunFileMutations: %v", err)
	}

	if len(gotPaths) < 2 {
		t.Fatalf("callback invoked %d time(s), want >= 2 (baseline + mutant)", len(gotPaths))
	}
	if gotPaths[0] != srcPath {
		t.Fatalf("baseline callback got %q, want the ORIGINAL file %q", gotPaths[0], srcPath)
	}

	if report.TotalMutations != 1 {
		t.Fatalf("TotalMutations = %d, want 1 (one `if ok {` line); report:\n%s", report.TotalMutations, report)
	}
	if report.KilledMutations != 1 || report.SurvivedMutations != 0 {
		t.Fatalf("got killed=%d survived=%d, want 1/0; report:\n%s",
			report.KilledMutations, report.SurvivedMutations, report)
	}

	res := report.Results[0]
	if res.TestPassed {
		t.Fatalf("mutant SURVIVED: the suite must catch the inverted condition; err=%v", res.Error)
	}
	if res.Error == nil {
		t.Fatal("killed mutant result carries no error")
	}
	out := res.Error.Error()
	if !strings.Contains(out, "--- FAIL") {
		t.Fatalf("mutant failure is not a test assertion failure (want `--- FAIL` from go test):\n%s", out)
	}
	for _, compileOnly := range []string{"syntax error", "build failed", "expected declaration", "internal compiler error"} {
		if strings.Contains(out, compileOnly) {
			t.Fatalf("mutant failed to COMPILE instead of failing assertions:\n%s", out)
		}
	}

	mutantPath := gotPaths[1]
	if mutantPath == srcPath {
		t.Fatal("mutant callback received the original source path")
	}
	mutantBytes, err := os.ReadFile(mutantPath)
	if err != nil {
		t.Fatalf("read mutant %s: %v", mutantPath, err)
	}
	if !strings.Contains(string(mutantBytes), "if !(ok) {") {
		t.Fatalf("mutant file %s does not contain the inverted condition:\n%s", mutantPath, mutantBytes)
	}

	after, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("re-read original: %v", err)
	}
	if string(after) != gateSource {
		t.Fatal("original source file was modified by RunFileMutations")
	}
}

// TestRunFileMutationsReportsSurvived verifies the missed-mutant path: a
// callback that always passes means the suite missed the mutation, and the
// report must count it as survived (not killed, not dropped).
func TestRunFileMutationsReportsSurvived(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "gate.go")
	if err := os.WriteFile(srcPath, []byte(gateSource), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	calls := 0
	report, err := callRunFileMutations(t, srcPath, func(string) error {
		calls++
		return nil // always passes: mutant survives
	})
	if err != nil {
		t.Fatalf("RunFileMutations: %v", err)
	}
	if calls < 2 {
		t.Fatalf("callback invoked %d time(s), want >= 2 (baseline + mutant)", calls)
	}
	if report.SurvivedMutations != 1 || report.KilledMutations != 0 {
		t.Fatalf("always-passing callback must count as survived: got killed=%d survived=%d",
			report.KilledMutations, report.SurvivedMutations)
	}
}

// TestRunFileMutationsNoMutations verifies a file with no mutable lines
// yields a zero-count report with a zero score (never a NaN from 0/0).
func TestRunFileMutationsNoMutations(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "plain.go")
	src := "package plain\n\n// No conditions here, nothing for the invert mutator.\n"
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	calls := 0
	report, err := callRunFileMutations(t, srcPath, func(string) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("RunFileMutations: %v", err)
	}
	if report.TotalMutations != 0 || len(report.Results) != 0 {
		t.Fatalf("TotalMutations=%d len(Results)=%d, want 0/0", report.TotalMutations, len(report.Results))
	}
	if report.MutationScore != 0 {
		t.Fatalf("MutationScore = %v, want 0 (no NaN on zero mutations)", report.MutationScore)
	}
	if calls != 1 {
		t.Fatalf("callback invoked %d time(s), want 1 (baseline only)", calls)
	}
}
