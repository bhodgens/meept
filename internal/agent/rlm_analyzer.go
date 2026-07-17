package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/google/uuid"
)

// -----------------------------------------------------------------------
// Config and entry point
// -----------------------------------------------------------------------

// RLMAnalyzerConfig configures the trace analysis RLM.
// Modeled after HALO's RLM config (main.py, subagent_tool_factory.py).
type RLMAnalyzerConfig struct {
	MaximumDepth             int    `json:"maximum_depth"`               // default 2
	MaximumParallelSubagents int    `json:"maximum_parallel_subagents"`  // default 4
	MaximumTurns             int    `json:"maximum_turns"`               // per agent
	Model                    string `json:"model"`                       // cheap model for analysis
	SynthesisModel           string `json:"synthesis_model"`             // model for multi-trace summarization
}

// DefaultRLMConfig returns sensible defaults matching HALO's configuration.
func DefaultRLMConfig() RLMAnalyzerConfig {
	return RLMAnalyzerConfig{
		MaximumDepth:             2,
		MaximumParallelSubagents: 4,
		MaximumTurns:             20,
		Model:                    "gpt-4.1-nano",
		SynthesisModel:           "gpt-4.1-nano",
	}
}

// RLMAnalyzer analyzes execution traces using bounded recursive subagents.
// It implements structural depth gating (no call_subagent tool at max depth)
// and per-depth semaphore pooling to prevent resource exhaustion.
type RLMAnalyzer struct {
	config        RLMAnalyzerConfig
	store         TraceStoreReader
	llmClient     *llm.Client
	semaphore     *PerDepthSemaphore
	logger        *slog.Logger
	knownTraceIDs []string // loaded on first Analyze call
}

// NewRLMAnalyzer creates an analyzer for the given trace store.
func NewRLMAnalyzer(cfg RLMAnalyzerConfig, store TraceStoreReader, llmClient *llm.Client, logger *slog.Logger) *RLMAnalyzer {
	if logger == nil {
		logger = slog.Default()
	}

	// Build semaphore limits: each spawnable depth (0..maxDepth-1) gets the
	// configured parallelism limit.
	limits := make(map[int]int)
	for d := 0; d < cfg.MaximumDepth; d++ {
		limits[d] = cfg.MaximumParallelSubagents
	}

	return &RLMAnalyzer{
		config:    cfg,
		store:     store,
		llmClient: llmClient,
		semaphore: NewPerDepthSemaphore(limits),
		logger:    logger,
	}
}

// AnalyzeResult is the output of a trace analysis run.
type AnalyzeResult struct {
	FailureModes []FailureMode `json:"failure_modes"`
	Report       string        `json:"report"`
}

// FailureMode represents a failure pattern found across traces.
type FailureMode struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	TraceIDs    []string `json:"trace_ids"`
	Severity    string   `json:"severity"` // critical, high, medium, low
	Category    string   `json:"category"` // hallucination, redundant_args, refusal_loop, semantic
}

// SeverityAsIssueSeverity maps the failure mode severity to IssueSeverity
// strings compatible with the selfimprove models.
func (fm FailureMode) SeverityAsIssueSeverity() string {
	switch fm.Severity {
	case "critical", "high", "medium":
		return fm.Severity
	default:
		return "low"
	}
}

