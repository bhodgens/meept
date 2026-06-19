// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/util/markdown"
)

// PatternType represents the type of learned pattern.
type PatternType string

const (
	PatternTypeStrategy    PatternType = "strategy"     // High-level approach
	PatternTypeTactic      PatternType = "tactic"       // Specific technique
	PatternTypeAntiPattern PatternType = "anti_pattern" // What NOT to do
	PatternTypeHeuristic   PatternType = "heuristic"    // Rule of thumb
)

// PatternStatus represents the lifecycle status of a pattern.
type PatternStatus string

const (
	PatternStatusPending    PatternStatus = "pending"    // Awaiting judgment
	PatternStatusActive     PatternStatus = "active"     // In use
	PatternStatusDeprecated PatternStatus = "deprecated" // Superseded
	PatternStatusRejected   PatternStatus = "rejected"   // Failed judgment
)

// LearnedPattern represents a pattern extracted from successful trajectories.
type LearnedPattern struct {
	ID           string         `json:"id"`
	Type         PatternType    `json:"type"`
	Status       PatternStatus  `json:"status"`
	Domain       string         `json:"domain"`        // e.g., "code", "debugging", "planning"
	Description  string         `json:"description"`   // Human-readable description
	Pattern      string         `json:"pattern"`       // The actual pattern/rule
	Examples     []string       `json:"examples"`      // Example applications
	Confidence   float64        `json:"confidence"`    // 0.0-1.0, how confident we are
	SuccessRate  float64        `json:"success_rate"`  // Historical success rate
	UseCount     int            `json:"use_count"`     // How many times used
	SuccessCount int            `json:"success_count"` // Successful applications
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	SupersededBy string         `json:"superseded_by,omitempty"` // ID of newer pattern
	ContentHash  string         `json:"content_hash"`            // For deduplication
	Tags         []string       `json:"tags"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Trajectory represents a sequence of actions and their outcome.
type Trajectory struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	Domain    string            `json:"domain"`
	Steps     []TrajectoryStep  `json:"steps"`
	Outcome   TrajectoryOutcome `json:"outcome"`
	StartedAt time.Time         `json:"started_at"`
	EndedAt   time.Time         `json:"ended_at"`
}

// TrajectoryStep represents a single step in a trajectory.
type TrajectoryStep struct {
	Action    string    `json:"action"`
	Input     string    `json:"input"`
	Output    string    `json:"output"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
}

// TrajectoryOutcome represents the outcome of a trajectory.
type TrajectoryOutcome struct {
	Success       bool    `json:"success"`
	Quality       float64 `json:"quality"`  // 0.0-1.0 quality score
	Feedback      string  `json:"feedback"` // User or system feedback
	TaskCompleted bool    `json:"task_completed"`
}

// JudgmentResult represents the result of evaluating a trajectory or pattern.
type JudgmentResult struct {
	TrajectoryID     string    `json:"trajectory_id,omitempty"`
	PatternID        string    `json:"pattern_id,omitempty"`
	Quality          float64   `json:"quality"`          // 0.0-1.0
	Correctness      float64   `json:"correctness"`      // Was the approach correct?
	Efficiency       float64   `json:"efficiency"`       // Was it efficient?
	Generalizability float64   `json:"generalizability"` // Can it be reused?
	ShouldStore      bool      `json:"should_store"`     // Should we store this?
	Reason           string    `json:"reason"`           // Explanation
	JudgedAt         time.Time `json:"judged_at"`
}

// ConsolidationResult represents the result of pattern consolidation.
type ConsolidationResult struct {
	PatternsReviewed    int           `json:"patterns_reviewed"`
	DuplicatesRemoved   int           `json:"duplicates_removed"`
	ContradictionsFound int           `json:"contradictions_found"`
	PatternsDeprecated  int           `json:"patterns_deprecated"`
	PatternsMerged      int           `json:"patterns_merged"`
	LowConfidencePruned int           `json:"low_confidence_pruned"`
	ConsolidatedAt      time.Time     `json:"consolidated_at"`
	Duration            time.Duration `json:"duration"`
}

