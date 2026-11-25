package scheduler

// M18 (bughunt 2026-11-24): the execution-time shell guard classified only
// command+args while Execute injected ShellConfig.Env into the child, so a
// job {Command:"bash", Args:["-c","true"], Env:{"BASH_ENV":"/tmp/evil.sh"}}
// classified as "true" and the fence never saw the sourced script. These
// pins require the guard source to carry EVERY env entry as KEY=VALUE so
// the existing policy (risk classifier, permission table, fence) decides on
// the full effective command surface. A job with no Env must keep the exact
// pre-fix derivation (existing guard pins stay byte-identical).

import (
	"strings"
	"testing"
)

// Pin M18-1: the guard source for the BASH_ENV job contains /tmp/evil.sh —
// the fence must SEE the script bash will source.
// Failing revert: dropping the env-append in newShellJob's guard-source
// derivation makes the source plain "bash -c true" again.
func TestShellJobGuardSourceIncludesEnv(t *testing.T) {
	cfg := JobConfig{
		ID: "m18-bash-env", Name: "m18", Type: JobTypeShell, Schedule: "@yearly",
		ShellConfig: &ShellJobConfig{
			Command: "bash",
			Args:    []string{"-c", "true"},
			Env:     map[string]string{"BASH_ENV": "/tmp/evil.sh"},
		},
	}
	g := &recordingGuard{}
	if _, err := newShellJob(cfg, nil, g, nil); err != nil {
		t.Fatalf("newShellJob failed: %v", err)
	}
	// Execute runs the guard phases before the (expected) spawn failure;
	// only the classification sources matter here.
	_ = runGuardPhase1(t, cfg, g)
	sources := g.all()
	if len(sources) == 0 {
		t.Fatal("guard never consulted")
	}
	for i, src := range sources {
		if !strings.Contains(src, "/tmp/evil.sh") {
			t.Errorf("guard source [%d] = %q; want it to contain /tmp/evil.sh (the fence must see the sourced script)", i, src)
		}
		if !strings.Contains(src, "BASH_ENV=/tmp/evil.sh") {
			t.Errorf("guard source [%d] = %q; want BASH_ENV=/tmp/evil.sh rendered as KEY=VALUE", i, src)
		}
	}
}

// Pin M18-2: env presence changes the derived source — a job with
// Env {FOO:bar} must classify differently from the same job with no Env.
// Failing revert: same as M18-1 (env-append dropped); both sources collide.
func TestShellJobGuardSourceEnvChangesSource(t *testing.T) {
	base := ShellJobConfig{Command: "run-this-binary", Args: []string{"--flag"}}
	withEnv := base
	withEnv.Env = map[string]string{"FOO": "bar"}

	noEnvSrc := shellGuardSourceWithEnv(base.Command, base.Args, base.Env)
	envSrc := shellGuardSourceWithEnv(withEnv.Command, withEnv.Args, withEnv.Env)
	if noEnvSrc == envSrc {
		t.Fatalf("env must change the guard source; both derived %q", noEnvSrc)
	}
	if !strings.Contains(envSrc, "FOO=bar") {
		t.Errorf("env source = %q; want it to contain FOO=bar", envSrc)
	}
}

// Pin M18-3: a job with NO Env derives a byte-identical source to the
// pre-fix derivation (plain shellGuardSource over command+args).
// Failing revert: any change that perturbs the no-Env derivation (e.g.
// always appending a trailing separator) breaks this pin.
func TestShellJobGuardSourceNoEnvByteIdentical(t *testing.T) {
	shapes := []ShellJobConfig{
		{Command: "/bin/sh", Args: []string{"-c", "echo hi"}},
		{Command: "curl", Args: []string{"-sS", "https://example.invalid"}},
		{Command: "bare-binary"},
	}
	for _, sc := range shapes {
		want := shellGuardSource(sc.Command, sc.Args)
		got := shellGuardSourceWithEnv(sc.Command, sc.Args, sc.Env)
		if got != want {
			t.Errorf("no-Env source for %q: got %q, want pre-fix derivation %q", sc.Command, got, want)
		}
	}
}
