package daemon

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// fakeProber is a seam-driven HealthChecker stand-in: no real runtime is
// ever spawned by these tests.
type fakeProber struct {
	healthy bool
	waitErr error
	waits   int
}

func (f *fakeProber) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
	f.waits++
	if f.waitErr != nil {
		return f.waitErr
	}
	if f.healthy {
		return nil
	}
	return errors.New("timeout waiting for runtime to become healthy")
}

// gateProviders builds a providers config with one local spawned provider
// (lifecycle block, loopback baseURL) and one cloud provider.
func gateProviders() *llm.ProvidersConfig {
	return &llm.ProvidersConfig{
		ClassifierModel: "mlx/classifier",
		ModelAliases: map[string]llm.ModelAliasEntry{
			"classifier": {Models: []string{"mlx/classifier", "llamacpp/failover"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"mlx": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:57795/v1"},
				Lifecycle: &llm.RuntimeLifecycleConfig{
					AutoStart:    true,
					ModelPath:    "/models/lfm2.5-1.2b",
					SpawnCommand: []string{"mlx_lm.server", "--model", "/models/lfm2.5-1.2b"},
					SpawnTimeout: 1,
					HealthCheck:  llm.HealthCheckConfig{IntervalSeconds: 1, UnhealthyThreshold: 2},
				},
			},
			"llamacpp": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1"},
			},
		},
	}
}

// metaForAdapter binds the real classifierRuntimeMetaFor to the metaFor seam.
func metaForAdapter(providers *llm.ProvidersConfig) classifierRuntimeMetaFunc {
	return func(ref string) (classifierRuntimeMeta, bool) {
		providerID, modelKey, hasSlash := strings.Cut(ref, "/")
		if !hasSlash {
			return classifierRuntimeMeta{}, false
		}
		return classifierRuntimeMetaFor(providers, providerID, modelKey)
	}
}

// TestClassifierChainRefs_SlotAndAliasExpansion pins the chain resolution:
// a bare alias slot ref expands to the alias's full member list (in priority
// order, deduped); an explicit provider/model slot ref stays as-is.
func TestClassifierChainRefs_SlotAndAliasExpansion(t *testing.T) {
	modelsCfg := &config.ModelsConfig{ClassifierModel: "classifier"}
	got := classifierChainRefs(modelsCfg, gateProviders())
	want := []string{"classifier", "mlx/classifier", "llamacpp/failover"}
	if len(got) != len(want) {
		t.Fatalf("classifierChainRefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("classifierChainRefs[%d] = %q, want %q (full: %v)", i, got[i], want[i], want)
		}
	}

	// Explicit provider/model ref: no alias expansion.
	direct := &config.ModelsConfig{ClassifierModel: "mlx/classifier"}
	got = classifierChainRefs(direct, gateProviders())
	if len(got) != 1 || got[0] != "mlx/classifier" {
		t.Fatalf("direct ref chain = %v, want [mlx/classifier]", got)
	}

	// Empty classifier_model falls back to small_model.
	fallback := &config.ModelsConfig{SmallModel: "mlx/small"}
	got = classifierChainRefs(fallback, gateProviders())
	if len(got) != 1 || got[0] != "mlx/small" {
		t.Fatalf("small_model fallback chain = %v, want [mlx/small]", got)
	}
}

// TestClassifierRuntimeMetaFor_LocalVsCloud pins the local-spawned
// classification: lifecycle + loopback = local runtime; no lifecycle or
// non-loopback = cloud-only (no gate).
func TestClassifierRuntimeMetaFor_LocalVsCloud(t *testing.T) {
	providers := gateProviders()

	meta, ok := classifierRuntimeMetaFor(providers, "mlx", "classifier")
	if !ok {
		t.Fatal("mlx provider must classify as a local spawned runtime")
	}
	if meta.Endpoint != "http://127.0.0.1:57795/v1" {
		t.Errorf("Endpoint = %q", meta.Endpoint)
	}
	if meta.ModelPath != "/models/lfm2.5-1.2b" {
		t.Errorf("ModelPath = %q", meta.ModelPath)
	}
	// Health window: spawn_timeout 1s + unhealthy_threshold 2 x interval 1s = 3s.
	if meta.HealthWindow != 3*time.Second {
		t.Errorf("HealthWindow = %v, want 3s (spawn_timeout + threshold x interval)", meta.HealthWindow)
	}
	if len(meta.SpawnCommand) == 0 || meta.SpawnCommand[0] != "mlx_lm.server" {
		t.Errorf("SpawnCommand = %v", meta.SpawnCommand)
	}

	// Cloud provider (no lifecycle): skipped.
	if _, ok := classifierRuntimeMetaFor(providers, "llamacpp", "failover"); ok {
		t.Fatal("provider without lifecycle must be cloud-only (no gate)")
	}
	// Unknown provider: skipped.
	if _, ok := classifierRuntimeMetaFor(providers, "ghost", "x"); ok {
		t.Fatal("unknown provider must not gate")
	}
}

