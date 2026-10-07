package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
// evidence_ids elements are coerced to strings when the producer emitted
// numbers (small models emit [101, 102] for ["101", "102"]); elements that
// are neither strings nor numbers are dropped rather than failing the whole
// lesson. Only the decode path is tolerant — EncodeLesson stays strict.
func DecodeLesson(content string) (*Lesson, error) {
	return decodeLessonWire(content)
}

// lessonWire is the tolerant decode shape for lesson JSON: evidence_ids is
// captured raw so element-level coercion can happen after the outer object
// parses.
type lessonWire struct {
	Principle   string          `json:"principle"`
	Because     string          `json:"because"`
	EvidenceIDs json.RawMessage `json:"evidence_ids"`
}

// decodeLessonWire parses raw lesson JSON into a Lesson with lenient
// evidence_ids handling; everything else keeps strict decoding semantics.
func decodeLessonWire(content string) (*Lesson, error) {
	var w lessonWire
	if err := json.Unmarshal([]byte(content), &w); err != nil {
		return nil, fmt.Errorf("%w: lesson: %w", ErrMalformedDistilled, err)
	}
	if strings.TrimSpace(w.Principle) == "" {
		return nil, fmt.Errorf("%w: lesson principle is empty", ErrMalformedDistilled)
	}
	return &Lesson{
		Principle:   w.Principle,
		Because:     w.Because,
		EvidenceIDs: coerceEvidenceIDs(w.EvidenceIDs),
	}, nil
}

