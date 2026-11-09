package signalleg

// Inbound Signal message routing (leaf 11b T3): an inbound envelope is
// deduped on (sourceUuid, timestamp), bound to the one
// (number="signal:"+sourceUuid, employee) session through the Binding
// seam, and the body is injected as prompt material - the same session
// machinery the voice legs use, so conversation context carries across
// voice and text. A message is NOT a call: no call rows are created; the
// session-bind path only. Attachments surface by local path only (never
// contents).

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/signal"
)

// signalSession is the session-handle shape the inbound binder resolves
// (mirrors voice.SessionHandle; kept local so the routing seam stays
// substitutable in tests). Both identity keys ride it (session_id and
// conversation_id are distinct).
type signalSession struct {
	ID             string
	ConversationID string
}

// inboundBinding is the narrow Binding seam RouteInbound consumes. The
// daemon adapter implements it over the voice Binding's session
// resolution (resolve-or-create the named session, bind employee) - the
// same machinery OnCallStart drives, minus the call row.
type inboundBinding interface {
	// ResolveSession resolves-or-creates the ONE session per
	// (number, employee) - number is "signal:<uuid>" shaped.
	ResolveSession(number, employeeID string) (signalSession, error)
	// BindMessage injects one message body into the bound session as
	// prompt material (the TurnRunner path other legs feed).
	BindMessage(ctx context.Context, number, employeeID, prompt string) error
}

// inboundRouter carries the dedupe state for RouteInbound. The mutex
// guards only the in-memory dedupe map (no I/O under it - repo mutexio
// rule).
type inboundRouter struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newInboundRouter() *inboundRouter {
	return &inboundRouter{seen: make(map[string]struct{})}
}

// dedupeKey is the envelope identity: (sourceUuid, timestamp).
func dedupeKey(msg signal.DataMessage) string {
	return msg.SourceUUID + "@" + strconv.FormatInt(msg.Timestamp, 10)
}

// dedupe reports (and records) whether this envelope is new.
func (r *inboundRouter) dedupe(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.seen[key]; ok {
		return false
	}
	r.seen[key] = struct{}{}
	return true
}

// RouteInbound routes one inbound Signal message: dedupe -> session bind
// (number="signal:"+sourceUuid) -> body injected as prompt material.
// Failures are logged, never fatal (an inbound message loss is logged,
// not a daemon error).
func (l *Leg) RouteInbound(msg signal.DataMessage) {
	if l == nil || l.deps.Binding == nil {
		return
	}
	key := dedupeKey(msg)
	if !l.inbound.dedupe(key) {
		l.log.Debug("signal inbound: duplicate envelope dropped")
		return
	}

	number := "signal:" + msg.SourceUUID
	employeeID := l.signalEmployeeID()
	prompt := inboundPrompt(msg)
	if prompt == "" {
		return
	}

	binder, ok := l.deps.Binding.(inboundBinding)
	if !ok {
		l.log.Error("signal inbound: binding seam does not implement inboundBinding; message dropped")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := binder.BindMessage(ctx, number, employeeID, prompt); err != nil {
		l.log.Error("signal inbound: bind/inject failed", "error", err)
	}
}

// signalEmployeeID resolves the employee the inbound message is for
// (single-employee posture, mirroring voice.default_employee; leaf 11a's
// Deps carries whichever field name it pins - both are consulted).
func (l *Leg) signalEmployeeID() string {
	if l.deps.EmployeeID != "" {
		return l.deps.EmployeeID
	}
	return l.deps.DefaultEmployeeID
}

// inboundPrompt shapes the prompt material for one inbound message: the
// body verbatim; attachment paths appended as context lines (paths only,
// never contents).
func inboundPrompt(msg signal.DataMessage) string {
	var b strings.Builder
	b.WriteString(msg.Body)
	for _, path := range msg.Attachments {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[attachment: " + path + "]")
	}
	return b.String()
}
