package daemon

// F-D8 classifier boot fail-fast.
//
// User decision (2026-09-18 e2e): the classifier runtime (mlx lfm2.5-1.2b on
// :57795) never became healthy after 120s, the daemon booted anyway, and every
// turn paid the degraded-classification cost (heuristic_fallback, compound
// misroute, ambiguity churn). An unhealthy local classifier at boot is a
// platform failure, not a degradation to tolerate: the daemon must LOG and
// EXIT.
//
// This file implements that gate. After ContainerManager.StartAll has spawned
// the local runtimes, the daemon resolves the classifier chain — the
// models.json5 classifier_model slot (falling back to small_model, mirroring
// components.go's client construction) plus every member of the "classifier"
// model alias when the slot names that alias — and, for every member that is
// a LOCAL SPAWNED runtime (provider carries a lifecycle block on a loopback
// baseURL), waits up to the member's configured health window:
//
//	spawn_timeout_seconds + unhealthy_threshold x interval_seconds
//
// (StartAll already waits SpawnTimeout for the first healthy verdict; the
// extra threshold x interval credits the periodic checker the remaining
// confirmations.) If any local classifier runtime is still unhealthy after
// its window, the gate returns a fatal error: Run fails, and cmd/meept-daemon
// exits non-zero. Cloud-only chains (no lifecycle block) never trip the gate.
// orchestrator.classifier_boot_fail_fast = false skips it entirely.
//
// Seams for tests: checkClassifierRuntimeHealth drives everything through the
// classifierRuntimeProber / classifierRuntimeMetaFunc function types — no real
// runtime is ever spawned.

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// The classifier failover alias is "classifier" (the dispatcher hardcodes
// this default when ClassifierAlias is empty). A classifier_model slot
// naming that alias (or any bare alias name) expands the boot gate to the
// alias's full member list: every member is a failover target, so any of
// them serving the classification turn dead is a platform failure.

// classifierRuntimeProber reports whether the local runtime registered for
// providerID is currently healthy. ok is false when no runtime (no endpoint
// with a health checker) is registered for the provider — itself a fatal
// registration gap when the provider carries a lifecycle block.
type classifierRuntimeProber interface {
	WaitForHealthy(ctx context.Context, timeout time.Duration) error
}

// classifierRuntimeProberFunc adapts a function to classifierRuntimeProber.
type classifierRuntimeProberFunc func(ctx context.Context, timeout time.Duration) error

// WaitForHealthy implements classifierRuntimeProber.
func (f classifierRuntimeProberFunc) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
	return f(ctx, timeout)
}

// classifierRuntimeMeta describes one local spawned classifier-chain runtime
// for error reporting: everything an operator needs to fix the boot failure
// without grepping config.
type classifierRuntimeMeta struct {
	ProviderID   string
	Endpoint     string
	ModelPath    string
	SpawnCommand []string
	HealthWindow time.Duration
}

// classifierRuntimeMetaFunc resolves the runtime metadata for providerID.
// ok is false when the provider has no registered runtime.
type classifierRuntimeMetaFunc func(providerID string) (classifierRuntimeMeta, bool)

// classifierChainRefs resolves the classifier slot to the ordered set of
// model refs the classification path can serve a turn from: the slot ref
// itself (defaulting to small_model, mirroring the classifier client
// construction in components.go) plus — when the slot names an alias — every
// member of that alias from the providers config's model_aliases. providers
// may be nil (aliases then contribute nothing).
func classifierChainRefs(modelsCfg *config.ModelsConfig, providers *llm.ProvidersConfig) []string {
	slotRef := ""
	if modelsCfg != nil {
		slotRef = strings.TrimSpace(modelsCfg.ClassifierModel)
		if slotRef == "" {
			slotRef = strings.TrimSpace(modelsCfg.SmallModel)
		}
	}
	if slotRef == "" {
		return nil
	}
	refs := []string{slotRef}
	if !strings.Contains(slotRef, "/") {
		// Bare alias name: expand to the alias's full member list.
		refs = append(refs, aliasMemberRefs(providers, slotRef)...)
	}
	return dedupeRefs(refs)
}

