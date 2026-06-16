package agent

import (
	"testing"
)

func TestModelReassignmentParser_Parse(t *testing.T) {
	parser := NewModelReassignmentParser()

	tests := []struct {
		name              string
		input             string
		wantFound         bool
		wantScope         string
		wantModelCount    int
		wantClarification bool
	}{
		// Specific model references - no clarification needed
		{"specific model - synthesis", "use glm-4.7 for synthesis", true, "synthesis", 1, false},
		{"specific model - planning", "GLM models for planning", true, "planning", 1, true},        // GLM is provider, ambiguous
		{"specific model - research", "local models only for research", true, "research", 1, true}, // local is provider, ambiguous
		{"specific model - claude-opus", "synthesize using claude-opus", true, "synthesize", 1, false},
		{"specific model - qwen-coder", "code with qwen-coder", true, "code", 1, false},
		{"specific model - synthesis task", "glm-4.7 for the synthesis task", true, "synthesis", 1, false},

		// Provider-level references - need clarification
		{"provider - GLM for coding", "use GLM for coding", true, "coding", 1, true},
		{"provider - GLM for planning", "GLM models for planning", true, "planning", 1, true},
		{"provider - local for research", "local models only for research", true, "research", 1, true},
		{"provider - GLM to handle", "I want GLM to handle coding", true, "coding", 1, true},

		// No scope - need clarification
		{"no scope - do this with", "do this with GLM-4.7", true, "", 1, true},

		// Ambiguous cases - these patterns should now match
		{"ambiguous - local models", "use local models", true, "", 1, true},
		{"ambiguous - no scope", "use GLM models", true, "", 1, true},
		{"ambiguous - glm provider", "use glm models for this", true, "", 1, true},

		// No match
		{"no match - simple", "do this task", false, "", 0, false},
		{"no match - chat", "hello how are you", false, "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.Parse(tt.input)

			if result.Found != tt.wantFound {
				t.Errorf("Parse(%q) Found = %v, want %v", tt.input, result.Found, tt.wantFound)
			}

			if tt.wantFound && result.Directive == nil {
				t.Fatalf("Parse(%q) expected directive but got nil", tt.input)
			}

			if result.Directive != nil {
				if tt.wantScope != "" && result.Directive.TargetScope != tt.wantScope {
					t.Errorf("Parse(%q) TargetScope = %q, want %q", tt.input, result.Directive.TargetScope, tt.wantScope)
				}

				if len(result.Directive.ModelReferences) != tt.wantModelCount {
					t.Errorf("Parse(%q) got %d model references, want %d", tt.input, len(result.Directive.ModelReferences), tt.wantModelCount)
				}

				if result.Directive.ClarificationNeeded != tt.wantClarification {
					t.Errorf("Parse(%q) ClarificationNeeded = %v, want %v", tt.input, result.Directive.ClarificationNeeded, tt.wantClarification)
				}
			}
		})
	}
}

func TestModelReassignmentParser_ResolveScope(t *testing.T) {
	parser := NewModelReassignmentParser()

	tests := []struct {
		name     string
		scope    string
		wantType IntentType
		wantOK   bool
	}{
		// Direct matches
		{"synthesis", "synthesis", IntentPlan, true},
		{"coding", "coding", IntentCode, true},
		{"research", "research", IntentResearch, true},
		{"debugging", "debugging", IntentDebug, true},
		{"planning", "planning", IntentPlan, true},
		{"analysis", "analysis", IntentAnalyze, true},

		// Case insensitive
		{"SYNTHESIS", "SYNTHESIS", IntentPlan, true},
		{"Coding", "Coding", IntentCode, true},

		// No match
		{"unknown", "unknown", "", false},
		{"chat", "chat", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotOK := parser.ResolveScope(tt.scope)
			if gotOK != tt.wantOK {
				t.Errorf("ResolveScope(%q) OK = %v, want %v", tt.scope, gotOK, tt.wantOK)
			}
			if gotOK && gotType != tt.wantType {
				t.Errorf("ResolveScope(%q) type = %v, want %v", tt.scope, gotType, tt.wantType)
			}
		})
	}
}

