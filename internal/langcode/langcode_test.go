package langcode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	accepted := map[string]string{
		"EN":     "en",
		"en-US":  "en",
		"EN_us":  "en",
		"  de  ": "de",
		"zho":    "zho",
	}
	for raw, want := range accepted {
		got, ok := Normalize(raw)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = (%q, %v), want (%q, true)", raw, got, ok, want)
		}
	}
	rejected := []string{"", "english", "e", "abcd", "en-", "-en", "12"}
	for _, raw := range rejected {
		if got, ok := Normalize(raw); ok {
			t.Errorf("Normalize(%q) = (%q, true), want rejection", raw, got)
		}
	}
}

func TestIsKnown(t *testing.T) {
	for _, code := range Builtin {
		if !IsKnown(code) {
			t.Errorf("IsKnown(%q) = false; every bundled code must be known", code)
		}
	}
	for _, raw := range []string{"fr", "xx", "english", ""} {
		if IsKnown(raw) {
			t.Errorf("IsKnown(%q) = true; only bundled detections are known without user data", raw)
		}
	}
}

func TestHasWordTable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte("le la de"), 0o600); err != nil {
		t.Fatalf("write table: %v", err)
	}
	if !HasWordTable("fr", dir) {
		t.Error(`HasWordTable("fr") = false, want true`)
	}
	if HasWordTable("it", dir) {
		t.Error(`HasWordTable("it") = true, want false`)
	}
	if HasWordTable("english", dir) {
		t.Error("malformed code must be rejected, not stat-matched")
	}
	if HasWordTable("fr", filepath.Join(dir, "missing")) {
		t.Error("absent dir must report no table")
	}
}
