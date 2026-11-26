package agent

// Approval-adjudication pins (plan-contract tree
// 20261125-plan-contract-quickplan leaf 03, master C-3, decision D3;
// closes bughunt 2026-11-24 M6/H6).
//
// Mechanism under test: TrueIntentAnalysis gains approval_requested; the
// analyzer prompt instructs true ONLY for explicit pre-execution approval
// requests; a narrow veto-suspect TRIGGER pattern (never a verdict) fires
// ONE bounded fallback adjudication call reusing the analyzer client; any
// adjudication error/degradation keeps false + one Debug line and NEVER
// parks or fails the turn (D3 fallback semantics). Model behavior itself is
// prompt-tested in e2e; this table pins parse + trigger + fallback.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// sequencedServer replies with canned contents in order (last one repeats),
// recording every request body — the analyzer's primary call takes slot 0,
// the fallback adjudication takes slot 1.
type sequencedServer struct {
	Server *httptest.Server

	mu      sync.Mutex
	bodies  [][]byte
	respons []string
}

func newSequencedServer(t *testing.T, responses ...string) *sequencedServer {
	t.Helper()
	ss := &sequencedServer{respons: responses}
	ss.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAllForTest(r)
		ss.mu.Lock()
		idx := len(ss.bodies)
		ss.bodies = append(ss.bodies, body)
		ss.mu.Unlock()
		content := responses[len(responses)-1]
		if idx < len(responses) {
			content = responses[idx]
		}
		w.Header().Set("Content-Type", "application/json")
		resp := `{"choices":[{"message":{"role":"assistant","content":` + strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(ss.Server.Close)
	return ss
}

func (ss *sequencedServer) callCount() int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return len(ss.bodies)
}

// adjudicationRequestMessages returns the messages array of request n —
// used to prove the fallback call carried the adjudication prompt.
func (ss *sequencedServer) adjudicationRequestMessages(t *testing.T, n int) []llm.ChatMessage {
	t.Helper()
	ss.mu.Lock()
	if n >= len(ss.bodies) {
		ss.mu.Unlock()
		t.Fatalf("expected at least %d captured requests, got %d", n+1, len(ss.bodies))
	}
	body := ss.bodies[n]
	ss.mu.Unlock()
	var req capturedChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode captured request %d: %v", n, err)
	}
	return req.Messages
}

func readAllForTest(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}

func newAnalyzerOnServer(ss *sequencedServer) *IntentAnalyzer {
	return newIntentAnalyzerWithConfig(IntentAnalyzerConfig{}, llm.NewClient(&llm.ModelConfig{
		BaseURL: ss.Server.URL,
		ModelID: "capture",
	}), digestTestLogger())
}

// TestApprovalRequested_Parse pins the C-3 wire shape: JSON with
// approval_requested true/false/absent lands the field; an absent field is
// legacy byte-identical (false, and re-marshal omits it via omitempty).
func TestApprovalRequested_Parse(t *testing.T) {
	cases := []struct {
		name string
		json string
		want bool
	}{
		{"true", `{"goal":"g","ambiguity":0.1,"scope":"narrow","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":true}`, true},
		{"false", `{"goal":"g","ambiguity":0.1,"scope":"narrow","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":false}`, false},
		{"absent", `{"goal":"g","ambiguity":0.1,"scope":"narrow","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan"}`, false},
	}
	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{}, nil, digestTestLogger())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ia.parseAnalysis(tc.json)
			if err != nil {
				t.Fatalf("parseAnalysis: %v", err)
			}
			if got.ApprovalRequested != tc.want {
				t.Errorf("ApprovalRequested = %v, want %v", got.ApprovalRequested, tc.want)
			}
		})
	}

	// Legacy byte-identity: an analysis parsed from absent-field JSON
	// re-marshals without the approval_requested key.
	got, err := ia.parseAnalysis(cases[2].json)
	if err != nil {
		t.Fatalf("parseAnalysis: %v", err)
	}
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "approval_requested") {
		t.Errorf("absent field leaked into re-marshaled JSON: %s", out)
	}
}

