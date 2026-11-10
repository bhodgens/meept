package contact

// Contact registry types (contact-channels plan, contract C1). The
// per-person record: display name, E.164 phone (normalized at write),
// nullable Signal UUID, and the ordered channel fallback policy. This
// package owns the SEAM only - it never imports telnyx or signal
// packages; channel adapters (leaves 11/12) implement ChannelAttempt
// against these types.

import "time"

// Channel is one contactable channel. The vocabulary is pinned by the
// gate tests byte-exactly: the chain policy and the telnyx/signal
// adapters all key on these strings.
type Channel string

// The four channels of the contact-channels tree (master.md C1).
const (
	ChannelTelnyxVoice Channel = "telnyx_voice"
	ChannelSignalVoice Channel = "signal_voice"
	ChannelSignalMsg   Channel = "signal_msg"
	ChannelTelnyxSMS   Channel = "telnyx_sms"
)

// Record is one person in the contact registry (C1 field names are
// contract-pinned; json tags on the wire surface the tool/report layers
// and the config-seeded records use).
type Record struct {
	ID          string    `json:"id"`                    // stable person id (pkg/id.Generate at create)
	DisplayName string    `json:"display_name"`          // human label
	Phone       string    `json:"phone"`                 // E.164, normalized at write; required
	SignalUUID  string    `json:"signal_uuid,omitempty"` // empty until observed; settable by config
	Policy      []Channel `json:"policy,omitempty"`      // ordered fallback chain; empty = employee default
	// VoiceApproved is the inbound voice-action approval bit (plan
	// 20261110-caller-authz O2: contacts.db is the system of record;
	// default FALSE = unapproved, and an unknown number never reaches a
	// record at all). Enforced downstream by the step-1c action gate;
	// the store persists it as INTEGER NOT NULL DEFAULT 0.
	VoiceApproved bool      `json:"voice_approved"`
	CreatedAt     time.Time `json:"created_at"` // RFC3339 at the store boundary
	UpdatedAt     time.Time `json:"updated_at"` // bumped on every Put
}

// ErrUnknownContact is the named sentinel for absent records (Get /
// GetByPhone / GetBySignalUUID / Delete on an unknown id). errors.Is-
// checkable by every consumer.
var ErrUnknownContact = errorString("contact: unknown contact")

// errorString is the minimal error implementation for the package-level
// sentinel; wrapping happens at call sites with %w so errors.Is still
// resolves to this value.
type errorString string

func (e errorString) Error() string { return string(e) }
