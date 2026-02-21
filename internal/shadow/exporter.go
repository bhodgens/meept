package shadow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ExportFormat specifies the output format.
type ExportFormat string

const (
	FormatJSONL  ExportFormat = "jsonl"
	FormatDPO    ExportFormat = "dpo"
	FormatOpenAI ExportFormat = "openai"
	FormatAlpaca ExportFormat = "alpaca"
)

// Exporter exports training data in various formats.
type Exporter struct {
	store  TrainingStore
	config *ExportConfig
	logger *slog.Logger
}

// ExporterOption is a functional option for Exporter.
type ExporterOption func(*Exporter)

// WithExporterLogger sets the logger.
func WithExporterLogger(logger *slog.Logger) ExporterOption {
	return func(e *Exporter) {
		e.logger = logger
	}
}

// NewExporter creates a new exporter.
func NewExporter(store TrainingStore, config *ExportConfig, opts ...ExporterOption) *Exporter {
	e := &Exporter{
		store:  store,
		config: config,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// ExportOptions specifies export parameters.
type ExportOptions struct {
	Format         ExportFormat
	MinQuality     float64
	MinMargin      float64
	Since          *time.Time
	Until          *time.Time
	OutputPath     string
	MarkAsExported bool
}

// ExportResult contains export statistics.
type ExportResult struct {
	RecordsExported int
	OutputPath      string
	Format          ExportFormat
	Duration        time.Duration
}

// Export exports training data to a file.
func (e *Exporter) Export(ctx context.Context, opts ExportOptions) (*ExportResult, error) {
	startTime := time.Now()

	// Determine output path
	outputPath := opts.OutputPath
	if outputPath == "" {
		timestamp := time.Now().Format("20060102")
		filename := fmt.Sprintf("%s_%s.jsonl", opts.Format, timestamp)
		outputPath = filepath.Join(expandPath(e.config.OutputDir), filename)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Open output file
	file, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	var count int
	var exportedIDs []string

	switch opts.Format {
	case FormatDPO:
		count, exportedIDs, err = e.exportDPO(ctx, writer, opts)
	case FormatOpenAI:
		count, err = e.exportOpenAI(ctx, writer, opts)
	case FormatAlpaca:
		count, err = e.exportAlpaca(ctx, writer, opts)
	default:
		count, err = e.exportJSONL(ctx, writer, opts)
	}

	if err != nil {
		return nil, err
	}

	// Mark as exported if requested
	if opts.MarkAsExported && len(exportedIDs) > 0 {
		if err := e.store.MarkExported(ctx, exportedIDs); err != nil {
			e.logger.Warn("Failed to mark records as exported", "error", err)
		}
	}

	return &ExportResult{
		RecordsExported: count,
		OutputPath:      outputPath,
		Format:          opts.Format,
		Duration:        time.Since(startTime),
	}, nil
}

func (e *Exporter) exportJSONL(ctx context.Context, writer *bufio.Writer, opts ExportOptions) (int, error) {
	listOpts := ListRecordsOptions{
		MinQuality: opts.MinQuality,
		Since:      opts.Since,
		Until:      opts.Until,
	}
	if !e.config.IncludeLowQuality {
		listOpts.HighQualityOnly = true
	}

	records, err := e.store.ListRecords(ctx, listOpts)
	if err != nil {
		return 0, err
	}

	seenHashes := make(map[string]bool)
	count := 0

	for _, record := range records {
		// Deduplication
		if e.config.Deduplicate {
			hash := hashRecord(record)
			if seenHashes[hash] {
				continue
			}
			seenHashes[hash] = true
		}

		// Build JSONL entry
		entry := map[string]any{
			"id":           record.ID,
			"messages":     record.Messages,
			"response":     getBestResponse(record),
			"metadata": map[string]any{
				"domain":        record.Domain,
				"task_type":     record.TaskType,
				"quality_score": record.QualityScore,
				"model":         getResponseModel(record),
			},
		}

		data, err := json.Marshal(entry)
		if err != nil {
			e.logger.Warn("Failed to marshal record", "id", record.ID, "error", err)
			continue
		}

		if _, err := writer.Write(append(data, '\n')); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

func (e *Exporter) exportDPO(ctx context.Context, writer *bufio.Writer, opts ExportOptions) (int, []string, error) {
	pairOpts := ListPairsOptions{
		MinMargin:      opts.MinMargin,
		UnexportedOnly: true,
		Since:          opts.Since,
	}

	pairs, err := e.store.ListPreferencePairs(ctx, pairOpts)
	if err != nil {
		return 0, nil, err
	}

	seenHashes := make(map[string]bool)
	count := 0
	var exportedIDs []string

	for _, pair := range pairs {
		// Deduplication
		if e.config.Deduplicate {
			hash := hashPair(pair)
			if seenHashes[hash] {
				continue
			}
			seenHashes[hash] = true
		}

		// Build DPO format
		// Format: {"prompt": "...", "chosen": "...", "rejected": "..."}
		entry := map[string]any{
			"prompt":   formatPrompt(pair.PromptMessages),
			"chosen":   pair.ChosenResponse,
			"rejected": pair.RejectedResponse,
		}

		data, err := json.Marshal(entry)
		if err != nil {
			e.logger.Warn("Failed to marshal pair", "id", pair.ID, "error", err)
			continue
		}

		if _, err := writer.Write(append(data, '\n')); err != nil {
			return count, exportedIDs, err
		}
		count++
		exportedIDs = append(exportedIDs, pair.ID)
	}

	return count, exportedIDs, nil
}

func (e *Exporter) exportOpenAI(ctx context.Context, writer *bufio.Writer, opts ExportOptions) (int, error) {
	listOpts := ListRecordsOptions{
		MinQuality: opts.MinQuality,
		Since:      opts.Since,
		Until:      opts.Until,
	}
	if !e.config.IncludeLowQuality {
		listOpts.HighQualityOnly = true
	}

	records, err := e.store.ListRecords(ctx, listOpts)
	if err != nil {
		return 0, err
	}

	seenHashes := make(map[string]bool)
	count := 0

	for _, record := range records {
		// Deduplication
		if e.config.Deduplicate {
			hash := hashRecord(record)
			if seenHashes[hash] {
				continue
			}
			seenHashes[hash] = true
		}

		// Build OpenAI fine-tuning format
		messages := make([]map[string]string, len(record.Messages)+1)

		for i, msg := range record.Messages {
			messages[i] = map[string]string{
				"role":    msg.Role,
				"content": msg.Content,
			}
		}

		// Add assistant response
		messages[len(record.Messages)] = map[string]string{
			"role":    "assistant",
			"content": getBestResponse(record),
		}

		entry := map[string]any{
			"messages": messages,
		}

		data, err := json.Marshal(entry)
		if err != nil {
			e.logger.Warn("Failed to marshal record", "id", record.ID, "error", err)
			continue
		}

		if _, err := writer.Write(append(data, '\n')); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

func (e *Exporter) exportAlpaca(ctx context.Context, writer *bufio.Writer, opts ExportOptions) (int, error) {
	listOpts := ListRecordsOptions{
		MinQuality: opts.MinQuality,
		Since:      opts.Since,
		Until:      opts.Until,
	}
	if !e.config.IncludeLowQuality {
		listOpts.HighQualityOnly = true
	}

	records, err := e.store.ListRecords(ctx, listOpts)
	if err != nil {
		return 0, err
	}

	seenHashes := make(map[string]bool)
	count := 0

	for _, record := range records {
		// Deduplication
		if e.config.Deduplicate {
			hash := hashRecord(record)
			if seenHashes[hash] {
				continue
			}
			seenHashes[hash] = true
		}

		// Get last user message as instruction
		var instruction string
		for i := len(record.Messages) - 1; i >= 0; i-- {
			if record.Messages[i].Role == "user" {
				instruction = record.Messages[i].Content
				break
			}
		}

		// Get system prompt as input context
		var input string
		for _, msg := range record.Messages {
			if msg.Role == "system" {
				input = msg.Content
				break
			}
		}

		// Build Alpaca format
		entry := map[string]string{
			"instruction": instruction,
			"input":       input,
			"output":      getBestResponse(record),
		}

		data, err := json.Marshal(entry)
		if err != nil {
			e.logger.Warn("Failed to marshal record", "id", record.ID, "error", err)
			continue
		}

		if _, err := writer.Write(append(data, '\n')); err != nil {
			return count, err
		}
		count++
	}

	return count, nil
}

// ExportDatabase copies the training database for external use.
// The training.db is self-contained and portable for use on training machines.
func (e *Exporter) ExportDatabase(ctx context.Context, srcPath, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source database: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy database: %w", err)
	}

	e.logger.Info("Exported training database", "source", srcPath, "output", outputPath)
	return nil
}

// Helper functions

func getBestResponse(record *ShadowRecord) string {
	if record.HasTeacherResponse() && record.Preference == PreferenceTeacher {
		return record.TeacherContent
	}
	return record.StudentContent
}

func getResponseModel(record *ShadowRecord) string {
	if record.HasTeacherResponse() && record.Preference == PreferenceTeacher {
		return record.TeacherModel
	}
	return record.StudentModel
}

func formatPrompt(messages []Message) string {
	var parts []string
	for _, msg := range messages {
		parts = append(parts, fmt.Sprintf("%s: %s", msg.Role, msg.Content))
	}
	return joinStrings(parts, "\n\n")
}

func hashRecord(record *ShadowRecord) string {
	// Simple hash based on user message content
	var userContent string
	for _, msg := range record.Messages {
		if msg.Role == "user" {
			userContent += msg.Content
		}
	}

	// Use first 100 chars as a fingerprint
	if len(userContent) > 100 {
		userContent = userContent[:100]
	}

	return userContent
}

func hashPair(pair *PreferencePair) string {
	// Hash based on chosen response start
	content := pair.ChosenResponse
	if len(content) > 100 {
		content = content[:100]
	}
	return content
}

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}
