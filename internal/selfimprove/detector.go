// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/google/uuid"
)

// IssueDetector detects issues from logs, metrics, and code.
type IssueDetector struct {
	config      DetectionConfig
	projectRoot string
	logger      *slog.Logger

	// Compiled patterns
	errorPatterns []*regexp.Regexp
}

// NewIssueDetector creates a new IssueDetector.
func NewIssueDetector(cfg DetectionConfig, projectRoot string, logger *slog.Logger) *IssueDetector {
	if logger == nil {
		logger = slog.Default()
	}

	// Compile error patterns
	patterns := make([]*regexp.Regexp, 0, len(cfg.ErrorPatterns))
	for _, p := range cfg.ErrorPatterns {
		if re, err := regexp.Compile(p); err == nil {
			patterns = append(patterns, re)
		}
	}

	return &IssueDetector{
		config:        cfg,
		projectRoot:   projectRoot,
		logger:        logger,
		errorPatterns: patterns,
	}
}

// DetectAll runs all detection methods and returns found issues.
func (d *IssueDetector) DetectAll(ctx context.Context) ([]Issue, error) {
	var allIssues []Issue

	// Scan logs
	logIssues, err := d.ScanLogs(ctx)
	if err != nil {
		d.logger.Warn("log scanning failed", "error", err)
	} else {
		allIssues = append(allIssues, logIssues...)
	}

	// Scan for common code issues
	codeIssues, err := d.ScanCode(ctx)
	if err != nil {
		d.logger.Warn("code scanning failed", "error", err)
	} else {
		allIssues = append(allIssues, codeIssues...)
	}

	return allIssues, nil
}

