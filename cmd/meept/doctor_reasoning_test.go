package main

// doctor_reasoning_test.go: TDD tests for the report-only doctor
// reasoning-wire-form check (docs/plans/20261007-reasoning-history-strip/
// 02-doctor-check.md, master Contract 3).
//
// Every classification shape is served by an httptest fake endpoint; the
// prober seam (probeFunc) lets each test inject its fake without touching
// internal/llm. The fake returns an OpenAI-compatible chat.completion body.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// fakeProbe builds a probeEndpointFn that always hits srv (ignoring the
// configured base URL) and always succeeds at the HTTP layer, returning the
// handler's status and body.
func fakeProbe(srv *httptest.Server) probeEndpointFn {
	return func(ctx context.Context, baseURL, modelName string) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/chat/completions", nil)
		if err != nil {
			return 0, nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close() //nolint:errcheck // read below
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, rerr := resp.Body.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if rerr != nil {
				break
			}
		}
		return resp.StatusCode, buf, nil
	}
}

// deadProbe is a prober whose endpoint never answers (connection refused).
func deadProbe() probeEndpointFn {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // shut it down: any request is a connection error
	return func(ctx context.Context, baseURL, modelName string) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/chat/completions", nil)
		if err != nil {
			return 0, nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close() //nolint:errcheck // unreachable in practice
		buf := make([]byte, 0, 1024)
		_, _ = resp.Body.Read(buf)
		return resp.StatusCode, buf, nil
	}
}

// openAICompatBody renders one non-streaming chat.completion JSON body.
func openAICompatBody(content, reasoningContent, reasoning string) []byte {
	msg := map[string]any{"role": "assistant", "content": content}
	if reasoningContent != "" {
		msg["reasoning_content"] = reasoningContent
	}
	if reasoning != "" {
		msg["reasoning"] = reasoning
	}
	body, err := json.Marshal(map[string]any{
		"id":      "chatcmpl-doctor-fake",
		"object":  "chat.completion",
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}},
	})
	if err != nil {
		panic(err)
	}
	return body
}

// reasoningCfg builds a minimal providers config: one loopback lifecycle
// provider with one reasoning-capable model, pointing at baseURL.
func reasoningCfg(baseURL string) *llm.ProvidersConfig {
	return &llm.ProvidersConfig{
		Providers: map[string]llm.ProviderConfig{
			"fake-local": {
				API: "openai",
				Options: llm.ProviderOptionsConfig{
					BaseURL: baseURL,
				},
				Lifecycle: &llm.RuntimeLifecycleConfig{
					Runtime:   "llama-cpp",
					ModelPath: "/tmp/fake-model.gguf",
				},
				Models: map[string]llm.ModelDef{
					"fake-model": {
						Name:         "fake-model-serving-name",
						Capabilities: []string{"completion", "reasoning"},
					},
				},
			},
		},
	}
}

// TestCheckReasoningDoctor_SeparateChannel: reasoning_content populated,
// content clean → ok, "separate reasoning channel".
func TestCheckReasoningDoctor_SeparateChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAICompatBody("ready", "I should say ready.", ""))
	}))
	defer srv.Close()

	checks := checkReasoningDoctorWith(reasoningCfg(srv.URL), fakeProbe(srv))
	if len(checks) != 1 {
		t.Fatalf("want 1 check, got %d: %+v", len(checks), checks)
	}
	c := checks[0]
	if !c.ok {
		t.Errorf("ok = false, want true (detail %q)", c.detail)
	}
	if c.warn {
		t.Errorf("warn = true, want false (detail %q)", c.detail)
	}
	if !strings.Contains(c.detail, "separate reasoning channel") {
		t.Errorf("detail %q missing 'separate reasoning channel'", c.detail)
	}
}

// TestCheckReasoningDoctor_InlineTags: <think> inside content → warn.
func TestCheckReasoningDoctor_InlineTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAICompatBody("<think>I should say ready.</think>ready", "", ""))
	}))
	defer srv.Close()

	checks := checkReasoningDoctorWith(reasoningCfg(srv.URL), fakeProbe(srv))
	if len(checks) != 1 {
		t.Fatalf("want 1 check, got %d: %+v", len(checks), checks)
	}
	c := checks[0]
	if c.ok {
		t.Errorf("ok = true, want false (detail %q)", c.detail)
	}
	if !c.warn {
		t.Errorf("warn = false, want true (detail %q)", c.detail)
	}
	if !strings.Contains(c.detail, "inline reasoning tags") {
		t.Errorf("detail %q missing 'inline reasoning tags'", c.detail)
	}
}

// TestCheckReasoningDoctor_UntaggedLeak: reasoning-style prose in content,
// no tags, no separate field → warn.
func TestCheckReasoningDoctor_UntaggedLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAICompatBody(
			"Okay, the user wants the word ready. Step 1: analyze. Step 2: answer. ready", "", ""))
	}))
	defer srv.Close()

	checks := checkReasoningDoctorWith(reasoningCfg(srv.URL), fakeProbe(srv))
	if len(checks) != 1 {
		t.Fatalf("want 1 check, got %d: %+v", len(checks), checks)
	}
	c := checks[0]
	if c.ok {
		t.Errorf("ok = true, want false (detail %q)", c.detail)
	}
	if !c.warn {
		t.Errorf("warn = false, want true (detail %q)", c.detail)
	}
	if !strings.Contains(c.detail, "untagged") {
		t.Errorf("detail %q missing 'untagged'", c.detail)
	}
}

