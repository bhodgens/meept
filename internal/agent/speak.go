package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// SpeakKind classifies how an agent run's final text should be delivered
// (leaf 11: harness-routed speak, Q11=A). The model always ends a turn the
// same way; the HARNESS decides bubble vs notify vs parent report.
type SpeakKind int

const (
	// SpeakSession marks a session-attached run: the existing chat bubble
	// path (RunOnce's return value delivered by the chat transport) already
	// surfaces the text, so Deliver is a no-op in production for this kind.
	SpeakSession SpeakKind = iota
	// SpeakNotify marks a session-detached run (e.g. an employee goal
	// round): there is no watching chat, so the final text is pushed as a
	// notification on the employee.notify bus topic.
	SpeakNotify
	// SpeakParent marks an isolated child run: it MUST NOT notify or bubble.
	// Only a parent-facing report is written (never a user surface).
	SpeakParent
)

// String returns a human-readable name for the kind (used in logs).
func (k SpeakKind) String() string {
	switch k {
	case SpeakSession:
		return "session"
	case SpeakNotify:
		return "notify"
	case SpeakParent:
		return "parent"
	default:
		return "unknown"
	}
}

// ClassifyRun resolves the SpeakKind for a run from its harness attachment
// bits. Matrix (C3/C4):
//
//	sessionAttached  isolatedChild  kind
//	true             false          SpeakSession  (chat bubble path)
//	true             true           SpeakParent   (C4 trumps: fail-closed)
//	false            true           SpeakParent   (isolated child)
//	false            false          SpeakNotify   (detached, not isolated)
//
// C4 is fail-closed: an isolated child can never reach the user, even when
// the attachment bits are contradictory (attached + isolated).
func ClassifyRun(sessionAttached, isolatedChild bool) SpeakKind {
	if isolatedChild {
		return SpeakParent
	}
	if sessionAttached {
		return SpeakSession
	}
	return SpeakNotify
}

// ErrIsolatedSpeak is the sentinel wrapped by every refusal to let an
// isolated child speak to the user. Match with errors.Is.
var ErrIsolatedSpeak = errors.New("isolated speak denied")

// SpeakTopicNotify is the bus topic carrying detached-run notifications.
// The WS bridge forwards it as the generic "event" type — NEVER as
// chat_message (AGENTS.md invariant: only chat_message/chat.response
// produce type chat_message).
const SpeakTopicNotify = "employee.notify"

// NotifyPayload is the JSON body published on SpeakTopicNotify.
type NotifyPayload struct {
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
}

// SpeakPublisher is the injection seam SpeakRouter routes through. Tests
// inject a fake that records (kind, sessionID, conversationID, text);
// production injects BusSpeakPublisher. kind lets a single publisher route
// per-kind (e.g. publish employee.notify only for SpeakNotify).
type SpeakPublisher func(kind SpeakKind, text, sessionID, conversationID string) error

// SpeakRouter routes a run's final text to the right delivery surface.
// The bus publisher is injected (C3) so the router never touches the bus
// directly and stays trivially fake-able. A nil publisher is safe: Deliver
// logs and returns nil (no-op) rather than panicking.
type SpeakRouter struct {
	pub    SpeakPublisher
	logger *slog.Logger
}

// NewSpeakRouter builds a router over the given publisher. A nil publisher
// is allowed (typed-nil interface guard pattern): Deliver degrades to a
// debug-logged no-op.
func NewSpeakRouter(pub SpeakPublisher) *SpeakRouter {
	return &SpeakRouter{pub: pub, logger: slog.Default()}
}

// SetPublisher replaces the injected publisher. Typed-nil guard: a nil fn
// is ignored so a wiring order bug cannot silently strip a live publisher.
func (r *SpeakRouter) SetPublisher(fn SpeakPublisher) {
	if fn != nil {
		r.pub = fn
	}
}

// Deliver sends text according to kind (C3 contract):
//
//   - Empty/whitespace text is a no-op returning nil (a detached run with
//     nothing to say must not notify).
//   - SpeakNotify for an isolated child is silently dropped with a debug
//     log and nil error: an isolated child MUST NOT notify (C4).
//   - SpeakSession is routed to the publisher unchanged — in production the
//     bus publisher no-ops this kind because the chat bubble path (the
//     RunOnce return value) is unchanged; the publisher seam keeps the
//     router testable.
//   - SpeakParent is routed to the publisher, which writes the parent
//     report only (never a user surface).
//
// Errors from the publisher are returned wrapped; callers treat delivery
// as best-effort (log, do not fail the turn).
func (r *SpeakRouter) Deliver(ctx context.Context, kind SpeakKind, text, sessionID, conversationID string) error {
	if strings.TrimSpace(text) == "" {
		// Empty text => no-op. A detached run that produced no final text
		// must not emit an empty notification.
		return nil
	}
	if r == nil {
		return nil
	}
	if r.logger != nil {
		r.logger.Debug("speak deliver",
			"kind", kind.String(),
			"session_id", sessionID,
			"conversation_id", conversationID,
			"text_len", len(text))
	}
	if r.pub == nil {
		if r.logger != nil {
			r.logger.Debug("speak deliver skipped: no publisher configured",
				"kind", kind.String())
		}
		return nil
	}
	return r.pub(kind, text, sessionID, conversationID)
}

