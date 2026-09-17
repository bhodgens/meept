package main

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// failingServer returns an OpenAI-compatible chat endpoint that always fails
// with the given status (or, when status is 0, a malformed-JSON body).
func failingServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if _, err := w.Write([]byte(`{"choices":[{"message":{"content":"not json at all"}}]}`)); err != nil {
			t.Errorf("write test response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(endpoint string) *chatClient {
	return newChatClient(endpoint+"/v1", "test-model", 5*time.Second)
}

// TestAmbientFailedCallsCountAsMisses: a segment whose extraction call fails
// (HTTP 500) must contribute its gold facts to false_negatives, not vanish
// from the recall denominator.
func TestAmbientFailedCallsCountAsMisses(t *testing.T) {
	srv := failingServer(t, http.StatusInternalServerError)
	c := &corpus{Segments: []segment{{
		ID:       "seg-fail",
		Messages: []string{"we use Go and target 1200 rps"},
		Gold:     goldSet{Claims: []string{"the team uses Go"}},
	}}}
	m := runAmbientEval(newTestClient(srv.URL), c, 0.6)
	if m.TransportErrors != 1 {
		t.Errorf("TransportErrors = %d, want 1", m.TransportErrors)
	}
	if m.FalseNegatives != 1 {
		t.Errorf("FalseNegatives = %d, want 1 (failed call must count as miss)", m.FalseNegatives)
	}
	if m.TruePositives != 0 || m.Recall != 0 {
		t.Errorf("TP=%d Recall=%v, want 0/0 (recall must include the failed segment)", m.TruePositives, m.Recall)
	}
}

// TestAmbientParseFailureCountsAsMisses: a 200 response with unparseable
// output must also count the segment's gold as misses.
func TestAmbientParseFailureCountsAsMisses(t *testing.T) {
	srv := failingServer(t, 0) // malformed JSON body
	c := &corpus{Segments: []segment{{
		ID:       "seg-parse",
		Messages: []string{"we use Go and target 1200 rps"},
		Gold:     goldSet{Claims: []string{"the team uses Go"}},
	}}}
	m := runAmbientEval(newTestClient(srv.URL), c, 0.6)
	if m.TransportErrors != 0 || m.JSONParseOK != 0 {
		t.Errorf("TransportErrors=%d JSONParseOK=%d, want 0/0", m.TransportErrors, m.JSONParseOK)
	}
	if m.FalseNegatives != 1 {
		t.Errorf("FalseNegatives = %d, want 1 (parse failure must count as miss)", m.FalseNegatives)
	}
	if m.Recall != 0 {
		t.Errorf("Recall = %v, want 0", m.Recall)
	}
}

// TestDistillFailedCallsCountAsMisses: the distillation path has the same
// denominator bug — a failed call must count the lesson as a false negative.
func TestDistillFailedCallsCountAsMisses(t *testing.T) {
	srv := failingServer(t, http.StatusInternalServerError)
	c := &corpus{Segments: []segment{{
		ID:   "seg-lesson",
		Gold: goldSet{Lessons: []string{"always set explicit timeouts on network calls"}},
	}}}
	m := runDistillEval(newTestClient(srv.URL), c, 0.6)
	if m.TransportErrors != 1 {
		t.Errorf("TransportErrors = %d, want 1", m.TransportErrors)
	}
	if m.FalseNegatives != 1 {
		t.Errorf("FalseNegatives = %d, want 1 (failed distill call must count as miss)", m.FalseNegatives)
	}
	if m.Recall != 0 {
		t.Errorf("Recall = %v, want 0", m.Recall)
	}
}

// TestDistillParseFailureCountsAsMisses: same for unparseable distill output.
func TestDistillParseFailureCountsAsMisses(t *testing.T) {
	srv := failingServer(t, 0)
	c := &corpus{Segments: []segment{{
		ID:   "seg-lesson-parse",
		Gold: goldSet{Lessons: []string{"always set explicit timeouts on network calls"}},
	}}}
	m := runDistillEval(newTestClient(srv.URL), c, 0.6)
	if m.TransportErrors != 0 || m.JSONParseOK != 0 {
		t.Errorf("TransportErrors=%d JSONParseOK=%d, want 0/0", m.TransportErrors, m.JSONParseOK)
	}
	if m.FalseNegatives != 1 {
		t.Errorf("FalseNegatives = %d, want 1 (parse failure must count as miss)", m.FalseNegatives)
	}
	if m.Recall != 0 {
		t.Errorf("Recall = %v, want 0", m.Recall)
	}
}

// TestParseJudgeYesNo: the judge's yes/no answer tolerates case, whitespace,
// markdown fences and trailing punctuation; anything else is false.
func TestParseJudgeYesNo(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"plain yes", "yes", true},
		{"uppercase", "YES", true},
		{"mixed case", "Yes", true},
		{"single letter", "y", true},
		{"uppercase letter", "Y", true},
		{"with period", "yes.", true},
		{"surrounding whitespace", "  yes  ", true},
		{"fence wrapped", "```yes```", true},
		{"fence wrapped json block", "```json\nyes\n```", true},
		{"plain no", "no", false},
		{"uppercase no", "NO", false},
		{"n", "n", false},
		{"junk", "banana", false},
		{"empty", "", false},
		{"sentence", "the candidate matches the gold assertion", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseJudgeYesNo(tt.in); got != tt.want {
				t.Errorf("parseJudgeYesNo(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// judgeYesServer returns an endpoint that answers "yes" (or "no") for every
// judge call and counts them.
func judgeAnswerServer(t *testing.T, answer string, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		resp := fmt.Sprintf(`{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, answer)
		if _, err := w.Write([]byte(resp)); err != nil {
			t.Errorf("write judge response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestJudgeCacheDedup: the same (candidate, gold) pair is judged exactly once
// no matter how many times it is asked.
func TestJudgeCacheDedup(t *testing.T) {
	calls := 0
	srv := judgeAnswerServer(t, "yes", &calls)
	jp := newJudgePairer(newTestClient(srv.URL))

	const cand = "the team ships on fridays"
	const gold = "the team deploys on fridays"
	for i := 0; i < 5; i++ {
		if !jp.judgePair(cand, gold) {
			t.Fatalf("judgePair said no on iteration %d, want yes", i)
		}
	}
	if calls != 1 {
		t.Errorf("endpoint got %d judge calls, want 1 (cache must dedup repeats)", calls)
	}
	if jp.cacheHits != 4 {
		t.Errorf("cacheHits = %d, want 4", jp.cacheHits)
	}
	// Same text modulo whitespace/case hits the same cache entry.
	if !jp.judgePair("The Team Ships On Fridays", gold) || calls != 1 {
		t.Errorf("normalized duplicate missed the cache (calls=%d)", calls)
	}
}

// TestJudgeCandidateStopsAtFirstYes: the candidate-level judge asks pairs
// until one gold matches.
func TestJudgeCandidateStopsAtFirstYes(t *testing.T) {
	calls := 0
	srv := judgeAnswerServer(t, "yes", &calls)
	jp := newJudgePairer(newTestClient(srv.URL))

	gold := []string{"a", "b", "c"}
	if !jp.judgeCandidate("x", gold) {
		t.Fatal("judgeCandidate = false, want true")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (first matching gold must stop the scan)", calls)
	}
}

// TestConfidenceSweepSynthetic: threshold sweep math on synthetic
// (confidence, matched) data with known answers.
func TestConfidenceSweepSynthetic(t *testing.T) {
	// 10 candidates total; matched flags per confidence bucket:
	// 0.95x2 matched, 0.85x2 (1 matched), 0.75x2 (0 matched),
	// 0.65x2 (1 matched), 0.35x2 (0 matched).
	pts := []ConfidencePoint{
		{Confidence: 0.95, MatchedJudge: true},
		{Confidence: 0.95, MatchedJudge: true},
		{Confidence: 0.85, MatchedJudge: true},
		{Confidence: 0.85, MatchedJudge: false},
		{Confidence: 0.75, MatchedJudge: false},
		{Confidence: 0.75, MatchedJudge: false},
		{Confidence: 0.65, MatchedJudge: true},
		{Confidence: 0.65, MatchedJudge: false},
		{Confidence: 0.35, MatchedJudge: false},
		{Confidence: 0.35, MatchedJudge: false},
	}
	rows := computeConfidenceSweep(pts, len(pts))
	if len(rows) != 7 {
		t.Fatalf("got %d sweep rows, want 7 (thresholds 0.3..0.9)", len(rows))
	}
	tests := []struct {
		threshold float64
		kept      int
		tp        int
		precAtT   float64
		keptFrac  float64
	}{
		{0.3, 10, 4, 0.4, 1.0},
		{0.4, 8, 4, 0.5, 0.8},
		{0.5, 8, 4, 0.5, 0.8},
		{0.6, 8, 4, 0.5, 0.8},
		{0.7, 6, 3, 0.5, 0.6},
		{0.8, 4, 3, 0.75, 0.4},
		{0.9, 2, 2, 1.0, 0.2},
	}
	for i, tt := range tests {
		r := rows[i]
		if r.Threshold != tt.threshold {
			t.Errorf("row %d threshold = %.2f, want %.2f", i, r.Threshold, tt.threshold)
		}
		if r.CandidatesGT != tt.kept || r.TruePositives != tt.tp {
			t.Errorf("row %.2f kept=%d tp=%d, want kept=%d tp=%d",
				r.Threshold, r.CandidatesGT, r.TruePositives, tt.kept, tt.tp)
		}
		if math.Abs(r.PrecisionAtT-tt.precAtT) > 1e-9 {
			t.Errorf("row %.2f precision_at_threshold = %.3f, want %.3f",
				r.Threshold, r.PrecisionAtT, tt.precAtT)
		}
		if math.Abs(r.KeptFraction-tt.keptFrac) > 1e-9 {
			t.Errorf("row %.2f kept_fraction = %.3f, want %.3f",
				r.Threshold, r.KeptFraction, tt.keptFrac)
		}
	}
}

// TestJudgeLaneRescueGrading: an unmatched candidate the judge confirms moves
// from FP to TP in the judge lane while the lexical lane is unchanged.
func TestJudgeLaneRescueGrading(t *testing.T) {
	calls := 0
	// Extraction server returns one paraphrased candidate (no lexical match)
	// plus one junk candidate the judge should reject... the judge answers
	// "yes" for every pair, so both get rescued in the judge lane.
	extractSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"choices":[{"message":{"content":"[{\"type\":\"claim\",\"text\":\" deploys every friday afternoon \",\"confidence\":0.9}]"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)); err != nil {
			t.Errorf("write extract response: %v", err)
		}
	}))
	t.Cleanup(extractSrv.Close)
	judgeSrv := judgeAnswerServer(t, "yes", &calls)

	c := &corpus{Segments: []segment{{
		ID:       "seg-rescue",
		Messages: []string{"we deploy on fridays"},
		Gold:     goldSet{Claims: []string{"the team deploys on fridays"}},
	}}}
	extractClient := newChatClient(extractSrv.URL+"/v1", "test-model", 5*time.Second)
	jp := newJudgePairer(newChatClient(judgeSrv.URL+"/v1", "test-model", 5*time.Second))
	m := runAmbientEvalJudge(extractClient, c, 0.6, jp)

	if m.TruePositives != 0 || m.FalsePositives != 1 || m.FalseNegatives != 1 {
		t.Fatalf("lexical lane tp/fp/fn = %d/%d/%d, want 0/1/1", m.TruePositives, m.FalsePositives, m.FalseNegatives)
	}
	if m.Judge == nil {
		t.Fatal("judge block missing with judge enabled")
	}
	if m.Judge.TruePositives != 1 || m.Judge.FalsePositives != 0 || m.Judge.FalseNegatives != 0 {
		t.Errorf("judge lane tp/fp/fn = %d/%d/%d, want 1/0/0 (rescued)",
			m.Judge.TruePositives, m.Judge.FalsePositives, m.Judge.FalseNegatives)
	}
	if m.Judge.Precision != 1 || m.Judge.Recall != 1 {
		t.Errorf("judge precision/recall = %.3f/%.3f, want 1/1", m.Judge.Precision, m.Judge.Recall)
	}
	if len(m.ConfidenceAnalysis) != 1 || !m.ConfidenceAnalysis[0].MatchedJudge {
		t.Errorf("confidence_analysis = %+v, want one judge-matched point", m.ConfidenceAnalysis)
	}
}
