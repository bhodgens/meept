package voice

// Contract C5 (FROZEN, quoted from
// docs/plans/20261113-filler-clips-and-context-warm/master.md):
//
//	voice.filler           // existing working-turn pool (keep; multiple lines now fully honored)
//	voice.greeting         // existing
//	voice.silence_prompt   // existing single line -> accept list (deprecated-shape rule below)
//	voice.fragment_clarify // existing single line -> accept list
//	voice.farewell         // NEW pool: ["Goodbye!","Bye!","Talk soon."] default non-empty
//
//	Tri-state/back-compat rule: silence_prompt/fragment_clarify stay
//	`string` in schema (existing per-employee overrides depend on it);
//	NEW list keys voice.silence_prompt_lines,
//	voice.fragment_clarify_lines, voice.farewell_lines take precedence
//	when non-empty. Pool selection = round-robin per lane with its own
//	cursor (existing fillerIdx pattern).
//
// Precedence ladder (branch orchestrator, leaf 05 Interface Contract):
// row list > row scalar > global list > global scalar > default.
//
// 00T gate pin (leaf 00T task 4). This file is the contract-literal
// cross-check; the sibling lanes_test.go (leaf 05) is the impl's own
// suite. Where both assert the same rule they MUST agree — the control
// agent compares the pinned strings against master.md.

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func clipGateCfg(mut func(v *config.VoiceConfig)) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Voice = config.VoiceConfig{}
	if mut != nil {
		mut(&cfg.Voice)
	}
	config.NormalizeVoiceAcousticDefaults(&cfg.Voice)
	return cfg
}

func clipGateLaneTexts(lines []LaneLine, lane Lane) []string {
	var out []string
	for _, l := range lines {
		if l.Lane == lane {
			out = append(out, l.Text)
		}
	}
	return out
}

// CG-L1: the C5 farewell default is the frozen three-line literal
// (master C5 + leaf 05: DefaultVoiceFarewellLines).
func TestClipGateFarewellDefaultIsFrozenLiteral(t *testing.T) {
	want := []string{"Goodbye!", "Bye!", "Talk soon."}
	if got := config.DefaultVoiceFarewellLines(); len(got) != len(want) {
		t.Fatalf("DefaultVoiceFarewellLines = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("DefaultVoiceFarewellLines = %v, want %v", got, want)
			}
		}
	}
	lines := VoiceLanes(clipGateCfg(func(v *config.VoiceConfig) {}), "")
	if got := clipGateLaneTexts(lines, LaneFarewell); len(got) != len(want) {
		t.Fatalf("zero-config farewell lane = %v, want the C5 default %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("zero-config farewell lane = %v, want %v", got, want)
			}
		}
	}
}

// CG-L2: full precedence ladder for the three re-shaped lanes —
// row list > row scalar > global list > global scalar > (farewell:
// default; silence/fragment: shipped scalar). Employee rows ride
// voice.employee_leg (the existing tri-state pattern).
func TestClipGateC5PrecedenceLadder(t *testing.T) {
	const emp = "clip-gate-emp"
	cases := []struct {
		name       string
		lane       Lane
		rowList    []string
		rowScalar  string
		globalList []string
		globalText string
		want       []string
	}{
		{"row-list-wins-all", LaneSilence, []string{"rowlist-a", "rowlist-b"}, "rowscalar", []string{"globallist"}, "globalscalar", []string{"rowlist-a", "rowlist-b"}},
		{"row-scalar-over-global-list", LaneSilence, nil, "rowscalar", []string{"globallist-a", "globallist-b"}, "globalscalar", []string{"rowscalar"}},
		{"global-list-over-global-scalar", LaneSilence, nil, "", []string{"globallist"}, "globalscalar", []string{"globallist"}},
		{"global-scalar-last", LaneSilence, nil, "", nil, "globalscalar", []string{"globalscalar"}},
		{"row-list-farewell", LaneFarewell, []string{"See ya."}, "", []string{"globallist"}, "", []string{"See ya."}},
		{"global-list-farewell-beats-default", LaneFarewell, nil, "", []string{"globallist"}, "", []string{"globallist"}},
		{"global-list-fragment", LaneFragment, nil, "", []string{"fraglist-a", "fraglist-b"}, "fragscalar", []string{"fraglist-a", "fraglist-b"}},
		{"row-list-fragment", LaneFragment, []string{"fragrow"}, "", []string{"fraglist-a"}, "fragscalar", []string{"fragrow"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := clipGateCfg(func(v *config.VoiceConfig) {
				switch tc.lane {
				case LaneSilence:
					v.SilencePrompt = tc.globalText
					v.SilencePromptLines = tc.globalList
				case LaneFragment:
					v.FragmentClarify = tc.globalText
					v.FragmentClarifyLines = tc.globalList
				case LaneFarewell:
					v.Farewell = tc.globalList
					if tc.globalText != "" {
						t.Fatal("farewell global scalar layer is the default pool, not a scalar field")
					}
				}
				v.EmployeeLeg = []config.EmployeeLegConfig{{
					Employee:             emp,
					SilencePrompt:        "",
					SilencePromptLines:   nil,
					FragmentClarifyLines: nil,
				}}
				row := &v.EmployeeLeg[0]
				switch tc.lane {
				case LaneSilence:
					row.SilencePrompt = tc.rowScalar
					row.SilencePromptLines = tc.rowList
				case LaneFragment:
					row.FragmentClarifyLines = tc.rowList
					// Row scalar for fragment reuses the existing
					// per-row silence-style scalar if the schema has
					// one; fragment_clarify per-row is list-only in
					// leaf 05's landed shape — empty rowScalar then.
					if tc.rowScalar != "" {
						t.Skip("C5 row scalar for fragment_clarify: employee_leg carries no fragment_clarify scalar row in the landed schema; ladder row covered via silence")
					}
				case LaneFarewell:
					row.Farewell = tc.rowList
					// farewell has no scalar layer at all (new pool):
					// rowScalar inputs for it are unused.
					if tc.rowScalar != "" {
						t.Fatalf("unexpected scalar input for a no-scalar lane")
					}
				}
			})
			got := clipGateLaneTexts(VoiceLanes(cfg, emp), tc.lane)
			if len(got) != len(tc.want) {
				t.Fatalf("lane %s (%s): got %v, want %v", tc.lane, tc.name, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("lane %s (%s): got %v, want %v", tc.lane, tc.name, got, tc.want)
				}
			}
		})
	}
}

