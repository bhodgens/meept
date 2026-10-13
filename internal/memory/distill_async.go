package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory/conflict"
	"github.com/caimlas/meept/internal/queue"
)

// Async distillation (plan forest 20261012-memory-capability-forest, tree 01
// leaf L2): raw L0 conversation lines are distilled OFF the turn path into L1
// claims and L2 session-scene summaries via the PersistentQueue.
//
// NOTE: this file cannot import internal/memory/l0 — l0 imports this package
// for SanitizeForStore. The l0Record/readL0 helpers below mirror the frozen
// §2.1 JSONL schema and l0's tolerant scan (blank/torn lines skipped); the
// test file uses the real l0.Recorder for fixtures.

// Queue job types for the async distill pipeline (SHARED-CONVENTIONS §2.2).
const (
	// MemoryJobL1Extract distills raw L0 lines into auto-status claims.
	MemoryJobL1Extract queue.JobType = "memory.l1_extract"
	// MemoryJobL2Scene segments a session-day into scene summary files.
	MemoryJobL2Scene queue.JobType = "memory.l2_scene"
)

// markerCategory is the Category of the per-session L0 progress marker
// memory. Markers are plain episodic memories whose metadata carries
// session_id and l0_progress (the last fully-processed L0 message ID);
// append-only semantics: each advance stores a NEW marker row and readers
// take the newest (newest-rowid-first GetByCategory order).
const markerCategory = "l0_progress"

// markerScanLimit bounds the GetByCategory scan when locating a session's
// newest progress marker.
const markerScanLimit = 200

// L1ExtractPayload is the memory.l1_extract payload (SHARED-CONVENTIONS §2.2).
type L1ExtractPayload struct {
	SessionID  string `json:"session_id"`
	L0Path     string `json:"l0_path"`
	SinceMsgID string `json:"since_msg_id,omitempty"`
}

// L2ScenePayload is the memory.l2_scene payload (SHARED-CONVENTIONS §2.2).
type L2ScenePayload struct {
	SessionID string `json:"session_id"`
	// SinceTS is RFC3339; its calendar date selects the L0 day file and the
	// scene file names. The scene pass covers the full session-day.
	SinceTS string `json:"since_ts"`
}

// JobEnqueuer is the minimal queue surface the distill pipeline needs.
// Adapted to the real queue API: *queue.PersistentQueue satisfies it via
// Queue.Enqueue(ctx, *Job). No handler registry exists in internal/queue —
// dispatch is worker.JobProcessor.Process — so DistillWorker exposes a
// matching Process method for the wiring leaf (T01-L3) to hand to a worker.
type JobEnqueuer interface {
	Enqueue(ctx context.Context, job *queue.Job) error
}

// DistillLLM is the narrow LLM interface for distill jobs, mirroring the
// AmbientClassifierLLM precedent (defined locally to avoid an import cycle on
// internal/llm). The daemon adapts its resolved memory_model slot chatter
// (memory_model client > general provider > general client) to this.
type DistillLLM interface {
	// DistillComplete sends one distill prompt and returns the raw JSON body.
	DistillComplete(ctx context.Context, prompt string) ([]byte, error)
}

// DistillWorkerConfig holds construction parameters for DistillWorker.
type DistillWorkerConfig struct {
	// Manager stores L1 claims and the per-session L0 progress markers.
	Manager *Manager
	// LLM runs extraction and scene-segmentation prompts. Nil LLM: L1 fails
	// (queue retry/backoff applies); L2 degrades to the fallback scene.
	LLM DistillLLM
	// DataDir is the memory data dir (<dataDir>/l0, <dataDir>/l2).
	DataDir string
	// Enqueuer, when non-nil, is used to chain one L2 job after each L1
	// success. Nil = chaining disabled (L3 wiring decides).
	Enqueuer JobEnqueuer
	// Conflict, when non-nil with Enabled=true, runs LLM conflict detection
	// over each L1 extraction batch before storing (tree 02 leaf L2). Nil or
	// disabled: candidates are stored directly — no recall, no LLM call.
	Conflict *ConflictWiring
	Logger   *slog.Logger
}

