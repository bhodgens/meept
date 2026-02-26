package shadow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Manager orchestrates the shadow training system.
type Manager struct {
	config *Config
	logger *slog.Logger

	// Stores
	trainingStore *SQLiteTrainingStore
	examplesStore *SQLiteExamplesStore
	adaptersStore *SQLiteAdaptersStore

	// Components
	teacher  *TeacherClient
	scorer   *Scorer
	selector *Selector
	exporter *Exporter

	// Metrics
	metrics *Metrics

	// Auto-train
	autoTrainStop chan struct{}
	autoTrainDone chan struct{}

	// Synchronization
	mu sync.RWMutex
}

// ManagerConfig holds configuration for creating a Manager.
type ManagerConfig struct {
	Config        *Config
	PrimaryLLM    *llm.Client
	FallbackLLM   *llm.Client
	Logger        *slog.Logger
}

// NewManager creates a new shadow training manager.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.Config == nil {
		cfg.Config = DefaultConfig()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	m := &Manager{
		config:  cfg.Config,
		logger:  cfg.Logger,
		metrics: NewMetrics(),
	}

	if !cfg.Config.Enabled {
		cfg.Logger.Info("Shadow training disabled")
		return m, nil
	}

	// Initialize data directory
	dataDir := expandPath(cfg.Config.DataDir)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize stores
	var err error

	m.trainingStore, err = NewSQLiteTrainingStore(filepath.Join(dataDir, "training.db"))
	if err != nil {
		return nil, fmt.Errorf("failed to create training store: %w", err)
	}

	m.examplesStore, err = NewSQLiteExamplesStore(filepath.Join(dataDir, "examples.db"))
	if err != nil {
		m.trainingStore.Close()
		return nil, fmt.Errorf("failed to create examples store: %w", err)
	}

	if cfg.Config.Adapters.Enabled {
		m.adaptersStore, err = NewSQLiteAdaptersStore(filepath.Join(dataDir, "adapters.db"))
		if err != nil {
			m.trainingStore.Close()
			m.examplesStore.Close()
			return nil, fmt.Errorf("failed to create adapters store: %w", err)
		}
	}

	// Initialize teacher client if LLM is provided
	if cfg.PrimaryLLM != nil {
		m.teacher = NewTeacherClient(
			cfg.PrimaryLLM,
			cfg.FallbackLLM,
			&cfg.Config.Teacher,
			WithTeacherLogger(cfg.Logger.With("component", "teacher")),
			WithTrainingStore(m.trainingStore),
		)
	}

	// Initialize scorer
	scorerOpts := []ScorerOption{
		WithScorerLogger(cfg.Logger.With("component", "scorer")),
	}
	if m.teacher != nil {
		scorerOpts = append(scorerOpts, WithTeacherClient(m.teacher))
	}
	m.scorer = NewScorer(&cfg.Config.Quality, scorerOpts...)

	// Initialize selector
	m.selector = NewSelector(
		m.examplesStore,
		&cfg.Config.Examples,
		WithSelectorLogger(cfg.Logger.With("component", "selector")),
	)

	// Initialize exporter
	m.exporter = NewExporter(
		m.trainingStore,
		&cfg.Config.Export,
		WithExporterLogger(cfg.Logger.With("component", "exporter")),
	)

	cfg.Logger.Info("Shadow training manager initialized",
		"data_dir", dataDir,
		"mode", cfg.Config.Shadowing.Mode,
		"teacher_model", cfg.Config.Teacher.Model,
	)

	// Start auto-train background check if enabled
	if cfg.Config.Adapters.AutoTrain && cfg.Config.Adapters.TrainThreshold > 0 {
		m.startAutoTrainChecker()
	}

	return m, nil
}

// IsEnabled returns true if shadow training is enabled and configured.
func (m *Manager) IsEnabled() bool {
	return m.config != nil && m.config.IsEnabled()
}

// GetTeacherResponse gets a response from the teacher model.
func (m *Manager) GetTeacherResponse(ctx context.Context, messages []llm.ChatMessage) (string, string, error) {
	if m.teacher == nil {
		return "", "", fmt.Errorf("teacher not configured")
	}
	return m.teacher.GetResponse(ctx, messages)
}

