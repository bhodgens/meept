package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Distilled memory types and domains (loop-economics leaf 15).
//
// Lessons are distilled principles ("always X before Y because Z");
// procedures are reusable how-to templates that are NEVER auto-executed —
// they are surfaced as reference outlines in the system prompt only.
const (
	TypeLesson    MemoryType = "lesson"
	TypeProcedure MemoryType = "procedure"

	DomainLesson    = "lesson"
	DomainProcedure = "procedure"
)

// Length caps enforced at distill time.
const (
	// MaxLessonPrincipleChars caps the lesson principle length.
	MaxLessonPrincipleChars = 280
	// MaxProcedureSteps caps the number of steps in a procedure.
	MaxProcedureSteps = 20
)

// DefaultDistillSimilarity is the cosine/token-similarity threshold above
// which a newly distilled entry is considered a duplicate of an existing one.
const DefaultDistillSimilarity = 0.85

// DefaultDistillMinRelevance is the minimum search relevance for a distilled
// memory to be injected into a system prompt (confidence-threshold pattern).
const DefaultDistillMinRelevance = 0.3

// Sentinel errors for the distill pipeline.
var (
	// ErrDistillDisabled is returned when the [memory.distill] flag is off.
	ErrDistillDisabled = errors.New("memory distillation is disabled")
	// ErrDuplicateDistill is returned when a distilled entry closely matches
	// an existing memory (cosine/Jaccard above the threshold).
	ErrDuplicateDistill = errors.New("distilled memory duplicates an existing memory")
	// ErrMalformedDistilled is returned when stored distilled content is not
	// valid JSON for its type. Malformed entries must be rejected at read.
	ErrMalformedDistilled = errors.New("malformed distilled memory content")
	// ErrNoDistillSummarizer is returned when no summarizer is wired.
	ErrNoDistillSummarizer = errors.New("no distill summarizer configured")
)