// DistillWorker consumes memory.l1_extract / memory.l2_scene queue jobs.
type DistillWorker struct {
	manager  *Manager
	llm      DistillLLM
	dataDir  string
	enqueuer JobEnqueuer
	conflict *ConflictWiring
	logger   *slog.Logger
}

// NewDistillWorker constructs a distill worker.
func NewDistillWorker(cfg DistillWorkerConfig) *DistillWorker {
	w := &DistillWorker{
		manager:  cfg.Manager,
		llm:      cfg.LLM,
		dataDir:  cfg.DataDir,
		enqueuer: cfg.Enqueuer,
		conflict: cfg.Conflict,
		logger:   cfg.Logger,
	}
	if w.logger == nil {
		w.logger = slog.Default()
	}
	return w
}

// jobProcessor is the internal/worker.JobProcessor shape, restated locally so
// internal/memory need not import internal/worker. Compile-time conformance.
type jobProcessor interface {
	Process(ctx context.Context, job *queue.Job) (any, error)
}

var _ jobProcessor = (*DistillWorker)(nil)

// Process dispatches one queue job by type. LLM and store failures return an
// error — the queue's retry/backoff/dead-letter path owns failure handling
// (no private retry loop).
func (w *DistillWorker) Process(ctx context.Context, job *queue.Job) (any, error) {
	if job == nil {
		return nil, errors.New("distill: nil job")
	}
	switch job.Type {
	case MemoryJobL1Extract:
		return w.handleL1Extract(ctx, job.Payload)
	case MemoryJobL2Scene:
		return w.handleL2Scene(ctx, job.Payload)
	default:
		return nil, fmt.Errorf("distill: unsupported job type %q", job.Type)
	}
}

// EnqueueL1 enqueues one memory.l1_extract job.
func EnqueueL1(ctx context.Context, q JobEnqueuer, p L1ExtractPayload) (string, error) {
	job, err := queue.NewJob(MemoryJobL1Extract, p)
	if err != nil {
		return "", fmt.Errorf("distill: build l1 job: %w", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		return "", fmt.Errorf("distill: enqueue l1: %w", err)
	}
	return job.ID, nil
}

// EnqueueL2 enqueues one memory.l2_scene job.
func EnqueueL2(ctx context.Context, q JobEnqueuer, p L2ScenePayload) (string, error) {
	job, err := queue.NewJob(MemoryJobL2Scene, p)
	if err != nil {
		return "", fmt.Errorf("distill: build l2 job: %w", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		return "", fmt.Errorf("distill: enqueue l2: %w", err)
	}
	return job.ID, nil
}

// handleL1Extract reads the session's L0 lines since the payload position,
// runs the ambient extraction prompt, stores each candidate as an auto claim,
// then advances the per-session marker. The marker advances ONLY on full
// success: an LLM or store error returns before any marker write, so the
// queue retry re-reads the same window. A duplicate enqueue (marker already
// at the payload position) suppresses the work — no LLM call, no claims.
func (w *DistillWorker) handleL1Extract(ctx context.Context, payload []byte) (any, error) {
	var p L1ExtractPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("distill: parse l1 payload: %w", err)
	}
	if p.SessionID == "" {
		return nil, errors.New("distill: l1 payload missing session_id")
	}
	if p.L0Path == "" {
		return nil, errors.New("distill: l1 payload missing l0_path")
	}
	if w.manager == nil {
		return nil, errors.New("distill: l1 worker has no memory manager")
	}
	if w.llm == nil {
		return nil, errors.New("distill: l1 worker has no llm configured")
	}

	marker, err := w.l0ProgressMarker(ctx, p.SessionID)
	if err != nil {
		return nil, err
	}
	if marker != "" && (marker == p.SinceMsgID || markerPosCoversFileEnd(p.L0Path, p.SessionID, marker)) {
		// Duplicate enqueue: this window is already fully distilled.
		w.chainL2(ctx, p.SessionID)
		return map[string]any{"status": "suppressed", "session_id": p.SessionID}, nil
	}

	recs, err := readL0Records(p.L0Path, p.SessionID, p.SinceMsgID)
	if err != nil {
		return nil, err
	}

	lastID := p.SinceMsgID
	stored := 0
	merged := 0
	if len(recs) > 0 {
		prompt := fmt.Sprintf(ambientExtractionPromptTemplate, strings.Join(formatL0Conversation(recs), "\n"))
		raw, err := w.llm.DistillComplete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("distill l1: llm extraction: %w", err)
		}
		candidates, err := ParseAmbientCandidates(raw)
		if err != nil {
			return nil, fmt.Errorf("distill l1: parse candidates: %w", err)
		}
		stored, merged, err = w.storeCandidates(ctx, candidates)
		if err != nil {
			return nil, err
		}
		lastID = recs[len(recs)-1].ID
	}

	if err := w.advanceMarker(ctx, p.SessionID, lastID); err != nil {
		return nil, err
	}
	w.chainL2(ctx, p.SessionID)
	return map[string]any{
		"status":          "ok",
		"session_id":      p.SessionID,
		"claims_stored":   stored,
		"conflict_merged": merged,
		"l0_progress":     lastID,
	}, nil
}

