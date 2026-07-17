package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Truncation budgets model HALO's two-tier pattern.
// Discovery (view_trace / search_trace) uses a tighter cap so the agent
// can inspect many spans at once; surgical (view_spans) allows 4x because
// the agent has already identified which span it cares about.
const (
	DiscoveryAttrTruncationChars   = 4096   // 4 KB for view_trace / search_trace
	SurgicalAttrTruncationChars    = 16384  // 16 KB for view_spans (4x — "zoom in")
	ViewTraceResponseBytesBudget   = 150000 // ~150 KB total response budget
	ViewSpansMaxIDs                = 200    // max span_ids per view_spans call
)

// OversizedTraceSummary is returned when a trace would exceed the response
// budget. Rather than silently truncating or erroring, we return planning
// metadata so the agent can choose a more targeted follow-up.
// Modeled after HALO's OversizedTraceSummary (trace_query_models.py:135).
type OversizedTraceSummary struct {
	SpanCount            int      `json:"span_count"`
	SpanResponseBytesMax int      `json:"span_response_bytes_max"`
	TopSpanNames         []string `json:"top_span_names"`
	ErrorSpanCount       int      `json:"error_span_count"`
	Recommendation       string   `json:"recommendation"`
}

// SearchMatch represents a span that matched a search regex, with context.
type SearchMatch struct {
	SpanID   string `json:"span_id"`
	TraceID  string `json:"trace_id"`
	Scope    string `json:"scope,omitempty"`
	LineNum  int    `json:"line_num"`
	MatchLen int    `json:"match_len"`
	Raw      string `json:"raw,omitempty"`
}

// SearchTraceResult is the response from SearchTrace.
type SearchTraceResult struct {
	TraceID   string       `json:"trace_id,omitempty"`
	Pattern   string       `json:"pattern"`
	TotalHits int          `json:"total_hits"`
	Matches   []SearchMatch `json:"matches"`
}

// ViewTraceResult is a union type -- either spans or an oversized summary.
type ViewTraceResult struct {
	Spans     []SpanRecord         `json:"spans,omitempty"`
	Oversized *OversizedTraceSummary `json:"oversized,omitempty"`
}

// TraceStore provides surgical read/query/render over trace JSONL via
// a sidecar index. All read methods are read-only and safe for concurrent use.
type TraceStore struct {
	mu            sync.RWMutex
	sourcePath    string
	indexPath     string
	rowsByTraceID map[string]TraceIndexRow
	built         bool
}

// TraceIndexBuilder builds the sidecar index for a source JSONL file and
// exposes a single entry point for loading trace rows.
type TraceIndexBuilder struct {
	sourcePath string
	indexPath  string
	metaPath   string
}

// NewTraceIndexBuilder creates a builder for the given source JSONL.
func NewTraceIndexBuilder(sourcePath string) *TraceIndexBuilder {
	base := strings.TrimSuffix(sourcePath, ".jsonl")
	return &TraceIndexBuilder{
		sourcePath: sourcePath,
		indexPath:  base + ".meept-index.jsonl",
		metaPath:   base + ".meept-index.meta.json",
	}
}

// NewTraceStore creates a store backed by the given source JSONL and optional
// builder. If a builder is provided it is used to build the sidecar index
// and populate rows; otherwise the store scans source directly.
func NewTraceStore(sourcePath string, builder *TraceIndexBuilder) (*TraceStore, error) {
	st := &TraceStore{
		sourcePath:    sourcePath,
		rowsByTraceID: make(map[string]TraceIndexRow),
	}

	if builder == nil {
		builder = NewTraceIndexBuilder(sourcePath)
	}
	st.indexPath = builder.indexPath

	rows, err := builder.indexRowsFromSource(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("trace store: %w", err)
	}
	st.rowsByTraceID = make(map[string]TraceIndexRow, len(rows))
	for _, r := range rows {
		st.rowsByTraceID[r.TraceID] = r
	}
	st.built = true
	return st, nil
}