// ProcessRecord scores and stores a shadow record.
func (m *Manager) ProcessRecord(ctx context.Context, record *ShadowRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Score the record
	result, err := m.scorer.Score(ctx, record)
	if err != nil {
		return fmt.Errorf("scoring failed: %w", err)
	}

	record.QualityScore = result.Score
	record.IsHighQuality = result.IsHighQuality

	// Determine preference if we have both responses
	if record.HasTeacherResponse() {
		studentScore, teacherScore, err := m.scorer.ScoreComparison(ctx, record)
		if err != nil {
			m.logger.Warn("Comparison scoring failed", "error", err)
		} else {
			margin := teacherScore - studentScore
			if margin > m.config.Quality.PreferenceMargin {
				record.Preference = PreferenceTeacher
			} else if margin < -m.config.Quality.PreferenceMargin {
				record.Preference = PreferenceStudent
			} else {
				record.Preference = PreferenceTie
			}

			// Create preference pair if there's a clear preference
			if record.Preference != PreferenceTie {
				pair := NewPreferencePair(record, studentScore, teacherScore)
				if err := m.trainingStore.SavePreferencePair(ctx, pair); err != nil {
					m.logger.Warn("Failed to save preference pair", "error", err)
				}
			}
		}
	}

	// Save the record
	if err := m.trainingStore.SaveRecord(ctx, record); err != nil {
		return fmt.Errorf("failed to save record: %w", err)
	}

	// Add to examples if high quality
	if record.IsHighQuality && m.config.Examples.Enabled {
		example := NewFewShotExample(record)
		if err := m.examplesStore.SaveExample(ctx, example); err != nil {
			m.logger.Warn("Failed to save example", "error", err)
		}
	}

	// Record metrics
	if m.metrics != nil {
		m.metrics.RecordCollected(record.QualityScore, record.IsHighQuality)
	}

	m.logger.Debug("Shadow record processed",
		"id", record.ID,
		"quality", record.QualityScore,
		"preference", record.Preference,
		"high_quality", record.IsHighQuality,
	)

	return nil
}

// CaptureInteraction captures an LLM interaction for shadow training.
// This is the primary entry point for data collection from the agent loop.
func (m *Manager) CaptureInteraction(ctx context.Context, conversationID string, messages []llm.ChatMessage, response *llm.Response, modelID string) {
	if !m.IsEnabled() || m.trainingStore == nil {
		return
	}

	// Convert llm.ChatMessage to shadow.Message
	shadowMessages := make([]Message, len(messages))
	for i, msg := range messages {
		shadowMessages[i] = Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	// Classify the interaction
	domain := m.classifyDomain(messages)
	taskType := m.classifyTaskType(messages, response)
	complexity := m.estimateComplexity(messages)

	// Check if we should shadow this interaction
	if !m.config.ShouldShadow(string(domain), string(taskType), complexity) {
		return
	}

	// Create the shadow record
	record := NewShadowRecord(conversationID, shadowMessages, modelID, response.Content)
	record.StudentTokensIn = response.Usage.PromptTokens
	record.StudentTokensOut = response.Usage.CompletionTokens
	record.Domain = domain
	record.TaskType = taskType

	switch m.config.Shadowing.Mode {
	case ModeSync:
		// Get teacher response synchronously
		if m.teacher != nil {
			teacherContent, teacherModel, err := m.teacher.GetResponse(ctx, messages)
			if err != nil {
				m.logger.Warn("Failed to get teacher response", "error", err)
			} else {
				record.TeacherModel = teacherModel
				record.TeacherContent = teacherContent
			}
		}
		// Process immediately
		if err := m.ProcessRecord(ctx, record); err != nil {
			m.logger.Error("Failed to process shadow record", "error", err)
		}

	case ModeAsync, ModeSelective:
		// Process in background
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if m.teacher != nil {
				teacherContent, teacherModel, err := m.teacher.GetResponse(bgCtx, messages)
				if err != nil {
					m.logger.Warn("Failed to get teacher response", "error", err)
				} else {
					record.TeacherModel = teacherModel
					record.TeacherContent = teacherContent
				}
			}
			if err := m.ProcessRecord(bgCtx, record); err != nil {
				m.logger.Error("Failed to process shadow record", "error", err)
			}
		}()
	}
}

