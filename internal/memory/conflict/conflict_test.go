package conflict

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

// fakeLLM is a deterministic LLM for tests. It records the prompts it was
// given and replies with canned responses, one per call (the last response
// repeats once calls exceed the canned list).
type fakeLLM struct {
	Responses [][]byte
	Calls     []string
	Err       error
}

func (f *fakeLLM) Complete(_ context.Context, prompt string) ([]byte, error) {
	f.Calls = append(f.Calls, prompt)
	if f.Err != nil {
		return nil, f.Err
	}
	if len(f.Responses) == 0 {
		return nil, errors.New("fakeLLM: no canned response")
	}
	i := len(f.Calls) - 1
	if i >= len(f.Responses) {
		i = len(f.Responses) - 1
	}
	return f.Responses[i], nil
}

// fakeStore implements RecallStore.
type fakeStore struct {
	results   []RecallResult // returned by Search
	searchErr error
	vector    bool // EmbeddingsAvailable
	queries   []string
}

func (s *fakeStore) Search(_ context.Context, query string, _ int) ([]RecallResult, error) {
	s.queries = append(s.queries, query)
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	return s.results, nil
}

func (s *fakeStore) EmbeddingsAvailable() bool { return s.vector }

func candidates(n int) []Candidate {
	out := make([]Candidate, n)
	for i := range out {
		out[i] = Candidate{ID: fmt.Sprintf("cand-%d", i), Content: fmt.Sprintf("fact number %d", i)}
	}
	return out
}

func TestDetectBatchHappyMerge(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "deploy runs at noon UTC"},
		},
	}
	llm := &fakeLLM{Responses: [][]byte{[]byte(`[
		{"candidate":0,"action":"merge","target_ids":["mem-1"],"merged_content":"deploys run at noon UTC on weekdays","reason":"both true aspects of the deploy schedule"},
		{"candidate":1,"action":"store","target_ids":[],"merged_content":"","reason":"unrelated new fact"}
	]`)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), []Candidate{
		{ID: "cand-0", Content: "deploys happen at noon"},
		{ID: "cand-1", Content: "the office is in Lisbon"},
	})

	if len(got) != 2 {
		t.Fatalf("got %d decisions, want 2", len(got))
	}
	if got[0].CandidateIdx != 0 || got[0].Action != ActionMerge {
		t.Errorf("decision[0] = %+v, want merge for candidate 0", got[0])
	}
	if len(got[0].TargetIDs) != 1 || got[0].TargetIDs[0] != "mem-1" {
		t.Errorf("decision[0].TargetIDs = %v, want [mem-1]", got[0].TargetIDs)
	}
	if got[0].MergedContent == "" || got[0].Reason == "" {
		t.Errorf("decision[0] missing merged_content/reason: %+v", got[0])
	}
	if got[1].Action != ActionStore {
		t.Errorf("decision[1].Action = %q, want store", got[1].Action)
	}
	if len(llm.Calls) != 1 {
		t.Errorf("LLM called %d times, want 1", len(llm.Calls))
	}
}

func TestDetectBatchMalformedJSONFailsOpen(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "deploy runs at noon UTC"},
		},
	}
	llm := &fakeLLM{Responses: [][]byte{[]byte("I could not produce valid JSON, sorry.")}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(3))

	if len(got) != 3 {
		t.Fatalf("got %d decisions, want 3", len(got))
	}
	for i, dec := range got {
		if dec.Action != ActionStore {
			t.Errorf("decision[%d].Action = %q, want fail-open store", i, dec.Action)
		}
	}
}

func TestDetectBatchLLMErrorFailsOpen(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "deploy runs at noon UTC"},
		},
	}
	llm := &fakeLLM{Err: errors.New("provider down")}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(2))

	if len(got) != 2 {
		t.Fatalf("got %d decisions, want 2", len(got))
	}
	for i, dec := range got {
		if dec.Action != ActionStore {
			t.Errorf("decision[%d].Action = %q, want fail-open store", i, dec.Action)
		}
	}
}

