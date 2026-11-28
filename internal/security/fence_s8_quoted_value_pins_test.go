// Bughunt 2026-11-27 fix wave F12 — audit S-8 (LOW).
//
// The audit claimed `KEY="a b"` SPLITS into `KEY=a` + `b` at HEAD. It does
// not: tokenizeCommand has been quote-aware since d0733f3d8 (pre-window),
// keeping quoted spans as one token. What WAS real at HEAD is the other
// half of the finding — extractPathFromToken's quote trim was one-sided
// (strings.TrimLeft), leaving trailing residue: KEY="'/etc/hosts'"
// extracted "/etc/hosts'" (a path that cannot exist), and a fence that
// compares value FORMS judges the wrong string. The fix trims BOTH sides
// of a balanced shell quote; interior/trailing quotes in unquoted values
// are preserved (weird"name'.txt is a legal filename).
//
// Pin direction 1 (the audit's headline claim, kept as a regression trap):
// tokenizeCommand("KEY=\"a b\"") yields exactly ["KEY=a b"] — one token.
// Reverting tokenizeCommand to whitespace-splitting makes the deny pin
// fail (the quoted value splits; the extractor never sees the full value).
//
// Pin direction 2 (the real HEAD defect): extractPathFromToken returns
// the DEQUOTED value for quoted assignment values, and byte-identical
// values for unquoted ones.
package security

import (
	"path/filepath"
	"testing"
)

// Direction 1: quoted assignment values survive tokenization as ONE token.
func TestS8_QuotedValueStaysOneToken(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{`KEY="a b"`, []string{"KEY=a b"}},
		{`FOO='/etc/pass wd'`, []string{"FOO=/etc/pass wd"}},
		{`env KEY="a b" other`, []string{"env", "KEY=a b", "other"}},
		{`--out="/etc/pass wd"`, []string{"--out=/etc/pass wd"}},
	}
	for _, tc := range cases {
		got := tokenizeCommand(tc.cmd)
		if len(got) != len(tc.want) {
			t.Errorf("tokenizeCommand(%q) = %q, want %q (S-8: quoted value split)", tc.cmd, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("tokenizeCommand(%q)[%d] = %q, want %q", tc.cmd, i, got[i], tc.want[i])
			}
		}
	}
}

// Direction 1b (fence-level): the FULL quoted value drives the verdict.
// An outside path in a quoted multi-word value is denied via the exact
// extracted path; an in-root one passes. With a splitting tokenizer the
// deny shape degrades to no denial at all (the residue is not path-like).
func TestS8_QuotedAssignmentPathFenceVerdict(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	fc := NewFenceChecker(FenceConfig{Enabled: true, RootPath: root}, nil)

	quotedOutside := `"` + filepath.Join(outside, "evil file.sh") + `"`
	if err := fc.CheckCommand(`BASH_ENV=`+quotedOutside+` bash -c 'echo hi'`, root); err == nil {
		t.Errorf("CheckCommand(BASH_ENV=%s ...) = nil, want denial (S-8: quoted outside path must be validated)", quotedOutside)
	}

	quotedInside := `"` + filepath.Join(root, "ok file.txt") + `"`
	if err := fc.CheckCommand(`OUT=`+quotedInside+` make build`, root); err != nil {
		t.Errorf("CheckCommand(OUT=%s make) = %v, want nil (in-root quoted value allowed)", quotedInside, err)
	}
}

// Direction 2 (the real HEAD defect): balanced quotes are trimmed from
// BOTH sides; unquoted values with quote characters are byte-identical.
func TestS8_QuoteTrimBothSides(t *testing.T) {
	cases := []struct {
		tok  string
		want string
	}{
		// Balanced shell quotes dequote.
		{`KEY="/etc/hosts"`, `/etc/hosts`},
		{`KEY='/etc/hosts'`, `/etc/hosts`},
		{`FOO="'/etc/hosts'"`, `/etc/hosts`}, // the TrimLeft residue shape: was "/etc/hosts'"
		{`--out='/tmp/x y'`, `/tmp/x y`},     // was "/tmp/x y'"
		{`KEY="/a b/../c"`, `/a b/../c`},     // traversal inside a quoted value still surfaces
		{`KEY="/a 'quoted' b"`, `/a 'quoted' b`},
		// Unquoted values keep every quote character (legal filenames);
		// these are NOT path-like, so the extractor returns "".
		{`FOO=a"b`, ``},
		{`FOO='weird"name'.txt`, ``},
		// Unquoted, non-path values unchanged (existing kv179 pins).
		{`FOO=bar`, ``},
		{`--color=auto`, ``},
		// Legacy byte-identical shapes.
		{`/k=v`, `/k=v`},
		{`~/k=v`, `~/k=v`},
	}
	for _, tc := range cases {
		if got := extractPathFromToken(tc.tok); got != tc.want {
			t.Errorf("extractPathFromToken(%q) = %q, want %q", tc.tok, got, tc.want)
		}
	}
}

// Non-vacuity: reverting extractPathFromToken's value split to the
// one-sided TrimLeft makes the `FOO="'/etc/hosts'"` and `--out='/tmp/x y'`
// assertions fail (trailing quote residue in the result); reverting
// tokenizeCommand to whitespace-splitting makes TestS8_QuotedValueStaysOneToken
// and the fence-level deny pin fail.
