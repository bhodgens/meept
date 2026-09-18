package agent

// Routing-repair leaf (docs/plans/20260917-routing-repair/04-routing.md).
//
// Pins for AR-1 (arithmetic fast path), AR-2 (media-URL guard hijack),
// AR-3 (locative-'at' / 'reminder' time evidence), AR-4 (punctuated
// polite lead-in defeats imperative recognition), and I11 (fallback
// taxonomy parity). Every accepted defect is driven through the REAL
// production entry points — classifyIntent / ClassifyAndRoute — with
// synthetic classifier verdicts injected over a local HTTP test server.
// No live model is involved; none of these tests claims model quality.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
)

// routingRepairMemCtx is the minimal MemoryContext the classifiers read.
func routingRepairMemCtx() *MemoryContext {
	return &MemoryContext{Results: []memory.MemoryResult{}, IntentCounts: map[string]int{}}
}

// ---------------------------------------------------------------------------
// Task 1 — AR-1: restrict the arithmetic fast path
// ---------------------------------------------------------------------------

// TestRoutingRepairArithmetic pins AR-1: the short-message guard's
// arithmetic fast path recognized "arithmetic" by punctuation anywhere
// in prose, so "what is the bug in src/parser.go?" matched
// "what is " + "/" and was short-circuited to chat
// (method=short_message_guard) before any classifier ran. An arithmetic
// EXPRESSION (operands around operators) is different from a question
// carrying a path. The diagnostic question must reach classification.
func TestRoutingRepairArithmetic(t *testing.T) {
	// An observing classifier: any input that reaches it is recorded.
	observed := map[string]bool{}
	d := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
		ClassifierClient: newRoutingRepairClassifierServer(t, func(input string) string {
			if strings.Contains(input, "bug in src/parser.go") {
				observed["diagnostic"] = true
			}
			return `{"intent":"skeptic","confidence":0.9}`
		}),
	})

	// The frozen live case (scratch rig 2026-09-17): a diagnostic path
	// question routed agent=chat method=short_message_guard conf=0.9.
	const diagnostic = "what is the bug in src/parser.go?"

	intent, err := d.classifyIntent(context.Background(), diagnostic, routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent: %v", err)
	}
	if intent.Method == "short_message_guard" {
		t.Fatalf("AR-1: diagnostic path question short-circuited to chat via short_message_guard; the arithmetic guard matched punctuation, not an expression")
	}
	if !observed["diagnostic"] {
		t.Fatalf("AR-1: diagnostic question never reached the classifier; the guard swallowed it before classification")
	}
	if intent.Type != string(IntentSkeptic) {
		t.Fatalf("AR-1: intent = %q (method=%q), want the classifier's skeptic verdict", intent.Type, intent.Method)
	}

	// Arithmetic POSITIVES: genuine expressions keep the fast path.
	for _, expr := range []string{
		"what is 2 + 2?",
		"what is 144/12?",
		"2+2",
		"what is 3.5 * 4",
		"(2 + 3) * 7",
	} {
		intent, err := d.classifyIntent(context.Background(), expr, routingRepairMemCtx())
		if err != nil {
			t.Fatalf("classifyIntent(%q): %v", expr, err)
		}
		if intent.Method != "short_message_guard" || intent.Type != string(IntentChat) {
			t.Errorf("arithmetic positive %q: method=%q type=%q, want short_message_guard/chat — the fast path must keep real arithmetic", expr, intent.Method, intent.Type)
		}
	}

	// Path/prose NEGATIVES: source paths, hyphenated names, operator
	// descriptions, and long prose must never read as arithmetic.
	for _, neg := range []string{
		"what is the bug in src/parser.go?",
		"what is wrong with routing/geometry.py?",
		"what is internal/agent/dispatcher.go for?",
		"what is a hyphenated-name like well-known-flag doing here?",
		"what is the difference between the + and - operators?",
		"what is the plus operator used for in the parser code?",
		"explain what the * operator means in Go pointer declarations and why the syntax is written that way in this language",
	} {
		intent, err := d.classifyIntent(context.Background(), neg, routingRepairMemCtx())
		if err != nil {
			t.Fatalf("classifyIntent(%q): %v", neg, err)
		}
		if intent.Method == "short_message_guard" {
			t.Errorf("arithmetic negative %q hit the short_message_guard; operator symbols in prose are not an expression", neg)
		}
	}
}

