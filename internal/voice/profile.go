package voice

import (
	"github.com/caimlas/meept/internal/config"
)

// Profile is one call's resolved voice selection (voice local-leg plan
// CL4). An empty field means "no preference reached this far": the child
// then applies the provider default (providers.ProviderConfig model
// fallbacks), never this resolver's.
type Profile struct {
	SttVendor string
	SttModel  string
	TtsVendor string
	TtsModel  string
	TtsVoice  string
}

// Acoustics is one call's resolved per-employee acoustic control set
// (voice-peragent leaf 01). ResolveAcoustics merges the
// voice.employee_leg row over the global [voice] values (row > global,
// the FreshSession tri-state pattern extended to every noise knob); a
// row pointer that is nil inherits the global. The daemon resolves this
// once per call: the keys frame, the talk-loop construction, and the
// open frame all read it, so no consumer ever re-implements precedence.
type Acoustics struct {
	TurnConfirmMs        int
	MinTurnWords         int
	ExemptPhrases        []string
	EnergyGate           bool
	MinRMS               int
	FragmentDecayS       int
	ReplyExemptS         int
	FillerGraceMs        int
	SilencePrompt        string
	SilencePromptSeconds int
	SilenceHangupAfterS  int
	BackchannelSoften    bool
}

// employeeEntry finds the voice.employee_leg row for employeeID, or nil
// when the employee carries no per-agent routing default.
func employeeEntry(cfg config.VoiceConfig, employeeID string) *config.EmployeeLegConfig {
	if employeeID == "" {
		return nil
	}
	for i := range cfg.EmployeeLeg {
		if cfg.EmployeeLeg[i].Employee == employeeID {
			return &cfg.EmployeeLeg[i]
		}
	}
	return nil
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// pickInt resolves one tri-state int pointer over a global fallback
// (voice-peragent leaf 01): a non-nil row pointer wins — an explicit 0
// is a real value (the "gate off for this employee" escape hatch); nil
// inherits the global.
func pickInt(row *int, global int) int {
	if row != nil {
		return *row
	}
	return global
}

// pickBool resolves one tri-state bool pointer over a global fallback.
func pickBool(row *bool, global bool) bool {
	if row != nil {
		return *row
	}
	return global
}

// ResolveProfile resolves one call's voice selection per the CL4
// precedence chain: ttsVoiceOverride > voice.employee_leg[employeeID] >
// per-leg config (voice.local.* when leg is "local", top-level voice.*
// when "phone") > provider default (represented by an empty field).
// Leg resolution itself is ResolveLeg; callers resolve the leg first and
// pass it in.
func ResolveProfile(cfg config.VoiceConfig, employeeID, ttsVoiceOverride, leg string) Profile {
	entry := employeeEntry(cfg, employeeID)
	var p Profile
	// Per-leg config layer: local leg starts at voice.local.*, phone at
	// top level; an empty local field falls through to top level.
	if leg == LegLocal {
		p.SttVendor = firstNonEmpty(cfg.Local.STTProvider, cfg.STTProvider)
		p.TtsVendor = firstNonEmpty(cfg.Local.TTSProvider, cfg.TTSProvider)
		p.TtsModel = firstNonEmpty(cfg.Local.TTSModel, cfg.TTSModel)
		p.TtsVoice = firstNonEmpty(cfg.Local.TTSVoice, cfg.TTSVoice)
	} else {
		p.SttVendor = cfg.STTProvider
		p.TtsVendor = cfg.TTSProvider
		p.TtsModel = cfg.TTSModel
		p.TtsVoice = cfg.TTSVoice
	}
	// STT model has no per-leg sub-block: top level only.
	p.SttModel = cfg.STTModel
	// Employee entry layer: pins what its row carries.
	if entry != nil {
		p.TtsModel = firstNonEmpty(entry.TTSModel, p.TtsModel)
		p.TtsVoice = firstNonEmpty(entry.TTSVoice, p.TtsVoice)
	}
	// Call override layer: highest for the TTS voice.
	p.TtsVoice = firstNonEmpty(ttsVoiceOverride, p.TtsVoice)
	return p
}

// ResolveAcoustics resolves one call's per-employee acoustic controls
// (voice-peragent leaf 01): every voice.employee_leg row pointer beats
// its global [voice] counterpart; an absent (nil) pointer inherits.
// There is NO per-leg sub-block for these knobs — voice.local.* never
// carries acoustic values, so "global" is always the top-level block.
// A hand-built config that never set a field lands on that field's zero
// value, which is the documented disabled/legacy posture for every gate
// knob, so a config built without DefaultConfig behaves byte-identically
// to pre-leaf code (the pin, not the hope). Pure config read
// (FreshSessionFor shape): the daemon resolves once per call at the
// keys-frame / talk-loop construction sites.
func ResolveAcoustics(cfg config.VoiceConfig, employeeID string) Acoustics {
	entry := employeeEntry(cfg, employeeID)
	a := Acoustics{
		TurnConfirmMs:        cfg.TurnConfirmMs,
		MinTurnWords:         cfg.MinTurnWords,
		ExemptPhrases:        cfg.ExemptPhrases,
		EnergyGate:           cfg.EnergyGate,
		MinRMS:               cfg.MinRMS,
		FragmentDecayS:       cfg.FragmentDecayS,
		ReplyExemptS:         cfg.ReplyExemptS,
		FillerGraceMs:        cfg.FillerGraceMs,
		SilencePrompt:        cfg.SilencePrompt,
		SilencePromptSeconds: cfg.SilencePromptSeconds,
		SilenceHangupAfterS:  cfg.SilenceHangupAfterSeconds,
		BackchannelSoften:    cfg.BackchannelSoften != nil && *cfg.BackchannelSoften,
	}
	if entry == nil {
		return a
	}
	a.TurnConfirmMs = pickInt(entry.TurnConfirmMs, a.TurnConfirmMs)
	a.MinTurnWords = pickInt(entry.MinTurnWords, a.MinTurnWords)
	if entry.ExemptPhrases != nil {
		a.ExemptPhrases = entry.ExemptPhrases
	}
	a.EnergyGate = pickBool(entry.EnergyGate, a.EnergyGate)
	a.MinRMS = pickInt(entry.MinRMS, a.MinRMS)
	a.FragmentDecayS = pickInt(entry.FragmentDecayS, a.FragmentDecayS)
	a.ReplyExemptS = pickInt(entry.ReplyExemptS, a.ReplyExemptS)
	a.FillerGraceMs = pickInt(entry.FillerGraceMs, a.FillerGraceMs)
	if entry.SilencePrompt != "" {
		a.SilencePrompt = entry.SilencePrompt
	}
	a.SilencePromptSeconds = pickInt(entry.SilencePromptSeconds, a.SilencePromptSeconds)
	a.SilenceHangupAfterS = pickInt(entry.SilenceHangupAfterSeconds, a.SilenceHangupAfterS)
	a.BackchannelSoften = pickBool(entry.BackchannelSoften, a.BackchannelSoften)
	return a
}

// ResolveBargeInRms resolves one call's local-leg barge-in RMS threshold
// (voice-peragent leaf 01): the employee_leg row's tri-state BargeInRms
// (explicit 0 = VAD off for this employee) overrides the global
// voice.local.barge_in_rms. Open-frame construction sites only: the
// value rides the per-call C1 open frame and was always per-call — this
// is a resolution-source change, not a plumbing change.
func ResolveBargeInRms(cfg config.VoiceConfig, employeeID string) int {
	if entry := employeeEntry(cfg, employeeID); entry != nil && entry.BargeInRms != nil {
		return *entry.BargeInRms
	}
	return cfg.Local.BargeInRms
}

// FreshSessionFor resolves one call's fresh-session-per-call flag
// (fresh-session plan leaf 02): the employee_leg row's tri-state
// FreshSession overrides the global voice.fresh_session_per_call; an
// absent (nil) row value inherits the global. Pure config read — the
// daemon computes it once per call and passes the bool to
// Binding.OnCallStart, so internal/voice's binding layer stays free of
// per-call config plumbing (mirrors the ResolveProfile shape).
func FreshSessionFor(cfg config.VoiceConfig, employeeID string) bool {
	if entry := employeeEntry(cfg, employeeID); entry != nil && entry.FreshSession != nil {
		return *entry.FreshSession
	}
	return cfg.FreshSessionPerCall
}

// GuidedIntakeFor resolves one call's guided-intake gate (uptractor-ports
// leaf 05): the employee_leg row's tri-state GuidedIntake; absent (nil)
// or no row = OFF. There is deliberately NO global default: guided intake
// is the secretary employee's call behavior (owner decision 2026-11-11),
// so only an employee whose own row opts in runs the intake procedure.
// Pure config read (ResolveProfile shape) — the daemon resolves it once
// per call and drives the prompt extension + intake_save grant from it.
func GuidedIntakeFor(cfg config.VoiceConfig, employeeID string) bool {
	if entry := employeeEntry(cfg, employeeID); entry != nil && entry.GuidedIntake != nil {
		return *entry.GuidedIntake
	}
	return false
}

// BackchannelSoftenFor resolves one call's #130 backchannel-soften flag
// (schema lands here via leaf 01; leaf 02 is the consumer): the
// employee_leg row's tri-state BackchannelSoften overrides the global
// voice.backchannel_soften; both default false = today's coordinator,
// byte-identical. The link-RPC wiring (internal/daemon/voice_link_rpc.go,
// leaf 02's fence) passes the resolved value into
// LinkDeps.BackchannelSoften.
func BackchannelSoftenFor(cfg config.VoiceConfig, employeeID string) bool {
	if entry := employeeEntry(cfg, employeeID); entry != nil && entry.BackchannelSoften != nil {
		return *entry.BackchannelSoften
	}
	return cfg.BackchannelSoften != nil && *cfg.BackchannelSoften
}