// CG-L3: legacy equivalence — with only the existing scalar keys set
// (no _lines keys, no farewell override), every lane resolves to exactly
// what today's talk loop can speak, so clip enumeration adds lines
// without changing any existing one.
func TestClipGateLegacyScalarsByteEquivalent(t *testing.T) {
	cfg := clipGateCfg(func(v *config.VoiceConfig) {
		v.GreetingLine = "Hi there."
		v.Filler = []string{"One moment."}
		v.SilencePrompt = "Still there?"
		v.FragmentClarify = "Come again?"
	})
	lines := VoiceLanes(cfg, "")
	if got := clipGateLaneTexts(lines, LaneGreeting); len(got) != 1 || got[0] != "Hi there." {
		t.Fatalf("greeting lane = %v", got)
	}
	if got := clipGateLaneTexts(lines, LaneFiller); len(got) != 1 || got[0] != "One moment." {
		t.Fatalf("filler lane = %v", got)
	}
	if got := clipGateLaneTexts(lines, LaneSilence); len(got) != 1 || got[0] != "Still there?" {
		t.Fatalf("silence lane = %v", got)
	}
	if got := clipGateLaneTexts(lines, LaneFragment); len(got) != 1 || got[0] != "Come again?" {
		t.Fatalf("fragment lane = %v", got)
	}
	// Greeting-empty rule preserved: empty greeting resolves to the
	// FIRST filler line (talkloop.go:537 today's rule).
	cfg2 := clipGateCfg(func(v *config.VoiceConfig) {
		v.Filler = []string{"Second.", "First."}
	})
	if got := clipGateLaneTexts(VoiceLanes(cfg2, ""), LaneGreeting); len(got) != 1 || got[0] != "Second." {
		t.Fatalf("empty-greeting fallback = %v, want [\"Second.\"] (first filler line)", got)
	}
}

// CG-L4: PickLine round-robin with its own cursor == today's fillerIdx
// semantics; a single-line pool picks that line every time (leaf 04
// unit pin "single-line pools ⇒ same lines, same order as legacy").
func TestClipGatePickLineRoundRobin(t *testing.T) {
	lanes := []LaneLine{
		{Lane: LaneFiller, Text: "A."},
		{Lane: LaneFiller, Text: "B."},
		{Lane: LaneSilence, Text: "S."},
	}
	cur := 0
	want := []string{"A.", "B.", "A.", "B.", "A."}
	for i, w := range want {
		if got := PickLine(lanes, LaneFiller, &cur); got != w {
			t.Fatalf("pick %d = %q, want %q (round-robin per lane cursor)", i, got, w)
		}
	}
	scur := 0
	for range 3 {
		if got := PickLine(lanes, LaneSilence, &scur); got != "S." {
			t.Fatalf("single-line pool must always pick its line, got %q", got)
		}
	}
	// Independent cursors per lane: the silence pick above must not
	// have advanced the filler lane (each lane "with its own cursor").
	if got := PickLine(lanes, LaneFiller, &cur); got != "B." {
		t.Fatalf("lane cursors shared state: filler picked %q, want \"B.\"", got)
	}
}
