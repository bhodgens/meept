package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/pkg/models"
)

// newRuntimeTestProjectManager builds a real ProjectManager backed by a
// temp-dir sqlite store. No git work happens (RegisterLocal only).
func newRuntimeTestProjectManager(t *testing.T) *project.ProjectManager {
	t.Helper()
	dir := t.TempDir()
	store, err := project.NewStore(filepath.Join(dir, "projects.db"), discardLogger())
	if err != nil {
		t.Fatalf("project.NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := config.ProjectsConfig{
		BaseDir:       filepath.Join(dir, "projects"),
		DefaultBranch: "main",
	}
	if err := os.MkdirAll(cfg.BaseDir, 0o755); err != nil {
		t.Fatalf("mkdir base dir: %v", err)
	}
	return project.NewProjectManager(store, nil, cfg, discardLogger())
}

func writePromptTemplate(t *testing.T, projectDir, body string) {
	t.Helper()
	path := filepath.Join(projectDir, ".meept", "prompts", "planner", "interview.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
}

// TestResolveProjectPromptDir covers the nil-guard path and the no-active-
// project path: the resolver must never fall back to the process CWD.
func TestResolveProjectPromptDir(t *testing.T) {
	if got := resolveProjectPromptDir(nil); got != "" {
		t.Errorf("resolveProjectPromptDir(nil) = %q, want empty", got)
	}
	pm := newRuntimeTestProjectManager(t)
	if got := resolveProjectPromptDir(&Components{ProjectManager: pm}); got != "" {
		t.Errorf("resolveProjectPromptDir with no active project = %q, want empty", got)
	}
}

// TestWireProjectPromptTier_NoOpWithoutInputs proves the nil guards: a missing
// bus, service, or resolver wires nothing instead of panicking.
func TestWireProjectPromptTier_NoOpWithoutInputs(t *testing.T) {
	b := bus.New(nil, discardLogger())
	t.Cleanup(b.Close)
	svc := services.NewDefaultPromptService()
	noop := func() string { return "" }

	if got := wireProjectPromptTier(nil, svc, noop, nil); got != nil {
		t.Error("nil bus should return nil subscriber")
	}
	if got := wireProjectPromptTier(b, nil, noop, nil); got != nil {
		t.Error("nil prompt service should return nil subscriber")
	}
	if got := wireProjectPromptTier(b, svc, nil, nil); got != nil {
		t.Error("nil resolver should return nil subscriber")
	}
}

// TestWireProjectPromptTier_UpdatesOnProjectChange drives a real project
// switch through the bus event the project.set RPC handler publishes and
// asserts the prompt service's project tier follows it — the runtime
// (no-restart) contract. The switch mechanism is the same one handleSet uses:
// mark the new project active, then publish project.set.
func TestWireProjectPromptTier_UpdatesOnProjectChange(t *testing.T) {
	ctx := context.Background()
	pm := newRuntimeTestProjectManager(t)
	components := &Components{ProjectManager: pm}

	// Project A is the initial active project.
	projADir := t.TempDir()
	writePromptTemplate(t, projADir, "A")
	projA, err := pm.RegisterLocal(ctx, "", "proj-a", projADir)
	if err != nil {
		t.Fatalf("RegisterLocal proj-a: %v", err)
	}

	projAPrompts := filepath.Join(projADir, ".meept", "prompts")
	svc := services.NewPromptService(projAPrompts, "", "", "")
	if got, err := svc.Get("interview"); err != nil || got.Content != "A" {
		t.Fatalf("initial Get = (%+v, %v), want content A", got.PromptEntry, err)
	}

	msgBus := bus.New(nil, discardLogger())
	t.Cleanup(msgBus.Close)
	if sub := wireProjectPromptTier(msgBus, svc,
		func() string { return resolveProjectPromptDir(components) },
		discardLogger()); sub == nil {
		t.Fatal("wireProjectPromptTier returned nil subscriber")
	}

	// Project B becomes active; enumerate the switch exactly like handleSet.
	projBDir := t.TempDir()
	writePromptTemplate(t, projBDir, "B")
	if err := pm.SetStatus(ctx, projA.ID, "inactive"); err != nil {
		t.Fatalf("SetStatus proj-a inactive: %v", err)
	}
	projB, err := pm.RegisterLocal(ctx, "", "proj-b", projBDir)
	if err != nil {
		t.Fatalf("RegisterLocal proj-b: %v", err)
	}
	if err := pm.SetStatus(ctx, projB.ID, "active"); err != nil {
		t.Fatalf("SetStatus proj-b active: %v", err)
	}

	// Publish the project-change event (payload mirrors handleSet).
	msg, err := models.NewBusMessage("project.set", "test", map[string]string{
		"session_id": "sess-test",
		"path":       projB.LocalPath,
	})
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	msgBus.Publish(projectPromptTierTopic, msg)

	wantPrompts := filepath.Join(projBDir, ".meept", "prompts")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if svc.ProjectDir() == wantPrompts {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ProjectDir = %q, want %q after project.set", svc.ProjectDir(), wantPrompts)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The switched project tier must win in lookup resolution.
	got, err := svc.Get("interview")
	if err != nil {
		t.Fatalf("Get after switch: %v", err)
	}
	if got.Content != "B" {
		t.Errorf("Get after switch content = %q, want B", got.Content)
	}
	if got.Tier != services.TierProject {
		t.Errorf("Get after switch tier = %s, want project", got.Tier)
	}
}