// TestCheckReasoningDoctor_Unreachable: connection error → skip semantics
// (ok=true, warn=true, "endpoint unreachable").
func TestCheckReasoningDoctor_Unreachable(t *testing.T) {
	checks := checkReasoningDoctorWith(reasoningCfg("http://127.0.0.1:1"), deadProbe())
	if len(checks) != 1 {
		t.Fatalf("want 1 check, got %d: %+v", len(checks), checks)
	}
	c := checks[0]
	if !c.ok {
		t.Errorf("ok = false, want true (skip semantics; detail %q)", c.detail)
	}
	if !c.warn {
		t.Errorf("warn = false, want true (skip semantics; detail %q)", c.detail)
	}
	if !strings.Contains(c.detail, "unreachable") {
		t.Errorf("detail %q missing 'unreachable'", c.detail)
	}
}

// TestCheckReasoningDoctor_CloudProviderSkipped: a non-loopback baseURL is
// probed ZERO times (no network calls to remote APIs from doctor) — the
// injected prober would fail the test if called.
func TestCheckReasoningDoctor_CloudProviderSkipped(t *testing.T) {
	cfg := reasoningCfg("https://api.example.com/v1")
	called := false
	probe := func(ctx context.Context, baseURL, modelName string) (int, []byte, error) {
		called = true
		return 500, nil, nil
	}
	checks := checkReasoningDoctorWith(cfg, probe)
	if called {
		t.Fatal("prober called for a cloud (non-loopback) provider; doctor must not make remote network calls")
	}
	if len(checks) != 0 {
		t.Fatalf("want 0 checks for cloud provider, got %+v", checks)
	}
}

// TestCheckReasoningDoctor_NoReasoningModel: lifecycle present but the only
// model lacks the reasoning capability → zero lines.
func TestCheckReasoningDoctor_NoReasoningModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("prober called for a provider with no reasoning-capable model")
	}))
	defer srv.Close()

	cfg := reasoningCfg(srv.URL)
	p := cfg.Providers["fake-local"]
	p.Models = map[string]llm.ModelDef{
		"plain-model": {Name: "plain", Capabilities: []string{"completion"}},
	}
	cfg.Providers["fake-local"] = p

	checks := checkReasoningDoctorWith(cfg, fakeProbe(srv))
	if len(checks) != 0 {
		t.Fatalf("want 0 checks when no reasoning-capable model, got %+v", checks)
	}
}

// TestCheckReasoningDoctor_NilLifecycle: no lifecycle → zero lines
// (cloud-only providers produce nothing, same as checkModelsDoctor).
func TestCheckReasoningDoctor_NilLifecycle(t *testing.T) {
	cfg := reasoningCfg("http://127.0.0.1:9999")
	p := cfg.Providers["fake-local"]
	p.Lifecycle = nil
	cfg.Providers["fake-local"] = p

	checks := checkReasoningDoctorWith(cfg, deadProbe())
	if len(checks) != 0 {
		t.Fatalf("want 0 checks for nil lifecycle, got %+v", checks)
	}
}

// TestCheckReasoningDoctor_ModelNameInRequestBody: the production prober
// (probeReasoningEndpoint) carries the reasoning-capable model's serving name
// in the request body (frozen contract) and a bounded max_tokens.
func TestCheckReasoningDoctor_ModelNameInRequestBody(t *testing.T) {
	var gotModel string
	var gotMaxTokens float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model     string  `json:"model"`
			MaxTokens float64 `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		gotModel = req.Model
		gotMaxTokens = req.MaxTokens
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAICompatBody("ready", "thinking", ""))
	}))
	defer srv.Close()

	cfg := reasoningCfg(srv.URL)
	checks := checkReasoningDoctor(cfg)
	if len(checks) != 1 {
		t.Fatalf("want 1 check, got %+v", checks)
	}
	if gotModel != "fake-model-serving-name" {
		t.Errorf("request model = %q, want serving name %q", gotModel, "fake-model-serving-name")
	}
	if gotMaxTokens != 32 {
		t.Errorf("request max_tokens = %v, want 32", gotMaxTokens)
	}
}

// TestClassifyReasoningWireForm pins the classifier itself on raw wire data
// (the full matrix, unit-level, no HTTP).
func TestClassifyReasoningWireForm(t *testing.T) {
	cases := []struct {
		name         string
		content      string
		reasoning    string
		wantOK       bool
		wantWarn     bool
		wantFragment string
	}{
		{"separate_channel_reasoning_content", "ready", "thinking", true, false, "separate reasoning channel"},
		{"separate_channel_reasoning_field", "ready", "thinking", true, false, "separate reasoning channel"},
		{"inline_tags", "<think>hm</think>ready", "", false, true, "inline reasoning tags"},
		{"untagged_leak", "let me think about this carefully. first, second. ready", "", false, true, "untagged"},
		{"clean_content_no_reasoning", "ready", "", true, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, warn, detail := classifyReasoningWireForm(tc.content, tc.reasoning)
			if ok != tc.wantOK || warn != tc.wantWarn {
				t.Errorf("classify(%q, %q) = (ok=%v, warn=%v), want (ok=%v, warn=%v) detail %q",
					tc.content, tc.reasoning, ok, warn, tc.wantOK, tc.wantWarn, tc.wantFragment)
			}
			if tc.wantFragment != "" && !strings.Contains(detail, tc.wantFragment) {
				t.Errorf("detail %q missing %q", detail, tc.wantFragment)
			}
		})
	}
}
