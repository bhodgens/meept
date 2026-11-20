package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/session"
)

// RegisterDispatchHandlers exposes dispatch-log query endpoints on the RPC
// server. If store is nil the function is a no-op (the daemon may run without
// a metrics store).
func RegisterDispatchHandlers(server *Server, store *metrics.Store) {
	if store == nil {
		return
	}

	// session.dispatch_trace -- routing decisions for a specific session.
	//
	// C5 (02-routing/02): dual-key, thread-aware lookup. Rows are recorded
	// under the session's CONVERSATION id, but GUI/TUI clients hold the
	// PRIMARY session id (or a thread-scoped conversation id). When a
	// session store is wired (SetDispatchSessionStore), the incoming id is
	// resolved to the owning session and every identity key that could
	// hold rows is queried and merged. Without the seam the lookup stays
	// session_id-only (legacy behavior). An unknown id resolves to nothing
	// and returns an empty list -- never another session's rows.
	server.RegisterHandler("session.dispatch_trace", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
		}
		if p.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}
		if p.Limit <= 0 {
			p.Limit = 50
		}
		keys := server.dispatchTraceKeys(p.SessionID)
		entries, err := queryDispatchTrace(store, keys, p.Limit)
		if err != nil {
			return nil, fmt.Errorf("session.dispatch_trace failed: %w", err)
		}
		return map[string]any{"entries": entries, "count": len(entries)}, nil
	})
}

// SetDispatchSessionStore wires the session store used by
// session.dispatch_trace for C5 dual-key resolution (primary session id or
// thread-scoped conversation id -> owning conversation). Nil store is
// ignored (setter convention): resolution is then skipped and the handler
// keeps its legacy session_id-only behavior.
func (s *Server) SetDispatchSessionStore(store chatSubmitSessionStore) {
	if store == nil {
		return
	}
	s.dispatchTraceMu.Lock()
	defer s.dispatchTraceMu.Unlock()
	s.dispatchSessionStore = store
}

// dispatchSessionStore returns the wired session store, or nil.
func (s *Server) getDispatchSessionStore() chatSubmitSessionStore {
	s.dispatchTraceMu.RLock()
	defer s.dispatchTraceMu.RUnlock()
	return s.dispatchSessionStore
}

// dispatchTraceKeys maps an incoming trace id to the set of dispatch_log
// session_id keys that may hold its rows. With no store wired (or an
// unresolvable id) the raw id is the only key -- legacy behavior, and the
// fail-safe direction for unknown ids (empty result, not a cross-return).
// When the id resolves to a session, both identities (primary +
// conversation) are keys: writers historically used either.
func (s *Server) dispatchTraceKeys(id string) []string {
	store := s.getDispatchSessionStore()
	if store == nil {
		return []string{id}
	}
	sess := session.ResolveThreadConversationID(id, store.GetByConversationID, store.Get)
	if sess == nil {
		return []string{id}
	}
	keys := make([]string, 0, 3)
	keys = append(keys, sess.ConversationID)
	if sess.ID != sess.ConversationID {
		keys = append(keys, sess.ID)
	}
	// A thread-scoped query may also have rows keyed under the thread id
	// itself (per-thread dispatches); include it when distinct.
	if id != sess.ConversationID && id != sess.ID {
		keys = append(keys, id)
	}
	return keys
}

// queryDispatchTrace fans the key set over the store, merges, re-sorts
// newest-first by arrival order per key (store rows come back most recent
// first), de-duplicates on (session_id, task_id, turn_no), and applies the
// shared limit. Every wire row is guaranteed a non-empty turn_id via the
// deterministic fallback in dispatchTraceRow.
func queryDispatchTrace(store *metrics.Store, keys []string, limit int) ([]map[string]any, error) {
	var merged []metrics.DispatchEntry
	seen := make(map[string]bool)
	for _, key := range keys {
		entries, err := store.QueryDispatchLogBySession(key, limit)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			dedup := e.SessionID + "\x00" + e.TaskID + "\x00" + strconv.Itoa(e.TurnNo)
			if seen[dedup] {
				continue
			}
			seen[dedup] = true
			merged = append(merged, e)
		}
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	rows := make([]map[string]any, 0, len(merged))
	for _, e := range merged {
		rows = append(rows, dispatchTraceRow(e))
	}
	return rows, nil
}

// dispatchTraceRow maps a DispatchEntry to its wire shape, pinning the C5
// row contract: turn_id is NEVER empty. Rows recorded before the turn_id
// column (or by writers without a turn in hand) get a deterministic
// fallback join key: task_id when present, else session id + turn number.
// Both are stable across reads, so a client can re-join the same row.
// The base wire shape is the DispatchEntry JSON (preserving omitempty
// semantics for existing clients); only turn_id is overridden.
func dispatchTraceRow(e metrics.DispatchEntry) map[string]any {
	turnID := e.TurnID
	if turnID == "" {
		switch {
		case e.TaskID != "":
			turnID = e.TaskID
		default:
			turnID = e.SessionID + ":" + strconv.Itoa(e.TurnNo)
		}
	}
	raw, err := json.Marshal(e)
	if err != nil {
		// DispatchEntry is plain scalars; marshal cannot realistically
		// fail. Fall back to a minimal explicit row rather than dropping
		// the evidence entry.
		return map[string]any{
			"session_id":    e.SessionID,
			"turn_id":       turnID,
			"input_summary": e.InputSummary,
			"agent_id":      e.AgentID,
			"task_id":       e.TaskID,
			"turn_no":       e.TurnNo,
			"outcome":       e.Outcome,
		}
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		return map[string]any{
			"session_id":    e.SessionID,
			"turn_id":       turnID,
			"input_summary": e.InputSummary,
		}
	}
	row["turn_id"] = turnID
	return row
}
