package llm

// Tests for the env-script resolver (envscript.go). Every test uses SENTINEL
// variable names (MEEPT_TEST_*) and sentinel values only — never real keys.
// Values are asserted on as data returned to the caller, never logged; the
// logging test asserts values do NOT reach the log while variable NAMES may.

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateResolver points MEEPT_HOME at a temp dir and resets the global
// resolver + env cache around each test.
func isolateResolver(t *testing.T) string {
	t.Helper()
	prevHome, hadHome := os.LookupEnv("MEEPT_HOME")
	dir := t.TempDir()
	t.Setenv("MEEPT_HOME", dir)
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("MEEPT_HOME", prevHome)
		} else {
			os.Unsetenv("MEEPT_HOME")
		}
	})
	return dir
}

// writeEnvScript installs an executable env script with the given body.
func writeEnvScript(t *testing.T, home, body string) {
	t.Helper()
	path := filepath.Join(home, EnvScriptName)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

// unsetSentinel makes sure the process environment does not carry the test
// variable (CI shells may export odd things).
func unsetSentinel(t *testing.T, name string) {
	t.Helper()
	prev, had := os.LookupEnv(name)
	os.Unsetenv(name)
	t.Cleanup(func() {
		if had {
			os.Setenv(name, prev)
		}
	})
}

func TestEnvScriptEnvPrecedence(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	// Script would provide a DIFFERENT value; it must never be consulted
	// because the process environment has the variable.
	writeEnvScript(t, home, "#!/bin/sh\necho from-script\n")
	r := newEnvScriptResolver()
	if r.scriptPath == "" {
		t.Fatal("env script not detected")
	}

	t.Setenv("MEEPT_TEST_VAR", "from-env")
	got, ok := r.Resolve("MEEPT_TEST_VAR")
	if !ok || got != "from-env" {
		t.Errorf("env precedence broken: got %q ok=%v, want from-env", got, ok)
	}
}

func TestEnvScriptFallbackWhenEnvLacksVar(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	writeEnvScript(t, home, "#!/bin/sh\necho from-script\n")
	r := newEnvScriptResolver()

	got, ok := r.Resolve("MEEPT_TEST_VAR")
	if !ok || got != "from-script" {
		t.Errorf("script fallback broken: got %q ok=%v, want from-script", got, ok)
	}
}

func TestEnvScriptMissingScriptIsNotFound(t *testing.T) {
	isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	r := newEnvScriptResolver()
	if r.scriptPath != "" {
		t.Fatal("resolver picked up a script that does not exist")
	}
	if _, ok := r.Resolve("MEEPT_TEST_VAR"); ok {
		t.Error("resolve succeeded without any env script")
	}
}

func TestEnvScriptNonExecutableIgnored(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	path := filepath.Join(home, EnvScriptName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newEnvScriptResolver()
	if r.scriptPath != "" {
		t.Error("non-executable env script was accepted")
	}
}

func TestEnvScriptNonzeroExitIsNotFound(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	writeEnvScript(t, home, "#!/bin/sh\nexit 1\n")
	r := newEnvScriptResolver()

	got, ok := r.Resolve("MEEPT_TEST_VAR")
	if ok || got != "" {
		t.Errorf("nonzero exit should be not-found, got %q ok=%v", got, ok)
	}
}

func TestEnvScriptTimeoutCountsAsNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps past the script timeout")
	}
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	// Script hangs far past the 5s budget; the test itself just waits for the
	// resolver to give up.
	writeEnvScript(t, home, "#!/bin/sh\nsleep 60\n")
	r := newEnvScriptResolver()

	start := time.Now()
	got, ok := r.Resolve("MEEPT_TEST_VAR")
	elapsed := time.Since(start)

	if ok || got != "" {
		t.Errorf("hung script should be not-found, got %q ok=%v", got, ok)
	}
	// Wall clock = kill after EnvScriptTimeout + up to WaitDelay grace for
	// the killed script's grandchild I/O. Bounded, but not instant.
	if elapsed > 3*EnvScriptTimeout {
		t.Errorf("resolver waited %v; timeout not enforced", elapsed)
	}
	if elapsed < EnvScriptTimeout/2 {
		t.Errorf("resolver returned after %v; expected it to wait out the timeout", elapsed)
	}
}