func TestModelReassignmentParser_modelAliases(t *testing.T) {
	parser := NewModelReassignmentParser()

	tests := []struct {
		name  string
		alias string
		want  string
	}{
		{"opus", "opus", "anthropic/claude-3-opus"},
		{"glm", "glm", "zai/glm-4.7"},
		{"glm-4.7", "glm-4.7", "zai/glm-4.7"},
		{"qwen", "qwen", "ollama/qwen2.5-coder"},
		{"llama", "llama", "ollama/llama3.2"},
		{"sonnet", "sonnet", "anthropic/claude-3-sonnet"},
		{"gpt-4o", "gpt-4o", "openai/gpt-4o"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parser.modelAliases[tt.alias]
			if !ok {
				t.Errorf("modelAliases[%q] not found", tt.alias)
				return
			}
			if got != tt.want {
				t.Errorf("modelAliases[%q] = %q, want %q", tt.alias, got, tt.want)
			}
		})
	}
}

func TestModelReassignmentParser_providerNames(t *testing.T) {
	parser := NewModelReassignmentParser()

	tests := []struct {
		name     string
		term     string
		provider string
	}{
		{"glm", "glm", "zai"},
		{"claude", "claude", "anthropic"},
		{"llama", "llama", "ollama"},
		{"gpt", "gpt", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parser.providerNames[tt.term]
			if !ok {
				t.Errorf("providerNames[%q] not found", tt.term)
				return
			}
			if got != tt.provider {
				t.Errorf("providerNames[%q] = %q, want %q", tt.term, got, tt.provider)
			}
		})
	}
}

func TestModelReassignmentParser_parseModelReferences(t *testing.T) {
	parser := NewModelReassignmentParser()

	tests := []struct {
		name     string
		input    string
		wantRefs []string
	}{
		{"single model", "glm-4.7", []string{"zai/glm-4.7"}},
		{"single alias", "opus", []string{"anthropic/claude-3-opus"}},
		{"provider only", "GLM", []string{"provider:zai"}},
		{"with 'and'", "glm and qwen", []string{"provider:zai", "provider:ollama"}},
		{"with 'or'", "opus or sonnet", []string{"anthropic/claude-3-opus", "anthropic/claude-3-sonnet"}},
		{"local models", "local models", []string{"provider:local"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRefs := parser.parseModelReferences(tt.input)
			if len(gotRefs) != len(tt.wantRefs) {
				t.Errorf("parseModelReferences(%q) got %d refs, want %d", tt.input, len(gotRefs), len(tt.wantRefs))
				return
			}
			for i, got := range gotRefs {
				if i < len(tt.wantRefs) && got != tt.wantRefs[i] {
					t.Errorf("parseModelReferences(%q)[%d] = %q, want %q", tt.input, i, got, tt.wantRefs[i])
				}
			}
		})
	}
}

func TestModelReassignmentParser_isAmbiguousReference(t *testing.T) {
	parser := NewModelReassignmentParser()

	tests := []struct {
		name          string
		ref           string
		wantAmbiguous bool
	}{
		{"provider reference", "provider:zai", true},
		{"local (broad)", "local", true},
		{"glm (broad)", "glm", true},
		{"specific model", "glm-4.7", false},
		{"specific alias", "opus", false},
		{"qwen-coder", "qwen-coder", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.isAmbiguousReference(tt.ref)
			if got != tt.wantAmbiguous {
				t.Errorf("isAmbiguousReference(%q) = %v, want %v", tt.ref, got, tt.wantAmbiguous)
			}
		})
	}
}

func TestModelReassignmentParser_scopeKeywords(t *testing.T) {
	parser := NewModelReassignmentParser()

	// Verify key scope mappings exist
	expectedScopes := map[string]IntentType{
		"synthesis": IntentPlan,
		"coding":    IntentCode,
		"research":  IntentResearch,
		"debugging": IntentDebug,
		"planning":  IntentPlan,
		"analysis":  IntentAnalyze,
	}

	for scope, wantType := range expectedScopes {
		t.Run(scope, func(t *testing.T) {
			gotType, ok := parser.scopeKeywords[scope]
			if !ok {
				t.Errorf("scopeKeywords[%q] not found", scope)
				return
			}
			if gotType != wantType {
				t.Errorf("scopeKeywords[%q] = %v, want %v", scope, gotType, wantType)
			}
		})
	}
}
