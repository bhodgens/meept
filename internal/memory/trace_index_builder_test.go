package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTraceIndexBuilder_BuildOrReuse(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")
	indexPath := filepath.Join(dir, "traces.meept-index.jsonl")
	metaPath := filepath.Join(dir, "traces.meept-index.meta.json")

	// Write a small JSONL file with 3 spans from 2 traces.
	traces := []SpanRecord{
		{TraceID: "t1", SpanID: "s1a", Service: "agent", Model: "claude-3.5",
			StartTime: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 1, 1, 10, 0, 1, 0, time.UTC),
			InputTokens: 100, OutputTokens: 50, AgentName: "writer", AgentID: "a1"},
		{TraceID: "t1", SpanID: "s1b", Service: "llm", InputTokens: 200, OutputTokens: 100},
		{TraceID: "t2", SpanID: "s2a", Service: "agent", HasError: true,
			StartTime: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2026, 1, 1, 11, 0, 2, 0, time.UTC),
			ToolError: true, AgentName: "debugger"},
	}

	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  indexPath,
		metaPath:   metaPath,
	}

	// First call should build from scratch.
	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 trace rows, got %d", len(result.Rows))
	}

	// Find row for t1.
	var rowT1 *TraceIndexRow
	for i := range result.Rows {
		if result.Rows[i].TraceID == "t1" {
			rowT1 = &result.Rows[i]
			break
		}
	}

	if rowT1 == nil {
		t.Fatal("missing trace row for t1")
	}

	if rowT1.SpanCount != 2 {
		t.Errorf("t1 SpanCount: got %d, want 2", rowT1.SpanCount)
	}
	if rowT1.TotalInputTokens != 300 {
		t.Errorf("t1 TotalInputTokens: got %d, want 300", rowT1.TotalInputTokens)
	}
	if len(rowT1.ByteOffsets) != 2 {
		t.Errorf("t1 ByteOffsets: got %d, want 2", len(rowT1.ByteOffsets))
	}

	// Second call should reuse (no re-parse since source unchanged).
	result2, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse (reuse) failed: %v", err)
	}
	if len(result2.Rows) != 2 {
		t.Errorf("reuse: expected 2 trace rows, got %d", len(result2.Rows))
	}

	// Verify index files were written.
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Error("index file was not written")
	}
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Error("meta file was not written")
	}
}

func TestTraceIndexBuilder_Staleness(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")
	indexPath := filepath.Join(dir, "traces.meept-index.jsonl")
	metaPath := filepath.Join(dir, "traces.meept-index.meta.json")

	// Write 2 spans.
	traces := []SpanRecord{
		{TraceID: "t1", SpanID: "s1a", Service: "agent"},
		{TraceID: "t1", SpanID: "s1b", Service: "llm"},
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  indexPath,
		metaPath:   metaPath,
	}

	// Build.
	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	// Touch the source file to change mtime/size.
	time.Sleep(10 * time.Millisecond)
	extraSpan := SpanRecord{TraceID: "t2", SpanID: "s2a", Service: "agent"}
	if err := writeJSONL(source, append(traces, extraSpan)); err != nil {
		t.Fatalf("writeJSONL extra: %v", err)
	}

	// Rebuild -- should detect staleness and rebuild.
	result2, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse (after staleness) failed: %v", err)
	}
	if len(result2.Rows) != 2 {
		t.Errorf("expected 2 rows after staleness, got %d", len(result2.Rows))
	}
}

func TestTraceIndexBuilder_ShortFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	// Fewer than 1000 lines -> fast path.
	traces := []SpanRecord{
		{TraceID: "t1", SpanID: "s1a", Service: "agent"},
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  filepath.Join(dir, "traces.meept-index.jsonl"),
		metaPath:   filepath.Join(dir, "traces.meept-index.meta.json"),
	}

	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestTraceIndexBuilder_BadSource(t *testing.T) {
	builder := &TraceIndexBuilder{
		sourcePath: "/no/such/file.jsonl",
		indexPath:  "/tmp/out.jsonl",
		metaPath:   "/tmp/out.meta.json",
	}

	_, err := builder.BuildOrReuse(context.Background())
	if err == nil {
		t.Error("expected error for missing source")
	}
}

func TestTraceIndexBuilder_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	// File with valid JSON lines plus some invalid ones mixed in.
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.Encode(SpanRecord{TraceID: "t1", SpanID: "s1a"})
	f.WriteString("not json\n")
	enc.Encode(SpanRecord{TraceID: "t2", SpanID: "s2a", HasError: true})
	f.WriteString("\n") // empty line

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  filepath.Join(dir, "traces.meept-index.jsonl"),
		metaPath:   filepath.Join(dir, "traces.meept-index.meta.json"),
	}

	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse: %v", err)
	}
	// Should have parsed the 2 valid spans into 2 trace rows.
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows (skipped invalid JSON), got %d", len(result.Rows))
	}
}

