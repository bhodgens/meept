package services

import (
	"strings"

	"github.com/caimlas/meept/internal/agent"
)

// ReflectionService wraps the external proposal queue for HTTP/RPC access.
// It provides read access to pending proposals and write access to mark
// proposals as applied or skipped, plus a /remember endpoint for manual
// proposal submission.
type ReflectionService struct {
	queue *agent.ProposalQueueExternal
}

// NewReflectionService creates a reflection service backed by the given queue
// path. If queuePath is empty, defaults to ".meept/improvements.md".
func NewReflectionService(queuePath string) *ReflectionService {
	if queuePath == "" {
		queuePath = ".meept/improvements.md"
	}
	return &ReflectionService{queue: agent.NewExternalProposalQueue(queuePath)}
}

// ListPending returns all pending proposals.
func (s *ReflectionService) ListPending() ([]agent.ReflectionProposal, error) {
	return s.queue.ListPending()
}

// Apply marks the proposal with the given ID as applied.
func (s *ReflectionService) Apply(id string) error {
	return s.queue.MarkApplied(id)
}

// Skip marks the proposal with the given ID as skipped.
func (s *ReflectionService) Skip(id string) error {
	return s.queue.MarkSkipped(id)
}

// Remember creates a manual proposal with the given fields and appends it
// to the queue. The proposal type is inferred from the target path.
func (s *ReflectionService) Remember(target, change, justification string) error {
	proposal := agent.ReflectionProposal{
		Type:          inferProposalType(target),
		Target:        target,
		Change:        change,
		Justification: justification,
		Confidence:    0.9,
		Source:        "manual:http",
	}
	return s.queue.Append(proposal)
}

// inferProposalType maps a target path to a proposal type.
func inferProposalType(target string) string {
	switch {
	case strings.Contains(target, "SKILL.md") || strings.Contains(target, ".meept/skills/"):
		return "skill_create"
	case strings.HasSuffix(target, "AGENT.md"):
		return "agent_prompt"
	case target == "CLAUDE.md":
		return "project_instruction"
	default:
		return "prompt_component"
	}
}
