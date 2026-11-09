package voice

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func baseCfg() config.VoiceConfig {
	cfg := config.DefaultConfig().Voice
	cfg.STTProvider = "deepgram"
	cfg.TTSProvider = "openai"
	cfg.STTModel = "nova-3"
	cfg.TTSModel = "gpt-4o-mini-tts"
	cfg.TTSVoice = "alloy"
	cfg.Local.STTProvider = "whisper"
	cfg.Local.TTSProvider = "piper"
	cfg.Local.TTSModel = "en_US-lessac-medium"
	cfg.Local.TTSVoice = "lessac"
	cfg.EmployeeLeg = []config.EmployeeLegConfig{
		{Employee: "echo", Leg: LegLocal, TTSVoice: "echo-voice", TTSModel: "echo-model"},
		{Employee: "bob", Leg: LegPhone},
	}
	return cfg
}

// CL4 precedence: override > employee_leg > per-leg config > (empty lets
// the provider default apply in the child). Phone leg reads top-level.
func TestResolveProfilePhoneDefaults(t *testing.T) {
	got := ResolveProfile(baseCfg(), "", "", LegPhone)
	want := Profile{SttVendor: "deepgram", SttModel: "nova-3",
		TtsVendor: "openai", TtsModel: "gpt-4o-mini-tts", TtsVoice: "alloy"}
	if got != want {
		t.Fatalf("phone defaults: got %+v want %+v", got, want)
	}
}

func TestResolveProfileLocalDefaults(t *testing.T) {
	got := ResolveProfile(baseCfg(), "", "", LegLocal)
	want := Profile{SttVendor: "whisper", SttModel: "nova-3",
		TtsVendor: "piper", TtsModel: "en_US-lessac-medium", TtsVoice: "lessac"}
	if got != want {
		t.Fatalf("local defaults: got %+v want %+v", got, want)
	}
}

// Local leg falls through to top-level config for fields it does not
// override (leaf 01 doc: empty local fields fall back to global voice.*).
func TestResolveProfileLocalFallsBackToGlobal(t *testing.T) {
	cfg := baseCfg()
	cfg.Local.TTSModel = ""
	cfg.Local.STTProvider = ""
	got := ResolveProfile(cfg, "", "", LegLocal)
	if got.TtsModel != "gpt-4o-mini-tts" {
		t.Fatalf("local empty tts_model should inherit global, got %q", got.TtsModel)
	}
	if got.SttVendor != "deepgram" {
		t.Fatalf("local empty stt_provider should inherit global, got %q", got.SttVendor)
	}
}

func TestResolveProfileEmployeeEntryWins(t *testing.T) {
	got := ResolveProfile(baseCfg(), "echo", "", LegLocal)
	if got.TtsVoice != "echo-voice" || got.TtsModel != "echo-model" {
		t.Fatalf("employee entry must win over per-leg config, got %+v", got)
	}
	if got.SttVendor != "whisper" {
		t.Fatalf("stt_vendor untouched by the employee entry, got %q", got.SttVendor)
	}
}

func TestResolveProfileOverrideWins(t *testing.T) {
	got := ResolveProfile(baseCfg(), "echo", "override-voice", LegLocal)
	if got.TtsVoice != "override-voice" {
		t.Fatalf("voice override must win, got %q", got.TtsVoice)
	}
}

// Unknown employee behaves as no employee entry at all.
func TestResolveProfileUnknownEmployee(t *testing.T) {
	got := ResolveProfile(baseCfg(), "ghost", "", LegPhone)
	want := ResolveProfile(baseCfg(), "", "", LegPhone)
	if got != want {
		t.Fatalf("ghost employee must not change resolution: got %+v want %+v", got, want)
	}
}

// EmployeeLeg list must decode from the JSON5 home-file shape and
// round-trip through LoadJSON5Config like every other voice field.
func TestEmployeeLegConfigDecodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meept.json5")
	raw := `{"voice": {"employee_leg": [{"employee": "echo", "leg": "local", "tts_voice": "v", "tts_model": "m"}]}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("load json5: %v", err)
	}
	if len(cfg.Voice.EmployeeLeg) != 1 {
		t.Fatalf("want 1 employee_leg entry, got %d", len(cfg.Voice.EmployeeLeg))
	}
	e := cfg.Voice.EmployeeLeg[0]
	if e.Employee != "echo" || e.Leg != "local" || e.TTSVoice != "v" || e.TTSModel != "m" {
		t.Fatalf("entry fields wrong: %+v", e)
	}
	// fresh_session absent on the row = tri-state nil (inherit global).
	if e.FreshSession != nil {
		t.Fatalf("absent fresh_session must stay nil, got %v", *e.FreshSession)
	}
}

// FreshSessionFor matrix (fresh-session plan leaf 02): the employee_leg
// row's tri-state FreshSession overrides the global
// voice.fresh_session_per_call; nil inherits it; no row uses the global.
func TestFreshSessionForMatrix(t *testing.T) {
	yes := true
	no := false
	tests := []struct {
		name       string
		global     bool
		setRow     string // employee to attach an explicit override to
		rowVal     *bool
		employeeID string
		want       bool
	}{
		{name: "global off, no row", global: false, employeeID: "", want: false},
		{name: "global on, no row", global: true, employeeID: "", want: true},
		{name: "global on, row absent employee", global: true, employeeID: "ghost", want: true},
		{name: "global off, row nil (echo)", global: false, employeeID: "echo", want: false},
		{name: "global on, row nil (echo)", global: true, employeeID: "echo", want: true},
		{name: "row true overrides global off", global: false, setRow: "echo", rowVal: &yes, employeeID: "echo", want: true},
		{name: "row false overrides global on", global: true, setRow: "bob", rowVal: &no, employeeID: "bob", want: false},
		{name: "row false does not leak to other employee", global: true, setRow: "bob", rowVal: &no, employeeID: "echo", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseCfg()
			c.FreshSessionPerCall = tt.global
			if tt.setRow != "" {
				for i := range c.EmployeeLeg {
					if c.EmployeeLeg[i].Employee == tt.setRow {
						c.EmployeeLeg[i].FreshSession = tt.rowVal
					}
				}
			}
			if got := FreshSessionFor(c, tt.employeeID); got != tt.want {
				t.Fatalf("FreshSessionFor(%q) = %v, want %v", tt.employeeID, got, tt.want)
			}
		})
	}
}
