package main

import (
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