// storeCandidates stores the extraction candidates, applying the conflict
// consumer rule when conflict wiring is enabled (tree 02 leaf L2). It returns
// (stored, merged, err): stored counts claims written (store/merge/supersede
// decisions), merged counts merge decisions specifically.
//
// Fail-open contract (SHARED-CONVENTIONS §2.3): detection runs through
// detectConflicts, which degrades to all-store on any error — extraction
// never fails because of conflict detection. Store errors after a decision
// still fail the job (the queue retry re-runs the window; claims are
// content-deduped downstream).
func (w *DistillWorker) storeCandidates(ctx context.Context, candidates []AmbientCandidate) (int, int, error) {
	// Build the conflict candidate list once; nil wiring/disabled skips.
	var ccands []conflictCandidate
	if w.conflict != nil && w.conflict.Enabled && len(candidates) > 0 {
		ccands = make([]conflictCandidate, len(candidates))
		for i, c := range candidates {
			ccands[i] = conflictCandidate{
				Idx: i,
				Cand: conflict.Candidate{
					ID:      fmt.Sprintf("cand-%d", i),
					Content: c.Text,
					Metadata: map[string]string{
						"type":       c.Type,
						"confidence": fmt.Sprintf("%.2f", c.Confidence),
					},
				},
			}
		}
	}

	var decisions []conflict.Decision
	if len(ccands) > 0 {
		var recall conflict.RecallStore
		if w.manager != nil {
			recall = w.manager.NewRecallStore()
		}
		decisions = detectConflicts(ctx, w.conflict, recall, ccands)
	}

	stored := 0
	merged := 0
	for i, c := range candidates {
		claim := Claim{
			Text:       c.Text,
			Premises:   c.Premises,
			Source:     c.Source,
			Confidence: c.Confidence,
			Status:     ClaimStatusAuto,
		}
		if decisions != nil {
			id, err := w.applyConflictDecision(ctx, ccands[i], decisions[i], claim)
			if err != nil {
				return stored, merged, err
			}
			if id != "" {
				stored++
				if decisions[i].Action == conflict.ActionMerge {
					merged++
				}
			}
			continue
		}
		// Conflict detection disabled: plain store (pre-detector behavior).
		if _, err := w.manager.StoreClaim(ctx, claim); err != nil {
			return stored, merged, fmt.Errorf("distill l1: store claim: %w", err)
		}
		stored++
	}
	return stored, merged, nil
}

// l0ProgressMarker returns the newest l0_progress position for the session
// ("" when none).
func (w *DistillWorker) l0ProgressMarker(ctx context.Context, sessionID string) (string, error) {
	epi := w.manager.Episodic()
	if epi == nil {
		return "", errors.New("distill: episodic store unavailable for l0 marker")
	}
	results, err := epi.GetByCategory(ctx, markerCategory, markerScanLimit)
	if err != nil {
		return "", fmt.Errorf("distill: read l0 marker: %w", err)
	}
	// GetByCategory orders newest-rowid first: the first session match is
	// the newest marker row.
	for _, r := range results {
		if asString(r.Memory.Metadata["session_id"]) == sessionID {
			return asString(r.Memory.Metadata["l0_progress"]), nil
		}
	}
	return "", nil
}

