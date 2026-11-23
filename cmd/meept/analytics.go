package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/spf13/cobra"

	_ "modernc.org/sqlite"
)

func newAnalyticsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analytics",
		Short: "Query and display analytics data",
		Long: `Query and display analytics data about agent task performance.

Commands:
  summary  Show performance summary by agent
  errors   Show error breakdown by type
  models   Compare model performance
  reviews  Show review verdict rollups (model rating)
  export   Export raw analytics data as JSON`,
	}

	cmd.AddCommand(newAnalyticsSummaryCmd())
	cmd.AddCommand(newAnalyticsErrorsCmd())
	cmd.AddCommand(newAnalyticsModelsCmd())
	cmd.AddCommand(newAnalyticsReviewsCmd())
	cmd.AddCommand(newAnalyticsExportCmd())

	return cmd
}

func newAnalyticsSummaryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show performance summary by agent (last 7 days)",
		Long: `Show performance summary grouped by agent_id.

Displays:
- agent_id: The agent identifier
- total_tasks: Total number of tasks
- successes: Number of successful tasks
- success_rate: Percentage of successful tasks
- avg_duration_ms: Average task duration in milliseconds
- avg_tokens: Average total tokens (input + output)`,
		RunE: runAnalyticsSummary,
	}

	return cmd
}

func runAnalyticsSummary(cmd *cobra.Command, args []string) error {
	db, err := openMetricsDB()
	if err != nil {
		return fmt.Errorf("failed to open metrics database: %w", err)
	}
	defer db.Close()

	query := `
		SELECT
			agent_id,
			COUNT(*) as total_tasks,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successes,
			(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) * 100.0 / COUNT(*)) as success_rate,
			AVG(duration_ms) as avg_duration_ms,
			AVG(tokens_input + tokens_output) as avg_tokens
		FROM agent_task_outcomes
		WHERE timestamp > datetime('now', '-7 days')
		GROUP BY agent_id
		ORDER BY success_rate DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query summary: %w", err)
	}
	defer rows.Close()

	type SummaryRow struct {
		AgentID       string  `db:"agent_id"`
		TotalTasks    int     `db:"total_tasks"`
		Successes     int     `db:"successes"`
		SuccessRate   float64 `db:"success_rate"`
		AvgDurationMs float64 `db:"avg_duration_ms"`
		AvgTokens     float64 `db:"avg_tokens"`
	}

	var results []SummaryRow
	for rows.Next() {
		var r SummaryRow
		if err := rows.Scan(&r.AgentID, &r.TotalTasks, &r.Successes, &r.SuccessRate, &r.AvgDurationMs, &r.AvgTokens); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed during row iteration: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No analytics data available")
		return nil
	}

	// Print results with tabwriter
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "%-20s\t%s\t%s\t%s\t%s\t%s\n", "AGENT_ID", "TOTAL_TASKS", "SUCCESSES", "SUCCESS_RATE", "AVG_DURATION_MS", "AVG_TOKENS")
	fmt.Fprintf(w, "%-20s\t%s\t%s\t%s\t%s\t%s\n", "--------", "----------", "---------", "------------", "---------------", "----------")

	for _, r := range results {
		agentID := r.AgentID
		if len(agentID) > 20 {
			agentID = agentID[:17] + "..."
		}
		fmt.Fprintf(w, "%-20s\t%d\t%d\t%.1f%%\t%.0f\t%.0f\n",
			agentID, r.TotalTasks, r.Successes, r.SuccessRate, r.AvgDurationMs, r.AvgTokens)
	}

	return nil
}

func newAnalyticsErrorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "errors",
		Short: "Show error breakdown by type (last 7 days)",
		Long: `Show error breakdown grouped by error_type.

Displays:
- error_type: The type of error
- count: Number of occurrences
- affected_tasks: Number of unique tasks affected
- resolved_count: Number of resolved errors`,
		RunE: runAnalyticsErrors,
	}

	return cmd
}

func runAnalyticsErrors(cmd *cobra.Command, args []string) error {
	db, err := openMetricsDB()
	if err != nil {
		return fmt.Errorf("failed to open metrics database: %w", err)
	}
	defer db.Close()

	query := `
		SELECT
			error_type,
			COUNT(*) as count,
			COUNT(DISTINCT task_id) as affected_tasks,
			SUM(CASE WHEN resolved = 1 THEN 1 ELSE 0 END) as resolved_count
		FROM agent_errors
		WHERE timestamp > datetime('now', '-7 days')
		GROUP BY error_type
		ORDER BY count DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query errors: %w", err)
	}
	defer rows.Close()

	type ErrorRow struct {
		ErrorType     string `db:"error_type"`
		Count         int    `db:"count"`
		AffectedTasks int    `db:"affected_tasks"`
		ResolvedCount int    `db:"resolved_count"`
	}

	var results []ErrorRow
	for rows.Next() {
		var r ErrorRow
		if err := rows.Scan(&r.ErrorType, &r.Count, &r.AffectedTasks, &r.ResolvedCount); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed during row iteration: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No errors recorded")
		return nil
	}

	// Print results with tabwriter
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "%-30s\t%s\t%s\t%s\n", "ERROR_TYPE", "COUNT", "AFFECTED_TASKS", "RESOLVED")
	fmt.Fprintf(w, "%-30s\t%s\t%s\t%s\n", "----------", "-----", "---------------", "--------")

	for _, r := range results {
		errorType := r.ErrorType
		if len(errorType) > 30 {
			errorType = errorType[:27] + "..."
		}
		fmt.Fprintf(w, "%-30s\t%d\t%d\t%d\n",
			errorType, r.Count, r.AffectedTasks, r.ResolvedCount)
	}

	return nil
}

func newAnalyticsModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Compare model performance (last 30 days)",
		Long: `Show model performance comparison grouped by model_id.

Displays:
- model_id: The model identifier
- tasks: Total number of tasks
- success_rate: Percentage of successful tasks
- avg_cost: Average estimated cost in cents
- avg_duration: Average task duration in milliseconds`,
		RunE: runAnalyticsModels,
	}

	return cmd
}

func runAnalyticsModels(cmd *cobra.Command, args []string) error {
	db, err := openMetricsDB()
	if err != nil {
		return fmt.Errorf("failed to open metrics database: %w", err)
	}
	defer db.Close()

	query := `
		SELECT
			model_id,
			COUNT(*) as tasks,
			(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) * 100.0 / COUNT(*)) as success_rate,
			AVG(estimated_cost_cents) as avg_cost,
			AVG(duration_ms) as avg_duration
		FROM agent_task_outcomes
		WHERE timestamp > datetime('now', '-30 days')
		  AND model_id IS NOT NULL
		GROUP BY model_id
		ORDER BY success_rate DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query models: %w", err)
	}
	defer rows.Close()

	type ModelRow struct {
		ModelID     string  `db:"model_id"`
		Tasks       int     `db:"tasks"`
		SuccessRate float64 `db:"success_rate"`
		AvgCost     float64 `db:"avg_cost"`
		AvgDuration float64 `db:"avg_duration"`
	}

	var results []ModelRow
	for rows.Next() {
		var r ModelRow
		if err := rows.Scan(&r.ModelID, &r.Tasks, &r.SuccessRate, &r.AvgCost, &r.AvgDuration); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed during row iteration: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No model data available")
		return nil
	}

	// Print results with tabwriter
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, "%-30s\t%s\t%s\t%s\t%s\n", "MODEL_ID", "TASKS", "SUCCESS_RATE", "AVG_COST", "AVG_DURATION")
	fmt.Fprintf(w, "%-30s\t%s\t%s\t%s\t%s\n", "--------", "-----", "------------", "--------", "------------")

	for _, r := range results {
		modelID := r.ModelID
		if len(modelID) > 30 {
			modelID = modelID[:27] + "..."
		}
		avgCostStr := "n/a"
		if r.AvgCost != 0 {
			avgCostStr = fmt.Sprintf("%.2f", r.AvgCost)
		}
		fmt.Fprintf(w, "%-30s\t%d\t%.1f%%\t%s\t%.0f\n",
			modelID, r.Tasks, r.SuccessRate, avgCostStr, r.AvgDuration)
	}

	return nil
}

func newAnalyticsExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export analytics data as JSON",
		Long: `Export all analytics data from agent_task_outcomes as JSON.

Outputs all fields including:
- task_id, agent_id, skill_name, status
- success, iterations, duration_ms
- tokens_input, tokens_output, estimated_cost_cents
- response_well_formed, syntax_errors_count, etc.
- model_id, edit_format`,
		RunE: runAnalyticsExport,
	}

	return cmd
}

func runAnalyticsExport(cmd *cobra.Command, args []string) error {
	db, err := openMetricsDB()
	if err != nil {
		return fmt.Errorf("failed to open metrics database: %w", err)
	}
	defer db.Close()

	query := `
		SELECT
			id, timestamp, task_id, agent_id, skill_name, status,
			success, iterations, duration_ms, tokens_input, tokens_output,
			estimated_cost_cents, response_well_formed, syntax_errors_count,
			indentation_errors_count, lazy_response_detected, context_exhausted,
			reflection_iterations, reflection_successful, user_interventions,
			user_satisfaction, model_id, edit_format
		FROM agent_task_outcomes
		ORDER BY timestamp DESC
		LIMIT 1000
	`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query export data: %w", err)
	}
	defer rows.Close()

	type OutcomeRow struct {
		ID                     int             `json:"id"`
		Timestamp              string          `json:"timestamp"`
		TaskID                 string          `json:"task_id"`
		AgentID                string          `json:"agent_id"`
		SkillName              sql.NullString  `json:"skill_name"`
		Status                 sql.NullString  `json:"status"`
		Success                sql.NullBool    `json:"success"`
		Iterations             sql.NullInt64   `json:"iterations"`
		DurationMs             sql.NullInt64   `json:"duration_ms"`
		TokensInput            sql.NullInt64   `json:"tokens_input"`
		TokensOutput           sql.NullInt64   `json:"tokens_output"`
		EstimatedCostCents     sql.NullFloat64 `json:"estimated_cost_cents"`
		ResponseWellFormed     sql.NullBool    `json:"response_well_formed"`
		SyntaxErrorsCount      sql.NullInt64   `json:"syntax_errors_count"`
		IndentationErrorsCount sql.NullInt64   `json:"indentation_errors_count"`
		LazyResponseDetected   sql.NullBool    `json:"lazy_response_detected"`
		ContextExhausted       sql.NullBool    `json:"context_exhausted"`
		ReflectionIterations   sql.NullInt64   `json:"reflection_iterations"`
		ReflectionSuccessful   sql.NullBool    `json:"reflection_successful"`
		UserInterventions      sql.NullInt64   `json:"user_interventions"`
		UserSatisfaction       sql.NullInt64   `json:"user_satisfaction"`
		ModelID                sql.NullString  `json:"model_id"`
		EditFormat             sql.NullString  `json:"edit_format"`
	}

	var results []OutcomeRow
	for rows.Next() {
		var r OutcomeRow
		if err := rows.Scan(
			&r.ID, &r.Timestamp, &r.TaskID, &r.AgentID, &r.SkillName, &r.Status,
			&r.Success, &r.Iterations, &r.DurationMs, &r.TokensInput, &r.TokensOutput,
			&r.EstimatedCostCents, &r.ResponseWellFormed, &r.SyntaxErrorsCount,
			&r.IndentationErrorsCount, &r.LazyResponseDetected, &r.ContextExhausted,
			&r.ReflectionIterations, &r.ReflectionSuccessful, &r.UserInterventions,
			&r.UserSatisfaction, &r.ModelID, &r.EditFormat,
		); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed during row iteration: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("[]")
		return nil
	}

	// Convert to JSON-friendly format
	type ExportRow struct {
		ID                 int      `json:"id"`
		Timestamp          string   `json:"timestamp"`
		TaskID             string   `json:"task_id"`
		AgentID            string   `json:"agent_id"`
		SkillName          *string  `json:"skill_name,omitempty"`
		Status             *string  `json:"status,omitempty"`
		Success            *bool    `json:"success,omitempty"`
		Iterations         *int64   `json:"iterations,omitempty"`
		DurationMs         *int64   `json:"duration_ms,omitempty"`
		TokensInput        *int64   `json:"tokens_input,omitempty"`
		TokensOutput       *int64   `json:"tokens_output,omitempty"`
		EstimatedCostCents *float64 `json:"estimated_cost_cents,omitempty"`
		ModelID            *string  `json:"model_id,omitempty"`
		EditFormat         *string  `json:"edit_format,omitempty"`
	}

	var exportData []ExportRow
	for _, r := range results {
		row := ExportRow{
			ID:        r.ID,
			Timestamp: r.Timestamp,
			TaskID:    r.TaskID,
			AgentID:   r.AgentID,
		}
		if r.SkillName.Valid {
			row.SkillName = &r.SkillName.String
		}
		if r.Status.Valid {
			row.Status = &r.Status.String
		}
		if r.Success.Valid {
			row.Success = &r.Success.Bool
		}
		if r.Iterations.Valid {
			row.Iterations = &r.Iterations.Int64
		}
		if r.DurationMs.Valid {
			row.DurationMs = &r.DurationMs.Int64
		}
		if r.TokensInput.Valid {
			row.TokensInput = &r.TokensInput.Int64
		}
		if r.TokensOutput.Valid {
			row.TokensOutput = &r.TokensOutput.Int64
		}
		if r.EstimatedCostCents.Valid {
			row.EstimatedCostCents = &r.EstimatedCostCents.Float64
		}
		if r.ModelID.Valid {
			row.ModelID = &r.ModelID.String
		}
		if r.EditFormat.Valid {
			row.EditFormat = &r.EditFormat.String
		}
		exportData = append(exportData, row)
	}

	data, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

// openMetricsDB opens the metrics database and returns a connection.
func openMetricsDB() (*sql.DB, error) {
	dbPath := metrics.DefaultDatabasePath
	expandedPath := expandUserPath(dbPath)

	db, err := sql.Open("sqlite", expandedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", expandedPath, err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// expandUserPath expands ~ to the home directory.
func expandUserPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return path
	}

	if path == "~" {
		return homeDir
	}
	return homeDir + path[1:]
}

// newAnalyticsReviewsCmd builds the `analytics reviews` subcommand (plan
// model-rating-metrics-plan, contract C6): a readable verdict rollup table
// over a day window, straight from the metrics DB.
func newAnalyticsReviewsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reviews",
		Short: "Show review verdict rollups by model, judge, category, or rubric",
		Long: `Show review verdict rollups grouped by model, judge, category, or rubric.

Displays:
- group: The grouped identifier (model id, judge id, category, or rubric id)
- verdicts: Total review verdicts in the group
- defects: Verdicts with a non-pass category
- pass_rate: Fraction of pass-category verdicts
- label: Rubric statement (group-by rubric only)`,
		RunE: runAnalyticsReviewsRunE,
	}

	cmd.Flags().Int("days", 30, "look-back window in days")
	cmd.Flags().String("group-by", "model", "group rows by model, judge, category, rubric, or model_category")
	cmd.Flags().Bool("include-workflow", false, "include workflow-scope verdicts")
	cmd.Flags().Bool("json", false, "emit machine-readable JSON mirroring GET /api/v1/metrics/reviews")

	return cmd
}

// runAnalyticsReviewsRunE is the cobra RunE for `analytics reviews`; the
// gate test's runAnalyticsReviews helper drives it through the command tree.
func runAnalyticsReviewsRunE(cmd *cobra.Command, args []string) error {
	days, err := cmd.Flags().GetInt("days")
	if err != nil {
		return fmt.Errorf("failed to read days flag: %w", err)
	}
	groupBy, err := cmd.Flags().GetString("group-by")
	if err != nil {
		return fmt.Errorf("failed to read group-by flag: %w", err)
	}
	includeWorkflow, err := cmd.Flags().GetBool("include-workflow")
	if err != nil {
		return fmt.Errorf("failed to read include-workflow flag: %w", err)
	}
	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("failed to read json flag: %w", err)
	}

	// Validate group-by BEFORE opening the store so a typo fails fast with a
	// clear message instead of a confusing store-level error.
	validGroupBys := map[string]bool{"model": true, "judge": true, "category": true, "rubric": true, "model_category": true}
	if !validGroupBys[groupBy] {
		return fmt.Errorf("invalid --group-by %q (want model, judge, category, rubric, or model_category)", groupBy)
	}

	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  config.MeeptPath("metrics.db"),
		BatchSize:     metrics.DefaultBatchSize,
		FlushInterval: metrics.DefaultFlushInterval,
	})
	if err != nil {
		return fmt.Errorf("failed to open metrics database: %w", err)
	}
	defer store.Close()

	since := time.Now().UTC().AddDate(0, 0, -days)

	// Breakdown mode: per-(model, category) counts — the issue's question 2
	// ("what kind of bad work did each model do") in one query.
	if groupBy == "model_category" {
		breakdown, err := store.QueryReviewVerdictBreakdown(since)
		if err != nil {
			return fmt.Errorf("failed to query review verdict breakdown: %w", err)
		}
		if asJSON {
			return printAnalyticsReviewsJSON(cmd.OutOrStdout(), days, groupBy, nil, breakdown)
		}
		if len(breakdown) == 0 {
			fmt.Println("(no verdicts recorded)")
			return nil
		}
		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		defer w.Flush()
		fmt.Fprintln(w, "MODEL	CATEGORY	COUNT")
		fmt.Fprintln(w, "-----	--------	-----")
		for _, r := range breakdown {
			model := r.Model
			if len(model) > 30 {
				model = model[:27] + "..."
			}
			fmt.Fprintf(w, "%-30s	%s	%d\n", model, r.Category, r.Count)
		}
		return nil
	}

	rows, err := store.QueryReviewVerdicts(metrics.ReviewVerdictQuery{
		Since:           since,
		GroupBy:         groupBy,
		IncludeWorkflow: includeWorkflow,
	})
	if err != nil {
		return fmt.Errorf("failed to query review verdicts: %w", err)
	}

	if asJSON {
		return printAnalyticsReviewsJSON(cmd.OutOrStdout(), days, groupBy, rows, nil)
	}

	if len(rows) == 0 {
		fmt.Println("(no verdicts recorded)")
		return nil
	}

	// Contract sort: defects DESC, then group ASC.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Defects != rows[j].Defects {
			return rows[i].Defects > rows[j].Defects
		}
		return rows[i].Group < rows[j].Group
	})

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	defer w.Flush()

	header := "GROUP\tVERDICTS\tDEFECTS\tPASS_RATE"
	if groupBy == "rubric" {
		header += "\tLABEL"
	}
	fmt.Fprintln(w, header)
	sep := "-----\t--------\t-------\t---------"
	if groupBy == "rubric" {
		sep += "\t-----"
	}
	fmt.Fprintln(w, sep)

	for _, r := range rows {
		group := r.Group
		if len(group) > 30 {
			group = group[:27] + "..."
		}
		line := fmt.Sprintf("%-30s	%d	%d	%.2f", group, r.Verdicts, r.Defects, r.PassRate)
		if groupBy == "rubric" {
			label := r.Label
			if label == "" {
				label = "-"
			}
			line += "	" + label
		}
		fmt.Fprintln(w, line)
	}

	return nil
}

// analyticsReviewsJSON is the --json payload of `meept analytics reviews`.
// It mirrors GET /api/v1/metrics/reviews' frozen shape: window (the CLI's
// --days N rendered in the endpoint's token form), group_by, and the two
// always-present arrays — rows for single-dimension modes, breakdown for
// model_category. Scripts can consume CLI and HTTP output identically.
type analyticsReviewsJSON struct {
	Window           string                             `json:"window"`
	GroupBy          string                             `json:"group_by"`
	Rows             []analyticsReviewsRowJSON          `json:"rows"`
	Breakdown        []analyticsReviewsBreakdownRowJSON `json:"breakdown"`
	WorkflowExcluded int64                              `json:"workflow_excluded"`
}

type analyticsReviewsRowJSON struct {
	Group    string  `json:"group"`
	Verdicts int64   `json:"verdicts"`
	Defects  int64   `json:"defects"`
	PassRate float64 `json:"pass_rate"`
	Label    string  `json:"label"`
}

type analyticsReviewsBreakdownRowJSON struct {
	Model    string `json:"model"`
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// printAnalyticsReviewsJSON renders the --json payload. Exactly one of rows /
// breakdown is populated per mode; the other stays an empty array so the
// shape is stable for consumers.
func printAnalyticsReviewsJSON(out io.Writer, days int, groupBy string, rows []metrics.ReviewVerdictRow, breakdown []metrics.ReviewVerdictBreakdownRow) error {
	payload := analyticsReviewsJSON{
		Window:    fmt.Sprintf("%dd", days),
		GroupBy:   groupBy,
		Rows:      []analyticsReviewsRowJSON{},
		Breakdown: []analyticsReviewsBreakdownRowJSON{},
	}
	for _, r := range rows {
		payload.Rows = append(payload.Rows, analyticsReviewsRowJSON{
			Group:    r.Group,
			Verdicts: r.Verdicts,
			Defects:  r.Defects,
			PassRate: r.PassRate,
			Label:    r.Label,
		})
	}
	for _, r := range breakdown {
		payload.Breakdown = append(payload.Breakdown, analyticsReviewsBreakdownRowJSON{
			Model:    r.Model,
			Category: r.Category,
			Count:    r.Count,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return fmt.Errorf("failed to encode analytics reviews JSON: %w", err)
	}
	return nil
}
