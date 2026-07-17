package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- helper ----------

// writeTestJSONL creates a temporary JSONL file with the given spans and
// returns its path.
func writeTestJSONL(t *testing.T, spans ...SpanRecord) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, s := range spans {
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---------- NewTraceStore tests ----------

func TestTraceStore_Constructor_Basic(t *testing.T) {
	spans := makeJSONLSpans(10, "trace-1")
	path := writeTestJSONL(t, spans...)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatalf("NewTraceStore: %v", err)
	}

	ids := store.GetTraceIDs()
	if len(ids) != 1 || ids[0] != "trace-1" {
		t.Errorf("unexpected trace IDs: %v", ids)
	}

	row, ok := store.GetTraceIndexRow("trace-1")
	if !ok {
		t.Fatal("expected trace-1 row, got false")
	}
	if row.SpanCount != 10 {
		t.Errorf("SpanCount: got %d, want 10", row.SpanCount)
	}
}

func TestTraceStore_Constructor_MultipleTraces(t *testing.T) {
	var spans []SpanRecord
	for tr := 0; tr < 3; tr++ {
		for sp := 0; sp < 5; sp++ {
			spans = append(spans, SpanRecord{
				TraceID:   fmt.Sprintf("trace-%d", tr),
				SpanID:    fmt.Sprintf("trace-%d-span-%d", tr, sp),
				StartTime: time.Now(),
				EndTime:   time.Now().Add(time.Second),
				Service:   fmt.Sprintf("svc-%d", tr),
			})
		}
	}
	path := writeTestJSONL(t, spans...)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatalf("NewTraceStore: %v", err)
	}

	ids := store.GetTraceIDs()
	if len(ids) != 3 {
		t.Fatalf("expected 3 trace IDs, got %d", len(ids))
	}
}

