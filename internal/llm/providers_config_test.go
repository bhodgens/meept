package llm

import (
	"encoding/json"
	"maps"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestConfigLoads(t *testing.T) {
	cfg, err := LoadProvidersConfig("../../config/models.json5")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify root-level model references (2026-09-12: the general default
	// moved from the mlx_lm endpoint to the same LFM2.5-8B weights on
	// llama.cpp, which is the runtime that extracts tool calls; the MLX
	// entry stays registered as the classifier alternate).
	if cfg.Model != "local-gguf/lfm-8b-gguf" {
		t.Errorf("model = %q, want local-gguf/lfm-8b-gguf", cfg.Model)
	}
	// 2026-11-09: the bundled MLX-era `local` provider was deleted — as a base
	// it collided with the modern user overlay (a `local` provider whose
	// lifecycle is llama-server --ctx-size 32768 keeps the bundled stale
	// MLX-era context_limits through the per-model-key merge and trips the
	// F-D8 ctx-size boot gate on every repo-root daemon boot). Its two model
	// ids retired with it: lfm-8b-mlx-4bit (weights duplicated on
	// local-gguf), lfm-1.2b-q8 (a GGUF path declared on an mlx_lm runtime
	// that never served it). The fast/small slot moved to the 1.2B-class
	// entry that actually resolves: local-mlx/lfm-combined-sft.
	if cfg.SmallModel != "local-mlx/lfm-combined-sft" {
		t.Errorf("small_model = %q, want local-mlx/lfm-combined-sft", cfg.SmallModel)
	}
	if cfg.ClassifierModel != "classifier" {
		t.Errorf("classifier_model = %q, want classifier", cfg.ClassifierModel)
	}
	if cfg.ImageModel != "xai-oauth/grok-imagine-image-2.0" {
		t.Errorf("image_model = %q, want xai-oauth/grok-imagine-image-2.0", cfg.ImageModel)
	}

	// Verify classifier alias primary: the fine-tuned combined-sft on the
	// local-mlx runtime (platform-intrinsic; A/B testing only).
	classifierAlias, ok := cfg.ModelAliases["classifier"]
	if !ok {
		t.Fatal("classifier alias not found")
	}
	// Classifier alias: the llama.cpp LFM2.5-8B primary (it is the general
	// driver too, so the classifier runs on the endpoint that can call
	// tools), then the same weights on mlx_lm — the 2026-09-06 A/B winner
	// (86.8% vs 54.4% corpus accuracy) — then combined-sft, then the fast
	// 1.2b.
	if len(classifierAlias.Models) == 0 || classifierAlias.Models[0] != "local-gguf/lfm-8b-gguf" {
		t.Errorf("classifier alias primary = %q, want local-gguf/lfm-8b-gguf", classifierAlias.Models[0])
	}
	// 2026-11-09: the classifier alternate slot was the deleted bundled `local`
	// MLX provider's lfm-8b-mlx-4bit; the A/B alternate is now the surviving
	// local-mlx SFT entry.
	if len(classifierAlias.Models) < 2 || classifierAlias.Models[1] != "local-mlx/lfm-combined-sft" {
		t.Errorf("classifier alias alternate = %v, want local-mlx/lfm-combined-sft at [1]", classifierAlias.Models)
	}

	// Verify coder/planner/analyst aliases (operator decision, 2026-09-26;
	// model bumped 2026-10-24): agnes/agnes-3.0-flash primary, local-gguf
	// fallback — and NO zai/ollama members (the intent is agnes + local-only
	// fallback, not a remote provider ladder).
	for _, id := range []string{"coder", "planner", "analyst"} {
		alias, ok := cfg.ModelAliases[id]
		if !ok {
			t.Fatalf("%s alias not found", id)
		}
		if len(alias.Models) == 0 || alias.Models[0] != "agnes/agnes-3.0-flash" {
			t.Errorf("%s alias primary = %v, want agnes/agnes-3.0-flash first", id, alias.Models)
		}
		if !slices.Contains(alias.Models, "local-gguf/lfm-8b-gguf") {
			t.Errorf("%s alias = %v, want a local-gguf/lfm-8b-gguf fallback member", id, alias.Models)
		}
		for _, m := range alias.Models {
			p, _, _ := strings.Cut(m, "/")
			if p == "zai" || p == "ollama" {
				t.Errorf("%s alias = %v: member %q violates the agnes+local-only intent", id, alias.Models, m)
			}
		}
	}

	// The bundled MLX-era `local` provider is gone (deletion rationale in the
	// small_model pin above and the tombstone comment in config/models.json5).
	// Pin its ABSENCE — like the zai pin below — so a stale copy cannot sneak
	// back in and re-arm the boot-gate collision for every repo-root daemon
	// boot, and pin that no alias/member still references it.
	if _, ok := cfg.Providers["local"]; ok {
		t.Error("bundled `local` provider present, want deleted (2026-11-09: its stale MLX-era context_limits merge under a modern user llama.cpp lifecycle override and trip the F-D8 ctx-size boot gate)")
	}
	for id, alias := range cfg.ModelAliases {
		for _, m := range alias.Models {
			if p, _, _ := strings.Cut(m, "/"); p == "local" {
				t.Errorf("alias %s still references deleted bundled provider via member %q", id, m)
			}
		}
	}
	for _, slot := range []struct{ name, ref string }{
		{"model", cfg.Model}, {"small_model", cfg.SmallModel},
		{"classifier_model", cfg.ClassifierModel}, {"summarizer_model", cfg.SummarizerModel},
		{"vision_model", cfg.VisionModel}, {"extract_model", cfg.ExtractModel},
	} {
		if p, _, _ := strings.Cut(slot.ref, "/"); p == "local" && slot.ref != "" {
			t.Errorf("%s slot still references deleted bundled provider: %q", slot.name, slot.ref)
		}
	}

	// Verify the general driver is the llama.cpp endpoint, and that it can
	// actually call tools: --jinja is what makes llama-server render the
	// model's chat template (tool list included). Without it the daemon
	// boots a driver that answers in prose and never calls a tool.
	ggufProvider, ok := cfg.Providers["local-gguf"]
	if !ok {
		t.Fatal("local-gguf provider not found")
	}
	if ggufProvider.Options.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Errorf("local-gguf baseURL = %q, want http://127.0.0.1:8080/v1", ggufProvider.Options.BaseURL)
	}
	if ggufProvider.Lifecycle == nil {
		t.Fatal("local-gguf lifecycle not configured")
	}
	if !slices.Contains(ggufProvider.Lifecycle.SpawnCommand, "--jinja") {
		t.Errorf("local-gguf spawn_command = %v, want --jinja (without it llama-server cannot emit tool calls)",
			ggufProvider.Lifecycle.SpawnCommand)
	}
	// Issue 51: 65536 does not load on the validated host class (M2 Ultra
	// 64GB, 3 resident llama-servers). --ctx-size must not exceed
	// context_limit; both stay at the measured 32768 ceiling.
	if i := slices.Index(ggufProvider.Lifecycle.SpawnCommand, "--ctx-size"); i < 0 || i+1 >= len(ggufProvider.Lifecycle.SpawnCommand) || ggufProvider.Lifecycle.SpawnCommand[i+1] != "32768" {
		t.Errorf("local-gguf spawn_command = %v, want --ctx-size 32768", ggufProvider.Lifecycle.SpawnCommand)
	}
	// Contract C7 (llm-serving-tiers leaf 03): BOTH llama-server spawn
	// entries carry explicit chunk-level KV reuse. Without the flag the
	// server falls back to plain prefix-only reuse and silently loses the
	// mid-prompt chunk reuse (256-token minimum chunk, R0 findings Section 2).
	if !slices.Contains(ggufProvider.Lifecycle.SpawnCommand, "--cache-reuse") {
		t.Errorf("local-gguf spawn_command = %v, want --cache-reuse", ggufProvider.Lifecycle.SpawnCommand)
	}
	// Port-8084 (extract) llama-server entry: same contract, found by scan.
	extractLifecycle := extractPort8084Lifecycle(cfg.Providers)
	if extractLifecycle == nil {
		t.Fatal("no llama-server spawn entry on port 8084")
	}
	if !slices.Contains(extractLifecycle.SpawnCommand, "--cache-reuse") {
		t.Errorf("port-8084 spawn_command = %v, want --cache-reuse", extractLifecycle.SpawnCommand)
	}
	for _, lc := range []*RuntimeLifecycleConfig{ggufProvider.Lifecycle, extractLifecycle} {
		if i := slices.Index(lc.SpawnCommand, "--cache-reuse"); i < 0 || i+1 >= len(lc.SpawnCommand) || lc.SpawnCommand[i+1] != "256" {
			t.Errorf("spawn_command = %v, want --cache-reuse followed by 256", lc.SpawnCommand)
		}
	}
	ggufModel, ok := ggufProvider.Models["lfm-8b-gguf"]
	if !ok {
		t.Fatal("lfm-8b-gguf model not found in local-gguf provider")
	}
	if !slices.Contains(ggufModel.Capabilities, "tool_use") {
		t.Errorf("lfm-8b-gguf capabilities = %v, want tool_use", ggufModel.Capabilities)
	}
	if ggufModel.ContextLimit != 32768 {
		t.Errorf("lfm-8b-gguf context_limit = %d, want 32768", ggufModel.ContextLimit)
	}

	// Port guard. Two loopback endpoints sharing one port makes the daemon
	// adopt a foreign listener as its own runtime (2026-09-12: the MLX
	// endpoint claimed :8082, the prompt-router sidecar's port, so the
	// driver answered with the router's canned JSON). 8082 is reserved for
	// that sidecar and is not a provider port.
	seenPort := map[string]string{}
	for name, p := range cfg.Providers {
		port := loopbackPort(p.Options.BaseURL)
		if port == "" {
			continue
		}
		if port == "8082" {
			t.Errorf("provider %s uses :8082, reserved for the prompt-router sidecar", name)
		}
		if prev, dup := seenPort[port]; dup {
			t.Errorf("providers %s and %s both use loopback port %s", prev, name, port)
		}
		seenPort[port] = name
	}

	// Provider ports must also avoid the daemon's OWN transport.http
	// listener (the GUI endpoint): a runtime sharing that port number made
	// every install one bind-race away from the spawn guard refusing the
	// runtime or a client hitting the wrong server (2026-10-14: the MLX
	// classifier shipped on :8081, the GUI TLS port). Read the shipped
	// config/meept.json5's transport.http addr directly — internal/config
	// cannot be imported from here (import cycle), and the precedence to
	// mirror is ListenAddr's: addr, else the port alias, else the
	// 127.0.0.1:8081 binding default (internal/comm/http NewServer).
	transportPorts := map[string]struct{}{"8081": {}} // NewServer default
	const meeptCfgPath = "../../config/meept.json5"
	if raw, err := os.ReadFile(meeptCfgPath); err == nil {
		if std, err := hujson.Standardize(raw); err == nil {
			var mc struct {
				Transport struct {
					HTTP struct {
						Addr string `json:"addr"`
						Port int    `json:"port"`
					} `json:"http"`
				} `json:"transport"`
			}
			if json.Unmarshal(std, &mc) == nil {
				if a := mc.Transport.HTTP.Addr; a != "" {
					if _, port, err := net.SplitHostPort(a); err == nil {
						transportPorts[port] = struct{}{}
					}
				} else if mc.Transport.HTTP.Port > 0 {
					transportPorts[strconv.Itoa(mc.Transport.HTTP.Port)] = struct{}{}
				}
			}
		}
		// Missing/unparseable file: the 8081 default above still guards.
	}
	for _, clash := range transportPortClashes(seenPort, transportPorts) {
		t.Errorf("provider %s uses loopback port %s, the daemon transport.http (GUI) port — "+
			"give the runtime its own port", clash.provName, clash.port)
	}

	// (the old lfm-1.2b-q8 pin retired with the `local` provider above)

	// zai/glm removed from the shipped config 2026-10-24 (operator
	// decision: glm quota windows exhausted mid-turn and the provider was
	// dropped from both the install and the template). Pin its ABSENCE so
	// a stale copy cannot sneak back in.
	if _, ok := cfg.Providers["zai"]; ok {
		t.Error("zai provider present, want removed (dropped 2026-10-24)")
	}
	for id, alias := range cfg.ModelAliases {
		for _, m := range alias.Models {
			if p, _, _ := strings.Cut(m, "/"); p == "zai" {
				t.Errorf("alias %s still references zai member %q", id, m)
			}
		}
	}

	// agnes: single current model + defaultModel naming it. The stale
	// agnes-2.0-flash entry plus random map-order pool picks registered
	// the wrong model on the wire (2026-10-23 outage); defaultModel makes
	// the pick explicit and this pin keeps the stale entry out.
	agnesProvider, ok := cfg.Providers["agnes"]
	if !ok {
		t.Fatal("agnes provider not found")
	}
	if _, ok := agnesProvider.Models["agnes-3.0-flash"]; !ok {
		t.Errorf("agnes models = %v, want an agnes-3.0-flash entry", maps.Keys(agnesProvider.Models))
	}
	if agnesProvider.Options.DefaultModel != "agnes-3.0-flash" {
		t.Errorf("agnes defaultModel = %q, want agnes-3.0-flash", agnesProvider.Options.DefaultModel)
	}
}

// loopbackPort returns the port of a loopback baseURL, or "" when the URL is
// remote or has no port. Remote providers may share :443 without conflict, so
// only loopback endpoints are compared by the port guard in TestConfigLoads.
func loopbackPort(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return ""
	}
	return u.Port()
}