// LearningConfig holds configuration for the learning pipeline.
type LearningConfig struct {
	// Minimum quality score to store a pattern (0.0-1.0)
	MinQualityThreshold float64 `json:"min_quality_threshold"`
	// Minimum confidence to keep a pattern during consolidation
	MinConfidence float64 `json:"min_confidence"`
	// Maximum patterns to retain per domain
	MaxPatternsPerDomain int `json:"max_patterns_per_domain"`
	// How often to run consolidation
	ConsolidationInterval time.Duration `json:"consolidation_interval"`
	// Similarity threshold for deduplication (0.0-1.0)
	SimilarityThreshold float64 `json:"similarity_threshold"`
	// Decay rate for pattern confidence over time
	ConfidenceDecayRate float64 `json:"confidence_decay_rate"`
}

// DefaultLearningConfig returns sensible defaults.
func DefaultLearningConfig() LearningConfig {
	return LearningConfig{
		MinQualityThreshold:   0.7,
		MinConfidence:         0.3,
		MaxPatternsPerDomain:  100,
		ConsolidationInterval: 24 * time.Hour,
		SimilarityThreshold:   0.85,
		ConfidenceDecayRate:   0.05, // 5% decay per day of non-use
	}
}

// LearningPipeline implements the RETRIEVE → JUDGE → DISTILL → CONSOLIDATE pipeline.
type LearningPipeline struct {
	mu        sync.RWMutex
	config    LearningConfig
	dataDir   string
	llmClient *llm.Client
	logger    *slog.Logger

	// In-memory caches
	patterns          map[string]*LearnedPattern
	lastConsolidation time.Time

	initialized bool
}

// NewLearningPipeline creates a new learning pipeline.
func NewLearningPipeline(cfg LearningConfig, llmClient *llm.Client, dataDir string, logger *slog.Logger) *LearningPipeline {
	if logger == nil {
		logger = slog.Default()
	}

	return &LearningPipeline{
		config:    cfg,
		dataDir:   dataDir,
		llmClient: llmClient,
		logger:    logger,
		patterns:  make(map[string]*LearnedPattern),
	}
}

