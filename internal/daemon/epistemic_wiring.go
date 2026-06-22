package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
)

// classifierAdapter wraps llm.Chatter to satisfy memory.ClassifierLLM
// without creating an import cycle between memory and llm.
type classifierAdapter struct {
	chatter llm.Chatter
	logger  *slog.Logger
}

// newClassifierAdapter constructs a ClassifierLLM adapter. Returns nil if
// chatter is nil so callers can pass a zero LLMClient safely.
func newClassifierAdapter(chatter llm.Chatter) *classifierAdapter {
	if chatter == nil {
		return nil
	}
	return &classifierAdapter{
		chatter: chatter,
		logger:  slog.Default().With("component", "epistemic-classifier"),
	}
}

// ClassifyRelationships calls the underlying LLM with the detector's
// classification prompt and parses the JSON verdict array.
func (a *classifierAdapter) ClassifyRelationships(ctx context.Context, newMem memory.Memory, candidates []memory.Memory) ([]memory.EdgeVerdict, error) {
	prompt := buildClassificationPrompt(newMem, candidates)
	resp, err := a.chatter.Chat(ctx, prompt, llm.WithTemperature(0.1))
	if err != nil {
		return nil, fmt.Errorf("classifier chat: %w", err)
	}
	if resp == nil || resp.Content == "" {
		return nil, fmt.Errorf("classifier returned empty response")
	}
	verdicts, err := memory.ParseClassifierJSON([]byte(resp.Content))
	if err != nil {
		return nil, fmt.Errorf("parse classifier json: %w", err)
	}
	return verdicts, nil
}

// Compile-time assertion that classifierAdapter satisfies the interface.
var _ memory.ClassifierLLM = (*classifierAdapter)(nil)

// buildClassificationPrompt constructs the chat prompt for the classifier LLM.
func buildClassificationPrompt(newMem memory.Memory, candidates []memory.Memory) []llm.ChatMessage {
	system := `You are an epistemic relationship classifier. Read the new memory and each candidate, then decide if a meaningful relationship exists.

Valid relationships:
- contradicts: the new memory asserts the opposite of the candidate
- superseded: the new memory replaces the candidate
- evidence_for: the new memory supports the candidate
- evidence_against: the new memory undermines the candidate
- derives_from: the new memory is derived from the candidate
- supports: the new memory reinforces the candidate (weaker than evidence_for)
- unrelated: no meaningful relationship

Return a JSON array of objects with keys: relation, target_id, confidence (0.0-1.0), explanation.
If no relationships, return [].`

	var candStr string
	for _, c := range candidates {
		candStr += fmt.Sprintf("- id=%s type=%s content=%q\n", c.ID, c.Type, c.Content)
	}
	user := fmt.Sprintf("New memory: id=%s type=%s content=%q\n\nCandidates:\n%s", newMem.ID, newMem.Type, newMem.Content, candStr)

	return []llm.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
}

// ambientClassifierAdapter wraps llm.Chatter to satisfy
// memory.AmbientClassifierLLM. It calls Chat with the extractor's prompt
// and returns the raw response body.
type ambientClassifierAdapter struct {
	chatter llm.Chatter
	logger  *slog.Logger
}

// ensure ambientClassifierAdapter satisfies memory.AmbientClassifierLLM.
var _ memory.AmbientClassifierLLM = (*ambientClassifierAdapter)(nil)

// newAmbientClassifierAdapter constructs an AmbientClassifierLLM adapter.
// Returns nil if chatter is nil.
func newAmbientClassifierAdapter(chatter llm.Chatter) *ambientClassifierAdapter {
	if chatter == nil {
		return nil
	}
	return &ambientClassifierAdapter{
		chatter: chatter,
		logger:  slog.Default().With("component", "ambient-extractor"),
	}
}

// ExtractCandidates calls the LLM with the given extraction prompt and
// returns the raw response body bytes.
func (a *ambientClassifierAdapter) ExtractCandidates(ctx context.Context, prompt string) ([]byte, error) {
	resp, err := a.chatter.Chat(ctx, []llm.ChatMessage{
		{Role: "user", Content: prompt},
	}, llm.WithTemperature(0.2))
	if err != nil {
		return nil, fmt.Errorf("ambient classifier chat: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("ambient classifier returned nil response")
	}
	return []byte(resp.Content), nil
}

// wireEpistemicDetector constructs and wires an EpistemicDetector on the
// memory manager. No-op when graph, manager, or chatter is nil.
func wireEpistemicDetector(memoryMgr *memory.Manager, chatter llm.Chatter, memCfg config.MemoryConfig, logger *slog.Logger) {
	if memoryMgr == nil || chatter == nil {
		return
	}
	graph := memoryMgr.Graph()
	if graph == nil {
		logger.Debug("epistemic detector skipped: no knowledge graph")
		return
	}
	detector := memory.NewEpistemicDetector(memory.EpistemicDetectorConfig{
		Graph:      graph,
		Manager:    memoryMgr,
		Classifier: newClassifierAdapter(chatter),
		Threshold:  memCfg.Epistemic.DetectionThreshold,
		AutoWeight: memory.EffectiveAutoTrustWeight(memCfg.Epistemic.AutoTrustWeight),
		Logger:     logger.With("component", "epistemic-detector"),
	})
	memoryMgr.SetEpistemicDetector(detector)
	logger.Info("epistemic detector wired")
}

// wireEpistemicHook constructs the AmbientExtractor + EpistemicHook and
// attaches the hook to the agent loop. No-op when ambient extraction is
// disabled, manager is nil, or chatter is nil.
func wireEpistemicHook(agentLoop *agent.AgentLoop, memoryMgr *memory.Manager, chatter llm.Chatter, memCfg config.MemoryConfig, logger *slog.Logger) {
	if agentLoop == nil || memoryMgr == nil || chatter == nil {
		return
	}
	if !memCfg.Epistemic.AmbientExtraction.Enabled {
		return
	}
	extractor := memory.NewAmbientExtractor(memory.AmbientExtractorConfig{
		Manager:    memoryMgr,
		Classifier: newAmbientClassifierAdapter(chatter),
		Logger:     logger.With("component", "ambient-extractor"),
	})
	hook := agent.NewEpistemicHook(agent.EpistemicHookConfig{
		Cfg:       memCfg.Epistemic,
		Extractor: extractor,
		Logger:    logger.With("component", "epistemic-hook"),
	})
	agentLoop.SetEpistemicHook(hook)
	logger.Info("epistemic hook wired",
		"ambient_extraction", true,
		"max_per_turn", memCfg.Epistemic.AmbientExtraction.MaxPerTurn,
	)
}

// Ensure unused imports are referenced.
var _ = json.Marshal
