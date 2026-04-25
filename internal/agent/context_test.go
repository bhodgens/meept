package agent

import "testing"

func TestApplyContextWeighting(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		name         string
		intent       *Intent
		memCtx       *MemoryContext
		input        string
		wantMinBoost float64
		wantMaxFinal float64
	}{
		{
			name:   "nil context",
			intent: &Intent{Type: "code", Confidence: 0.7, AgentType: "coder"},
			memCtx: nil,
			input:  "test",
		},
		{
			name:   "same intent boost",
			intent: &Intent{Type: "code", Confidence: 0.7, AgentType: "coder"},
			memCtx: &MemoryContext{
				LastIntent:   &Intent{Type: "code"},
				LastAgent:    "coder",
				IntentCounts: map[string]int{"code": 3},
			},
			input:        "write more code",
			wantMinBoost: 0.15,
		},
		{
			name:   "same agent boost",
			intent: &Intent{Type: "code", Confidence: 0.7, AgentType: "coder"},
			memCtx: &MemoryContext{
				LastIntent:   &Intent{Type: "debug"},
				LastAgent:    "coder",
				IntentCounts: map[string]int{},
			},
			input:        "write code",
			wantMinBoost: 0.1,
		},
		{
			name:   "frequency boost",
			intent: &Intent{Type: "code", Confidence: 0.7, AgentType: "coder"},
			memCtx: &MemoryContext{
				LastIntent:   &Intent{Type: "chat"},
				LastAgent:    "chat",
				IntentCounts: map[string]int{"code": 3},
			},
			input:        "write code",
			wantMinBoost: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := d.applyContextWeighting(tt.intent, tt.memCtx, tt.input)
			if tt.memCtx == nil {
				if result.Confidence != tt.intent.Confidence {
					t.Errorf("nil context changed confidence from %v to %v", tt.intent.Confidence, result.Confidence)
				}
			}
			if result.Confidence > 1.0 {
				t.Errorf("confidence %v > 1.0", result.Confidence)
			}
		})
	}
}

func TestApplyContextWeightingCap(t *testing.T) {
	d := &Dispatcher{}
	intent := &Intent{Type: "code", Confidence: 0.9, AgentType: "coder"}
	memCtx := &MemoryContext{
		LastIntent:   &Intent{Type: "code"},
		LastAgent:    "coder",
		IntentCounts: map[string]int{"code": 10},
	}

	result := d.applyContextWeighting(intent, memCtx, "do the same for main.go")
	if result.Confidence > 1.0 {
		t.Errorf("confidence %v exceeds 1.0", result.Confidence)
	}
	if result.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", result.Confidence)
	}
}

func TestHasAnaphora(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"do the same thing", true},
		{"also fix this", true},
		{"continue with that", true},
		{"keep going", true},
		{"this is a test", true},
		{"write a function", false},
		{"implement feature", false},
		{"hello world", false},
	}

	for _, tt := range tests {
		got := hasAnaphora(tt.input)
		if got != tt.want {
			t.Errorf("hasAnaphora(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestResolveAnaphora(t *testing.T) {
	d := &Dispatcher{}

	tests := []struct {
		name   string
		input  string
		memCtx *MemoryContext
		want   string
	}{
		{
			name:   "nil context passthrough",
			input:  "do the same for main.go",
			memCtx: nil,
			want:   "do the same for main.go",
		},
		{
			name:   "no last intent passthrough",
			input:  "do the same for main.go",
			memCtx: &MemoryContext{},
			want:   "do the same for main.go",
		},
		{
			name:  "do the same for X",
			input: "do the same for main.go",
			memCtx: &MemoryContext{
				LastIntent: &Intent{Summary: "write a function"},
			},
			want: "write a function for main.go",
		},
		{
			name:   "no match passthrough",
			input:  "write code",
			memCtx: &MemoryContext{LastIntent: &Intent{Summary: "test"}},
			want:   "write code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.resolveAnaphora(tt.input, tt.memCtx)
			if got != tt.want {
				t.Errorf("resolveAnaphora() = %q, want %q", got, tt.want)
			}
		})
	}
}