// ScanTraces runs the RLM trace analyzer over the given trace store path
// and converts failure modes into selfimprove Issues.
//
// NOTE: RLM analyzer integration is deferred to avoid import cycle.
// Trace analysis can be performed directly via agent.RLMAnalyzer.
func (d *IssueDetector) ScanTraces(ctx context.Context, traceStorePath string) ([]Issue, error) {
	// Load the trace store from JSONL source.
	// Uses memory package directly to avoid import cycle (selfimprove -> agent -> selfimprove).
	store, err := memory.LoadTraceStore(traceStorePath)
	if err != nil {
		return nil, fmt.Errorf("load trace store: %w", err)
	}

	traceIDs := store.GetTraceIDs()
	if len(traceIDs) == 0 {
		return nil, nil
	}

	// Scan all traces for failure patterns (deterministic RLM-style analysis).
	var issues []Issue
	seenPatterns := make(map[string]bool)

	for _, traceID := range traceIDs {
		select {
		case <-ctx.Done():
			return issues, ctx.Err()
		default:
		}

		result, err := store.ViewTrace(traceID)
		if err != nil {
			continue
		}

		// Check for oversized traces.
		if result.Oversized != nil {
			key := "oversized_trace:" + traceID
			if !seenPatterns[key] {
				seenPatterns[key] = true
				issues = append(issues, Issue{
					ID:          uuid.New().String()[:16],
					Type:        IssueTypePerformance,
					Severity:    SeverityMedium,
					Description: fmt.Sprintf("Trace %s is oversized (%d spans) — analysis may be incomplete", traceID, result.Oversized.SpanCount),
					Source:      traceStorePath,
					Context:     fmt.Sprintf("span_count=%d recommendation=%s", result.Oversized.SpanCount, result.Oversized.Recommendation),
					DetectedAt:  time.Now(),
					Metadata: map[string]any{
						"category":         "semantic",
						"trace_id":         traceID,
						"span_count":       result.Oversized.SpanCount,
						"error_span_count": result.Oversized.ErrorSpanCount,
					},
				})
			}
			continue
		}

		spans := result.Spans
		if len(spans) == 0 {
			continue
		}

		// Check for refusal/rejection patterns (refusal_loop).
		for _, span := range spans {
			lower := strings.ToLower(span.ToolName)
			if strings.Contains(lower, "refusal") || strings.Contains(lower, "denied") || strings.Contains(lower, "reject") {
				pattern := "refusal:" + traceID + ":" + span.ToolName
				if !seenPatterns[pattern] {
					seenPatterns[pattern] = true
					issues = append(issues, Issue{
						ID:          uuid.New().String()[:16],
						Type:        IssueTypeReliability,
						Severity:    SeverityHigh,
						Description: fmt.Sprintf("Refusal/rejection pattern in trace %s at %s", traceID, span.ToolName),
						Source:      traceStorePath,
						Context:     span.ToolName,
						DetectedAt:  time.Now(),
						Metadata: map[string]any{
							"category": "refusal_loop",
							"trace_id": traceID,
						},
					})
				}
			}
		}

		// Check for error spans (tool_error).
		for _, span := range spans {
			if span.HasError {
				pattern := "error:" + traceID + ":" + span.ToolName
				if !seenPatterns[pattern] {
					seenPatterns[pattern] = true
					issues = append(issues, Issue{
						ID:          uuid.New().String()[:16],
						Type:        IssueTypeReliability,
						Severity:    SeverityHigh,
						Description: fmt.Sprintf("Error span in trace %s at %s (model: %s, error_type: %s)", traceID, span.ToolName, span.Model, span.ErrorType),
						Source:      traceStorePath,
						Context:     span.ToolName,
						DetectedAt:  time.Now(),
						Metadata: map[string]any{
							"category": "tool_error",
							"trace_id": traceID,
							"model":    span.Model,
							"error":    span.ErrorType,
						},
					})
				}
			}
		}

		// Check for repeated tool calls (redundant_args).
		toolCount := make(map[string]int)
		for _, span := range spans {
			toolCount[span.ToolName]++
		}
		for tool, count := range toolCount {
			if count >= 3 {
				key := "repeated:" + traceID + ":" + tool
				if !seenPatterns[key] {
					seenPatterns[key] = true
					issues = append(issues, Issue{
						ID:          uuid.New().String()[:16],
						Type:        IssueTypePerformance,
						Severity:    SeverityMedium,
						Description: fmt.Sprintf("Repeated tool call '%s' (%d times) in trace %s", tool, count, traceID),
						Source:      traceStorePath,
						Context:     fmt.Sprintf("tool=%s,count=%d", tool, count),
						DetectedAt:  time.Now(),
						Metadata: map[string]any{
							"category": "redundant_args",
							"trace_id": traceID,
							"tool":     tool,
							"count":    count,
						},
					})
				}
			}
		}
	}

	return issues, nil
}

// FailureModeToIssueType maps an RLM analyzer failure mode category to an IssueType.
// Exported for use by external RLM analyzer callers.
func FailureModeToIssueType(category string) IssueType {
	switch {
	case category == "hallucination", category == "semantic":
		return IssueTypeError
	case category == "refusal_loop":
		return IssueTypeReliability
	case category == "redundant_args":
		return IssueTypePerformance
	case category == "tool_error":
		return IssueTypeReliability
	case category == "timeout":
		return IssueTypeReliability
	default:
		return IssueTypeReliability
	}
}

func (d *IssueDetector) ScanLogs(ctx context.Context) ([]Issue, error) {
	var issues []Issue

	for _, pattern := range d.config.LogPatterns {
		logPath := filepath.Join(d.projectRoot, pattern)
		matches, err := filepath.Glob(logPath)
		if err != nil {
			continue
		}

		for _, match := range matches {
			fileIssues, err := d.scanLogFile(ctx, match)
			if err != nil {
				d.logger.Warn("failed to scan log file", "file", match, "error", err)
				continue
			}
			issues = append(issues, fileIssues...)
		}
	}

	return issues, nil
}