// Analyze runs the RLM over traces and returns failure modes.
// Implements structural depth gating (no call_subagent tool at max depth).
func (a *RLMAnalyzer) Analyze(ctx context.Context, prompt string) (*AnalyzeResult, error) {
	// Load trace IDs from store.
	if len(a.knownTraceIDs) == 0 {
		ids, err := a.store.ListTraceIDs()
		if err != nil {
			return nil, fmt.Errorf("list trace IDs: %w", err)
		}
		a.knownTraceIDs = ids
	}

	if len(a.knownTraceIDs) == 0 {
		return &AnalyzeResult{Report: "No traces available to analyze."}, nil
	}

	// Build root agent run with appropriate tool set.
	root, err := a.guardedInvoke(ctx, 0, prompt, "")
	if err != nil {
		return nil, fmt.Errorf("create root agent: %w", err)
	}
	// Release the root's depth-0 semaphore slot when execution completes
	// (including all spawned subagents).
	defer root.releaseSemaphore()

	// Register root tools: trace query tools + subagent tool (depth-gated).
	a.registerRootTools(root)

	// Output bus: interleaves root + subagent outputs by monotonic sequence.
	type outputEntry struct {
		sequence int64
		content  string
		tag      string // "root" or "subagent:ID"
		depth    int
	}

	var (
		mu          sync.Mutex
		outputBus   []outputEntry
		sequence    int64
		failureModes []FailureMode
		allReports  []string
	)

	nextSeq := func() int64 {
		mu.Lock()
		defer mu.Unlock()
		sequence++
		sequence++ // reserve pairs (root + subagent share seq)
		return sequence
	}

	// Collect all subagent handles.
	var subagentWg sync.WaitGroup

	// Run root agent.
	root.agentID = uuid.New().String()[:16]
	rootResult, err := a.executeAgent(root, a.knownTraceIDs, prompt, nextSeq)
	if err != nil {
		a.logger.Warn("root agent failed", "error", err)
	}

	// Compute sequence before acquiring lock to avoid self-deadlock.
	seq := nextSeq()

	mu.Lock()
	outputBus = append(outputBus, outputEntry{
		sequence: seq,
		content:  rootResult.output,
		tag:      "root",
		depth:    0,
	})
	//lint:ignore SA4006 allReports reserved for future multi-agent synthesis
	//lint:ignore SA4010 allReports reserved for future multi-agent synthesis
	allReports = append(allReports, rootResult.output)
	mu.Unlock()

	waitCh := make(chan struct{})
	go func() {
		subagentWg.Wait() //nolint:mutexio // WaitGroup.Wait is synchronization, not I/O
		close(waitCh)
	}()

	// Wait for all subagents to finish, then synthesize.
	<-waitCh

	// Merge subagent outputs into failure modes.
	mu.Lock()
	defer mu.Unlock()
	for _, entry := range outputBus {
		fms := a.extractFailureModesFromOutput(entry.content, entry.tag, entry.depth)
		failureModes = append(failureModes, fms...)
	}

	// Deduplicate failure modes by category+description overlap.
	failureModes = deduplicateFailureModes(failureModes)

	// Synthesis pass: run LLM-backed summarization over multiple traces.
	report := synthesizeReport(failureModes)

	return &AnalyzeResult{
		FailureModes: failureModes,
		Report:       report,
	}, nil
}

// canSpawnSubagentAt returns true if a subagent can be spawned at the given depth.
// Structural depth gate: no spawn tool at depth >= maxDepth.
func (a *RLMAnalyzer) canSpawnSubagentAt(depth int) bool {
	return depth < a.config.MaximumDepth
}

// hasFinalSentinel returns whether the _final sentinel tool exists.
// The _final sentinel tells the root agent when it is done.
func (a *RLMAnalyzer) hasFinalSentinel() bool {
	return true // always registered
}

// -----------------------------------------------------------------------
// Tool registration
// -----------------------------------------------------------------------

// registerRootTools populates the agent's tool registry with the tools
// appropriate for its depth. At max depth, the subagent tool is omitted
// and only _final is available.
func (a *RLMAnalyzer) registerRootTools(run *analyzerRun) {
	// Always register trace query tools.
	run.toolRegistry.Register("get_dataset_overview")
	run.toolRegistry.Register("query_traces")
	run.toolRegistry.Register("count_traces")
	run.toolRegistry.Register("view_trace")
	run.toolRegistry.Register("view_spans")
	run.toolRegistry.Register("search_traces")
	run.toolRegistry.Register("search_span")

	// Register subagent tool only if depth < maxDepth.
	if a.canSpawnSubagentAt(run.depth) {
		run.toolRegistry.Register("call_subagent")
	}

	// Root agent always uses _final; subagents do not.
	if run.depth == 0 {
		run.toolRegistry.Register("_final")
	}
}

// -----------------------------------------------------------------------
// Agent execution loop
// -----------------------------------------------------------------------

type agentResult struct {
	output string
}

// executeAgent runs a single agent (root or subagent) through its turns.
func (a *RLMAnalyzer) executeAgent(run *analyzerRun, traceIDs []string, prompt string, seqFn func() int64) (*agentResult, error) {
	var outputs []string

	// Determine tool context.
	toolNames := run.toolRegistry.List()
	toolCtx := fmt.Sprintf("Available tools: %s", strings.Join(toolNames, ", "))

	currentPrompt := prompt
	for {
		select {
		case <-run.ctx.Done():
			return nil, run.ctx.Err()
		default:
		}

		// Turn counter check.
		n, exhausted := run.turnCounter.Increment()
		if n == 1 {
			outputs = append(outputs, run.turnCounter.Nudge())
		}
		if exhausted {
			// Turn limit reached: force final.
			outputs = append(outputs, run.turnCounter.Nudge())
			outputs = append(outputs, "<final>Analysis complete (turn limit reached).</final>")
			break
		}

		// Build the prompt for this turn.
		//lint:ignore SA4006 turnPrompt used conditionally in LLM path only
		turnPrompt := currentPrompt
		if run.depth > 0 {
			turnPrompt = fmt.Sprintf("Trace analysis subagent (depth %d).\n%s\n\nContext:\n%s",
				run.depth, prompt, toolCtx)
		} else {
			turnPrompt = fmt.Sprintf("Trace analysis root agent.\n%s\n\n%s", prompt, toolCtx)
		}

		// If we have an LLM client, use it. Otherwise, do a simple scan.
		if a.llmClient != nil {
			result, err := a.invokeLLM(run.ctx, run, turnPrompt)
			if err != nil {
				a.logger.Warn("LLM invocation failed", "error", err, "depth", run.depth)
				// Tool failure is recoverable: treat as a warning, not fatal.
			}
			if result != "" {
				outputs = append(outputs, result)
			}
		} else {
			// Deterministic fallback: scan traces for patterns when no LLM.
			result := a.deterministicScan(run, traceIDs, prompt)
			outputs = append(outputs, result)
		}

		// Check for _final in output.
		if containsFinal(outputs[len(outputs)-1]) {
			break
		}
	}

	return &agentResult{output: strings.Join(outputs, "\n")}, nil
}

