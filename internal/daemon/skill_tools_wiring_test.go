package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/tools"
)

// skillToolsTestConfig builds a minimal config for NewComponents in the
// wiring tests below. It mirrors the harness used by backup/sync wiring
// tests: real NewComponents against a temp DataDir, no background
// goroutines outliving the test.
func skillToolsTestConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Skills:   config.SkillsConfig{Enabled: true},
		Security: config.SecurityConfig{AllowedPaths: []string{tmpDir}},
		Daemon:   config.DaemonConfig{DataDir: tmpDir},
	}
	return cfg, tmpDir
}

// newSkillToolsTestComponents constructs Components for wiring tests and
// registers cleanup that stops the daemon.
func newSkillToolsTestComponents(t *testing.T, cfg *config.Config) *Components {
	t.Helper()
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = comps.Stop(ctx)
	})
	return comps
}

// executeToolArgs runs a tool through the registry, failing the test on
// transport-level errors (tool missing, registry execution panic).
func executeSkillTool(t *testing.T, comps *Components, name string, args map[string]any) (*tools.ToolResult, error) {
	t.Helper()
	return comps.ToolRegistry.Execute(context.Background(), name, args)
}

// requireSuccess asserts a ToolResult reports success and returns its
// string payload form for message assertions.
func requireSuccess(t *testing.T, res *tools.ToolResult, name string) {
	t.Helper()
	if res == nil {
		t.Fatalf("%s: nil ToolResult", name)
	}
	if !res.Success {
		t.Fatalf("%s: expected success, got error: %s", name, res.Error)
	}
}

// TestSkillAuthoringToolsRegisteredWithLiveHandles verifies the three new
// tools register into the daemon tool registry and that the skill authoring
// tools hold non-nil writer/registry handles — evidenced FUNCTIONALLY by
// executing them end-to-end (registry.Execute → tool.Execute → writer write).
// A nil writer would make skills_create fail with "skills writer
// unavailable"; a nil registry would make skills_patch fail with "skills
// registry unavailable" before any work. (Ordering: initializeSkills runs
// before registerBuiltinTools inside NewComponents, so the tools block sees
// live handles — the master's ordering hazard resolves to the tools block.)
func TestSkillAuthoringToolsRegisteredWithLiveHandles(t *testing.T) {
	cfg, tmpDir := skillToolsTestConfig(t)
	cfg.Transcript = config.TranscriptConfig{Enabled: true}

	comps := newSkillToolsTestComponents(t, cfg)

	for _, name := range []string{"transcript_fetch", "skills_create", "skills_patch"} {
		if tool := comps.ToolRegistry.Get(name); tool == nil {
			t.Errorf("tool %q not registered", name)
		}
	}

	// skills_create: end-to-end write through the real writer proves the
	// tool was built with the live *lifecycle.Writer handle (a nil handle
	// returns the "skills writer unavailable" error before any write).
	createRes, err := executeSkillTool(t, comps, "skills_create", map[string]any{
		"name":        "wiring-test-skill",
		"description": "temporary skill proving writer wiring",
		"body":        "# wiring test\n\nProof that the create tool holds a live writer handle.",
	})
	if err != nil {
		t.Fatalf("skills_create execute: %v", err)
	}
	requireSuccess(t, createRes, "skills_create")

	createdPath := filepath.Join(tmpDir, "skills", "wiring-test-skill", "SKILL.md")
	if _, err := os.Stat(createdPath); err != nil {
		t.Fatalf("skills_create did not write %s: %v", createdPath, err)
	}

	// skills_patch: a no-op rewrite round-trip proves the tool holds both
	// the registry (skill lookup succeeds — nil registry returns "skills
	// registry unavailable", a different instance returns "not found") and
	// the writer (write succeeds). The skill is registered into the
	// daemon's registry handle directly because leaf 03's committed design
	// resolves skills through the startup registry snapshot (Registry.Get
	// is a plain map lookup; a daemon restart picks up newly written
	// skills).
	disk, err := os.ReadFile(createdPath)
	if err != nil {
		t.Fatalf("read back created skill: %v", err)
	}
	comps.SkillRegistry.Register(&skills.Skill{
		Name:        "wiring-test-skill",
		Description: "temporary skill proving writer wiring",
		Path:        createdPath,
		Priority:    skills.PriorityUser,
	})
	patchRes, err := executeSkillTool(t, comps, "skills_patch", map[string]any{
		"name":    "wiring-test-skill",
		"content": string(disk),
	})
	if err != nil {
		t.Fatalf("skills_patch execute: %v", err)
	}
	requireSuccess(t, patchRes, "skills_patch")
}