// advanceMarker appends a new marker row for the session. Append-only: never
// mutates prior markers.
func (w *DistillWorker) advanceMarker(ctx context.Context, sessionID, lastID string) error {
	_, err := w.manager.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Category: markerCategory,
		Content:  fmt.Sprintf("l0 distill progress marker for session %s", sessionID),
		Metadata: map[string]any{
			"session_id":  sessionID,
			"l0_progress": lastID,
			"advanced_at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return fmt.Errorf("distill: advance l0 marker: %w", err)
	}
	return nil
}

// chainL2 enqueues one follow-up L2 job for the session (best-effort: an L1
// success must not be retried — the marker already advanced — so a chain
// failure is logged, not returned).
func (w *DistillWorker) chainL2(ctx context.Context, sessionID string) {
	if w.enqueuer == nil {
		return
	}
	payload := L2ScenePayload{
		SessionID: sessionID,
		SinceTS:   time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := EnqueueL2(ctx, w.enqueuer, payload); err != nil {
		w.logger.Warn("distill: chain l2 enqueue failed", "session_id", sessionID, "error", err)
	}
}

// sceneSegment is one LLM-proposed scene.
type sceneSegment struct {
	SceneName  string   `json:"scene_name"`
	MessageIDs []string `json:"message_ids"`
}

// sceneFile is the on-disk L2 scene document.
type sceneFile struct {
	SessionID        string   `json:"session_id"`
	Date             string   `json:"date"`
	SceneName        string   `json:"scene_name"`
	Summary          string   `json:"summary"`
	SourceMessageIDs []string `json:"source_message_ids"`
	SegmentMethod    string   `json:"segment_method"` // "llm" | "fallback"
}

// handleL2Scene segments the session-day's L0 lines into scene files under
// <dataDir>/l2/. ONE memory-model call proposes [{scene_name, message_ids}];
// a malformed (or fully-invalid) segmentation falls back to ONE whole-session
// scene file with segment_method "fallback" — an L2 job never dead-letters on
// a malformed segmentation response. LLM transport errors still return an
// error (queue retry owns them).
func (w *DistillWorker) handleL2Scene(ctx context.Context, payload []byte) (any, error) {
	var p L2ScenePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("distill: parse l2 payload: %w", err)
	}
	if p.SessionID == "" {
		return nil, errors.New("distill: l2 payload missing session_id")
	}
	ts, err := time.Parse(time.RFC3339, p.SinceTS)
	if err != nil {
		return nil, fmt.Errorf("distill: l2 payload since_ts: %w", err)
	}
	date := ts.Format("2006-01-02")

	recs, err := readL0Records(w.l0DayPath(date), p.SessionID, "")
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return map[string]any{"status": "ok", "session_id": p.SessionID, "scenes": 0}, nil
	}

	segments, err := w.segmentScenes(ctx, recs)
	segmentMethod := "llm"
	if err != nil || len(segments) == 0 {
		if err != nil {
			w.logger.Warn("distill: scene segmentation unusable, falling back to whole-session scene",
				"session_id", p.SessionID, "error", err)
		}
		segments = []sceneSegment{{SceneName: "session", MessageIDs: recordIDs(recs)}}
		segmentMethod = "fallback"
	}

	written := 0
	for _, seg := range segments {
		sf := sceneFile{
			SessionID:        p.SessionID,
			Date:             date,
			SceneName:        seg.SceneName,
			Summary:          deriveSceneSummary(recs, seg.MessageIDs),
			SourceMessageIDs: seg.MessageIDs,
			SegmentMethod:    segmentMethod,
		}
		path := filepath.Join(w.l2Dir(), fmt.Sprintf("%s-%s-%s.json", sceneSlug(p.SessionID), date, sceneSlug(seg.SceneName)))
		if err := writeSceneFile(path, sf); err != nil {
			return nil, err
		}
		written++
	}
	return map[string]any{
		"status":     "ok",
		"session_id": p.SessionID,
		"scenes":     written,
		"method":     segmentMethod,
	}, nil
}