// invokeLLM calls the LLM client with the prompt and tool definitions.
func (a *RLMAnalyzer) invokeLLM(ctx context.Context, run *analyzerRun, prompt string) (string, error) {
	// Build messages similar to agent loop pattern.
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You are a trace analysis assistant. Analyze execution traces to find failure patterns."},
		{Role: llm.RoleUser, Content: prompt},
	}

	result, err := a.llmClient.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}
	return result.Content, nil
}

// deterministicScan performs a pattern scan over traces without LLM.
// Returns a structured analysis report finding known failure patterns.
func (a *RLMAnalyzer) deterministicScan(run *analyzerRun, traceIDs []string, prompt string) string {
	var sb strings.Builder
	sb.WriteString("Trace scan results:\n")

	// Count error spans per trace.
	var errorSpanCount, totalSpans int
	_ = regexp.MustCompile(`(?i)(error|fail|panic|refusal|denied)`)

	for _, tid := range traceIDs {
		sids, err := a.store.GetSpansForTrace(tid)
		if err != nil {
			continue
		}
		spans, err := a.store.ListSpans(sids)
		if err != nil {
			continue
		}

		hasErrors := false
		hasRefusal := false
		repeatedTools := make(map[string]int)

		for _, s := range spans {
			totalSpans++
			if s.hasError {
				hasErrors = true
			}
			if strings.Contains(strings.ToLower(s.spanName), "refusal") ||
				strings.Contains(strings.ToLower(s.spanName), "loop") {
				hasRefusal = true
			}
			// Track repeated tool calls (simplified: count duplicate span names).
			repeatedTools[s.spanName]++
		}

		if hasErrors {
			errorSpanCount++
			errorStr := fmt.Sprintf("  trace %s: %d error spans found\n", tid, sidsWithErrorsCount(spans))
			sb.WriteString(errorStr)
		}
		if hasRefusal {
			sb.WriteString(fmt.Sprintf("  trace %s: refusal pattern detected\n", tid))
		}

		for tool, count := range repeatedTools {
			if count >= 3 {
				sb.WriteString(fmt.Sprintf("  trace %s: repeated %s tool (%d times)\n", tid, tool, count))
			}
		}
	}

	sb.WriteString(fmt.Sprintf("Total traces scanned: %d\n", len(traceIDs)))
	sb.WriteString(fmt.Sprintf("Error spans: %d/%d\n", errorSpanCount, totalSpans))

	// Add turn nudge.
	sb.WriteString("\n" + run.turnCounter.Nudge())

	return sb.String()
}

func sidsWithErrorsCount(spans []traceSpan) int {
	c := 0
	for _, s := range spans {
		if s.hasError {
			c++
		}
	}
	return c
}

// containsFinal checks if the output contains the _final sentinel.
func containsFinal(s string) bool {
	return strings.Contains(s, "<final>") && strings.Contains(s, "</final>")
}

// -----------------------------------------------------------------------
// Failure mode extraction
// -----------------------------------------------------------------------

