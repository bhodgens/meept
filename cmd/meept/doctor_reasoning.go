package main

// doctor_reasoning.go: the report-only `meept doctor` reasoning-wire-form
// check (docs/plans/20261007-reasoning-history-strip, master Contract 3).
//
// For every configured provider whose lifecycle is local (loopback base URL)
// and that declares at least one model with the `reasoning` capability, ONE
// minimal chat completion is sent and the response wire form is classified:
//
//   - reasoning_content / reasoning populated, content clean → ok
//     ("separate reasoning channel")
//   - <think> tags inside content → warn (endpoint burns output tokens on
//     reasoning; suggest --jinja with a reasoning parser / reasoning_format)
//   - reasoning-style prose in content, no tags, no separate field → warn
//     ("reasoning may be leaking into content untagged")
//   - connection error / timeout → skip semantics (ok=true, warn=true)
//
// Doctor convention: report-only — no restarts, no config writes, no --fix
// behavior, one probe per endpoint, never a retry loop. Cloud (non-loopback)
// providers are skipped entirely: doctor makes no network calls to remote
// APIs.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// reasoningProbeTimeout bounds the single probe per endpoint.
const reasoningProbeTimeout = 10 * time.Second

// reasoningProbePrompt is the frozen probe user message.
const reasoningProbePrompt = "Reply with the word: ready"

// probeEndpointFn is the HTTP seam: run ONE probe against baseURL for the
// named model serving name and return (status, raw body, err). Production is
// probeReasoningEndpoint; tests inject httptest-backed fakes.
type probeEndpointFn func(ctx context.Context, baseURL, modelName string) (int, []byte, error)

// checkReasoningDoctor classifies the reasoning wire form of every local
// reasoning-capable endpoint in the loaded models config. One check line per
// probed endpoint (prefix "reasoning:"), zero lines when nothing qualifies.
// The HTTP layer sits behind the probeEndpointFn seam (model serving name +
// base URL) so tests inject fakes (see doctor_reasoning_test.go).
func checkReasoningDoctor(cfg *llm.ProvidersConfig) []doctorCheck {
	return checkReasoningDoctorWith(cfg, probeReasoningEndpoint)
}

// checkReasoningDoctorWith is the seam-injected core. probeEndpointFn takes
// (ctx, baseURL, modelServingName) so the production prober gets the
// reasoning-capable model's serving name while fakes can ignore it.
func checkReasoningDoctorWith(cfg *llm.ProvidersConfig, probe probeEndpointFn) []doctorCheck {
	if cfg == nil {
		return nil
	}

	// Deterministic order: sorted provider IDs.
	provIDs := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		provIDs = append(provIDs, id)
	}
	sort.Strings(provIDs)

	var checks []doctorCheck
	for _, pid := range provIDs {
		p := cfg.Providers[pid]
		if p.Lifecycle == nil {
			continue
		}
		baseURL := p.Options.BaseURL
		// Cloud guard: never send doctor probes to remote APIs.
		if !llm.IsLoopbackBaseURL(baseURL) {
			continue
		}
		// Only reasoning-capable providers are probed.
		modelName, ok := firstReasoningModel(p.Models)
		if !ok {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), reasoningProbeTimeout)
		status, body, err := probe(ctx, baseURL, modelName)
		cancel()
		if err != nil {
			checks = append(checks, doctorCheck{
				name: "reasoning:" + pid,
				ok:   true,
				warn: true,
				detail: fmt.Sprintf("%s unreachable — check skipped (%v)",
					displayEndpoint(baseURL), err),
			})
			continue
		}
		if status != http.StatusOK {
			checks = append(checks, doctorCheck{
				name: "reasoning:" + pid,
				ok:   true,
				warn: true,
				detail: fmt.Sprintf("%s answered http %d — check skipped",
					displayEndpoint(baseURL), status),
			})
			continue
		}

		var completion struct {
			Choices []struct {
				Message struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					Reasoning        string `json:"reasoning"`
				} `json:"message"`
			} `json:"choices"`
		}
		if jsonErr := json.Unmarshal(body, &completion); jsonErr != nil {
			checks = append(checks, doctorCheck{
				name: "reasoning:" + pid,
				ok:   true,
				warn: true,
				detail: fmt.Sprintf("%s returned an unparseable completion body — check skipped (%v)",
					displayEndpoint(baseURL), jsonErr),
			})
			continue
		}
		content, reasoning := "", ""
		if len(completion.Choices) > 0 {
			content = completion.Choices[0].Message.Content
			reasoning = completion.Choices[0].Message.ReasoningContent
			if reasoning == "" {
				reasoning = completion.Choices[0].Message.Reasoning
			}
		}
		okLine, warnLine, detail := classifyReasoningWireForm(content, reasoning)
		checks = append(checks, doctorCheck{
			name:   "reasoning:" + pid,
			ok:     okLine,
			warn:   warnLine,
			detail: detail + fmt.Sprintf(" (probed %s, model %s)", displayEndpoint(baseURL), modelName),
		})
	}
	return checks
}

