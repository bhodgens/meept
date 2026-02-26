package selfimprove

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLearningPipeline_Initialize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	if err := lp.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Should be idempotent
	if err := lp.Initialize(ctx); err != nil {
		t.Fatalf("second Initialize failed: %v", err)
	}

	stats := lp.GetStats()
	if stats["initialized"] != true {
		t.Error("expected initialized to be true")
	}
}

func TestLearningPipeline_JudgeHeuristic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	// Create a successful trajectory
	trajectory := Trajectory{
		ID:        "traj-001",
		SessionID: "sess-001",
		Domain:    "code",
		Steps: []TrajectoryStep{
			{Action: "read_file", Input: "main.go", Success: true},
			{Action: "edit_file", Input: "add function", Success: true},
			{Action: "run_tests", Input: "go test", Success: true},
		},
		Outcome: TrajectoryOutcome{
			Success:       true,
			Quality:       0.9,
			TaskCompleted: true,
		},
	}

	result, err := lp.Judge(ctx, trajectory)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if !result.ShouldStore {
		t.Error("expected ShouldStore to be true for successful trajectory")
	}

	if result.Quality < 0.7 {
		t.Errorf("expected quality >= 0.7, got %f", result.Quality)
	}
}

func TestLearningPipeline_JudgeHeuristic_Unsuccessful(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	// Create an unsuccessful trajectory
	trajectory := Trajectory{
		ID:        "traj-002",
		SessionID: "sess-001",
		Domain:    "code",
		Steps: []TrajectoryStep{
			{Action: "read_file", Input: "main.go", Success: true},
			{Action: "edit_file", Input: "bad change", Success: false},
			{Action: "run_tests", Input: "go test", Success: false},
		},
		Outcome: TrajectoryOutcome{
			Success:       false,
			Quality:       0.3,
			TaskCompleted: false,
		},
	}

	result, err := lp.Judge(ctx, trajectory)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if result.ShouldStore {
		t.Error("expected ShouldStore to be false for unsuccessful trajectory")
	}
}

func TestLearningPipeline_DistillHeuristic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	trajectory := Trajectory{
		ID:        "traj-001",
		SessionID: "sess-001",
		Domain:    "debugging",
		Steps: []TrajectoryStep{
			{Action: "read_error_log", Success: true},
			{Action: "analyze_stack_trace", Success: true},
			{Action: "identify_root_cause", Success: true},
			{Action: "apply_fix", Success: true},
		},
		Outcome: TrajectoryOutcome{
			Success: true,
			Quality: 0.85,
		},
	}

	judgment := &JudgmentResult{
		TrajectoryID:     trajectory.ID,
		Quality:          0.85,
		Generalizability: 0.7,
		ShouldStore:      true,
	}

	patterns, err := lp.Distill(ctx, trajectory, judgment)
	if err != nil {
		t.Fatalf("Distill failed: %v", err)
	}

	if len(patterns) == 0 {
		t.Error("expected at least one pattern to be extracted")
	}

	if len(patterns) > 0 {
		p := patterns[0]
		if p.Domain != "debugging" {
			t.Errorf("expected domain 'debugging', got '%s'", p.Domain)
		}
		if p.Confidence <= 0 {
			t.Error("expected positive confidence")
		}
	}
}

func TestLearningPipeline_StoreAndRetrieve(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	pattern := &LearnedPattern{
		ID:          "pat-001",
		Type:        PatternTypeStrategy,
		Status:      PatternStatusActive,
		Domain:      "code",
		Description: "Test-driven development approach",
		Pattern:     "Write tests first, then implement the feature, then refactor",
		Tags:        []string{"tdd", "testing", "development"},
		Confidence:  0.9,
		SuccessRate: 0.85,
		UseCount:    10,
		SuccessCount: 8,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContentHash: "test-hash",
	}

	if err := lp.StorePattern(ctx, pattern); err != nil {
		t.Fatalf("StorePattern failed: %v", err)
	}

	// Retrieve patterns
	results, err := lp.Retrieve(ctx, "testing development approach", "code", 5)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected to retrieve at least one pattern")
	}

	found := false
	for _, r := range results {
		if r.ID == "pat-001" {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected to find the stored pattern")
	}
}

