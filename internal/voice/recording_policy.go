package voice

// RecordingPolicy is the leaf-06 recording gate seam, declared early so
// the 00T gate suite compiles (leaf 06 owns the real policy wiring: the
// config keys voice.record_calls / voice.record_consent_pin and the
// carrier StartRecording call). Zero value = disabled.
type RecordingPolicy struct {
	Enabled          bool // voice.record_calls
	ConsentAnnounced bool // non-empty voice.record_consent_pin + announcement played
}

// StartOnAnswer reports whether the answer path must start a recording
// for ccid: enabled policy AND the consent announcement happened.
// Default off (zero value) never records.
func (p RecordingPolicy) StartOnAnswer(ccid string) bool {
	return p.Enabled && p.ConsentAnnounced && ccid != ""
}

// ConsentRefused reports the consent-gate refusal: recording enabled but
// the consent announcement pin never satisfied. The wiring logs it at
// Error level at startup and refuses every per-call start.
func (p RecordingPolicy) ConsentRefused() bool {
	return p.Enabled && !p.ConsentAnnounced
}