// LoadTraceStore is a convenience wrapper that constructs a builder,
// calls BuildOrReuse to get (or build) the sidecar index, and returns
// a fully-populated TraceStore.
func LoadTraceStore(sourcePath string) (*TraceStore, error) {
	builder := NewTraceIndexBuilder(sourcePath)
	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		return nil, err
	}

	st := &TraceStore{
		sourcePath:    sourcePath,
		indexPath:     builder.indexPath,
		rowsByTraceID: make(map[string]TraceIndexRow, len(result.Rows)),
		built:         true,
	}
	for _, r := range result.Rows {
		st.rowsByTraceID[r.TraceID] = r
	}
	return st, nil
}

// IndexResult holds both index rows and metadata from a build.
type IndexResult struct {
	Rows []TraceIndexRow
	Meta TraceIndexMeta
}

// isStale checks whether the meta fingerprint matches current source file stats.
func (b *TraceIndexBuilder) isStale(meta *TraceIndexMeta, srcStat fs.FileInfo) bool {
	if srcStat.Size() != meta.SourceSize {
		return true
	}
	if srcStat.ModTime().UnixNano() != meta.SourceMtimeNs {
		return true
	}
	return false
}

// loadMeta reads the meta JSON file.
func (b *TraceIndexBuilder) loadMeta() (*TraceIndexMeta, error) {
	data, err := os.ReadFile(b.metaPath)
	if err != nil {
		return nil, err
	}
	var meta TraceIndexMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// loadIndex reads the index JSONL file and returns rows + meta.
func (b *TraceIndexBuilder) loadIndex() (*IndexResult, error) {
	data, err := os.ReadFile(b.indexPath)
	if err != nil {
		return nil, err
	}
	var rows []TraceIndexRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	meta, err := b.loadMeta()
	if err != nil {
		return nil, err
	}
	return &IndexResult{Rows: rows, Meta: *meta}, nil
}

// BuildOrReuse builds the sidecar index or returns cached if source unchanged.
// Checks staleness via source size + mtime fingerprint.
// Writes atomically via .tmp -> rename.
func (b *TraceIndexBuilder) BuildOrReuse(ctx context.Context) (*IndexResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Check staleness via meta fingerprint.
	if meta, err := b.loadMeta(); err == nil {
		srcStat, err := os.Stat(b.sourcePath)
		if err == nil && !b.isStale(meta, srcStat) {
			return b.loadIndex()
		}
	}

	// Build from scratch: scan source, accumulate rows, write atomically.
	rows, err := b.indexRowsFromSource(b.sourcePath)
	if err != nil {
		return nil, err
	}

	// Write index atomically.
	tmpPath := b.indexPath + ".tmp"
	tmpMetaPath := b.metaPath + ".tmp"

	// Write index.
	idxData, err := json.Marshal(&rows)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(tmpPath, idxData, 0o644); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, b.indexPath); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	// Write meta atomically.
	srcStat, _ := os.Stat(b.sourcePath)
	meta := TraceIndexMeta{
		SchemaVersion: CurrentSchemaVersion,
		TraceCount:    len(rows),
		BuiltAt:       time.Now().UTC(),
	}
	if srcStat != nil {
		meta.SourceSize = srcStat.Size()
		meta.SourceMtimeNs = srcStat.ModTime().UnixNano()
	}
	metaData, err := json.Marshal(&meta)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(tmpMetaPath, metaData, 0o644); err != nil {
		os.Remove(tmpMetaPath)
		return nil, err
	}
	if err := os.Rename(tmpMetaPath, b.metaPath); err != nil {
		os.Remove(tmpMetaPath)
		return nil, err
	}

	return &IndexResult{Rows: rows, Meta: meta}, nil
}

// GetTraceIDs returns all known trace IDs.
func (s *TraceStore) GetTraceIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.rowsByTraceID))
	for id := range s.rowsByTraceID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// SourcePath returns the path to the source JSONL file backing the store.
func (s *TraceStore) SourcePath() string {
	return s.sourcePath
}

// ListTraceIDs returns all known trace IDs (satisfies TraceStoreReader).
func (s *TraceStore) ListTraceIDs() ([]string, error) {
	ids := s.GetTraceIDs()
	return ids, nil
}