// Initialize loads persisted patterns.
func (lp *LearningPipeline) Initialize(ctx context.Context) error {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	if lp.initialized {
		return nil
	}

	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(lp.dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	if err := lp.loadPatterns(); err != nil {
		lp.logger.Warn("Failed to load patterns", "error", err)
		// Continue with empty patterns
	}

	lp.initialized = true
	lp.logger.Info("Learning pipeline initialized", "patterns_loaded", len(lp.patterns))
	return nil
}

// Retrieve returns the top-k most relevant patterns for a query.
func (lp *LearningPipeline) Retrieve(ctx context.Context, query, domain string, k int) ([]*LearnedPattern, error) {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	if !lp.initialized {
		return nil, errors.New("learning pipeline not initialized")
	}

	// Filter by domain and status
	var candidates []*LearnedPattern
	for _, p := range lp.patterns {
		if p.Status != PatternStatusActive {
			continue
		}
		if domain != "" && p.Domain != domain && p.Domain != "general" {
			continue
		}
		candidates = append(candidates, p)
	}

	// Score by relevance (simple keyword matching + confidence)
	type scored struct {
		pattern *LearnedPattern
		score   float64
	}

	var scoredPatterns []scored
	queryWords := strings.Fields(strings.ToLower(query))

	for _, p := range candidates {
		// Calculate relevance score
		textToMatch := strings.ToLower(p.Description + " " + p.Pattern + " " + strings.Join(p.Tags, " "))
		matchCount := 0
		for _, word := range queryWords {
			if len(word) > 3 && strings.Contains(textToMatch, word) {
				matchCount++
			}
		}

		relevance := 0.0
		if len(queryWords) > 0 {
			relevance = float64(matchCount) / float64(len(queryWords))
		}

		// Combine with confidence and success rate
		score := 0.4*relevance + 0.3*p.Confidence + 0.3*p.SuccessRate
		scoredPatterns = append(scoredPatterns, scored{pattern: p, score: score})
	}

	// Sort by score descending
	sort.Slice(scoredPatterns, func(i, j int) bool {
		return scoredPatterns[i].score > scoredPatterns[j].score
	})

	// Apply MMR-style diversity (simplified)
	result := make([]*LearnedPattern, 0, k)
	selected := make(map[string]bool)

	for _, sp := range scoredPatterns {
		if len(result) >= k {
			break
		}

		// Skip if too similar to already selected
		isDiverse := true
		for _, existing := range result {
			if lp.similarity(sp.pattern.Pattern, existing.Pattern) > lp.config.SimilarityThreshold {
				isDiverse = false
				break
			}
		}

		if isDiverse && !selected[sp.pattern.ID] {
			result = append(result, sp.pattern)
			selected[sp.pattern.ID] = true
		}
	}

	return result, nil
}

// Judge evaluates a trajectory's quality and decides if it should be stored.
func (lp *LearningPipeline) Judge(ctx context.Context, trajectory Trajectory) (*JudgmentResult, error) {
	if lp.llmClient == nil {
		return lp.judgeHeuristic(trajectory), nil
	}

	// Use LLM to evaluate the trajectory
	prompt := lp.buildJudgmentPrompt(trajectory)

	messages := []llm.ChatMessage{
		{Role: "system", Content: judgmentSystemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := lp.llmClient.Chat(ctx, messages)
	if err != nil {
		lp.logger.Warn("LLM judgment failed, using heuristic", "error", err)
		return lp.judgeHeuristic(trajectory), nil
	}

	result, err := lp.parseJudgmentResponse(resp.Content, trajectory.ID)
	if err != nil {
		lp.logger.Warn("Failed to parse judgment response", "error", err)
		return lp.judgeHeuristic(trajectory), nil
	}

	return result, nil
}

const judgmentSystemPrompt = `You are an evaluator for AI agent trajectories. Analyze the given trajectory and rate it on:
1. Quality (0-1): Overall quality of the approach
2. Correctness (0-1): Was the solution correct?
3. Efficiency (0-1): Was it done efficiently?
4. Generalizability (0-1): Can this approach be reused?

Respond in JSON format:
{
  "quality": 0.8,
  "correctness": 0.9,
  "efficiency": 0.7,
  "generalizability": 0.6,
  "should_store": true,
  "reason": "Clear, correct approach that could help similar tasks"
}`

func (lp *LearningPipeline) buildJudgmentPrompt(trajectory Trajectory) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Domain: %s\n", trajectory.Domain)
	fmt.Fprintf(&sb, "Outcome: success=%v, quality=%.2f\n\n", trajectory.Outcome.Success, trajectory.Outcome.Quality)
	sb.WriteString("Steps:\n")

	for i, step := range trajectory.Steps {
		fmt.Fprintf(&sb, "%d. Action: %s\n", i+1, step.Action)
		if len(step.Input) > 200 {
			fmt.Fprintf(&sb, "   Input: %s...\n", step.Input[:200])
		} else {
			fmt.Fprintf(&sb, "   Input: %s\n", step.Input)
		}
		fmt.Fprintf(&sb, "   Success: %v\n", step.Success)
	}

	if trajectory.Outcome.Feedback != "" {
		fmt.Fprintf(&sb, "\nFeedback: %s\n", trajectory.Outcome.Feedback)
	}

	return sb.String()
}

func (lp *LearningPipeline) parseJudgmentResponse(content, trajectoryID string) (*JudgmentResult, error) {
	jsonData := markdown.ExtractJSON(content)
	if jsonData == nil {
		return nil, fmt.Errorf("no valid JSON found in response")
	}

	var parsed struct {
		Quality          float64 `json:"quality"`
		Correctness      float64 `json:"correctness"`
		Efficiency       float64 `json:"efficiency"`
		Generalizability float64 `json:"generalizability"`
		ShouldStore      bool    `json:"should_store"`
		Reason           string  `json:"reason"`
	}

	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		return nil, err
	}

	return &JudgmentResult{
		TrajectoryID:     trajectoryID,
		Quality:          parsed.Quality,
		Correctness:      parsed.Correctness,
		Efficiency:       parsed.Efficiency,
		Generalizability: parsed.Generalizability,
		ShouldStore:      parsed.ShouldStore,
		Reason:           parsed.Reason,
		JudgedAt:         time.Now(),
	}, nil
}

// judgeHeuristic provides a basic heuristic judgment when LLM is unavailable.
func (lp *LearningPipeline) judgeHeuristic(trajectory Trajectory) *JudgmentResult {
	// Basic quality assessment
	successfulSteps := 0
	for _, step := range trajectory.Steps {
		if step.Success {
			successfulSteps++
		}
	}

	stepSuccessRate := 0.0
	if len(trajectory.Steps) > 0 {
		stepSuccessRate = float64(successfulSteps) / float64(len(trajectory.Steps))
	}

	quality := (stepSuccessRate + trajectory.Outcome.Quality) / 2

	return &JudgmentResult{
		TrajectoryID:     trajectory.ID,
		Quality:          quality,
		Correctness:      trajectory.Outcome.Quality,
		Efficiency:       stepSuccessRate,
		Generalizability: 0.5, // Default middle value
		ShouldStore:      quality >= lp.config.MinQualityThreshold && trajectory.Outcome.Success,
		Reason:           fmt.Sprintf("Heuristic evaluation: %.0f%% step success, outcome quality %.2f", stepSuccessRate*100, trajectory.Outcome.Quality),
		JudgedAt:         time.Now(),
	}
}

// Distill extracts patterns from a judged trajectory.
func (lp *LearningPipeline) Distill(ctx context.Context, trajectory Trajectory, judgment *JudgmentResult) ([]*LearnedPattern, error) {
	if !judgment.ShouldStore {
		return nil, nil
	}

	if lp.llmClient == nil {
		return lp.distillHeuristic(trajectory, judgment), nil
	}

	// Use LLM to extract patterns
	prompt := lp.buildDistillPrompt(trajectory, judgment)

	messages := []llm.ChatMessage{
		{Role: "system", Content: distillSystemPrompt},
		{Role: "user", Content: prompt},
	}

	resp, err := lp.llmClient.Chat(ctx, messages)
	if err != nil {
		lp.logger.Warn("LLM distillation failed, using heuristic", "error", err)
		return lp.distillHeuristic(trajectory, judgment), nil
	}

	patterns, err := lp.parseDistillResponse(resp.Content, trajectory.Domain, judgment)
	if err != nil {
		lp.logger.Warn("Failed to parse distill response", "error", err)
		return lp.distillHeuristic(trajectory, judgment), nil
	}

	return patterns, nil
}

const distillSystemPrompt = `You are a pattern extractor. Analyze the trajectory and extract reusable patterns.
For each pattern, provide:
- type: "strategy", "tactic", "anti_pattern", or "heuristic"
- description: Brief human-readable description
- pattern: The actual rule or approach
- tags: Relevant tags

Respond in JSON array format:
[
  {
    "type": "strategy",
    "description": "Use incremental approach for complex tasks",
    "pattern": "Break down complex tasks into smaller, testable steps",
    "tags": ["decomposition", "testing"]
  }
]`

func (lp *LearningPipeline) buildDistillPrompt(trajectory Trajectory, judgment *JudgmentResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Domain: %s\n", trajectory.Domain)
	fmt.Fprintf(&sb, "Quality Score: %.2f\n", judgment.Quality)
	fmt.Fprintf(&sb, "Generalizability: %.2f\n\n", judgment.Generalizability)

	sb.WriteString("Successful trajectory:\n")
	for i, step := range trajectory.Steps {
		if step.Success {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, step.Action)
		}
	}

	sb.WriteString("\nExtract reusable patterns from this approach.")
	return sb.String()
}

