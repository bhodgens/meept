package voice

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// Voice-peragent leaf 01: the per-employee acoustic controls resolution
// matrix. Every key pins the full precedence chain:
//
//	row set value > row absent (nil) > global voice.* > DefaultConfig.
//
// Pointer semantics get their own pins: an explicit 0 / false on the
// row is a REAL override (the escape hatch), while a nil pointer
// inherits - exactly the FreshSession tri-state posture.

func acouCfg() config.VoiceConfig {
	cfg := config.DefaultConfig().Voice
	cfg.TurnConfirmMs = 900
	cfg.MinTurnWords = 2
	cfg.ExemptPhrases = []string{"stop", "go ahead"}
	cfg.EnergyGate = true
	cfg.MinRMS = 1500
	cfg.FragmentDecayS = 30
	cfg.ReplyExemptS = 15
	cfg.FillerGraceMs = 2500
	cfg.SilencePrompt = "Are you still there?"
	cfg.SilencePromptSeconds = 45
	cfg.SilenceHangupAfterSeconds = 45
	cfg.Local.BargeInRms = 3000
	cfg.EmployeeLeg = []config.EmployeeLegConfig{
		{Employee: "tweaker", Leg: LegLocal,
			TurnConfirmMs: ptr(1200), MinTurnWords: ptr(4),
			ExemptPhrases:        []string{"pause"},
			EnergyGate:           bptr(false),
			MinRMS:               ptr(900),
			FragmentDecayS:       ptr(60),
			ReplyExemptS:         ptr(8),
			FillerGraceMs:        ptr(0),
			SilencePrompt:        "Hello?",
			SilencePromptSeconds: ptr(10), SilenceHangupAfterSeconds: ptr(20),
			BargeInRms:        ptr(5000),
			BackchannelSoften: bptr(true)},
		{Employee: "plain", Leg: LegLocal},
		// gate-off escape hatch: explicit 0 row over a running global.
		{Employee: "silent", Leg: LegPhone, MinTurnWords: ptr(0), ReplyExemptS: ptr(0)},
	}
	return cfg
}

func ptr(n int) *int    { return &n }
func bptr(b bool) *bool { return &b }

// Row-set values beat the global for EVERY key.
func TestResolveAcousticsRowWinsEveryKey(t *testing.T) {
	a := ResolveAcoustics(acouCfg(), "tweaker")
	if a.TurnConfirmMs != 1200 {
		t.Errorf("turn_confirm_ms = %d, want row 1200", a.TurnConfirmMs)
	}
	if a.MinTurnWords != 4 {
		t.Errorf("min_turn_words = %d, want row 4", a.MinTurnWords)
	}
	if len(a.ExemptPhrases) != 1 || a.ExemptPhrases[0] != "pause" {
		t.Errorf("exempt_phrases = %v, want the row set exactly", a.ExemptPhrases)
	}
	if a.EnergyGate != false {
		t.Errorf("energy_gate = %v, want row false (explicit false overrides global true)", a.EnergyGate)
	}
	if a.MinRMS != 900 {
		t.Errorf("min_rms = %d, want row 900", a.MinRMS)
	}
	if a.FragmentDecayS != 60 {
		t.Errorf("fragment_decay_s = %d, want row 60", a.FragmentDecayS)
	}
	if a.ReplyExemptS != 8 {
		t.Errorf("reply_exempt_s = %d, want row 8", a.ReplyExemptS)
	}
	if a.FillerGraceMs != 0 {
		t.Errorf("filler_grace_ms = %d, want row 0 (explicit immediate)", a.FillerGraceMs)
	}
	if a.SilencePrompt != "Hello?" {
		t.Errorf("silence_prompt = %q, want row", a.SilencePrompt)
	}
	if a.SilencePromptSeconds != 10 || a.SilenceHangupAfterS != 20 {
		t.Errorf("silence windows = %d/%d, want row 10/20", a.SilencePromptSeconds, a.SilenceHangupAfterS)
	}
	if a.BackchannelSoften != true {
		t.Errorf("backchannel_soften = %v, want row true", a.BackchannelSoften)
	}
}

