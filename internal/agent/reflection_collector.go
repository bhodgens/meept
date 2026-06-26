package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
)

// classifierClient is the minimal LLM interface ReflectionCollector needs.
// *llm.Client satisfies this via its Chat method.
type classifierClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error)
}

// ReflectionCollector runs immediate self-reflection after each agent turn,
// extracting 0-1 operational lessons per turn and queuing them as proposals.
type ReflectionCollector struct {
	cfg              config.ReflectionCollectorConfig
	classifier       classifierClient
	classifierModel  string // model override; empty = use classifier default
	templateReg      *plannerTemplateLoader
	queue            *proposalQueue
	logger           *slog.Logger

	// classifierRunOnce, if non-nil, overrides the classifier call for tests.
	// The test substitutes a function returning a canned LLM output string.
	classifierRunOnce func(ctx context.Context, prompt, convID string) (string, error)
}

// NewReflectionCollector constructs a collector wired to the given classifier,
// template loader, and proposal queue path. logger MUST be non-nil.
func NewReflectionCollector(
	cfg config.ReflectionCollectorConfig,
	classifier classifierClient,
	classifierModel string,
	templateReg *plannerTemplateLoader,
	queuePath string,
	logger *slog.Logger,
) *ReflectionCollector {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReflectionCollector{
		cfg:              cfg,
		classifier:       classifier,
		classifierModel:  classifierModel,
		templateReg:      templateReg,
		queue:            newProposalQueue(queuePath),
		logger:           logger.With("component", "reflection"),
	}
}

// ReflectTurn runs per-turn reflection. Builds a prompt from the trajectory,
// calls the classifier, validates confidence, and queues proposals.
func (rc *ReflectionCollector) ReflectTurn(ctx context.Context, traj ReflectionTrajectory) error {
	if !rc.cfg.Enabled {
		return nil
	}
	if rc.templateReg == nil {
		return fmt.Errorf("reflection collector: templateReg not wired")
	}
	trajJSON, err := traj.JSON()
	if err != nil {
		return fmt.Errorf("trajectory JSON: %w", err)
	}
	prompt, err := rc.templateReg.render("reflection/turn.md", map[string]any{
		"AgentID":        traj.AgentID,
		"UserInput":      traj.UserInput,
		"Outcome":        traj.Outcome,
		"TrajectoryJSON": string(trajJSON),
	})
	if err != nil {
		return fmt.Errorf("render turn.md: %w", err)
	}
	convID := fmt.Sprintf("reflect-%s-%s", traj.SessionID, id.Generate(""))

	var output string
	if rc.classifierRunOnce != nil {
		output, err = rc.classifierRunOnce(ctx, prompt, convID)
	} else if rc.classifier != nil {
		// Call classifier via the Chat interface.
		opts := []llm.ChatOption{
			llm.WithMaxTokens(500),
			llm.WithTemperature(0.2),
		}
		// classifierModel override is documented but not wired here because
		// llm.WithModel does not exist in the current llm package. Model
		// selection happens via the Client's configured ModelID at construction
		// time; a future ChatOption addition could plumb this through.
		messages := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: "You are a self-reflection assistant. Output ONLY valid JSON."},
			{Role: llm.RoleUser, Content: prompt},
		}
		resp, callErr := rc.classifier.Chat(ctx, messages, opts...)
		if callErr != nil {
			return fmt.Errorf("classifier call failed: %w", callErr)
		}
		if resp == nil {
			return fmt.Errorf("classifier returned nil response")
		}
		output = resp.Content
	} else {
		return fmt.Errorf("reflection collector: no classifier configured")
	}

	// Parse response
	jsonStr := ExtractJSON(output)
	if jsonStr == "" {
		return nil // nothing to do — no JSON in output
	}
	var resp struct {
		Proposal *struct {
			Type          string  `json:"type"`
			Target        string  `json:"target"`
			Change        string  `json:"change"`
			Justification string  `json:"justification"`
			Confidence    float64 `json:"confidence"`
		} `json:"proposal"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		rc.logger.Warn("Failed to parse reflection output", "error", err, "output_prefix", truncStr(output, 200))
		return nil
	}
	if resp.Proposal == nil {
		return nil
	}
	if resp.Proposal.Confidence < rc.cfg.TurnConfidenceMin {
		rc.logger.Debug("Dropping low-confidence proposal",
			"confidence", resp.Proposal.Confidence,
			"threshold", rc.cfg.TurnConfidenceMin,
			"target", resp.Proposal.Target,
		)
		return nil
	}
	// Authorization: agent_prompt / project_instruction / prompt_component
	// types always land in improvements.md (propose-only) regardless of
	// auto_apply_all setting. Skill creates under auto_skill_under may be
	// auto-applied when auto_apply_all=true. For MVP, always queue —
	// the CLI/HTTP layer enforces authorization at apply time.
	proposal := ReflectionProposal{
		Type:          resp.Proposal.Type,
		Target:        resp.Proposal.Target,
		Change:        resp.Proposal.Change,
		Justification: resp.Proposal.Justification,
		Confidence:    resp.Proposal.Confidence,
		Source:        fmt.Sprintf("turn:%s", traj.SessionID),
	}
	if err := rc.queue.Append(proposal); err != nil {
		return fmt.Errorf("append proposal: %w", err)
	}
	rc.logger.Info("Reflection proposal queued",
		"type", proposal.Type,
		"target", proposal.Target,
		"confidence", proposal.Confidence,
	)
	return nil
}

// ReflectInactiveSessions is the periodic-timer entry point. For MVP it is
// a stub: deeper session-level reflection requires SessionStore integration
// (finding sessions inactive >= InactivityMinutes and gathering their
// trajectories). Logged at debug level so operators can see the timer firing.
func (rc *ReflectionCollector) ReflectInactiveSessions(ctx context.Context) {
	if !rc.cfg.Enabled {
		return
	}
	rc.logger.Debug("ReflectInactiveSessions scheduled (MVP stub: needs SessionStore integration)")
}

// IsAlwaysProposeOnly is an exported wrapper around isAlwaysProposeOnly for
// external callers (CLI, HTTP handlers).
func IsAlwaysProposeOnly(target string) bool {
	return isAlwaysProposeOnly(target)
}

// Ensure strings import is used (truncStr helper is in handoff.go; this
// placeholder keeps the import if future refactors move helpers). The
// reference is intentional, not dead code.
var _ = strings.TrimSpace