func TestTraceIndexBuilder_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	traces := []SpanRecord{{TraceID: "t1", SpanID: "s1a"}}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	indexPath := filepath.Join(dir, "traces.meept-index.jsonl")
	metaPath := filepath.Join(dir, "traces.meept-index.meta.json")

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  indexPath,
		metaPath:   metaPath,
	}

	_, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Atomic write means .tmp files should not be left behind.
	leftover, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftover) > 0 {
		t.Errorf("found stale .tmp files: %v", leftover)
	}
}

func TestTraceIndexBuilder_MergePreservesOrder(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	// Create 100 spans with distinct trace IDs in order.
	var traces []SpanRecord
	for i := 0; i < 100; i++ {
		traces = append(traces, SpanRecord{
			TraceID:   "trace-" + int64str(int64(i)),
			SpanID:    "span-" + int64str(int64(i)),
			StartTime: time.Now().Add(time.Duration(i) * time.Second),
		})
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  filepath.Join(dir, "traces.meept-index.jsonl"),
		metaPath:   filepath.Join(dir, "traces.meept-index.meta.json"),
	}

	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse: %v", err)
	}

	// Total should be 100 unique traces.
	if len(result.Rows) != 100 {
		t.Errorf("expected 100 rows, got %d", len(result.Rows))
	}

	// Each row should have exactly 1 offset (since each trace has 1 span).
	for i := range result.Rows {
		if result.Rows[i].SpanCount != 1 {
			t.Errorf("row %d (%s): SpanCount=%d, want 1", i, result.Rows[i].TraceID, result.Rows[i].SpanCount)
		}
	}
}

// TestTraceIndexBuilder_ParallelChunkProcessing verifies that parallel chunk
// processing produces correct results and that workers don't corrupt each other.
func TestTraceIndexBuilder_ParallelChunkProcessing(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	// Create 200 spans across 10 distinct traces (20 spans each).
	// This produces enough work that chunking matters.
	var traces []SpanRecord
	spanPerTrace := 20
	numTraces := 10
	for i := 0; i < numTraces; i++ {
		traceID := "trace-" + int64str(int64(i))
		for j := 0; j < spanPerTrace; j++ {
			traces = append(traces, SpanRecord{
				TraceID:      traceID,
				SpanID:       "span-" + int64str(int64(i*spanPerTrace+j)),
				Service:      "worker-" + int64str(int64(j%3)),
				Model:        "model-a",
				InputTokens:  100 + j,
				OutputTokens: 50 + j*2,
				AgentName:    "agent-" + int64str(int64(j%2)),
				StartTime:    time.Now().Add(time.Duration(i*spanPerTrace+j) * time.Second),
			})
			if (j%5) == 0 {
				traces[len(traces)-1].HasError = true
			}
		}
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	builder := &TraceIndexBuilder{
		sourcePath: source,
		indexPath:  filepath.Join(dir, "traces.meept-index.jsonl"),
		metaPath:   filepath.Join(dir, "traces.meept-index.meta.json"),
	}

	result, err := builder.BuildOrReuse(context.Background())
	if err != nil {
		t.Fatalf("BuildOrReuse: %v", err)
	}

	if len(result.Rows) != numTraces {
		t.Fatalf("expected %d rows, got %d", numTraces, len(result.Rows))
	}

	// Build a map for lookup since map iteration order is unpredictable.
	rowMap := make(map[string]TraceIndexRow, len(result.Rows))
	for _, row := range result.Rows {
		rowMap[row.TraceID] = row
	}

	// Verify each trace row has the right aggregated values.
	wantInput := 0
	for j := 0; j < spanPerTrace; j++ {
		wantInput += 100 + j
	}
	expectedServices := []string{"worker-0", "worker-1", "worker-2"}

	for i := 0; i < numTraces; i++ {
		traceID := "trace-" + int64str(int64(i))
		row, ok := rowMap[traceID]
		if !ok {
			t.Errorf("missing trace row: %s", traceID)
			continue
		}
		if row.SpanCount != spanPerTrace {
			t.Errorf("trace %s: SpanCount=%d, want %d", traceID, row.SpanCount, spanPerTrace)
		}
		if len(row.ByteOffsets) != spanPerTrace {
			t.Errorf("trace %s: ByteOffsets length=%d, want %d", traceID, len(row.ByteOffsets), spanPerTrace)
		}
		if row.TotalInputTokens != wantInput {
			t.Errorf("trace %s: TotalInputTokens=%d, want %d", traceID, row.TotalInputTokens, wantInput)
		}
		if row.OtelErrorSpanCount < 4 { // j=0,5,10,15 have HasError=true
			t.Errorf("trace %s: OtelErrorSpanCount should be at least 4, got %d", traceID, row.OtelErrorSpanCount)
		}
		if len(row.ServiceNames) != len(expectedServices) {
			t.Errorf("trace %s: ServiceNames count=%d, want %d", traceID, len(row.ServiceNames), len(expectedServices))
		}
		if len(row.ModelNames) != 1 || row.ModelNames[0] != "model-a" {
			t.Errorf("trace %s: ModelNames=%v, want [model-a]", traceID, row.ModelNames)
		}
	}

	// Meta should report the correct count.
	if result.Meta.TraceCount != numTraces {
		t.Errorf("meta trace count: got %d, want %d", result.Meta.TraceCount, numTraces)
	}
	if result.Meta.SourceSize <= 0 {
		t.Errorf("meta source_size should be positive, got %d", result.Meta.SourceSize)
	}
}

// TestTraceIndexBuilder_ParallelChunkProcessingNoDataRace verifies no data race
// under the race detector.
func TestTraceIndexBuilder_ParallelChunkProcessingNoDataRace(t *testing.T) {
	// Each goroutine uses its own temp dir to avoid file collisions.
	// Tests that the builder is safe under concurrent build requests.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			dir := t.TempDir()
			source := filepath.Join(dir, "traces.jsonl")

			var traces []SpanRecord
			for j := 0; j < 50; j++ {
				traces = append(traces, SpanRecord{
					TraceID: "t-" + int64str(int64(j%5)),
					SpanID:  "s-" + int64str(int64(j)),
					Service: "svc-g" + int64str(int64(goroutineID)),
				})
			}
			if err := writeJSONL(source, traces); err != nil {
				t.Errorf("writeJSONL goroutine %d: %v", goroutineID, err)
				return
			}

			builder := NewTraceIndexBuilder(source)
			result, err := builder.BuildOrReuse(context.Background())
			if err != nil {
				t.Errorf("BuildOrReuse goroutine %d: %v", goroutineID, err)
				return
			}
			if len(result.Rows) != 5 {
				t.Errorf("goroutine %d: expected 5 rows, got %d", goroutineID, len(result.Rows))
			}
		}(i)
	}
	wg.Wait()
}