// DeliverIsolatedNotify silently drops a SpeakNotify delivery for an
// isolated child (C4). Split from Deliver so the drop site is explicit and
// documented rather than inferred from kind+flags at the call site.
func (r *SpeakRouter) DeliverIsolatedNotify(ctx context.Context, text, sessionID, conversationID string) error {
	if r != nil && r.logger != nil {
		r.logger.Debug("speak: isolated child notify silently dropped (C4)",
			"session_id", sessionID,
			"conversation_id", conversationID,
			"text_len", len(text))
	}
	return nil
}

// ReplyFuncSetter is implemented by tools that accept a per-turn reply
// carrier (e.g. builtin reply_to_user). The agent loop injects a carrier
// closure into every registry tool implementing this interface at turn
// start, so the tool delivers through the loop's classified SpeakRouter
// without the tool package importing the router wiring. Satisfied
// structurally to avoid an import cycle (builtin imports agent).
type ReplyFuncSetter interface {
	SetReplyFunc(fn func(text string) error)
}

// ReplyCarrier returns the closure the loop injects into ReplyFuncSetter
// tools at turn start. The carrier classifies the CURRENT run from the
// captured attachment bits and routes through the router:
//
//   - attached, not isolated: no-op success — the bubble path already
//     delivers mid-turn text to a watching chat session.
//   - detached, not isolated: Deliver(SpeakNotify) — the real mid-turn
//     notify seam for employee goal rounds.
//   - isolated child: error "isolated child cannot speak to user" (C4).
//
// Called on the tool-execution goroutine; router/publisher safety is their
// own concern (both are concurrency-safe or nil-guarded).
func (r *SpeakRouter) ReplyCarrier(sessionAttached, isolatedChild bool, sessionID, conversationID string) func(text string) error {
	kind := ClassifyRun(sessionAttached, isolatedChild)
	return func(text string) error {
		switch kind {
		case SpeakSession:
			// Watching chat session: the bubble already carries text to
			// the user; acknowledging is correct and silent.
			return nil
		case SpeakParent:
			return fmt.Errorf("isolated child cannot speak to user: %w", ErrIsolatedSpeak)
		default: // SpeakNotify
			return r.Deliver(context.Background(), kind, text, sessionID, conversationID)
		}
	}
}

// BusSpeakPublisher adapts a MessageBus into a SpeakPublisher. Routing:
//
//   - SpeakSession: no-op nil — the chat bubble path (RunOnce's return
//     value) already delivers attached text; publishing here would
//     duplicate every bubble as a second chat_message.
//   - SpeakNotify: publishes SpeakTopicNotify with session_id,
//     conversation_id, text. The WS bridge forwards this topic as the
//     generic "event" type, never chat_message.
//   - SpeakParent: debug-logged no-op — parent reports have no user
//     surface (C4); a dedicated parent-report channel lands later.
//
// A nil bus yields a no-op publisher (safe for tests and disabled builds).
func BusSpeakPublisher(b *bus.MessageBus, source string) SpeakPublisher {
	return func(kind SpeakKind, text, sessionID, conversationID string) error {
		if kind == SpeakParent {
			slog.Debug("speak: parent report (no user surface)",
				"session_id", sessionID, "conversation_id", conversationID)
			return nil
		}
		if kind == SpeakSession {
			// Bubble path unchanged: attached text is delivered by the
			// chat transport, not the bus.
			return nil
		}
		if b == nil {
			return nil
		}
		payload := NotifyPayload{
			SessionID:      sessionID,
			ConversationID: conversationID,
			Text:           text,
		}
		msg, err := models.NewBusMessage(models.MessageTypeEvent, source, payload)
		if err != nil {
			return err
		}
		b.Publish(SpeakTopicNotify, msg)
		return nil
	}
}

// wireReplyCarriers injects the speak reply carrier into every registry
// tool implementing ReplyFuncSetter (loop side of the seam; lives here to
// keep loop.go edits to the one Deliver block per the leaf scope). Tools
// without a router configured get a nil-safe carrier that reports the tool
// as unavailable at call time.
func (l *AgentLoop) wireReplyCarriers(conversationID string) {
	router, attached, isolated := l.speakContextSnapshot()
	if router == nil || l.registry == nil {
		return
	}
	carrier := router.ReplyCarrier(attached, isolated, l.sessionID, conversationID)
	for _, tool := range l.registry.List() {
		if setter, ok := tool.(ReplyFuncSetter); ok {
			setter.SetReplyFunc(carrier)
		}
	}
}
