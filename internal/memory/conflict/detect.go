package conflict

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

// LLM is the single-method completion seam the detector needs — mirrors the
// AmbientClassifierLLM shape in internal/memory/epistemic_ambient.go. Callers
// adapt their LLM client; the conflict package stays dependency-free.
type LLM interface {
	// Complete sends the prompt and returns the raw response body.
	Complete(ctx context.Context, prompt string) ([]byte, error)
}

// DefaultMaxBatch is the default number of candidates per LLM call.
const DefaultMaxBatch = 20

// DefaultRecallPerCandidate is the default number of recalled targets shown
// per candidate in the prompt.
const DefaultRecallPerCandidate = 5

// DetectorConfig holds construction parameters for Detector.
type DetectorConfig struct {
	// LLM performs the batch conflict-detection completion. Required.
	LLM LLM
	// Store recalls potentially conflicting memories. May be nil — detection
	// then short-circuits to store for every candidate.
	Store RecallStore
	// Logger receives structured warnings on fail-open paths. May be nil.
	Logger *slog.Logger
	// MaxBatch caps candidates per LLM call (default 20).
	MaxBatch int
	// RecallPerCandidate caps recalled targets per candidate (default 5).
	RecallPerCandidate int
}

// Detector decides, per candidate memory, whether it conflicts with already
// stored memories. Every failure mode fails open to "store".
type Detector struct {
	llm                LLM
	store              RecallStore
	log                *slog.Logger
	maxBatch           int
	recallPerCandidate int
}