func TestTraceStore_Constructor_TraceNotFound(t *testing.T) {
	path := writeTestJSONL(t, SpanRecord{TraceID: "trace-1"})
	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ViewTrace("unknown-trace")
	if err == nil {
		t.Error("expected error for unknown trace")
	}
	if !strings.Contains(err.Error(), "unknown trace") {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---------- truncation tests (renamed to avoid conflict with uncommitted Phase 1 test) ----------

func TestTraceStoreAttrTruncate_CapsInput(t *testing.T) {
	largePayload := strings.Repeat("x", DiscoveryAttrTruncationChars+100)
	span := SpanRecord{Input: largePayload, Output: "ok", Attributes: map[string]string{"key": largePayload}}
	truncated := truncateAttributes(span, DiscoveryAttrTruncationChars)

	if len(truncated.Input) > DiscoveryAttrTruncationChars {
		t.Errorf("Input truncated: got %d, want <= %d", len(truncated.Input), DiscoveryAttrTruncationChars)
	}
	if len(truncated.Output) != 2 {
		t.Errorf("Output unchanged: got %d", len(truncated.Output))
	}
	if len(truncated.Attributes["key"]) > DiscoveryAttrTruncationChars {
		t.Errorf("Attribute truncated: got %d, want <= %d", len(truncated.Attributes["key"]), DiscoveryAttrTruncationChars)
	}
}

func TestTraceStoreAttrTruncate_SurgicalCap(t *testing.T) {
	payload := strings.Repeat("x", SurgicalAttrTruncationChars+500)
	span := SpanRecord{Input: payload}

	truncated4k := truncateAttributes(span, DiscoveryAttrTruncationChars)
	if len(truncated4k.Input) != DiscoveryAttrTruncationChars {
		t.Errorf("Discovery cap: got %d, want %d", len(truncated4k.Input), DiscoveryAttrTruncationChars)
	}

	// Surgical cap SHOULD truncate payloads larger than 16KB.
	truncated16k := truncateAttributes(span, SurgicalAttrTruncationChars)
	if len(truncated16k.Input) != SurgicalAttrTruncationChars {
		t.Errorf("Surgical cap: got %d, want %d", len(truncated16k.Input), SurgicalAttrTruncationChars)
	}

	// Payload smaller than surgical cap should NOT be truncated.
	smallPayload := strings.Repeat("y", SurgicalAttrTruncationChars-100)
	smallSpan := SpanRecord{Input: smallPayload}
	truncatedSmall := truncateAttributes(smallSpan, SurgicalAttrTruncationChars)
	if truncatedSmall.Input != smallPayload {
		t.Error("Surgical cap should not have truncated this smaller payload")
	}
}

func TestTraceStore_TwoTierTruncation_Phase3(t *testing.T) {
	// Create a span with > 4KB attributes but < 16KB overall.
	payload := strings.Repeat("x", 10240) // 10KB
	span := SpanRecord{
		TraceID:   "t1",
		SpanID:    "s1",
		Input:     payload,
		Output:    payload,
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Second),
	}
	path := writeTestJSONL(t, span)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	// ViewTrace uses discovery cap (4KB).
	result, err := store.ViewTrace("t1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Oversized != nil {
		t.Fatal("expected spans, got oversized")
	}
	if len(result.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(result.Spans))
	}
	attrSize := len(result.Spans[0].Input) + len(result.Spans[0].Output)
	if attrSize > 2*DiscoveryAttrTruncationChars {
		t.Errorf("discovery cap violated: attribute total %d, want <= %d",
			attrSize, 2*DiscoveryAttrTruncationChars)
	}

	// ViewSpans uses surgical cap (16KB).
	spansResult, err := store.ViewSpans("t1", []string{"s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(spansResult.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spansResult.Spans))
	}
	attrSize2 := len(spansResult.Spans[0].Input) + len(spansResult.Spans[0].Output)
	if attrSize2 != 2*10240 {
		t.Errorf("surgical cap: attribute total %d, want 20480 (no truncation)", attrSize2)
	}
}

// ---------- oversized tests ----------

func TestTraceStore_OversizedReturnsPlanningMetadata_Phase3(t *testing.T) {
	// Create a trace that exceeds 150KB budget.
	// Each span with 10KB payload takes about 10KB, so 20 spans = ~200KB.
	var spans []SpanRecord
	for i := 0; i < 20; i++ {
		spans = append(spans, SpanRecord{
			TraceID:   "big-trace",
			SpanID:    fmt.Sprintf("s%d", i),
			Input:     strings.Repeat("x", 10240),
			Output:    strings.Repeat("y", 10240),
			Service:   "svc-test",
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Duration(i+1) * time.Second),
		})
	}
	path := writeTestJSONL(t, spans...)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ViewTrace("big-trace")
	if err != nil {
		t.Fatal(err)
	}

	if result.Oversized == nil {
		t.Fatal("expected OversizedTraceSummary, got spans")
	}

	oversized := result.Oversized
	if oversized.SpanCount != 20 {
		t.Errorf("SpanCount: got %d, want 20", oversized.SpanCount)
	}
	if oversized.SpanResponseBytesMax == 0 {
		t.Error("SpanResponseBytesMax should be non-zero")
	}
	if oversized.ErrorSpanCount < 0 {
		t.Error("ErrorSpanCount should be >= 0")
	}
	if len(oversized.Recommendation) == 0 {
		t.Error("Recommendation should not be empty")
	}

	// Verify recommendation mentions the follow-up tool.
	rec := oversized.Recommendation
	if !strings.Contains(rec, "search_trace") {
		t.Errorf("Recommendation should mention 'search_trace': %s", rec)
	}
	if !strings.Contains(rec, "regex") {
		t.Errorf("Recommendation should mention 'regex': %s", rec)
	}
	if !strings.Contains(rec, "view_spans") {
		t.Errorf("Recommendation should mention 'view_spans': %s", rec)
	}
}