// SpanView is the simplified span view used by the RLM analyzer.
type SpanView struct {
	SpanID       string
	SpanName     string
	Service      string
	Model        string
	InputTokens  int
	OutputTokens int
	HasError     bool
}

// GetSpansForTrace returns the span IDs for a given trace from the sidecar index.
func (s *TraceStore) GetSpansForTrace(traceID string) ([]string, error) {
	s.mu.RLock()
	row, ok := s.rowsByTraceID[traceID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown trace: %s", traceID)
	}
	ids := make([]string, len(row.ByteOffsets))
	for i := range ids {
		ids[i] = fmt.Sprintf("%s--span-%d", traceID, i)
	}
	return ids, nil
}

// ListSpans returns span views by IDs, reading directly from source JSONL.
func (s *TraceStore) ListSpans(spanIDs []string) ([]SpanView, error) {
	f, err := os.Open(s.sourcePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	idSet := make(map[string]struct{}, len(spanIDs))
	for _, id := range spanIDs {
		idSet[id] = struct{}{}
	}

	var results []SpanView
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var sr SpanRecord
		if jErr := json.Unmarshal(scanner.Bytes(), &sr); jErr != nil {
			continue
		}
		if sr.TraceID == "" {
			continue
		}
		generatedID := fmt.Sprintf("%s--span-%d", sr.TraceID, len(results))
		_, foundGenerated := idSet[generatedID]
		_, foundID := idSet[sr.SpanID]
		if !foundGenerated && !foundID {
			continue
		}

		delete(idSet, generatedID)
		delete(idSet, sr.SpanID)

		spanName := sr.Service
		if sr.ToolName != "" {
			spanName = sr.ToolName
		}
		results = append(results, SpanView{
			SpanID:       sr.SpanID,
			SpanName:     spanName,
			Service:      sr.Service,
			Model:        sr.Model,
			InputTokens:  sr.InputTokens,
			OutputTokens: sr.OutputTokens,
			HasError:     sr.HasError,
		})

		if len(idSet) == 0 {
			break
		}
	}
	return results, scanner.Err()
}

// GetTraceIndexRow returns the index row for a trace, if known.
func (s *TraceStore) GetTraceIndexRow(traceID string) (TraceIndexRow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.rowsByTraceID[traceID]
	return row, ok
}

// ---------- ViewTrace ----------

// ViewTrace returns all spans of one trace.
//
// Each string attribute is truncated to DiscoveryAttrTruncationChars (4 KB).
// The total raw response is budgeted at ViewTraceResponseBytesBudget (~150 KB).
// When the budget would be exceeded the method returns an OversizedTraceSummary
// instead, guiding the agent toward targeted search + view_spans.
func (s *TraceStore) ViewTrace(traceID string) (*ViewTraceResult, error) {
	s.mu.RLock()
	row, ok := s.rowsByTraceID[traceID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown trace: %s", traceID)
	}

	spans, totalBytes, err := s.readSpansForTrace(traceID, row)
	if err != nil {
		return nil, err
	}

	if totalBytes > ViewTraceResponseBytesBudget {
		return &ViewTraceResult{
			Oversized: &OversizedTraceSummary{
				SpanCount:            row.SpanCount,
				SpanResponseBytesMax: totalBytes,
				TopSpanNames:         topNValues(row.ServiceNames, 10),
				ErrorSpanCount:       row.OtelErrorSpanCount + row.ToolErrorSpanCount,
				Recommendation:       buildRecommendation("search_trace"),
			},
		}, nil
	}

	return &ViewTraceResult{Spans: spans}, nil
}

// ---------- view_spans ----------