// classifyDomain classifies message domain.
func (m *Manager) classifyDomain(messages []llm.ChatMessage) Domain {
	var text string
	for _, msg := range messages {
		text += " " + msg.Content
	}

	codeKeywords := []string{"code", "function", "class", "variable", "bug", "error", "compile", "syntax", "import", "package"}
	planningKeywords := []string{"plan", "step", "strategy", "approach", "design", "architecture"}
	debuggingKeywords := []string{"debug", "fix", "issue", "problem", "crash", "stack trace", "exception"}
	analysisKeywords := []string{"analyze", "explain", "how does", "what is", "understand", "review"}

	lower := strings.ToLower(text)
	if containsAnyWord(lower, codeKeywords) {
		return DomainCode
	}
	if containsAnyWord(lower, debuggingKeywords) {
		return DomainDebugging
	}
	if containsAnyWord(lower, planningKeywords) {
		return DomainPlanning
	}
	if containsAnyWord(lower, analysisKeywords) {
		return DomainAnalysis
	}
	return DomainGeneral
}

// classifyTaskType classifies the task type.
func (m *Manager) classifyTaskType(messages []llm.ChatMessage, response *llm.Response) TaskType {
	if response != nil && response.HasToolCalls() {
		return TaskTypeToolUse
	}

	var text string
	for _, msg := range messages {
		text += " " + msg.Content
	}

	lower := strings.ToLower(text)
	if containsAnyWord(lower, []string{"step by step", "first", "second", "then", "finally"}) {
		return TaskTypeMultiStep
	}
	if containsAnyWord(lower, []string{"think", "reason", "consider", "evaluate", "compare"}) {
		return TaskTypeReasoning
	}
	return TaskTypeChat
}

// estimateComplexity estimates interaction complexity.
func (m *Manager) estimateComplexity(messages []llm.ChatMessage) Complexity {
	var totalLength int
	var hasCode bool

	for _, msg := range messages {
		totalLength += len(msg.Content)
		lower := strings.ToLower(msg.Content)
		if strings.Contains(lower, "```") || strings.Contains(lower, "func ") ||
			strings.Contains(lower, "def ") || strings.Contains(lower, "class ") {
			hasCode = true
		}
	}

	if totalLength > 2000 || (hasCode && len(messages) > 2) {
		return ComplexityComplex
	}
	if totalLength > 500 || hasCode || len(messages) > 2 {
		return ComplexityModerate
	}
	return ComplexitySimple
}