// ---------------------------------------------------------------------------
// Task 2 — AR-2: distinguish media consumption from URL data
// ---------------------------------------------------------------------------

// TestRoutingRepairMediaOperation pins AR-2 through ClassifyAndRoute: the
// media-URL guard fired on ANY message carrying a YouTube URL, so a
// request to WRITE A UNIT TEST using a video's content was hijacked to
// the analyst at 0.9 before classification. A media-consumption request
// (watch/summarize/transcribe the video) is different from a coding
// request that merely cites a URL as DATA. Deterministic ingestion
// still requires media-consumption evidence; summary/transcript
// requests and context-qualified bare IDs keep the old route.
func TestRoutingRepairMediaOperation(t *testing.T) {
	const vid = "https://www.youtube.com/watch?v=DWoJZs6TuVs"

	// AR-2 repro: a coding request whose payload cites a YouTube URL.
	// Injected code verdict @0.9 (above the 0.75 code threshold): the
	// classifier chain — not the media guard — must decide.
	codingRequests := []string{
		"write a unit test for the transcript parser using the video " + vid + " as the fixture",
		"add a test case covering https://youtu.be/DWoJZs6TuVs to the parser test file",
	}
	for _, in := range codingRequests {
		d := NewDispatcher(DispatcherConfig{
			Logger: testLogger(),
			ClassifierClient: newRoutingRepairClassifierServer(t, func(_ string) string {
				return `{"intent":"code","confidence":0.9}`
			}),
		})
		res, err := d.ClassifyAndRoute(context.Background(), in, "sess-routing-repair-media", nil, "", "")
		if err != nil {
			t.Fatalf("ClassifyAndRoute(%q): %v", in, err)
		}
		if res.Intent == nil {
			t.Fatalf("ClassifyAndRoute(%q): nil intent", in)
		}
		if res.Intent.Method == "media_url_guard" {
			t.Errorf("AR-2: coding request %q hijacked by media_url_guard; a URL cited as data is not a media-consumption request", in)
			continue
		}
		if res.Intent.Type != string(IntentCode) {
			t.Errorf("AR-2: coding request %q routed intent=%q (method=%q), want the injected code verdict", in, res.Intent.Type, res.Intent.Method)
		}
	}

	// Quoted URL: a coding request quoting the URL as a string literal.
	quoted := "write a parser handling \"https://youtu.be/DWoJZs6TuVs\" style links"
	dq := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
		ClassifierClient: newRoutingRepairClassifierServer(t, func(_ string) string {
			return `{"intent":"code","confidence":0.9}`
		}),
	})
	resq, err := dq.ClassifyAndRoute(context.Background(), quoted, "sess-routing-repair-media", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(quoted): %v", err)
	}
	if resq.Intent == nil || resq.Intent.Method == "media_url_guard" || resq.Intent.Type != string(IntentCode) {
		t.Errorf("quoted-URL coding request %q: intent=%+v, want the injected code verdict (URL as string data)", quoted, resq.Intent)
	}

	// Media-consumption POSITIVES: deterministic analyst ingestion kept.
	for _, in := range []string{
		"summarize this video: " + vid,
		"get the transcript for " + vid,
		"watch " + vid + " and take notes",
		"summarize this video DWoJZs6TuVs",
	} {
		d := NewDispatcher(DispatcherConfig{Logger: testLogger()})
		res, err := d.ClassifyAndRoute(context.Background(), in, "sess-routing-repair-media", nil, "", "")
		if err != nil {
			t.Fatalf("ClassifyAndRoute(%q): %v", in, err)
		}
		if res.Intent == nil || res.Intent.Method != "media_url_guard" || res.Intent.AgentType != config.AgentIDAnalyst {
			t.Errorf("media positive %q: intent=%+v, want analyst via media_url_guard — consumption evidence keeps deterministic ingestion", in, res.Intent)
		}
	}

	// Mixed control: consumption verb + coding verb in one request with a
	// URL must NOT deterministically ingest — the compound-signal guard
	// excludes it from any direct route and the multi-intent detector
	// (compound words + analyze/write lanes) owns the turn. The media
	// guard must not preempt that chain.
	mixed := "summarize the video " + vid + " and then write the summary into notes.md"
	d := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
		ClassifierClient: newRoutingRepairClassifierServer(t, func(_ string) string {
			return `[{"intent":"analyze","confidence":0.9},{"intent":"write","confidence":0.9}]`
		}),
	})
	if !hasCompoundSignalWords(mixed) {
		t.Fatalf("mixed control precondition: %q should carry a compound signal word", mixed)
	}
	res, err := d.ClassifyAndRoute(context.Background(), mixed, "sess-routing-repair-media", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(mixed): %v", err)
	}
	if res.Intent != nil && res.Intent.Method == "media_url_guard" {
		t.Errorf("mixed consumption+write request hit media_url_guard; compound-shaped work must go through the chain")
	}
}

