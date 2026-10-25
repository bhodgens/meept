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
	if cfg.SmallModel != "local/lfm-1.2b-q8" {
		t.Errorf("small_model = %q, want local/lfm-1.2b-q8", cfg.SmallModel)
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
	if len(classifierAlias.Models) < 2 || classifierAlias.Models[1] != "local/lfm-8b-mlx-4bit" {
		t.Errorf("classifier alias alternate = %v, want local/lfm-8b-mlx-4bit at [1]", classifierAlias.Models)
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

	// Verify local provider has lfm-8b-mlx-4bit model (2026-09-06: the
	// GGUF F16 general model was retired in favor of the MLX 4-bit one).
	localProvider, ok := cfg.Providers["local"]
	if !ok {
		t.Fatal("local provider not found")
	}

	lfm8b, ok := localProvider.Models["lfm-8b-mlx-4bit"]
	if !ok {
		t.Fatal("lfm-8b-mlx-4bit model not found in local provider")
	}
	// 2026-10-24: the shipped config roots model paths at
	// ${MEEPT_MODELS_DIR:-$HOME/.meept/models} — the layout the
	// make deps-models downloader writes. LoadProvidersConfig expands env
	// vars at load time, so the expected value is built from the same
	// default the config declares (MEEPT_MODELS_DIR wins when exported).
	modelsDir := os.Getenv("MEEPT_MODELS_DIR")
	if modelsDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("cannot resolve HOME for the models-dir default: %v", err)
		}
		modelsDir = home + "/.meept/models"
	}
	wantMlxPath := modelsDir + "/LiquidAI/LFM2.5-8B-A1B-MLX-4bit"
	if lfm8b.Name != wantMlxPath {
		t.Errorf("lfm-8b-mlx-4bit name = %q, want %q", lfm8b.Name, wantMlxPath)
	}
	if lfm8b.MaxConcurrency != 2 {
		t.Errorf("lfm-8b-mlx-4bit max_concurrency = %d, want 2", lfm8b.MaxConcurrency)
	}
	if lfm8b.ContextLimit != 16384 {
		t.Errorf("lfm-8b-mlx-4bit context_limit = %d, want 16384", lfm8b.ContextLimit)
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
	ggufModel, ok := ggufProvider.Models["lfm-8b-gguf"]
	if !ok {
		t.Fatal("lfm-8b-gguf model not found in local-gguf provider")
	}
	if !slices.Contains(ggufModel.Capabilities, "tool_use") {
		t.Errorf("lfm-8b-gguf capabilities = %v, want tool_use", ggufModel.Capabilities)
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

	// Verify lfm-1.2b-q8 model (primary small model)
	lfm12b, ok := localProvider.Models["lfm-1.2b-q8"]
	if !ok {
		t.Fatal("lfm-1.2b-q8 model not found in local provider")
	}
	if lfm12b.ContextLimit != 8192 {
		t.Errorf("lfm-1.2b-q8 context_limit = %d, want 8192", lfm12b.ContextLimit)
	}

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
