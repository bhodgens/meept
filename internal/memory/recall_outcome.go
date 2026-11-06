package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// RecallOutcomeRecord is one line of recall_outcomes.jsonl (leaf 04, contract
// C5): per injected memory item, whether the final assistant reply cited it.
// The key set is the contract's verbatim C5 set — do not rename fields.
type RecallOutcomeRecord struct {
	TS        string `json:"ts"`         // RFC3339 (UTC)
	SessionID string `json:"session_id"` // the turn's conversation/session id
	MemoryID  string `json:"memory_id"`  // id of the injected memory item
	Query     string `json:"query"`      // the recall query that surfaced it
	Injected  bool   `json:"injected"`   // true: the item reached a prompt this turn
	Cited     bool   `json:"cited"`      // overlap rule: reply's shared tokens >= minCiteOverlapTokens
	Tokens    int    `json:"tokens"`     // the item's injected token estimate

	// Salient is NOT serialized: it is the injected item's pre-computed
	// salient token set, staged with the snapshot so the terminal path
	// never re-parses the raw text twice.
	Salient []string `json:"-"`
}

// recallOutcomeFile is the filename under the memory data dir (gitignored:
// rows carry user text).
const recallOutcomeFile = "recall_outcomes.jsonl"

// minCiteOverlapTokens is the citation bar (C5): a reply "cites" an injected
// item when it shares AT LEAST this many word tokens with the item's salient
// text (lowercase comparison; reply-side tokens count with multiplicity —
// the committed F5 fixture's reply restates a 7-unique-token fact once per
// shared token and must classify as citing).
const minCiteOverlapTokens = 8

// RecallOutcomeLogger appends recall outcome rows to
// <dataDir>/recall_outcomes.jsonl. Nil-safe; a write error disables the
// logger (best-effort — telemetry must never fail the turn). Rotation rides
// the shared calibration-JSONL mechanism (RotateIfNeeded, 10 MiB cap, one .1
// generation).
type RecallOutcomeLogger struct {
	mu   sync.Mutex // guards disable-once state and the pending-turn buffer
	path string
	log  *slog.Logger
	// pending holds the per-turn injected-item snapshot for turns still in
	// flight, keyed by the conversation id the assembly ran under. The
	// turn.terminal subscriber calls BeginTurn at event time and TakeRows
	// inside the capture goroutine; a turn that never published (crashed
	// daemon, unhooked lane) leaves a stale buffer entry that the next
	// BeginTurn/TakeRows for the same conversation replaces.
	pending map[string][]RecallOutcomeRecord
	// loopSource is the snapshot drain seam (leaf 04 T3): the in-memory
	// conversation snapshot the render sites stage, drained by the
	// turn-terminal capture. Set via SetLoopSource at daemon wiring.
	loopSource func(conversationID string) []RecallOutcomeRecord
}

// NewRecallOutcomeLogger creates a logger writing into the memory data dir.
// Empty dataDir returns nil (disabled).
func NewRecallOutcomeLogger(dataDir string, log *slog.Logger) *RecallOutcomeLogger {
	if dataDir == "" {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &RecallOutcomeLogger{
		path:    filepath.Join(dataDir, recallOutcomeFile),
		log:     log.With("component", "recall-outcome"),
		pending: make(map[string][]RecallOutcomeRecord),
	}
}

// Path returns the logger's JSONL path (empty when disabled).
func (r *RecallOutcomeLogger) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// SetLoopSource wires the snapshot drain seam (leaf 04 T3): given a turn's
// conversation id, the source returns the injected-memory items the recall
// render site staged pre-render (draining = consuming). The daemon passes
// agent.DrainRecallOutcomeSnapshot closed over its loop sources. Nil source
// disables the drain (no rows). Nil-safe setter.
func (r *RecallOutcomeLogger) SetLoopSource(drain func(conversationID string) []RecallOutcomeRecord) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.loopSource = drain
	r.mu.Unlock()
}

// Drain is the capture subscriber's seam: it delegates to the wired loop
// source. A nil source (or nil logger) returns nothing.
func (r *RecallOutcomeLogger) Drain(conversationID string) []RecallOutcomeRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	drain := r.loopSource
	r.mu.Unlock()
	if drain == nil {
		return nil
	}
	return drain(conversationID)
}

