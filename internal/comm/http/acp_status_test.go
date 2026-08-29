package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type acpListEnvelope struct {
	Enabled bool               `json:"enabled" yaml:"enabled"`
	Agents  []acpAgentEnvelope `json:"agents" yaml:"agents"`
}

type acpAgentEnvelope struct {
	ID      string `json:"id" yaml:"id"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Running bool   `json:"running" yaml:"running"`
	State   string `json:"state" yaml:"state"`
	UptimeS int    `json:"uptime_s" yaml:"uptime_s"`
}

func TestACPAgentsList_NilRPCCallReturnsDisabledEnvelope(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/agents", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPAgentsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got acpListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if got.Agents == nil {
		t.Fatal("agents = nil, want empty slice")
	}
	if len(got.Agents) != 0 {
		t.Fatalf("agents len = %d, want 0", len(got.Agents))
	}
}

func TestACPAgentsList_FakeRPCCallReturnsEnvelope(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method != "acp.list" {
				t.Errorf("rpc method = %q, want acp.list", method)
			}
			return acpListEnvelope{
				Enabled: true,
				Agents: []acpAgentEnvelope{
					{
						ID:      "codex",
						Enabled: true,
						Running: true,
						State:   "ready",
						UptimeS: 0,
					},
				},
			}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/agents", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPAgentsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got acpListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled {
		t.Fatal("enabled = false, want true")
	}
	if len(got.Agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(got.Agents))
	}
	a := got.Agents[0]
	if a.ID != "codex" || !a.Enabled || !a.Running || a.State != "ready" || a.UptimeS != 0 {
		t.Errorf("agent = %+v", a)
	}
}

func TestACPAgentsList_RPCErrorLowercase(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errors.New("ACP Not Available")
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/agents", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPAgentsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msg := body["error"]
	if msg == "" {
		t.Fatal("missing error field")
	}
	if msg != strings.ToLower(msg) {
		t.Errorf("error %q is not lowercase", msg)
	}
}