// containsAnyWord checks if text contains any keyword.
func containsAnyWord(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// GetFewShotExamples retrieves relevant few-shot examples for prompt injection.
func (m *Manager) GetFewShotExamples(ctx context.Context, domain Domain, taskType TaskType, query string, count int) ([]*FewShotExample, error) {
	if m.selector == nil || !m.config.Examples.Enabled {
		return nil, nil
	}
	return m.selector.SelectExamples(ctx, query, domain, taskType, count)
}

// FormatExamplesForInjection formats examples as messages for injection.
func (m *Manager) FormatExamplesForInjection(examples []*FewShotExample) []Message {
	if m.selector == nil {
		return nil
	}
	return m.selector.FormatForInjection(examples)
}

// Export exports training data.
func (m *Manager) Export(ctx context.Context, opts ExportOptions) (*ExportResult, error) {
	if m.exporter == nil {
		return nil, fmt.Errorf("exporter not initialized")
	}
	return m.exporter.Export(ctx, opts)
}

// ExportDatabase copies the portable training.db for use on a training machine.
func (m *Manager) ExportDatabase(ctx context.Context, outputPath string) error {
	if m.exporter == nil {
		return fmt.Errorf("exporter not initialized")
	}
	srcPath := filepath.Join(expandPath(m.config.DataDir), "training.db")
	return m.exporter.ExportDatabase(ctx, srcPath, outputPath)
}

// TrainingDBPath returns the path to the training database.
func (m *Manager) TrainingDBPath() string {
	return filepath.Join(expandPath(m.config.DataDir), "training.db")
}

// GetPreferencePairCount returns the number of preference pairs available for training.
func (m *Manager) GetPreferencePairCount(ctx context.Context) (int, error) {
	if m.trainingStore == nil {
		return 0, fmt.Errorf("training store not initialized")
	}
	pairs, err := m.trainingStore.ListPreferencePairs(ctx, ListPairsOptions{})
	if err != nil {
		return 0, err
	}
	return len(pairs), nil
}

// GetStats returns shadow training statistics.
func (m *Manager) GetStats(ctx context.Context) (*ShadowStats, error) {
	if m.trainingStore == nil {
		return NewShadowStats(), nil
	}

	stats, err := m.trainingStore.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	// Add examples count
	if m.examplesStore != nil {
		count, err := m.examplesStore.Count(ctx)
		if err == nil {
			stats.FewShotExamples = count
		}
	}

	return stats, nil
}

// RebuildExamples rebuilds the examples database from training data.
func (m *Manager) RebuildExamples(ctx context.Context) error {
	if m.trainingStore == nil || m.examplesStore == nil {
		return fmt.Errorf("stores not initialized")
	}

	// Get high-quality records
	records, err := m.trainingStore.ListRecords(ctx, ListRecordsOptions{
		HighQualityOnly: true,
	})
	if err != nil {
		return fmt.Errorf("failed to list records: %w", err)
	}

	// Rebuild examples
	if err := m.examplesStore.RebuildFromRecords(ctx, records, m.config.Examples.MinQuality); err != nil {
		return fmt.Errorf("failed to rebuild examples: %w", err)
	}

	m.logger.Info("Examples rebuilt", "count", len(records))
	return nil
}

// PruneExamples removes old examples.
func (m *Manager) PruneExamples(ctx context.Context, maxAgeDays int) (int, error) {
	if m.examplesStore == nil {
		return 0, fmt.Errorf("examples store not initialized")
	}

	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour
	return m.examplesStore.PruneExamples(ctx, maxAge)
}

// ListAdapters returns all registered adapters.
func (m *Manager) ListAdapters(ctx context.Context) ([]*Adapter, error) {
	if m.adaptersStore == nil {
		return nil, fmt.Errorf("adapters store not initialized")
	}
	return m.adaptersStore.ListAdapters(ctx)
}

// RegisterAdapter registers a new adapter.
func (m *Manager) RegisterAdapter(ctx context.Context, adapter *Adapter) error {
	if m.adaptersStore == nil {
		return fmt.Errorf("adapters store not initialized")
	}
	return m.adaptersStore.SaveAdapter(ctx, adapter)
}

// ActivateAdapter activates an adapter.
func (m *Manager) ActivateAdapter(ctx context.Context, id string) error {
	if m.adaptersStore == nil {
		return fmt.Errorf("adapters store not initialized")
	}
	return m.adaptersStore.SetActiveAdapter(ctx, id)
}

// GetActiveAdapter returns the active adapter for a base model.
func (m *Manager) GetActiveAdapter(ctx context.Context, modelBase string) (*Adapter, error) {
	if m.adaptersStore == nil {
		return nil, nil
	}
	return m.adaptersStore.GetActiveAdapter(ctx, modelBase)
}

// Config returns the current configuration.
func (m *Manager) Config() *Config {
	return m.config
}

// Metrics returns the current metrics snapshot.
func (m *Manager) Metrics() *MetricsSnapshot {
	if m.metrics == nil {
		return &MetricsSnapshot{}
	}
	return m.metrics.Snapshot()
}

// ResetMetrics resets all metrics counters.
func (m *Manager) ResetMetrics() {
	if m.metrics != nil {
		m.metrics.Reset()
	}
}

// CaptureToolInteraction captures a tool-use interaction for shadow training.
// This is called when the LLM returns tool calls, capturing the intermediate step.
func (m *Manager) CaptureToolInteraction(ctx context.Context, conversationID string, messages []llm.ChatMessage, response *llm.Response, modelID string) {
	if !m.IsEnabled() || m.trainingStore == nil {
		return
	}

	// Convert llm.ChatMessage to shadow.Message
	shadowMessages := make([]Message, len(messages))
	for i, msg := range messages {
		shadowMessages[i] = Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		}
	}

	// Classify the interaction
	domain := m.classifyDomain(messages)
	complexity := m.estimateComplexity(messages)

	// Always classify as tool_use since we know there are tool calls
	taskType := TaskTypeToolUse

	// Check if we should shadow this interaction
	if !m.config.ShouldShadow(string(domain), string(taskType), complexity) {
		return
	}

	// Build a content representation of the tool calls
	var toolContent string
	for i, tc := range response.ToolCalls {
		if i > 0 {
			toolContent += "\n"
		}
		toolContent += "Tool: " + tc.Function.Name + "\nArgs: " + tc.Function.Arguments
	}

	// Create the shadow record
	record := NewShadowRecord(conversationID, shadowMessages, modelID, toolContent)
	record.StudentTokensIn = response.Usage.PromptTokens
	record.StudentTokensOut = response.Usage.CompletionTokens
	record.Domain = domain
	record.TaskType = taskType

	// For tool-use interactions, we typically don't get teacher responses
	// since the exact tool choice is context-dependent. Just process the record.
	switch m.config.Shadowing.Mode {
	case ModeSync:
		if err := m.ProcessRecord(ctx, record); err != nil {
			m.logger.Error("Failed to process shadow tool record", "error", err)
		}

	case ModeAsync, ModeSelective:
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := m.ProcessRecord(bgCtx, record); err != nil {
				m.logger.Error("Failed to process shadow tool record", "error", err)
			}
		}()
	}
}

