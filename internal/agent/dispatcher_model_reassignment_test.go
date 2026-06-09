package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDispatcher_ModelReassignmentParserInitialization(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})

	assert.NotNil(t, d.modelParser, "modelParser should be initialized in NewDispatcher")
}

func TestDispatcher_BuildClarificationQuestion(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})

	tests := []struct {
		name      string
		directive *ModelReassignmentDirective
		wantEmpty bool
	}{
		{
			name: "no models parsed",
			directive: &ModelReassignmentDirective{
				Instruction:          "use something",
				ModelReferences:      nil,
				TargetScope:          "coding",
				ClarificationNeeded:  true,
				ClarificationQuestions: []string{"Which model?"},
			},
			wantEmpty: false,
		},
		{
			name: "no scope parsed",
			directive: &ModelReassignmentDirective{
				Instruction:     "use GLM models",
				ModelReferences: []string{"provider:zai"},
				TargetScope:      "",
				ClarificationNeeded: true,
			},
			wantEmpty: false,
		},
		{
			name: "provider reference needs clarification",
			directive: &ModelReassignmentDirective{
				Instruction:     "use GLM for coding",
				ModelReferences: []string{"provider:zai"},
				TargetScope:      "coding",
				ClarificationNeeded: true,
			},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			question := d.buildClarificationQuestion(tt.directive)
			if tt.wantEmpty {
				assert.Empty(t, question)
			} else {
				assert.NotEmpty(t, question, "clarification question should not be empty")
			}
		})
	}
}

func TestDispatcher_ModelDirectiveInDispatchResult(t *testing.T) {
	// Verify that DispatchResult struct has the ModelDirective field
	result := &DispatchResult{
		ModelDirective:       &ModelReassignmentDirective{Instruction: "use GLM for coding"},
		ClarificationReply:   "Which GLM model?",
		ClarificationNeeded:  true,
	}

	assert.NotNil(t, result.ModelDirective)
	assert.True(t, result.ClarificationNeeded)
	assert.Equal(t, "use GLM for coding", result.ModelDirective.Instruction)
}

func TestDispatcher_ModelReassignmentParserInDispatcher(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})

	// Verify parser can parse common patterns
	result := d.modelParser.Parse("use glm-4.7 for synthesis")
	assert.True(t, result.Found, "should detect model reassignment pattern")
	assert.NotNil(t, result.Directive)
	assert.Equal(t, "synthesis", result.Directive.TargetScope)
	assert.Contains(t, result.Directive.ModelReferences, "zai/glm-4.7")

	// Verify no-match doesn't crash
	noMatch := d.modelParser.Parse("hello how are you")
	assert.False(t, noMatch.Found)
}

func TestDispatcher_ScopeMatchingToIntent(t *testing.T) {
	d := NewDispatcher(DispatcherConfig{})
	parser := d.modelParser

	// Verify scope resolution to intent types
	intentType, ok := parser.ResolveScope("coding")
	assert.True(t, ok)
	assert.Equal(t, IntentCode, intentType)

	intentType, ok = parser.ResolveScope("synthesis")
	assert.True(t, ok)
	assert.Equal(t, IntentPlan, intentType)

	intentType, ok = parser.ResolveScope("research")
	assert.True(t, ok)
	assert.Equal(t, IntentResearch, intentType)

	intentType, ok = parser.ResolveScope("debugging")
	assert.True(t, ok)
	assert.Equal(t, IntentDebug, intentType)

	intentType, ok = parser.ResolveScope("analysis")
	assert.True(t, ok)
	assert.Equal(t, IntentAnalyze, intentType)
}