// segmentScenes runs ONE memory-model call returning [{scene_name,
// message_ids}], drops unknown/duplicate message IDs and empty segments, and
// returns the validated segments. Any parse or validation failure is an
// error (the caller falls back).
func (w *DistillWorker) segmentScenes(ctx context.Context, recs []l0Record) ([]sceneSegment, error) {
	if w.llm == nil {
		return nil, errors.New("distill: l2 worker has no llm configured")
	}
	prompt := fmt.Sprintf(sceneSegmentationPromptTemplate, strings.Join(formatL0WithIDs(recs), "\n"))
	raw, err := w.llm.DistillComplete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("distill l2: llm segmentation: %w", err)
	}
	var segments []sceneSegment
	trimmed := stripCodeFences(strings.TrimSpace(string(raw)))
	if json.Valid([]byte(trimmed)) {
		if err := json.Unmarshal([]byte(trimmed), &segments); err != nil {
			return nil, fmt.Errorf("distill l2: unmarshal segmentation: %w", err)
		}
	} else if block := extractJSONArray(trimmed); block != "" {
		if err := json.Unmarshal([]byte(block), &segments); err != nil {
			return nil, fmt.Errorf("distill l2: unmarshal segmentation: %w", err)
		}
	} else {
		return nil, errors.New("distill l2: no JSON array in segmentation response")
	}

	known := make(map[string]bool, len(recs))
	for _, r := range recs {
		known[r.ID] = true
	}
	used := make(map[string]bool)
	valid := make([]sceneSegment, 0, len(segments))
	for _, seg := range segments {
		name := strings.TrimSpace(seg.SceneName)
		if name == "" {
			continue
		}
		ids := make([]string, 0, len(seg.MessageIDs))
		for _, id := range seg.MessageIDs {
			if known[id] && !used[id] {
				ids = append(ids, id)
				used[id] = true
			}
		}
		if len(ids) == 0 {
			continue
		}
		valid = append(valid, sceneSegment{SceneName: name, MessageIDs: ids})
	}
	return valid, nil
}

func (w *DistillWorker) l0Dir() string { return filepath.Join(w.dataDir, "l0") }
func (w *DistillWorker) l2Dir() string { return filepath.Join(w.dataDir, "l2") }
func (w *DistillWorker) l0DayPath(date string) string {
	return filepath.Join(w.l0Dir(), date+".jsonl")
}

// writeSceneFile writes one L2 scene document (mkdir -p, overwrite-safe so a
// retried L2 job replaces its own output).
func writeSceneFile(path string, sf sceneFile) error {
	line, err := json.Marshal(sf)
	if err != nil {
		return fmt.Errorf("distill: marshal scene file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("distill: create l2 dir: %w", err)
	}
	if err := os.WriteFile(path, line, 0o644); err != nil {
		return fmt.Errorf("distill: write scene file %s: %w", path, err)
	}
	return nil
}

// sceneSegmentationPromptTemplate is the L2 scene-segmentation prompt
// (resolved Q2b: LLM scene segmentation, TAM SceneSegment shape).
const sceneSegmentationPromptTemplate = `You are a conversation segmenter. Split the following conversation into
coherent scenes (a scene is one topic or activity block).

Rules:
- Every message id MUST appear in exactly one segment.
- Do not invent ids; use only the ids shown.
- Give each scene a short lowercase name (1-4 words).

Return ONLY a JSON array:
[{"scene_name": "<short name>", "message_ids": ["<id>", ...]}, ...]

Conversation (one message per line, format: <id> | <role>: <content>):
%s`

// formatL0Conversation renders records for the extraction prompt.
func formatL0Conversation(recs []l0Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Role+": "+r.Content)
	}
	return out
}

