# Epistemic Memory Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend meept's memory platform with epistemic types (Claim, Decision, Prediction, Question), epistemic edges (contradicts, superseded, evidence_for/against, derives_from, supports), trust-graded claim status, LLM-driven relationship detection, four creation paths (explicit tools, ambient extraction, backlog mining, reflection surfacing), a destructive-tool confirmation protocol wired across CLI/TUI/GUI, and RPC/HTTP API exposure.

**Architecture:** All new memory types and edges are additive constants on existing TEXT columns — zero migrations. The `Manager` gains new typed helpers (`StoreClaim`, `MarkSuperseded`, etc.) that wrap existing `Store`/`StoreVersioned`. A new `EpistemicDetector` runs LLM classification after epistemic memories are stored. An opt-in `EpistemicHook` extracts claims from conversation post-turn. A two-phase confirmation protocol (`requires_confirmation: true` → UI prompts → re-invoke with `confirmed: true`) gates destructive tools.

**Tech Stack:** Go 1.24, SQLite (FTS5, existing `episodic_memories`/`memory_edges` tables), bubbletea TUI, Flutter, internal/llm.Chatter, internal/memory.EmbeddingProvider, JSON-RPC + REST.

**Source spec:** `docs/superpowers/specs/2026-06-21-epistemic-memory-and-agent-roster-design.md` (Plan 1 sections). The spec contains full code snippets — this plan references them by section where duplication would bloat the document, but includes complete code for every step that writes code.

---

## File Structure Mapping

### Files to Create

| File | Responsibility |
|------|----------------|
| `internal/memory/epistemic.go` | Claim/Decision/Prediction/Question structs; ClaimStatus enum + TrustWeight; Manager helpers (StoreClaim/StoreDecision/StorePrediction/StoreQuestion, MarkSuperseded, MarkResolved, RecordReview, PromoteClaim, RejectClaim, ListPendingReviews, ListAutoClaims, FindCanonicalFor) |
| `internal/memory/epistemic_test.go` | Unit tests for epistemic helpers |
| `internal/memory/epistemic_detection.go` | EpistemicDetector: embedding-NN search → LLM classification → candidate edges; PotentialContradicts queue |
| `internal/memory/epistemic_detection_test.go` | Tests for detection pipeline (uses stub classifier) |
| `internal/memory/epistemic_ambient.go` | AmbientExtractor: LLM-based claim mining from conversation window |
| `internal/memory/epistemic_ambient_test.go` | Tests for ambient extraction (uses stub classifier) |
| `internal/agent/epistemic_hook.go` | Post-turn hook wiring: gate → call AmbientExtractor → write auto claims |
| `internal/agent/epistemic_hook_test.go` | Tests for hook gating and exclude-intents filter |
| `internal/tools/builtin/retain_typed.go` | Path A tools: `retain_claim`, `retain_decision`, `retain_prediction` |
| `internal/tools/builtin/retain_typed_test.go` | Tests for Path A tools (nil-manager, missing fields, happy path) |
| `internal/tools/builtin/confirmation.go` | Shared confirmation helpers: `ConfirmationResponse`, `IsConfirmationRequest`, `DeclineResponse` |
| `internal/tools/builtin/confirmation_test.go` | Tests for confirmation helpers |
| `internal/tools/builtin/epistemic_actions.go` | Destructive tools with two-phase confirmation: `mark_superseded`, `mark_resolved`, `record_review`, `reject_claim`, `purge_auto_claims` |
| `internal/tools/builtin/epistemic_actions_test.go` | Tests for destructive tools (phase 1 preview, phase 2 execute, decline path) |
| `internal/tui/confirmation.go` | TUI confirmation modal (bubbletea component) |
| `internal/tui/confirmation_test.go` | Tests for modal rendering and keybindings |
| `ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart` | Flutter dialog widget |
| `config/epistemic_tags.json5` | Curated tag taxonomy (domains, confidence_levels, scopes) |
| `docs/configuration/epistemic-memory.md` | Documentation page for epistemic config |
| `internal/rpc/epistemic.go` | Direct RPC handlers dispatching to Manager methods |

### Files to Modify

| File | Change |
|------|--------|
| `internal/memory/types.go:15-22` | Add `MemoryTypeClaim`, `MemoryTypeDecision`, `MemoryTypePrediction`, `MemoryTypeQuestion` constants |
| `internal/memory/graph.go:25-36` | Add `EdgeTypeContradicts`, `EdgeTypeSuperseded`, `EdgeTypeEvidenceFor`, `EdgeTypeEvidenceAgainst`, `EdgeTypeDerivesFrom`, `EdgeTypeSupports`, `EdgeTypePotentialContradicts` constants |
| `internal/memory/graph.go` (new method) | Add `EdgeCountForMemory(ctx, memoryID) (int, error)` — needed by mark_superseded preview |
| `internal/memory/manager.go:41-86` | Add `detector *EpistemicDetector` field, `SetEpistemicDetector` setter, `epistemicCfg config.EpistemicConfig` field |
| `internal/memory/manager.go:342-416` | `Store`: after persist, if `mem.Type` is epistemic and detector is configured, run `DetectRelationships` and persist edges |
| `internal/config/schema.go:505-526` | Add `Epistemic EpistemicConfig` field to `MemoryConfig` |
| `internal/config/schema.go` (new types) | Add `EpistemicConfig` and `AmbientExtractionConfig` structs |
| `internal/tools/builtin/memory.go:781-...` | Deepen `MemoryReflectTool.Execute` to surface epistemic themes, contradictions, pending reviews, auto candidates |
| `internal/daemon/components.go:3055-3075` | Wire new tools: retain_typed (3), epistemic_actions (5); instantiate EpistemicDetector and AmbientExtractor; wire post-turn hook |
| `internal/tui/tools.go` (tool dispatch site) | Intercept `requires_confirmation` tool results, show modal, re-invoke with `confirmed=true` on approval |
| `ui/flutter_ui/lib/services/tool_runner.dart` | Same interceptor for Flutter |
| `internal/rpc/proxy.go:58-63` | Register new RPC methods (memory.retainClaim, memory.markSuperseded, etc.) |
| `internal/comm/http/api_handlers.go` | New HTTP endpoints under `/api/v1/memory/` |
| `internal/comm/http/config_service.go` | Expose Epistemic config |
| `cmd/meept/memory.go` (or equivalent) | CLI: `meept memory review`, `meept memory supersede OLD NEW [--confirm]`, `meept memory promote ID`, `meept memory reject ID` |
| `docs/reference/generated/` | Regenerate via `make docs-generate` after schema changes |

---

## Task 1: Add Epistemic MemoryType and EdgeType Constants

**Files:**
- Modify: `internal/memory/types.go:15-22`
- Modify: `internal/memory/graph.go:25-36`
- Test: `internal/memory/epistemic_constants_test.go` (new)

### Steps

- [ ] **Step 1: Write failing test for new constants**

Create `internal/memory/epistemic_constants_test.go`:

