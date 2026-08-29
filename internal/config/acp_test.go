package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoadACPAgents_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acp_agents.json5")

	want := &ACPAgentsConfig{
		Agents: []ACPAgentEntry{
			{
				ID:          "codex",
				Description: "OpenAI Codex via codex-acp adapter",
				Command:     []string{"npx", "-y", "@agentclientprotocol/codex-acp"},
				Env:         map[string]string{"FOO": "bar"},
				Cwd:         "/tmp/work",
				DefaultMode: "read-only",
				Enabled:     false,
			},
		},
	}
	if err := SaveACPAgents(path, want); err != nil {
		t.Fatalf("SaveACPAgents: %v", err)
	}
	got, err := LoadACPAgents(path)
	if err != nil {
		t.Fatalf("LoadACPAgents: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch\ngot  %+v\nwant %+v", got, want)
	}
}

func TestLoadACPAgents_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json5")
	got, err := LoadACPAgents(path)
	if err != nil {
		t.Fatalf("missing file should return empty config, nil error; got err %v", err)
	}
	if got == nil {
		t.Fatal("missing file returned nil config")
	}
	if len(got.Agents) != 0 {
		t.Errorf("missing file agents = %#v, want empty", got.Agents)
	}
}

func TestLoadACPAgents_MalformedJSON5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken-acp-agents.json5")
	if err := os.WriteFile(path, []byte("{agents: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadACPAgents(path)
	if err == nil {
		t.Fatal("malformed JSON5 should return an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention path %q", err, path)
	}
}

func TestLoadACPAgents_ShippedTemplate(t *testing.T) {
	cfg, err := LoadACPAgents("../../config/acp_agents.json5")
	if err != nil {
		t.Fatalf("LoadACPAgents(shipped template): %v", err)
	}
	if len(cfg.Agents) == 0 {
		t.Fatal("shipped catalog has no agents")
	}
	var found *ACPAgentEntry
	for i := range cfg.Agents {
		if cfg.Agents[i].ID == "codex" {
			found = &cfg.Agents[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("codex entry missing from catalog (%d agents)", len(cfg.Agents))
	}
	if found.Enabled {
		t.Error("codex must ship enabled: false")
	}
	if len(found.Command) == 0 {
		t.Error("codex command should be non-empty")
	}
}