func (lp *LearningPipeline) parseDistillResponse(content, domain string, judgment *JudgmentResult) ([]*LearnedPattern, error) {
	jsonData := markdown.ExtractJSONArray(content)
	if jsonData == nil {
		return nil, fmt.Errorf("no valid JSON array found in response")
	}

	var parsed []struct {
		Type        string   `json:"type"`
		Description string   `json:"description"`
		Pattern     string   `json:"pattern"`
		Tags        []string `json:"tags"`
	}

	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		return nil, err
	}

	now := time.Now()
	var patterns []*LearnedPattern

	for _, p := range parsed {
		pattern := &LearnedPattern{
			ID:           lp.generatePatternID(p.Pattern),
			Type:         PatternType(p.Type),
			Status:       PatternStatusPending,
			Domain:       domain,
			Description:  p.Description,
			Pattern:      p.Pattern,
			Tags:         p.Tags,
			Confidence:   judgment.Quality * judgment.Generalizability,
			SuccessRate:  1.0, // First occurrence was successful
			UseCount:     1,
			SuccessCount: 1,
			CreatedAt:    now,
			UpdatedAt:    now,
			ContentHash:  lp.hashContent(p.Pattern),
		}
		patterns = append(patterns, pattern)
	}

	return patterns, nil
}