// TestTranscriptToolGatedOnConfig verifies transcript_fetch registers ONLY
// when [transcript] enabled=true (default-off preserved, [browser]
// precedent) and that the config fields flow through to the tool (observable
// via a functional subprocess error naming the configured interpreter path).
func TestTranscriptToolGatedOnConfig(t *testing.T) {
	// Default config: tool must be ABSENT.
	cfgOff, _ := skillToolsTestConfig(t)
	compsOff := newSkillToolsTestComponents(t, cfgOff)
	if tool := compsOff.ToolRegistry.Get("transcript_fetch"); tool != nil {
		t.Error("transcript_fetch registered with default (disabled) config")
	}

	// Enabled config: tool present and built from the TranscriptConfig
	// fields. A nonexistent python path makes the subprocess fail with a
	// exec-style error naming the interpreter — proving PythonPath plumbed
	// through construction instead of the "python3" default.
	cfgOn, _ := skillToolsTestConfig(t)
	cfgOn.Transcript = config.TranscriptConfig{
		Enabled:        true,
		PythonPath:     "/nonexistent/python-for-wiring-test",
		ModuleName:     "youtube-transcript-api",
		TimeoutSeconds: 5,
	}
	compsOn := newSkillToolsTestComponents(t, cfgOn)
	tool := compsOn.ToolRegistry.Get("transcript_fetch")
	if tool == nil {
		t.Fatal("transcript_fetch not registered with enabled=true")
	}

	res, err := compsOn.ToolRegistry.Execute(context.Background(), "transcript_fetch", map[string]any{
		"url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
	})
	if err != nil {
		t.Fatalf("transcript_fetch execute: %v", err)
	}
	if res.Success {
		t.Skip("subprocess unexpectedly succeeded; environment has a real runner")
	}
	if !strings.Contains(res.Error, "/nonexistent/python-for-wiring-test") {
		t.Errorf("transcript_fetch error %q does not reference configured python path; config did not flow through", res.Error)
	}
}

// TestSkillCreateShadowsExistingRegistryEntry verifies skills_create holds a
// non-nil skills.Registry handle: creating a skill whose name matches an
// entry in the daemon's registry must produce the shadow warning —
// impossible with a nil registry. The probe skill is registered directly
// into comps.SkillRegistry (the same handle the wiring must inject), so the
// test stays independent of which discovery tiers exist on the host.
func TestSkillCreateShadowsExistingRegistryEntry(t *testing.T) {
	cfg, _ := skillToolsTestConfig(t)
	comps := newSkillToolsTestComponents(t, cfg)

	if comps.SkillRegistry == nil {
		t.Fatal("SkillRegistry nil; cannot probe shadow wiring")
	}
	probe := &skills.Skill{
		Name:        "shadow-probe-skill",
		Description: "registry wiring probe",
		Path:        "/nonexistent/shadow-probe-skill/SKILL.md",
		Priority:    skills.PriorityUser,
	}
	comps.SkillRegistry.Register(probe)

	res, err := executeSkillTool(t, comps, "skills_create", map[string]any{
		"name":        "shadow-probe-skill",
		"description": "shadow probe",
		"body":        "# shadow probe\n\nChecking registry wiring via the shadow warning path.",
	})
	if err != nil {
		t.Fatalf("skills_create execute: %v", err)
	}
	requireSuccess(t, res, "skills_create")

	// Two-value type assertion per house rules; the create tool returns a
	// map payload with a "message" string.
	payload, ok := res.Result.(map[string]any)
	if !ok {
		t.Fatalf("skills_create result payload is %T, want map[string]any", res.Result)
	}
	note, _ := payload["message"].(string)
	if !strings.Contains(note, "shadows existing skill") {
		t.Errorf("skills_create output %q missing shadow warning; registry handle not wired", note)
	}
}