// aliasMemberRefs returns the "provider/model" members of the named alias,
// in priority order. Unknown aliases contribute nothing.
func aliasMemberRefs(providers *llm.ProvidersConfig, aliasName string) []string {
	if providers == nil || len(providers.ModelAliases) == 0 {
		return nil
	}
	entry, ok := providers.ModelAliases[aliasName]
	if !ok {
		return nil
	}
	return entry.Models
}

// dedupeRefs preserves order while dropping duplicate refs.
func dedupeRefs(refs []string) []string {
	seen := make(map[string]struct{}, len(refs))
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// classifierRuntimeMetaFor resolves the local-runtime metadata for a
// "provider/model" ref against the providers config. ok is false for refs
// that are not local spawned runtimes (no lifecycle block, non-loopback
// baseURL = cloud endpoint, or unknown provider).
func classifierRuntimeMetaFor(
	providers *llm.ProvidersConfig,
	providerID, modelKey string,
) (classifierRuntimeMeta, bool) {
	if providers == nil {
		return classifierRuntimeMeta{}, false
	}
	provider, ok := providers.Providers[providerID]
	if !ok || provider.Lifecycle == nil {
		return classifierRuntimeMeta{}, false
	}
	// Non-loopback baseURL = remote endpoint: nothing is spawned for it, so
	// there is no local runtime to gate on. Mirrors the localhost gate in
	// components.go's runtime registration.
	if !llm.IsLoopbackBaseURL(provider.Options.BaseURL) {
		return classifierRuntimeMeta{}, false
	}

	window := provider.DefaultSpawnTimeout() +
		time.Duration(provider.UnhealthyThreshold())*provider.HealthCheckInterval()

	meta := classifierRuntimeMeta{
		ProviderID:   providerID,
		Endpoint:     provider.Options.BaseURL,
		HealthWindow: window,
		SpawnCommand: append([]string(nil), provider.Lifecycle.SpawnCommand...),
	}
	if p := strings.TrimSpace(provider.Lifecycle.ModelPath); p != "" {
		meta.ModelPath = p
	} else if p := strings.TrimSpace(provider.Lifecycle.ModelPaths[modelKey]); p != "" {
		meta.ModelPath = p
	} else {
		// Multi-model lifecycle without a per-model path: name the declared
		// paths deterministically so the log is still actionable.
		keys := make([]string, 0, len(provider.Lifecycle.ModelPaths))
		for k := range provider.Lifecycle.ModelPaths {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		paths := make([]string, 0, len(keys))
		for _, k := range keys {
			paths = append(paths, k+"="+provider.Lifecycle.ModelPaths[k])
		}
		meta.ModelPath = strings.Join(paths, ", ")
	}
	return meta, true
}

// checkClassifierRuntimeHealth is the F-D8 boot gate. It resolves the
// classifier chain from modelsCfg (+ providers for alias expansion), and for
// every member that is a local spawned runtime (per metaFor) requires the
// endpoint healthy within its configured health window (via proberFor).
// Returns an error naming the endpoint, model path and spawn command when any
// local classifier runtime stays unhealthy; nil when every local member is
// healthy or the chain is cloud-only.
//
// failFast=false skips the gate entirely (orchestrator
// classifier_boot_fail_fast escape hatch). Both proberFor and metaFor are
// seams: tests inject fakes and no real runtime is touched.
func checkClassifierRuntimeHealth(
	ctx context.Context,
	logger *slog.Logger,
	modelsCfg *config.ModelsConfig,
	providers *llm.ProvidersConfig,
	failFast bool,
	proberFor func(providerID string) (classifierRuntimeProber, bool),
	metaFor classifierRuntimeMetaFunc,
) error {
	if !failFast {
		logger.Debug("Classifier boot fail-fast disabled; skipping runtime health gate")
		return nil
	}
	refs := classifierChainRefs(modelsCfg, providers)
	if len(refs) == 0 {
		// No classifier slot at all: classification falls back to the main
		// LLM client, which is not a local-classifier degradation.
		logger.Debug("No classifier model configured; skipping classifier runtime health gate")
		return nil
	}

	var localMetas []classifierRuntimeMeta
	for _, ref := range refs {
		meta, ok := metaFor(ref)
		if !ok {
			continue // cloud-only member
		}
		localMetas = append(localMetas, meta)
	}
	if len(localMetas) == 0 {
		logger.Info("Classifier chain is cloud-only; no local runtime health gate applies",
			"chain_refs", refs)
		return nil
	}

	for _, meta := range localMetas {
		prober, ok := proberFor(meta.ProviderID)
		if !ok {
			return fmt.Errorf(
				"classifier runtime for provider %q (endpoint %s) is not registered; "+
					"model_path %q, spawn_command %q — fix the lifecycle block in models.json5 "+
					"or set orchestrator.classifier_boot_fail_fast=false to skip this gate",
				meta.ProviderID, meta.Endpoint, meta.ModelPath, strings.Join(meta.SpawnCommand, " "))
		}
		logger.Info("Waiting for local classifier runtime health",
			"provider", meta.ProviderID,
			"endpoint", meta.Endpoint,
			"health_window", meta.HealthWindow)
		if err := prober.WaitForHealthy(ctx, meta.HealthWindow); err != nil {
			return fmt.Errorf(
				"classifier runtime on %s not healthy after %s (provider %q, model_path %q, "+
					"spawn_command %q): %w — an unhealthy local classifier at boot is a "+
					"platform failure; check the model path and port, then restart, or set "+
					"orchestrator.classifier_boot_fail_fast=false to tolerate degraded classification",
				meta.Endpoint, meta.HealthWindow, meta.ProviderID, meta.ModelPath,
				strings.Join(meta.SpawnCommand, " "), err)
		}
		logger.Info("Local classifier runtime healthy",
			"provider", meta.ProviderID, "endpoint", meta.Endpoint)
	}
	return nil
}

// gateClassifierRuntimeHealth is the production entry point the daemon calls
// after ContainerManager.StartAll. It loads the providers config for alias
// expansion and binds the seams to the live runtime manager:
//
//   - proberFor: the endpoint's HealthChecker, so the wait honours the same
//     verdict (including the process-alive probe) StartAll's own wait uses;
//   - metaFor: the provider's lifecycle block (model path, spawn command,
//     endpoint, health window) for the actionable failure log.
//
// A nil ContainerManager means no local runtimes exist at all: the chain is
// necessarily cloud-only and the gate passes.
func gateClassifierRuntimeHealth(
	ctx context.Context,
	logger *slog.Logger,
	modelsCfg *config.ModelsConfig,
	failFast bool,
	mgr *llm.RuntimeManager,
) error {
	if mgr == nil {
		return nil
	}
	providers, providersErr := llm.LoadProvidersConfigDefault()
	if providersErr != nil {
		// No providers config: no lifecycle blocks exist, so the chain is
		// cloud-only by construction.
		logger.Debug("No providers config; classifier chain is cloud-only", "error", providersErr)
	}
	proberFor := func(providerID string) (classifierRuntimeProber, bool) {
		hc, ok := mgr.GetHealthChecker(providerID)
		if !ok || hc == nil {
			return nil, false
		}
		return classifierRuntimeProberFunc(hc.WaitForHealthy), true
	}
	metaFor := func(ref string) (classifierRuntimeMeta, bool) {
		providerID, modelKey, hasSlash := strings.Cut(ref, "/")
		if !hasSlash {
			return classifierRuntimeMeta{}, false
		}
		return classifierRuntimeMetaFor(providers, providerID, modelKey)
	}
	return checkClassifierRuntimeHealth(ctx, logger, modelsCfg, providers, failFast, proberFor, metaFor)
}
