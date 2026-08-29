package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Reconfigure_SwapsConfig(t *testing.T) {
	var aCalls, bCalls int
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"from-a"}}]}`))
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bCalls++
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"intent":"code"}`}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer serverB.Close()

	cfgA := &ModelConfig{BaseURL: serverA.URL, ModelID: "model-a"}
	cfgB := &ModelConfig{BaseURL: serverB.URL, ModelID: "model-b"}

	client := NewClient(cfgA)
	ctx := context.Background()

	resp, err := client.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("initial chat failed: %v", err)
	}
	if resp.Content != "from-a" {
		t.Fatalf("expected 'from-a', got %q", resp.Content)
	}

	client.Reconfigure(cfgB)

	resp, err = client.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi again"}})
	if err != nil {
		t.Fatalf("post-reconfigure chat failed: %v", err)
	}
	if resp.Content != `{"intent":"code"}` {
		t.Fatalf("expected server B response after Reconfigure, got %q", resp.Content)
	}

	if aCalls != 1 || bCalls != 1 {
		t.Errorf("expected 1 call to each server, got A=%d B=%d", aCalls, bCalls)
	}
}

func TestClient_Reconfigure_NilIsNoOp(t *testing.T) {
	cfgA := &ModelConfig{BaseURL: "http://example.invalid", ModelID: "model-a"}
	client := NewClient(cfgA)
	client.Reconfigure(nil)
	if got := client.config; got != cfgA {
		t.Errorf("Reconfigure(nil) changed config: %+v", got)
	}
}