func TestTraceStore_RecommendationGuidesFollowUp_Phase3(t *testing.T) {
	// Test buildRecommendation with different follow-up tools.
	// Note: the budget message says "146.5 KB" (150000 / 1024 = 146.48...).
	rec1 := buildRecommendation("search_trace")
	if !strings.Contains(rec1, "search_trace") {
		t.Errorf("recommendation should mention search_trace: %s", rec1)
	}
	if !strings.Contains(rec1, "regex") {
		t.Errorf("recommendation should guide toward regex: %s", rec1)
	}
	if !strings.Contains(rec1, "view_spans") {
		t.Errorf("recommendation should cross-reference view_spans: %s", rec1)
	}
	if !strings.Contains(rec1, "span_ids") {
		t.Errorf("recommendation should mention span_ids: %s", rec1)
	}

	rec2 := buildRecommendation("view_spans")
	if !strings.Contains(rec2, "view_spans") {
		t.Errorf("recommendation should mention view_spans: %s", rec2)
	}
	if !strings.Contains(rec2, "span_ids") {
		t.Errorf("recommendation should mention span_ids: %s", rec2)
	}
}

func TestTraceStore_ViewSpansOversized_Phase3(t *testing.T) {
	var spans []SpanRecord
	for i := 0; i < 20; i++ {
		spans = append(spans, SpanRecord{
			TraceID:   "big-t",
			SpanID:    fmt.Sprintf("sp%d", i),
			Input:     strings.Repeat("x", 10240),
			Output:    strings.Repeat("y", 10240),
			StartTime: time.Now(),
			EndTime:   time.Now(),
		})
	}
	path := writeTestJSONL(t, spans...)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Request a subset that's still oversize.
	var ids []string
	for i := 0; i < 10; i++ {
		ids = append(ids, fmt.Sprintf("sp%d", i))
	}
	result, err := store.ViewSpans("big-t", ids)
	if err != nil {
		t.Fatal(err)
	}

	if result.Oversized == nil {
		t.Fatal("expected OversizedTraceSummary from ViewSpans")
	}
	if !strings.Contains(result.Oversized.Recommendation, "view_spans") {
		t.Errorf("Oversized recommendation should guide to view_spans: %s",
			result.Oversized.Recommendation)
	}
}