// distillHeuristic extracts basic patterns without LLM.
func (lp *LearningPipeline) distillHeuristic(trajectory Trajectory, judgment *JudgmentResult) []*LearnedPattern {
	now := time.Now()

	// Create a single high-level pattern from the trajectory
	var actions []string
	for _, step := range trajectory.Steps {
		if step.Success {
			actions = append(actions, step.Action)
		}
	}

	if len(actions) == 0 {
		return nil
	}

	patternText := fmt.Sprintf("For %s tasks: %s", trajectory.Domain, strings.Join(actions, " → "))

	pattern := &LearnedPattern{
		ID:           lp.generatePatternID(patternText),
		Type:         PatternTypeTactic,
		Status:       PatternStatusPending,
		Domain:       trajectory.Domain,
		Description:  fmt.Sprintf("Successful approach for %s", trajectory.Domain),
		Pattern:      patternText,
		Tags:         []string{trajectory.Domain, "auto-extracted"},
		Confidence:   judgment.Quality * 0.7, // Lower confidence for heuristic extraction
		SuccessRate:  1.0,
		UseCount:     1,
		SuccessCount: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
		ContentHash:  lp.hashContent(patternText),
	}

	return []*LearnedPattern{pattern}
}

// StorePattern stores a pattern after judgment.
func (lp *LearningPipeline) StorePattern(ctx context.Context, pattern *LearnedPattern) error {
	lp.mu.Lock()

	if !lp.initialized {
		lp.mu.Unlock()
		return errors.New("learning pipeline not initialized")
	}

	// Check for duplicates
	for _, existing := range lp.patterns {
		if existing.ContentHash != pattern.ContentHash {
			continue
		}
		// Update existing pattern instead
		existing.UseCount++
		existing.SuccessCount++
		existing.UpdatedAt = time.Now()
		// Boost confidence
		existing.Confidence = minFloat(1.0, existing.Confidence*1.1)
		snapshot := deepCopyPatterns(lp.patterns)
		lp.mu.Unlock()
		return lp.savePatternsFromSnapshot(snapshot)
	}

	// Activate if confidence is high enough
	if pattern.Confidence >= lp.config.MinQualityThreshold {
		pattern.Status = PatternStatusActive
	}

	lp.patterns[pattern.ID] = pattern
	snapshot := deepCopyPatterns(lp.patterns)
	lp.mu.Unlock()
	return lp.savePatternsFromSnapshot(snapshot)
}

