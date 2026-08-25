package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- helpers ---

// makeJWT builds a three-segment token whose payload carries the given
// chatgpt_account_id claim under the OpenAI auth namespace.
func makeJWT(t *testing.T, accountID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadMap := map[string]any{"sub": "user-123", "exp": 9999999999}
	if accountID != "" {
		payloadMap["https://api.openai.com/auth"] = map[string]any{
			"chatgpt_account_id": accountID,
		}
	}
	payloadJSON, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return header + "." + payload + ".sig"
}

// stubTokenResolver is a minimal TokenResolver returning fixed values.
type stubTokenResolver struct {
	token string
	err   error
}

func (s *stubTokenResolver) ResolveToken(_ context.Context, _ string) (string, error) {
	return s.token, s.err
}

// codexResponder describes one canned server response.
type codexResponder struct {
	status int
	body   string
}

// newCodexTestServer spins up an httptest server and returns it plus a
// channel delivering each captured request (method, path, headers, body).
func newCodexTestServer(t *testing.T, respond codexResponder) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		ch <- capturedRequest{
			Method:      r.Method,
			Path:        r.URL.Path,
			Header:      r.Header.Clone(),
			Body:        string(body),
			ContentType: r.Header.Get("Content-Type"),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(respond.status)
		if respond.body != "" {
			_, _ = w.Write([]byte(respond.body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

type capturedRequest struct {
	Method      string
	Path        string
	Header      http.Header
	Body        string
	ContentType string
}

func newCodexClientForTest(t *testing.T, baseURL string, opts ...CodexClientOption) *CodexClient {
	t.Helper()
	cfg := &ModelConfig{
		BaseURL:    baseURL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
	}
	return NewCodexClient(cfg, opts...)
}

// --- Task 1: codexAccountID ---

func TestCodexAccountID(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"valid JWT with account id", makeJWT(t, "acct-abc-123"), "acct-abc-123"},
		{"two segments only", "aaa.bbb", ""},
		{"garbage", "not-a-jwt", ""},
		{"empty", "", ""},
		{"valid JWT without claim", makeJWT(t, ""), ""},
		{"payload not json", base64.RawURLEncoding.EncodeToString([]byte("header")) + "." + base64.RawURLEncoding.EncodeToString([]byte("notjson")) + ".sig", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codexAccountID(tt.token); got != tt.want {
				t.Fatalf("codexAccountID(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func TestCodexAccountIDStdEncodingPadding(t *testing.T) {
	// Build a payload long enough that base64 (std, padded) differs from
	// RawURL in length, encoded with standard padding, to exercise the
	// StdEncoding fallback path.
	payload := fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":"%s"}}`, strings.Repeat("x", 61))
	enc := base64.StdEncoding.EncodeToString([]byte(payload))
	token := base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + "." + enc + ".sig"
	if got := codexAccountID(token); got != strings.Repeat("x", 61) {
		t.Fatalf("codexAccountID padded = %q, want %q", got, strings.Repeat("x", 61))
	}
}

// --- Task 2: request shape ---

func TestCodexChatRequestShape(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[],"usage":{"input_tokens":0,"output_tokens":0}}`})
	client := newCodexClientForTest(t, srv.URL,
		WithCodexTokenResolver(&stubTokenResolver{token: makeJWT(t, "acct-shape")}, "openai-codex"),
	)
	messages := []ChatMessage{
		{Role: RoleSystem, Content: "You are terse."},
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
		{Role: RoleUser, Content: "ping"},
	}
	if _, err := client.Chat(context.Background(), messages); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	select {
	case req := <-ch:
		if req.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", req.Method)
		}
		if req.Path != "/responses" {
			t.Errorf("path = %q, want /responses", req.Path)
		}
		if got := req.Header.Get("User-Agent"); got != "codex_cli_rs/0.0.0 (Hermes meept)" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := req.Header.Get("originator"); got != "codex_cli_rs" {
			t.Errorf("originator = %q", got)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer "+makeJWT(t, "acct-shape") {
			t.Errorf("Authorization = %q", got)
		}
		if got := req.Header.Get("ChatGPT-Account-ID"); got != "acct-shape" {
			t.Errorf("ChatGPT-Account-ID = %q, want acct-shape", got)
		}
		if got := req.ContentType; got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := req.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
			t.Fatalf("body not JSON: %v\nbody: %s", err, req.Body)
		}
		if body["model"] != "gpt-5.1-codex" {
			t.Errorf("model = %v", body["model"])
		}
		if body["input"] != "ping" {
			t.Errorf("input = %v, want last user message", body["input"])
		}
		if body["instructions"] != "You are terse." {
			t.Errorf("instructions = %v", body["instructions"])
		}
		if body["stream"] != false {
			t.Errorf("stream = %v, want false", body["stream"])
		}
		if body["store"] != false {
			t.Errorf("store = %v, want false", body["store"])
		}
		if _, ok := body["tools"]; ok {
			t.Errorf("tools present without WithTools")
		}
	default:
		t.Fatal("no request captured")
	}
}

func TestCodexChatAccountIDHeaderOmitted(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	client := newCodexClientForTest(t, srv.URL,
		WithCodexTokenResolver(&stubTokenResolver{token: makeJWT(t, "")}, "openai-codex"),
	)
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	select {
	case req := <-ch:
		if got := req.Header.Get("ChatGPT-Account-ID"); got != "" {
			t.Errorf("ChatGPT-Account-ID = %q, want omitted", got)
		}
	default:
		t.Fatal("no request captured")
	}
}

func TestCodexChatAPIKeyFallback(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	cfg := &ModelConfig{BaseURL: srv.URL, ModelID: "m", ProviderID: "openai-codex", APIKey: "sk-static"}
	client := NewCodexClient(cfg)
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	select {
	case req := <-ch:
		if got := req.Header.Get("Authorization"); got != "Bearer sk-static" {
			t.Errorf("Authorization = %q", got)
		}
	default:
		t.Fatal("no request captured")
	}
}

func TestCodexChatToolsShape(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	client := newCodexClientForTest(t, srv.URL)
	tools := []ToolDefinition{{
		Type: "function",
		Function: FunctionDef{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  FunctionParameters{Type: "object"},
		},
	}}
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}}, WithTools(tools)); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	select {
	case req := <-ch:
		var body map[string]any
		if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		toolsRaw, ok := body["tools"].([]any)
		if !ok || len(toolsRaw) != 1 {
			t.Fatalf("tools = %v", body["tools"])
		}
		tool, ok := toolsRaw[0].(map[string]any)
		if !ok {
			t.Fatalf("tool[0] not object: %T", toolsRaw[0])
		}
		// FLAT shape: name/description/parameters at top level, no "function" wrapper.
		if tool["type"] != "function" || tool["name"] != "get_weather" || tool["description"] != "Get weather" {
			t.Errorf("flat tool shape wrong: %v", tool)
		}
		if _, ok := tool["function"]; ok {
			t.Errorf("tool has nested \"function\" key, want flat")
		}
		if body["tool_choice"] != "auto" {
			t.Errorf("tool_choice = %v", body["tool_choice"])
		}
	default:
		t.Fatal("no request captured")
	}
}

// --- Task 2: response parsing ---

func TestCodexChatResponseParsing(t *testing.T) {
	tests := []struct {
		name string
		body string
		want Response
	}{
		{
			name: "message only",
			body: `{"output":[{"type":"message","content":[{"type":"output_text","text":"Hello!"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`,
			want: Response{Content: "Hello!", Model: "gpt-5.1-codex", FinishReason: "stop", Usage: TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
		{
			name: "reasoning plus message",
			body: `{"output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"thinking hard"}]},{"type":"message","content":[{"type":"output_text","text":"Answer"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`,
			want: Response{Content: "Answer", Reasoning: "thinking hard", Model: "gpt-5.1-codex", FinishReason: "stop", Usage: TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
		},
		{
			name: "function call",
			body: `{"output":[{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"SF\"}"}],"usage":{"input_tokens":7,"output_tokens":3}}`,
			want: Response{
				Model: "gpt-5.1-codex", FinishReason: "tool_calls",
				ToolCalls: []ToolCall{{
					ID: "call_1", Type: "function",
					Function: ToolCallFunction{Name: "get_weather", Arguments: `{"city":"SF"}`},
				}},
				Usage: TokenUsage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10},
			},
		},
		{
			name: "multiple message items concatenate",
			body: `{"output":[{"type":"message","content":[{"type":"output_text","text":"a"}]},{"type":"message","content":[{"type":"output_text","text":"b"}]}]}`,
			want: Response{Content: "ab", Model: "gpt-5.1-codex", FinishReason: "stop"},
		},
		{
			name: "empty output",
			body: `{"output":[]}`,
			want: Response{Model: "gpt-5.1-codex", FinishReason: "stop"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newCodexTestServer(t, codexResponder{status: 200, body: tt.body})
			client := newCodexClientForTest(t, srv.URL)
			resp, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if resp.Content != tt.want.Content {
				t.Errorf("Content = %q, want %q", resp.Content, tt.want.Content)
			}
			if resp.Reasoning != tt.want.Reasoning {
				t.Errorf("Reasoning = %q, want %q", resp.Reasoning, tt.want.Reasoning)
			}
			if resp.FinishReason != tt.want.FinishReason {
				t.Errorf("FinishReason = %q, want %q", resp.FinishReason, tt.want.FinishReason)
			}
			if resp.Model != tt.want.Model {
				t.Errorf("Model = %q, want %q", resp.Model, tt.want.Model)
			}
			if resp.Usage != tt.want.Usage {
				t.Errorf("Usage = %+v, want %+v", resp.Usage, tt.want.Usage)
			}
			if len(resp.ToolCalls) != len(tt.want.ToolCalls) {
				t.Fatalf("ToolCalls = %+v, want %+v", resp.ToolCalls, tt.want.ToolCalls)
			}
			for i := range tt.want.ToolCalls {
				if resp.ToolCalls[i] != tt.want.ToolCalls[i] {
					t.Errorf("ToolCalls[%d] = %+v, want %+v", i, resp.ToolCalls[i], tt.want.ToolCalls[i])
				}
			}
		})
	}
}

// --- Task 2: errors ---

func TestCodexChatErrorMapping(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexResponder{status: 401, body: `{"error":{"message":"m"}}`})
	client := newCodexClientForTest(t, srv.URL)
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var ce *ClientError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not *ClientError", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q missing 401", err.Error())
	}
	if !strings.Contains(err.Error(), "m") {
		t.Errorf("error %q missing body message", err.Error())
	}
}

func TestCodexChatTokenResolverError(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexResponder{status: 200, body: `{}`})
	client := newCodexClientForTest(t, srv.URL,
		WithCodexTokenResolver(&stubTokenResolver{err: errors.New("no token")}, "openai-codex"),
	)
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *ClientError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not *ClientError", err)
	}
	if !strings.Contains(err.Error(), "failed to resolve OAuth token") {
		t.Errorf("error = %q, want resolve failure", err.Error())
	}
}

func TestCodexChatBadJSON(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexResponder{status: 200, body: `not json`})
	client := newCodexClientForTest(t, srv.URL)
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}})
	if err == nil {
		t.Fatal("expected error for unparseable body")
	}
	var ce *ClientError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not *ClientError", err)
	}
}