// ---------------------------------------------------------------------------
// Task 3 — AR-3/AR-4: evidence predicates without moving arbitration
// ---------------------------------------------------------------------------

// TestRoutingRepairArbitrationEvidence pins AR-3 and AR-4 with injected
// verdicts. AR-3: hasTimeSignal counted a LOCATIVE preposition ("at
// src/parser.go") and the artifact noun "reminder" as timing
// evidence, so untrusted schedule verdicts survived arbitration. AR-4:
// the polite lead-in loops compared the RAW field, so "hey," (comma
// attached) defeated imperative recognition and the platform/git
// salvage never fired. Order recall > imperative > git-veto is
// preserved; real reminder/schedule requests keep their route.
func TestRoutingRepairArbitrationEvidence(t *testing.T) {
	newDisp := func(verdict string) *Dispatcher {
		return NewDispatcher(DispatcherConfig{
			Logger: testLogger(),
			ClassifierClient: newRoutingRepairClassifierServer(t, func(_ string) string {
				return `{"intent":"` + verdict + `","confidence":0.9}`
			}),
		})
	}

	// --- AR-3: locative 'at' is not temporal evidence ---
	// Injected schedule @0.9 on a file question locating a path with "at"
	// must be arbitrated away (no real time signal).
	locative := "what is the parse error at src/parser.go line 12"
	if hasTimeSignal(locative) {
		t.Errorf("AR-3 precondition: hasTimeSignal(%q) = true; a locative preposition is not timing", locative)
	}
	intent, err := newDisp("schedule").classifyIntent(context.Background(), locative, routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(locative): %v", err)
	}
	if intent.Type == string(IntentSchedule) {
		t.Errorf("AR-3: untrusted schedule verdict survived on %q (method=%q); time evidence must express timing, not location", locative, intent.Method)
	}

	// AR-3 second arm: "reminder" naming an ARTIFACT is not a scheduling
	// request. The leaf fixes the evidence class only if the injected
	// production route reproduces the defect; the predicate-level pin
	// holds regardless.
	artifactReminder := "Create a reminder component in React with a bell icon and a snooze button"
	if hasTimeSignal(artifactReminder) {
		t.Errorf("AR-3: hasTimeSignal(%q) = true; 'reminder' naming an artifact is not a time signal", artifactReminder)
	}

	// AR-3 control: genuine time signals still count.
	for _, s := range []string{
		"remind me to call mom tomorrow",
		"set a timer for 5 minutes",
		"schedule a meeting at 3pm",
	} {
		if !hasTimeSignal(s) {
			t.Errorf("AR-3 control: hasTimeSignal(%q) = false, want true — real scheduling vocabulary must keep firing", s)
		}
	}

	// --- AR-4: punctuated polite lead-ins are still imperative ---
	// git @0.9 on "hey create…" and "hey, create…" with no git verb: the
	// git agreement veto must discard both.
	for _, in := range []string{
		"hey create a file named hello.txt containing the word hello",
		"hey, create a file named hello.txt containing the word hello",
	} {
		if !hasLeadingImperativeVerb(in) {
			t.Errorf("AR-4: hasLeadingImperativeVerb(%q) = false; punctuation on a polite lead-in must not defeat imperative recognition", in)
		}
		intent, err := newDisp("git").classifyIntent(context.Background(), in, routingRepairMemCtx())
		if err != nil {
			t.Fatalf("classifyIntent(%q): %v", in, err)
		}
		if intent.Type == string(IntentGit) {
			t.Errorf("AR-4: git verdict survived on %q (method=%q); the agreement veto required imperative recognition", in, intent.Method)
		}
	}
	// platform @0.9 on the punctuated lead-in must also be discarded.
	intent, err = newDisp("platform").classifyIntent(context.Background(),
		"hey, create a file named hello.txt containing the word hello", routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(punctuated platform): %v", err)
	}
	if intent.Type == string(IntentPlatform) {
		t.Errorf("AR-4: platform verdict survived on a punctuated-lead-in imperative (method=%q)", intent.Method)
	}

	// AR-4 controls: real imperative + real git verb keep their routes.
	gitImperative := "commit the changes in the working tree"
	intent, err = newDisp("git").classifyIntent(context.Background(), gitImperative, routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(git control): %v", err)
	}
	if intent.Type != string(IntentGit) {
		t.Errorf("AR-4 control: genuine git imperative %q routed %q (method=%q), want git — true git verbs must survive the veto", gitImperative, intent.Type, intent.Method)
	}

	// Recall control (recall > imperative order preserved): "hey, did the
	// change get made?" with a platform verdict still lands on recall.
	intent, err = newDisp("platform").classifyIntent(context.Background(),
		"hey, did the change get made?", routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(recall control): %v", err)
	}
	if intent.Type != string(IntentRecall) || intent.Method != "platform_recall_arbitration" {
		t.Errorf("recall control: intent=%q method=%q, want recall/platform_recall_arbitration — the recall branch outranks the imperative salvage", intent.Type, intent.Method)
	}

	// Disabled-veto control: config-disable still bypasses the git veto.
	vetoOff := false
	d := NewDispatcher(DispatcherConfig{
		Logger: testLogger(),
		ClassifierClient: newRoutingRepairClassifierServer(t, func(_ string) string {
			return `{"intent":"git","confidence":0.9}`
		}),
		GitVerbAgreementVeto: &vetoOff,
	})
	intent, err = d.classifyIntent(context.Background(),
		"hey, create a file named hello.txt containing the word hello", routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(veto disabled): %v", err)
	}
	if intent.Type != string(IntentGit) {
		t.Errorf("disabled-veto control: intent=%q (method=%q), want git surviving with git_verb_agreement_veto=false", intent.Type, intent.Method)
	}
}