// TestApprovalRequested_PromptInstruction pins that the frozen owner
// examples + the field are IN the analyzer system prompt (the prompt is
// what instructs the model; behavior is e2e-tested elsewhere).
func TestApprovalRequested_PromptInstruction(t *testing.T) {
	needles := []string{
		"approval_requested",
		"plan the refactoring and implement it only after I approve",
		"write a plan for X and create a markdown file",
		"plan X then implement it",
	}
	for _, n := range needles {
		if !strings.Contains(intentAnalysisSystemPrompt, n) {
			t.Errorf("analyzer system prompt missing instruction fragment %q", n)
		}
	}
	// The prompt must tie true to an EXPLICIT pre-execution approval request.
	lower := strings.ToLower(intentAnalysisSystemPrompt)
	if !strings.Contains(lower, "before") || !strings.Contains(lower, "approv") {
		t.Error("approval_requested instruction must gate on explicit pre-execution approval")
	}
}

// TestApprovalRequested_ProbeSentenceTable pins the 6 M6/owner probe
// sentences against the NEW mechanism. Each row: canned primary-analysis
// response (what the model returns when instructed per the pinned prompt)
// plus the expected landing after trigger+fallback adjudication. Old-regex
// polarity escapes ("write a plan ... create a markdown file" -> parked)
// now land false: doc-only work is NOT an approval request (D4).
func TestApprovalRequested_ProbeSentenceTable(t *testing.T) {
	const quickplanJSON = `{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan"`
	cases := []struct {
		sentence  string
		primary   string // canned primary analysis JSON (no approval_requested => fallback decides)
		adjud     string // canned adjudication response
		wantFinal bool
	}{
		{
			sentence:  "Plan X and implement it only after I approve",
			primary:   quickplanJSON + "}",
			adjud:     `{"approval_required":true}`,
			wantFinal: true,
		},
		{
			sentence:  "plan the refactoring and implement it only after I approve",
			primary:   quickplanJSON + "}",
			adjud:     `{"approval_required":true}`,
			wantFinal: true,
		},
		{
			// M6 escape (doc-only): no approval vocabulary => trigger
			// never fires => false with ZERO adjudication calls.
			sentence:  "Write a plan for X and create a markdown file",
			primary:   quickplanJSON + "}",
			adjud:     `{"approval_required":true}`, // would flip if wrongly called
			wantFinal: false,
		},
		{
			sentence:  "plan X then implement it",
			primary:   quickplanJSON + "}",
			adjud:     `{"approval_required":true}`,
			wantFinal: false,
		},
		{
			// "dont implement it" carries NO approve/sign-off/go-ahead
			// vocabulary: it is not a pre-execution approval REQUEST; the
			// new mechanism parks only on approval requests (D3/D4).
			sentence:  "plan the migration but dont implement it",
			primary:   quickplanJSON + "}",
			adjud:     `{"approval_required":true}`,
			wantFinal: false,
		},
		{
			// Primary model already voted false while the phrase is
			// veto-suspect: the bounded adjudication overrules to true.
			sentence:  "plan the rollout, hold off coding until I sign off",
			primary:   quickplanJSON + `,"approval_requested":false}`,
			adjud:     `{"approval_required":true}`,
			wantFinal: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.sentence, func(t *testing.T) {
			ss := newSequencedServer(t, tc.primary, tc.adjud)
			ia := newAnalyzerOnServer(ss)
			got, err := ia.AnalyzeTrueIntent(context.Background(), tc.sentence, nil)
			if err != nil {
				t.Fatalf("AnalyzeTrueIntent: %v", err)
			}
			if got.ApprovalRequested != tc.wantFinal {
				t.Errorf("ApprovalRequested = %v, want %v", got.ApprovalRequested, tc.wantFinal)
			}
			if ss.callCount() > 1 && !vetoSuspectTrigger(tc.sentence) {
				t.Errorf("adjudication fired (%d calls) but trigger pattern rejects the sentence", ss.callCount())
			}
			if !tc.wantFinal && !vetoSuspectTrigger(tc.sentence) && ss.callCount() != 1 {
				t.Errorf("non-suspect sentence made %d calls, want 1 (no adjudication)", ss.callCount())
			}
		})
	}
}