// --- Task 2: options / misc ---

func TestNewCodexClientDefaults(t *testing.T) {
	client := NewCodexClient(&ModelConfig{BaseURL: "http://x", ModelID: "m"})
	if client.httpClient.Timeout != 120*time.Second {
		t.Errorf("default timeout = %v, want 120s", client.httpClient.Timeout)
	}
	if client.logger == nil {
		t.Error("default logger nil")
	}
	custom := NewCodexClient(&ModelConfig{BaseURL: "http://x", ModelID: "m"},
		WithCodexTimeout(5*time.Second),
		WithCodexLogger(slog.New(slog.DiscardHandler)),
	)
	if custom.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", custom.httpClient.Timeout)
	}
	// nil-guard: resolver not set when tr is nil.
	guarded := NewCodexClient(&ModelConfig{BaseURL: "http://x", ModelID: "m"},
		WithCodexTokenResolver(nil, "openai-codex"),
	)
	if guarded.tokenResolver != nil {
		t.Error("WithCodexTokenResolver(nil) should not set resolver")
	}
}

func TestCodexClientConfig(t *testing.T) {
	cfg := &ModelConfig{BaseURL: "http://x", ModelID: "m", ProviderID: "openai-codex"}
	client := NewCodexClient(cfg)
	if client.Config() != cfg {
		t.Error("Config() should return the config")
	}
}

