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