// transportPortClash names one provider registered on a daemon transport.http
// (GUI) port, as reported by the guard below.
type transportPortClash struct {
	port     string
	provName string
}

// transportPortClashes is the extracted core predicate of the ac63861e
// key/value-swap guard in TestConfigLoads (the `for port, provName := range
// seenPort` loop). seenPort maps loopback port -> provider name; transportPorts
// is the set of daemon transport.http ports. A clash is a provider whose
// registered port is in transportPorts. Extracting it here lets
// TestTransportPortClashGuard pin the guard against the key/value swap
// mutation: if someone re-swaps the map pair (rangeing values as ports and
// keys as names), this predicate flips and the pin test fails.
func transportPortClashes(seenPort map[string]string, transportPorts map[string]struct{}) []transportPortClash {
	var clashes []transportPortClash
	for port, provName := range seenPort {
		if _, clash := transportPorts[port]; clash {
			clashes = append(clashes, transportPortClash{port: port, provName: provName})
		}
	}
	// Map iteration order is random; sort for deterministic test output.
	slices.SortFunc(clashes, func(a, b transportPortClash) int {
		if a.port != b.port {
			return strings.Compare(a.port, b.port)
		}
		return strings.Compare(a.provName, b.provName)
	})
	return clashes
}

// TestTransportPortClashGuard is the mutation pin for the transport.http port
// guard in TestConfigLoads. Without it, re-swapping the seenPort map pair in
// the guard silently disables it (the guard ranges over a map[string]string
// keyed by port; swapping to `for provName, port := range seenPort` compiles
// but never matches a transport port). The guard lives in TestConfigLoads and
// cannot be extracted without this helper — the production config test is the
// only caller, and this file is a _test.go, so the predicate is replicated
// here (extracted from the guard's own range) and the guard is refactored to
// call it. See transportPortClashes.
func TestTransportPortClashGuard(t *testing.T) {
	cases := []struct {
		name           string
		seenPort       map[string]string // port -> provider name
		transportPorts map[string]struct{}
		wantClashes    []transportPortClash
	}{
		{
			name:           "provider on transport.http port fires guard",
			seenPort:       map[string]string{"8081": "mlx-classifier"},
			transportPorts: map[string]struct{}{"8081": {}},
			wantClashes:    []transportPortClash{{port: "8081", provName: "mlx-classifier"}},
		},
		{
			name: "distinct ports stay silent",
			seenPort: map[string]string{
				"8080": "local-gguf",
				"9090": "zai",
			},
			transportPorts: map[string]struct{}{"8081": {}},
			wantClashes:    nil,
		},
		{
			name: "multiple providers, one clash",
			seenPort: map[string]string{
				"8080": "local-gguf",
				"8081": "mlx-classifier",
				"9090": "zai",
			},
			transportPorts: map[string]struct{}{"8081": {}, "8443": {}},
			wantClashes:    []transportPortClash{{port: "8081", provName: "mlx-classifier"}},
		},
		{
			name:           "empty seenPort is silent",
			seenPort:       map[string]string{},
			transportPorts: map[string]struct{}{"8081": {}},
			wantClashes:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := transportPortClashes(tc.seenPort, tc.transportPorts)
			if len(got) != len(tc.wantClashes) {
				t.Fatalf("clashes = %v, want %v", got, tc.wantClashes)
			}
			for i := range got {
				if got[i] != tc.wantClashes[i] {
					t.Errorf("clash[%d] = %+v, want %+v", i, got[i], tc.wantClashes[i])
				}
			}
		})
	}
}

// extractPort8084Lifecycle finds the provider whose lifecycle spawn_command
// runs llama-server on port 8084 (the extract runtime). Contract C7 (leaf 03)
// pins --cache-reuse 256 on BOTH llama-server entries; the 8080 entry is
// pinned inline in TestConfigLoads via the local-gguf provider.
func extractPort8084Lifecycle(providers map[string]ProviderConfig) *RuntimeLifecycleConfig {
	for name := range providers {
		p := providers[name]
		if p.Lifecycle == nil {
			continue
		}
		cmd := p.Lifecycle.SpawnCommand
		isLlama := slices.Contains(cmd, "llama-server")
		portIdx := slices.Index(cmd, "8084")
		if isLlama && portIdx >= 0 {
			return p.Lifecycle
		}
	}
	return nil
}