func TestCodexChatWithProgress(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`})
	client := newCodexClientForTest(t, srv.URL)
	var stages []ProgressStage
	resp, err := client.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "x"}},
		func(stage ProgressStage, _ string) { stages = append(stages, stage) },
	)
	if err != nil {
		t.Fatalf("ChatWithProgress: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(stages) == 0 {
		t.Error("no progress callbacks fired")
	}
	found := false
	for _, s := range stages {
		if s == ProgressStageDone {
			found = true
		}
	}
	if !found {
		t.Errorf("stages = %v, want ProgressStageDone", stages)
	}
}

func TestCodexChatBudgetRecording(t *testing.T) {
	srv, _ := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":100,"output_tokens":50}}`})
	budget := NewBudgetFromDefaults(slog.New(slog.DiscardHandler))
	client := newCodexClientForTest(t, srv.URL, WithCodexBudget(budget))
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "x"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	status := budget.GetStatus()
	// NewBudgetFromDefaults hourly limit 500000; after one request hourly
	// used should reflect 150 tokens.
	if status.HourlyUsed != 150 {
		t.Errorf("HourlyUsed = %v, want 150", status.HourlyUsed)
	}
}

// --- Task 3: dispatch + registry ---

func TestCreateChatterForCodex(t *testing.T) {
	cfg := &ModelConfig{
		ProviderID:    "openai-codex",
		BaseURL:       "https://chatgpt.com/backend-api/codex",
		ModelID:       "gpt-5.1-codex",
		OAuthProvider: "openai-codex",
	}
	chatter := createChatterFor(cfg, nil, slog.Default(), &stubTokenResolver{token: "t"})
	cc, ok := chatter.(*CodexClient)
	if !ok {
		t.Fatalf("createChatterFor returned %T, want *CodexClient", chatter)
	}
	if cc.tokenResolver == nil {
		t.Error("token resolver not wired")
	}
	if cc.oauthProvider != "openai-codex" {
		t.Errorf("oauthProvider = %q", cc.oauthProvider)
	}

	// ProviderID-based dispatch only (ModelConfig has no Transport field).
	cfg2 := &ModelConfig{
		ProviderID: "openai-codex",
		BaseURL:    "https://example.com",
	}
	if c2, ok := createChatterFor(cfg2, nil, slog.Default(), nil).(*CodexClient); !ok {
		t.Fatalf("createChatterFor(openai-codex) returned %T", c2)
	}
}

func TestGetProviderByIDCodex(t *testing.T) {
	def, ok := GetProviderByID("openai-codex")
	if !ok {
		t.Fatal("openai-codex provider not found")
	}
	if def.AuthType != AuthOAuthDevice {
		t.Errorf("AuthType = %q, want %q", def.AuthType, AuthOAuthDevice)
	}
	if def.Transport != TransportCodexResponses {
		t.Errorf("Transport = %q", def.Transport)
	}
	if def.BaseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("BaseURL = %q", def.BaseURL)
	}
}

// Compile-time interface check.
var _ Chatter = (*CodexClient)(nil)
