// Package selfimprove provides the self-improvement system for meept.
package selfimprove

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

// ScanLogs scans log files for errors.
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
	err := filepath.Walk(d.projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
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
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "/_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
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