// ViewSpans returns up to 200 named span_ids with a surgical 16 KB
// per-attribute cap. Like ViewTrace it gates on the overall budget,
// but the per-attribute cap is higher so the agent actually gets
// more bytes per span than via ViewTrace -- the deliberate "zoom in"
// affordance that makes view_spans complementary to search_trace.
func (s *TraceStore) ViewSpans(traceID string, spanIDs []string) (*ViewTraceResult, error) {
	if len(spanIDs) == 0 {
		return nil, fmt.Errorf("view_spans: no span IDs provided")
	}
	if len(spanIDs) > ViewSpansMaxIDs {
		return nil, fmt.Errorf("view_spans: too many span IDs (max %d)", ViewSpansMaxIDs)
	}

	s.mu.RLock()
	row, hasIdx := s.rowsByTraceID[traceID]
	s.mu.RUnlock()

	var spans []SpanRecord
	totalBytes := 0
	idSet := make(map[string]struct{}, len(spanIDs))
	for _, id := range spanIDs {
		idSet[id] = struct{}{}
	}

	if hasIdx && len(row.ByteOffsets) > 0 {
		// Use sidecar index for seeking.
		spans, totalBytes = s.readSpansByID(row, idSet)
	} else {
		// Full scan fallback.
		spans, totalBytes = s.scanSpans(traceID, idSet)
	}

	if totalBytes > ViewTraceResponseBytesBudget {
		return &ViewTraceResult{
			Oversized: &OversizedTraceSummary{
				SpanCount:            len(spans),
				SpanResponseBytesMax: totalBytes,
				TopSpanNames: func() []string {
					names := make([]string, len(spans))
					for i, sp := range spans {
						names[i] = sp.Service
					}
					return topNValues(names, 10)
				}(),
				ErrorSpanCount: countErrors(spans),
				Recommendation: fmt.Sprintf(
					"Trace exceeds %.1f KB budget (%d spans). Provide fewer span_ids to view_spans, or use search_trace to find relevant spans first.",
					ViewTraceResponseBytesBudget/1024.0,
					len(spans),
				),
			},
		}, nil
	}

	return &ViewTraceResult{Spans: spans}, nil
}

// ---------- search_trace ----------

// SearchTrace runs a compiled regexp over raw JSONL lines of one trace and returns
// up to maxMatches SearchMatch records with context windows.
func (s *TraceStore) SearchTrace(traceID, pattern string, maxMatches int) (*SearchTraceResult, error) {
	if _, err := regexp.Compile(pattern); err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}
	if maxMatches <= 0 {
		maxMatches = 50
	}

	matches, totalHits, err := searchLinesFiltered(s.sourcePath, traceID, pattern, maxMatches)
	if err != nil {
		return nil, err
	}

	return &SearchTraceResult{
		TraceID:   traceID,
		Pattern:   pattern,
		TotalHits: totalHits,
		Matches:   matches,
	}, nil
}

// SearchSpans is a convenience wrapper around SearchTrace for agent use.
// spanPattern is matched against span_id/service/model fields;
// attrPattern is matched against arbitrary JSON content.
func (s *TraceStore) SearchSpans(traceID, spanPattern, attrPattern string, maxMatches int) (*SearchTraceResult, error) {
	parts := []string{}
	if spanPattern != "" {
		parts = append(parts, spanPattern)
	}
	if attrPattern != "" {
		parts = append(parts, attrPattern)
	}
	pattern := strings.Join(parts, "|")
	if pattern == "" {
		pattern = "."
	}
	return s.SearchTrace(traceID, pattern, maxMatches)
}

// ---------- internal readers ----------