func TestEnvScriptValuesNeverLogged(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	const sentinelValue = "MEEPT_SENTINEL_SECRET_DO_NOT_LOG"
	script := "#!/bin/sh\necho " + sentinelValue + "\n"
	writeEnvScript(t, home, script)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := newEnvScriptResolver()
	got, ok := r.Resolve("MEEPT_TEST_VAR")
	if !ok || got != sentinelValue {
		t.Fatalf("script value not resolved: %q ok=%v", got, ok)
	}
	// Also force the failure paths (nonzero exit + timeout) with logging on.
	writeEnvScript(t, home, "#!/bin/sh\nexit 3\n")
	r2 := newEnvScriptResolver()
	if _, ok := r2.Resolve("MEEPT_TEST_VAR"); ok {
		t.Fatal("unexpected found after exit 3")
	}

	logged := buf.String()
	if strings.Contains(logged, sentinelValue) {
		t.Errorf("sentinel VALUE leaked into logs: %s", logged)
	}
	if !strings.Contains(logged, "MEEPT_TEST_VAR") {
		t.Errorf("diagnostics should NAME the variable, log was: %s", logged)
	}
}

func TestEnvScriptCacheRunsScriptOnce(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	// Counter script: appends a line per invocation.
	script := "#!/bin/sh\necho x >> " + filepath.Join(home, "count") + "\necho value\n"
	writeEnvScript(t, home, script)
	r := newEnvScriptResolver()

	if v, ok := r.Resolve("MEEPT_TEST_VAR"); !ok || v != "value" {
		t.Fatalf("first resolve failed: %q ok=%v", v, ok)
	}
	if v, ok := r.Resolve("MEEPT_TEST_VAR"); !ok || v != "value" {
		t.Fatalf("second resolve failed: %q ok=%v", v, ok)
	}
	data, err := os.ReadFile(filepath.Join(home, "count"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "x\n"); n != 1 {
		t.Errorf("script ran %d times; per-boot cache should run it once", n)
	}
}

func TestEnvScriptWidePermissionsWarn(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	path := filepath.Join(home, EnvScriptName)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho v\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	newEnvScriptResolver()
	if !strings.Contains(buf.String(), "0700") {
		t.Errorf("expected a permissions warning, got: %s", buf.String())
	}
}

func TestExpandEnvVarsUsesResolver(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	writeEnvScript(t, home, "#!/bin/sh\necho script-value\n")
	prev := expandEnvScriptResolver
	SetEnvScriptResolver(newEnvScriptResolver())
	t.Cleanup(func() { expandEnvScriptResolver = prev })

	got := expandEnvVars(`{"apiKey": "${MEEPT_TEST_VAR}"}`)
	if want := `{"apiKey": "script-value"}`; got != want {
		t.Errorf("expandEnvVars = %q, want %q", got, want)
	}
}

func TestExpandEnvVarsNilResolverEnvOnly(t *testing.T) {
	isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	prev := expandEnvScriptResolver
	expandEnvScriptResolver = nil
	t.Cleanup(func() { expandEnvScriptResolver = prev })

	t.Setenv("MEEPT_TEST_VAR", "env-value")
	if got, want := expandEnvVars("${MEEPT_TEST_VAR}"), "env-value"; got != want {
		t.Errorf("expandEnvVars = %q, want %q", got, want)
	}
}

func TestEnvScriptResolverConcurrent(t *testing.T) {
	home := isolateResolver(t)
	unsetSentinel(t, "MEEPT_TEST_VAR")

	writeEnvScript(t, home, "#!/bin/sh\necho value\n")
	r := newEnvScriptResolver()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, ok := r.Resolve("MEEPT_TEST_VAR"); !ok || v != "value" {
				t.Errorf("concurrent resolve got %q ok=%v", v, ok)
			}
		}()
	}
	wg.Wait()
}
