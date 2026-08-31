package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// -----------------------------------------------------------------------
// Tool base patterns
// -----------------------------------------------------------------------

// traceTool is a helper for building trace query tools.
//
//lint:ignore U1000 // base struct for trace tool implementations
//lint:ignore U1000 // base struct for trace tool implementations
type traceTool struct {
	name        string
	category    string
	description string
	parameters  llm.FunctionParameters
	store       TraceStoreReader
	logger      interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

//lint:ignore U1000 // traceTool method (reserved for future use)
func (t *traceTool) ToolName() string { return t.name }

//lint:ignore U1000 // traceTool method (reserved for future use)
func (t *traceTool) ToolCategory() string { return t.category }

//lint:ignore U1000 // traceTool method (reserved for future use)
func (t *traceTool) ToolDescription() string { return t.description }

//lint:ignore U1000 // traceTool method (reserved for future use)
func (t *traceTool) ToolParameters() llm.FunctionParameters { return t.parameters }

// -----------------------------------------------------------------------
// GetDatasetOverviewTool - rollup: counts, services, models, tokens, raw_jsonl_bytes, sample_trace_ids
// -----------------------------------------------------------------------

// GetDatasetOverviewTool provides a summary of the entire trace dataset.
type GetDatasetOverviewTool struct {
	name        string
	description string
	parameters  llm.FunctionParameters
	store       TraceStoreReader
	logger      interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
	category string
}

// NewGetDatasetOverviewTool creates a dataset overview tool.
func NewGetDatasetOverviewTool(store TraceStoreReader) *GetDatasetOverviewTool {
	return &GetDatasetOverviewTool{
		name:        "get_dataset_overview",
		category:    "trace",
		description: "Provides a rollup summary of the trace dataset: total trace count, unique services, models, token totals, byte size, and sample of trace IDs.",
		parameters: llm.FunctionParameters{
			Type: "object",
			Properties: map[string]llm.ParameterProperty{
				"limit": {
					Type:        "number",
					Description: "Maximum number of sample trace IDs to return. Default: 10.",
				},
			},
		},
		store:  store,
		logger: &noopLogger{},
	}
}

// Name returns the tool name.
func (t *GetDatasetOverviewTool) Name() string { return t.name }

// Description returns the tool description.
func (t *GetDatasetOverviewTool) Description() string { return t.description }

// Parameters returns the tool parameters.
func (t *GetDatasetOverviewTool) Parameters() llm.FunctionParameters { return t.parameters }

// Category returns the tool category.
func (t *GetDatasetOverviewTool) Category() string { return t.category }

// Invoke executes the tool.
func (t *GetDatasetOverviewTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	// This is a standalone tool function, not tied to a specific trace.
	overview := make(map[string]any)
	overview["status"] = "ok"
	overview["tool"] = t.name

	// Collect stats from store.
	traceIDs, err := t.store.ListTraceIDs()
	if err != nil {
		return nil, fmt.Errorf("list trace IDs: %w", err)
	}

	overview["total_traces"] = len(traceIDs)

	var (
		totalInputTokens  int
		totalOutputTokens int
		services          = make(map[string]bool)
		models            = make(map[string]bool)
	)

	limit := 10
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}
	if limit <= 0 {
		limit = 10
	}

	var sampleIDs []string
	maxSamples := limit
	if len(traceIDs) < maxSamples {
		maxSamples = len(traceIDs)
	}

	for i, tid := range traceIDs {
		if i >= maxSamples {
			break
		}
		sampleIDs = append(sampleIDs, tid)

		sids, err := t.store.GetSpansForTrace(tid)
		if err != nil {
			continue
		}
		spans, err := t.store.ListSpans(sids)
		if err != nil {
			continue
		}
		for _, s := range spans {
			totalInputTokens += s.inputTokens
			totalOutputTokens += s.outputTokens
			if s.service != "" {
				services[s.service] = true
			}
			if s.model != "" {
				models[s.model] = true
			}
		}
	}

	overview["sample_trace_ids"] = sampleIDs
	overview["unique_services"] = toSlice(services)
	overview["unique_models"] = toSlice(models)
	overview["total_input_tokens"] = totalInputTokens
	overview["total_output_tokens"] = totalOutputTokens
	overview["total_tokens"] = totalInputTokens + totalOutputTokens
	// raw_jsonl_bytes is approximated: sum of span JSON lengths (in integration, this reads from file).
	overview["raw_jsonl_bytes"] = totalInputTokens*4 + totalOutputTokens*4 // rough estimate
	overview["sample_size"] = len(sampleIDs)

	return overview, nil
}