func TestLearningPipeline_Consolidate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := DefaultLearningConfig()
	cfg.MinConfidence = 0.5
	lp := NewLearningPipeline(cfg, nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	// Directly set patterns to bypass StorePattern's dedup logic
	// This simulates patterns that may have been stored before dedup was added
	lp.mu.Lock()
	lp.patterns["pat-001"] = &LearnedPattern{
		ID:          "pat-001",
		Type:        PatternTypeStrategy,
		Status:      PatternStatusActive,
		Domain:      "code",
		Description: "Test first approach",
		Pattern:     "Write tests before implementation",
		Confidence:  0.9,
		SuccessRate: 0.85,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContentHash: "hash-001",
	}
	lp.patterns["pat-002"] = &LearnedPattern{
		ID:          "pat-002",
		Type:        PatternTypeStrategy,
		Status:      PatternStatusPending,
		Domain:      "code",
		Description: "Low confidence pattern",
		Pattern:     "Some uncertain approach",
		Confidence:  0.2, // Below threshold
		SuccessRate: 0.3,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContentHash: "hash-002",
	}
	lp.patterns["pat-003"] = &LearnedPattern{
		ID:          "pat-003",
		Type:        PatternTypeStrategy,
		Status:      PatternStatusActive,
		Domain:      "code",
		Description: "Duplicate pattern",
		Pattern:     "Write tests before implementation",
		Confidence:  0.7, // Lower than pat-001, should be removed
		SuccessRate: 0.7,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContentHash: "hash-001", // Same hash = duplicate
	}
	lp.mu.Unlock()

	result, err := lp.Consolidate(ctx)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}

	// Should have removed the duplicate (pat-003 has lower confidence)
	if result.DuplicatesRemoved != 1 {
		t.Errorf("expected 1 duplicate removed, got %d", result.DuplicatesRemoved)
	}

	// Should have pruned the low-confidence pattern
	if result.LowConfidencePruned != 1 {
		t.Errorf("expected 1 low confidence pruned, got %d", result.LowConfidencePruned)
	}

	// Should have 1 pattern remaining
	remaining := lp.GetPatterns()
	if len(remaining) != 1 {
		t.Errorf("expected 1 pattern remaining, got %d", len(remaining))
	}
}

func TestLearningPipeline_RecordPatternUse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	pattern := &LearnedPattern{
		ID:          "pat-001",
		Status:      PatternStatusActive,
		Domain:      "code",
		Confidence:  0.8,
		SuccessRate: 1.0,
		UseCount:    1,
		SuccessCount: 1,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ContentHash: "hash-001",
	}

	lp.StorePattern(ctx, pattern)

	// Record some uses
	lp.RecordPatternUse("pat-001", true)
	lp.RecordPatternUse("pat-001", true)
	lp.RecordPatternUse("pat-001", false)

	patterns := lp.GetPatterns()
	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}

	p := patterns[0]
	if p.UseCount != 4 {
		t.Errorf("expected UseCount 4, got %d", p.UseCount)
	}
	if p.SuccessCount != 3 {
		t.Errorf("expected SuccessCount 3, got %d", p.SuccessCount)
	}

	expectedRate := 3.0 / 4.0
	if p.SuccessRate != expectedRate {
		t.Errorf("expected SuccessRate %f, got %f", expectedRate, p.SuccessRate)
	}
}

func TestLearningPipeline_DomainFiltering(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	// Add patterns from different domains
	patterns := []*LearnedPattern{
		{
			ID:          "pat-001",
			Status:      PatternStatusActive,
			Domain:      "code",
			Description: "Code pattern",
			Pattern:     "Code approach",
			Confidence:  0.9,
			SuccessRate: 0.9,
			ContentHash: "hash-001",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "pat-002",
			Status:      PatternStatusActive,
			Domain:      "debugging",
			Description: "Debug pattern",
			Pattern:     "Debug approach",
			Confidence:  0.9,
			SuccessRate: 0.9,
			ContentHash: "hash-002",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
		{
			ID:          "pat-003",
			Status:      PatternStatusActive,
			Domain:      "general",
			Description: "General pattern",
			Pattern:     "General approach",
			Confidence:  0.9,
			SuccessRate: 0.9,
			ContentHash: "hash-003",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		},
	}

	for _, p := range patterns {
		lp.StorePattern(ctx, p)
	}

	// Retrieve for code domain - should get code + general
	results, err := lp.Retrieve(ctx, "approach", "code", 10)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 patterns for code domain, got %d", len(results))
	}

	// Retrieve for debugging domain - should get debugging + general
	results, err = lp.Retrieve(ctx, "approach", "debugging", 10)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 patterns for debugging domain, got %d", len(results))
	}

	// Retrieve with no domain filter - should get all
	results, err = lp.Retrieve(ctx, "approach", "", 10)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 patterns with no domain filter, got %d", len(results))
	}
}