```go
package memory

import "testing"

func TestEpistemicMemoryTypes(t *testing.T) {
	cases := []struct {
		got, want MemoryType
	}{
		{MemoryTypeClaim, MemoryType("claim")},
		{MemoryTypeDecision, MemoryType("decision")},
		{MemoryTypePrediction, MemoryType("prediction")},
		{MemoryTypeQuestion, MemoryType("question")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestEpistemicEdgeTypes(t *testing.T) {
	cases := []struct {
		got, want EdgeType
	}{
		{EdgeTypeContradicts, EdgeType("contradicts")},
		{EdgeTypeSuperseded, EdgeType("superseded")},
		{EdgeTypeEvidenceFor, EdgeType("evidence_for")},
		{EdgeTypeEvidenceAgainst, EdgeType("evidence_against")},
		{EdgeTypeDerivesFrom, EdgeType("derives_from")},
		{EdgeTypeSupports, EdgeType("supports")},
		{EdgeTypePotentialContradicts, EdgeType("potential_contradicts")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run TestEpistemic -v`
Expected: FAIL with "undefined: MemoryTypeClaim" (and friends).

- [ ] **Step 3: Add MemoryType constants**

Edit `internal/memory/types.go`. Inside the existing const block (between `MemoryTypePersonality` and the closing paren), add:

```go
	// MemoryTypeClaim is a structured assertion of belief.
	MemoryTypeClaim MemoryType = "claim"
	// MemoryTypeDecision is a recorded decision with expected outcome.
	MemoryTypeDecision MemoryType = "decision"
	// MemoryTypePrediction is a forecast with a horizon.
	MemoryTypePrediction MemoryType = "prediction"
	// MemoryTypeQuestion is an open tracked question.
	MemoryTypeQuestion MemoryType = "question"
```

- [ ] **Step 4: Add EdgeType constants**

Edit `internal/memory/graph.go`. Inside the existing const block (after `EdgeTypeCausal`, before closing paren), add:

```go
	// Epistemic edges (Plan 1: epistemic memory platform)
	EdgeTypeContradicts          EdgeType = "contradicts"
	EdgeTypeSuperseded           EdgeType = "superseded"
	EdgeTypeEvidenceFor          EdgeType = "evidence_for"
	EdgeTypeEvidenceAgainst      EdgeType = "evidence_against"
	EdgeTypeDerivesFrom          EdgeType = "derives_from"
	EdgeTypeSupports             EdgeType = "supports"
	EdgeTypePotentialContradicts EdgeType = "potential_contradicts"
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/memory/ -run TestEpistemic -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/memory/types.go internal/memory/graph.go internal/memory/epistemic_constants_test.go
git commit -m "feat(memory): add epistemic MemoryType and EdgeType constants"
```

---

## Task 2: Add EpistemicConfig and AmbientExtractionConfig

**Files:**
- Modify: `internal/config/schema.go` (add structs; add field to MemoryConfig)
- Test: `internal/config/epistemic_config_test.go` (new)

### Steps

- [ ] **Step 1: Write failing test**

Create `internal/config/epistemic_config_test.go`:

```go
package config

import "testing"

func TestEpistemicConfigDefaults(t *testing.T) {
	cfg := MemoryConfig{}
	// Ambient extraction must default to false.
	if cfg.Epistemic.AmbientExtraction.Enabled {
		t.Error("ambient extraction must default to false")
	}
	// AutoTrustWeight default is 0 (helpers substitute DefaultAutoClaimTrustWeight).
	if cfg.Epistemic.AutoTrustWeight != 0 {
		t.Errorf("AutoTrustWeight zero-value expected, got %v", cfg.Epistemic.AutoTrustWeight)
	}
}

func TestEpistemicConfigRoundTrip(t *testing.T) {
	cfg := EpistemicConfig{
		AmbientExtraction: AmbientExtractionConfig{
			Enabled:             true,
			ConfidenceThreshold: 0.75,
			MaxPerTurn:          3,
			ExcludeIntents:      []string{"chat"},
			ExcludeCategories:   []string{"joke"},
			ContextWindow:       5,
		},
		AutoTrustWeight:       0.5,
		DetectionThreshold:    0.7,
		ReviewPromptFrequency: "weekly",
		MaxPendingReviews:     20,
	}
	if !cfg.AmbientExtraction.Enabled {
		t.Error("Enabled did not round-trip")
	}
	if cfg.AutoTrustWeight != 0.5 {
		t.Errorf("AutoTrustWeight got %v, want 0.5", cfg.AutoTrustWeight)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestEpistemic -v`
Expected: FAIL with "undefined: EpistemicConfig".

- [ ] **Step 3: Add config structs to schema.go**

In `internal/config/schema.go`, after the `MemoryConfig` struct definition (around line 526), add the `EpistemicConfig` and `AmbientExtractionConfig` structs exactly as specified in spec sections "Configuration / New MemoryConfig.Epistemic sub-config". Use json+toml tags. The structs are:

```go
type EpistemicConfig struct {
	AmbientExtraction    AmbientExtractionConfig `json:"ambient_extraction" toml:"ambient_extraction"`
	AutoTrustWeight      float64                 `json:"auto_trust_weight" toml:"auto_trust_weight"`
	DetectionThreshold   float64                 `json:"detection_threshold" toml:"detection_threshold"`
	ReviewPromptFrequency string                 `json:"review_prompt_frequency" toml:"review_prompt_frequency"`
	MaxPendingReviews    int                     `json:"max_pending_reviews" toml:"max_pending_reviews"`
}

type AmbientExtractionConfig struct {
	Enabled             bool     `json:"enabled" toml:"enabled"`
	ConfidenceThreshold float64  `json:"confidence_threshold" toml:"confidence_threshold"`
	MaxPerTurn          int      `json:"max_per_turn" toml:"max_per_turn"`
	ExcludeIntents      []string `json:"exclude_intents" toml:"exclude_intents"`
	ExcludeCategories   []string `json:"exclude_categories" toml:"exclude_categories"`
	ContextWindow       int      `json:"context_window" toml:"context_window"`
}
```

Then add the `Epistemic` field to `MemoryConfig` (after `ProjectOverrides`):

```go
	Epistemic EpistemicConfig `json:"epistemic" toml:"epistemic"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestEpistemic -v`
Expected: PASS.

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/config/schema.go internal/config/epistemic_config_test.go
git commit -m "feat(config): add EpistemicConfig and AmbientExtractionConfig"
```

---

## Task 3: ClaimStatus, TrustWeight, and Epistemic Type Structs

**Files:**
- Create: `internal/memory/epistemic.go`
- Test: `internal/memory/epistemic_test.go`

### Steps

- [ ] **Step 1: Write failing tests for ClaimStatus.TrustWeight**

Create `internal/memory/epistemic_test.go` with tests covering: (a) `ClaimStatusConfirmed/Promoted → 1.0`, (b) `ClaimStatusAuto` with various autoWeight values (0.5, 0, 1.5, -0.1, 0.8), (c) `ClaimStatusRejected → 0.0`, (d) unknown status → 0.0, (e) `EffectiveAutoTrustWeight` zero/out-of-range/negative substitution, (f) `IsEpistemicType` true for claim/decision/prediction/question and false for episodic. Refer to spec section "ClaimStatus enum and trust weight" for the exact method signature. Full test code:

