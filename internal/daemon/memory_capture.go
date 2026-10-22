package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/l0"
	"github.com/caimlas/meept/internal/session"
)

// memoryCapturePage is the last-page size when scanning the session store for
// the exchange's user message. Generous enough to cover any single turn's
// tool/assistant traffic before the final user line.
const memoryCapturePage = 20

// memoryCaptureLRUSize bounds the (ConversationID, Reply) dedup LRU.
const memoryCaptureLRUSize = 512

// WireMemoryCapture builds the turn-terminal memory capture subscriber (option
// C, plan docs/plans/20261013-turn-terminal-capture/plan.md). On every
// turn.terminal event with status "completed" it reads the exchange's user
// message from the session store, appends the L0 dialogue via recorder,
// enqueues one memory.l1_extract distill job via enqueuer (retired
// l0_record-hook semantics: since_msg_id = the day's last record before this
// append), and runs ambient epistemic extraction over the exchange.
//
// Every dependency is nil-safe; a nil piece disables just that piece:
//   - recorder nil   → no L0 lines
//   - enqueuer nil   → no distill enqueue
//   - extractor nil  → no ambient extraction
//   - store nil      → no user message resolvable → nothing captured
//   - b nil          → returns nil (no subscriber to wire)
//
// The per-event work runs async inside one panic-recovered goroutine and can
// never block the bus pump or fail a turn. Config gates (memory.l0_enabled,
// memory.epistemic.ambient_extraction.enabled) are applied by the caller in
// components.go, which decides which pieces to pass.
func WireMemoryCapture(
	b *bus.MessageBus,
	store session.Store,
	recorder *l0.Recorder,
	enqueuer memory.JobEnqueuer,
	extractor *memory.AmbientExtractor,
	logger *slog.Logger,
) *bus.Subscriber {
	if b == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", "memory-capture")
	mc := &memoryCapture{
		store:     store,
		enqueuer:  enqueuer,
		extractor: extractor,
		logger:    logger,
		seen:      newCaptureLRU(memoryCaptureLRUSize),
	}
	// Typed-nil guard: recorder is a CONCRETE *l0.Recorder, and assigning
	// it straight into the l0Appender interface field boxes a nil pointer
	// as a non-nil interface — mc.recorder != nil then passes and Append
	// derefs nil on EVERY captured turn (observed live 2026-10-22:
	// "memory capture panicked ... nil pointer dereference" per turn with
	// l0_enabled=false). Only assign a non-nil recorder.
	if recorder != nil {
		mc.recorder = recorder
	}
	return bus.SubscribeT(b, "memory-capture", agent.TopicTurnTerminal, mc.handleEvent)
}

// l0Appender is the recorder surface memoryCapture uses; *l0.Recorder
// satisfies it. An interface here lets tests inject a failing recorder (same
// package) without widening the frozen WireMemoryCapture contract.
type l0Appender interface {
	Append(sessionID, role, content string, meta map[string]any) error
	DatePath(date string) string
}

// memoryCapture holds the subscriber's dependencies and dedup state.
type memoryCapture struct {
	store     session.Store
	recorder  l0Appender
	enqueuer  memory.JobEnqueuer
	extractor *memory.AmbientExtractor
	logger    *slog.Logger
	seen      *captureLRU
}

// handleEvent is the typed-bus callback: synchronous, cheap, hands real work
// to a recovered goroutine.
func (mc *memoryCapture) handleEvent(ev agent.TurnTerminalEvent) {
	if ev.Status != "completed" {
		return
	}
	if ev.ConversationID == "" {
		return
	}
	if mc.seen.contains(ev.ConversationID, ev.Reply) {
		mc.logger.Debug("duplicate turn-terminal capture dropped",
			"conversation_id", ev.ConversationID, "turn_id", ev.TurnID)
		return
	}
	mc.seen.add(ev.ConversationID, ev.Reply)
	go mc.capture(ev) //nolint:context // bus pump context: each capture is fire-and-forget
}

