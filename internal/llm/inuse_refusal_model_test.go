package llm_test

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestBuildModelsInUse_RefusalModelSlot pins that a configured
// refusal_model slot value enters the boot pre-warm set (refusal-fallback
// tree 02): a local refusal-fallback endpoint must start at boot instead
// of being found dead on first fallback.
func TestBuildModelsInUse_RefusalModelSlot(t *testing.T) {
	slots := llm.ModelSlots{RefusalModel: "mlx-local/uncensored-8b"}
	got := llm.BuildModelsInUse(nil, slots, nil, nil)
	want := map[string]struct{}{
		"mlx-local/uncensored-8b": {},
	}
	if !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Empty slot stays out of the set (feature off).
	got = llm.BuildModelsInUse(nil, llm.ModelSlots{}, nil, nil)
	if len(got) != 0 {
		t.Errorf("empty refusal slot contributed %v, want empty set", got)
	}
}
