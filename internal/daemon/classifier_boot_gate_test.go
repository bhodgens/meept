package daemon

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"runtime"
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

// llamaCtxProviders is a local llama-server whose --ctx-size can be set
// independently of the model's context_limit.
func llamaCtxProviders(ctxSize string, limit int) *llm.ProvidersConfig {
	cmd := []string{"llama-server", "--model", "/models/lfm.gguf", "--port", "8080", "--ctx-size", ctxSize}
	return &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"local-gguf": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1"},
				Lifecycle: &llm.RuntimeLifecycleConfig{
					Runtime:      "llama-cpp",
					ModelPath:    "/models/lfm.gguf",
					SpawnCommand: cmd,
				},
				Models: map[string]llm.ModelDef{
					"lfm-8b-gguf": {ContextLimit: limit},
				},
			},
		},
	}
}

// TestCheckClassifierRuntimeHealth_CtxSizeExceedsLimitFails pins issue 51:
// boot fails immediately when --ctx-size exceeds the model's context_limit.
// The value is not clamped, and the health prober is not consulted.
func TestCheckClassifierRuntimeHealth_CtxSizeExceedsLimitFails(t *testing.T) {
	providers := llamaCtxProviders("65536", 32768)
	calls := 0
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		calls++
		return &fakeProber{healthy: true}, true
	}
	modelsCfg := &config.ModelsConfig{ClassifierModel: "local-gguf/lfm-8b-gguf"}

	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, true,
		proberFor, metaForAdapter(providers),
	)
	if err == nil {
		t.Fatal("ctx-size above context_limit must fail the boot gate")
	}
	if !errors.Is(err, errLlamaCtxSizeExceedsLimit) {
		t.Fatalf("error = %v, want errLlamaCtxSizeExceedsLimit", err)
	}
	for _, want := range []string{"65536", "32768", "local-gguf", "lfm-8b-gguf"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	if calls != 0 {
		t.Fatalf("ctx-size gate must fail before the health prober, got %d calls", calls)
	}

	// Equal values are not a failure. A smaller loaded window is not this
	// gate (the task is exceeds, not a silent clamp and not !=).
	equal := llamaCtxProviders("32768", 32768)
	err = checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, equal, true,
		proberFor, metaForAdapter(equal),
	)
	if err != nil {
		t.Fatalf("ctx-size equal to context_limit must not fail the gate: %v", err)
	}
	if calls == 0 {
		t.Fatal("equal ctx-size must fall through to the health prober")
	}

	smaller := llamaCtxProviders("16384", 32768)
	err = checkLlamaCtxSizeLimit(smaller)
	if err != nil {
		t.Fatalf("ctx-size below context_limit must not fail: %v", err)
	}

	// The health-gate escape hatch does not skip a ctx-size violation.
	err = checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, false,
		proberFor, metaForAdapter(providers),
	)
	if !errors.Is(err, errLlamaCtxSizeExceedsLimit) {
		t.Fatalf("failFast=false must still fail a ctx-size violation: %v", err)
	}
}

// TestCheckClassifierRuntimeHealth_UnhealthyPrintsCtxSize pins issue 51 item 3:
// the health-gate error names the configured ctx-size, not only the argv.
func TestCheckClassifierRuntimeHealth_UnhealthyPrintsCtxSize(t *testing.T) {
	providers := llamaCtxProviders("32768", 32768)
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		return &fakeProber{healthy: false}, true
	}
	modelsCfg := &config.ModelsConfig{ClassifierModel: "local-gguf/lfm-8b-gguf"}

	err := checkClassifierRuntimeHealth(
		context.Background(), slog.Default(), modelsCfg, providers, true,
		proberFor, metaForAdapter(providers),
	)
	if err == nil {
		t.Fatal("unhealthy runtime must fail the health gate")
	}
	if !strings.Contains(err.Error(), "ctx_size 32768") {
		t.Errorf("health-gate error missing configured ctx-size: %v", err)
	}
}