// BeginTurn stages the injected-item snapshot for a turn about to emit its
// terminal event, keyed by conversation id. The terminal subscriber calls
// this synchronously at event time — before the async capture goroutine can
// read — so a same-conversation turn racing the previous turn's capture can
// never lose its snapshot (the capture consumes TakeRows, not the live map).
// An empty items slice explicitly CLEARS the staged snapshot for the
// conversation (no rows are fabricated for context the model never saw).
func (r *RecallOutcomeLogger) BeginTurn(conversationID string, items []RecallOutcomeRecord) {
	if r == nil || conversationID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = make(map[string][]RecallOutcomeRecord)
	}
	if len(items) == 0 {
		delete(r.pending, conversationID)
		return
	}
	r.pending[conversationID] = items
}

// TakeRows atomically consumes the staged snapshot for a completed turn.
// Snapshot items are NOT deleted on take: the chat lane injects memory on
// EVERY turn while only turns whose assembly ran publish a snapshot, so the
// last-seen items carry over until the next assembly for the same
// conversation (a turn with no assembly is a turn with the same context).
// An assembly that produced ZERO items explicitly clears the carry-over via
// BeginTurn with no items. Non-completed turns never reach the writer.
func (r *RecallOutcomeLogger) TakeRows(conversationID string) []RecallOutcomeRecord {
	if r == nil || conversationID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending[conversationID]
}

// LogOutcomes appends one C5 row per item. The items are the turn's consumed
// snapshot; the caller supplies the session id (the capture pipeline's
// resolved session, with the conversation-id fallback) and the final reply.
// Write failures disable the logger (logged, never fatal).
func (r *RecallOutcomeLogger) LogOutcomes(ctx context.Context, sessionID string, items []RecallOutcomeRecord, reply string) {
	if r == nil || len(items) == 0 {
		return
	}
	r.mu.Lock()
	path := r.path
	r.mu.Unlock()
	if path == "" {
		return
	}
	replyTokens := ReplyTokens(reply)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		row := item
		row.TS = now
		row.SessionID = sessionID
		row.Injected = true
		row.Cited = CitedByOverlap(replyTokens, item.Salient)
		if err := appendJSONL(path, row); err != nil {
			r.mu.Lock()
			r.path = ""
			r.mu.Unlock()
			r.log.Warn("recall outcome logging disabled", "error", err)
			return
		}
	}
}

// TokenEstimate is the injected-item token estimate (the shared 3-chars/token
// heuristic — the same estimator the memory context budgets use).
func TokenEstimate(text string) int {
	return llm.EstimateTokenCountHeuristic(text)
}

// SalientTokens returns the deduplicated lowercase word tokens of text (the
// citation-overlap token set for the INJECTED ITEM side). Empty text
// returns nil.
func SalientTokens(text string) []string {
	toks := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	seen := make(map[string]struct{}, len(toks))
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// ReplyTokens returns the lowercase word tokens of a reply, kept WITH
// multiplicity: each occurrence that matches the injected item's salient
// set counts toward the C5 bar (a reply that restates the fact twice cites
// twice as strongly).
func ReplyTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}

// CitedByOverlap applies the C5 citation rule: the reply cites an injected
// item when replyTokens (with multiplicity) share >= minCiteOverlapTokens
// members with itemSalient (the item's deduplicated token set).
// Conservative: an empty reply or item never cites.
func CitedByOverlap(replyTokens, itemSalient []string) bool {
	if len(replyTokens) == 0 || len(itemSalient) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(itemSalient))
	for _, t := range itemSalient {
		set[t] = struct{}{}
	}
	shared := 0
	for _, t := range replyTokens {
		if _, ok := set[t]; ok {
			shared++
			if shared >= minCiteOverlapTokens {
				return true
			}
		}
	}
	return false
}

// Cited reports whether reply cites item under the C5 overlap rule
// (convenience wrapper over SalientTokens/ReplyTokens + CitedByOverlap).
func Cited(item, reply string) bool {
	return CitedByOverlap(ReplyTokens(reply), SalientTokens(item))
}

// OutcomeRows reads back a recall_outcomes.jsonl file (tests / future
// analysis tooling). Malformed lines are skipped, never fatal.
func OutcomeRows(path string) ([]RecallOutcomeRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []RecallOutcomeRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row RecallOutcomeRecord
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue // malformed line: skip, never panic
		}
		rows = append(rows, row)
	}
	return rows, nil
}
