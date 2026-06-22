package agent

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

// AmbientExtractorInterface is the subset of memory.AmbientExtractor used by
// the epistemic hook. Defined locally so tests can substitute a fake and so
// we don't import the concrete type by name in the public surface.
type AmbientExtractorInterface interface {
	Extract(ctx context.Context, messages []string) ([]memory.AmbientCandidate, error)
	WriteCandidates(ctx context.Context, candidates []memory.AmbientCandidate) ([]string, error)
}

// EpistemicHookConfig holds construction parameters for EpistemicHook.
type EpistemicHookConfig struct {
	Cfg       config.EpistemicConfig
	Extractor AmbientExtractorInterface
	Logger    *slog.Logger
}

// EpistemicHook is the post-turn ambient-extraction hook (Path B). It is
// gated entirely by EpistemicConfig.AmbientExtraction.Enabled and the
// ExcludeIntents filter.
type EpistemicHook struct {
	cfg       config.EpistemicConfig
	extractor AmbientExtractorInterface
	logger    *slog.Logger
}

// NewEpistemicHook constructs a hook from the given configuration.
func NewEpistemicHook(cfg EpistemicHookConfig) *EpistemicHook {
	h := &EpistemicHook{
		cfg:       cfg.Cfg,
		extractor: cfg.Extractor,
		logger:    cfg.Logger,
	}
	if h.logger == nil {
		h.logger = slog.Default()
	}
	return h
}

// AfterTurn is called by the agent loop after a turn completes. It extracts
// candidate claims from the conversation window, filters them, and writes
// them as auto claims. Returns the IDs of the written claims.
//
// The method is best-effort: errors are logged but do not propagate unless
// the underlying extractor fails.
func (h *EpistemicHook) AfterTurn(ctx context.Context, intent string, messages []string) ([]string, error) {
	if h == nil {
		return nil, nil
	}
	if !h.cfg.AmbientExtraction.Enabled {
		return nil, nil
	}
	if h.extractor == nil {
		return nil, nil
	}
	if intentExcluded(intent, h.cfg.AmbientExtraction.ExcludeIntents) {
		return nil, nil
	}

	window := messages
	if w := h.cfg.AmbientExtraction.ContextWindow; w > 0 && len(window) > w {
		window = window[len(window)-w:]
	}

	candidates, err := h.extractor.Extract(ctx, window)
	if err != nil {
		h.logger.Warn("epistemic hook extract failed", "error", err, "intent", intent)
		return nil, nil
	}
	candidates = filterAmbientCandidates(candidates, h.cfg.AmbientExtraction)
	if len(candidates) == 0 {
		return nil, nil
	}

	ids, err := h.extractor.WriteCandidates(ctx, candidates)
	if err != nil {
		h.logger.Warn("epistemic hook write candidates failed", "error", err, "intent", intent)
		return nil, nil
	}
	if len(ids) > 0 {
		h.logger.Info("epistemic hook wrote auto claims", "count", len(ids), "intent", intent)
	}
	return ids, nil
}

// intentExcluded reports whether the given intent is in the exclude list.
// The comparison is case-insensitive.
func intentExcluded(intent string, exclude []string) bool {
	if len(exclude) == 0 {
		return false
	}
	target := normalizeIntentName(intent)
	for _, e := range exclude {
		if normalizeIntentName(e) == target {
			return true
		}
	}
	return false
}

// normalizeIntentName lowercases and trims for comparison.
func normalizeIntentName(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, byte(r-'A'+'a'))
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_' || r == '-':
			out = append(out, byte(r))
		}
	}
	return string(out)
}

// filterAmbientCandidates applies the ConfidenceThreshold, ExcludeCategories,
// and MaxPerTurn gates from AmbientExtractionConfig.
func filterAmbientCandidates(in []memory.AmbientCandidate, cfg config.AmbientExtractionConfig) []memory.AmbientCandidate {
	if len(in) == 0 {
		return nil
	}
	max := cfg.MaxPerTurn
	if max <= 0 {
		max = 3 // spec default
	}
	threshold := cfg.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.7 // spec default
	}
	excludedCat := make(map[string]struct{}, len(cfg.ExcludeCategories))
	for _, c := range cfg.ExcludeCategories {
		excludedCat[c] = struct{}{}
	}

	out := make([]memory.AmbientCandidate, 0, len(in))
	for _, c := range in {
		if c.Confidence < threshold {
			continue
		}
		if _, skip := excludedCat[c.Category]; skip {
			continue
		}
		out = append(out, c)
		if len(out) >= max {
			break
		}
	}
	return out
}
