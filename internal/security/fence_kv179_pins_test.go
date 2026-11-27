package security

// Issue #179 pins — fence extractPathFromToken KEY=VALUE / flag=value gap.
//
// Origin: bughunt 2026-11-24 wave, finding M18 consume-side. The scheduler
// guard (internal/scheduler, shellGuardSourceWithEnv) started including Env
// entries in the guarded source string, but the execution-time fence dropped
// every KEY=VALUE token inside extractPathFromToken (fence.go), so
// BASH_ENV=/outside/fence/evil.sh reached the raw substitution scan yet
// skipped the path allowlist layer entirely. Posture chosen per the issue
// checklist: env-token VALUES containing paths ARE path-validated.
//
// Each pin is non-vacuous: reverting extractPathFromToken to the pre-fix
// version (skip any token containing "=" or starting with "-") makes the
// deny-pins fail.

import (
	"path/filepath"
	"strings"
	"testing"
)

func kv179Fixture(t *testing.T) (fc *FenceChecker, root, outside string) {
	t.Helper()
	root = t.TempDir()
	outside = t.TempDir()
	return NewFenceChecker(FenceConfig{Enabled: true, RootPath: root}, nil), root, outside
}

// Pin (a): an env-assignment token whose value is a path OUTSIDE the fence
// root must be denied by the fence path layer. Pre-fix, the "=" caused the
// token to be skipped, so CheckCommand returned nil.
func TestIssue179_EnvAssignmentPathOutsideRootDenied(t *testing.T) {
	fc, root, outside := kv179Fixture(t)
	evil := filepath.Join(outside, "evil.sh")

	// workDir is INSIDE the root: the only denial vector is the env token.
	err := fc.CheckCommand("BASH_ENV="+evil+" bash -c 'echo hi'", root)
	if err == nil {
		t.Fatalf("CheckCommand(BASH_ENV=%s ...) = nil, want denial (issue #179: env-assignment path outside root must be validated)", evil)
	}
	if !strings.Contains(err.Error(), "outside project root") {
		t.Errorf("CheckCommand error = %v, want the fence path denial", err)
	}

	// Same shape through the other classic shell-hijack env vars.
	for _, key := range []string{"ENV", "PYTHONSTARTUP", "LD_PRELOAD"} {
		cmd := key + "=" + evil
		if err := fc.CheckCommand(cmd, root); err == nil {
			t.Errorf("CheckCommand(%q) = nil, want denial", cmd)
		}
	}
}

// Pin (a2): flag=value shapes (--out=/outside/x) carry paths too and must be
// validated. Pre-fix, the leading "-" caused the token to be skipped.
func TestIssue179_FlagValuePathOutsideRootDenied(t *testing.T) {
	fc, root, outside := kv179Fixture(t)
	outsideFile := filepath.Join(outside, "secret.txt")

	err := fc.CheckCommand("tar cf - --file="+outsideFile, root)
	if err == nil {
		t.Fatalf("CheckCommand(tar --file=%s) = nil, want denial (flag=value path outside root)", outsideFile)
	}

	err = fc.CheckCommand("grep --color=auto pat "+root, root)
	if err != nil {
		t.Errorf("CheckCommand(grep --color=auto ...) = %v, want nil (non-path flag value unaffected)", err)
	}
}

// Pin (b): KEY=VALUE tokens whose path is INSIDE the fence root still pass.
func TestIssue179_EnvAssignmentPathInsideRootAllowed(t *testing.T) {
	fc, root, _ := kv179Fixture(t)
	inside := filepath.Join(root, "build.log")

	if err := fc.CheckCommand("OUT="+inside+" make all", root); err != nil {
		t.Errorf("CheckCommand(OUT=%s make) = %v, want nil (in-root KEY=VALUE path allowed)", inside, err)
	}
	// flag=value inside root.
	if err := fc.CheckCommand("tee --output="+inside, root); err != nil {
		t.Errorf("CheckCommand(tee --output=%s) = %v, want nil", inside, err)
	}
}

// Pin (c): non-path KEY=VALUE tokens are unaffected — no extraction, no
// denial. FOO=bar, N=5, --flag=word, and values that merely contain "=" or
// ".." inside a non-path word must not change CheckCommand outcomes.
func TestIssue179_NonPathAssignmentsUnaffected(t *testing.T) {
	fc, root, _ := kv179Fixture(t)

	for _, cmd := range []string{
		"FOO=bar make test",
		"N=5 python train.py",
		"LC_ALL=C LANG=en_US.UTF-8 sort data.txt",
		"grep --color=auto --max-count=10 pattern file.txt",
		"make -j8 CFLAGS=-O2",
		"echo a=b=c",
		"VERSION=1.2.3 curl -sSf https://example.com/install",
	} {
		if err := fc.CheckCommand(cmd, root); err != nil {
			t.Errorf("CheckCommand(%q) = %v, want nil (non-path KEY=VALUE token unaffected)", cmd, err)
		}
	}
}

