// Quality-gated autonomy (loop-economics leaf 08).
//
// This file implements completion gating for employee goals: an autonomous
// run may only claim success after a user-defined shell check passes, and a
// failed gate is not re-run while the workspace is unchanged.
//
// The gate engine itself lives in internal/gate (extracted so that
// internal/agent can reuse RunGate for roster AGENT.md gates without an
// import cycle: employee → bot → comm/http → agent). Everything here is a
// type/var alias re-export; behaviour is unchanged and all employee-side
// callers keep compiling.
//
// This is ORTHOGONAL to the enforcement engine (enforcement.go): enforcement
// polices individual actions pre-execution; the quality gate decides whether
// a goal may be marked complete after execution. Do not conflate them.
package employee

import (
	"github.com/caimlas/meept/internal/gate"
)

// DefaultGateTimeoutSeconds is used when GateConfig.TimeoutSeconds is unset.
const DefaultGateTimeoutSeconds = gate.DefaultGateTimeoutSeconds

// GateConfig configures the quality gate for a goal. An empty Command means
// no gate is configured (legacy behaviour: the model's own judgment completes
// the goal).
type GateConfig = gate.GateConfig

// GateResult reports the outcome of a gate run.
type GateResult = gate.GateResult

// GateState carries the cross-round memory needed for skip-on-unchanged.
type GateState = gate.GateState

// RunGate executes the configured quality gate in the given workspace. See
// internal/gate.RunGate for the full behavioural contract.
var RunGate = gate.RunGate