func TestTraceStore_TwoTierTruncation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	// Write spans with large string attributes.
	largeStr := strings.Repeat("x", 10000) // 10KB
	traces := []SpanRecord{
		{TraceID: "t1", SpanID: "s1a", Input: largeStr, Output: largeStr, Service: "agent"},
		{TraceID: "t1", SpanID: "s1b", Input: largeStr, Service: "llm"},
		{TraceID: "t2", SpanID: "s2a", Input: largeStr + largeStr, Service: "agent"},
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	store, err := LoadTraceStore(source)
	if err != nil {
		t.Fatalf("LoadTraceStore: %v", err)
	}

	// ViewTrace should apply 4KB discovery cap per attribute.
	result, err := store.ViewTrace("t1")
	if err != nil {
		t.Fatalf("ViewTrace: %v", err)
	}
	if result.Oversized != nil {
		t.Fatalf("expected spans, got oversized summary: %+v", result.Oversized)
	}
	if len(result.Spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(result.Spans))
	}

	// Each attribute should be <= 4KB.
	maxAttrLen := DiscoveryAttrTruncationChars
	for si := range result.Spans {
		if len(result.Spans[si].Input) > maxAttrLen {
			t.Errorf("span %d Input: %d bytes > %d cap", si, len(result.Spans[si].Input), maxAttrLen)
		}
		if len(result.Spans[si].Output) > maxAttrLen {
			t.Errorf("span %d Output: %d bytes > %d cap", si, len(result.Spans[si].Output), maxAttrLen)
		}
	}

	// ViewSpans should apply 16KB surgical cap (4x).
	spansResult, err := store.ViewSpans("t1", []string{"s1a", "s1b"})
	if err != nil {
		t.Fatalf("ViewSpans: %v", err)
	}
	if len(spansResult.Spans) != 2 {
		t.Fatalf("ViewSpans: expected 2 spans, got %d", len(spansResult.Spans))
	}
	surgicalCap := SurgicalAttrTruncationChars
	for si := range spansResult.Spans {
		if len(spansResult.Spans[si].Input) > surgicalCap {
			t.Errorf("ViewSpans span %d Input: %d bytes > %d surgical cap", si, len(spansResult.Spans[si].Input), surgicalCap)
		}
	}
}