// readSpansForTrace reads all spans of the trace via the sidecar index,
// applying discovery-level truncation and accumulating raw byte size.
func (s *TraceStore) readSpansForTrace(traceID string, row TraceIndexRow) ([]SpanRecord, int, error) {
	f, err := os.Open(s.sourcePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var spans []SpanRecord
	totalBytes := 0

	for i, off := range row.ByteOffsets {
		length := row.ByteLengths[i]
		buf := make([]byte, length)
		if _, err := f.ReadAt(buf, off); err != nil {
			return nil, 0, err
		}

		var sr SpanRecord
		if jErr := json.Unmarshal(buf, &sr); jErr != nil {
			continue
		}
		if sr.TraceID != traceID {
			continue
		}

		spans = append(spans, truncateAttributes(sr, DiscoveryAttrTruncationChars))
		totalBytes += len(buf)
	}

	return spans, totalBytes, nil
}

// readSpansByID reads only the spans whose IDs are in idSet, applying
// surgical truncation. Returns spans and total raw byte size.
func (s *TraceStore) readSpansByID(row TraceIndexRow, idSet map[string]struct{}) ([]SpanRecord, int) {
	f, err := os.Open(s.sourcePath)
	if err != nil {
		return nil, 0
	}
	defer f.Close()

	var spans []SpanRecord
	totalBytes := 0

	for i, off := range row.ByteOffsets {
		length := row.ByteLengths[i]
		buf := make([]byte, length)
		if _, err := f.ReadAt(buf, off); err != nil {
			break
		}

		var sr SpanRecord
		if jErr := json.Unmarshal(buf, &sr); jErr != nil {
			continue
		}
		if _, present := idSet[sr.SpanID]; !present {
			continue
		}

		spans = append(spans, truncateAttributes(sr, SurgicalAttrTruncationChars))
		delete(idSet, sr.SpanID)
		totalBytes += len(buf)

		if len(idSet) == 0 {
			break // found all requested IDs
		}
	}

	return spans, totalBytes
}

// scanSpans is a fallback when no index exists: linear scan of JSONL.
func (s *TraceStore) scanSpans(traceID string, idSet map[string]struct{}) ([]SpanRecord, int) {
	f, err := os.Open(s.sourcePath)
	if err != nil {
		return nil, 0
	}
	defer f.Close()

	var spans []SpanRecord
	totalBytes := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1 MB per line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var sr SpanRecord
		if jErr := json.Unmarshal(line, &sr); jErr != nil {
			continue
		}
		if sr.TraceID != traceID {
			continue
		}
		if _, present := idSet[sr.SpanID]; !present {
			continue
		}

		spans = append(spans, truncateAttributes(sr, SurgicalAttrTruncationChars))
		delete(idSet, sr.SpanID)
		totalBytes += len(line)

		if len(idSet) == 0 {
			break
		}
	}

	return spans, totalBytes
}

// ---------- search helpers ----------

// searchLines scans a JSONL file for lines matching a regex, returning up
// to maxMatches SearchMatch records. totalHits is the total (unbounded) count.
//lint:ignore U1000 reserved for future search implementation
func searchLines(filePath, pattern string, maxMatches int) ([]SearchMatch, int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid regex: %w", err)
	}

	ctx := bufio.NewReaderSize(f, 64*1024)
	var matches []SearchMatch
	totalHits := 0
	lineNum := 0

	for {
		line, err := ctx.ReadString('\n')
		if err != nil {
			break
		}
		lineNum++
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		// Parse to get span metadata first.
		var sr SpanRecord
		if jErr := json.Unmarshal([]byte(line), &sr); jErr == nil {
			if sr.TraceID == "" {
				continue // skip non-span JSONL lines
			}
		}

		matchIdx := re.FindStringIndex(line)
		if matchIdx == nil {
			continue
		}

		totalHits++
		if len(matches) < maxMatches {
			// Capture context: up to 128 bytes around the match.
			start := matchIdx[0]
			end := matchIdx[1]
			pad := 128
			s := start - pad
			if s < 0 {
				s = 0
			}
			e := end + pad
			if e > len(line) {
				e = len(line)
			}
			ctxRaw := "[...]" + line[s:e] + "[...]"

			matches = append(matches, SearchMatch{
				SpanID:   sr.SpanID,
				TraceID:  sr.TraceID,
				Scope:    sr.Service,
				LineNum:  lineNum,
				MatchLen: matchIdx[1] - matchIdx[0],
				Raw:      ctxRaw,
			})
		}
	}

	return matches, totalHits, nil
}