// Direct extractor-level pins: the three dispatch shapes at the unit
// boundary, so a future refactor of CheckCommand keeps the semantics even
// if it stops calling the extractor per token.
func TestIssue179_ExtractPathFromTokenShapes(t *testing.T) {
	cases := []struct {
		tok  string
		want string
	}{
		{"BASH_ENV=/tmp/evil.sh", "/tmp/evil.sh"},
		{"OUT=/tmp/dir/out.bin", "/tmp/dir/out.bin"},
		{"--file=/etc/shadow", "/etc/shadow"},
		{"-o=~/x", "~/x"},
		{"KEY=../escape/f", "../escape/f"},
		{"KEY=./rel/f", "./rel/f"},
		{"FOO=bar", ""},
		{"N=5", ""},
		{"--color=auto", ""},
		{"--verbose", ""},
		{"-j8", ""},
		{"make", ""},
		{"cat", ""},
		{"/abs/path", "/abs/path"},
		{"./rel", "./rel"},
		{"~/home", "~/home"},
		{"a..b", ""}, // M3: ".." not in path position — a bare word, not a path (was "a..b" under the old Contains rule)
		{"KEY=", ""},
		{"=", ""},        // legacy byte-identical: "=" is not path-like
		{"/k=v", "/k=v"}, // legacy byte-identical: leading "/" wins, no split
		{"~/k=v", "~/k=v"},
		// New #179 posture: multi-path env values are validated too; the
		// whole colon-list is checked as one path and refused when it is
		// outside the root (documented, deliberate — env PATH overrides in
		// guarded commands are exactly what the fence should police).
		{"PATH=/usr/bin:/bin", "/usr/bin:/bin"},
	}
	for _, tc := range cases {
		if got := extractPathFromToken(tc.tok); got != tc.want {
			t.Errorf("extractPathFromToken(%q) = %q, want %q", tc.tok, got, tc.want)
		}
	}
}

// Redirection handling regression: the ">" arm also feeds tokens through the
// extractor; assignment-shaped redirect targets keep working.
func TestIssue179_RedirectUnaffectedByKvChange(t *testing.T) {
	fc, root, outside := kv179Fixture(t)

	if err := fc.CheckCommand("echo hi > "+filepath.Join(root, "ok.txt"), root); err != nil {
		t.Errorf("in-root redirect = %v, want nil", err)
	}
	if err := fc.CheckCommand("echo hi > "+filepath.Join(outside, "x.txt"), root); err == nil {
		t.Error("outside redirect = nil, want denial")
	}
}

// Bughunt 2026-11-27 M3 pins (audit S-5): ".." is judged in PATH POSITION.
// The old `strings.Contains(value, "..")` rule classified format strings,
// env values and bare words as paths, and since relative forms Abs against
// the daemon cwd they were denied on every jail. The three audit shapes
// must pass; genuine traversal must still be denied.
func TestM3_NonPathDotDotTokensPass(t *testing.T) {
	fc, root, _ := kv179Fixture(t)

	for _, cmd := range []string{
		"git log --format=%h..%s -5",
		"echo a..b",
		"env X=Y..Z printenv X",
		"curl -H 'X=Y..Z' https://example.com",
		"make CFLAGS=-std=c11..c17 target", // flag value containing ".."
	} {
		if err := fc.CheckCommand(cmd, root); err != nil {
			t.Errorf("CheckCommand(%q) = %v, want nil (M3: \"..\" not in path position)", cmd, err)
		}
	}
}

func TestM3_GenuineTraversalStillDenied(t *testing.T) {
	fc, root, _ := kv179Fixture(t)

	for _, cmd := range []string{
		"cat ../../../../etc/passwd",
		"cat a/../b",
		"cat ../sibling/secret",
		"tar --file=../escape.tar -cf x",
	} {
		if err := fc.CheckCommand(cmd, root); err == nil {
			t.Errorf("CheckCommand(%q) = nil, want denial (traversal must stay blocked)", cmd)
		}
	}
}

func TestM3_DevNullRedirectsPass(t *testing.T) {
	fc, root, outside := kv179Fixture(t)

	for _, cmd := range []string{
		"make build 2>/dev/null",
		"make build 2> /dev/null",
		"make build 2>/dev/null 1>/dev/null",
		"cmd 2>/dev/stderr",
		"cmd >/dev/tty",
	} {
		if err := fc.CheckCommand(cmd, root); err != nil {
			t.Errorf("CheckCommand(%q) = %v, want nil (M3: device redirect targets are not filesystem writes)", cmd, err)
		}
	}
	// A REAL outside file redirect stays denied — the exemption is the
	// device list only.
	if err := fc.CheckCommand("make build 2>"+filepath.Join(outside, "log.txt"), root); err == nil {
		t.Error("outside-file redirect = nil, want denial (device exemption must not widen)")
	}
}

// Non-vacuity: reverting extractPathFromToken to Contains(value, "..")
// turns TestM3_NonPathDotDotTokensPass red (all five shapes extract as
// paths); deleting the devNullTargets exemption turns
// TestM3_DevNullRedirectsPass red on the first shape.