// loadBundledModelsConfig loads the shipped template exactly as
// LoadProvidersConfigDefault loads it as the merge base (repo-root daemon
// boot: config/models.json5 resolves via bundledModelsPaths). The path is
// resolved from this test file's location, not the process CWD — other
// tests in this package chdir.
func loadBundledModelsConfig(t *testing.T) *llm.ProvidersConfig {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller: cannot locate the test file")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "config", "models.json5")
	cfg, err := llm.LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("load bundled config/models.json5: %v", err)
	}
	return cfg
}

// TestBundledTemplate_NoLocalProviderCtxSizeCollision is the F-D8 boot-gate
// regression for the MLX-era bundled `local` provider (deleted 2026-11-09).
//
// LoadProvidersConfigDefault merges the bundled config/models.json5 as the
// base with the user's ~/.meept/models.json5 overlaying it PER MODEL KEY
// under a provider id. The old bundled `local` provider declared
// lfm-8b-mlx-4bit (context_limit 16384) and lfm-1.2b-q8 (8192) under an mlx
// lifecycle; a user overlay that REPLACED that provider's lifecycle with
// llama-server --ctx-size 32768 (the modern local-gguf layout most installs
// converge on — the operator's real file does exactly this) kept the stale
// bundled context_limits, and checkLlamaCtxSizeLimit then refused boot with
// "llama.cpp ctx-size exceeds context_limit" on models the user never
// declared. The collision only fires when the daemon runs from the repo
// root, where config/models.json5 resolves as the bundled base.
//
// Positive: merge the shipped template with exactly that overlay shape and
// the ctx-size gate must pass. Negative: replay the same overlay against a
// synthetic base carrying the deleted provider (the pre-fix template) and
// the gate must fail naming both stale models — pinning that the gate
// mechanics are what the template deletion removes, and that a reintroduced
// bundled `local` MLX-era provider re-arms the boot failure.
func TestBundledTemplate_NoLocalProviderCtxSizeCollision(t *testing.T) {
	overlay := &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1", NoAuth: true},
				Lifecycle: &llm.RuntimeLifecycleConfig{
					Runtime: "llama-cpp",
					SpawnCommand: []string{
						"llama-server", "--model", "${MODEL_PATH}", "--port", "8080",
						"--host", "127.0.0.1", "--jinja", "--ctx-size", "32768",
					},
				},
				Models: map[string]llm.ModelDef{
					"lfm-8b-q4": {ContextLimit: 32768},
				},
			},
		},
	}

	// Positive: the shipped template must survive the modern overlay.
	merged := llm.MergeProvidersConfig(loadBundledModelsConfig(t), overlay)
	if _, ok := merged.Providers["local"]; !ok {
		t.Fatal("the user's own `local` provider must survive the merge")
	}
	if err := checkLlamaCtxSizeLimit(merged); err != nil {
		t.Fatalf("bundled base + modern user `local` overlay must not trip the ctx-size gate: %v", err)
	}

	// Negative: the retired pre-fix base (the deleted bundled provider,
	// reproduced verbatim: mlx lifecycle + the two stale MLX-era limits).
	staleBase := &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8083/v1"},
				Lifecycle: &llm.RuntimeLifecycleConfig{
					Runtime:      "mlx",
					SpawnCommand: []string{"mlx_lm", "server", "--model", "${MODEL_PATH}", "--port", "8083"},
				},
				Models: map[string]llm.ModelDef{
					"lfm-8b-mlx-4bit": {ContextLimit: 16384},
					"lfm-1.2b-q8":     {ContextLimit: 8192},
				},
			},
		},
	}
	mergedStale := llm.MergeProvidersConfig(staleBase, overlay)
	err := checkLlamaCtxSizeLimit(mergedStale)
	if err == nil {
		t.Fatal("reintroducing the bundled MLX-era `local` provider must re-arm the ctx-size boot failure")
	}
	if !errors.Is(err, errLlamaCtxSizeExceedsLimit) {
		t.Fatalf("error = %v, want errLlamaCtxSizeExceedsLimit", err)
	}
	for _, want := range []string{"lfm-8b-mlx-4bit", "lfm-1.2b-q8", "32768"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("collision error missing %q: %v", want, err)
		}
	}
}