// TestApprovalRequested_PrimaryTrueSkipsFallback pins the A-path: when the
// analyzer itself votes approval_requested=true, no second call fires.
func TestApprovalRequested_PrimaryTrueSkipsFallback(t *testing.T) {
	primary := `{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":true}`
	ss := newSequencedServer(t, primary, `{"approval_required":false}`)
	ia := newAnalyzerOnServer(ss)
	got, err := ia.AnalyzeTrueIntent(context.Background(), "plan the refactor and implement it only after I approve", nil)
	if err != nil {
		t.Fatalf("AnalyzeTrueIntent: %v", err)
	}
	if !got.ApprovalRequested {
		t.Error("primary true vote was overwritten")
	}
	if ss.callCount() != 1 {
		t.Errorf("adjudication fired %d extra times on a primary true vote, want 0", ss.callCount()-1)
	}
}

// TestApprovalRequested_FallbackErrorKeepsOneshot pins D3: an adjudication
// that errors (garbage reply on slot 2) keeps false — the turn is never
// parked nor failed by adjudication.
func TestApprovalRequested_FallbackErrorKeepsOneshot(t *testing.T) {
	primary := `{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan","approval_requested":false}`
	ss := newSequencedServer(t, primary, "sorry, I cannot answer that")
	ia := newAnalyzerOnServer(ss)
	got, err := ia.AnalyzeTrueIntent(context.Background(), "plan the refactor and implement it only after I approve", nil)
	if err != nil {
		t.Fatalf("adjudication degradation must not fail the analysis: %v", err)
	}
	if got.ApprovalRequested {
		t.Error("failed adjudication parked the turn (true)")
	}
	if ss.callCount() != 2 {
		t.Errorf("calls = %d, want 2 (one bounded adjudication attempt)", ss.callCount())
	}
	// The second call is the ADJUDICATION, not a re-run of the analysis:
	// its system prompt is the phrasing-adjudicator text.
	msgs := ss.adjudicationRequestMessages(t, 1)
	if len(msgs) == 0 || msgs[0].Content != approvalAdjudicationSystemPrompt {
		t.Error("second call did not carry the adjudication system prompt")
	}
}

// TestApprovalRequested_NonSuspectSkipsFallback pins the cheap prefilter:
// ordinary quickplan phrasing never spends the adjudication call.
func TestApprovalRequested_NonSuspectSkipsFallback(t *testing.T) {
	primary := `{"goal":"g","ambiguity":0.1,"scope":"medium","category":"quickplan","suggested_questions":[],"confidence":0.9,"suggested_mode":"quick_plan"}`
	ss := newSequencedServer(t, primary, `{"approval_required":true}`)
	ia := newAnalyzerOnServer(ss)
	if _, err := ia.AnalyzeTrueIntent(context.Background(), "just do the migration end to end without check-ins", nil); err != nil {
		t.Fatalf("AnalyzeTrueIntent: %v", err)
	}
	if ss.callCount() != 1 {
		t.Errorf("non-suspect input made %d calls, want 1", ss.callCount())
	}
}

// TestVetoSuspectTrigger pins the trigger vocabulary ITSELF: it fires only
// on approve/sign-off/go-ahead class words joined to before/wait
// vocabulary; it is a TRIGGER, never a verdict.
func TestVetoSuspectTrigger(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"plan X and implement it only after I approve", true},
		{"plan the rollout, hold off coding until I sign off", true},
		{"wait for my go-ahead before building it", true},
		{"write a plan for X and create a markdown file", false},
		{"plan X then implement it", false},
		{"plan the migration but dont implement it", false},
		{"just do the migration end to end without check-ins", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := vetoSuspectTrigger(tc.input); got != tc.want {
			t.Errorf("vetoSuspectTrigger(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