// Row with no acoustic pointers: every value inherits the global.
func TestResolveAcousticsRowAbsentInheritsGlobal(t *testing.T) {
	a := ResolveAcoustics(acouCfg(), "plain")
	g := acouCfg()
	if a.TurnConfirmMs != g.TurnConfirmMs || a.MinTurnWords != g.MinTurnWords ||
		a.EnergyGate != g.EnergyGate || a.MinRMS != g.MinRMS ||
		a.FragmentDecayS != g.FragmentDecayS || a.ReplyExemptS != g.ReplyExemptS ||
		a.FillerGraceMs != g.FillerGraceMs || a.SilencePrompt != g.SilencePrompt ||
		a.SilencePromptSeconds != g.SilencePromptSeconds ||
		a.SilenceHangupAfterS != g.SilenceHangupAfterSeconds {
		t.Fatalf("no-pointer row must inherit every global, got %+v", a)
	}
	if len(a.ExemptPhrases) != 2 {
		t.Fatalf("exempt_phrases = %v, want the global set", a.ExemptPhrases)
	}
	if a.BackchannelSoften {
		t.Error("backchannel_soften default false global must inherit false")
	}
}

// Explicit zero/false on the row is a REAL override, not "inherit" —
// the escape-hatch semantics pointer types exist for.
func TestResolveAcousticsExplicitZeroOverrides(t *testing.T) {
	a := ResolveAcoustics(acouCfg(), "silent")
	if a.MinTurnWords != 0 {
		t.Errorf("min_turn_words = %d, want the row's explicit 0 (gate off for this employee despite global N=2)", a.MinTurnWords)
	}
	if a.ReplyExemptS != 0 {
		t.Errorf("reply_exempt_s = %d, want the row's explicit 0 (reply exemption off)", a.ReplyExemptS)
	}
}

// Unknown/empty employee id: the pure global read.
func TestResolveAcousticsNoRow(t *testing.T) {
	a := ResolveAcoustics(acouCfg(), "nobody")
	if a.MinTurnWords != 2 || a.EnergyGate != true || a.MinRMS != 1500 {
		t.Fatalf("unknown employee must read globals, got %+v", a)
	}
}

// Hand-built config without DefaultConfig: every gate knob lands on
// its disabled zero (the byte-identical legacy posture).
func TestResolveAcousticsZeroConfigIsLegacy(t *testing.T) {
	a := ResolveAcoustics(config.VoiceConfig{}, "")
	if a.TurnConfirmMs != 0 || a.MinTurnWords != 0 || a.ExemptPhrases != nil ||
		a.EnergyGate || a.MinRMS != 0 || a.FragmentDecayS != 0 || a.ReplyExemptS != 0 ||
		a.FillerGraceMs != 0 || a.SilencePrompt != "" || a.SilencePromptSeconds != 0 ||
		a.SilenceHangupAfterS != 0 || a.BackchannelSoften {
		t.Fatalf("zero config must be all-disabled, got %+v", a)
	}
}

func TestResolveBargeInRms(t *testing.T) {
	cfg := acouCfg()
	if got := ResolveBargeInRms(cfg, "tweaker"); got != 5000 {
		t.Errorf("row barge_in_rms = %d, want 5000", got)
	}
	if got := ResolveBargeInRms(cfg, "plain"); got != cfg.Local.BargeInRms {
		t.Errorf("no-pointer row = %d, want the global %d", got, cfg.Local.BargeInRms)
	}
	if got := ResolveBargeInRms(cfg, ""); got != cfg.Local.BargeInRms {
		t.Errorf("no employee = %d, want the global", got)
	}
	// Explicit 0 on the row = VAD off for this employee (tri-state).
	zero := 0
	cfg.EmployeeLeg[0].BargeInRms = &zero
	if got := ResolveBargeInRms(cfg, "tweaker"); got != 0 {
		t.Errorf("explicit-0 row = %d, want 0 (VAD off override)", got)
	}
}

func TestBackchannelSoftenFor(t *testing.T) {
	cfg := acouCfg()
	if !BackchannelSoftenFor(cfg, "tweaker") {
		t.Error("row true must win")
	}
	if BackchannelSoftenFor(cfg, "plain") {
		t.Error("no-pointer row inherits the global false")
	}
	tr := true
	cfg.BackchannelSoften = &tr // global on
	if BackchannelSoftenFor(cfg, "plain") != true {
		t.Error("global true must bind for a row-less read")
	}
	fl := false
	cfg.EmployeeLeg[1].BackchannelSoften = &fl
	if BackchannelSoftenFor(cfg, "plain") {
		t.Error("explicit-false row must override a true global")
	}
}

// FreshSession/GuidedIntake pins stay true while the same rows carry
// acoustic pointers (the pre-existing helpers must not drift).
func TestExistingRowHelpersUnaffectedByAcousticRows(t *testing.T) {
	cfg := acouCfg()
	if FreshSessionFor(cfg, "tweaker") != cfg.FreshSessionPerCall {
		t.Error("FreshSessionFor must stay the global read on a pointer-less row")
	}
	if GuidedIntakeFor(cfg, "tweaker") {
		t.Error("GuidedIntakeFor has no global default: absent row pointer = off")
	}
}