// searchLinesFiltered is like searchLines but filters to a single traceID.
// It uses lazy parsing: first checks raw bytes for regex match, only parses
// JSON for matching lines, skipping Pydantic-parse cost for non-matches.
func searchLinesFiltered(filePath, traceID, pattern string, maxMatches int) ([]SearchMatch, int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid regex: %w", err)
	}

	ctx := bufio.NewReaderSize(f, 64*1024)
	var matches []SearchMatch
	totalHits := 0
	lineNum := 0

	for {
		line, err := ctx.ReadString('\n')
		if err != nil {
			break
		}
		lineNum++
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		// Quick regex check on raw bytes (lazy -- no JSON parse yet).
		matchIdx := re.FindStringIndex(line)
		if matchIdx == nil {
			continue
		}

		// Only parse JSON to extract span metadata if regex matched.
		var sr SpanRecord
		if jErr := json.Unmarshal([]byte(line), &sr); jErr == nil {
			if sr.TraceID != traceID {
				continue
			}
		} else {
			// Skip non-JSON lines.
			continue
		}

		totalHits++
		if len(matches) < maxMatches {
			start := matchIdx[0]
			end := matchIdx[1]
			pad := 128
			s := start - pad
			if s < 0 {
				s = 0
			}
			e := end + pad
			if e > len(line) {
				e = len(line)
			}
			ctxRaw := "[...]" + line[s:e] + "[...]"

			matches = append(matches, SearchMatch{
				SpanID:   sr.SpanID,
				TraceID:  sr.TraceID,
				Scope:    sr.Service,
				LineNum:  lineNum,
				MatchLen: matchIdx[1] - matchIdx[0],
				Raw:      ctxRaw,
			})
		}
	}

	return matches, totalHits, nil
}

// ---------- truncation ----------

// truncateAttributes returns a copy of span with string fields capped
// to maxChars. This is the core of the two-tier truncation: discovery
// uses 4 KB, surgical uses 16 KB.
func truncateAttributes(span SpanRecord, maxChars int) SpanRecord {
	out := span
	if len(out.Input) > maxChars {
		out.Input = out.Input[:maxChars]
	}
	if len(out.Output) > maxChars {
		out.Output = out.Output[:maxChars]
	}
	if out.Attributes != nil {
		tr := make(map[string]string, len(out.Attributes))
		for k, v := range out.Attributes {
			if len(v) > maxChars {
				v = v[:maxChars]
			}
			tr[k] = v
		}
		out.Attributes = tr
	}
	return out
}

// ---------- misc helpers ----------

// buildRecommendation produces an actionable string that guides the agent
// toward the correct follow-up tool.
func buildRecommendation(followUpTool string) string {
	return fmt.Sprintf(
		"Trace exceeds %.1f KB budget (%d spans). Use %s with regex='<pattern>' to narrow scope, then use view_spans on matched span_ids.",
		ViewTraceResponseBytesBudget/1024.0,
		ViewSpansMaxIDs,
		followUpTool,
	)
}

// topNValues returns the top N most frequent unique values.
func topNValues(values []string, n int) []string {
	if len(values) == 0 {
		return nil
	}
	// Deduplicate while preserving order.
	seen := make(map[string]bool)
	ordered := make([]string, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			ordered = append(ordered, v)
		}
	}
	if len(ordered) <= n {
		return ordered
	}
	return ordered[:n]
}

// countErrors counts spans with HasError set.
func countErrors(spans []SpanRecord) int {
	n := 0
	for _, s := range spans {
		if s.HasError {
			n++
		}
	}
	return n
}

// indexRowsFromSource builds TraceIndexRow entries by scanning source JSONL.
// This is shared between TraceStore construction and the sidecar index builder.
func (b *TraceIndexBuilder) indexRowsFromSource(sourcePath string) ([]TraceIndexRow, error) {
	f, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// First pass: read offsets.
	type lineInfo struct {
		offset int64
		length int64
	}
	var lines []lineInfo
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	offset := int64(0)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) > 0 {
			lines = append(lines, lineInfo{offset: offset, length: int64(len(line))})
		}
		offset += int64(len(line)) + 1 // +1 for the consumed newline
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}

	// Second pass: read each line, accumulate per-trace.
	byTrace := make(map[string]*rowAccumulator)
	for _, li := range lines {
		buf := make([]byte, li.length)
		if _, err := f.ReadAt(buf, li.offset); err != nil {
			continue // skip unreadable lines
		}
		var sr SpanRecord
		if jErr := json.Unmarshal(buf, &sr); jErr != nil {
			continue
		}
		if sr.TraceID == "" {
			continue
		}

		acc, ok := byTrace[sr.TraceID]
		if !ok {
			acc = &rowAccumulator{}
			byTrace[sr.TraceID] = acc
		}
		acc.addSpan(sr, li.offset, li.length)
	}

	// Flush accumulators.
	rows := make([]TraceIndexRow, 0, len(byTrace))
	for _, acc := range byTrace {
		rows = append(rows, acc.row())
	}
	return rows, nil
}

