package voice

// Lane pools (plan 20261113-filler-clips leaf 05, root Contract C5
// FROZEN). Today every filler-class line the talk loop can speak lives in
// one of four ad-hoc shapes: voice.greeting (scalar), voice.filler
// (list), voice.silence_prompt (scalar, list-capable via
// voice.silence_prompt_lines), voice.fragment_clarify (scalar,
// list-capable via voice.fragment_clarify_lines). The farewell pool
// (voice.farewell) is new (R5). C5 makes every such line belong to
// exactly one enumerable lane set so the clip planner (leaf 02) can
// pre-render ALL of them at sidecar start: VoiceLanes resolves the
// five lanes for one employee and returns the deduplicated set;
// PickLine does the per-lane round-robin the talk loop already does
// for filler (the fillerIdx pattern, talkloop.go).
//
// Precedence ladder (per lane, C5): employee_leg row list > row scalar >
// global list > global scalar > shipped default — the row > global
// tri-state posture of ResolveAcoustics extended to list keys, where
// "set" for a list means NON-EMPTY (an empty/absent list never masks a
// lower layer; that is what keeps zero-config byte-identical: unset new
// keys leave the existing scalars binding, and the existing scalars keep
// their documented empty -> fallback chains at the talk-loop layer).
//
// Zero-config rule (internal/voice/AGENTS.md): a config with none of the
// new keys set resolves every EXISTING lane to exactly today's effective
// single-line pools (greeting = GreetingLine else Filler[0]; silence =
// the SilencePrompt scalar; fragment = the FragmentClarify scalar;
// empty scalars leave the lane empty and the talk loop keeps its legacy
// fallbacks — silence-empty -> filler round-robin, fragment-empty -> no
// clarify line). Only the new farewell lane lands on its non-empty
// shipped default.

import (
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// Lane names one filler-class pool the talk loop can speak from.
type Lane string

const (
	LaneGreeting Lane = "greeting"
	LaneFiller   Lane = "filler"
	LaneSilence  Lane = "silence"
	LaneFragment Lane = "fragment"
	LaneFarewell Lane = "farewell"
)

// LaneLine is one speakable line tagged with the lane it belongs to.
// The pair (Lane, Text) is the dedup identity: a text shared across two
// lanes appears once per lane (clip keys are text-hashed, so the render
// set dedups again on its side).
type LaneLine struct {
	Lane Lane
	Text string
}

// laneOrder is the canonical emission order of VoiceLanes: greeting,
// filler, silence, fragment, farewell — the C5 key listing.
var laneOrder = []Lane{LaneGreeting, LaneFiller, LaneSilence, LaneFragment, LaneFarewell}

// resolveLanePool applies the C5 ladder to one lane: row list > row
// scalar > global list > global scalar > default. A list "counts as set"
// only when non-empty after whitespace filtering, so the shipped
// empty-list default never masks the scalar layers (back-compat rule).
func resolveLanePool(rowList []string, rowScalar string, globalList []string, globalScalar string, def []string) []string {
	if pool := cleanPool(rowList); len(pool) > 0 {
		return pool
	}
	if rowScalar != "" {
		return []string{rowScalar}
	}
	if pool := cleanPool(globalList); len(pool) > 0 {
		return pool
	}
	if globalScalar != "" {
		return []string{globalScalar}
	}
	return cleanPool(def)
}

// cleanPool drops empty/whitespace-only entries, preserving order.
// A speakable-line enumeration must not carry blank lines.
func cleanPool(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// VoiceLanes resolves the five filler-class lane pools for one employee
// (employeeID "" = the global config only, the employeeEntry convention)
// and returns the deduplicated set of every line any of those lanes can
// speak this session, in canonical lane order with each lane's pool order
// intact. Pure config read (ResolveProfile shape): no talk-loop state,
// safe to call at planner/reload time.
//
// Greeting special case (today's rule preserved, talkloop.go
// greetingLine): voice.greeting empty rules to the FIRST resolved filler
// line; an empty filler pool plus empty greeting leaves the lane empty
// (the greeting is filler-class, never hardcoded).
func VoiceLanes(cfg *config.Config, employeeID string) []LaneLine {
	if cfg == nil {
		return nil
	}
	v := cfg.Voice
	entry := employeeEntry(v, employeeID)

	filler := cleanPool(v.Filler)
	var greetingPool []string
	if strings.TrimSpace(v.GreetingLine) != "" {
		greetingPool = []string{v.GreetingLine}
	} else if len(filler) > 0 {
		greetingPool = []string{filler[0]}
	}

	var rowSilenceLines, rowFragmentLines, rowFarewell []string
	var rowSilenceScalar string
	if entry != nil {
		rowSilenceLines = entry.SilencePromptLines
		rowSilenceScalar = entry.SilencePrompt
		rowFragmentLines = entry.FragmentClarifyLines
		rowFarewell = entry.Farewell
	}
	silence := resolveLanePool(rowSilenceLines, rowSilenceScalar, v.SilencePromptLines, v.SilencePrompt, nil)
	// The fragment lane has no row scalar today (fragment_clarify never
	// carried a per-employee override), so the ladder skips that rung.
	fragment := resolveLanePool(rowFragmentLines, "", v.FragmentClarifyLines, v.FragmentClarify, nil)
	farewell := resolveLanePool(rowFarewell, "", v.Farewell, "", config.DefaultVoiceFarewellLines())

	pools := map[Lane][]string{
		LaneGreeting: greetingPool,
		LaneFiller:   filler,
		LaneSilence:  silence,
		LaneFragment: fragment,
		LaneFarewell: farewell,
	}
	var out []LaneLine
	for _, lane := range laneOrder {
		seen := map[string]bool{}
		for _, text := range pools[lane] {
			if seen[text] {
				continue
			}
			seen[text] = true
			out = append(out, LaneLine{Lane: lane, Text: text})
		}
	}
	return out
}

// PickLine round-robins within one lane, the exact cursor semantics of
// today's fillerIdx (talkloop.go fillerLine): the cursor advances one
// per pick; a nil cursor takes the first line (one-shot consumers); an
// unknown/empty lane returns "" (the caller keeps its own fallback
// chain). The cursor domain is per-lane: callers keep one *int per lane
// (C5: "Pool selection = round-robin per lane with its own cursor").
func PickLine(lanes []LaneLine, lane Lane, cursor *int) string {
	var pool []string
	for _, ll := range lanes {
		if ll.Lane == lane {
			pool = append(pool, ll.Text)
		}
	}
	if len(pool) == 0 {
		return ""
	}
	if cursor == nil {
		return pool[0]
	}
	idx := *cursor % len(pool)
	if idx < 0 {
		idx += len(pool)
	}
	*cursor = idx + 1
	return pool[idx]
}