// coerceEvidenceIDs converts a raw evidence_ids JSON value into []string.
// String elements pass through; numeric elements are formatted losslessly;
// anything else (null, object, bool) is dropped. A non-array or absent value
// yields nil.
func coerceEvidenceIDs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil // not an array: treat as absent
	}
	out := make([]string, 0, len(elems))
	for _, el := range elems {
		if string(el) == "null" {
			continue // null element: drop rather than coerce to ""
		}
		var s string
		if err := json.Unmarshal(el, &s); err == nil {
			out = append(out, s)
			continue
		}
		var num json.Number
		if err := json.Unmarshal(el, &num); err == nil {
			out = append(out, num.String())
		}
		// null / object / bool element: drop rather than fail the lesson.
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	switch category {
	case DomainLesson:
		_, err := DecodeLesson(content)
		return err
	case DomainProcedure:
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
	if err := m.checkDistillDuplicate(ctx, kind, content, src); err != nil {
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
//
// Identity scoping (bughunt L15). Root AGENTS.md names "Content comparison for
// deduplication" as the canonical hacky pattern: a lesson and a procedure that
// happen to restate the same principle have identical canonical text and would
// otherwise collapse into each other. The fix is a SCOPING KEY, not a
// content hash: two candidates only dedupe against each other when they share
// an identity scope.
//
// The scope is the source lineage, which the ledger already carries:
//   - a memory ID, when the distill item references source memories by id
//     (EvidenceIDs) — the strongest scope, and the one a re-import of the SAME
//     evidence carries;
//   - otherwise the (kind, session/task/bot) tuple of the sources.
//
// Consequences, both pinned by tests:
//   - two DISTINCT lessons that restate the same principle, from different
//     sources, do NOT collapse (different scope) — content similarity alone is
//     not identity;
//   - the SAME lesson re-imported (same source ids, same scope) still dedupes.
//
// Within a single scope, content similarity still decides — a scope is a
// containment boundary, not an identity replacement. Without a scope (no ids,
// no session/task/bot on the candidate) the comparison stays content-keyed and
// conservative, which is the only option available for a candidate with no
// provenance at all.
func (m *Manager) checkDistillDuplicate(ctx context.Context, kind, content string, src []Memory) error {
	candScope := distillScopeKey(src)
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
	useVec := false
	if embedder != nil {
		if v, verr := embedder.GenerateEmbedding(ctx, dedupeText); verr == nil && len(v) > 0 {
			candVec = v
			useVec = true
		}
	}
	for _, r := range existing {
		// Identity scope: a candidate only competes with memories from the
		// same lineage. Comparing across scopes is what let an unrelated
		// lesson silently swallow a new one.
		if candScope != "" && !distillScopeMatches(candScope, r.Memory) {
			continue
		}
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

// distillScopeKey is the identity scope for one distill candidate: the sorted
// set of SOURCE MEMORY IDS it was derived from. Two candidates sharing this key
// are the same derivation re-run (a re-import, a queue drain retry, an
// evolver cycle revisiting the same evidence) and may dedupe against each other.
//
// The ids are read from BOTH carriers, so a candidate (whose ids arrive via
// src[].Metadata["evidence_ids"]) and a stored row (whose ids are inside the
// encoded Lesson/Procedure payload, written by storeDistilled) resolve to the
// same key without a schema change:
//   - src metadata: map[string]any{"evidence_ids": []string}
//   - payload:     {"principle":"...","evidence_ids":["m1","m2"]}
//
// Empty when nothing carries ids — the caller then falls back to comparing
// content within whatever scope the stored memories declare.
func distillScopeKey(src []Memory) string {
	ids := collectEvidenceIDs(src)
	if len(ids) == 0 {
		ids = payloadEvidenceIDs(src)
	}
	if len(ids) == 0 {
		return ""
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	// Dedupe repeated ids so a source listed twice is the same scope.
	uniq := sorted[:0]
	for i, id := range sorted {
		if i == 0 || id != sorted[i-1] {
			uniq = append(uniq, id)
		}
	}
	return "ids:" + strings.Join(uniq, ",")
}

// payloadEvidenceIDs reads evidence ids out of an encoded Lesson/Procedure
// payload's own content, which is where storeDistilled persists them.
func payloadEvidenceIDs(src []Memory) []string {
	var ids []string
	for _, s := range src {
		var l Lesson
		if err := json.Unmarshal([]byte(s.Content), &l); err == nil && len(l.EvidenceIDs) > 0 {
			ids = append(ids, l.EvidenceIDs...)
			continue
		}
		var p Procedure
		if err := json.Unmarshal([]byte(s.Content), &p); err == nil && len(p.TriggerHints) > 0 {
			ids = append(ids, p.TriggerHints...)
		}
	}
	return ids
}

// distillScopeMatches reports whether a stored memory belongs to the
// candidate's scope, i.e. whether the candidate may COMPETE with it.
//
// Direction matters and is asymmetric on purpose (bughunt M2, owner decision
// "exclude"):
//
//   - candidate HAS a scope, stored row has none → NO MATCH. The stored row
//     predates evidence_ids, so nothing links it to this lineage. Comparing on
//     content alone is what let an unrelated legacy lesson swallow a new one —
//     the exact failure the identity scope was added to stop. A near-duplicate
//     that lands twice is VISIBLE and cheap; a silently swallowed lesson is
//     neither.
//   - candidate HAS a scope, stored row has the same scope → match.
//   - candidate HAS a scope, stored row has a DIFFERENT scope → no match.
//   - candidate has NO scope → match everything. Nothing better exists for a
//     candidate with no provenance, and failing closed here would let unbounded
//     duplicates accumulate. This is the pre-L15 behaviour, pinned by
//     TestDistill_NoEvidenceIDsStillDedupesOnContent.
//
// The cost of the exclude rule is that near-duplicates can re-admit during the
// transition, until every legacy row carries evidence ids. That is the intended
// trade: visible duplicates over invisible data loss.
func distillScopeMatches(candScope string, stored Memory) bool {
	if candScope == "" {
		return true // candidate has no identity to compare on
	}
	storedScope := distillScopeKey([]Memory{stored})
	if storedScope == "" {
		return false // legacy row: not provably this lineage (bughunt M2)
	}
	return storedScope == candScope
}

// TODO(bughunt-2026-10-07): the exclude rule above re-admits near-duplicates
// while legacy rows lack evidence_ids. Backfill removes the transition cost
// entirely: re-stamp existing distilled rows with the ids their Lesson/Procedure
// payloads already carry (storeDistilled writes them), then this TODO closes.
//
// Until then the exclude direction is the deliberate trade — a duplicate is
// visible, a swallowed lesson is not — and blast radius stays opt-in (Distill
// is off by default).
//
// DECIDED by the owner 2026-10-07 as option (b) EXCLUDE, over (a) BACKFILL:
// (a) is a data migration on user rows, and it has to be right for every
// legacy row shape; (b) is one line, cannot lose data, and can be revised later
// once a backfill proves out. Option (a) remains the follow-up, not a
// rejection.// canonicalDedupeText extracts the comparable text from distilled content:
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
	for f := range strings.FieldsSeq(strings.ToLower(s)) {
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
	// MatchAny (FTS5 OR join) ranks partial term overlap instead of
	// demanding every token — the right semantics for relevance ranking.
	results, err := m.searchViaSQLite(ctx, MemoryQuery{
		Query:        query,
		Type:         MemoryTypeTask,
		Limit:        limit * 4,
		MinRelevance: minRel,
		MatchAny:     true,
	})
	if err != nil {
		return nil, err
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
	distillLessonSystemPrompt    = "You are a distillation engine. Condense observations into ONE reusable principle. Output ONLY JSON: {\"principle\": string (<=280 chars), \"because\": string, \"evidence_ids\": [string]}. evidence_ids MUST be an array of strings (IDs as quoted strings). If no evidence IDs are known, use an empty array."
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
	grammar := llm.LessonGrammar()
	if kind == DomainProcedure {
		sys = distillProcedureSystemPrompt
		// Bughunt F28: procedure distillation was forced through the LESSON
		// grammar, so every constrained procedure response failed grammar
		// validation and blocked the distill queue. Procedure shapes ride
		// their own grammar mirroring the lesson one.
		grammar = llm.ProcedureGrammar()
	}
	resp, err := s.client.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: sys},
		{Role: llm.RoleUser, Content: sb.String()},
	}, llm.WithMaxTokens(500), llm.WithTemperature(0.2), llm.WithRawGrammar(grammar))
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
