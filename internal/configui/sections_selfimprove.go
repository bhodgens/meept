// internal/configui/sections_selfimprove.go
package configui

import (
	"strings"

)

func buildSelfImproveFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.SelfImprove
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewTextField("data_dir", "data dir", s.DataDir),
		NewNumberField("auto_run_interval_hours", "auto run interval hours", s.AutoRunIntervalHours),
		NewNumberField("max_iterations_per_cycle", "max iterations per cycle", s.MaxIterationsPerCycle),
		NewNumberField("max_fixes_per_cycle", "max fixes per cycle", s.MaxFixesPerCycle),
		NewDrilldownField("ai_infra", "ai infra", []DrilldownItem{
			{Name: "ai_infra", Fields: []Field{
				NewToggleField("ai_infra.enabled", "enabled", s.AIInfra.Enabled),
				NewTextField("ai_infra.base_url", "base url", s.AIInfra.BaseURL),
				NewMaskedField("ai_infra.api_key_env", "api key env", s.AIInfra.APIKeyEnv),
				NewTextField("ai_infra.analysis_model", "analysis model", s.AIInfra.AnalysisModel),
				NewTextField("ai_infra.generation_model", "generation model", s.AIInfra.GenerationModel),
				NewTextField("ai_infra.review_model", "review model", s.AIInfra.ReviewModel),
				NewFloatField("ai_infra.timeout_seconds", "timeout seconds", s.AIInfra.TimeoutSeconds),
				NewNumberField("ai_infra.max_retries", "max retries", s.AIInfra.MaxRetries),
			}},
		}),
		NewDrilldownField("sandbox", "sandbox", []DrilldownItem{
			{Name: "sandbox", Fields: []Field{
				NewTextField("sandbox.worktree_dir", "worktree dir", s.Sandbox.WorktreeDir),
				NewToggleField("sandbox.cleanup_on_success", "cleanup on success", s.Sandbox.CleanupOnSuccess),
				NewToggleField("sandbox.cleanup_on_failure", "cleanup on failure", s.Sandbox.CleanupOnFailure),
				NewNumberField("sandbox.max_worktrees", "max worktrees", s.Sandbox.MaxWorktrees),
				NewFloatField("sandbox.test_timeout_seconds", "test timeout seconds", s.Sandbox.TestTimeoutSeconds),
			}},
		}),
		NewDrilldownField("safety", "safety", []DrilldownItem{
			{Name: "safety", Fields: []Field{
				NewToggleField("safety.require_human_approval", "require human approval", s.Safety.RequireHumanApproval),
				NewNumberField("safety.max_files_per_fix", "max files per fix", s.Safety.MaxFilesPerFix),
				NewNumberField("safety.max_lines_changed_per_fix", "max lines changed per fix", s.Safety.MaxLinesChangedPerFix),
				NewTextField("safety.blocked_paths", "blocked paths", strings.Join(s.Safety.BlockedPaths, ",")),
				NewMultiSelectField("safety.allowed_risk_levels", "allowed risk levels", s.Safety.AllowedRiskLevels, []string{"low", "medium", "high", "critical"}),
				NewToggleField("safety.block_critical_risk", "block critical risk", s.Safety.BlockCriticalRisk),
				NewToggleField("safety.require_tests_pass", "require tests pass", s.Safety.RequireTestsPass),
				NewFloatField("safety.min_confidence_threshold", "min confidence threshold", s.Safety.MinConfidenceThreshold),
			}},
		}),
		NewDrilldownField("detection", "detection", []DrilldownItem{
			{Name: "detection", Fields: []Field{
				NewToggleField("detection.scan_pytest", "scan pytest", s.Detection.ScanPytest),
				NewToggleField("detection.scan_runtime_logs", "scan runtime logs", s.Detection.ScanRuntimeLogs),
				NewToggleField("detection.scan_type_check", "scan type check", s.Detection.ScanTypeCheck),
				NewToggleField("detection.scan_lint", "scan lint", s.Detection.ScanLint),
				NewTextField("detection.log_file", "log file", s.Detection.LogFile),
				NewNumberField("detection.log_lookback_hours", "log lookback hours", s.Detection.LogLookbackHours),
				NewTextField("detection.pytest_args", "pytest args", strings.Join(s.Detection.PytestArgs, ",")),
				NewTextField("detection.mypy_args", "mypy args", strings.Join(s.Detection.MypyArgs, ",")),
				NewTextField("detection.ruff_args", "ruff args", strings.Join(s.Detection.RuffArgs, ",")),
				NewTextField("detection.code_error_patterns", "code error patterns", strings.Join(s.Detection.CodeErrorPatterns, ",")),
				NewNumberField("detection.max_code_issues_per_file", "max code issues per file", s.Detection.MaxCodeIssuesPerFile),
				NewToggleField("detection.deduplicate_todos", "deduplicate todos", s.Detection.DeduplicateTODOs),
			}},
		}),
	}
}