// capture performs steps 3-6 of the contract inside one panic-recovered
// goroutine. Errors at any step are logged and never fatal — a failed L0
// write must not fail the user's turn (retired hook's posture).
func (mc *memoryCapture) capture(ev agent.TurnTerminalEvent) {
	defer func() {
		if r := recover(); r != nil {
			mc.logger.Error("memory capture panicked",
				"conversation_id", ev.ConversationID, "panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	if mc.store == nil {
		return
	}
	// Resolve the capture session: ev.SessionID if non-empty, else
	// ev.ConversationID (contract step 2). For RELAYED task turns both
	// fields carry the session's CONVERSATION id (conv-…), not the primary
	// session id — createTask links tasks by the conversation id the chat
	// handler passes the dispatcher, and publishTaskTerminalRelay overwrites
	// sessionID with that linked id. The message store keys messages by the
	// PRIMARY session id, so resolve the real session via
	// GetByConversationID when direct lookups miss (observed live: the
	// task's reply recorded no L0 without this fallback).
	captureSession := ev.SessionID
	if captureSession == "" {
		captureSession = ev.ConversationID
	}
	if !mc.storeHasMessages(captureSession) {
		if sess := mc.store.GetByConversationID(captureSession); sess != nil && sess.ID != captureSession {
			mc.logger.Debug("memory capture: resolved session by conversation id",
				"given", captureSession, "session_id", sess.ID)
			captureSession = sess.ID
		}
	}

	// Step 3: last user message from the store. Not found → skip the whole
	// turn; a reply without its prompt is noise.
	userMsg, ok := mc.lastUserMessage(captureSession)
	if !ok {
		mc.logger.Debug("memory capture skipped: no user message in store",
			"session_id", captureSession)
		return
	}

	// Step 4+5: L0 appends (user then assistant — same order as the retired
	// l0_record hook), then the distill enqueue with retired-hook marker
	// semantics. Recorder errors are logged, enqueue is still attempted.
	if mc.recorder != nil {
		appended := mc.recordExchange(captureSession, userMsg, ev.Reply)
		mc.enqueueDistill(captureSession, appended)
	}

	// Step 6: ambient extraction over the exchange. Best-effort; its own
	// recover (above) already covers panics from the extractor's classifier.
	if mc.extractor != nil && ev.Reply != "" {
		mc.runAmbient(captureSession, ev.TurnID, userMsg, ev.Reply)
	}
}

// storeHasMessages reports whether the store has any messages keyed under
// this exact id (cheap probe used to decide whether the conversation-id
// fallback should run).
func (mc *memoryCapture) storeHasMessages(sessionID string) bool {
	total, err := mc.store.GetMessageCount(sessionID)
	return err == nil && total > 0
}

// lastUserMessage fetches the last page of messages and scans backwards for
// the newest role=="user" message (pagination idiom from
// internal/services/session_service.go GetMessages / store GetMessages
// offset+limit semantics: ORDER BY id, LIMIT ? OFFSET ?).
func (mc *memoryCapture) lastUserMessage(sessionID string) (string, bool) {
	total, err := mc.store.GetMessageCount(sessionID)
	if err != nil || total == 0 {
		return "", false
	}
	offset := total - memoryCapturePage
	if offset < 0 {
		offset = 0
	}
	msgs, err := mc.store.GetMessages(sessionID, offset, memoryCapturePage)
	if err != nil {
		mc.logger.Warn("memory capture: get messages failed",
			"session_id", sessionID, "error", err)
		return "", false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && msgs[i].Content != "" {
			return msgs[i].Content, true
		}
	}
	return "", false
}

// recordExchange appends the user line then the assistant line (retired-hook
// append order). Per-append error tolerance matches l0RecordHook. Returns the
// number of lines actually appended (the enqueue boundary needs it).
func (mc *memoryCapture) recordExchange(sessionID, userMsg, reply string) int {
	appended := 0
	if userMsg != "" {
		if err := mc.recorder.Append(sessionID, "user", userMsg, nil); err != nil {
			mc.logger.Warn("l0 append failed", "session_id", sessionID, "error", err)
		} else {
			appended++
		}
	}
	if reply != "" {
		if err := mc.recorder.Append(sessionID, "assistant", reply, nil); err != nil {
			mc.logger.Warn("l0 append failed", "session_id", sessionID, "error", err)
		} else {
			appended++
		}
	}
	return appended
}

// enqueueDistill enqueues ONE memory.l1_extract job per turn with new L0
// lines, mirroring l0RecordHook.OnSessionEnd's enqueue block: since_msg_id is
// the day's last record BEFORE this turn's appends ("" on the day's first
// turn); the worker's l0_progress marker suppresses duplicate/re-run work.
// The enqueue is attempted even when every append failed (the retired hook's
// independent-error posture). Enqueue failure is logged, never fatal.
func (mc *memoryCapture) enqueueDistill(sessionID string, appended int) {
	if mc.enqueuer == nil || appended < 0 {
		return
	}
	date := time.Now().Format("2006-01-02")
	l0Dir := mc.recorderDir()
	sinceID := ""
	// Retired-hook marker semantics (l0_wiring.go OnSessionEnd): sinceID is
	// the last record BEFORE this turn's just-appended lines. With known
	// successful appends the boundary is exact (len-1-appended); the window
	// re-reads this turn's own appends exactly as the retired hook's len-2
	// boundary did. When appends failed (posture: enqueue still attempted)
	// or nothing pre-existed, fall back to "" — the worker's l0_progress
	// marker suppresses duplicate work, and a re-read window is safe.
	if recs, err := l0.ReadSession(l0Dir, date, sessionID); err == nil && appended > 0 {
		if idx := len(recs) - 1 - appended; idx >= 0 {
			sinceID = recs[idx].ID
		}
	}
	payload := memory.L1ExtractPayload{
		SessionID:  sessionID,
		L0Path:     mc.recorder.DatePath(date),
		SinceMsgID: sinceID,
	}
	if _, err := memory.EnqueueL1(context.Background(), mc.enqueuer, payload); err != nil {
		mc.logger.Warn("l1 enqueue failed", "session_id", sessionID, "error", err)
	}
}

// recorderDir returns the recorder's <dataDir>/l0 directory. The recorder
// writes <dataDir>/l0/<date>.jsonl via NewRecorder(dataDir, ...), so the
// parent of the date path's directory is <dataDir>; DatePath(date) =
// <dir>/<date>.jsonl where <dir> = filepath.Dir — exactly l0DirFor(dataDir).
func (mc *memoryCapture) recorderDir() string {
	date := time.Now().Format("2006-01-02")
	return filepath.Dir(mc.recorder.DatePath(date))
}

// runAmbient runs the extractor over the exchange and writes the surviving
// candidates as auto claims. The retired loop hook gated/filter via
// EpistemicHook.AfterTurn; the subscriber path runs the same Extract →
// splitFiltered → WriteCandidates sequence with the same config defaults
// (gates applied by the caller when constructing the extractor).
func (mc *memoryCapture) runAmbient(sessionID, turnID, userMsg, reply string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	candidates, err := mc.extractor.Extract(ctx, []string{userMsg, reply})
	if err != nil {
		mc.logger.Warn("ambient extract failed", "session_id", sessionID, "error", err)
		return
	}
	if len(candidates) == 0 {
		return
	}
	ids, err := mc.extractor.WriteCandidates(ctx, candidates)
	if err != nil {
		mc.logger.Warn("ambient write candidates failed", "session_id", sessionID, "error", err)
		return
	}
	if len(ids) > 0 {
		mc.logger.Info("ambient extraction wrote auto claims",
			"count", len(ids), "session_id", sessionID, "turn_id", turnID)
	}
}

// captureLRU is a fixed-size (key1, key2) LRU for turn-terminal dedup.
type captureLRU struct {
	mu      sync.Mutex
	max     int
	entries map[captureKey]struct{}
	order   []captureKey
}

type captureKey struct {
	conversationID string
	reply          string
}

func newCaptureLRU(max int) *captureLRU {
	return &captureLRU{max: max, entries: make(map[captureKey]struct{}, max)}
}

func (l *captureLRU) contains(k1, k2 string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.entries[captureKey{k1, k2}]
	return ok
}

func (l *captureLRU) add(k1, k2 string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := captureKey{k1, k2}
	if _, ok := l.entries[k]; ok {
		return
	}
	l.entries[k] = struct{}{}
	l.order = append(l.order, k)
	if len(l.order) > l.max {
		evict := l.order[0]
		l.order = l.order[1:]
		delete(l.entries, evict)
	}
}
