package wsclass

import "testing"

// FuzzWSClassString exercises the WSClass wire-string mapping with arbitrary
// int values, including out-of-range ones. Invariants:
//   - never panics (an unknown value falls through to "event");
//   - always returns one of the six valid wire strings — never empty.
func FuzzWSClassString(f *testing.F) {
	seeds := []int{
		int(WSChatMessage),
		int(WSProgress),
		int(WSMetricsUpdate),
		int(WSJobUpdate),
		int(WSPlanUpdate),
		int(WSEvent),
		-1,
		6,
		42,
		1 << 30,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	valid := map[string]bool{
		"chat_message":    true,
		"agent_progress":  true,
		"metrics_update":  true,
		"job_update":      true,
		"plan_update":     true,
		"event":           true,
	}
	f.Fuzz(func(t *testing.T, v int) {
		got := WSClass(v).String()
		if !valid[got] {
			t.Fatalf("WSClass(%d).String() = %q, want one of the six wire strings", v, got)
		}
	})
}