// scanLogFile scans a single log file.
func (d *IssueDetector) scanLogFile(ctx context.Context, path string) ([]Issue, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var issues []Issue
	scanner := bufio.NewScanner(file)
	lineNum := 0
	var contextLines []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return issues, ctx.Err()
		default:
		}

		lineNum++
		line := scanner.Text()

		// Keep context window
		contextLines = append(contextLines, line)
		if len(contextLines) > 5 {
			contextLines = contextLines[1:]
		}

		// Check for error patterns
		for _, pattern := range d.errorPatterns {
			if pattern.MatchString(line) {
				issue := Issue{
					ID:          uuid.New().String()[:16],
					Type:        IssueTypeError,
					Severity:    d.determineSeverity(line),
					Description: d.extractDescription(line),
					Source:      path,
					Context:     strings.Join(contextLines, "\n"),
					DetectedAt:  time.Now(),
					Metadata: map[string]any{
						"line_number": lineNum,
						"pattern":     pattern.String(),
					},
				}
				issues = append(issues, issue)
				break // One issue per line
			}
		}
	}

	return issues, scanner.Err()
}

// ScanCode scans code files for common issues.
func (d *IssueDetector) ScanCode(ctx context.Context) ([]Issue, error) {
	var issues []Issue

	// Common patterns to look for in Go code
	codePatterns := []struct {
		Pattern  *regexp.Regexp
		Type     IssueType
		Severity IssueSeverity
		Desc     string
	}{
		{
			Pattern:  regexp.MustCompile(`//\s*TODO:?\s+(.+)`),
			Type:     IssueTypeUsability,
			Severity: SeverityLow,
			Desc:     "TODO comment",
		},
		{
			Pattern:  regexp.MustCompile(`//\s*FIXME:?\s+(.+)`),
			Type:     IssueTypeError,
			Severity: SeverityMedium,
			Desc:     "FIXME comment",
		},
		{
			Pattern:  regexp.MustCompile(`//\s*HACK:?\s+(.+)`),
			Type:     IssueTypeReliability,
			Severity: SeverityMedium,
			Desc:     "HACK comment",
		},
		{
			Pattern:  regexp.MustCompile(`panic\([^)]+\)`),
			Type:     IssueTypeReliability,
			Severity: SeverityHigh,
			Desc:     "Explicit panic",
		},
	}

	// Walk Go files
	err := filepath.Walk(d.projectRoot, func(path string, info os.FileInfo, err error) error { //nolint:gosec // WalkDir stat is advisory, not security-critical
		if err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip non-Go files and test files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip vendor and test directories
		if strings.Contains(path, "/vendor/") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		//nolint:gosec // path validated by trusted source
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			for _, cp := range codePatterns {
				if matches := cp.Pattern.FindStringSubmatch(line); len(matches) > 0 {
					issue := Issue{
						ID:          uuid.New().String()[:16],
						Type:        cp.Type,
						Severity:    cp.Severity,
						Description: cp.Desc,
						Source:      path,
						Context:     line,
						DetectedAt:  time.Now(),
						Metadata: map[string]any{
							"line_number": lineNum + 1,
							"match":       matches[0],
						},
					}
					issues = append(issues, issue)
				}
			}
		}

		return nil
	})

	return issues, err
}

// determineSeverity determines the severity based on log content.
func (d *IssueDetector) determineSeverity(line string) IssueSeverity {
	lower := strings.ToLower(line)

	if strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
		return SeverityCritical
	}
	if strings.Contains(lower, "error") {
		return SeverityHigh
	}
	if strings.Contains(lower, "warn") {
		return SeverityMedium
	}
	return SeverityLow
}

// extractDescription extracts a description from a log line.
func (d *IssueDetector) extractDescription(line string) string {
	// Try to extract the message after common prefixes
	prefixes := []string{"ERROR:", "FATAL:", "panic:", "exception:", "error:"}
	for _, prefix := range prefixes {
		if idx := strings.Index(strings.ToLower(line), strings.ToLower(prefix)); idx != -1 {
			return strings.TrimSpace(line[idx+len(prefix):])
		}
	}

	// Truncate if too long
	if len(line) > 200 {
		return line[:200] + "..."
	}
	return line
}