func TestDetectBatchThinkWrappedJSONParses(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "backup job runs nightly"},
		},
	}
	body := `<think>The candidate overlaps with the backup schedule memory.
I will recommend a merge.</think>` + "```json\n" + `[
	{"candidate":0,"action":"merge","target_ids":["mem-1"],"merged_content":"backup job runs nightly at 02:00","reason":"same schedule fact"}
]` + "\n```"
	llm := &fakeLLM{Responses: [][]byte{[]byte(body)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(1))

	if len(got) != 1 {
		t.Fatalf("got %d decisions, want 1", len(got))
	}
	if got[0].Action != ActionMerge || len(got[0].TargetIDs) != 1 || got[0].TargetIDs[0] != "mem-1" {
		t.Errorf("decision = %+v, want merge onto mem-1 (think block/fences not stripped?)", got[0])
	}
}

func TestDetectBatchMissingEntriesFilledWithStore(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "deploy runs at noon UTC"},
		},
	}
	// Only candidate 1 is answered; candidates 0 and 2 must fall back to store.
	llm := &fakeLLM{Responses: [][]byte{[]byte(`[
		{"candidate":1,"action":"skip","target_ids":["mem-1"],"merged_content":"","reason":"exact duplicate"}
	]`)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(3))

	if len(got) != 3 {
		t.Fatalf("got %d decisions, want 3", len(got))
	}
	if got[1].Action != ActionSkip {
		t.Errorf("decision[1].Action = %q, want skip", got[1].Action)
	}
	for _, i := range []int{0, 2} {
		if got[i].Action != ActionStore {
			t.Errorf("decision[%d].Action = %q, want filled store", i, got[i].Action)
		}
	}
}

func TestDetectBatchUnknownActionFallsBackToStore(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "deploy runs at noon UTC"},
		},
	}
	llm := &fakeLLM{Responses: [][]byte{[]byte(`[
		{"candidate":0,"action":"obliterate","target_ids":["mem-1"],"merged_content":"","reason":"hallucinated verb"}
	]`)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(1))
	if len(got) != 1 || got[0].Action != ActionStore {
		t.Errorf("got %+v, want single store decision (unknown action rejected)", got)
	}
}

func TestDetectBatchSplitsAtMaxBatchSize(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-1", Content: "deploy runs at noon UTC"},
		},
	}
	llm := &fakeLLM{Responses: [][]byte{[]byte(`[]`)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t), MaxBatch: 20})

	got := d.DetectBatch(context.Background(), candidates(25))

	if len(llm.Calls) != 2 {
		t.Fatalf("LLM called %d times, want 2 (batches of 20 + 5)", len(llm.Calls))
	}
	if len(got) != 25 {
		t.Fatalf("got %d decisions, want 25", len(got))
	}
	for i, dec := range got {
		if dec.Action != ActionStore {
			t.Errorf("decision[%d].Action = %q, want store (empty batch answer fills store)", i, dec.Action)
		}
	}
	// Second batch prompt must contain the global numbering offset.
	if !strings.Contains(llm.Calls[1], "fact number 20") {
		t.Errorf("second batch prompt missing candidate 20 content; got: %.200s", llm.Calls[1])
	}
}

