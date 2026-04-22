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
	if len(anthropicProviders) != 1 {
		t.Errorf("expected 1 Anthropic provider, got %d", len(anthropicProviders))
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