```go
package memory

import (
	"context"
	"testing"
	"time"
)

func TestClaimStatusTrustWeight(t *testing.T) {
	cases := []struct {
		status     ClaimStatus
		autoWeight float64
		want       float64
	}{
		{ClaimStatusConfirmed, 0.5, 1.0},
		{ClaimStatusPromoted, 0.5, 1.0},
		{ClaimStatusAuto, 0.5, 0.5},
		{ClaimStatusAuto, 0.0, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, 1.5, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, -0.1, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, 0.8, 0.8},
		{ClaimStatusRejected, 0.5, 0.0},
		{ClaimStatus("bogus"), 0.5, 0.0},
	}
	for _, c := range cases {
		got := c.status.TrustWeight(c.autoWeight)
		if got != c.want {
			t.Errorf("status=%q autoWeight=%v: got %v, want %v", c.status, c.autoWeight, got, c.want)
		}
	}
}

func TestEffectiveAutoTrustWeight(t *testing.T) {
	if EffectiveAutoTrustWeight(0) != DefaultAutoClaimTrustWeight {
		t.Error("zero value should yield default")
	}
	if EffectiveAutoTrustWeight(0.9) != 0.9 {
		t.Error("explicit value should pass through")
	}
	if EffectiveAutoTrustWeight(1.5) != DefaultAutoClaimTrustWeight {
		t.Error("out-of-range should yield default")
	}
}

func TestIsEpistemicType(t *testing.T) {
	for _, mt := range []MemoryType{MemoryTypeClaim, MemoryTypeDecision, MemoryTypePrediction, MemoryTypeQuestion} {
		if !IsEpistemicType(mt) {
			t.Errorf("%q should be epistemic", mt)
		}
	}
	if IsEpistemicType(MemoryTypeEpisodic) {
		t.Error("episodic should not be epistemic")
	}
}

var _ = context.Background
var _ = time.Now
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run "TestClaimStatus|TestEffectiveAuto|TestIsEpistemicType" -v`
Expected: FAIL with "undefined: ClaimStatus".

- [ ] **Step 3: Create epistemic.go**

Create `internal/memory/epistemic.go` implementing:
- `ClaimStatus` string type with four constants (`ClaimStatusConfirmed`, `ClaimStatusAuto`, `ClaimStatusPromoted`, `ClaimStatusRejected`)
- `DefaultAutoClaimTrustWeight = 0.5`
- `EffectiveAutoTrustWeight(configured float64) float64` — returns default for zero/negative/>1
- `(s ClaimStatus) TrustWeight(autoWeight float64) float64` — confirmed/promoted=1.0, auto=EffectiveAutoTrustWeight(autoWeight), rejected=0.0
- `(s ClaimStatus) IsRejected() bool` and `IsEligibleCanonical() bool` (true for confirmed/promoted)
- `EpistemicMemTypes` slice and `IsEpistemicType(t MemoryType) bool`
- Struct types `Claim`, `Decision`, `Prediction`, `Question` with fields per spec section "Structured types"
- Helper `asString(v any) string` for metadata extraction
- Helper `stringTokenSet(s string) map[string]struct{}` and `outcomeOverlapScore(expected, actual string) float64` (Jaccard token overlap)

Use imports: `context`, `errors`, `fmt`, `strings`, `time`. Add `var _ = context.Background` etc. to suppress unused-import warnings until later tasks.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/ -run "TestClaimStatus|TestEffectiveAuto|TestIsEpistemicType" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/epistemic.go internal/memory/epistemic_test.go
git commit -m "feat(memory): add ClaimStatus, TrustWeight, and epistemic type structs"
```

---

## Task 4: Manager Helpers — StoreClaim/Decision/Prediction/Question

**Files:**
- Modify: `internal/memory/epistemic.go` (extend)
- Test: `internal/memory/epistemic_test.go` (extend)

### Steps

- [ ] **Step 1: Write failing tests**

Append to `internal/memory/epistemic_test.go`. Each test creates an uninitialized `Manager` via `NewManager(ManagerConfig{})` and asserts the helper returns an error:

```go
func TestStoreClaimRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreClaim(context.Background(), Claim{Text: "x", Status: ClaimStatusConfirmed}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}
```

Repeat for `StoreDecision`, `StorePrediction`, `StoreQuestion`. Each should fail because `m.initialized == false` causes `Store` to return an error.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run "TestStoreClaim|TestStoreDecision|TestStorePrediction|TestStoreQuestion" -v`
Expected: FAIL with "undefined: StoreClaim".

- [ ] **Step 3: Implement StoreClaim/StoreDecision/StorePrediction/StoreQuestion**

Append to `internal/memory/epistemic.go`. Each method builds a `Memory{Type: MemoryTypeX, Category: "x", Content: ..., Metadata: map[string]any{...}}` and calls `m.Store(ctx, mem)`. See spec section "Storage functions" for exact metadata keys. `StoreClaim` sets `status`, `confidence`, `source`, optional `premises` and `tags`. `StoreDecision` sets `status="open"`, `expected_outcome`, optional `alternatives`, `review_at` (RFC3339), `premises`. `StorePrediction` sets `horizon` (RFC3339), optional `related_decision`. `StoreQuestion` sets `status="open"`, optional `related_claims`, `answer_claim`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/ -run "TestStoreClaim|TestStoreDecision|TestStorePrediction|TestStoreQuestion" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/epistemic.go internal/memory/epistemic_test.go
git commit -m "feat(memory): add StoreClaim/Decision/Prediction/Question helpers"
```

---

## Task 5: Promotion, Rejection, Listing, and Canonical Lookup

**Files:**
- Modify: `internal/memory/epistemic.go`
- Test: `internal/memory/epistemic_test.go`

### Steps

- [ ] **Step 1: Write failing tests**

Append to `internal/memory/epistemic_test.go`. Tests call each method on an uninitialized Manager and assert errors:

```go
func TestPromoteRejectUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if err := m.PromoteClaim(context.Background(), "x"); err == nil {
		t.Error("PromoteClaim should fail on uninitialized manager")
	}
	if err := m.RejectClaim(context.Background(), "x"); err == nil {
		t.Error("RejectClaim should fail on uninitialized manager")
	}
	if _, err := m.ListAutoClaims(context.Background(), time.Now(), 10); err == nil {
		t.Error("ListAutoClaims should fail on uninitialized manager")
	}
	if _, _, err := m.ListPendingReviews(context.Background(), time.Now()); err == nil {
		t.Error("ListPendingReviews should fail on uninitialized manager")
	}
	if _, err := m.FindCanonicalFor(context.Background(), "topic"); err == nil {
		t.Error("FindCanonicalFor should fail on uninitialized manager")
	}
}

