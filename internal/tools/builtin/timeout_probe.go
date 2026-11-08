package builtin

import (
	"context"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// TimeoutProbeTimeoutMs is the cooperative budget the probe tool declares
// (contract B1 / tool-surface leaf 01). The F9 e2e gate invokes it with a
// sleep far longer than this so the executor's derived deadline expires and
// substitutes the structured TOOL_TIMEOUT result (ErrCode "TOOL_TIMEOUT"),
// never a raw context.DeadlineExceeded in the model-facing envelope.
const TimeoutProbeTimeoutMs = 1500 * time.Millisecond

// TimeoutProbeTool is an instrumentation tool that sleeps for the requested
// duration and reports the elapsed time. It declares a short cooperative time
// budget (TimeoutMs), so a caller asking for a sleep beyond that budget
// exercises the executor's declared-timeout substitution through the live
// turn path. The daemon registers it ONLY when the opt-in config flag
// [agent].tool_timeout_probe_enabled is set, keeping production tool
// registries and agent grants untouched.
type TimeoutProbeTool struct {
	tools.ToolDefaults
}

// NewTimeoutProbeTool creates the probe tool.
func NewTimeoutProbeTool() *TimeoutProbeTool { return &TimeoutProbeTool{} }

func (t *TimeoutProbeTool) Name() string     { return "timeout_probe" }
func (t *TimeoutProbeTool) Category() string { return "code" }

func (t *TimeoutProbeTool) Description() string {
	return "Sleep for sleep_ms milliseconds and report the elapsed time. Used to verify per-tool timeout enforcement."
}

func (t *TimeoutProbeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"sleep_ms": {
				Type:        schemaTypeInteger,
				Description: "How long to sleep, in milliseconds.",
			},
		},
		Required: []string{"sleep_ms"},
	}
}

// TimeoutMs declares a short cooperative budget (contract B1): a sleep beyond
// this must surface as a structured TOOL_TIMEOUT, not a hang.
func (t *TimeoutProbeTool) TimeoutMs() time.Duration { return TimeoutProbeTimeoutMs }

// IsReadOnly reports the probe as a pure read: sleeping has no side effects.
func (t *TimeoutProbeTool) IsReadOnly(map[string]any) bool { return true }

// TimeoutProbeResult is the success payload returned when the sleep finishes
// within the declared budget.
type TimeoutProbeResult struct {
	SleepMs int    `json:"sleep_ms"`
	SleptMs int    `json:"slept_ms"`
	Message string `json:"message"`
}

// Execute sleeps for the requested duration, honoring ctx cancellation so the
// executor's derived deadline (TimeoutMs) can fire. When the budget expires
// first, Execute returns ctx.Err() (a plain context deadline exceeded) — the
// executor recognizes the derived timer fired and substitutes the structured
// TOOL_TIMEOUT error (contract B1, T5: upstream cancellation is never masked).
func (t *TimeoutProbeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sleepMs := 0
	if v, ok := args["sleep_ms"].(float64); ok {
		sleepMs = int(v)
	}
	if sleepMs <= 0 {
		sleepMs = 1000
	}
	timer := time.NewTimer(time.Duration(sleepMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return TimeoutProbeResult{
		SleepMs: sleepMs,
		SleptMs: sleepMs,
		Message: fmt.Sprintf("slept %d ms", sleepMs),
	}, nil
}

// Ensure TimeoutProbeTool implements the Tool interface.
var _ tools.Tool = (*TimeoutProbeTool)(nil)