func TestDetectBatchEmptyRecallSkipsLLM(t *testing.T) {
	store := &fakeStore{vector: true, results: nil}
	llm := &fakeLLM{Responses: [][]byte{[]byte(`[]`)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: store, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(4))

	if len(llm.Calls) != 0 {
		t.Errorf("LLM called %d times on empty recall, want 0", len(llm.Calls))
	}
	if len(got) != 4 {
		t.Fatalf("got %d decisions, want 4", len(got))
	}
	for i, dec := range got {
		if dec.Action != ActionStore {
			t.Errorf("decision[%d].Action = %q, want store", i, dec.Action)
		}
	}
}

func TestDetectBatchNilStoreSkipsLLM(t *testing.T) {
	llm := &fakeLLM{Responses: [][]byte{[]byte(`[]`)}}
	d := NewDetector(DetectorConfig{LLM: llm, Store: nil, Logger: testLogger(t)})

	got := d.DetectBatch(context.Background(), candidates(2))
	if len(llm.Calls) != 0 {
		t.Errorf("LLM called %d times with nil store, want 0", len(llm.Calls))
	}
	if len(got) != 2 || got[0].Action != ActionStore || got[1].Action != ActionStore {
		t.Errorf("got %+v, want all store", got)
	}
}

func TestTopSimilarVectorPathUsesStoreOrder(t *testing.T) {
	store := &fakeStore{
		vector: true,
		results: []RecallResult{
			{MemoryID: "mem-a", Content: "deploy at noon"},
			{MemoryID: "mem-b", Content: "office in Lisbon"},
			{MemoryID: "mem-c", Content: "deploys happen at noon sharp"},
		},
	}
	c := Candidate{ID: "cand-0", Content: "deploys happen at noon"}

	got, err := topSimilar(context.Background(), store, c, 2)
	if err != nil {
		t.Fatalf("topSimilar: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d targets, want k=2", len(got))
	}
	if got[0].MemoryID != "mem-a" || got[1].MemoryID != "mem-b" {
		t.Errorf("vector path must preserve store ranking: got %v, %v", got[0].MemoryID, got[1].MemoryID)
	}
}

func TestTopSimilarTokenOverlapFallbackOrdering(t *testing.T) {
	// No embeddings: store returns a coarse pool and topSimilar must re-rank
	// by token-overlap Jaccard against the candidate content.
	store := &fakeStore{
		vector: false,
		results: []RecallResult{
			{MemoryID: "mem-weak", Content: "the office is located in Lisbon Portugal"},
			{MemoryID: "mem-strong", Content: "deploys happen at noon every weekday"},
			{MemoryID: "mem-mid", Content: "deploys happen at midnight"},
		},
	}
	c := Candidate{ID: "cand-0", Content: "deploys happen at noon"}

	got, err := topSimilar(context.Background(), store, c, 3)
	if err != nil {
		t.Fatalf("topSimilar: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d targets, want 3", len(got))
	}
	if got[0].MemoryID != "mem-strong" {
		t.Errorf("top result = %q, want mem-strong (highest token overlap)", got[0].MemoryID)
	}
	if got[1].MemoryID != "mem-mid" {
		t.Errorf("second result = %q, want mem-mid", got[1].MemoryID)
	}
	if got[2].MemoryID != "mem-weak" {
		t.Errorf("third result = %q, want mem-weak", got[2].MemoryID)
	}
	if got[0].Score <= got[1].Score || got[1].Score <= got[2].Score {
		t.Errorf("scores not strictly descending: %v %v %v", got[0].Score, got[1].Score, got[2].Score)
	}
}

func TestTopSimilarRecallErrorFailsOpen(t *testing.T) {
	store := &fakeStore{searchErr: errors.New("index offline")}
	got, err := topSimilar(context.Background(), store, Candidate{Content: "anything"}, 5)
	if err != nil {
		t.Errorf("topSimilar returned error on recall failure, want fail-open empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d targets on recall failure, want 0", len(got))
	}
}

func TestSanitizeStripsThinkBlocksAndFences(t *testing.T) {
	in := "<think>deep thoughts</think>\n```json\n[{\"candidate\":0}]\n```"
	got := sanitizeLLMOutput(in)
	if strings.Contains(got, "<think>") || strings.Contains(got, "```") {
		t.Errorf("sanitizeLLMOutput left artifacts: %q", got)
	}
	if !strings.Contains(got, "[{\"candidate\":0}]") {
		t.Errorf("sanitizeLLMOutput dropped the payload: %q", got)
	}
}
