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
	"slices"
	"strings"
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

// dedupState holds state for semantic deduplication during export.
type dedupState struct {
	seenHashes    map[string]bool
	seenTokenSets []map[string]int
	threshold     float64
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
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
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

	// Use semantic deduplication with threshold
	dedup := newDedupState(e.config.DedupSimilarityThreshold)
	count := 0

	for _, record := range records {
		// Deduplication using semantic similarity
		if e.config.Deduplicate {
			// Build text for dedup from user messages
			var dedupText strings.Builder
			for _, msg := range record.Messages {
				if msg.Role == RoleUser {
					dedupText.WriteString(msg.Content + " ")
				}
			}
			if dedup.isDuplicate(dedupText.String()) {
				continue
			}
		}

		// Build JSONL entry
		entry := map[string]any{
			"id":       record.ID,
			"messages": record.Messages,
			"response": getBestResponse(record),
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

func (e *Exporter) exportDPO(ctx context.Context, writer *bufio.Writer, opts ExportOptions) (n int, result []string, err error) {
	pairOpts := ListPairsOptions{
		MinMargin:      opts.MinMargin,
		UnexportedOnly: true,
		Since:          opts.Since,
	}

	pairs, err := e.store.ListPreferencePairs(ctx, pairOpts)
	if err != nil {
		return 0, nil, err
	}

	// Use semantic deduplication with threshold
	dedup := newDedupState(e.config.DedupSimilarityThreshold)
	count := 0
	var exportedIDs []string

	for _, pair := range pairs {
		// Deduplication using semantic similarity
		if e.config.Deduplicate {
			// Build text for dedup from prompt
			dedupText := formatPrompt(pair.PromptMessages)
			if dedup.isDuplicate(dedupText) {
				continue
			}
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

	// Use semantic deduplication with threshold
	dedup := newDedupState(e.config.DedupSimilarityThreshold)
	count := 0

	for _, record := range records {
		// Deduplication using semantic similarity
		if e.config.Deduplicate {
			var dedupText strings.Builder
			for _, msg := range record.Messages {
				if msg.Role == RoleUser {
					dedupText.WriteString(msg.Content + " ")
				}
			}
			if dedup.isDuplicate(dedupText.String()) {
				continue
			}
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

	// Use semantic deduplication with threshold
	dedup := newDedupState(e.config.DedupSimilarityThreshold)
	count := 0

	for _, record := range records {
		// Deduplication using semantic similarity
		if e.config.Deduplicate {
			var dedupText strings.Builder
			for _, msg := range record.Messages {
				if msg.Role == RoleUser {
					dedupText.WriteString(msg.Content + " ")
				}
			}
			if dedup.isDuplicate(dedupText.String()) {
				continue
			}
		}

		// Get last user message as instruction
		var instruction string
		for _, v := range slices.Backward(record.Messages) {
			if v.Role == RoleUser {
				instruction = v.Content
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
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
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
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		parts = append(parts, fmt.Sprintf("%s: %s", msg.Role, msg.Content))
	}
	return strings.Join(parts, "\n\n")
}

// newDedupState creates a new deduplication state with the given similarity threshold.
func newDedupState(threshold float64) *dedupState {
	return &dedupState{
		seenHashes:    make(map[string]bool),
		seenTokenSets: make([]map[string]int, 0),
		threshold:     threshold,
	}
}

// isDuplicate checks if the given text is semantically similar to any previously seen text.
// It uses both hash-based and token-based Jaccard similarity.
func (d *dedupState) isDuplicate(text string) bool {
	// Quick hash check first
	hash := textHash(text)
	if d.seenHashes[hash] {
		return true
	}

	// Tokenize for semantic similarity
	tokens := tokenizeForDedup(text)
	tokenSet := make(map[string]int)
	for _, t := range tokens {
		tokenSet[t]++
	}

	// Check semantic similarity against all seen token sets
	for _, seen := range d.seenTokenSets {
		sim := jaccardSimilarity(tokenSet, seen)
		if sim >= d.threshold {
			return true
		}
	}

	// Not a duplicate - add to seen sets
	d.seenHashes[hash] = true
	d.seenTokenSets = append(d.seenTokenSets, tokenSet)
	return false
}

// textHash generates a hash fingerprint from text.
func textHash(text string) string {
	// Use first 100 chars as fingerprint
	if len(text) > 100 {
		text = text[:100]
	}
	return text
}

// tokenizeForDedup tokenizes text for deduplication similarity comparison.
func tokenizeForDedup(text string) []string {
	// Simple whitespace tokenization with lowercasing
	text = strings.ToLower(text)
	var tokens []string
	var current []byte

	for i := range len(text) {
		c := text[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			current = append(current, c)
		} else if len(current) > 0 {
			if len(current) >= 3 { // Skip very short tokens
				tokens = append(tokens, string(current))
			}
			current = current[:0]
		}
	}
	if len(current) >= 3 {
		tokens = append(tokens, string(current))
	}

	return tokens
}

// jaccardSimilarity computes the Jaccard similarity between two token multisets.
func jaccardSimilarity(a, b map[string]int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	intersection := 0
	union := 0

	// Count all unique tokens
	allTokens := make(map[string]bool)
	for t := range a {
		allTokens[t] = true
	}
	for t := range b {
		allTokens[t] = true
	}

	for t := range allTokens {
		countA := a[t]
		countB := b[t]

		// Min for intersection
		if countA < countB {
			intersection += countA
		} else {
			intersection += countB
		}

		// Max for union
		if countA > countB {
			union += countA
		} else {
			union += countB
		}
	}

	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

func expandPath(path string) string {
	if path != "" && path[0] == '~' {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[1:])
	}
	return path
}


