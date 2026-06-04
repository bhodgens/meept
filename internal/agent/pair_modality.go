package agent

import (
	"encoding/json"
	"strings"
)

// PairModality defines the type of agentic pairing applied to a task step.
// The orchestrator selects a modality based on task complexity, step tool hint,
// and user preferences. This enum is shared across all agentic pair options.
type PairModality int

//go:generate go run golang.org/x/tools/cmd/stringer -type=PairModality -linecomment

const (
	// PairModalityNone means no agentic pairing; single-agent execution.
	PairModalityNone PairModality = iota
	// PairModalitySpecReview is Option A: specification-driven review loop
	// where acceptance criteria are generated during planning and the reviewer
	// checks against them.
	PairModalitySpecReview
	// PairModalityPairSession is Option B: shared-context pair session where
	// two agents iterate on a full task with accumulated review history.
	PairModalityPairSession
	// PairModalityDebate is Option C: bus-channel-based dual-agent conversation
	// where agents take turns via a shared topic.
	PairModalityDebate
	// PairModalityInline is Option D: tool-based inline review where the actor
	// calls request_review within its own execution loop.
	PairModalityInline
)

var pairModalityLookup = map[string]PairModality{
	"none":         PairModalityNone,
	"spec_review":  PairModalitySpecReview,
	"pair_session": PairModalityPairSession,
	"debate":       PairModalityDebate,
	"inline":       PairModalityInline,
}

// IsActive returns true if the modality represents an active pairing (not none).
func (m PairModality) IsActive() bool {
	return m != PairModalityNone
}

// MarshalJSON implements json.Marshaler for PairModality.
func (m PairModality) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON implements json.Unmarshaler for PairModality.
func (m *PairModality) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*m = ParsePairModality(s)
	return nil
}

// ParsePairModality parses a string into a PairModality, returning
// PairModalityNone for unrecognized values.
func ParsePairModality(s string) PairModality {
	if m, ok := pairModalityLookup[strings.ToLower(s)]; ok {
		return m
	}
	return PairModalityNone
}
