package skills

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLinkedAssetClassification(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"scripts/x.py", "script"},
		{"scripts/deep/nested.sh", "script"},
		{"references/x.md", "reference"},
		{"references/deep/notes.md", "reference"},
		{"templates/x.tmpl", "template"},
		{"templates/body.md", "template"},
		{"assets/x.png", "asset"},
		{"assets/logo.svg", "asset"},
		{"other/x.txt", ""},
		{"random/file.bin", ""},
		{"SKILL.md", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := classifyAsset(tt.relPath); got != tt.want {
			t.Errorf("classifyAsset(%q) = %q, want %q", tt.relPath, got, tt.want)
		}
	}
}

func TestLinkedAssetJSONTags(t *testing.T) {
	// JSON tags are a contract: skills.list/skills.get RPC serialize Skill
	// directly, so dir/linked_assets/LinkedAsset tags must be snake_case.
	la := LinkedAsset{Name: "x.py", RelPath: "scripts/x.py", Kind: "script"}
	data, err := json.Marshal(la)
	if err != nil {
		t.Fatalf("marshal LinkedAsset failed: %v", err)
	}
	for _, key := range []string{`"name"`, `"rel_path"`, `"kind"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("LinkedAsset JSON missing key %s: %s", key, data)
		}
	}

	skill := &Skill{
		Name:         "linked",
		Dir:          "/tmp/skills/linked",
		LinkedAssets: []LinkedAsset{la},
	}
	data, err = json.Marshal(skill)
	if err != nil {
		t.Fatalf("marshal Skill failed: %v", err)
	}
	for _, key := range []string{`"dir"`, `"linked_assets"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("Skill JSON missing key %s: %s", key, data)
		}
	}

	// Empty Dir/LinkedAssets must be omitted (flat-layout skills).
	flat := &Skill{Name: "flat"}
	data, err = json.Marshal(flat)
	if err != nil {
		t.Fatalf("marshal flat Skill failed: %v", err)
	}
	if strings.Contains(string(data), "dir") || strings.Contains(string(data), "linked_assets") {
		t.Errorf("flat Skill should omit dir/linked_assets: %s", data)
	}
}