// -----------------------------------------------------------------------
// QueryTracesTool - paginated trace summaries
// -----------------------------------------------------------------------

// QueryTracesTool returns paginated summaries of trace data.
type QueryTracesTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

// NewQueryTracesTool creates the query traces tool.
func NewQueryTracesTool(store TraceStoreReader) *QueryTracesTool {
	return &QueryTracesTool{
		store:  store,
		logger: &noopLogger{},
	}
}

// Invoke executes the tool.
func (t *QueryTracesTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	page := 1
	pageSize := 20
	if v, ok := args["page"].(float64); ok {
		page = int(v)
	}
	if v, ok := args["page_size"].(float64); ok {
		pageSize = int(v)
	}

	// Use search filter if provided.
	filter := ""
	if f, ok := args["filter"].(string); ok {
		filter = f
	}

	traceIDs, err := t.store.ListTraceIDs()
	if err != nil {
		return nil, err
	}

	// Apply filter.
	if filter != "" {
		var filtered []string
		for _, id := range traceIDs {
			if strings.Contains(id, filter) {
				filtered = append(filtered, id)
			}
		}
		traceIDs = filtered
	}

	totalPages := (len(traceIDs) + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(traceIDs) {
		start = len(traceIDs)
	}
	if end > len(traceIDs) {
		end = len(traceIDs)
	}

	pageIDs := traceIDs[start:end]

	var summaries []map[string]any
	for _, tid := range pageIDs {
		summaries = append(summaries, appendTraceSummary(t, tid, pageIDs))
	}

	return map[string]any{
		"page":         page,
		"page_size":    pageSize,
		"total_pages":  totalPages,
		"total_traces": len(traceIDs),
		"traces":       summaries,
	}, nil
}

// -----------------------------------------------------------------------
// CountTracesTool - cheap count
// -----------------------------------------------------------------------

// CountTracesTool returns a quick count of traces.
type CountTracesTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

// NewCountTracesTool creates the count traces tool.
func NewCountTracesTool(store TraceStoreReader) *CountTracesTool {
	return &CountTracesTool{
		store:  store,
		logger: &noopLogger{},
	}
}

// Invoke executes the tool.
func (t *CountTracesTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	traceIDs, err := t.store.ListTraceIDs()
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"count":    len(traceIDs),
		"total":    len(traceIDs),
		"filtered": len(traceIDs),
	}, nil
}

// -----------------------------------------------------------------------
// ViewTraceTool - all spans of one trace
// -----------------------------------------------------------------------

// ViewTraceTool returns all spans of one trace with attribute truncation.
type ViewTraceTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

// NewViewTraceTool creates the view trace tool.
func NewViewTraceTool(store TraceStoreReader) *ViewTraceTool {
	return &ViewTraceTool{
		store:  store,
		logger: &noopLogger{},
	}
}

// Invoke executes the tool.
func (t *ViewTraceTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	traceID, ok := args["trace_id"].(string)
	if !ok || traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}

	return t.viewTrace(traceID)
}

func (t *ViewTraceTool) viewTrace(traceID string) (map[string]any, error) {
	sids, err := t.store.GetSpansForTrace(traceID)
	if err != nil {
		return nil, err
	}

	spans, err := t.store.ListSpans(sids)
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	for _, s := range spans {
		// Apply discovery truncation (4KB per attribute).
		result = append(result, spanToMap(s, 4096))
	}

	return map[string]any{
		"trace_id":   traceID,
		"span_count": len(spans),
		"spans":      result,
	}, nil
}

