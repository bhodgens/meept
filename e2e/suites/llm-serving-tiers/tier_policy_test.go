//go:build e2e

// Tier policy scenarios (leaf 05 T2, over leaf 01's implementation): the
// depth ceiling stops the rotation at the floor, degraded-tier metadata
// keys ride every completed call, and the precedence regression (a
// quota-blocked member consumes zero depth) holds.
//
// Route (documented deviation): the daemon-side SetTierPolicy call site
// (components.go consuming llm.tiers) is not wired yet, so a chat turn
// through the harness daemon cannot arm the policy. The scenarios drive
// the REAL exported seam the daemon wiring will use - llm.NewProviderManager
// over REAL OpenAI-compatible clients against real stub endpoints, with
// llm.SetTierPolicy - the context-memory-engine in-process pattern. The
// live-daemon config-acceptance pin lives in config_ttft_test.go.
package llmservingtiers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// TestTierFloorStopsAtDepthBound (T2a): a 4-member chain all serving empty
// completions with llm.tiers max_depth=2 fails the call classified at the
// floor (errors.Is ErrTierFloor) - exactly 3 members dispatch (primary + 2
// degradations) and member 4 is REFUSED with no dispatch (Contract C2's
// worked example).
func TestTierFloorStopsAtDepthBound(t *testing.T) {
	m1 := newMemberStub(t, "empty")
	m2 := newMemberStub(t, "empty")
	m3 := newMemberStub(t, "empty")
	m4 := newMemberStub(t, "empty")
	pm := tierManager(t, &llm.TierPolicy{Enabled: true, MaxDepth: 2},
		[]*memberStub{m1, m2, m3, m4})

	_, err := tierChat(context.Background(), pm)
	if err == nil {
		t.Fatal("expected the rotation to stop at the tier floor with an error")
	}
	if !llm.IsTierFloorError(err) && !errors.Is(err, llm.ErrTierFloor) {
		t.Fatalf("error is not tier-floor classified: %v", err)
	}
	// The floor error names the REFUSED next hop (prov-4 - the call site
	// passes the loop entry it refuses; depth 3 = primary + 2 degradations,
	// limit 2) and carries the floor classification.
	if !strings.Contains(err.Error(), "prov-4") || !strings.Contains(err.Error(), "depth=3") || !strings.Contains(err.Error(), "limit=2") {
		t.Fatalf("floor error does not name the refused hop + depth + limit: %v", err)
	}
	if m4.hitCount() != 0 {
		t.Fatalf("member 4 dispatched %d times; the floor must refuse the hop beyond max_depth", m4.hitCount())
	}
	if m1.hitCount() == 0 || m2.hitCount() == 0 || m3.hitCount() == 0 {
		t.Fatalf("the three legal members did not all serve (m1=%d m2=%d m3=%d)",
			m1.hitCount(), m2.hitCount(), m3.hitCount())
	}
}

// TestTierMetadataKeysOnDegradedTurn (T2b): member 1 returns empty
// completions, member 2 serves - the completed call's metadata carries
// exactly llm_tier = prov-2/stub-model, llm_tier_degraded = true,
// llm_tier_depth = 1.
func TestTierMetadataKeysOnDegradedTurn(t *testing.T) {
	m1 := newMemberStub(t, "empty")
	m2 := newMemberStub(t, "ok")
	pm := tierManager(t, &llm.TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: true},
		[]*memberStub{m1, m2})

	resp, err := tierChat(context.Background(), pm)
	if err != nil {
		t.Fatalf("degraded turn failed: %v", err)
	}
	if resp == nil || resp.Metadata == nil {
		t.Fatal("completed degraded turn carried no metadata map")
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTier].(string); got != "prov-2/stub-model" {
		t.Fatalf("llm_tier = %q, want prov-2/stub-model", got)
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTierDegraded].(bool); !got {
		t.Fatal("llm_tier_degraded = false on a member-2-served turn")
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTierDepth].(int); got != 1 {
		t.Fatalf("llm_tier_depth = %v, want 1", resp.Metadata[llm.MetaKeyLLMTierDepth])
	}
	if len(resp.Metadata) != 3 {
		t.Fatalf("metadata carries %d keys, want exactly the 3 tier keys: %v", len(resp.Metadata), resp.Metadata)
	}
}

