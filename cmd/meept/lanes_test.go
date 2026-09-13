package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

// The artifact is consumed by the prompt-router sidecar through
// ROUTER_LANES_FILE, so its shape is a contract: a lanes array of
// {intent, agent} objects plus a source string.
func TestBuildLanesArtifact_ShapeAndCoverage(t *testing.T) {
	artifact := buildLanesArtifact()
	if len(artifact.Lanes) == 0 {
		t.Fatal("routing table is empty")
	}
	if artifact.Source != "frontmatter" && artifact.Source != "static" {
		t.Errorf("source = %q, want frontmatter or static", artifact.Source)
	}
	byLane := map[string]string{}
	for _, route := range artifact.Lanes {
		if route.Intent == "" || route.Agent == "" {
			t.Errorf("incomplete route: %+v", route)
		}
		if prev, dup := byLane[route.Intent]; dup {
			t.Errorf("lane %q appears twice (%q and %q)", route.Intent, prev, route.Agent)
		}
		byLane[route.Intent] = route.Agent
	}
	// The lanes this campaign made reachable must be present and routed.
	want := map[string]string{
		"quickplan": "orchestrator",
		"research":  "researcher",
		"tooluse":   "coder",
		"chat":      "chat",
	}
	for lane, agent := range want {
		if got, ok := byLane[lane]; !ok {
			t.Errorf("lane %q missing from the routing table", lane)
		} else if got != agent {
			t.Errorf("lane %q routes to %q, want %q", lane, got, agent)
		}
	}
}

func TestBuildLanesArtifact_EveryLaneMatchesResolver(t *testing.T) {
	artifact := buildLanesArtifact()
	for _, route := range artifact.Lanes {
		// The printed agent must be what the classifier resolves now that the
		// table has been installed by buildLanesArtifact.
		if got := agent.LaneAgentFor(route.Intent); got != route.Agent {
			t.Errorf("lane %q: table says %q, resolver says %q", route.Intent, route.Agent, got)
		}
	}
}

func TestLanesCmd_JSONShape(t *testing.T) {
	cmd := newLanesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var decoded struct {
		Lanes []struct {
			Intent string `json:"intent"`
			Agent  string `json:"agent"`
		} `json:"lanes"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(decoded.Lanes) == 0 {
		t.Fatal("no lanes in JSON output")
	}
	if decoded.Source == "" {
		t.Error("source field missing")
	}
}

func TestLanesCmd_TableMentionsLanes(t *testing.T) {
	cmd := newLanesCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "intent") || !strings.Contains(text, "agent") {
		t.Errorf("table header missing:\n%s", text)
	}
	if !strings.Contains(text, "quickplan") || !strings.Contains(text, "orchestrator") {
		t.Errorf("table omits quickplan:\n%s", text)
	}
}

func TestAgentConfigDirs_IncludeShippedDefaults(t *testing.T) {
	dirs := agentConfigDirs()
	if len(dirs) == 0 {
		t.Fatal("no agent directories resolved")
	}
	joined := strings.Join(dirs, ",")
	if !strings.Contains(joined, "config/agents") {
		t.Errorf("shipped agent dir missing from %v", dirs)
	}
}
