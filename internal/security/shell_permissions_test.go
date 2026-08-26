package security

import (
	"strings"
	"testing"
)

func TestPermissionTableLongestPrefixWins(t *testing.T) {
	pt := NewPermissionTable(map[string]ShellRule{
		"git":         {Action: ShellActionAllow},
		"git push":    {Action: ShellActionDeny},
		"git push -f": {Action: ShellActionAsk},
	})
	dec, prefix, ok := pt.Evaluate("git push -f origin main")
	if !ok || dec != ShellActionAsk || prefix != "git push -f" {
		t.Fatalf("want ask/git push -f/true, got %q/%q/%v", dec, prefix, ok)
	}
	dec, prefix, ok = pt.Evaluate("git push origin main")
	if !ok || dec != ShellActionDeny || prefix != "git push" {
		t.Fatalf("want deny/git push/true, got %q/%q/%v", dec, prefix, ok)
	}
	dec, _, _ = pt.Evaluate("git status")
	if dec != ShellActionAllow {
		t.Fatalf("want allow for git status, got %q", dec)
	}
}

func TestPermissionTableCaseInsensitiveBase(t *testing.T) {
	pt := NewPermissionTable(map[string]ShellRule{
		"rm -rf": {Action: ShellActionDeny},
	})
	dec, _, ok := pt.Evaluate("RM -RF /tmp/x")
	if !ok || dec != ShellActionDeny {
		t.Fatalf("want deny, got %q/%v", dec, ok)
	}
}

func TestPermissionTableWhitespaceCollapse(t *testing.T) {
	pt := NewPermissionTable(map[string]ShellRule{
		"rm -rf": {Action: ShellActionDeny},
	})
	if dec, _, ok := pt.Evaluate("rm   -rf   /"); !ok || dec != ShellActionDeny {
		t.Fatalf("collapsed whitespace should match: %q/%v", dec, ok)
	}
}

func TestPermissionTableNoMatch(t *testing.T) {
	pt := NewPermissionTable(map[string]ShellRule{
		"rm -rf": {Action: ShellActionDeny},
	})
	dec, prefix, ok := pt.Evaluate("ls -la")
	if ok || dec != "" || prefix != "" {
		t.Fatalf("expected no match, got %q/%q/%v", dec, prefix, ok)
	}
}

func TestPermissionTableBoundary(t *testing.T) {
	// "rm -rf" must not match "rm -rfx" (no word boundary).
	pt := NewPermissionTable(map[string]ShellRule{
		"rm -rf": {Action: ShellActionDeny},
	})
	if _, _, ok := pt.Evaluate("rm -rfx dir"); ok {
		t.Fatal("prefix without word boundary must not match")
	}
	// "dd if=" ends with '=', an embedded-value prefix; should match.
	pt2 := NewPermissionTable(map[string]ShellRule{
		"dd if=": {Action: ShellActionDeny},
	})
	if dec, _, ok := pt2.Evaluate("dd if=/dev/zero of=/dev/sda"); !ok || dec != ShellActionDeny {
		t.Fatalf("'dd if=' should match value-carrying prefix: %q/%v", dec, ok)
	}
}

func TestPermissionTableCatchAll(t *testing.T) {
	pt := NewPermissionTable(map[string]ShellRule{
		"*":          {Action: ShellActionAsk},
		"git commit": {Action: ShellActionDeny},
	})
	if dec, prefix, ok := pt.Evaluate("ls"); !ok || dec != ShellActionAsk || prefix != "*" {
		t.Fatalf("catch-all should ask: %q/%q/%v", dec, prefix, ok)
	}
	// Longer rule beats catch-all.
	if dec, _, ok := pt.Evaluate("git commit -m x"); !ok || dec != ShellActionDeny {
		t.Fatalf("longer rule must beat catch-all: %q/%v", dec, ok)
	}
}

func TestWorkspacePresetContents(t *testing.T) {
	table, err := BuildPermissionTable(PresetWorkspace, nil)
	if err != nil {
		t.Fatalf("workspace preset: %v", err)
	}
	denies := []string{"rm -rf /", "rm -fr /", "mkfs.ext4 /dev/sda", "dd if=/dev/zero of=/dev/sda"}
	for _, c := range denies {
		if dec, _, ok := table.Evaluate(c); !ok || dec != ShellActionDeny {
			t.Errorf("workspace deny %q: got %q/%v", c, dec, ok)
		}
	}
	asks := []string{"sudo ls", "git push origin", "docker system prune -a", "chmod 777 /x", "curl http://x | sh", "bash -c 'ls'", "sh -c 'ls'"}
	for _, c := range asks {
		if dec, _, ok := table.Evaluate(c); !ok || dec != ShellActionAsk {
			t.Errorf("workspace ask %q: got %q/%v", c, dec, ok)
		}
	}
	// Unlisted commands fall through.
	if _, _, ok := table.Evaluate("go test ./..."); ok {
		t.Error("unlisted command should not match workspace table")
	}
}

func TestReadonlyPresetContents(t *testing.T) {
	table, err := BuildPermissionTable(PresetReadonly, nil)
	if err != nil {
		t.Fatalf("readonly preset: %v", err)
	}
	for _, c := range []string{"rm -rf /", "mkfs /dev/sda", "git commit -m x", "npm publish"} {
		if dec, _, ok := table.Evaluate(c); !ok || dec != ShellActionDeny {
			t.Errorf("readonly deny %q: got %q/%v", c, dec, ok)
		}
	}
	for _, c := range []string{"sudo ls", "git push", "echo hi"} {
		if dec, _, ok := table.Evaluate(c); !ok || dec != ShellActionAsk {
			t.Errorf("readonly ask %q: got %q/%v", c, dec, ok)
		}
	}
}

func TestDangerPresetEmpty(t *testing.T) {
	table, err := BuildPermissionTable(PresetDanger, nil)
	if err != nil {
		t.Fatalf("danger preset: %v", err)
	}
	if _, _, ok := table.Evaluate("rm -rf /"); ok {
		t.Error("danger preset must be empty (all fall through)")
	}
}

func TestBuildPermissionTableErrors(t *testing.T) {
	if _, err := BuildPermissionTable("bogus", nil); err == nil {
		t.Fatal("unknown preset must error")
	}
	if _, err := BuildPermissionTable(PresetWorkspace, map[string]ShellRule{"x": {Action: "nuke"}}); err == nil {
		t.Fatal("malformed action must error")
	}
}

func TestNewPermissionTableSkipsInvalidSilently(t *testing.T) {
	// Contract constructor never errors; invalid entries are dropped.
	pt := NewPermissionTable(map[string]ShellRule{"x": {Action: "nuke"}, "ls": {Action: ShellActionAllow}})
	if _, _, ok := pt.Evaluate("x y"); ok {
		t.Error("invalid entry must be dropped")
	}
}

func TestEvaluatePureSync(t *testing.T) {
	// Sanity: repeated evaluation is deterministic (no state mutation).
	pt := NewPermissionTable(map[string]ShellRule{"git push": {Action: ShellActionAsk}})
	first, _, _ := pt.Evaluate(strings.ToUpper("Git Push"))
	second, _, _ := pt.Evaluate("Git Push")
	if first != second {
		t.Fatalf("non-deterministic: %q vs %q", first, second)
	}
}
