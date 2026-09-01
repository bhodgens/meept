package llm

import "testing"

func TestGetProviderByID(t *testing.T) {
	tests := []struct {
		id        string
		wantFound bool
		wantID    string
	}{
		{"anthropic", true, "anthropic"},
		{"openrouter", true, "openrouter"},
		{"openai", true, "openai"},
		{"ollama", true, "ollama"},
		{"zai", true, "zai"},
		{"unknown", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			p, found := GetProviderByID(tt.id)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if found && p.ID != tt.wantID {
				t.Errorf("got ID = %s, want %s", p.ID, tt.wantID)
			}
		})
	}
}

func TestGetProviderByEnvVar(t *testing.T) {
	tests := []struct {
		envVar    string
		wantFound bool
		wantID    string
	}{
		{"ANTHROPIC_API_KEY", true, "anthropic"},
		{"OPENAI_API_KEY", true, "openai"},
		{"ZAI_API_KEY", true, "zai"},
		{"UNKNOWN_KEY", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.envVar, func(t *testing.T) {
			p, found := GetProviderByEnvVar(tt.envVar)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if found && p.ID != tt.wantID {
				t.Errorf("got ID = %s, want %s", p.ID, tt.wantID)
			}
		})
	}
}

func TestListProviders(t *testing.T) {
	all := ListProviders("")
	if len(all) == 0 {
		t.Fatal("expected some providers")
	}

	openaiProviders := ListProviders(TransportOpenAIChat)
	if len(openaiProviders) == 0 {
		t.Fatal("expected OpenAI-compatible providers")
	}

	// Verify all returned providers have OpenAI transport
	for _, p := range openaiProviders {
		if p.Transport != TransportOpenAIChat {
			t.Errorf("provider %s has transport %s, want %s", p.ID, p.Transport, TransportOpenAIChat)
		}
	}

	anthropicProviders := ListProviders(TransportAnthropicMessages)
	// anthropic (API key) + anthropic-sub (OAuth device).
	if len(anthropicProviders) != 2 {
		t.Errorf("expected 2 Anthropic providers, got %d", len(anthropicProviders))
	}
}

func TestCanonicalProviders(t *testing.T) {
	// Verify all providers have required fields
	for _, p := range CanonicalProviders {
		if p.ID == "" {
			t.Error("provider missing ID")
		}
		if p.Name == "" {
			t.Error("provider missing Name")
		}
		if p.Transport == "" {
			t.Error("provider missing Transport")
		}
		if p.Supports == nil {
			t.Error("provider missing Supports")
		}
	}
}

// TestGetProviderByIDLmStudio pins the LM Studio registry entry
// field-for-field (llm-resilience-forest tree 05 leaf 03 Task 1; D12
// "same treatment as Ollama").
func TestGetProviderByIDLmStudio(t *testing.T) {
	p, found := GetProviderByID(ProviderIDLMStudio)
	if !found {
		t.Fatalf("provider %q not registered", ProviderIDLMStudio)
	}
	if p.ID != "lmstudio" {
		t.Errorf("ID = %q, want lmstudio", p.ID)
	}
	if p.Name != "LM Studio" {
		t.Errorf("Name = %q, want %q", p.Name, "LM Studio")
	}
	if p.Transport != TransportOpenAIChat {
		t.Errorf("Transport = %q, want %q", p.Transport, TransportOpenAIChat)
	}
	// Local provider pattern: AuthEnvVar with NO APIKeyEnvVar set
	// (there is no AuthNone const; this IS the Ollama shape).
	if p.AuthType != AuthEnvVar {
		t.Errorf("AuthType = %q, want %q", p.AuthType, AuthEnvVar)
	}
	if p.APIKeyEnvVar != "" {
		t.Errorf("APIKeyEnvVar = %q, want empty (local provider)", p.APIKeyEnvVar)
	}
	if p.BaseURL != "http://localhost:1234/v1" {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, "http://localhost:1234/v1")
	}
	if p.DocURL != "https://lmstudio.ai/docs/api" {
		t.Errorf("DocURL = %q, want %q", p.DocURL, "https://lmstudio.ai/docs/api")
	}
	wantCaps := []string{CapStreaming, CapTools, "local"}
	if len(p.Supports) != len(wantCaps) {
		t.Fatalf("Supports = %v, want %v", p.Supports, wantCaps)
	}
	for i, want := range wantCaps {
		if p.Supports[i] != want {
			t.Errorf("Supports[%d] = %q, want %q", i, p.Supports[i], want)
		}
	}
}

// TestListProvidersIncludesLMStudio verifies the entry resolves through
// the OpenAI-compat transport listing (leaf Task 1 "transport resolves").
func TestListProvidersIncludesLMStudio(t *testing.T) {
	found := false
	for _, p := range ListProviders(TransportOpenAIChat) {
		if p.ID == ProviderIDLMStudio {
			found = true
			break
		}
	}
	if !found {
		t.Error("lmstudio missing from ListProviders(TransportOpenAIChat)")
	}
}
