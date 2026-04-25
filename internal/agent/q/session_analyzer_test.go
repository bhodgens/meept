package q

import (
	"context"
	"testing"
	"time"
)

func TestComputeDifficultyScore(t *testing.T) {
	tests := []struct {
		name           string
		duration       time.Duration
		iterations     int
		revisions      int
		errors         int
		agentSwitches  int
		wantMin        float64
		wantMax        float64
	}{
		{
			name:           "easy session - short and simple",
			duration:       5 * time.Minute,
			iterations:     3,
			revisions:      0,
			errors:         0,
			agentSwitches:  0,
			wantMin:        0.0,
			wantMax:        0.0,
		},
		{
			name:           "moderate - several iterations",
			duration:       10 * time.Minute,
			iterations:     12,
			revisions:      1,
			errors:         0,
			agentSwitches:  0,
			wantMin:        0.15,
			wantMax:        0.15,
		},
		{
			name:           "hard - long duration and many iterations",
			duration:       35 * time.Minute,
			iterations:     25,
			revisions:      6,
			errors:         4,
			agentSwitches:  5,
			wantMin:        1.0,
			wantMax:        1.0,
		},
		{
			name:           "medium - duration and agent switches",
			duration:       15 * time.Minute,
			iterations:     5,
			revisions:      0,
			errors:         0,
			agentSwitches:  2,
			wantMin:        0.2,
			wantMax:        0.2,
		},
		{
			name:           "high revisions only",
			duration:       5 * time.Minute,
			iterations:     2,
			revisions:      10,
			errors:         0,
			agentSwitches:  0,
			wantMin:        0.25,
			wantMax:        0.25,
		},
		{
			name:           "many errors only",
			duration:       5 * time.Minute,
			iterations:     2,
			revisions:      0,
			errors:         5,
			agentSwitches:  0,
			wantMin:        0.25,
			wantMax:        0.25,
		},
		{
			name:           "all thresholds maxed out",
			duration:       1 * time.Hour,
			iterations:     30,
			revisions:      10,
			errors:         10,
			agentSwitches:  10,
			wantMin:        1.0,
			wantMax:        1.0,
		},
		{
			name:           "edge case - exactly at boundary (10 min)",
			duration:       10 * time.Minute,
			iterations:     10,
			revisions:      2,
			errors:         1,
			agentSwitches:  1,
			wantMin:        0.0,
			wantMax:        0.0,
		},
		{
			name:           "edge case - just over boundary (10m1s)",
			duration:       10*time.Minute + 1,
			iterations:     11,
			revisions:      3,
			errors:         2,
			agentSwitches:  2,
			wantMin:        0.4,
			wantMax:        0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &SessionData{
				Metrics: SessionMetrics{
					Duration:      tt.duration,
					Iterations:    tt.iterations,
					Revisions:     tt.revisions,
					Errors:        tt.errors,
					AgentSwitches: tt.agentSwitches,
				},
			}

			analyzer := &SessionAnalyzer{}
			score := analyzer.computeDifficultyScore(data, nil)

			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("computeDifficultyScore() = %.2f, wanted in range [%.2f, %.2f]", score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestDetectAnomalies(t *testing.T) {
	tests := []struct {
		name           string
		duration       time.Duration
		iterationCount int
		agentSwitches  int
		revisionCycles int
		wantFlags      []string
	}{
		{
			name:           "no anomalies - simple session",
			duration:       5 * time.Minute,
			iterationCount: 3,
			agentSwitches:  0,
			revisionCycles: 0,
			wantFlags:      []string{},
		},
		{
			name:           "long duration anomaly",
			duration:       65 * time.Minute,
			iterationCount: 3,
			agentSwitches:  0,
			revisionCycles: 0,
			wantFlags:      []string{"long_duration"},
		},
		{
			name:           "high iterations anomaly",
			duration:       5 * time.Minute,
			iterationCount: 30,
			agentSwitches:  0,
			revisionCycles: 0,
			wantFlags:      []string{"high_iterations"},
		},
		{
			name:           "agent thrashing anomaly",
			duration:       5 * time.Minute,
			iterationCount: 3,
			agentSwitches:  6,
			revisionCycles: 0,
			wantFlags:      []string{"agent_thrashing"},
		},
		{
			name:           "high revisions anomaly",
			duration:       5 * time.Minute,
			iterationCount: 3,
			agentSwitches:  0,
			revisionCycles: 7,
			wantFlags:      []string{"high_revisions"},
		},
		{
			name:           "multiple anomalies",
			duration:       90 * time.Minute,
			iterationCount: 50,
			agentSwitches:  8,
			revisionCycles: 10,
			wantFlags:      []string{"long_duration", "high_iterations", "agent_thrashing", "high_revisions"},
		},
		{
			name:           "at boundaries - exactly 60 min",
			duration:       60 * time.Minute,
			iterationCount: 25,
			agentSwitches:  4,
			revisionCycles: 5,
			wantFlags:      []string{},
		},
		{
			name:           "just over all boundaries",
			duration:       61 * time.Minute,
			iterationCount: 26,
			agentSwitches:  5,
			revisionCycles: 6,
			wantFlags:      []string{"long_duration", "high_iterations", "agent_thrashing", "high_revisions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &SessionAnalysis{
				Duration:       tt.duration,
				IterationCount: tt.iterationCount,
				AgentSwitches:  tt.agentSwitches,
				RevisionCycles: tt.revisionCycles,
			}

			analyzer := &SessionAnalyzer{}
			flags := analyzer.detectAnomalies(analysis)

			if len(flags) != len(tt.wantFlags) {
				t.Errorf("detectAnomalies() returned %d flags, wanted %d. got=%v, want=%v",
					len(flags), len(tt.wantFlags), flags, tt.wantFlags)
				return
			}

			flagSet := make(map[string]bool)
			for _, f := range flags {
				flagSet[f] = true
			}
			for _, w := range tt.wantFlags {
				if !flagSet[w] {
					t.Errorf("detectAnomalies() missing flag %q. got=%v", w, flags)
				}
			}
		})
	}
}

func TestExtractToolCalls(t *testing.T) {
	tests := []struct {
		name      string
		messages  []Message
		wantCount int
	}{
		{
			name:      "empty messages",
			messages:  nil,
			wantCount: 0,
		},
		{
			name: "no tool calls",
			messages: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
			wantCount: 0,
		},
		{
			name: "single tool call",
			messages: []Message{
				{
					Role: "assistant",
					Content: "Let me check that for you",
					ToolCalls: []ToolCall{
						{Name: "file_read", Arguments: "file.txt", Success: true},
					},
				},
			},
			wantCount: 1,
		},
		{
			name: "multiple tool calls across messages",
			messages: []Message{
				{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{Name: "file_read", Arguments: "a.go", Success: true},
						{Name: "shell_execute", Arguments: "go build", Success: false},
					},
				},
				{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{Name: "memory_store", Arguments: "key=value", Success: true},
					},
				},
			},
			wantCount: 3,
		},
		{
			name: "user messages with tool calls mixed in",
			messages: []Message{
				{Role: "user", Content: "do x"},
				{
					Role: "assistant",
					ToolCalls: []ToolCall{
						{Name: "shell_execute", Arguments: "ls", Success: true},
					},
				},
				{Role: "user", Content: "yes"},
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer := &SessionAnalyzer{}
			records := analyzer.extractToolCalls(tt.messages)

			if len(records) != tt.wantCount {
				t.Errorf("extractToolCalls() returned %d records, wanted %d", len(records), tt.wantCount)
			}

			for _, rec := range records {
				if rec.ToolName == "" {
					t.Error("extractToolCalls() returned record with empty ToolName")
				}
			}
		})
	}
}

func TestGetString(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]interface{}
		key    string
		want   string
	}{
		{
			name:  "existing string key",
			input: map[string]interface{}{"foo": "bar"},
			key:   "foo",
			want:  "bar",
		},
		{
			name:  "missing key",
			input: map[string]interface{}{"foo": "bar"},
			key:   "baz",
			want:  "",
		},
		{
			name:  "non-string value",
			input: map[string]interface{}{"foo": 42},
			key:   "foo",
			want:  "",
		},
		{
			name:  "empty map",
			input: map[string]interface{}{},
			key:   "foo",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getString(tt.input, tt.key)
			if got != tt.want {
				t.Errorf("getString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	tests := []struct {
		name   string
		input  map[string]interface{}
		key    string
		want   bool
	}{
		{
			name:  "existing true bool",
			input: map[string]interface{}{"enabled": true},
			key:   "enabled",
			want:  true,
		},
		{
			name:  "existing false bool",
			input: map[string]interface{}{"enabled": false},
			key:   "enabled",
			want:  false,
		},
		{
			name:  "missing key",
			input: map[string]interface{}{"foo": "bar"},
			key:   "enabled",
			want:  false,
		},
		{
			name:  "non-bool value",
			input: map[string]interface{}{"enabled": "yes"},
			key:   "enabled",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBool(tt.input, tt.key)
			if got != tt.want {
				t.Errorf("getBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

// computeAnalysis with an empty SessionData.
func TestComputeAnalysisEdgeCases(t *testing.T) {
	analyzer := &SessionAnalyzer{}
	data := &SessionData{
		Metrics: SessionMetrics{},
	}

	analysis := analyzer.computeAnalysis(data, nil)

	if analysis == nil {
		t.Fatal("computeAnalysis() returned nil for empty data")
	}

	if len(analysis.AnomalyFlags) != 0 {
		t.Errorf("expected no anomalies for empty session, got %v", analysis.AnomalyFlags)
	}

	if analysis.DifficultyScore != 0.0 {
		t.Errorf("expected difficulty score 0, got %.2f", analysis.DifficultyScore)
	}
}

// Test AnalyzeMultipleSessions with empty slice.
func TestSessionAnalyzerEmptySessions(t *testing.T) {
	analyzer := &SessionAnalyzer{}

	results, err := analyzer.AnalyzeMultipleSessions(context.Background(), []string{})
	if err != nil {
		t.Errorf("AnalyzeMultipleSessions(empty) error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("AnalyzeMultipleSessions(empty) returned %d results, wanted 0", len(results))
	}
}

// Test the helper functions with nil input.
func TestSessionAnalyzerHelpers(t *testing.T) {
	if getString(nil, "key") != "" {
		t.Error("getString(nil, _) should return empty string")
	}
	if getBool(nil, "key") != false {
		t.Error("getBool(nil, _) should return false")
	}
}
