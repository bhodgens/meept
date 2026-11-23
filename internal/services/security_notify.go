package services

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
)

// SecurityAlertEvent is the typed payload published on TopicSecurityAlert
// when [notifications].security_channel = "push". QuotaNotifier subscribes
// to that topic and delivers it through PushService. The struct deliberately
// carries NO quota fields (credential_key/escalation) so its delivery can
// never share the quota dedup key (issue #166/#167).
type SecurityAlertEvent struct {
	Message string `json:"message"`
	Source  string `json:"source"`
}

// TopicSecurityAlert is the bus topic for channel-B security alert delivery.
// Declared as a typed Topic[T] so publisher and subscriber payload agreement
// is compiler-checked (internal/comm/AGENTS.md).
var TopicSecurityAlert = bus.NewTopic[SecurityAlertEvent]("security.alert")

// SecurityNotification is the channel-neutral shape emitted for channel A.
// It mirrors the fields of http.NotificationEvent that NotificationBot reads
// (Title/Message/Type/ID/Timestamp). The daemon-side publisher seam converts
// it into a concrete *http.NotificationEvent; this package deliberately does
// NOT import internal/comm/http (that package imports services — import
// cycle). The converter stamps "security" into the Title so
// NotificationBot.mapEventToTrigger resolves TriggerSecurityAlert.
type SecurityNotification struct {
	ID        string
	Timestamp string
	Type      string
	Title     string
	Message   string
	Source    string
}

// SecurityEventPublisher is the narrow seam channel A uses to reach the
// NotificationBot's security-alert trigger. The daemon wiring supplies a
// func that constructs a real *http.NotificationEvent and publishes it on
// the EventEmitter; tests supply a recording stub. A nil func means no
// publisher (the channel is inert, never a nil deref).
type SecurityEventPublisher func(SecurityNotification)

// SecurityNotifier dispatches MCP security alerts to the configured channel.
// It is constructed once in daemon wiring and exposed to the MCP gate through
// the narrow Notify func field seam (no MCP code is modified here).
type SecurityNotifier struct {
	channel string
	b       *bus.MessageBus
	logger  *slog.Logger

	publisher SecurityEventPublisher

	// seenPush is the channel-B dedup ledger, keyed by source+"|"+message.
	// It is deliberately separate from QuotaNotifier.seen: a security alert
	// must never be suppressed by a quota event with the same key.
	seenMu   sync.Mutex
	seenPush map[string]bool
}

// NewSecurityNotifier constructs a notifier for the given channel value.
// Unknown/empty channels are normalized to "" (off) so Notify is a no-op.
func NewSecurityNotifier(channel string, b *bus.MessageBus, logger *slog.Logger) *SecurityNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	switch channel {
	case "bot", "push":
	default:
		channel = ""
	}
	return &SecurityNotifier{
		channel:  channel,
		b:        b,
		logger:   logger,
		seenPush: make(map[string]bool),
	}
}

// SetSecurityEventPublisher attaches the channel-A publisher seam. A nil
// func CLEARS the seam (the notifier stays inert on that channel rather
// than panicking on a nil call) — this is the setter nil-guard contract.
func (sn *SecurityNotifier) SetSecurityEventPublisher(p SecurityEventPublisher) {
	if sn == nil {
		return
	}
	sn.publisher = p
}

// Notify dispatches one security alert message. source is a short subsystem
// tag (e.g. "mcp") used for channel-B dedup and the push event's source.
// It returns without side effects when the channel is "" (off).
func (sn *SecurityNotifier) Notify(message, source string) {
	if sn == nil || sn.channel == "" {
		return
	}
	if strings.TrimSpace(message) == "" {
		return
	}
	if source == "" {
		source = "mcp"
	}
	switch sn.channel {
	case "bot":
		sn.notifyBot(message, source)
	case "push":
		sn.notifyPush(message, source)
	}
}

// notifyBot emits a SecurityNotification whose TITLE contains "security" so
// NotificationBot.mapEventToTrigger routes it to TriggerSecurityAlert. The
// daemon publisher seam turns it into an http.NotificationEvent of type
// "warning" (never a chat_message topic); delivery rides the existing
// emitter→bot subscription, with no extra bus topic.
func (sn *SecurityNotifier) notifyBot(message, source string) {
	if sn.publisher == nil {
		sn.logger.Debug("security alert bot channel skipped: no publisher")
		return
	}
	sn.publisher(SecurityNotification{
		ID:        id.Generate("security-"),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Type:      "warning",
		Title:     "security alert",
		Message:   message,
		Source:    source,
	})
}

// notifyPush publishes a typed security.alert bus event. QuotaNotifier
// subscribes to TopicSecurityAlert and delivers it via PushService; the
// separate seenPush map here prevents duplicate source+message alerts from
// being published repeatedly without ever consulting the quota dedup key.
func (sn *SecurityNotifier) notifyPush(message, source string) {
	if sn.b == nil {
		sn.logger.Debug("security alert push channel skipped: no bus")
		return
	}
	dedupKey := source + "|" + message
	sn.seenMu.Lock()
	if sn.seenPush[dedupKey] {
		sn.seenMu.Unlock()
		return
	}
	sn.seenPush[dedupKey] = true
	sn.seenMu.Unlock()

	bus.PublishT(sn.b, TopicSecurityAlert, source, SecurityAlertEvent{
		Message: message,
		Source:  source,
	})
}

// Channel returns the normalized channel value. Exposed so wiring and tests
// can assert the selected channel without reaching into private state.
func (sn *SecurityNotifier) Channel() string {
	if sn == nil {
		return ""
	}
	return sn.channel
}