func TestOutcomeOverlapScore(t *testing.T) {
	if got := outcomeOverlapScore("same", "same"); got != 1.0 {
		t.Errorf("identical: got %v, want 1.0", got)
	}
	if got := outcomeOverlapScore("a", "b"); got != 0.0 {
		t.Errorf("disjoint: got %v, want 0.0", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run "TestPromoteReject|TestOutcomeOverlap" -v`
Expected: FAIL with "undefined: PromoteClaim".

- [ ] **Step 3: Implement helpers**

Append to `internal/memory/epistemic.go`:
- `PromoteClaim(ctx, claimID) error` — GetByID, check type and status, call `updateMetadataField(ctx, id, "status", "promoted")`
- `RejectClaim(ctx, claimID) error` — GetByID, check type, call `updateMetadataField`
- `ListAutoClaims(ctx, createdAfter, limit) ([]MemoryResult, error)` — under RLock, check initialized, use `m.episodic.Search(ctx, "", limit*4)`, filter by Type==Claim and Metadata["status"]=="auto"
- `ListPendingReviews(ctx, before) (decisions, predictions []MemoryResult, err error)` — scan recent memories, filter by Decision (review_at <= before, status==open) and Prediction (horizon <= before)
- `FindCanonicalFor(ctx, topic) (*Memory, error)` — search by topic, return first eligible claim with `canonical_for == topic`, else first eligible claim
- `updateMetadataField(ctx, id, key, value) error` — GetByID, set metadata key, StoreVersioned with CreateVersion=true

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/ -run "TestPromoteReject|TestOutcomeOverlap" -v`
Expected: PASS.

- [ ] **Step 5: Run the whole memory package**

Run: `go test ./internal/memory/ -v`
Expected: PASS for all.

- [ ] **Step 6: Commit**

```bash
git add internal/memory/epistemic.go internal/memory/epistemic_test.go
git commit -m "feat(memory): add Promote/Reject/List/Pending/Canonical helpers"
```

---

## Task 6: MarkSuperseded, MarkResolved, RecordReview + EdgeCountForMemory

**Files:**
- Modify: `internal/memory/epistemic.go`
- Modify: `internal/memory/graph.go` (add EdgeCountForMemory)
- Test: `internal/memory/epistemic_test.go`

### Steps

- [ ] **Step 1: Write failing tests**

Append to `internal/memory/epistemic_test.go`:

```go
func TestMarkSupersededUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.MarkSuperseded(context.Background(), "a", "b"); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestMarkResolvedUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.MarkResolved(context.Background(), "x", "outcome"); err == nil {
		t.Error("expected error")
	}
}

func TestRecordReviewUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.RecordReview(context.Background(), "x", "actual"); err == nil {
		t.Error("expected error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run "TestMarkSuperseded|TestMarkResolved|TestRecordReview" -v`
Expected: FAIL.

- [ ] **Step 3: Add EdgeCountForMemory to graph.go**

In `internal/memory/graph.go`, after `GetRelatedMemoryIDs` (around line 372), add:

```go
func (g *KnowledgeGraph) EdgeCountForMemory(ctx context.Context, memoryID string) (int, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return 0, errors.New("knowledge graph not initialized")
	}
	pool := g.pool
	g.mu.RUnlock()

	db := pool.Get(ctx)
	if db == nil {
		return 0, errors.New("database unavailable")
	}
	defer pool.Put(db)

	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_edges WHERE source_id = ? OR target_id = ?`,
		memoryID, memoryID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count edges: %w", err)
	}
	return count, nil
}
```

- [ ] **Step 4: Implement MarkSuperseded/MarkResolved/RecordReview**

Append to `internal/memory/epistemic.go`. Add `crypto/rand` and `slices` to imports. Implement per spec sections "MarkSuperseded", "MarkResolved", "RecordReview":
- `MarkSuperseded`: validate both memories exist, enforce auto-cannot-supersede-confirmed, `markVersionNonCurrent(oldID)`, `graph.AddEdge(superseded)`, redirect `evidence_for`/`evidence_against` edges via `GetEdgesForMemory`
- `MarkResolved`: update metadata with `outcome` and `resolved_at`, `StoreVersioned`
- `RecordReview`: compute `outcomeOverlapScore`, update metadata with `actual_outcome`, `review_score`, `status="reviewed"`, `StoreVersioned`
- `generateEdgeID()`: crypto/rand-based 8-byte hex prefix

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/memory/ -run "TestMarkSuperseded|TestMarkResolved|TestRecordReview" -v`
Expected: PASS.

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/memory/epistemic.go internal/memory/epistemic_test.go internal/memory/graph.go
git commit -m "feat(memory): add MarkSuperseded/MarkResolved/RecordReview and EdgeCountForMemory"
```

---

## Task 7: Confirmation Protocol Helpers

**Files:**
- Create: `internal/tools/builtin/confirmation.go`
- Test: `internal/tools/builtin/confirmation_test.go`

### Steps

- [ ] **Step 1: Write failing tests**

Create `internal/tools/builtin/confirmation_test.go` with tests for:
- `ConfirmationResponse("mark_superseded", true, "summary", map)` returns map with `requires_confirmation: true`, `action`, `reversible`, `summary`, `details`, `confirm_arg: "confirmed"`
- `IsConfirmationRequest` returns true only when `requires_confirmation` is bool `true`
- `DeclineResponse(orig)` returns map with `declined: true`, `action`, `summary`, `user_note`

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/builtin/ -run "TestConfirmation|TestIsConfirmation|TestDecline" -v`
Expected: FAIL.

- [ ] **Step 3: Create confirmation.go**

Create `internal/tools/builtin/confirmation.go` implementing the three helpers per spec section "Confirmation Protocol / Shared helper". The package declaration is `package builtin`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/builtin/ -run "TestConfirmation|TestIsConfirmation|TestDecline" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/builtin/confirmation.go internal/tools/builtin/confirmation_test.go
git commit -m "feat(tools): add two-phase confirmation protocol helpers"
```

---

## Task 8: Path A Tools (retain_claim, retain_decision, retain_prediction)

**Files:**
- Create: `internal/tools/builtin/retain_typed.go`
- Test: `internal/tools/builtin/retain_typed_test.go`

### Steps

- [ ] **Step 1: Write failing tests**

Create `internal/tools/builtin/retain_typed_test.go` with tests for:
- `NewRetainClaimTool(nil).Execute(ctx, {"text":"x"})` returns error (nil manager)
- `NewRetainClaimTool(&memory.Manager{}).Execute(ctx, {})` returns error (missing text)
- Metadata: `Name()`, `Category()`, `Description()`, `Parameters().Type` for all three tools
- Same pattern for `NewRetainDecisionTool` and `NewRetainPredictionTool`

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/builtin/ -run "TestRetainClaim|TestRetainDecision|TestRetainPrediction" -v`
Expected: FAIL.

- [ ] **Step 3: Create retain_typed.go**

Create `internal/tools/builtin/retain_typed.go`. Three tool structs, each implementing the tool interface (`Name`, `Category`, `Description`, `Parameters`, `Execute`). Each calls the corresponding Manager helper (`StoreClaim`, `StoreDecision`, `StorePrediction`). Use the `llm.FunctionParameters` and `llm.ParameterProperty` types from the existing `internal/tools/builtin/memory.go` pattern. Include helpers `toStringSlice(v any) []string` and `asStringArg(v any) string` for JSON arg extraction. Schema constants (`schemaTypeObject`, `schemaTypeString`, etc.) already exist in the package.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/builtin/ -run "TestRetainClaim|TestRetainDecision|TestRetainPrediction" -v`
Expected: PASS.

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/builtin/retain_typed.go internal/tools/builtin/retain_typed_test.go
git commit -m "feat(tools): add retain_claim, retain_decision, retain_prediction (Path A)"
```

---

## Task 9: Destructive Tools with Two-Phase Confirmation

**Files:**
- Create: `internal/tools/builtin/epistemic_actions.go`
- Test: `internal/tools/builtin/epistemic_actions_test.go`

### Steps

- [ ] **Step 1: Write failing tests**

Create `internal/tools/builtin/epistemic_actions_test.go` with tests for all five tools (`NewMarkSupersededTool`, `NewMarkResolvedTool`, `NewRecordReviewTool`, `NewRejectClaimTool`, `NewPurgeAutoClaimsTool`):
- Name/Category/Description/Parameters metadata checks
- Nil-manager returns error
- Missing required args returns error
- Also test `truncatePreview("short", 10)` returns "short", `truncatePreview("this is a long string", 10)` returns "this is..."

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/builtin/ -run "TestMarkSuperseded|TestMarkResolved|TestRecordReview|TestRejectClaim|TestPurgeAuto|TestTruncate" -v`
Expected: FAIL.

- [ ] **Step 3: Create epistemic_actions.go**

Create `internal/tools/builtin/epistemic_actions.go` implementing five destructive tools. Each follows the two-phase pattern: when `confirmed` arg is false/absent, return `ConfirmationResponse(...)` with a preview; when true, call the Manager method. Define local interface `epistemicGraph` with `EdgeCountForMemory(ctx, memoryID) (int, error)` (matching the method name added in Task 6) so `MarkSupersededTool` can accept the graph without a hard dep. Add `truncatePreview(s string, maxLen int) string` helper. Use `strings` and `time` imports. Schema constants follow existing `internal/tools/builtin` conventions. See spec section "Destructive tools" and the `mark_superseded` example for exact behavior.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/builtin/ -run "TestMarkSuperseded|TestMarkResolved|TestRecordReview|TestRejectClaim|TestPurgeAuto|TestTruncate" -v`
Expected: PASS.

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/builtin/epistemic_actions.go internal/tools/builtin/epistemic_actions_test.go
git commit -m "feat(tools): add destructive epistemic tools with two-phase confirmation"
```

---

## Task 10: Epistemic Detector (LLM-driven relationship detection)

**Files:**
- Create: `internal/memory/epistemic_detection.go`
- Test: `internal/memory/epistemic_detection_test.go`

### Steps

- [ ] **Step 1: Write failing test**

Create `internal/memory/epistemic_detection_test.go`:

```go
package memory

import (
	"context"
	"testing"
)

func TestEpistemicDetectorZeroValue(t *testing.T) {
	d := &EpistemicDetector{}
	edges, err := d.DetectRelationships(context.Background(), Memory{
		Type:    MemoryTypeClaim,
		Content: "x",
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestPotentialContradictionThreshold(t *testing.T) {
	if PotentialContradictionThreshold >= DefaultDetectionThreshold {
		t.Errorf("potential (%v) should be < default (%v)",
			PotentialContradictionThreshold, DefaultDetectionThreshold)
	}
}

func TestDetectRelationshipsNonEpistemic(t *testing.T) {
	d := &EpistemicDetector{}
	edges, _ := d.DetectRelationships(context.Background(), Memory{Type: MemoryTypeEpisodic})
	if len(edges) != 0 {
		t.Errorf("non-epistemic type should yield 0 edges, got %d", len(edges))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run "TestEpistemicDetector|TestPotentialContradiction|TestDetectRelationshipsNonEpistemic" -v`
Expected: FAIL.

- [ ] **Step 3: Create epistemic_detection.go**

Create `internal/memory/epistemic_detection.go` with:
- `DefaultDetectionThreshold = 0.7` and `PotentialContradictionThreshold = 0.4` constants
- `ClassifierLLM` interface with `ClassifyRelationships(ctx, newMem Memory, candidates []Memory) ([]edgeVerdict, error)` — defined locally to avoid import cycle
- `edgeVerdict` struct: `Relation string`, `Confidence float64`, `Explanation string`
- `EpistemicDetector` struct with `graph`, `manager`, `classifier`, `embedder`, `threshold`, `autoWeight`, `logger` fields
- `NewEpistemicDetector(cfg EpistemicDetectorConfig) *EpistemicDetector`
- `DetectRelationships(ctx, newMem) ([]MemoryEdge, error)`: gate on classifier/manager nil → return nil; gate on `IsEpistemicType`; Search candidates (Limit:10); filter rejected; call classifier; build edges with threshold/potential routing
- `PersistCandidateEdges(ctx, edges)`: write via `graph.AddEdge`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/ -run "TestEpistemicDetector|TestPotentialContradiction|TestDetectRelationshipsNonEpistemic" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/epistemic_detection.go internal/memory/epistemic_detection_test.go
git commit -m "feat(memory): add EpistemicDetector for LLM-driven relationship detection"
```

---

## Task 11: Ambient Extractor (Path B)

**Files:**
- Create: `internal/memory/epistemic_ambient.go`
- Test: `internal/memory/epistemic_ambient_test.go`

### Steps

- [ ] **Step 1: Write failing test**

Create `internal/memory/epistemic_ambient_test.go`:

```go
package memory

import (
	"context"
	"testing"
)

func TestAmbientExtractorZeroValue(t *testing.T) {
	ex := &AmbientExtractor{}
	// With nil manager/classifier, extract returns no candidates and no error.
	candidates, err := ex.Extract(context.Background(), []string{"hello world"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

func TestAmbientExtractionCandidateFields(t *testing.T) {
	c := AmbientCandidate{Type: "claim", Text: "x", Confidence: 0.8}
	if c.Type != "claim" || c.Text != "x" || c.Confidence != 0.8 {
		t.Error("candidate fields did not round-trip")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run "TestAmbientExtractor|TestAmbientExtraction" -v`
Expected: FAIL.

- [ ] **Step 3: Create epistemic_ambient.go**

Create `internal/memory/epistemic_ambient.go` with:
- `AmbientCandidate` struct: `Type string` (claim/decision/prediction), `Text string`, `Source string`, `Confidence float64`, `Premises []string`, `Category string`
- `AmbientExtractor` struct: `classifier ClassifierLLM`, `manager *Manager`, `cfg config.AmbientExtractionConfig`, `logger`
- Note: importing `internal/config` here creates no cycle because `memory` already imports it via `manager.go`.
- `Extract(ctx, messages []string) ([]AmbientCandidate, error)`: gate on nil classifier → return nil; build LLM prompt per spec section "Path B: Ambient extraction / LLM prompt template"; call `classifier.ClassifyRelationships`-equivalent (we reuse a generic LLM call); parse JSON array of candidates; filter by `cfg.ConfidenceThreshold`; filter by `cfg.ExcludeCategories`; cap to `cfg.MaxPerTurn`
- `WriteCandidates(ctx, candidates) ([]string, error)`: for each candidate, call `manager.StoreClaim` with `Status: ClaimStatusAuto`, return IDs

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/ -run "TestAmbientExtractor|TestAmbientExtraction" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/epistemic_ambient.go internal/memory/epistemic_ambient_test.go
git commit -m "feat(memory): add AmbientExtractor for post-turn claim mining (Path B)"
```

---

## Task 12: Post-Turn Epistemic Hook (Path B wiring)

**Files:**
- Create: `internal/agent/epistemic_hook.go`
- Test: `internal/agent/epistemic_hook_test.go`

### Steps

- [ ] **Step 1: Write failing test**

Create `internal/agent/epistemic_hook_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestEpistemicHookDisabledByDefault(t *testing.T) {
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg: config.EpistemicConfig{}, // AmbientExtraction.Enabled = false
	})
	// Should return immediately with no action.
	written, err := hook.AfterTurn(context.Background(), "chat", []string{"hello"})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if len(written) != 0 {
		t.Errorf("expected 0 written, got %d", len(written))
	}
}

func TestEpistemicHookExcludesIntent(t *testing.T) {
	hook := NewEpistemicHook(EpistemicHookConfig{
		Cfg: config.EpistemicConfig{
			AmbientExtraction: config.AmbientExtractionConfig{
				Enabled:        true,
				ExcludeIntents: []string{"chat"},
			},
		},
	})
	written, _ := hook.AfterTurn(context.Background(), "chat", []string{"hello"})
	if len(written) != 0 {
		t.Errorf("chat intent should be excluded, got %d written", len(written))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "TestEpistemicHook" -v`
Expected: FAIL.

- [ ] **Step 3: Create epistemic_hook.go**

Create `internal/agent/epistemic_hook.go` with:
- `EpistemicHookConfig` struct: `Cfg config.EpistemicConfig`, `Extractor AmbientExtractorInterface`, `Logger`
- `EpistemicHook` struct holding the config
- `NewEpistemicHook(cfg EpistemicHookConfig) *EpistemicHook`
- `(h *EpistemicHook) AfterTurn(ctx, intent string, messages []string) ([]string, error)`:
  1. Gate: if `!h.Cfg.AmbientExtraction.Enabled`, return nil
  2. Gate: if intent in `ExcludeIntents`, return nil
  3. If Extractor is nil, return nil
  4. Cap messages to `ContextWindow`
  5. Call `Extractor.Extract(ctx, messages)`
  6. Call `Extractor.WriteCandidates(ctx, candidates)`
  7. Return IDs

Define `AmbientExtractorInterface` locally to avoid import cycle (the `memory.AmbientExtractor` satisfies it):
```go
type AmbientExtractorInterface interface {
	Extract(ctx context.Context, messages []string) ([]AmbientCandidate, error)
	WriteCandidates(ctx context.Context, candidates []AmbientCandidate) ([]string, error)
}
```
Note: `AmbientCandidate` must be re-exposed or the interface methods changed to return generic types. Easiest path: define the interface in terms of `memory.AmbientCandidate` and import `memory` (agent already imports it).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run "TestEpistemicHook" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/epistemic_hook.go internal/agent/epistemic_hook_test.go
git commit -m "feat(agent): add post-turn epistemic hook for ambient extraction"
```

---

## Task 13: Wire Manager Store Post-Hook for Detection

**Files:**
- Modify: `internal/memory/manager.go` (add detector field, setter, post-Store hook)
- Test: `internal/memory/epistemic_test.go` (extend)

### Steps

- [ ] **Step 1: Write failing test**

Append to `internal/memory/epistemic_test.go`:

```go
func TestManagerSetEpistemicDetector(t *testing.T) {
	m := NewManager(ManagerConfig{})
	// Setter should accept nil without panic (defense in depth).
	m.SetEpistemicDetector(nil)
	// Setter should accept a real detector.
	d := NewEpistemicDetector(EpistemicDetectorConfig{})
	m.SetEpistemicDetector(d)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -run TestManagerSetEpistemicDetector -v`
Expected: FAIL with "undefined: SetEpistemicDetector".

- [ ] **Step 3: Add detector field and setter**

In `internal/memory/manager.go`:
- Add field to Manager struct: `detector *EpistemicDetector`
- Add setter: `func (m *Manager) SetEpistemicDetector(d *EpistemicDetector) { if d != nil { m.detector = d } }`

- [ ] **Step 4: Add post-Store hook**

In `Store` method, after `id, err := m.storeViaSQLite(ctx, mem)` succeeds (or the memvid path), add:

```go
if m.detector != nil && IsEpistemicType(mem.Type) && id != "" {
    mem.ID = id
    go func() {
        edges, derr := m.detector.DetectRelationships(context.Background(), mem)
        if derr != nil || len(edges) == 0 {
            return
        }
        _ = m.detector.PersistCandidateEdges(context.Background(), edges)
    }()
}
```

The goroutine is intentional: detection is best-effort and should not block Store. Use a fresh `context.Background()` to avoid caller cancellation.

**Note on the second wiring point:** The spec also mentions running detection during `Consolidator.Run` for a full pass over all epistemic memories added since the last consolidation. This is a future enhancement — the per-Store hook covers the primary path. Document this as a TODO in a code comment but don't implement it in this task.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/memory/ -run TestManagerSetEpistemicDetector -v`
Expected: PASS.

- [ ] **Step 6: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/memory/manager.go internal/memory/epistemic_test.go
git commit -m "feat(memory): wire post-Store detection hook for epistemic memories"
```

---

## Task 14: Deepen MemoryReflectTool

**Files:**
- Modify: `internal/tools/builtin/memory.go` (rewrite `MemoryReflectTool.Execute`)
- Test: `internal/tools/builtin/memory.go` test file (extend)

### Steps

- [ ] **Step 1: Write failing test**

In the existing `internal/tools/builtin/memory_test.go` (or create one), add:

```go
func TestMemoryReflectToolNilManager(t *testing.T) {
	tool := NewMemoryReflectTool(nil, nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": "x"}); err == nil {
		t.Error("expected error for nil manager")
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes (verify current behavior)**

Run: `go test ./internal/tools/builtin/ -run TestMemoryReflectToolNil -v`
Expected: behavior depends on existing code; should pass after we ensure error path.

- [ ] **Step 3: Rewrite MemoryReflectTool.Execute**

In `internal/tools/builtin/memory.go`, deepen `MemoryReflectTool.Execute` per spec section "Deepened ReflectTool". The new implementation:
1. Gathers recent epistemic memories (Claim/Decision/Prediction/Question) via `manager.Search` with `Type: MemoryTypeClaim`
2. Walks `contradicts`/`superseded`/`potential_contradicts` edges via `manager.graph.GetEdgesForMemory` (add a getter or pass graph to tool)
3. Calls `manager.ListPendingReviews(ctx, time.Now())` for decisions/predictions due
4. Calls `manager.ListAutoClaims(ctx, since, limit)` for auto-claim candidates
5. Calls LLM for theme synthesis (existing LLM client) using the spec prompt
6. Returns the structured JSON shape per spec section "New return shape"

Add `graph` field to `MemoryReflectTool` (type `*memory.KnowledgeGraph`) and update `NewMemoryReflectTool` signature to accept it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/builtin/ -run TestMemoryReflectTool -v`
Expected: PASS.

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/builtin/memory.go internal/tools/builtin/memory_test.go
git commit -m "feat(tools): deepen MemoryReflectTool with epistemic themes and contradictions"
```

---

## Task 15: TUI Confirmation Modal

**Files:**
- Create: `internal/tui/confirmation.go`
- Test: `internal/tui/confirmation_test.go`

### Steps

- [ ] **Step 1: Write failing test**

Create `internal/tui/confirmation_test.go` with tests for:
- `NewConfirmationModel(response map[string]any)` creates a model with the action and summary fields populated
- `Init()` returns nil
- `Update(msg)` handles `y` (confirm), `n` (cancel), `esc` (cancel)
- `View()` renders the action title and summary

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run "TestConfirmation" -v`
Expected: FAIL.

- [ ] **Step 3: Create confirmation.go**

Create `internal/tui/confirmation.go` implementing a bubbletea Model:
- `ConfirmationModel` struct: `response map[string]any`, `confirmed bool`, `cancelled bool`
- `NewConfirmationModel(response) ConfirmationModel`
- `Init() tea.Cmd` returns nil
- `Update(msg) (tea.Model, tea.Cmd)`: on key `y` → set confirmed=true, quit; `n` or `esc` → cancelled=true, quit
- `View() string`: render modal box per spec section "TUI (bubbletea)" mockup. Use lowercase for all button labels per CLAUDE.md UI conventions.
- `IsConfirmed() bool` and `IsCancelled() bool` accessors

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run "TestConfirmation" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/confirmation.go internal/tui/confirmation_test.go
git commit -m "feat(tui): add confirmation modal for destructive tool prompts"
```

---

## Task 16: Wire TUI Tool Dispatch Interceptor

**Files:**
- Modify: `internal/tui/tools.go` (or wherever tool results are returned to the UI)
- Test: `internal/tui/tools_test.go` (extend or create)

### Steps

- [ ] **Step 1: Write failing test**

Test that a tool result with `requires_confirmation: true` triggers the confirmation flow. This may require an integration-style test that mocks the tool executor.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestToolConfirmationInterceptor -v`
Expected: FAIL.

- [ ] **Step 3: Add interceptor to tool dispatch**

In the TUI tool dispatch site (where `tool.Execute(ctx, args)` results are processed), add per spec section "TUI (bubbletea) / Edit":

```go
result, err := tool.Execute(ctx, args)
if err != nil { return ... }
if resultMap, ok := result.(map[string]any); ok && builtin.IsConfirmationRequest(resultMap) {
    // Show modal, block for user input.
    confirmed := t.showConfirmationModal(resultMap)
    if !confirmed {
        return builtin.DeclineResponse(resultMap), nil
    }
    args["confirmed"] = true
    return tool.Execute(ctx, args)
}
return result, nil
```

The `showConfirmationModal` method creates a `ConfirmationModel`, runs it via `tea.NewProgram`, and returns the confirmed bool.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestToolConfirmationInterceptor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tools.go internal/tui/tools_test.go
git commit -m "feat(tui): intercept requires_confirmation tool results with modal"
```

---

## Task 17: Flutter Confirmation Dialog

**Files:**
- Create: `ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart`
- Modify: `ui/flutter_ui/lib/services/tool_runner.dart`

### Steps

- [ ] **Step 1: Create dialog widget**

Create `ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart` — a `StatefulWidget` that takes a `Map<String, dynamic> response` and renders:
- Action title (lowercase per CLAUDE.md)
- Summary text
- Details (old_preview, new_preview, affected_edges)
- Reversibility indicator
- Confirm (y) / Cancel (n) buttons

- [ ] **Step 2: Add interceptor to tool_runner.dart**

In `ui/flutter_ui/lib/services/tool_runner.dart`, add the interceptor per spec section "GUI (Flutter)":

```dart
final result = await toolRunner.execute(toolName, args);
if (result['requires_confirmation'] == true) {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (_) => DestructiveConfirmationDialog(response: result),
  );
  if (confirmed != true) {
    return ToolResult.declined(result);
  }
  return toolRunner.execute(toolName, {...args, 'confirmed': true});
}
return result;
```

- [ ] **Step 3: Verify Flutter build**

Run: `cd ui/flutter_ui && flutter build linux` (or macOS, depending on platform)
Expected: build succeeds.

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/widgets/destructive_confirmation_dialog.dart ui/flutter_ui/lib/services/tool_runner.dart
git commit -m "feat(flutter): add destructive confirmation dialog and tool interceptor"
```

---

## Task 18: Daemon Wiring (register all new tools + detector + hook)

**Files:**
- Modify: `internal/daemon/components.go` (around lines 3055-3075)

### Steps

- [ ] **Step 1: Register Path A tools**

In `internal/daemon/components.go`, inside the `if memoryMgr != nil && memoryMgr.IsInitialized()` block (around line 3063), add:

```go
registry.Register(builtin.NewRetainClaimTool(memoryMgr))
registry.Register(builtin.NewRetainDecisionTool(memoryMgr))
registry.Register(builtin.NewRetainPredictionTool(memoryMgr))
```

- [ ] **Step 2: Register destructive tools**

In the same block, add:

```go
registry.Register(builtin.NewMarkSupersededTool(memoryMgr, memoryMgr.Graph()))
registry.Register(builtin.NewMarkResolvedTool(memoryMgr))
registry.Register(builtin.NewRecordReviewTool(memoryMgr))
registry.Register(builtin.NewRejectClaimTool(memoryMgr))
registry.Register(builtin.NewPurgeAutoClaimsTool(memoryMgr))
```

Note: if `Manager` doesn't expose `Graph()`, add a `Graph() *KnowledgeGraph` getter to `manager.go`.

- [ ] **Step 3: Instantiate and wire EpistemicDetector**

After memory manager initialization, add:

```go
if graph := memoryMgr.Graph(); graph != nil && llmClient != nil {
    detector := memory.NewEpistemicDetector(memory.EpistemicDetectorConfig{
        Graph:     graph,
        Manager:   memoryMgr,
        Classifier: newClassifierAdapter(llmClient), // adapter satisfying memory.ClassifierLLM
        Threshold: memCfg.Epistemic.DetectionThreshold,
        AutoWeight: memory.EffectiveAutoTrustWeight(memCfg.Epistemic.AutoTrustWeight),
        Logger:    logger,
    })
    memoryMgr.SetEpistemicDetector(detector)
}
```

The `newClassifierAdapter` wraps `llm.Chatter` to satisfy `memory.ClassifierLLM`. Create it in `internal/memory/epistemic_detection.go` or `internal/daemon/components.go`.

- [ ] **Step 4: Instantiate and wire AmbientExtractor + EpistemicHook**

```go
if memCfg.Epistemic.AmbientExtraction.Enabled && llmClient != nil {
    extractor := memory.NewAmbientExtractor(memory.AmbientExtractorConfig{
        Manager:    memoryMgr,
        Classifier: newClassifierAdapter(llmClient),
        Cfg:        memCfg.Epistemic.AmbientExtraction,
        Logger:     logger,
    })
    hook := agent.NewEpistemicHook(agent.EpistemicHookConfig{
        Cfg:       memCfg.Epistemic,
        Extractor: extractor,
        Logger:    logger,
    })
    // Wire hook into agent loop's post-turn callback.
    agentLoop.SetEpistemicHook(hook)
}
```

If `AgentLoop` doesn't have `SetEpistemicHook`, add it as a setter following the existing pattern.

- [ ] **Step 5: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/components.go internal/memory/manager.go
git commit -m "feat(daemon): wire epistemic tools, detector, and ambient hook"
```

---

## Task 19: RPC Handlers

**Files:**
- Create: `internal/rpc/epistemic.go`
- Modify: `internal/rpc/proxy.go:58-63`

### Steps

- [ ] **Step 1: Create direct RPC handlers**

Create `internal/rpc/epistemic.go` with `RegisterEpistemicHandlers(server *Server, manager *memory.Manager)` following the pattern in `internal/rpc/skills.go`. Register these methods (each dispatches to the corresponding Manager method):
- `memory.retainClaim` → `manager.StoreClaim`
- `memory.retainDecision` → `manager.StoreDecision`
- `memory.retainPrediction` → `manager.StorePrediction`
- `memory.markSuperseded` → `manager.MarkSuperseded`
- `memory.markResolved` → `manager.MarkResolved`
- `memory.recordReview` → `manager.RecordReview`
- `memory.promoteClaim` → `manager.PromoteClaim`
- `memory.rejectClaim` → `manager.RejectClaim`
- `memory.listAutoClaims` → `manager.ListAutoClaims`
- `memory.listPendingReviews` → `manager.ListPendingReviews`
- `memory.findCanonical` → `manager.FindCanonicalFor`
- `memory.reviewQueue` → combines `manager.ListAutoClaims` + `manager.ListPendingReviews` into a single response `{auto_claims: [...], pending_decisions: [...], pending_predictions: [...]}`

- [ ] **Step 2: Register handlers in daemon wiring**

In `internal/daemon/components.go` (wherever RPC server is set up), add:

```go
if memoryMgr != nil && memoryMgr.IsInitialized() {
    rpc.RegisterEpistemicHandlers(rpcServer, memoryMgr)
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/rpc/epistemic.go internal/daemon/components.go
git commit -m "feat(rpc): add epistemic memory RPC handlers"
```

---

## Task 20: HTTP Endpoints

**Files:**
- Modify: `internal/comm/http/api_handlers.go`
- Modify: `internal/comm/http/config_service.go`

### Steps

- [ ] **Step 1: Add HTTP endpoints**

In `internal/comm/http/api_handlers.go`, add handlers for the endpoints listed in spec section "HTTP endpoints (new)". Each handler dispatches through `s.rpcCall` (the existing HTTP-to-RPC bridge pattern) to the corresponding RPC method from Task 19:

| Endpoint | Method | RPC Method |
|----------|--------|------------|
| `/api/v1/memory/claims` | POST | `memory.retainClaim` |
| `/api/v1/memory/claims/{id}/promote` | POST | `memory.promoteClaim` |
| `/api/v1/memory/claims/{id}/reject` | POST | `memory.rejectClaim` |
| `/api/v1/memory/decisions` | POST | `memory.retainDecision` |
| `/api/v1/memory/decisions/{id}/review` | POST | `memory.recordReview` |
| `/api/v1/memory/predictions` | POST | `memory.retainPrediction` |
| `/api/v1/memory/predictions/{id}/resolve` | POST | `memory.markResolved` |
| `/api/v1/memory/supersede` | POST | `memory.markSuperseded` |
| `/api/v1/memory/canonical` | GET | `memory.findCanonical` |
| `/api/v1/memory/review-queue` | GET | `memory.listPendingReviews` |
| `/api/v1/memory/auto-claims` | GET | `memory.listAutoClaims` |

- [ ] **Step 2: Register routes**

In the HTTP server setup (wherever routes are registered), add the new routes.

- [ ] **Step 3: Expose epistemic config**

In `internal/comm/http/config_service.go`, add the `Epistemic` sub-config to the memory config response so the menubar app can read ambient extraction status.

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/comm/http/api_handlers.go internal/comm/http/config_service.go
git commit -m "feat(http): add epistemic memory REST endpoints"
```

---

## Task 21: CLI Commands

**Files:**
- Modify: `cmd/meept/memory.go` (or wherever memory CLI commands live)

### Steps

- [ ] **Step 1: Add CLI subcommands**

Add these subcommands to the `meept memory` command group:
- `meept memory review` — calls `memory.listPendingReviews` and `memory.listAutoClaims`, renders a list with actions
- `meept memory supersede OLD NEW [--confirm]` — calls `memory.markSuperseded` (with `--confirm` skipping the prompt)
- `meept memory promote ID` — calls `memory.promoteClaim`
- `meept memory reject ID` — calls `memory.rejectClaim`

Each dispatches through the RPC client. For `supersede` without `--confirm`, render the preview text and prompt `Confirm? [y/N]`.

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/meept/`
Expected: no output.

- [ ] **Step 3: Verify CLI help**

Run: `./bin/meept memory --help`
Expected: new subcommands listed.

- [ ] **Step 4: Commit**

```bash
git add cmd/meept/memory.go
git commit -m "feat(cli): add memory review, supersede, promote, reject commands"
```

---

## Task 22: Tag Taxonomy Config File

**Files:**
- Create: `config/epistemic_tags.json5`

### Steps

- [ ] **Step 1: Create the config file**

Create `config/epistemic_tags.json5`:

```json5
{
  // Curated tag taxonomy for epistemic memories.
  // Users can extend via ~/.meept/epistemic_tags.json5.
  domains: ["architecture", "business", "technical", "methodology", "opinion", "prediction"],
  confidence_levels: ["speculative", "probable", "certain"],
  scopes: ["project", "personal", "industry", "universal"],
}
```

- [ ] **Step 2: Commit**

```bash
git add config/epistemic_tags.json5
git commit -m "feat(config): add curated epistemic tag taxonomy"
```

---

## Task 23: Documentation

**Files:**
- Create: `docs/configuration/epistemic-memory.md`
- Regenerate: `docs/reference/generated/`

### Steps

- [ ] **Step 1: Write config docs**

Create `docs/configuration/epistemic-memory.md` documenting every field in `EpistemicConfig` and `AmbientExtractionConfig` with default, range, and example. Reference the JSON5 config example from spec section "Configuration / example".

- [ ] **Step 2: Regenerate reference docs**

Run: `make docs-generate`
Expected: generated docs include the new `EpistemicConfig` struct.

- [ ] **Step 3: Update mkdocs.yml**

Add the new docs page to the mkdocs navigation.

- [ ] **Step 4: Commit**

```bash
git add docs/configuration/epistemic-memory.md docs/reference/generated/ mkdocs.yml
git commit -m "docs: add epistemic memory configuration page"
```

---

## Task 24: Full Build and Test Pass

### Steps

- [ ] **Step 1: Clean build**

Run: `go clean -cache && go build ./...`
Expected: no output.

- [ ] **Step 2: Full test suite**

Run: `go test ./... -v`
Expected: all PASS.

- [ ] **Step 3: Race detector**

Run: `go test -race ./internal/memory/... ./internal/tools/builtin/... ./internal/agent/...`
Expected: all PASS.

- [ ] **Step 4: Static analysis**

Run: `go vet ./...`
Expected: no issues.

- [ ] **Step 5: Commit any fixups**

If any issues were found and fixed:
```bash
git add -A
git commit -m "fix: address build/test issues from epistemic memory platform"
```

---

## Verification Checklist

Before marking this plan complete, verify:

- [ ] All new MemoryType and EdgeType constants are additive (no migrations)
- [ ] `ClaimStatus.TrustWeight` returns correct values for all four statuses
- [ ] Path A tools write `confirmed` claims; Path B writes `auto` claims
- [ ] All five destructive tools return `requires_confirmation: true` on phase 1
- [ ] TUI modal renders and handles `y`/`n`/`esc`
- [ ] Flutter dialog renders and returns confirmed/cancelled
- [ ] CLI `meept memory` subcommands work via RPC
- [ ] HTTP endpoints dispatch through the service layer
- [ ] EpistemicDetector gates on nil classifier/manager and non-epistemic types
- [ ] AmbientExtractor gates on `Enabled` and `ExcludeIntents`
- [ ] `MemoryReflectTool` returns the new structured shape
- [ ] `make docs-generate` succeeds with new config types
- [ ] `go test -race ./...` passes