// startAutoTrainChecker starts a background goroutine that periodically checks
// if enough preference pairs are available to trigger training.
func (m *Manager) startAutoTrainChecker() {
	m.autoTrainStop = make(chan struct{})
	m.autoTrainDone = make(chan struct{})

	// Parse schedule or use default (check every hour)
	checkInterval := time.Hour
	if m.config.Adapters.TrainSchedule != "" {
		// Simple parsing: "1h", "30m", "24h"
		if d, err := time.ParseDuration(m.config.Adapters.TrainSchedule); err == nil {
			checkInterval = d
		}
	}

	m.logger.Info("Auto-train checker started",
		"interval", checkInterval,
		"threshold", m.config.Adapters.TrainThreshold,
	)

	go func() {
		defer close(m.autoTrainDone)
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-m.autoTrainStop:
				return
			case <-ticker.C:
				m.checkAutoTrain()
			}
		}
	}()
}

// checkAutoTrain checks if training threshold is met and triggers training if so.
func (m *Manager) checkAutoTrain() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	count, err := m.GetPreferencePairCount(ctx)
	if err != nil {
		m.logger.Warn("Auto-train check failed", "error", err)
		return
	}

	threshold := m.config.Adapters.TrainThreshold
	if count >= threshold {
		m.logger.Info("Auto-train threshold met",
			"pairs", count,
			"threshold", threshold,
		)

		// Export DPO data for training
		timestamp := time.Now().Format("20060102-150405")
		outputPath := filepath.Join(expandPath(m.config.Export.OutputDir), fmt.Sprintf("auto_dpo_%s.jsonl", timestamp))

		result, err := m.Export(ctx, ExportOptions{
			Format:         FormatDPO,
			OutputPath:     outputPath,
			MarkAsExported: true,
		})
		if err != nil {
			m.logger.Error("Auto-train export failed", "error", err)
			return
		}

		m.logger.Info("Auto-train data exported",
			"records", result.RecordsExported,
			"path", result.OutputPath,
		)

		// Note: Actual training execution would be triggered here if a trainer is configured.
		// For now, we just export the data and log that training should be triggered.
		// The actual training is handled by the CLI 'shadow adapters train' command.
	}
}

// StopAutoTrain stops the auto-train checker goroutine.
func (m *Manager) StopAutoTrain() {
	if m.autoTrainStop != nil {
		close(m.autoTrainStop)
		<-m.autoTrainDone
		m.autoTrainStop = nil
		m.autoTrainDone = nil
	}
}

// Close closes all resources.
func (m *Manager) Close() error {
	// Stop auto-train checker first
	m.StopAutoTrain()

	var lastErr error

	if m.trainingStore != nil {
		if err := m.trainingStore.Close(); err != nil {
			lastErr = err
		}
	}

	if m.examplesStore != nil {
		if err := m.examplesStore.Close(); err != nil {
			lastErr = err
		}
	}

	if m.adaptersStore != nil {
		if err := m.adaptersStore.Close(); err != nil {
			lastErr = err
		}
	}

	m.logger.Info("Shadow training manager closed")
	return lastErr
}

// WrapLLMClient wraps an LLM client with shadow middleware.
func (m *Manager) WrapLLMClient(client LLMChatter) *Middleware {
	if !m.IsEnabled() {
		return nil
	}

	return NewMiddleware(
		client,
		m,
		m.config,
		WithMiddlewareLogger(m.logger.With("component", "middleware")),
	)
}