// formatL0WithIDs renders records with their ids for the segmentation prompt.
func formatL0WithIDs(recs []l0Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID+" | "+r.Role+": "+r.Content)
	}
	return out
}

func recordIDs(recs []l0Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}

// deriveSceneSummary builds a short extracted-context summary (no extra LLM
// call): the first sentences of the segment's content, capped at 280 runes.
func deriveSceneSummary(recs []l0Record, ids []string) string {
	byID := make(map[string]string, len(recs))
	for _, r := range recs {
		byID[r.ID] = r.Content
	}
	var parts []string
	for _, id := range ids {
		if c, ok := byID[id]; ok && strings.TrimSpace(c) != "" {
			parts = append(parts, strings.TrimSpace(c))
		}
	}
	text := strings.Join(parts, " ")
	sentences := strings.Split(text, ". ")
	n := 3
	if len(sentences) < n {
		n = len(sentences)
	}
	summary := strings.TrimSpace(strings.Join(sentences[:n], ". "))
	if runes := []rune(summary); len(runes) > 280 {
		summary = string(runes[:277]) + "..."
	}
	return summary
}

// sceneSlug reduces a scene name (or session id) to a filesystem-safe ASCII
// slug: letters/digits kept, every other rune run collapses to one dash,
// capped at 48 runes. Empty input yields "scene".
func sceneSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteRune('-')
			lastDash = true
		}
	}
	out := []rune(strings.Trim(b.String(), "-"))
	if len(out) > 48 {
		out = out[:48]
	}
	if len(out) == 0 {
		return "scene"
	}
	return string(out)
}

// l0Record mirrors the frozen §2.1 L0 line schema (see file comment: the l0
// package imports this one, so the shape is restated here).
type l0Record struct {
	ID        string         `json:"id"`
	TS        string         `json:"ts"`
	SessionID string         `json:"session_id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Meta      map[string]any `json:"meta"`
}

// readL0Records streams one JSONL file, skips blank/torn lines, keeps only
// the given session (when non-empty), and returns records strictly after
// sinceID (empty sinceID reads from the start). A missing file is an empty
// slice, mirroring l0 reader semantics.
//
// Day-boundary fallback (tree 01 L3): a sinceID that does not appear in
// THIS day's file (the marker lives in a previous day's file) reads from
// the start instead of returning nothing — otherwise the first turn of
// every new day would silently distill zero lines and the marker would
// strand in yesterday's file forever. IDs are unique (pkg/id), so the
// fallback cannot re-read an already-distilled window within the day.
func readL0Records(path, sessionID, sinceID string) ([]l0Record, error) {
	f, err := os.Open(path) // #nosec G304 -- path is caller-supplied data-dir path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("distill: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var out []l0Record
	seen := sinceID == ""
	sinceFound := sinceID == ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec l0Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // torn/corrupt line: skip, keep scanning
		}
		if !seen {
			if rec.ID == sinceID {
				seen = true
				sinceFound = true
			}
			continue
		}
		if sessionID != "" && rec.SessionID != sessionID {
			continue
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("distill: scan %s: %w", path, err)
	}
	if !sinceFound {
		// sinceID lives in another day's file (or was lost): restart the
		// window at the beginning of this file.
		return readL0RecordsFromStart(path, sessionID)
	}
	return out, nil
}

// readL0RecordsFromStart is readL0Records with an empty sinceID (used only
// by the day-boundary fallback above).
func readL0RecordsFromStart(path, sessionID string) ([]l0Record, error) {
	return readL0Records(path, sessionID, "")
}

// markerPosCoversFileEnd reports whether the session's l0_progress marker
// sits on the session's LAST record in the payload's day file — i.e. the
// requested window (anything after sinceID) contains no undistilled lines
// and the job is a duplicate no-op. Cheap single scan of an already-open
// file shape; used only for the suppression check (the window read itself
// happens after).
func markerPosCoversFileEnd(l0Path, sessionID, marker string) bool {
	recs, err := readL0RecordsFromStart(l0Path, sessionID)
	if err != nil || len(recs) == 0 {
		return false
	}
	return recs[len(recs)-1].ID == marker
}
