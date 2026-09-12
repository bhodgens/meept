package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
)

// TestMetricsStoreWrapper_NilStoreIsSafe proves the nil guard: a wrapper with
// no store (metrics disabled) returns empty, non-nil slices rather than
// panicking.
func TestMetricsStoreWrapper_NilStoreIsSafe(t *testing.T) {
	var w *metricsStoreWrapper
	since := time.Now().Add(-time.Hour)

	models, err := w.ModelUsageSince(context.Background(), since)
	if err != nil {
		t.Fatalf("ModelUsageSince: %v", err)
	}
	if models == nil || len(models) != 0 {
		t.Fatalf("models = %v, want empty non-nil slice", models)
	}

	agents, err := w.AgentUsageSince(context.Background(), since)
	if err != nil {
		t.Fatalf("AgentUsageSince: %v", err)
	}
	if agents == nil || len(agents) != 0 {
		t.Fatalf("agents = %v, want empty non-nil slice", agents)
	}
}

// TestMetricsStoreWrapper_DelegatesToStore proves the wrapper reads usage from
// the daemon's actual store, which is what makes a custom --state-dir work
// instead of the read-only <meept home>/metrics.db fallback.
func TestMetricsStoreWrapper_DelegatesToStore(t *testing.T) {
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	store.RecordLLMCall(metrics.LLMCallRecord{
		Timestamp: time.Now(), Provider: "prov-x", ModelID: "m7",
		AgentID: "chat", TokensSent: 40, TokensRecv: 4, LatencyMs: 10,
	})

	w := &metricsStoreWrapper{store: store}
	models, err := w.ModelUsageSince(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ModelUsageSince: %v", err)
	}
	if len(models) != 1 || models[0].ID != "prov-x/m7" {
		t.Fatalf("models = %+v, want one row prov-x/m7", models)
	}
	agents, err := w.AgentUsageSince(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("AgentUsageSince: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "chat" {
		t.Fatalf("agents = %+v, want one row chat", agents)
	}
}

// TestResolvePromptDirs_HomeAndBundled proves the user tier honors $MEEPT_HOME
// and the bundled tier is resolved off the CWD (no nil-pointer with no
// components, no project tier without an active project).
func TestResolvePromptDirs_HomeAndBundled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	dirs := resolvePromptDirs(nil)
	if dirs.User != filepath.Join(home, "prompts") {
		t.Errorf("User = %q, want %q (config.MeeptPath semantics)", dirs.User, filepath.Join(home, "prompts"))
	}
	if dirs.Bundled == "" {
		t.Error("Bundled is empty; the daemon must resolve config/prompts")
	}
	if dirs.Project != "" {
		t.Errorf("Project = %q, want empty with no active project", dirs.Project)
	}
	if want := config.MeeptPath("prompts"); dirs.User != want {
		t.Errorf("User = %q, want config.MeeptPath(\"prompts\") = %q", dirs.User, want)
	}
}