func TestLearningPipeline_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// First instance
	lp1 := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)
	lp1.Initialize(ctx)

	pattern := &LearnedPattern{
		ID:          "pat-001",
		Status:      PatternStatusActive,
		Domain:      "code",
		Description: "Persistent pattern",
		Pattern:     "This should persist",
		Confidence:  0.9,
		SuccessRate: 0.9,
		ContentHash: "hash-001",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	lp1.StorePattern(ctx, pattern)
	lp1.Close()

	// Second instance - should load persisted patterns
	lp2 := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)
	lp2.Initialize(ctx)

	patterns := lp2.GetPatterns()
	if len(patterns) != 1 {
		t.Errorf("expected 1 persisted pattern, got %d", len(patterns))
	}

	if len(patterns) > 0 && patterns[0].ID != "pat-001" {
		t.Errorf("expected pattern ID 'pat-001', got '%s'", patterns[0].ID)
	}
}

func TestSimilarity(t *testing.T) {
	lp := &LearningPipeline{}

	tests := []struct {
		name     string
		a        string
		b        string
		expected float64
	}{
		{
			name:     "identical",
			a:        "write tests before code",
			b:        "write tests before code",
			expected: 1.0,
		},
		{
			name:     "similar",
			a:        "write tests before implementation",
			b:        "write tests before writing code",
			expected: 0.5, // ~50% overlap
		},
		{
			name:     "different",
			a:        "use dependency injection",
			b:        "write integration tests",
			expected: 0, // No meaningful overlap
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lp.similarity(tt.a, tt.b)
			// Allow some tolerance
			if result < tt.expected-0.2 || result > tt.expected+0.2 {
				t.Errorf("expected similarity ~%f, got %f", tt.expected, result)
			}
		})
	}
}

func TestLearningPipeline_FullPipeline(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "learning_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	lp := NewLearningPipeline(DefaultLearningConfig(), nil, tmpDir, nil)

	ctx := context.Background()
	lp.Initialize(ctx)

	// Run full pipeline: Judge -> Distill -> Store
	trajectory := Trajectory{
		ID:        "traj-001",
		SessionID: "sess-001",
		Domain:    "code",
		Steps: []TrajectoryStep{
			{Action: "analyze_requirements", Success: true},
			{Action: "design_solution", Success: true},
			{Action: "implement", Success: true},
			{Action: "test", Success: true},
		},
		Outcome: TrajectoryOutcome{
			Success: true,
			Quality: 0.9,
		},
	}

	// Step 1: Judge
	judgment, err := lp.Judge(ctx, trajectory)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if !judgment.ShouldStore {
		t.Skip("judgment says not to store, skipping rest of pipeline")
	}

	// Step 2: Distill
	patterns, err := lp.Distill(ctx, trajectory, judgment)
	if err != nil {
		t.Fatalf("Distill failed: %v", err)
	}

	if len(patterns) == 0 {
		t.Fatal("expected patterns to be extracted")
	}

	// Step 3: Store
	for _, p := range patterns {
		if err := lp.StorePattern(ctx, p); err != nil {
			t.Fatalf("StorePattern failed: %v", err)
		}
	}

	// Verify storage
	stored := lp.GetPatterns()
	if len(stored) == 0 {
		t.Error("expected patterns to be stored")
	}

	// Step 4: Consolidate
	result, err := lp.Consolidate(ctx)
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}

	if result.PatternsReviewed == 0 {
		t.Error("expected patterns to be reviewed during consolidation")
	}
}
