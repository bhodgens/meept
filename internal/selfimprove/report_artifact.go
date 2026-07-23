package selfimprove

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// ReportFormat enum for output format selection.
type ReportFormat string

const (
	ReportFormatMarkdown ReportFormat = "markdown"
	ReportFormatJSON     ReportFormat = "json"
	ReportFormatHTML     ReportFormat = "html"
)

// ReportConfig holds configuration for the ReportArtifact generator.
type ReportConfig struct {
	OutputDir            string       `json:"output_dir"`
	Format               ReportFormat `json:"format"`
	IncludeTraces        bool         `json:"include_traces"`
	IncludeRecommendations bool       `json:"include_recommendations"`
	CleanupAfterDays     int          `json:"cleanup_after_days"`
	// ModelsUsed records which LLM models were invoked during the analysis run.
	ModelsUsed []string `json:"models_used,omitempty"`
	// EmployeeID identifies the employee that triggered the report.
	EmployeeID string `json:"employee_id,omitempty"`
}

// DefaultReportConfig returns sensible defaults.
func DefaultReportConfig() ReportConfig {
	homeDir, _ := os.UserHomeDir()
	return ReportConfig{
		OutputDir:            filepath.Join(homeDir, ".meept", "reports"),
		Format:               ReportFormatMarkdown,
		IncludeTraces:        true,
		IncludeRecommendations: true,
		CleanupAfterDays:     30,
	}
}

// Validate returns an error if the config is misconfigured.
func (r *ReportConfig) Validate() error {
	switch r.Format {
	case ReportFormatMarkdown, ReportFormatJSON, ReportFormatHTML:
		// valid
	default:
		return fmt.Errorf("report format must be one of: markdown, json, html; got %q", r.Format)
	}
	if r.OutputDir == "" {
		r.OutputDir = DefaultReportConfig().OutputDir
	}
	if r.CleanupAfterDays < 0 {
		r.CleanupAfterDays = 0
	}
	return nil
}