func TestViewSpans_TooManyIDs(t *testing.T) {
	path := writeTestJSONL(t, SpanRecord{TraceID: "t1", SpanID: "s1"})
	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for i := 0; i < 250; i++ {
		ids = append(ids, fmt.Sprintf("s%d", i))
	}
	_, err = store.ViewSpans("t1", ids)
	if err == nil {
		t.Fatal("expected error for >200 span IDs")
	}
	if !strings.Contains(err.Error(), "too many") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestViewSpans_EmptyIDs(t *testing.T) {
	path := writeTestJSONL(t, SpanRecord{TraceID: "t1"})
	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ViewSpans("t1", []string{})
	if err == nil {
		t.Fatal("expected error for empty span IDs")
	}
}

// ---------- search tests ----------

func TestSearchTrace_Matches(t *testing.T) {
	var spans []SpanRecord
	spans = append(spans, SpanRecord{
		TraceID:   "t1",
		SpanID:    "s-ok",
		Service:   "auth-service",
		Input:     "authenticate user",
		StartTime: time.Now(),
		EndTime:   time.Now(),
	})
	spans = append(spans, SpanRecord{
		TraceID:   "t1",
		SpanID:    "s-other",
		Service:   "billing",
		Input:     "charge card",
		StartTime: time.Now(),
		EndTime:   time.Now(),
	})
	path := writeTestJSONL(t, spans...)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.SearchTrace("t1", "auth", 10)
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalHits != 1 {
		t.Errorf("TotalHits: got %d, want 1", result.TotalHits)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("Matches: got %d, want 1", len(result.Matches))
	}
	if result.Matches[0].SpanID != "s-ok" {
		t.Errorf("SpanID: got %s, want s-ok", result.Matches[0].SpanID)
	}
}

func TestSearchTrace_MaxMatches(t *testing.T) {
	var spans []SpanRecord
	for i := 0; i < 10; i++ {
		spans = append(spans, SpanRecord{
			TraceID:   "t1",
			SpanID:    fmt.Sprintf("s%d", i),
			Service:   "auth-service",
			StartTime: time.Now(),
			EndTime:   time.Now(),
		})
	}
	path := writeTestJSONL(t, spans...)

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.SearchTrace("t1", "auth", 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Matches) > 3 {
		t.Errorf("Matches capped at 3: got %d", len(result.Matches))
	}
}

func TestSearchTrace_InvalidRegex(t *testing.T) {
	path := writeTestJSONL(t, SpanRecord{TraceID: "t1"})
	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SearchTrace("t1", "[invalid", 10)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

// ---------- utility function tests ----------

func TestBuildRecommendation(t *testing.T) {
	// 150000 / 1024 = ~146.5 KB.
	rec := buildRecommendation("search_trace")
	if !strings.Contains(rec, "146.5 KB") {
		t.Errorf("recommendation should mention budget: %s", rec)
	}
	if !strings.Contains(rec, "200") {
		t.Errorf("recommendation should mention span limit: %s", rec)
	}
	if !strings.Contains(rec, "search_trace") {
		t.Errorf("recommendation should mention tool: %s", rec)
	}
}

func TestTopNValues(t *testing.T) {
	// Fewer than n -- return all.
	result := topNValues([]string{"a", "b", "c"}, 10)
	if len(result) != 3 {
		t.Errorf("topNValues(3): got %d, want 3", len(result))
	}

	// More than n -- cap.
	result = topNValues([]string{"a", "b", "c", "d", "e"}, 2)
	if len(result) != 2 {
		t.Errorf("topNValues(5->2): got %d, want 2", len(result))
	}

	// Nil input.
	result = topNValues(nil, 5)
	if result != nil {
		t.Errorf("nil input should return nil")
	}

	// Empty input.
	result = topNValues([]string{}, 5)
	if len(result) != 0 {
		t.Errorf("empty input should return empty slice")
	}
}

func TestCountErrors(t *testing.T) {
	spans := []SpanRecord{
		{HasError: true},
		{HasError: false},
		{HasError: true},
	}
	n := countErrors(spans)
	if n != 2 {
		t.Errorf("countErrors: got %d, want 2", n)
	}

	empty := countErrors(nil)
	if empty != 0 {
		t.Errorf("countErrors(nil): got %d, want 0", empty)
	}
}

// ---------- SearchSpans convenience wrapper ----------

func TestSearchSpans_Wrapper(t *testing.T) {
	path := writeTestJSONL(t, SpanRecord{TraceID: "t1", Service: "auth-service"})
	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.SearchSpans("t1", "auth", "charge", 10)
	if err != nil {
		t.Fatal(err)
	}

	// Should match via the "auth" pattern (first part, joined with |).
	if result.TotalHits == 0 {
		t.Error("search_spans should find at least 1 match")
	}
}

// ---------- end-to-end ----------

func TestTraceStore_E2E_SmallTrace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")

	var spans []SpanRecord
	for i := 0; i < 5; i++ {
		spans = append(spans, SpanRecord{
			TraceID:   "e2e",
			SpanID:    fmt.Sprintf("e2e-%d", i),
			Service:   "web-server",
			Input:     fmt.Sprintf("request %d", i),
			Output:    fmt.Sprintf("response %d", i),
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Duration(i) * time.Millisecond),
		})
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for _, s := range spans {
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	store, err := NewTraceStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	ids := store.GetTraceIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 trace ID, got %d", len(ids))
	}

	result, err := store.ViewTrace("e2e")
	if err != nil {
		t.Fatal(err)
	}
	if result.Oversized != nil {
		t.Fatal("small trace should not be oversized")
	}
	if len(result.Spans) != 5 {
		t.Fatalf("expected 5 spans, got %d", len(result.Spans))
	}

	searchResult, err := store.SearchTrace("e2e", "request 2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if searchResult.TotalHits == 0 {
		t.Error("search should find matching span")
	}
}

// ---------- format helper ----------

// makeJSONLSpans creates trace spans for testing.
func makeJSONLSpans(count int, track string) []SpanRecord {
	var spans []SpanRecord
	for i := 0; i < count; i++ {
		spans = append(spans, SpanRecord{
			TraceID:   track,
			SpanID:    fmt.Sprintf("span-%d", i),
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Service:   "test-svc",
		})
	}
	return spans
}
