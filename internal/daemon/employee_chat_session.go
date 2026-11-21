package daemon

import (
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/session"
)

// ensureEmployeeChatSession makes sure the existing session store has a row
// for the standing employee chat session before Execute. A nil store is a
// no-op: the routing-funnel gate constructs an executor with no store and
// only checks the id passed to RunOnce. A store error is returned to the
// caller, which logs it and still runs the turn — a missing row must not
// panic and must not fail the turn.
func ensureEmployeeChatSession(store session.Store, employeeID string) error {
	if store == nil || employeeID == "" {
		return nil
	}
	sessionID := employee.EmployeeChatSessionID(employeeID)
	existing := store.Get(sessionID)
	if existing == nil {
		var err error
		existing, err = store.Ensure(sessionID, "employee "+employeeID)
		if err != nil {
			return err
		}
	}
	if existing != nil && existing.EmployeeID != employeeID {
		return store.SetEmployeeID(sessionID, employeeID)
	}
	return nil
}

// persistEmployeeExchange writes the user turn and assistant reply through
// the same session.Store.SaveMessages writer the chat handler uses
// (ChatHandler.persistExchange → messageSaver.SaveMessages, and the daemon
// wires that saver to the session store). RunOnce only updates the
// in-memory conversation on a throwaway clone, so this is the durable
// write. A nil store or a write error is logged and never fails the turn.
func persistEmployeeExchange(store session.Store, sessionID, employeeID, userMsg, reply string, logger *slog.Logger) {
	if store == nil || sessionID == "" {
		return
	}
	if userMsg == "" && reply == "" {
		return
	}

	now := time.Now().UTC()
	msgs := make([]session.Message, 0, 2)
	if userMsg != "" {
		msgs = append(msgs, session.Message{
			SessionID: sessionID,
			Role:      "user",
			Content:   userMsg,
			Timestamp: now,
			EntryType: "message",
			BranchID:  "main",
		})
	}
	if reply != "" {
		msgs = append(msgs, session.Message{
			SessionID: sessionID,
			Role:      "assistant",
			Content:   reply,
			Timestamp: now,
			EntryType: "message",
			BranchID:  "main",
			AgentID:   employeeID,
		})
	}
	if err := store.SaveMessages(sessionID, msgs); err != nil && logger != nil {
		logger.Warn("failed to persist employee chat exchange",
			"session_id", sessionID,
			"employee_id", employeeID,
			"error", err)
	}
}

func warnEmployeeSession(logger *slog.Logger, msg string, args ...any) {
	if logger == nil {
		return
	}
	logger.Warn(msg, args...)
}