// rowAccumulator collects span data for one trace_id during index building.
type rowAccumulator struct {
	traceID           string
	offsets           []int64
	lengths           []int64
	startTime         *timeTime
	endTime           *timeTime
	hasErrors         bool
	serviceNames      map[string]struct{}
	modelNames        map[string]struct{}
	//lint:ignore U1000 reserved for future token analysis
	tokenNames        map[string]struct{}
	totalInputTokens  int
	totalOutputTokens int
	agentNames        map[string]struct{}
	agentIDs          map[string]struct{}
	missingParent     int
	missingAgentID    int
	otelErrors        int
	toolErrors        int
}

// timeTime is a json-serializable time.Time wrapper.
type timeTime struct{ time.Time }

func (t timeTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format("2006-01-02T15:04:05.000Z"))
}

func (acc *rowAccumulator) addSpan(sr SpanRecord, off, length int64) {
	if acc.traceID == "" {
		acc.traceID = sr.TraceID
	}
	acc.offsets = append(acc.offsets, off)
	acc.lengths = append(acc.lengths, length)

	if acc.startTime == nil || sr.StartTime.Before(acc.startTime.Time) {
		acc.startTime = &timeTime{sr.StartTime}
	}
	if acc.endTime == nil || sr.EndTime.After(acc.endTime.Time) {
		acc.endTime = &timeTime{sr.EndTime}
	}

	if sr.HasError {
		acc.hasErrors = true
		acc.otelErrors++
	}
	if sr.ToolError {
		acc.toolErrors++
	}
	if sr.ParentID == "" && sr.TraceID == sr.SpanID {
		// root span with no parent
	} else if sr.ParentID == "" {
		acc.missingParent++
	}
	if sr.AgentName == "" && sr.AgentID == "" {
		acc.missingAgentID++
	}

	if sr.Service != "" {
		if acc.serviceNames == nil {
			acc.serviceNames = make(map[string]struct{})
		}
		acc.serviceNames[sr.Service] = struct{}{}
	}
	if sr.Model != "" {
		if acc.modelNames == nil {
			acc.modelNames = make(map[string]struct{})
		}
		acc.modelNames[sr.Model] = struct{}{}
	}
	if acc.agentNames == nil {
		acc.agentNames = make(map[string]struct{})
	}
	if acc.agentIDs == nil {
		acc.agentIDs = make(map[string]struct{})
	}
	if sr.AgentName != "" {
		acc.agentNames[sr.AgentName] = struct{}{}
	}
	if sr.AgentID != "" {
		acc.agentIDs[sr.AgentID] = struct{}{}
	}

	acc.totalInputTokens += sr.InputTokens
	acc.totalOutputTokens += sr.OutputTokens
}

func (acc *rowAccumulator) row() TraceIndexRow {
	r := TraceIndexRow{
		TraceID:                 acc.traceID,
		ByteOffsets:             acc.offsets,
		ByteLengths:             acc.lengths,
		SpanCount:               len(acc.offsets),
		HasErrors:               acc.hasErrors,
		TotalInputTokens:        acc.totalInputTokens,
		TotalOutputTokens:       acc.totalOutputTokens,
		MissingParentCount:      acc.missingParent,
		MissingAgentIdentityCount: acc.missingAgentID,
		OtelErrorSpanCount:      acc.otelErrors,
		ToolErrorSpanCount:      acc.toolErrors,
	}
	if acc.startTime != nil {
		r.StartTime = acc.startTime.Time
	}
	if acc.endTime != nil {
		r.EndTime = acc.endTime.Time
	}
	if acc.serviceNames != nil {
		r.ServiceNames = mapKeys(acc.serviceNames)
	}
	if acc.modelNames != nil {
		r.ModelNames = mapKeys(acc.modelNames)
	}
	if acc.agentNames != nil {
		r.AgentNames = mapKeys(acc.agentNames)
	}
	if acc.agentIDs != nil {
		r.AgentIDs = mapKeys(acc.agentIDs)
	}
	return r
}

func mapKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