// FailureMode is a simplified failure representation for reports.
type FailureMode struct {
	Category       string `json:"category"`
	Description    string `json:"description"`
	Severity       string `json:"severity"`
	TraceID        string `json:"trace_id,omitempty"`
	Model          string `json:"model,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

// ReportArtifact generates self-improvement analysis reports to disk.
type ReportArtifact struct {
	config   ReportConfig
	lastPath string
}

// NewReportArtifact creates a new ReportArtifact.
func NewReportArtifact(cfg ReportConfig) (*ReportArtifact, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("report config validate: %w", err)
	}
	return &ReportArtifact{
		config: cfg,
	}, nil
}

// Generate creates a report from the given data and saves it to disk.
// Creates a timestamped subdirectory under config.OutputDir containing:
//
//	- report.md/json/html (main report)
//	- traces.jsonl (optional, when IncludeTraces)
//	- metadata.json (timestamps, models, etc.)
func (ra *ReportArtifact) Generate(failures []FailureMode, analysis string) error {
	return ra.GenerateWithMeta(failures, analysis, nil, "")
}

// GenerateWithMeta creates a report with extra metadata for the models used.
func (ra *ReportArtifact) GenerateWithMeta(failures []FailureMode, analysis string, modelsUsed []string, employeeID string) error {
	dir, err := ra.ensureOutputDir()
	if err != nil {
		return fmt.Errorf("report artifact mkdir: %w", err)
	}

	// Cleanup old reports if configured
	if ra.config.CleanupAfterDays > 0 {
		_ = ra.cleanupOld(dir)
	}

	ts := time.Now().UTC()
	subDir := filepath.Join(dir, "self-improve-"+ts.Format("2006-01-02T15-04-05Z"))
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		return fmt.Errorf("report artifact subdir mkdir: %w", err)
	}

	// Write traces
	if ra.config.IncludeTraces {
		if err := ra.writeTraces(subDir, failures); err != nil {
			return fmt.Errorf("report artifact traces: %w", err)
		}
	}

	// Write main report
	var reportPath string
	switch ra.config.Format {
	case ReportFormatMarkdown:
		reportPath, err = ra.writeMarkdown(subDir, failures, analysis)
	case ReportFormatJSON:
		reportPath, err = ra.writeJSON(subDir, failures, analysis)
	case ReportFormatHTML:
		reportPath, err = ra.writeHTML(subDir, failures, analysis)
	default:
		return fmt.Errorf("unsupported report format: %s", ra.config.Format)
	}
	if err != nil {
		return fmt.Errorf("report artifact write: %w", err)
	}

	// Write metadata
	meta := &ReportMetadata{
		Timestamp:      ts,
		Format:         string(ra.config.Format),
		ModelsUsed:     modelsUsed,
		EmployeeID:     employeeID,
		FailureCount: len(failures),
		AnalysisLength: len(analysis),
		OutputDir:      subDir,
	}
	if err := ra.writeMetadata(subDir, meta); err != nil {
		return fmt.Errorf("report artifact metadata: %w", err)
	}

	ra.lastPath = reportPath
	return nil
}

// Save persists the report to disk (alias for Generate to match interface expectations).
// The primary entry point is Generate; Save is a no-op that returns the last path.
func (ra *ReportArtifact) Save() (path string, err error) {
	return ra.lastPath, nil
}

// GetPath returns the path of the last generated report.
func (ra *ReportArtifact) GetPath() string {
	return ra.lastPath
}

func (ra *ReportArtifact) ensureOutputDir() (string, error) {
	dir := ra.config.OutputDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (ra *ReportArtifact) cleanupOld(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // best-effort
	}
	cut := time.Now().AddDate(0, 0, -ra.config.CleanupAfterDays)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "self-improve-") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cut) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
	return nil
}

func (ra *ReportArtifact) writeTraces(dir string, failures []FailureMode) error {
	path := filepath.Join(dir, "traces.jsonl")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, fm := range failures {
		// Only write traces that have a trace_id
		if fm.TraceID == "" {
			continue
		}
		line, err := json.Marshal(fm)
		if err != nil {
			continue
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------
// Markdown report
// -----------------------------------------------------------------------

func (ra *ReportArtifact) writeMarkdown(dir string, failures []FailureMode, analysis string) (string, error) {
	var sb strings.Builder
	sb.WriteString("# Self-Improvement Report\n\n")
	sb.WriteString("## Executive Summary\n\n")
	sb.WriteString(fmt.Sprintf("- **Generated**: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **Failure Modes Detected**: %d\n", len(failures)))
	sb.WriteString("- **Severity Breakdown**:\n\n")

	sevCount := make(map[string]int)
	for _, fm := range failures {
		sevCount[fm.Severity]++
	}
	for sev, count := range sevCount {
		sb.WriteString(fmt.Sprintf("  - %s: %d\n", sev, count))
	}

	if analysis != "" {
		sb.WriteString("\n## Analysis\n\n")
		sb.WriteString(strings.TrimSpace(strings.ReplaceAll(analysis, "```", "")))
	}

	if len(failures) > 0 {
		sb.WriteString("\n## Failure Modes\n\n")
		for i, fm := range failures {
			sb.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, fm.Category))
			sb.WriteString(fmt.Sprintf("**Severity**: %s\n\n", fm.Severity))
			sb.WriteString(fmt.Sprintf("%s\n\n", fm.Description))
			if fm.TraceID != "" {
				sb.WriteString(fmt.Sprintf("**Trace ID**: `%s`\n\n", fm.TraceID))
			}
			if fm.Model != "" {
				sb.WriteString(fmt.Sprintf("**Model**: `%s`\n\n", fm.Model))
			}
			if ra.config.IncludeRecommendations && fm.Recommendation != "" {
				sb.WriteString(fmt.Sprintf("**Recommendation**: %s\n\n", fm.Recommendation))
			}
		}
	}

	if ra.config.IncludeTraces {
		sb.WriteString("\n## Trace Evidence\n\n")
		sb.WriteString("Traces are provided as a separate `traces.jsonl` file in this report directory.\n")
	}

	path := filepath.Join(dir, "report.md")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// -----------------------------------------------------------------------
// JSON report
// -----------------------------------------------------------------------

type reportJSON struct {
	Timestamp              string           `json:"timestamp"`
	Format                 string           `json:"format"`
	Failures               []FailureMode    `json:"failures"`
	Analysis               string           `json:"analysis"`
	IncludeTraces          bool             `json:"include_traces"`
	IncludeRecommendations bool             `json:"include_recommendations"`
	FailureCount           int                `json:"failure_count"`
}