// Lesson is a distilled principle with supporting evidence references.
type Lesson struct {
	Principle   string   `json:"principle"`
	Because     string   `json:"because,omitempty"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

// Procedure is a reusable how-to template. It is documentation, not an
// executable recipe: nothing in meept runs these steps automatically.
type Procedure struct {
	Title        string   `json:"title"`
	Steps        []string `json:"steps"`
	TriggerHints []string `json:"trigger_hints,omitempty"`
}

// EncodeLesson validates caps and serializes a Lesson to stored content JSON.
func EncodeLesson(l Lesson) (string, error) {
	if strings.TrimSpace(l.Principle) == "" {
		return "", fmt.Errorf("%w: lesson principle is empty", ErrMalformedDistilled)
	}
	if len(l.Principle) > MaxLessonPrincipleChars {
		l.Principle = l.Principle[:MaxLessonPrincipleChars]
	}
	data, err := json.Marshal(l)
	if err != nil {
		return "", fmt.Errorf("encode lesson: %w", err)
	}
	return string(data), nil
}

// DecodeLesson parses stored content into a Lesson, rejecting malformed JSON.
func DecodeLesson(content string) (*Lesson, error) {
	var l Lesson
	if err := json.Unmarshal([]byte(content), &l); err != nil {
		return nil, fmt.Errorf("%w: lesson: %w", ErrMalformedDistilled, err)
	}
	if strings.TrimSpace(l.Principle) == "" {
		return nil, fmt.Errorf("%w: lesson principle is empty", ErrMalformedDistilled)
	}
	return &l, nil
}

// EncodeProcedure validates caps and serializes a Procedure to stored content.
func EncodeProcedure(p Procedure) (string, error) {
	if strings.TrimSpace(p.Title) == "" {
		return "", fmt.Errorf("%w: procedure title is empty", ErrMalformedDistilled)
	}
	if len(p.Steps) > MaxProcedureSteps {
		p.Steps = p.Steps[:MaxProcedureSteps]
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode procedure: %w", err)
	}
	return string(data), nil
}

// DecodeProcedure parses stored content into a Procedure, rejecting
// malformed JSON.
func DecodeProcedure(content string) (*Procedure, error) {
	var p Procedure
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return nil, fmt.Errorf("%w: procedure: %w", ErrMalformedDistilled, err)
	}
	if strings.TrimSpace(p.Title) == "" {
		return nil, fmt.Errorf("%w: procedure title is empty", ErrMalformedDistilled)
	}
	return &p, nil
}

// ValidateDistilledContent rejects malformed stored distilled JSON at read
// time. Content for a memory whose category is "lesson" or "procedure" must
// parse as the corresponding structure; anything else returns
// ErrMalformedDistilled. Non-distill categories return nil.
func ValidateDistilledContent(category, content string) error {
	switch {
	case category == DomainLesson:
		_, err := DecodeLesson(content)
		return err
	case category == DomainProcedure:
		_, err := DecodeProcedure(content)
		return err
	default:
		return nil
	}
}

// IsDistillDomain reports whether a task-domain string is a distill domain.
func IsDistillDomain(domain string) bool {
	return domain == DomainLesson || domain == DomainProcedure
}

// DistillSummarizer condenses a set of source memories into the structured
// JSON payload for a distill kind ("lesson" or "procedure"). The production
// implementation wraps the manager's LLM client; tests inject fakes.
type DistillSummarizer interface {
	SummarizeForDistill(ctx context.Context, kind string, sources []Memory) (string, error)
}

// DistillItem is a queued distillation request originating from a reflection
// collector proposal of kind "pattern".
type DistillItem struct {
	// Kind is "lesson" or "procedure".
	Kind string `json:"kind"`
	// Change is the proposed distilled content seed (raw observation).
	Change string `json:"change"`
	// Justification is why this pattern was proposed.
	Justification string `json:"justification,omitempty"`
	// EvidenceIDs are memory IDs supporting the pattern.
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

// DistillQueueSummary reports the outcome of one drain pass.
type DistillQueueSummary struct {
	Stored     int
	Duplicates int
	Retained   int
}

// distillQueue is the bounded pending queue drained on evolver-cycle timing.
type distillQueue struct {
	mu    sync.Mutex
	items []DistillItem
}

func (q *distillQueue) enqueue(item DistillItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
}

// popAll pops all pending items; requeue pushes them back on failure so
// the queue retains them for the next cycle.
func (q *distillQueue) popAll() []DistillItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.items
	q.items = nil
	return out
}

func (q *distillQueue) requeue(items []DistillItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(items, q.items...)
}

// SetDistillSummarizer overrides the summarizer used by Distill. Primarily
// for tests; production falls back to the manager's LLM client wrapper.
func (m *Manager) SetDistillSummarizer(s DistillSummarizer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.distillSummarizer = s
}

// QueueDistill appends a distillation request to the pending queue. Items sit
// until DrainDistillQueue is invoked (evolver cycle timing — no scheduler of
// its own). No-op when the distill flag is off (flag gates everything).
func (m *Manager) QueueDistill(item DistillItem) {
	if !m.config.Distill.Enabled {
		return
	}
	if item.Kind != DomainLesson && item.Kind != DomainProcedure {
		item.Kind = DomainLesson
	}
	m.distillQ.enqueue(item)
}

// DrainDistillQueue processes every pending distill item: summarize, enforce
// caps, dedupe against existing memories, and store. A summarizer failure
// retains the remaining items in the queue (graceful skip) and returns the
// error. Duplicates are dropped (not retained).
func (m *Manager) DrainDistillQueue(ctx context.Context) (DistillQueueSummary, error) {
	if !m.config.Distill.Enabled {
		return DistillQueueSummary{}, ErrDistillDisabled
	}
	var summary DistillQueueSummary
	pending := m.distillQ.popAll()
	for i, item := range pending {
		src := []Memory{{
			Content:  item.Change,
			Category: item.Kind,
			Metadata: map[string]any{
				"justification": item.Justification,
				"evidence_ids":  item.EvidenceIDs,
			},
		}}
		mem, err := m.Distill(ctx, src)
		switch {
		case err == nil:
			summary.Stored++
			m.logger.Info("distilled memory stored",
				"id", mem.ID,
				"kind", item.Kind,
			)
		case errors.Is(err, ErrDuplicateDistill):
			summary.Duplicates++
			m.logger.Debug("distilled memory dropped as duplicate", "kind", item.Kind)
		default:
			// Summarizer/storage failure: retain this item and everything
			// behind it for the next cycle, then stop this drain.
			summary.Retained = len(pending) - i
			m.distillQ.requeue(pending[i:])
			return summary, fmt.Errorf("distill %s: %w", item.Kind, err)
		}
	}
	return summary, nil
}

// Distill condenses source memories into a single lesson or procedure memory
// using the summarization infra, enforcing length caps, deduping against
// existing memories (cosine > threshold on embeddings when an embedder is
// wired, else token-Jaccard), and storing the result. The kind is taken from
// src[0].Category ("lesson" default, or "procedure").
func (m *Manager) Distill(ctx context.Context, src []Memory) (*Memory, error) {
	if !m.config.Distill.Enabled {
		return nil, ErrDistillDisabled
	}
	if len(src) == 0 {
		return nil, fmt.Errorf("distill requires at least one source memory")
	}
	summarizer := m.distillSummarizerFor()
	if summarizer == nil {
		return nil, ErrNoDistillSummarizer
	}

	kind := DomainLesson
	if c := strings.ToLower(strings.TrimSpace(src[0].Category)); c == DomainProcedure {
		kind = DomainProcedure
	}

	payload, err := summarizer.SummarizeForDistill(ctx, kind, src)
	if err != nil {
		return nil, fmt.Errorf("distill summarizer: %w", err)
	}

	content, err := normalizeDistillPayload(kind, payload, src)
	if err != nil {
		return nil, err
	}
	if err := m.checkDistillDuplicate(ctx, kind, content); err != nil {
		return nil, err
	}
	return m.storeDistilled(kind, content)
}

// distillSummarizerFor resolves the effective summarizer: the injected test
// hook, else a wrapper around the manager LLM client.
func (m *Manager) distillSummarizerFor() DistillSummarizer {
	m.mu.RLock()
	s := m.distillSummarizer
	llmClient := m.llm
	m.mu.RUnlock()
	if s != nil {
		return s
	}
	if llmClient != nil {
		return &llmDistillSummarizer{client: llmClient}
	}
	return nil
}

// normalizeDistillPayload decodes the summarizer output, enforces length
// caps, and re-encodes canonical stored content. Evidence IDs from the
// source memories are preserved when the summarizer omits them.
func normalizeDistillPayload(kind, payload string, src []Memory) (string, error) {
	evidence := collectEvidenceIDs(src)
	if kind == DomainProcedure {
		var p Procedure
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return "", fmt.Errorf("%w: summarizer output for procedure: %w", ErrMalformedDistilled, err)
		}
		if len(p.Steps) > MaxProcedureSteps {
			p.Steps = p.Steps[:MaxProcedureSteps]
		}
		if len(p.TriggerHints) == 0 {
			p.TriggerHints = evidence
		}
		return EncodeProcedure(p)
	}
	var l Lesson
	if err := json.Unmarshal([]byte(payload), &l); err != nil {
		return "", fmt.Errorf("%w: summarizer output for lesson: %w", ErrMalformedDistilled, err)
	}
	if len(l.Principle) > MaxLessonPrincipleChars {
		l.Principle = l.Principle[:MaxLessonPrincipleChars]
	}
	if len(l.EvidenceIDs) == 0 {
		l.EvidenceIDs = evidence
	}
	return EncodeLesson(l)
}

// collectEvidenceIDs gathers evidence_ids metadata from source memories.
func collectEvidenceIDs(src []Memory) []string {
	var ids []string
	for _, s := range src {
		if s.Metadata == nil {
			continue
		}
		raw, ok := s.Metadata["evidence_ids"].([]string)
		if !ok {
			continue
		}
		ids = append(ids, raw...)
	}
	return ids
}

// checkDistillDuplicate compares the candidate content against existing
// distilled/task memories and returns ErrDuplicateDistill when similarity
// exceeds the configured threshold.
func (m *Manager) checkDistillDuplicate(ctx context.Context, kind, content string) error {
	dedupeText := canonicalDedupeText(kind, content)
	threshold := m.config.Distill.SimilarityThreshold
	if threshold <= 0 {
		threshold = DefaultDistillSimilarity
	}
	existing, err := m.searchViaSQLite(ctx, MemoryQuery{
		Query: "",
		Type:  MemoryTypeTask,
		Limit: 200,
	})
	if err != nil {
		// If we cannot check, be conservative and allow the store rather
		// than failing the whole distillation.
		m.logger.Warn("distill dedupe check failed; allowing store", "error", err)
		return nil
	}

	m.mu.RLock()
	embedder := m.embedder
	m.mu.RUnlock()

	var candVec []float32
	useVec := embedder != nil
	if useVec {
		v, verr := embedder.GenerateEmbedding(ctx, content)
		if verr != nil || len(v) == 0 {
			useVec = false
		} else {
			candVec = v
		}
	}
	candVec = nil
	useVec = false
	if embedder != nil {
		if v, verr := embedder.GenerateEmbedding(ctx, dedupeText); verr == nil && len(v) > 0 {
			candVec = v
			useVec = true
		}
	}
	for _, r := range existing {
		// Compare against the semantic text of prior memories (their
		// canonical distilled text when they are distilled entries) so
		// evidence-id churn does not mask true duplicates.
		rText := canonicalDedupeText(r.Memory.Category, r.Memory.Content)
		var sim float64
		if useVec {
			ev, eerr := embedder.GenerateEmbedding(ctx, rText)
			if eerr != nil || len(ev) == 0 {
				sim = tokenJaccard(dedupeText, rText)
			} else {
				sim = float64(cosineSimilarity(candVec, ev))
			}
		} else {
			sim = tokenJaccard(dedupeText, rText)
		}
		if sim > threshold {
			return ErrDuplicateDistill
		}
	}
	return nil
}

// canonicalDedupeText extracts the comparable text from distilled content:
// the principle for lessons, title+steps for procedures, falling back to the
// raw content for non-distilled entries.
func canonicalDedupeText(kind, content string) string {
	switch kind {
	case DomainLesson:
		var l Lesson
		if err := json.Unmarshal([]byte(content), &l); err == nil && l.Principle != "" {
			return l.Principle
		}
	case DomainProcedure:
		var p Procedure
		if err := json.Unmarshal([]byte(content), &p); err == nil && p.Title != "" {
			return p.Title + "\n" + strings.Join(p.Steps, "\n")
		}
	}
	return content
}

// tokenJaccard computes whitespace-token Jaccard similarity between two
// texts. Used as the dedupe fallback when embeddings are unavailable.
func tokenJaccard(a, b string) float64 {
	setA := tokenize(a)
	setB := tokenize(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	inter := 0
	for t := range setA {
		if setB[t] {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union <= 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenize(s string) map[string]bool {
	out := make(map[string]bool)
	for _, f := range strings.Fields(strings.ToLower(s)) {
		out[strings.Trim(f, ".,;:!?\"'()")] = true
	}
	delete(out, "")
	return out
}

// storeDistilled persists a validated distilled payload.
func (m *Manager) storeDistilled(kind, content string) (*Memory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mem := Memory{
		Content:  content,
		Type:     MemoryTypeTask,
		Category: kind,
	}
	id, err := m.Store(ctx, mem)
	if err != nil {
		return nil, fmt.Errorf("store distilled %s: %w", kind, err)
	}
	stored, err := m.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reload distilled %s: %w", kind, err)
	}
	return stored, nil
}

// RelevantDistilled returns stored lessons and procedures relevant to the
// query, above the minimum-relevance threshold, capped at limit. Returns
// nothing when the distill flag is off (flag-off = zero behavior change).
func (m *Manager) RelevantDistilled(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	if !m.config.Distill.Enabled {
		return nil, nil
	}
	minRel := m.config.Distill.MinRelevance
	if minRel <= 0 {
		minRel = DefaultDistillMinRelevance
	}
	if limit <= 0 {
		limit = 5
	}
	// A negative MinRelevance disables relevance filtering entirely (tests);
	// zero uses the package default.
	if m.config.Distill.MinRelevance < 0 {
		minRel = 0
	}
	runSearch := func(q string) ([]MemoryResult, error) {
		return m.searchViaSQLite(ctx, MemoryQuery{
			Query:        q,
			Type:         MemoryTypeTask,
			Limit:        limit * 4,
			MinRelevance: minRel,
		})
	}
	// Implicit-AND full-text matching is too strict for relevance ranking
	// over JSON-blob content: a query of three terms where the stored text
	// has only two would miss entirely. Search the whole query first, then
	// fall back to per-token probes and merge when it under-delivers.
	results, err := runSearch(query)
	if err != nil {
		return nil, err
	}
	if len(results) < limit && strings.TrimSpace(query) != "" {
		for _, tok := range strings.Fields(query) {
			more, serr := runSearch(tok)
			if serr != nil {
				continue
			}
			results = append(results, more...)
		}
	}
	var out []MemoryResult
	seen := make(map[string]bool)
	for _, r := range results {
		if !IsDistillDomain(r.Memory.Category) {
			continue
		}
		if seen[r.Memory.ID] {
			continue // appear once
		}
		seen[r.Memory.ID] = true
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// llmDistillSummarizer adapts the manager's llm.Chatter to DistillSummarizer.
type llmDistillSummarizer struct {
	client llm.Chatter
}

var _ DistillSummarizer = (*llmDistillSummarizer)(nil)

const (
	distillLessonSystemPrompt    = "You are a distillation engine. Condense observations into ONE reusable principle. Output ONLY JSON: {\"principle\": string (<=280 chars), \"because\": string, \"evidence_ids\": [string]}."
	distillProcedureSystemPrompt = "You are a distillation engine. Condense observations into ONE reusable how-to procedure template (documentation only, never auto-executed). Output ONLY JSON: {\"title\": string, \"steps\": [string] (max 20), \"trigger_hints\": [string]}."
)

// SummarizeForDistill asks the LLM to condense sources into structured JSON.
func (s *llmDistillSummarizer) SummarizeForDistill(ctx context.Context, kind string, sources []Memory) (string, error) {
	var sb strings.Builder
	for i, src := range sources {
		fmt.Fprintf(&sb, "--- observation %d (%s) ---\n%s\n", i+1, src.Category, src.Content)
		if j, ok := src.Metadata["justification"].(string); ok && j != "" {
			fmt.Fprintf(&sb, "why: %s\n", j)
		}
	}
	sys := distillLessonSystemPrompt
	if kind == DomainProcedure {
		sys = distillProcedureSystemPrompt
	}
	resp, err := s.client.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: sys},
		{Role: llm.RoleUser, Content: sb.String()},
	}, llm.WithMaxTokens(500), llm.WithTemperature(0.2))
	if err != nil {
		return "", fmt.Errorf("distill chat: %w", err)
	}
	if resp == nil {
		return "", errors.New("distill chat returned nil response")
	}
	return ExtractJSONFromLLM(resp.Content), nil
}

// ExtractJSONFromLLM extracts the first {...} block from an LLM response,
// tolerating markdown fences and prose wrappers.
func ExtractJSONFromLLM(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}