// ---------------------------------------------------------------------------
// Task 4 — I11: align degraded routing with the output-based taxonomy
// ---------------------------------------------------------------------------

// TestRoutingRepairFallbackTaxonomy pins I11: with the LLM classifier
// unavailable, the degraded path (keyword → heuristic fallback) must
// agree with docs/workflows/intent-routing.md. Review-and-correct
// requests selected DEBUG from the "bug(s)" substring; informational
// "help me understand" selected PLATFORM from the platform keyword row.
// A shared operation/evidence rule — verdict-only phrasing routes
// review; one named defect routes debug; correction/autonomy clauses
// route quickplan; platform questions about meept itself route platform
// — replaces per-sentence exceptions.
func TestRoutingRepairFallbackTaxonomy(t *testing.T) {
	// No ClassifierClient: the LLM lane is unavailable, so routing runs
	// the keyword/heuristic degraded path after the guards.
	newDisp := func() *Dispatcher {
		return NewDispatcher(DispatcherConfig{Logger: testLogger()})
	}

	// I11 repro 1: review-and-correct must not select debug via "bugs".
	reviewCorrect := "review the auth module for bugs and correct them as you find them"
	d := newDisp()
	res, err := d.ClassifyAndRoute(context.Background(), reviewCorrect, "sess-routing-repair-tax", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(review-and-correct): %v", err)
	}
	if res.Intent == nil {
		t.Fatal("review-and-correct: nil intent")
	}
	if res.Intent.Type == string(IntentDebug) {
		t.Errorf("I11: review-and-correct %q routed debug via %q; a verdict-plus-correction request is not one named defect", reviewCorrect, res.Intent.Method)
	}

	// I11 repro 2: informational "help me understand" must not select
	// platform via the bare keyword.
	helpUnderstand := "help me understand how the tokenizer handles unicode"
	res, err = newDisp().ClassifyAndRoute(context.Background(), helpUnderstand, "sess-routing-repair-tax", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(help me understand): %v", err)
	}
	if res.Intent == nil {
		t.Fatal("help me understand: nil intent")
	}
	if res.Intent.Type == string(IntentPlatform) {
		t.Errorf("I11: informational %q routed platform via %q; asking to understand X is not platform introspection", helpUnderstand, res.Intent.Method)
	}

	// Operation-class POSITIVES (shared rule, not sentence exceptions):

	// Verdict-only review → review (findings only, no changes).
	verdictOnly := "review the json files to make sure they are complete and report any that are missing required fields"
	res, err = newDisp().ClassifyAndRoute(context.Background(), verdictOnly, "sess-routing-repair-tax", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(verdict-only): %v", err)
	}
	if res.Intent == nil || res.Intent.Type != string(IntentReview) {
		t.Errorf("verdict-only review %q: intent=%+v, want review", verdictOnly, res.Intent)
	}

	// One named defect → debug.
	namedDefect := "fix the nil pointer dereference in handler.go line 42"
	intent, err := newDisp().classifyIntent(context.Background(), namedDefect, routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(named defect): %v", err)
	}
	if intent.Type != string(IntentDebug) {
		t.Errorf("one named defect %q: intent=%q (method=%q), want debug", namedDefect, intent.Type, intent.Method)
	}

	// Actual platform question → platform.
	platformQ := "what are your capabilities for processing large files"
	res, err = newDisp().ClassifyAndRoute(context.Background(), platformQ, "sess-routing-repair-tax", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(platform): %v", err)
	}
	if res.Intent == nil || res.Intent.Type != string(IntentPlatform) {
		t.Errorf("platform question %q: intent=%+v, want platform", platformQ, res.Intent)
	}

	// Repo-document changes request → code (artifact to change).
	repoDoc := "update the README file to document the new configuration keys in the repository"
	intent, err = newDisp().classifyIntent(context.Background(), repoDoc, routingRepairMemCtx())
	if err != nil {
		t.Fatalf("classifyIntent(repo-doc): %v", err)
	}
	if intent.Type != string(IntentCode) && intent.Type != string(IntentWrite) {
		t.Errorf("repo-document change %q: intent=%q (method=%q), want code or write", repoDoc, intent.Type, intent.Method)
	}
}

