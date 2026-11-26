// golden_scenarios.go - the canonical FakeLLM scripts for the committed
// golden fixtures, registered into regenRegistry here (not in the suite)
// so the Makefile golden-regenerate dispatch — which runs in THIS
// package's test binary — finds a registered regeneration script.
//
// Bughunt 2026-11-25 A7-8: RegenerateScenarioScript had zero callers, the
// registry was empty at runtime, and `make golden-regenerate` therefore
// always refused AFTER running the suite gate green. The scripts live
// here, the suite replays them from here, and regen recaptures with the
// registry copy — fixture and suite read ONE source, so they can never
// drift apart by construction (the original leaf-03 design intent, now
// actually wired).
package harness

import (
	"github.com/caimlas/meept/internal/eval/trajectorydiff"
)

// ScenarioTrajectoryGoldenBasic is the fixture stem of the canonical
// trajectory-golden scenario (e2e/fixtures/golden/trajectory-golden-basic.golden.json).
const ScenarioTrajectoryGoldenBasic = "trajectory-golden-basic"

// TrajectoryGoldenBasicScript is the canonical suite scenario:
// read_file -> file_write -> read_file verify. The trajectory-golden
// suite replays it as its gate, golden-regenerate recaptures the fixture
// from it, and the fixture-authoring dispatch stamps its Match block —
// all three read this one script on purpose.
func TrajectoryGoldenBasicScript(f *FakeLLM) {
	f.SetPostToolText("scenario finished")
	f.EnqueueToolCalls(
		ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/meept-trajectory-golden-basic/input.txt"}`},
		ToolCall{Name: "file_write", Arguments: `{"path":"/tmp/meept-trajectory-golden-basic/out.txt","content":"golden","direct":true}`},
		ToolCall{Name: "read_file", Arguments: `{"path":"/tmp/meept-trajectory-golden-basic/out.txt"}`},
	)
}

func init() {
	RegenerateScenarioScript(CaptureOptions{
		ScenarioName: ScenarioTrajectoryGoldenBasic,
		Script:       TrajectoryGoldenBasicScript,
		Match:        trajectorydiff.Options{Mode: trajectorydiff.MatchInOrder, Threshold: 1.0},
	})
}