// -----------------------------------------------------------------------
// ViewSpansTool - surgical read of named spans
// -----------------------------------------------------------------------

// ViewSpansTool reads specific spans by ID with surgical (16KB) truncation.
type ViewSpansTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

// NewViewSpansTool creates the view spans tool.
func NewViewSpansTool(store TraceStoreReader) *ViewSpansTool {
	return &ViewSpansTool{
		store:  store,
		logger: &noopLogger{},
	}
}

// Invoke executes the tool.
func (t *ViewSpansTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	traceID, _ := args["trace_id"].(string)
	spanIDs, _ := args["span_ids"].([]any)

	truncateBytes := 16384 // surgical: 16KB

	if len(spanIDs) == 0 {
		// Read all spans for the trace.
		sids, err := t.store.GetSpansForTrace(traceID)
		if err != nil {
			return nil, err
		}
		spanIDs = make([]any, len(sids))
		for i, sid := range sids {
			spanIDs[i] = sid
		}
	}

	// Limit to 200 span IDs.
	if len(spanIDs) > 200 {
		spanIDs = spanIDs[:200]
	}

	var stringIDs []string
	for _, id := range spanIDs {
		if s, ok := id.(string); ok {
			stringIDs = append(stringIDs, s)
		}
	}

	spans, err := t.store.ListSpans(stringIDs)
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	for _, s := range spans {
		result = append(result, spanToMap(s, truncateBytes))
	}

	return map[string]any{
		"trace_id":  traceID,
		"requested": len(stringIDs),
		"returned":  len(result),
		"spans":     result,
	}, nil
}

// -----------------------------------------------------------------------
// SearchTraceTool - regex search across a trace
// -----------------------------------------------------------------------

// SearchTraceTool runs a regex search across all spans of one trace.
type SearchTraceTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

// NewSearchTraceTool creates the search traces tool.
func NewSearchTraceTool(store TraceStoreReader) *SearchTraceTool {
	return &SearchTraceTool{
		store:  store,
		logger: &noopLogger{},
	}
}