// newRoutingRepairClassifierServer is the synthetic-verdict seam: a real
// *llm.Client over an httptest server whose response is chosen per input
// by the supplied selector. Drives the production LLM-classifier wire
// path with no live model. The selector receives the FULL last message
// content (the classification prompt embeds the user input after its
// "Input: " line); match on classifierPromptInput(input) for
// input-keyed verdicts.
func newRoutingRepairClassifierServer(t *testing.T, selectVerdict func(input string) string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []llm.ChatMessage `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		var input string
		// The classifier prompt places the user input in the final
		// message; the analyzer prompt carries its own markers and is
		// answered with a non-ambiguous analysis.
		if n := len(req.Messages); n > 0 {
			input = req.Messages[n-1].Content
		}
		var content string
		switch {
		case strings.Contains(input, "ambiguity"):
			content = `{"goal":"routing repair probe","ambiguity":0.0,"scope":"narrow","category":"code","suggested_questions":[],"confidence":0.9}`
		case strings.Contains(input, "Classify this user input"):
			content = selectVerdict(input)
		case strings.Contains(input, "identify ALL distinct intents"):
			// Multi-intent detector: honor a JSON-ARRAY verdict from the
			// selector (compound-shaped mixed requests); otherwise never
			// compound.
			if v := selectVerdict(input); strings.HasPrefix(strings.TrimSpace(v), "[") {
				content = v
			} else {
				content = `[]`
			}
		default:
			content = selectVerdict(input)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "routing-repair-capture"})
}