// Consolidate performs deduplication, contradiction detection, and pruning.
func (lp *LearningPipeline) Consolidate(ctx context.Context) (*ConsolidationResult, error) {
	lp.mu.Lock()

	if !lp.initialized {
		lp.mu.Unlock()
		return nil, errors.New("learning pipeline not initialized")
	}

	start := time.Now()
	result := &ConsolidationResult{}

	// Apply confidence decay
	for _, p := range lp.patterns {
		daysSinceUpdate := time.Since(p.UpdatedAt).Hours() / 24
		if daysSinceUpdate > 1 {
			decayFactor := 1.0 - (lp.config.ConfidenceDecayRate * daysSinceUpdate)
			if decayFactor < 0.5 {
				decayFactor = 0.5 // Floor at 50%
			}
			p.Confidence *= decayFactor
		}
		result.PatternsReviewed++
	}

	// Find and remove duplicates
	contentHashes := make(map[string]string) // hash -> first ID
	var duplicateIDs []string

	for id, p := range lp.patterns {
		if firstID, exists := contentHashes[p.ContentHash]; exists {
			// Keep the one with higher confidence
			first := lp.patterns[firstID]
			if p.Confidence > first.Confidence {
				duplicateIDs = append(duplicateIDs, firstID)
				contentHashes[p.ContentHash] = id
			} else {
				duplicateIDs = append(duplicateIDs, id)
			}
		} else {
			contentHashes[p.ContentHash] = id
		}
	}

	for _, id := range duplicateIDs {
		delete(lp.patterns, id)
		result.DuplicatesRemoved++
	}

	// Detect contradictions (simplified: anti-patterns that conflict with patterns)
	result.ContradictionsFound = lp.detectContradictions()

	// Prune low-confidence patterns
	var toDelete []string
	for id, p := range lp.patterns {
		if p.Confidence < lp.config.MinConfidence && p.Status != PatternStatusActive {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(lp.patterns, id)
		result.LowConfidencePruned++
	}

	// Enforce per-domain limits
	domainPatterns := make(map[string][]*LearnedPattern)
	for _, p := range lp.patterns {
		domainPatterns[p.Domain] = append(domainPatterns[p.Domain], p)
	}

	for domain, patterns := range domainPatterns {
		if len(patterns) > lp.config.MaxPatternsPerDomain {
			// Sort by confidence * success_rate descending
			sort.Slice(patterns, func(i, j int) bool {
				scoreI := patterns[i].Confidence * patterns[i].SuccessRate
				scoreJ := patterns[j].Confidence * patterns[j].SuccessRate
				return scoreI > scoreJ
			})

			// Remove excess
			for _, p := range patterns[lp.config.MaxPatternsPerDomain:] {
				delete(lp.patterns, p.ID)
				result.PatternsDeprecated++
			}

			lp.logger.Info("Pruned domain patterns",
				"domain", domain,
				"removed", len(patterns)-lp.config.MaxPatternsPerDomain,
			)
		}
	}

	result.ConsolidatedAt = time.Now()
	result.Duration = time.Since(start)
	lp.lastConsolidation = result.ConsolidatedAt
	snapshot := deepCopyPatterns(lp.patterns)
	lp.mu.Unlock()

	patternsPath := filepath.Join(lp.dataDir, "patterns.json")
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return result, fmt.Errorf("failed to marshal patterns: %w", err)
	}
	//nolint:gosec // user config directory/file permissions
	if err := os.WriteFile(patternsPath, data, 0o644); err != nil {
		return result, fmt.Errorf("failed to save patterns: %w", err)
	}

	lp.logger.Info("Consolidation complete",
		"reviewed", result.PatternsReviewed,
		"duplicates_removed", result.DuplicatesRemoved,
		"contradictions", result.ContradictionsFound,
		"deprecated", result.PatternsDeprecated,
		"pruned", result.LowConfidencePruned,
		"duration", result.Duration,
	)

	return result, nil
}

// detectContradictions finds patterns that contradict each other.
func (lp *LearningPipeline) detectContradictions() int {
	// Group by domain
	domainPatterns := make(map[string][]*LearnedPattern)
	for _, p := range lp.patterns {
		if p.Status == PatternStatusActive {
			domainPatterns[p.Domain] = append(domainPatterns[p.Domain], p)
		}
	}

	contradictions := 0

	for _, patterns := range domainPatterns {
		// Check anti-patterns against regular patterns
		var antiPatterns []*LearnedPattern
		var regularPatterns []*LearnedPattern

		for _, p := range patterns {
			if p.Type == PatternTypeAntiPattern {
				antiPatterns = append(antiPatterns, p)
			} else {
				regularPatterns = append(regularPatterns, p)
			}
		}

		for _, anti := range antiPatterns {
			for _, regular := range regularPatterns {
				if lp.similarity(anti.Pattern, regular.Pattern) > 0.7 {
					// High similarity between anti-pattern and regular pattern
					// Deprecate the one with lower confidence
					if anti.Confidence > regular.Confidence {
						regular.Status = PatternStatusDeprecated
						regular.SupersededBy = anti.ID
					} else {
						anti.Status = PatternStatusDeprecated
						anti.SupersededBy = regular.ID
					}
					contradictions++
				}
			}
		}
	}

	return contradictions
}

// RecordPatternUse records that a pattern was used.
func (lp *LearningPipeline) RecordPatternUse(patternID string, success bool) {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	p, ok := lp.patterns[patternID]
	if !ok {
		return
	}

	p.UseCount++
	if success {
		p.SuccessCount++
	}
	p.UpdatedAt = time.Now()

	// Update success rate
	if p.UseCount > 0 {
		p.SuccessRate = float64(p.SuccessCount) / float64(p.UseCount)
	}

	// Adjust confidence based on success rate
	if p.UseCount >= 5 {
		p.Confidence = (p.Confidence + p.SuccessRate) / 2
	}
}

