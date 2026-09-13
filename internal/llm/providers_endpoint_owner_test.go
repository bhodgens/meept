package llm

import "testing"

// TestMergeProvidersConfig_UserProviderOwnsSharedEndpoint is the regression for
// the 127.0.0.1:8080 collision observed on 2026-09-13: the bundled config ships
// local-gguf on 8080 and the user's models.json5 declared `local` on the same
// port, so the runtime manager logged "Conflicting spawn_command for shared
// endpoint; keeping the first" and silently ran the bundled spawn_command -
// from a stale llama.cpp binary the user had replaced.
func TestMergeProvidersConfig_UserProviderOwnsSharedEndpoint(t *testing.T) {
	base := &ProvidersConfig{Providers: map[string]ProviderConfig{
		"local-gguf": {Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1"}},
		"agnes":      {Options: ProviderOptionsConfig{BaseURL: "https://api.example.com/v1"}},
	}}
	overlay := &ProvidersConfig{Providers: map[string]ProviderConfig{
		"local": {Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1/"}},
	}}

	merged := MergeProvidersConfig(base, overlay)
	if _, ok := merged.Providers["local-gguf"]; ok {
		t.Fatal("bundled provider on the user's endpoint must be dropped")
	}
	if _, ok := merged.Providers["local"]; !ok {
		t.Fatal("the user's provider must survive")
	}
	if _, ok := merged.Providers["agnes"]; !ok {
		t.Fatal("a provider on a distinct endpoint must be untouched")
	}
}

// TestMergeProvidersConfig_SameIDStillMerges pins that the endpoint rule does
// not disturb the normal same-ID overlay (the user tweaking a bundled entry).
func TestMergeProvidersConfig_SameIDStillMerges(t *testing.T) {
	base := &ProvidersConfig{Providers: map[string]ProviderConfig{
		"local-gguf": {
			Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1"},
			Models:  map[string]ModelDef{"a": {Name: "a"}},
		},
	}}
	overlay := &ProvidersConfig{Providers: map[string]ProviderConfig{
		"local-gguf": {Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1"}},
	}}

	merged := MergeProvidersConfig(base, overlay)
	if len(merged.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(merged.Providers))
	}
	if _, ok := merged.Providers["local-gguf"].Models["a"]; !ok {
		t.Fatal("same-ID merge must keep the base model definitions")
	}
}

// TestMergeProvidersConfig_NoOverlayOrNoURLs covers the trivial and degenerate
// inputs: no overlay, an unparseable base URL, and an empty overlay list.
func TestMergeProvidersConfig_NoOverlayOrNoURLs(t *testing.T) {
	base := &ProvidersConfig{Providers: map[string]ProviderConfig{
		"weird": {Options: ProviderOptionsConfig{BaseURL: ""}},
		"ok":    {Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:9000/v1"}},
	}}
	if got := MergeProvidersConfig(base, nil); len(got.Providers) != 2 {
		t.Fatalf("nil overlay must keep both providers, got %d", len(got.Providers))
	}
	overlay := &ProvidersConfig{Providers: map[string]ProviderConfig{
		"mine": {Options: ProviderOptionsConfig{BaseURL: ""}},
	}}
	merged := MergeProvidersConfig(base, overlay)
	if len(merged.Providers) != 3 {
		t.Fatalf("a provider with no base URL claims nothing, got %d providers", len(merged.Providers))
	}
}

// TestEndpointHostPort pins the normalization the rule depends on.
func TestEndpointHostPort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://127.0.0.1:8080/v1", "127.0.0.1:8080"},
		{"http://127.0.0.1:8080/v1/", "127.0.0.1:8080"},
		{"https://API.Example.com/v1", "api.example.com"},
		{"http://127.0.0.1:8080", "127.0.0.1:8080"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := endpointHostPort(c.in); got != c.want {
			t.Errorf("endpointHostPort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