// TestTierMetadataPrimaryTurn (T2b quiet side): member 1 serves directly -
// metadata still carries the three keys, but degraded=false, depth=0.
func TestTierMetadataPrimaryTurn(t *testing.T) {
	m1 := newMemberStub(t, "ok")
	pm := tierManager(t, &llm.TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: true},
		[]*memberStub{m1})

	resp, err := tierChat(context.Background(), pm)
	if err != nil {
		t.Fatalf("primary-served turn failed: %v", err)
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTier].(string); got != "prov-1/stub-model" {
		t.Fatalf("llm_tier = %q, want prov-1/stub-model", got)
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTierDegraded].(bool); got {
		t.Fatal("llm_tier_degraded = true on a primary-served turn")
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTierDepth].(int); got != 0 {
		t.Fatalf("llm_tier_depth = %v, want 0", resp.Metadata[llm.MetaKeyLLMTierDepth])
	}
}

// TestTierDisabledNoMetadata (T2 quiet control): with the policy disabled
// the same primary-served topology writes NO metadata keys - the zero
// behavior-change posture (Contract C3's disabled arm).
func TestTierDisabledNoMetadata(t *testing.T) {
	m1 := newMemberStub(t, "ok")
	pm := tierManager(t, nil, []*memberStub{m1})

	resp, err := tierChat(context.Background(), pm)
	if err != nil {
		t.Fatalf("turn failed with tiers disabled: %v", err)
	}
	if resp.Metadata != nil {
		t.Fatalf("disabled policy wrote metadata keys: %v", resp.Metadata)
	}
}

// TestTierQuotaSkipConsumesNoDepth (T2c precedence regression): member 1
// quota-blocked, member 2 serves. Turn 1 arms the per-credential block
// (the 429 arrives; the block persists); turn 2 must SKIP member 1 with no
// dispatch (zero depth consumed) - member 2 serves a NON-degraded turn
// (llm_tier_degraded=false, depth 0) even though it is alias member 2.
func TestTierQuotaSkipConsumesNoDepth(t *testing.T) {
	m1 := newMemberStub(t, "quota")
	m1.mu.Lock()
	m1.quota = llmShape{Code: "usage_limit_reached", ResetsAt: time.Now().Add(time.Hour).Unix()}
	m1.mu.Unlock()
	m2 := newMemberStub(t, "ok")
	pm := tierManager(t, &llm.TierPolicy{Enabled: true, MaxDepth: 2, SignalDegraded: true},
		[]*memberStub{m1, m2})

	ctx := context.Background()
	// Turn 1: member 1's 429 arrives and arms the credential block;
	// member 2 serves (degraded - a real dispatch + failure happened).
	if _, err := tierChat(ctx, pm); err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if m1.hitCount() == 0 {
		t.Fatal("turn 1 never reached member 1; the block was never armed")
	}
	blockedHits := m1.hitCount()

	// Turn 2: member 1 is skipped BEFORE any depth counting - no request
	// dispatched, and member 2's serve is NOT a degradation.
	resp, err := tierChat(ctx, pm)
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	if m1.hitCount() != blockedHits {
		t.Fatalf("quota-blocked member 1 was dispatched again (%d -> %d); skips must consume no depth",
			blockedHits, m1.hitCount())
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTier].(string); got != "prov-2/stub-model" {
		t.Fatalf("llm_tier = %q, want prov-2/stub-model (member 2 serves)", got)
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTierDegraded].(bool); got {
		t.Fatal("quota skip must NOT count as degradation: llm_tier_degraded=true observed")
	}
	if got, _ := resp.Metadata[llm.MetaKeyLLMTierDepth].(int); got != 0 {
		t.Fatalf("llm_tier_depth = %v after a quota skip, want 0", got)
	}
}
