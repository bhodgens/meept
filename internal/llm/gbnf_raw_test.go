package llm

import "testing"

// TestAttachRawGrammar_DoesNotRequireTools pins the reason this seam exists:
// no tools in chatOpts, yet the grammar still attaches for a local endpoint.
// No global switch involved: WithRawGrammar is an explicit per-call opt-in,
// gated only on the endpoint being local (llama.cpp grammar wire field).
func TestAttachRawGrammar_DoesNotRequireTools(t *testing.T) {
	opts := &chatOptions{}
	WithRawGrammar("root ::= \"x\"")(opts)
	if len(opts.tools) != 0 {
		t.Fatal("test precondition broken: tools must be empty")
	}
	payload := map[string]any{}
	attachRawGrammar(payload, &ModelConfig{BaseURL: "http://127.0.0.1:8080/v1"}, opts)
	if _, ok := payload["grammar"]; !ok {
		t.Fatal("raw grammar must attach on tool-free local requests")
	}
}

// TestAttachRawGrammar_LocalEndpointGate pins the cloud-safety gate: the
// "grammar" wire field is a llama.cpp extension and must never reach a
// non-local endpoint, regardless of caller opt-in.
func TestAttachRawGrammar_LocalEndpointGate(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"loopback ip", "http://127.0.0.1:8080/v1", true},
		{"localhost", "http://localhost:8080/v1", true},
		{"cloud", "https://api.z.ai/api/coding/paas/v4", false},
		{"empty", "", false},
		{"garbage", "::::", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := &chatOptions{}
			WithRawGrammar("root ::= \"x\"")(opts)
			payload := map[string]any{}
			attachRawGrammar(payload, &ModelConfig{BaseURL: tc.baseURL}, opts)
			_, got := payload["grammar"]
			if got != tc.want {
				t.Fatalf("grammar attached = %v, want %v (baseURL %q)", got, tc.want, tc.baseURL)
			}
		})
	}
}

// TestIsLocalEndpoint covers the classifier directly.
func TestIsLocalEndpoint(t *testing.T) {
	if !isLocalEndpoint("http://localhost:8084/v1") {
		t.Fatal("localhost must classify local")
	}
	if isLocalEndpoint("https://example.com/v1") {
		t.Fatal("public host must not classify local")
	}
}