// firstReasoningModel returns the serving name of the first model (sorted
// map key) that declares the reasoning capability, and whether one exists.
func firstReasoningModel(models map[string]llm.ModelDef) (string, bool) {
	keys := make([]string, 0, len(models))
	for k := range models {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m := models[k]
		for _, capName := range m.Capabilities {
			if capName == llm.CapReasoning {
				if m.Name != "" {
					return m.Name, true
				}
				return k, true
			}
		}
	}
	return "", false
}

// classifyReasoningWireForm maps one completion's wire data to the doctor
// line: (ok, warn, detail). Pure function, pinned by TestClassifyReasoningWireForm.
func classifyReasoningWireForm(content, reasoning string) (ok, warn bool, detail string) {
	trimmed := strings.TrimSpace(content)
	switch {
	case reasoning != "" && !strings.Contains(content, "<think>"):
		return true, false, "separate reasoning channel"
	case strings.Contains(content, "<think>"):
		return false, true, "inline reasoning tags — meept strips them from history, " +
			"but the endpoint burns output tokens on reasoning; enable --jinja with " +
			"a reasoning parser (llama.cpp) or reasoning_format to split the channel"
	case looksLikeReasoningProse(trimmed):
		return false, true, "reasoning may be leaking into content untagged"
	default:
		return true, false, "content clean (no reasoning observed on the wire)"
	}
}

// reasoningProseMarkers are structural hints of chain-of-thought prose:
// enumerations and self-reference. Kept deliberately conservative — a plain
// correct answer ("ready") must never warn.
var reasoningProseMarkers = regexp.MustCompile(
	`(?im)^(step \d+|first(ly)?)[,: ]|^(okay|ok|alright|let me|hmm)\b`)

// looksLikeReasoningProse reports whether content smells like untagged
// chain-of-thought: a marker plus enough prose to look like reasoning rather
// than a terse answer.
func looksLikeReasoningProse(content string) bool {
	if len(content) < 40 {
		return false
	}
	return reasoningProseMarkers.MatchString(content)
}

// displayEndpoint strips the scheme for the report line (matches the compact
// house style of the other doctor details).
func displayEndpoint(baseURL string) string {
	return strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
}

// probeReasoningEndpoint is the production probeEndpointFn: it builds and
// sends ONE non-streaming chat completion for one endpoint. The model name is
// the reasoning-capable model's serving name (frozen contract).
func probeReasoningEndpoint(ctx context.Context, baseURL, modelName string) (int, []byte, error) {
	payload, err := json.Marshal(map[string]any{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": reasoningProbePrompt},
		},
		"max_tokens": 32,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("marshal probe body: %w", err)
	}
	url := strings.TrimSuffix(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return 0, nil, fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // body fully read below
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, buf, nil
}
