package voice

// C5 lane pools (plan 20261113-filler-clips leaf 05). The precedence
// table IS the C5 contract: row list > row scalar > global list > global
// scalar > shipped default, with the zero-config rule (internal/voice/
// AGENTS.md): a config that never sets a new key resolves every EXISTING
// lane to today's effective single-line pools, byte-identically.

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func laneCfg(mut func(v *config.VoiceConfig)) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Voice.EmployeeLeg = nil // rows only via mut, unless mut sets them
	if mut != nil {
		mut(&cfg.Voice)
	}
	return cfg
}

func laneTexts(lines []LaneLine, lane Lane) []string {
	var out []string
	for _, ll := range lines {
		if ll.Lane == lane {
			out = append(out, ll.Text)
		}
	}
	return out
}

func equalTexts(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Zero-config: DefaultConfig with no new keys set lands the existing
// lanes on today's effective pools (greeting = "" -> filler[0]; silence
// = the scalar; fragment = scalar-empty -> lane empty) and only the new
// farewell lane on its frozen default.
func TestVoiceLanesZeroConfigByteIdentical(t *testing.T) {
	got := VoiceLanes(laneCfg(nil), "")

	if v := laneTexts(got, LaneFiller); !equalTexts(v, []string{"Hold on, let me check."}) {
		t.Errorf("zero-config filler lane = %v, want the shipped one-line pool", v)
	}
	if v := laneTexts(got, LaneGreeting); !equalTexts(v, []string{"Hold on, let me check."}) {
		t.Errorf("zero-config greeting lane = %v, want filler[0] (the greeting-empty-rules-to-first-filler rule)", v)
	}
	if v := laneTexts(got, LaneSilence); !equalTexts(v, []string{"Are you still there?"}) {
		t.Errorf("zero-config silence lane = %v, want the silence_prompt scalar (the empty _lines list must keep the scalar binding)", v)
	}
	if v := laneTexts(got, LaneFragment); len(v) != 0 {
		t.Errorf("zero-config fragment lane = %v, want empty (scalar unset -> no line, legacy behavior)", v)
	}
	if v := laneTexts(got, LaneFarewell); !equalTexts(v, []string{"Goodbye!", "Bye!", "Talk soon."}) {
		t.Errorf("zero-config farewell lane = %v, want the frozen C5 default pool", v)
	}
}

// Greeting empty + no filler lines: lane stays empty (never hardcoded).
func TestVoiceLanesGreetingFallsToEmptyWithNoFiller(t *testing.T) {
	cfg := laneCfg(func(v *config.VoiceConfig) { v.Filler = nil })
	got := VoiceLanes(cfg, "")
	if v := laneTexts(got, LaneGreeting); len(v) != 0 {
		t.Errorf("greeting lane = %v, want empty when greeting and filler are both unset", v)
	}
}

func TestVoiceLanesExplicitGreetingWins(t *testing.T) {
	cfg := laneCfg(func(v *config.VoiceConfig) { v.GreetingLine = "Hello!" })
	got := VoiceLanes(cfg, "")
	if v := laneTexts(got, LaneGreeting); !equalTexts(v, []string{"Hello!"}) {
		t.Errorf("greeting lane = %v, want the explicit greeting scalar", v)
	}
}

// The C5 precedence ladder for the silence lane, all five rungs.
func TestVoiceLanesSilencePrecedenceLadder(t *testing.T) {
	const emp = "op"
	row := func(mut func(e *config.EmployeeLegConfig)) []config.EmployeeLegConfig {
		e := config.EmployeeLegConfig{Employee: emp}
		mut(&e)
		return []config.EmployeeLegConfig{e}
	}
	cases := []struct {
		name string
		mut  func(v *config.VoiceConfig)
		want []string
	}{
		{
			name: "default scalar (no new keys anywhere)",
			mut:  func(v *config.VoiceConfig) {},
			want: []string{"Are you still there?"},
		},
		{
			name: "global list beats global scalar",
			mut: func(v *config.VoiceConfig) {
				v.SilencePromptLines = []string{"Still there?", "You there?"}
			},
			want: []string{"Still there?", "You there?"},
		},
		{
			name: "row scalar beats global list",
			mut: func(v *config.VoiceConfig) {
				v.SilencePromptLines = []string{"Still there?"}
				v.EmployeeLeg = row(func(e *config.EmployeeLegConfig) { e.SilencePrompt = "Row nudge" })
			},
			want: []string{"Row nudge"},
		},
		{
			name: "row list beats row scalar",
			mut: func(v *config.VoiceConfig) {
				v.SilencePromptLines = []string{"Still there?"}
				v.EmployeeLeg = row(func(e *config.EmployeeLegConfig) {
					e.SilencePrompt = "Row nudge"
					e.SilencePromptLines = []string{"Row one", "Row two"}
				})
			},
			want: []string{"Row one", "Row two"},
		},
		{
			name: "empty row list + empty row scalar inherit the global list (no rung masks)",
			mut: func(v *config.VoiceConfig) {
				v.SilencePromptLines = []string{"Still there?"}
				v.EmployeeLeg = row(func(e *config.EmployeeLegConfig) {
					e.SilencePromptLines = []string{}
					e.SilencePrompt = ""
				})
			},
			want: []string{"Still there?"},
		},
		{
			name: "whitespace-only entries are dropped from pools",
			mut: func(v *config.VoiceConfig) {
				v.SilencePromptLines = []string{"  ", "Real line", ""}
			},
			want: []string{"Real line"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := laneCfg(tc.mut)
			got := laneTexts(VoiceLanes(cfg, emp), LaneSilence)
			if !equalTexts(got, tc.want) {
				t.Errorf("silence lane = %v, want %v", got, tc.want)
			}
		})
	}
}

// Fragment lane: list > scalar ladder (no row scalar rung exists for
// fragment_clarify) + the row list override.
func TestVoiceLanesFragmentPrecedence(t *testing.T) {
	const emp = "op"
	cfg := laneCfg(func(v *config.VoiceConfig) {
		v.FragmentClarify = "Could you finish that thought?"
		v.EmployeeLeg = []config.EmployeeLegConfig{
			{Employee: emp, FragmentClarifyLines: []string{"Row frag one", "Row frag two"}},
		}
	})
	if got := laneTexts(VoiceLanes(cfg, emp), LaneFragment); !equalTexts(got, []string{"Row frag one", "Row frag two"}) {
		t.Errorf("fragment lane with row list = %v, want the row pool (row list beats the global scalar)", got)
	}
	// A different employee inherits the global scalar.
	if got := laneTexts(VoiceLanes(cfg, "other"), LaneFragment); !equalTexts(got, []string{"Could you finish that thought?"}) {
		t.Errorf("fragment lane for an unrowed employee = %v, want the global scalar", got)
	}
	// Global list beats the global scalar for unrowed employees.
	cfg2 := laneCfg(func(v *config.VoiceConfig) {
		v.FragmentClarify = "scalar"
		v.FragmentClarifyLines = []string{"list a", "list b"}
	})
	if got := laneTexts(VoiceLanes(cfg2, ""), LaneFragment); !equalTexts(got, []string{"list a", "list b"}) {
		t.Errorf("fragment lane = %v, want the global list over the global scalar", got)
	}
}

// Farewell lane: row list > global list > frozen default (no scalars
// exist for this lane).
func TestVoiceLanesFarewellPrecedence(t *testing.T) {
	const emp = "op"
	cfg := laneCfg(func(v *config.VoiceConfig) {
		v.Farewell = []string{"See ya!"}
		v.EmployeeLeg = []config.EmployeeLegConfig{
			{Employee: emp, Farewell: []string{"Row bye", "Later"}},
		}
	})
	if got := laneTexts(VoiceLanes(cfg, emp), LaneFarewell); !equalTexts(got, []string{"Row bye", "Later"}) {
		t.Errorf("farewell lane = %v, want the row pool", got)
	}
	if got := laneTexts(VoiceLanes(cfg, "other"), LaneFarewell); !equalTexts(got, []string{"See ya!"}) {
		t.Errorf("farewell lane for an unrowed employee = %v, want the global pool", got)
	}
	// Empty global list inherits the frozen default (the pool never
	// ships empty; an operator CANNOT silence the lane with []).
	cfg2 := laneCfg(func(v *config.VoiceConfig) { v.Farewell = nil })
	if got := laneTexts(VoiceLanes(cfg2, ""), LaneFarewell); !equalTexts(got, config.DefaultVoiceFarewellLines()) {
		t.Errorf("farewell lane with unset pool = %v, want the C5 default", got)
	}
}

// Dedup identity: (lane, text). The same text across two lanes stays one
// line per lane; repeats WITHIN a lane collapse.
func TestVoiceLanesDedupWithinLane(t *testing.T) {
	cfg := laneCfg(func(v *config.VoiceConfig) {
		v.Filler = []string{"One", "Two", "One"}
		v.SilencePromptLines = []string{"One", "One"}
	})
	got := VoiceLanes(cfg, "")
	if v := laneTexts(got, LaneFiller); !equalTexts(v, []string{"One", "Two"}) {
		t.Errorf("filler lane = %v, want within-lane dedup preserving order", v)
	}
	if v := laneTexts(got, LaneSilence); !equalTexts(v, []string{"One"}) {
		t.Errorf("silence lane = %v, want within-lane dedup", v)
	}
	// "One" rides every lane it appears in: greeting (filler[0] rule),
	// filler, and silence — dedup identity is (lane, text), one LaneLine
	// per pair.
	count := 0
	for _, ll := range got {
		if ll.Text == "One" {
			count++
		}
	}
	if count != 3 {
		t.Errorf("\"One\" appears %d times across lanes, want 3 (greeting, filler, silence — one LaneLine per (lane, text))", count)
	}
	// Canonical lane order: greeting <= filler <= silence <= fragment <= farewell.
	order := map[Lane]int{LaneGreeting: 0, LaneFiller: 1, LaneSilence: 2, LaneFragment: 3, LaneFarewell: 4}
	last := -1
	for _, ll := range got {
		if order[ll.Lane] < last {
			t.Fatalf("VoiceLanes emitted out of canonical lane order: %v", got)
		}
		last = order[ll.Lane]
	}
}

// Every employee with no row resolves exactly like the global config
// (employeeEntry convention: unknown id = no row).
func TestVoiceLanesUnknownEmployeeIsGlobal(t *testing.T) {
	cfg := laneCfg(func(v *config.VoiceConfig) {
		v.EmployeeLeg = []config.EmployeeLegConfig{{Employee: "someone"}}
	})
	gotEmp := VoiceLanes(cfg, "nope")
	gotGlob := VoiceLanes(cfg, "")
	if len(gotEmp) != len(gotGlob) {
		t.Fatalf("unknown-employee lanes = %d lines, global = %d", len(gotEmp), len(gotGlob))
	}
	for i := range gotEmp {
		if gotEmp[i] != gotGlob[i] {
			t.Errorf("lane line %d differs: %+v vs %+v", i, gotEmp[i], gotGlob[i])
		}
	}
}

func TestVoiceLanesNilConfig(t *testing.T) {
	if got := VoiceLanes(nil, ""); got != nil {
		t.Errorf("VoiceLanes(nil) = %v, want nil", got)
	}
}

// PickLine: round-robin within one lane with its own cursor, the
// fillerIdx semantics (talkloop.go fillerLine: idx = cursor%len, advance).
func TestPickLineRoundRobin(t *testing.T) {
	lanes := VoiceLanes(laneCfg(func(v *config.VoiceConfig) {
		v.Filler = []string{"a", "b", "c"}
	}), "")

	var cursor int
	var seq []string
	for i := 0; i < 7; i++ {
		seq = append(seq, PickLine(lanes, LaneFiller, &cursor))
	}
	want := []string{"a", "b", "c", "a", "b", "c", "a"}
	if !equalTexts(seq, want) {
		t.Errorf("PickLine round-robin = %v, want %v", seq, want)
	}

	// Per-lane cursors are independent.
	var silenceCursor int
	if got := PickLine(lanes, LaneSilence, &silenceCursor); got != "Are you still there?" {
		t.Errorf("PickLine(silence) = %q, want the one-line pool's line", got)
	}
	if got := PickLine(lanes, LaneSilence, &silenceCursor); got != "Are you still there?" {
		t.Errorf("PickLine(silence) wraparound = %q, want the same line", got)
	}

	// nil cursor = first line, no state (one-shot consumers).
	if got := PickLine(lanes, LaneFiller, nil); got != "a" {
		t.Errorf("PickLine(nil cursor) = %q, want the first line", got)
	}
	// Unknown/empty lane = "" (caller keeps its own fallback chain).
	if got := PickLine(lanes, LaneFragment, &cursor); got != "" {
		t.Errorf("PickLine(empty lane) = %q, want \"\"", got)
	}
	if got := PickLine(nil, LaneFiller, &cursor); got != "" {
		t.Errorf("PickLine(nil lanes) = %q, want \"\"", got)
	}
	// A negative cursor is normalized, never a negative index panic.
	neg := -1
	if got := PickLine(lanes, LaneFiller, &neg); got != "c" {
		t.Errorf("PickLine(cursor=-1) = %q, want %q", got, "c")
	}
}

// Config surface: every C5 key resolves through the editor keypath
// reflection onto the real schema (parity posture of
// TestVoiceAcousticKeypathsResolve — that test lives in internal/configui
// and keeps its own assertions; this one proves the schema side here).
func TestVoiceLaneKeysResolveOnSchema(t *testing.T) {
	cfg := laneCfg(func(v *config.VoiceConfig) {
		v.SilencePromptLines = []string{"s1"}
		v.FragmentClarifyLines = []string{"f1"}
		v.Farewell = []string{"bye"}
	})
	if !equalTexts(cfg.Voice.SilencePromptLines, []string{"s1"}) ||
		!equalTexts(cfg.Voice.FragmentClarifyLines, []string{"f1"}) ||
		!equalTexts(cfg.Voice.Farewell, []string{"bye"}) {
		t.Fatal("C5 keys did not hold their set values")
	}
}
