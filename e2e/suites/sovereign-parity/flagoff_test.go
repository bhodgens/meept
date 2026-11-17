//go:build e2e

// Gate leaf 00T — flag-off control (T25, master Test Gate row "ALL").
//
// With NONE of the nine sovereign-router features configured (no quality
// block, no data_posture, no llm.budget block, no pool, no price_group),
// every routing decision must be byte-identical to pre-change behavior
// (main @ 4ec87974). The golden fixture
// e2e/fixtures/golden/sovereign-parity-flagoff.golden.json was captured
// FROM main and is checked in — it is never regenerated at runtime.
//
// RED state today: on main the suite compiles, the daemon boots, the
// turns run — so this control's comparison itself must hold (a GREEN
// here proves the harness faithful); the RED evidence for the tree lives
// in the contract-level files. If this control fails on main, the
// harness topology is wrong, not the tree.
package sovereignparity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// goldenFlagOffShape is the normalized decision shape compared against
// the checked-in fixture: per turn, the alias's chosen provider/model and
// the routing reason. Sorted fields only — no timestamps, no absolute
// paths, no ids — so the fixture is stable across runs and machines.
type goldenFlagOffShape struct {
	Scenario string           `json:"scenario"`
	Turns    []flagOffTurnRow `json:"turns"`
}

type flagOffTurnRow struct {
	Turn     int    `json:"turn"`
	Alias    string `json:"alias"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Reason   string `json:"reason"`
}

func committedFixturePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve this file's path")
	}
	// Walk up from this file to the directory containing go.mod (the repo
	// root) so the lookup works from any package-directory depth.
	dir := filepath.Dir(thisFile)
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return filepath.Join(dir, "e2e", "fixtures", "golden", "sovereign-parity-flagoff.golden.json")
}

// flagOffConfigFor builds the flag-off world against live stubs: a plain
// two-member rotation alias, no new config keys anywhere. Members differ
// in cost so the legacy cost/health order is observable, and every model
// carries NO quality/data_posture keys — the byte-identical posture.
func flagOffConfigFor(t *testing.T) parityConfig {
	t.Helper()
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "prova",
				stub:   stubA,
				apiKey: "key-a",
				models: []parityModel{
					// cheap primary
					{name: "model-a", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
			{
				id:     "provb",
				stub:   stubB,
				apiKey: "key-b",
				models: []parityModel{
					// dearer secondary
					{name: "model-b", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 3.0, outCost: 3.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"prova/model-a", "provb/model-b"}, timeout: 5, maxFails: 3},
		},
		chatAlias: "general",
	}
}

// TestFlagOffByteIdenticalControl replays the pinned scenario and diffs
// every routing_decisions row against the checked-in golden captured
// from main.
func TestFlagOffByteIdenticalControl(t *testing.T) {
	cfg := flagOffConfigFor(t)
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "flagoff")
	// Three plain turns: three round-robin steps over the two members.
	for i := range 3 {
		if reply := s.chatTurn(t, sessID, fmt.Sprintf("flagoff turn %d", i), 120*time.Second); reply == "" {
			t.Fatalf("flag-off turn %d returned empty reply", i)
		}
	}

	rows := s.routingRowsForAlias(t, "general")
	if len(rows) < 3 {
		t.Fatalf("flag-off control: only %d routing_decisions rows for alias general; the chat lane never resolved through the alias", len(rows))
	}

	got := goldenFlagOffShape{Scenario: "sovereign-parity-flagoff"}
	for i, r := range rows {
		got.Turns = append(got.Turns, flagOffTurnRow{
			Turn:     i + 1,
			Alias:    r.Alias,
			Provider: r.Provider,
			Model:    r.Model,
			Reason:   r.Reason,
		})
	}

	wantRaw, err := os.ReadFile(committedFixturePath(t))
	if err != nil {
		t.Fatalf("read committed golden fixture: %v", err)
	}
	var want goldenFlagOffShape
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatalf("decode committed golden fixture: %v", err)
	}

	if len(got.Turns) != len(want.Turns) {
		t.Fatalf("flag-off control: got %d decision rows, golden captured from main carries %d\ngot: %+v\nwant: %+v",
			len(got.Turns), len(want.Turns), got.Turns, want.Turns)
	}
	for i := range want.Turns {
		if got.Turns[i] != want.Turns[i] {
			t.Fatalf("flag-off control DIVERGED at turn %d:\ngot  %+v\nwant %+v (golden captured from main @ 4ec87974)\nA new routing behavior leaked into the flag-off posture — the nine features are not byte-identical when unconfigured.",
				i+1, got.Turns[i], want.Turns[i])
		}
	}
	t.Logf("flag-off control: %d routing decisions byte-identical to the main-captured golden", len(got.Turns))
}