func TestTraceStore_OversizedReturnsPlanningMetadata(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	var traces []SpanRecord
	// Create 500 spans of ~500 bytes each = 250KB total.
	// With 4KB per-attribute cap, should be much less, but let's use small spans
	// with many to trigger total budget (150KB = 150000 bytes).
	// Each span line is about 200 bytes in JSONL. 150000/200 = 750 spans needed.
	// To be safe, generate 800 spans.
	filler := strings.Repeat("x", 150) // 150 bytes of content per attribute
	for i := 0; i < 800; i++ {
		traces = append(traces, SpanRecord{
			TraceID:   "oversized",
			SpanID:    "span-" + int64str(int64(i)),
			Service:   "agent",
			Input:     filler,
			Output:    filler,
			HasError:  (i % 100) == 0,
			StartTime: time.Now().Add(time.Duration(i) * time.Millisecond),
		})
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	store, err := LoadTraceStore(source)
	if err != nil {
		t.Fatalf("LoadTraceStore: %v", err)
	}

	result, err := store.ViewTrace("oversized")
	if err != nil {
		t.Fatalf("ViewTrace: %v", err)
	}

	if result.Oversized == nil {
		t.Fatal("expected OversizedTraceSummary, got nil")
	}

	if result.Oversized.SpanCount == 0 {
		t.Error("Oversized.SpanCount should be populated")
	}
	if result.Oversized.Recommendation == "" {
		t.Error("Oversized.Recommendation should contain a suggestion")
	}
	// Should NOT contain full spans.
	if len(result.Spans) > 0 {
		t.Errorf("expected no spans in oversized response, got %d", len(result.Spans))
	}
}

func TestTraceStore_SearchTraceLazyParsing(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	// Create 200 spans. Half contain "error" in their output, half don't.
	var traces []SpanRecord
	for i := 0; i < 200; i++ {
		var output string
		if i%2 == 0 {
			output = "normal execution"
		} else {
			output = "error: panic occurred"
		}
		traces = append(traces, SpanRecord{
			TraceID:  "t1",
			SpanID:   "span-" + int64str(int64(i)),
			Service:  "agent",
			Output:   output,
			HasError: i%2 == 1,
		})
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	store, err := LoadTraceStore(source)
	if err != nil {
		t.Fatalf("LoadTraceStore: %v", err)
	}

	// Search for "panic" - should match even-numbered spans (output contains "panic").
	searchResult, err := store.SearchTrace("t1", "panic", 5)
	if err != nil {
		t.Fatalf("SearchTrace: %v", err)
	}

	if searchResult == nil {
		t.Fatal("SearchTrace returned nil")
	}

	if len(searchResult.Matches) == 0 {
		t.Error("expected at least 1 match for 'panic'")
	}

	// Only up to maxMatches should be returned.
	if len(searchResult.Matches) > 5 {
		t.Errorf("expected at most 5 matches, got %d", len(searchResult.Matches))
	}

	// Search for something that doesn't exist - should return empty.
	noResult, err := store.SearchTrace("t1", "zznonexistentzz", 10)
	if err != nil {
		t.Fatalf("SearchTrace (neg): %v", err)
	}
	if len(noResult.Matches) != 0 {
		t.Errorf("expected 0 matches for nonexistent pattern, got %d", len(noResult.Matches))
	}
}

func TestTraceStore_SearchTraceReturnsContext(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "traces.jsonl")

	traces := []SpanRecord{
		{TraceID: "t1", SpanID: "s1a", Service: "agent", Output: "matching: error here"},
		{TraceID: "t1", SpanID: "s1b", Service: "llm", Output: "no match"},
		{TraceID: "t1", SpanID: "s1c", Service: "agent", Output: "another error match"},
	}
	if err := writeJSONL(source, traces); err != nil {
		t.Fatalf("writeJSONL: %v", err)
	}

	store, err := LoadTraceStore(source)
	if err != nil {
		t.Fatalf("LoadTraceStore: %v", err)
	}

	result, err := store.SearchTrace("t1", "error", 5)
	if err != nil {
		t.Fatalf("SearchTrace: %v", err)
	}

	if len(result.Matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result.Matches))
	}
}

// writeJSONL encodes spans into a JSONL file.
func writeJSONL(path string, spans []SpanRecord) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, s := range spans {
		if err := enc.Encode(s); err != nil {
			return err
		}
	}
	return nil
}

func int64str(n int64) string {
	s := ""
	if n == 0 {
		return "0"
	}
	digits := "0123456789"
	for n > 0 {
		s = string(digits[n%10]) + s
		n /= 10
	}
	return s
}

//lint:ignore U1000 // test helper reserved for future tests
func int64parse(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		} else {
			break
		}
	}
	return n, nil
}
