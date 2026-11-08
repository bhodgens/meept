package signal

// Wire and domain types for the signal-cli json-rpc client (contact
// channels plan, contract C3, owner leaf 10).

// CallState is a signal-cli call-event state (PR #1932).
type CallState string

// CallEventState is the state type the gate tests observe on the demux
// streams — the same value space as CallState.
type CallEventState = CallState

// CallAudioModeSocket is the preferred socket-PCM audio mode
// (signal-call-tunnel); socket path rides the supervisor config.
var CallAudioModeSocket = CallAudioMode{SocketAudio: true}

const (
	CallEventStateRingingIncoming CallState = "RINGING_INCOMING"
	CallEventStateRingingOutgoing CallState = "RINGING_OUTGOING"
	CallEventStateConnecting      CallState = "CONNECTING"
	CallEventStateConnected       CallState = "CONNECTED"
	CallEventStateReconnecting    CallState = "RECONNECTING"
	CallEventStateEnded           CallState = "ENDED"
)

// CallEvent is one signal-cli `callEvent` notification, demuxed.
// Reason is populated on ENDED (busy|timeout|declined|hangup|...).
type CallEvent struct {
	EnvelopeSourceUUID string    `json:"source_uuid"`
	CallID             string    `json:"call_id"`
	State              CallState `json:"state"`
	Reason             string    `json:"reason,omitempty"`
}

// DataMessage is one inbound signal message, demuxed from an
// `envelope` notification's dataMessage block. Attachments are local
// paths after signal-cli downloads them. GroupID is empty for 1:1.
type DataMessage struct {
	SourceUUID  string   `json:"source_uuid"`
	SourceName  string   `json:"source_name,omitempty"`
	Body        string   `json:"body"`
	Attachments []string `json:"attachments,omitempty"`
	Timestamp   int64    `json:"timestamp"`
	GroupID     string   `json:"group_id,omitempty"`
}

// CallAudioMode selects where call audio routes. SocketAudio=true is
// the preferred socket PCM mode via the signal-call-tunnel helper
// (framing authority: the Rust helper source, not the docs).
type CallAudioMode struct {
	SocketAudio bool   `json:"socketAudio"`
	SocketPath  string `json:"socketPath,omitempty"`
}

// CallInfo is one live call from `listCalls`.
type CallInfo struct {
	CallID string    `json:"call_id"`
	Peer   string    `json:"peer,omitempty"`
	State  CallState `json:"state,omitempty"`
}