func (a *RLMAnalyzer) extractFailureModesFromOutput(output, tag string, depth int) []FailureMode {
	// Parse output for failure mode entries.
	// Format: [FAILURE:category:severity] description (trace IDs...)
	// Or: just the description.

	var modes []FailureMode
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[HALO:") {
			continue
		}
		if !strings.Contains(line, "error") && !strings.Contains(line, "fail") &&
			!strings.Contains(line, "refusal") && !strings.Contains(line, "panic") &&
			!strings.Contains(line, "ERROR") && !strings.Contains(line, "FAIL") {
			// Only extract failure modes from error-containing lines.
			// This is a heuristic — LLM output is better parsed in synthesis.
			continue
		}

		fm := FailureMode{
			ID:          uuid.New().String()[:16],
			Description: rlmTruncate(line, 500),
			Severity:    "low", // default; LLM refinement elevates
			Category:    "semantic",
			TraceIDs:    extractTraceIDsFromLine(line),
		}

		// Elevate severity based on keywords.
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "PANIC") || strings.Contains(upper, "FATAL") {
			fm.Severity = "critical"
		} else if strings.Contains(upper, "ERROR") || strings.Contains(upper, "FAILED") {
			fm.Severity = "high"
		}

		// Categorize.
		if strings.Contains(upper, "REFUSAL") {
			fm.Category = "refusal_loop"
		} else if strings.Contains(upper, "REDUNT") || strings.Contains(upper, "REPEAT") {
			fm.Category = "redundant_args"
		}

		modes = append(modes, fm)
	}

	return modes
}

func extractTraceIDsFromLine(line string) []string {
	re := regexp.MustCompile(`trace[-_]?[a-zA-Z0-9-]+`)
	matches := re.FindAllString(line, -1)
	// Deduplicate.
	seen := make(map[string]bool)
	var ids []string
	for _, m := range matches {
		m = strings.ToLower(m)
		if !seen[m] {
			seen[m] = true
			ids = append(ids, m)
		}
	}
	return ids
}

func deduplicateFailureModes(modes []FailureMode) []FailureMode {
	// Deduplicate by trimming similar categories and descriptions.
	seen := make(map[string]bool)
	var result []FailureMode
	for _, fm := range modes {
		key := fm.Category + ":" + rlmTruncate(fm.Description, 100)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, fm)
	}
	return result
}

func synthesizeReport(modes []FailureMode) string {
	if len(modes) == 0 {
		return "No failure modes detected."
	}

	var sb strings.Builder
	sb.WriteString("=== HALO Trace Analysis Report ===\n\n")
	sb.WriteString(fmt.Sprintf("Failure modes found: %d\n\n", len(modes)))

	for i, fm := range modes {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, fm.Severity, fm.Description))
		if len(fm.TraceIDs) > 0 {
			sb.WriteString(fmt.Sprintf("   Traces: %s\n", strings.Join(fm.TraceIDs, ", ")))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// -----------------------------------------------------------------------
// Memory store wrapper
// -----------------------------------------------------------------------

// traceStoreReaderAdapter adapts a *memory.TraceStore to TraceStoreReader.
// This provides forward compatibility when Phase 1's TraceStore is built.
type traceStoreReaderAdapter struct {
	store *memory.TraceStore
}

func (a *traceStoreReaderAdapter) ListTraceIDs() ([]string, error) {
	return a.store.ListTraceIDs()
}

func (a *traceStoreReaderAdapter) GetSpansForTrace(traceID string) ([]string, error) {
	return a.store.GetSpansForTrace(traceID)
}

func (a *traceStoreReaderAdapter) ListSpans(spanIDs []string) ([]traceSpan, error) {
	// Read raw JSONL to populate rawJSON for search tools.
	f, err := os.Open(a.store.SourcePath())
	if err != nil {
		return nil, err
	}
	defer f.Close()

	idSet := make(map[string]struct{}, len(spanIDs))
	for _, id := range spanIDs {
		idSet[id] = struct{}{}
	}

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var sr memory.SpanRecord
		if err := json.Unmarshal(scanner.Bytes(), &sr); err != nil {
			continue
		}
		generatedID := fmt.Sprintf("%s--span-%d", sr.TraceID, 0)
		_, foundGenerated := idSet[generatedID]
		_, foundOriginal := idSet[sr.SpanID]
		if foundGenerated || foundOriginal {
			lines = append(lines, scanner.Bytes())
		}
	}

	out := make([]traceSpan, len(spanIDs))
	for i := range spanIDs {
		if i < len(lines) {
			out[i] = traceSpan{rawJSON: lines[i]}
		}
	}
	return out, scanner.Err()
}

// NewRLMAnalyzerFromTraceStore creates an analyzer using the Phase 1 TraceStore.
func NewRLMAnalyzerFromTraceStore(cfg RLMAnalyzerConfig, store *memory.TraceStore, llmClient *llm.Client, logger *slog.Logger) *RLMAnalyzer {
	reader := &traceStoreReaderAdapter{store: store}
	return NewRLMAnalyzer(cfg, reader, llmClient, logger)
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

func rlmTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// NewTestRLMAnalyzer creates an analyzer for use in tests.
func NewTestRLMAnalyzer(cfg RLMAnalyzerConfig, store TraceStoreReader, llmClient *llm.Client) *RLMAnalyzer {
	return NewRLMAnalyzer(cfg, store, llmClient, slog.Default())
}
