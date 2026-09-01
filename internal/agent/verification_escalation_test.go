package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubResolver implements ModelResolver for DecideEscalation tests.
type stubResolver struct {
	resolved string
	err      error
	lastRef  string
}

func (s *stubResolver) ResolveEscalationRef(ref string) (string, error) {
	s.lastRef = ref
	return s.resolved, s.err
}

func TestDecideEscalation(t *testing.T) {
	tests := []struct {
		name         string
		spec         *AgentSpec
		resolver     ModelResolver
		wantEscalate bool
		wantRef      string
		wantReason   string
	}{
		{
			name:         "nil spec",
			spec:         nil,
			resolver:     &stubResolver{resolved: "x"},
			wantEscalate: false,
			wantRef:      "",
			wantReason:   "no_escalation_model",
		},
		{
			name:         "empty escalation model",
			spec:         &AgentSpec{EscalationModel: ""},
			resolver:     &stubResolver{resolved: "x"},
			wantEscalate: false,
			wantRef:      "",
			wantReason:   "no_escalation_model",
		},
		{
			name:         "resolving resolver",
			spec:         &AgentSpec{EscalationModel: "strong/model"},
			resolver:     &stubResolver{resolved: "openai/gpt-5"},
			wantEscalate: true,
			wantRef:      "openai/gpt-5",
			wantReason:   "fix_loops_exhausted",
		},
		{
			name:         "erroring resolver",
			spec:         &AgentSpec{EscalationModel: "strong/model"},
			resolver:     &stubResolver{err: errors.New("unknown alias")},
			wantEscalate: false,
			wantRef:      "",
			wantReason:   "resolution_failed",
		},
		{
			// nil resolver with a non-empty ref must degrade to
			// resolution_failed, not panic.
			name:         "nil resolver",
			spec:         &AgentSpec{EscalationModel: "strong/model"},
			resolver:     nil,
			wantEscalate: false,
			wantRef:      "",
			wantReason:   "resolution_failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideEscalation(tt.spec, tt.resolver)
			assert.Equal(t, tt.wantEscalate, got.Escalate)
			assert.Equal(t, tt.wantRef, got.ModelRef)
			assert.Equal(t, tt.wantReason, got.Reason)
		})
	}

	// The passed-through ref must reach the resolver untouched.
	t.Run("ref forwarded to resolver", func(t *testing.T) {
		res := &stubResolver{resolved: "r"}
		DecideEscalation(&AgentSpec{EscalationModel: "alias-1"}, res)
		assert.Equal(t, "alias-1", res.lastRef)
	})
}