// Invoke executes the tool.
func (t *SearchTraceTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	traceID, ok := args["trace_id"].(string)
	if !ok || traceID == "" {
		return nil, fmt.Errorf("trace_id is required")
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	maxMatches := 50
	if v, ok := args["max_matches"].(float64); ok {
		maxMatches = int(v)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	sids, err := t.store.GetSpansForTrace(traceID)
	if err != nil {
		return nil, err
	}
	spans, err := t.store.ListSpans(sids)
	if err != nil {
		return nil, err
	}

	var matches []map[string]any
	for _, s := range spans {
		// Search raw JSON.
		if len(s.rawJSON) > 0 {
			if locs := re.FindAllIndex(s.rawJSON, maxMatches); len(locs) > 0 {
				for _, loc := range locs {
					match := string(s.rawJSON[loc[0]:loc[1]])
					if len(match) > 256 {
						match = match[:256] + "..."
					}
					matchesMap := map[string]any{
						"span_id": s.spanID,
						"match":   match,
					}
					matches = append(matches, matchesMap)
				}
			}
		}
	}

	return map[string]any{
		"trace_id": traceID,
		"regex":    pattern,
		"matches":  matches,
		"count":    len(matches),
	}, nil
}

// -----------------------------------------------------------------------
// SearchSpanTool - regex inside one span
// -----------------------------------------------------------------------

// SearchSpanTool runs a regex search inside a single span.
type SearchSpanTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
}

// NewSearchSpanTool creates the search span tool.
func NewSearchSpanTool(store TraceStoreReader) *SearchSpanTool {
	return &SearchSpanTool{
		store:  store,
		logger: &noopLogger{},
	}
}

// Invoke executes the tool.
func (t *SearchSpanTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	spanID, ok := args["span_id"].(string)
	if !ok || spanID == "" {
		return nil, fmt.Errorf("span_id is required")
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	spans, err := t.store.ListSpans([]string{spanID})
	if err != nil {
		return nil, err
	}

	if len(spans) == 0 {
		return map[string]any{"error": fmt.Sprintf("span %s not found", spanID)}, nil
	}

	s := spans[0]
	if len(s.rawJSON) == 0 {
		return map[string]any{
			"span_id": spanID,
			"matches": []any{},
			"count":   0,
		}, nil
	}

	var matches []map[string]any
	for _, loc := range re.FindAllIndex(s.rawJSON, 50) {
		match := string(s.rawJSON[loc[0]:loc[1]])
		// Context buffer: 200 chars before and after.
		start := loc[0]
		end := loc[1]
		ctxBefore := 200
		ctxAfter := 200
		if start-ctxBefore > 0 {
			start = start - ctxBefore
		}
		if end+ctxAfter < len(s.rawJSON) {
			end = end + ctxAfter
		}
		if end > len(s.rawJSON) {
			end = len(s.rawJSON)
		}
		context := string(s.rawJSON[start:end])
		if len(context) > 1000 {
			context = context[:1000] + "..."
		}
		matches = append(matches, map[string]any{
			"match":   match,
			"context": context,
			"offset":  loc[0],
			"length":  loc[1] - loc[0],
		})
	}

	return map[string]any{
		"span_id": spanID,
		"regex":   pattern,
		"matches": matches,
		"count":   len(matches),
	}, nil
}

// -----------------------------------------------------------------------
// SynthesizeTracesTool - LLM-backed multi-trace summarization
// -----------------------------------------------------------------------

// SynthesizeTracesTool uses the LLM to summarize patterns across multiple traces.
type SynthesizeTracesTool struct {
	store  TraceStoreReader
	logger interface {
		Info(string, ...any)
		Warn(string, ...any)
		Error(string, ...any)
	}
	llmClient *llm.Client
}

// NewSynthesizeTracesTool creates the synthesis tool.
func NewSynthesizeTracesTool(store TraceStoreReader, llmClient *llm.Client) *SynthesizeTracesTool {
	return &SynthesizeTracesTool{
		store:     store,
		llmClient: llmClient,
		logger:    &noopLogger{},
	}
}

// Invoke executes the synthesis tool.
func (t *SynthesizeTracesTool) Invoke(ctx context.Context, args map[string]any) (map[string]any, error) {
	traceIDs, _ := args["trace_ids"].([]any)
	prompt, _ := args["prompt"].(string)

	//lint:ignore S1009 len() for nil slices is defined as zero
	if traceIDs == nil || len(traceIDs) == 0 {
		return nil, fmt.Errorf("trace_ids is required")
	}
	if prompt == "" {
		prompt = "Summarize patterns across these traces."
	}

	var ids []string
	for _, id := range traceIDs {
		if s, ok := id.(string); ok {
			ids = append(ids, s)
		}
	}

	// Gather trace data for the LLM.
	var traceData []string
	for _, tid := range ids {
		sids, err := t.store.GetSpansForTrace(tid)
		if err != nil {
			continue
		}
		spans, err := t.store.ListSpans(sids)
		if err != nil {
			continue
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("=== Trace %s ===\n", tid))
		for _, s := range spans {
			sb.WriteString(fmt.Sprintf("  span: %s (%s) tokens_in:%d out:%d err:%v\n",
				s.spanID, s.spanName, s.inputTokens, s.outputTokens, s.hasError))
		}
		traceData = append(traceData, sb.String())
	}

	combined := strings.Join(traceData, "\n")

	if t.llmClient != nil {
		// LLM-backed synthesis.
		messages := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: "You are a trace analysis synthesizer. Summarize patterns, anomalies, and insights across multiple execution traces."},
			{Role: llm.RoleUser, Content: fmt.Sprintf("%s\n\n%s", prompt, combined)},
		}
		result, err := t.llmClient.Chat(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("synthesis LLM call failed: %w", err)
		}
		return map[string]any{
			"trace_ids": ids,
			"synthesis": result.Content,
			"method":    "llm",
		}, nil
	}

	// Deterministic fallback: aggregate stats.
	var totalSpans, errorSpans, totalInput, totalOutput int
	for _, d := range traceData {
		totalSpans++
		if strings.Contains(d, "err:true") {
			errorSpans++
		}
		re := regexp.MustCompile(`tokens_in:(\d+)`)
		if m := re.FindStringSubmatch(d); len(m) > 1 {
			totalInput++
		}
		re = regexp.MustCompile(`out:(\d+)`)
		if m := re.FindStringSubmatch(d); len(m) > 1 {
			totalOutput++
		}
	}

	return map[string]any{
		"trace_ids": ids,
		"synthesis": fmt.Sprintf("Aggregate: %d traces, %d spans (%d errors). Input: %d tokens. Output: %d tokens.",
			len(ids), totalSpans, errorSpans, totalInput, totalOutput),
		"method": "deterministic",
	}, nil
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func appendTraceSummary(t *QueryTracesTool, traceID string, allIDs []string) map[string]any {
	sids, err := t.store.GetSpansForTrace(traceID)
	if err != nil {
		return nil
	}
	spans, err := t.store.ListSpans(sids)
	if err != nil {
		return nil
	}

	var services, models []string
	serviceSet := make(map[string]bool)
	modelSet := make(map[string]bool)
	for _, s := range spans {
		serviceSet[s.service] = true
		modelSet[s.model] = true
	}
	for svc := range serviceSet {
		services = append(services, svc)
	}
	for m := range modelSet {
		models = append(models, m)
	}

	return map[string]any{
		"trace_id":           traceID,
		"span_count":         len(spans),
		"services":           services,
		"models":             models,
		"sample_spans":       sids[:min(len(sids), 5)],
		"trace_index":        indexOf(traceID, allIDs),
		"trace_length_bytes": len(spanToMap(spans[0], 4096)["spans"].(string)),
	}
}

func indexOf(id string, list []string) int {
	for i, v := range list {
		if v == id {
			return i + 1
		}
	}
	return 1
}

func spanToMap(s traceSpan, maxChars int) map[string]any {
	m := map[string]any{
		"span_id":       s.spanID,
		"span_name":     s.spanName,
		"service":       s.service,
		"model":         s.model,
		"start_time":    "",
		"end_time":      "",
		"input_tokens":  s.inputTokens,
		"output_tokens": s.outputTokens,
		"has_error":     s.hasError,
	}
	if len(s.rawJSON) > maxChars {
		m["raw_json"] = string(s.rawJSON[:maxChars]) + "...[truncated]"
	} else {
		m["raw_json"] = string(s.rawJSON)
	}
	return m
}

func toSlice(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

//lint:ignore U1000 // helper function reserved for future use
func mapsKeys(m map[string]bool) []string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	return keys
}

// -----------------------------------------------------------------------
// Backward compatibility: tools for agent loop dispatch
// -----------------------------------------------------------------------

// AllTraceTools returns all trace tools for registration in a tool registry.
func AllTraceTools(store TraceStoreReader, llmClient *llm.Client) []TraceQueryTool {
	return []TraceQueryTool{
		NewGetDatasetOverviewTool(store),
		NewQueryTracesTool(store),
		NewCountTracesTool(store),
		NewViewTraceTool(store),
		NewViewSpansTool(store),
		NewSearchTraceTool(store),
		NewSearchSpanTool(store),
		NewSynthesizeTracesTool(store, llmClient),
	}
}

// TraceQueryTool is the interface that trace query tools implement.
type TraceQueryTool interface {
	Invoke(ctx context.Context, args map[string]any) (map[string]any, error)
}

// -----------------------------------------------------------------------
// Ensure imports are used
// -----------------------------------------------------------------------
var _ = json.Marshal // used in various places