func (ra *ReportArtifact) writeJSON(dir string, failures []FailureMode, analysis string) (string, error) {
	data := reportJSON{
		Timestamp:              time.Now().UTC().Format(time.RFC3339),
		Format:                 string(ra.config.Format),
		Failures:               failures,
		Analysis:               analysis,
		IncludeTraces:          ra.config.IncludeTraces,
		IncludeRecommendations: ra.config.IncludeRecommendations,
		FailureCount:         len(failures),
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "report.json")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// -----------------------------------------------------------------------
// HTML report
// -----------------------------------------------------------------------

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Self-Improvement Report</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; max-width: 900px; margin: 0 auto; padding: 2rem; background: #fafafa; color: #333; }
  h1 { color: #1a1a2e; border-bottom: 2px solid #16213e; padding-bottom: 0.5rem; }
  h2 { color: #16213e; margin-top: 2rem; }
  h3 { color: #0f3460; }
  .summary { background: #e8eaf6; border-left: 4px solid #3f51b5; padding: 1rem; margin: 1rem 0; }
  .summary li { margin: 0.25rem 0; }
  .failure { background: #fff; border: 1px solid #ddd; border-radius: 4px; margin: 1rem 0; overflow: hidden; }
  .failure-header { background: #f5f5f5; padding: 0.75rem 1rem; cursor: pointer; user-select: none; display: flex; justify-content: space-between; align-items: center; }
  .failure-header:hover { background: #eeeeee; }
  .severity-critical { color: #d32f2f; font-weight: bold; }
  .severity-high { color: #f57c00; font-weight: bold; }
  .severity-medium { color: #fbc02d; font-weight: bold; }
  .severity-low { color: #388e3c; }
  .failure-body { padding: 1rem; display: none; }
  .failure.open .failure-body { display: block; }
  code { background: #f5f5f5; padding: 0.125rem 0.375rem; border-radius: 3px; font-size: 0.9em; }
  .trace-link { display: inline-block; background: #3f51b5; color: #fff; padding: 0.25rem 0.75rem; border-radius: 3px; text-decoration: none; margin-top: 0.5rem; }
  .analysis { background: #fff; border: 1px solid #e0e0e0; padding: 1rem; border-radius: 4px; white-space: pre-wrap; line-height: 1.6; }
  footer { margin-top: 2rem; text-align: center; color: #999; font-size: 0.85rem; }
</style>
<script>
  document.addEventListener('DOMContentLoaded', function() {
    document.querySelectorAll('.failure-header').forEach(function(header) {
      header.addEventListener('click', function() {
        var failureEl = this.parentElement;
        failureEl.classList.toggle('open');
        var arrow = this.querySelector('.arrow');
        arrow.textContent = failureEl.classList.contains('open') ? '\u25B2' : '\u25BC';
      });
    });
  });
</script>
</head>
<body>
<h1>Self-Improvement Report</h1>
<div class="summary">
  <ul>
    <li><strong>Generated</strong>: {{.Timestamp}}</li>
    <li><strong>Failure Modes Detected</strong>: {{.FailureCount}}</li>
    <li><strong>Format</strong>: {{.Format}}</li>
    {{if .ModelsUsed}}<li><strong>Models Used</strong>: {{range .ModelsUsed}}{{.}} {{end}}</li>{{end}}
  </ul>
</div>

<h2>Analysis</h2>
<div class="analysis">{{.Analysis}}</div>

{{if gt (len .Failures) 0}}<h2>Failure Modes ({{len .Failures}})</h2>{{end}}
{{range $i, $fm := .Failures}}
<div class="failure {{if (eq $i 0)}}open{{end}}">
  <div class="failure-header">
    <span><strong>{{.Category}}</strong> <span style="color:#666">({{.TraceID}})</span></span>
    <span><span class="severity-{{.Severity}}">{{.Severity}}</span> <span class="arrow">▼</span></span>
  </div>
  <div class="failure-body">
    <p>{{.Description}}</p>
    {{if .Model}}<p><strong>Model</strong>: <code>{{.Model}}</code></p>{{end}}
    {{if .TraceID}}<p><strong>Trace ID</strong>: <code>{{.TraceID}}</code></p>{{end}}
    {{if $.IncludeRecommendations}}
      {{if .Recommendation}}<p><strong>Recommendation</strong>: {{.Recommendation}}</p>{{end}}
    {{end}}
    <a class="trace-link" href="traces.jsonl">View traces</a>
  </div>
</div>
{{end}}

{{if .IncludeTraces}}
<h2>Trace Evidence</h2>
<p>Full trace data is available in <a href="traces.jsonl">traces.jsonl</a>.</p>
{{end}}

<footer>Generated by meept self-improvement system</footer>
</body>
</html>`

type htmlReportData struct {
	Timestamp              string
	FailureCount           int
	Format                 string
	ModelsUsed             []string
	Failures               []FailureMode
	Analysis               string
	IncludeRecommendations bool
	IncludeTraces          bool
}

func (ra *ReportArtifact) writeHTML(dir string, failures []FailureMode, analysis string) (string, error) {
	data := htmlReportData{
		Timestamp:              time.Now().UTC().Format(time.RFC3339),
		FailureCount:           len(failures),
		Format:                 string(ra.config.Format),
		ModelsUsed:             make([]string, 0),
		Failures:               failures,
		Analysis:               analysis,
		IncludeRecommendations: ra.config.IncludeRecommendations,
		IncludeTraces:          ra.config.IncludeTraces,
	}

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return "", fmt.Errorf("html template parse: %w", err)
	}

	path := filepath.Join(dir, "report.html")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", err
	}
	return path, nil
}

// -----------------------------------------------------------------------
// Metadata
// -----------------------------------------------------------------------

// ReportMetadata captures static information about a generated report.
type ReportMetadata struct {
	Timestamp        time.Time  `json:"timestamp"`
	Format           string     `json:"format"`
	ModelsUsed       []string   `json:"models_used,omitempty"`
	EmployeeID       string     `json:"employee_id,omitempty"`
	FailureCount     int        `json:"failure_count"`
	AnalysisLength   int        `json:"analysis_length"`
	OutputDir        string     `json:"output_dir"`
}

func (ra *ReportArtifact) writeMetadata(dir string, meta *ReportMetadata) error {
	out, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "metadata.json")
	return os.WriteFile(path, out, 0o644)
}