// GetStats returns statistics about the learning pipeline.
func (lp *LearningPipeline) GetStats() map[string]any {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	domainCounts := make(map[string]int)
	typeCounts := make(map[string]int)
	statusCounts := make(map[string]int)
	totalConfidence := 0.0
	totalSuccessRate := 0.0

	for _, p := range lp.patterns {
		domainCounts[p.Domain]++
		typeCounts[string(p.Type)]++
		statusCounts[string(p.Status)]++
		totalConfidence += p.Confidence
		totalSuccessRate += p.SuccessRate
	}

	avgConfidence := 0.0
	avgSuccessRate := 0.0
	if len(lp.patterns) > 0 {
		avgConfidence = totalConfidence / float64(len(lp.patterns))
		avgSuccessRate = totalSuccessRate / float64(len(lp.patterns))
	}

	return map[string]any{
		"total_patterns":     len(lp.patterns),
		"patterns_by_domain": domainCounts,
		"patterns_by_type":   typeCounts,
		"patterns_by_status": statusCounts,
		"avg_confidence":     avgConfidence,
		"avg_success_rate":   avgSuccessRate,
		"last_consolidation": lp.lastConsolidation,
		"initialized":        lp.initialized,
	}
}

// GetPatterns returns all active patterns.
func (lp *LearningPipeline) GetPatterns() []*LearnedPattern {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	result := make([]*LearnedPattern, 0, len(lp.patterns))
	for _, p := range lp.patterns {
		result = append(result, p)
	}
	return result
}

// Close stops the pipeline and saves state.
func (lp *LearningPipeline) Close() error {
	// Deep-copy patterns under the lock, then marshal and write to disk
	// outside the lock to avoid holding the mutex during CPU-bound work.
	lp.mu.Lock()
	snapshot := deepCopyPatterns(lp.patterns)
	lp.mu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(lp.dataDir, "patterns.json")
	//nolint:gosec // user config directory/file permissions
	return os.WriteFile(path, data, 0o644)
}

// Helper functions

func (lp *LearningPipeline) generatePatternID(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:8])
}

func (lp *LearningPipeline) hashContent(content string) string {
	// Normalize content for comparison
	normalized := strings.ToLower(strings.TrimSpace(content))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func (lp *LearningPipeline) similarity(a, b string) float64 {
	// Simple word-based Jaccard similarity
	wordsA := make(map[string]bool)
	for w := range strings.FieldsSeq(strings.ToLower(a)) {
		if len(w) > 2 {
			wordsA[w] = true
		}
	}

	wordsB := make(map[string]bool)
	for w := range strings.FieldsSeq(strings.ToLower(b)) {
		if len(w) > 2 {
			wordsB[w] = true
		}
	}

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA) + len(wordsB) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// savePatternsFromSnapshot writes the given patterns to disk without holding
// any lock. Callers must ensure no concurrent mutation of the patterns
// (e.g., by providing deep copies or holding the lock during marshal).
func (lp *LearningPipeline) savePatternsFromSnapshot(patterns map[string]*LearnedPattern) error {
	path := filepath.Join(lp.dataDir, "patterns.json")

	data, err := json.MarshalIndent(patterns, "", "  ")
	if err != nil {
		return err
	}

	//nolint:gosec // user config directory/file permissions
	return os.WriteFile(path, data, 0o644)
}

// deepCopyPatterns creates a deep copy of the patterns map so the caller can
// safely marshal or iterate the snapshot without holding the mutex.
// Must be called with lp.mu held.
func deepCopyPatterns(src map[string]*LearnedPattern) map[string]*LearnedPattern {
	dst := make(map[string]*LearnedPattern, len(src))
	for k, p := range src {
		if p == nil {
			dst[k] = nil
			continue
		}
		cp := *p // shallow copy of value types
		if p.Examples != nil {
			cp.Examples = append([]string(nil), p.Examples...)
		}
		if p.Tags != nil {
			cp.Tags = append([]string(nil), p.Tags...)
		}
		if p.Metadata != nil {
			cp.Metadata = make(map[string]any, len(p.Metadata))
			maps.Copy(cp.Metadata, p.Metadata)
		}
		dst[k] = &cp
	}
	return dst
}

func (lp *LearningPipeline) loadPatterns() error {
	path := filepath.Join(lp.dataDir, "patterns.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No patterns yet
		}
		return err
	}

	return json.Unmarshal(data, &lp.patterns)
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