// TestCheckClassifierRuntimeHealth_UnhealthyLocalFails pins F-D8: a local
// classifier runtime that stays unhealthy after its health window is a fatal
// error naming the endpoint, model path, and spawn command.
func TestCheckClassifierRuntimeHealth_UnhealthyLocalFails(t *testing.T) {
	probers := map[string]*fakeProber{"mlx": {healthy: false}}
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		p, ok := probers[providerID]
		return p, ok
	}
	modelsCfg := &config.ModelsConfig{ClassifierModel: "classifier"}
	providers := gateProviders()

	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, true,
		proberFor, metaForAdapter(providers),
	)
	if err == nil {
		t.Fatal("unhealthy local classifier runtime must fail the gate")
	}
	for _, want := range []string{"127.0.0.1:57795", "/models/lfm2.5-1.2b", "mlx_lm.server"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing actionable detail %q: %v", want, err)
		}
	}
	if probers["mlx"].waits == 0 {
		t.Fatal("gate never waited on the local classifier runtime's health checker")
	}
	// The cloud alias member must never be waited on.
	if probers["llamacpp"] != nil && probers["llamacpp"].waits != 0 {
		t.Fatal("cloud-only alias member must not be health-gated")
	}
}

// TestCheckClassifierRuntimeHealth_HealthyLocalPasses pins: a healthy local
// classifier runtime lets the boot proceed.
func TestCheckClassifierRuntimeHealth_HealthyLocalPasses(t *testing.T) {
	probers := map[string]*fakeProber{"mlx": {healthy: true}}
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		p, ok := probers[providerID]
		return p, ok
	}
	modelsCfg := &config.ModelsConfig{ClassifierModel: "classifier"}
	providers := gateProviders()

	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, true,
		proberFor, metaForAdapter(providers),
	)
	if err != nil {
		t.Fatalf("healthy local classifier runtime must pass the gate: %v", err)
	}
}

// TestCheckClassifierRuntimeHealth_FailFastFalseSkips pins the escape hatch:
// failFast=false skips the gate without consulting any health checker.
func TestCheckClassifierRuntimeHealth_FailFastFalseSkips(t *testing.T) {
	probers := map[string]*fakeProber{"mlx": {healthy: false}}
	calls := 0
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		calls++
		p, ok := probers[providerID]
		return p, ok
	}
	modelsCfg := &config.ModelsConfig{ClassifierModel: "classifier"}
	providers := gateProviders()

	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, false,
		proberFor, metaForAdapter(providers),
	)
	if err != nil {
		t.Fatalf("fail_fast=false must skip the gate: %v", err)
	}
	if calls != 0 {
		t.Fatalf("fail_fast=false must not consult health checkers, got %d calls", calls)
	}
	if probers["mlx"].waits != 0 {
		t.Fatal("fail_fast=false must not wait on any runtime")
	}
}

// TestCheckClassifierRuntimeHealth_CloudOnlyChainSkips pins: a cloud-only
// classifier chain (no lifecycle members) never trips the gate.
func TestCheckClassifierRuntimeHealth_CloudOnlyChainSkips(t *testing.T) {
	calls := 0
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		calls++
		return nil, false
	}
	modelsCfg := &config.ModelsConfig{ClassifierModel: "llamacpp/failover"}
	providers := gateProviders() // llamacpp has no lifecycle block

	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, true,
		proberFor, metaForAdapter(providers),
	)
	if err != nil {
		t.Fatalf("cloud-only classifier chain must skip the gate: %v", err)
	}
	if calls != 0 {
		t.Fatalf("cloud-only chain must not consult health checkers, got %d calls", calls)
	}
}

// TestCheckClassifierRuntimeHealth_NoSlotSkips pins: no classifier_model and
// no small_model means classification rides the main LLM client — no gate.
func TestCheckClassifierRuntimeHealth_NoSlotSkips(t *testing.T) {
	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), &config.ModelsConfig{}, gateProviders(), true,
		func(string) (classifierRuntimeProber, bool) { return nil, false },
		metaForAdapter(gateProviders()),
	)
	if err != nil {
		t.Fatalf("no classifier slot must skip the gate: %v", err)
	}
}