// NewDetector builds a Detector, applying defaults for unset config fields.
func NewDetector(cfg DetectorConfig) *Detector {
	maxBatch := cfg.MaxBatch
	if maxBatch <= 0 {
		maxBatch = DefaultMaxBatch
	}
	recall := cfg.RecallPerCandidate
	if recall <= 0 {
		recall = DefaultRecallPerCandidate
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Detector{
		llm:                cfg.LLM,
		store:              cfg.Store,
		log:                log,
		maxBatch:           maxBatch,
		recallPerCandidate: recall,
	}
}

// thinkBlockRE strips reasoning blocks some models emit before their answer.
var thinkBlockRE = regexp.MustCompile(`(?s)<think>.*?</think>`)

// fenceRE strips markdown code fences around the JSON payload.
var fenceRE = regexp.MustCompile("(?s)`{3,}[a-zA-Z0-9_-]*\\n?(.*?)`{3,}")

// sanitizeLLMOutput removes <think>...</think> blocks and markdown fences,
// then trims whitespace, leaving (hopefully) bare JSON.
func sanitizeLLMOutput(raw string) string {
	s := thinkBlockRE.ReplaceAllString(raw, "")
	s = fenceRE.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// llmDecision is the wire shape of one JSON entry in the LLM's array.
type llmDecision struct {
	Candidate     *int     `json:"candidate"`
	Action        Action   `json:"action"`
	TargetIDs     []string `json:"target_ids"`
	MergedContent string   `json:"merged_content"`
	Reason        string   `json:"reason"`
}

// DetectBatch decides an action for every candidate. Candidates are grouped
// into batches of at most MaxBatch; each batch gets one LLM call with its
// recalled targets.
//
// Fail-open semantics: empty recall (no candidate recalled anything) skips
// the LLM entirely, missing entries in a well-formed answer fill with
// "store", and ANY error — recall failure, LLM failure, unparseable output —
// makes every candidate in the affected batch "store" with a structured
// warning logged. DetectBatch itself never returns an error.
func (d *Detector) DetectBatch(ctx context.Context, candidates []Candidate) []Decision {
	decisions := make([]Decision, len(candidates))
	for i := range decisions {
		decisions[i] = Decision{CandidateIdx: i, Action: ActionStore}
	}
	if len(candidates) == 0 {
		return decisions
	}

	// Recall everything up front so a completely empty recall skips the LLM.
	recalls, err := recallBatch(ctx, d.store, candidates, d.recallPerCandidate)
	if err != nil {
		d.warn("conflict recall failed; storing all candidates", "error", err, "candidates", len(candidates))
		return decisions
	}
	if len(recalls) == 0 {
		// Nothing recalled at all — no conflict possible.
		return decisions
	}

	for offset := 0; offset < len(candidates); offset += d.maxBatch {
		end := min(offset+d.maxBatch, len(candidates))
		batch := candidates[offset:end]

		batchRecalls := make(map[int][]RecallTarget, len(batch))
		for localIdx := range batch {
			if targets, ok := recalls[offset+localIdx]; ok {
				batchRecalls[localIdx] = targets
			}
		}

		prompt := buildPrompt(batch, batchRecalls, offset)
		answers, llmErr := d.completeBatch(ctx, prompt, len(batch))
		for localIdx := range batch {
			decisions[offset+localIdx] = d.resolveDecision(answers, llmErr, offset, localIdx, len(batch))
		}
	}
	return decisions
}

// completeBatch runs one LLM call for a batch and validates the answer into
// per-local-index decisions. On any failure it returns (nil, err) — callers
// fail open.
func (d *Detector) completeBatch(ctx context.Context, prompt string, batchLen int) (map[int]Decision, error) {
	if d.llm == nil {
		return nil, fmt.Errorf("no LLM configured")
	}
	raw, err := d.llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("llm completion failed: %w", err)
	}
	decisions, parseErr := parseLLMDecisions(sanitizeLLMOutput(string(raw)), batchLen)
	if parseErr != nil {
		return nil, parseErr
	}
	return decisions, nil
}

// resolveDecision picks the decision for one candidate from a batch answer,
// failing open (store) when the batch call failed or the candidate's entry is
// missing/invalid. offset is the batch's first global index; localIdx is the
// candidate's position within the batch.
func (d *Detector) resolveDecision(answers map[int]Decision, llmErr error, offset, localIdx, batchSize int) Decision {
	globalIdx := offset + localIdx
	if llmErr != nil {
		d.warn("conflict detection failed open; storing candidates",
			"error", llmErr, "candidate_idx", globalIdx, "batch_size", batchSize)
		return Decision{CandidateIdx: globalIdx, Action: ActionStore}
	}
	dec, ok := answers[localIdx]
	if !ok {
		d.warn("conflict detection failed open; storing candidate",
			"reason", "llm omitted candidate entry", "candidate_idx", globalIdx, "batch_size", batchSize)
		return Decision{CandidateIdx: globalIdx, Action: ActionStore}
	}
	dec.CandidateIdx = globalIdx
	return dec
}

// parseLLMDecisions validates the sanitized JSON array into per-local-index
// decisions. Entries with unknown actions, out-of-range or missing candidate
// numbers, or target_ids referencing non-existent batch entries are rejected;
// absence from the returned map means "fill with store".
func parseLLMDecisions(body string, batchLen int) (map[int]Decision, error) {
	var wire []llmDecision
	dec := json.NewDecoder(strings.NewReader(body))
	// The payload must be exactly one JSON array: the first decode fills it,
	// and the next token must be EOF (no trailing commentary or junk).
	if err := dec.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode conflict decisions: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after decision array: %w", err)
	}

	out := make(map[int]Decision, len(wire))
	for _, w := range wire {
		if w.Candidate == nil {
			continue
		}
		idx := *w.Candidate
		if idx < 0 || idx >= batchLen {
			continue
		}
		action := w.Action
		if !validAction(action) {
			continue
		}
		if action != ActionStore && len(w.TargetIDs) == 0 {
			// merge/supersede/skip must name at least one target; an answer
			// without targets is unusable — treat as store.
			continue
		}
		out[idx] = Decision{
			CandidateIdx:  idx,
			Action:        action,
			TargetIDs:     w.TargetIDs,
			MergedContent: strings.TrimSpace(w.MergedContent),
			Reason:        strings.TrimSpace(w.Reason),
		}
	}
	return out, nil
}

func validAction(a Action) bool {
	switch a {
	case ActionStore, ActionMerge, ActionSupersede, ActionSkip:
		return true
	default:
		return false
	}
}

func (d *Detector) warn(msg string, args ...any) {
	d.log.Warn(msg, args...)
}
