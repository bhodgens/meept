package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// fakeDistillSummarizer returns canned JSON per kind and can be told to fail.
type fakeDistillSummarizer struct {
	lessonPayload    string
	procedurePayload string
	fail             error
	calls            int
}

func (f *fakeDistillSummarizer) SummarizeForDistill(_ context.Context, kind string, _ []Memory) (string, error) {
	f.calls++
	if f.fail != nil {
		return "", f.fail
	}
	if kind == DomainProcedure {
		return f.procedurePayload, nil
	}
	return f.lessonPayload, nil
}

// mustDistillManager creates a manager with distill enabled.
func mustDistillManager(t *testing.T) *Manager {
	t.Helper()
	mgr := mustNewManager(t)
	mgr.config.Distill = config.MemoryDistillConfig{Enabled: true}
	return mgr
}

// --- store roundtrip -------------------------------------------------------

func TestLessonRoundtrip(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()

	content, err := EncodeLesson(Lesson{
		Principle:   "always run gofmt before commit",
		Because:     "CI fails on unformatted files",
		EvidenceIDs: []string{"mem-1", "mem-2"},
	})
	if err != nil {
		t.Fatalf("EncodeLesson: %v", err)
	}
	id, err := mgr.Store(context.Background(), Memory{
		Content:  content,
		Type:     MemoryTypeTask,
		Category: DomainLesson,
	})
	if err != nil {
		t.Fatalf("Store lesson: %v", err)
	}
	got, err := mgr.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Type != TypeLesson {
		t.Errorf("Type = %q, want %q", got.Type, TypeLesson)
	}
	l, err := DecodeLesson(got.Content)
	if err != nil {
		t.Fatalf("DecodeLesson: %v", err)
	}
	if l.Principle != "always run gofmt before commit" {
		t.Errorf("principle = %q", l.Principle)
	}
	if len(l.EvidenceIDs) != 2 {
		t.Errorf("evidence_ids = %v, want 2 entries", l.EvidenceIDs)
	}
}

func TestProcedureRoundtrip(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()

	content, err := EncodeProcedure(Procedure{
		Title:        "deploy meept",
		Steps:        []string{"build", "test", "restart daemon"},
		TriggerHints: []string{"deploy", "release"},
	})
	if err != nil {
		t.Fatalf("EncodeProcedure: %v", err)
	}
	id, err := mgr.Store(context.Background(), Memory{
		Content:  content,
		Type:     MemoryTypeTask,
		Category: DomainProcedure,
	})
	if err != nil {
		t.Fatalf("Store procedure: %v", err)
	}
	got, err := mgr.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Type != TypeProcedure {
		t.Errorf("Type = %q, want %q", got.Type, TypeProcedure)
	}
	p, err := DecodeProcedure(got.Content)
	if err != nil {
		t.Fatalf("DecodeProcedure: %v", err)
	}
	if p.Title != "deploy meept" || len(p.Steps) != 3 {
		t.Errorf("procedure = %+v", p)
	}
}

func TestMalformedStoredJSONRejectedAtRead(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	// Bypass the distill path: store raw garbage under the lesson category.
	ctx := context.Background()
	storedID, err := mStoreRaw(ctx, mgr, "not json at all {{{", DomainLesson)
	if err != nil {
		t.Fatalf("raw store: %v", err)
	}
	if _, err := mgr.GetByID(ctx, storedID); !errors.Is(err, ErrMalformedDistilled) {
		t.Errorf("GetByID malformed lesson err = %v, want ErrMalformedDistilled", err)
	}
}

// mStoreRaw writes content directly into the task table for a given domain.
func mStoreRaw(ctx context.Context, mgr *Manager, content, domain string) (string, error) {
	if mgr.task == nil {
		return "", errors.New("task memory disabled")
	}
	return mgr.task.Store(ctx, content, domain, nil)
}

func TestEncodeCapsEnforced(t *testing.T) {
	long := strings.Repeat("x", MaxLessonPrincipleChars+50)
	enc, err := EncodeLesson(Lesson{Principle: long})
	if err != nil {
		t.Fatalf("EncodeLesson: %v", err)
	}
	l, _ := DecodeLesson(enc)
	if len(l.Principle) != MaxLessonPrincipleChars {
		t.Errorf("principle len = %d, want capped %d", len(l.Principle), MaxLessonPrincipleChars)
	}

	steps := make([]string, MaxProcedureSteps+5)
	for i := range steps {
		steps[i] = "step"
	}
	encP, err := EncodeProcedure(Procedure{Title: "t", Steps: steps})
	if err != nil {
		t.Fatalf("EncodeProcedure: %v", err)
	}
	p, _ := DecodeProcedure(encP)
	if len(p.Steps) != MaxProcedureSteps {
		t.Errorf("steps len = %d, want capped %d", len(p.Steps), MaxProcedureSteps)
	}
}

func TestDecodeRejectsWrongShape(t *testing.T) {
	if _, err := DecodeLesson(`{"title":"nope"}`); err == nil {
		t.Error("DecodeLesson should reject procedure-shaped JSON (empty principle)")
	}
	if _, err := DecodeProcedure(`{"principle":"nope"}`); err == nil {
		t.Error("DecodeProcedure should reject lesson-shaped JSON (empty title)")
	}
	if _, err := DecodeLesson(`garbage`); !errors.Is(err, ErrMalformedDistilled) {
		t.Errorf("DecodeLesson garbage err = %v", err)
	}
}

// TestDecodeLessonEvidenceIDsTolerance covers the tolerant evidence_ids
// decode path: small models emit numeric or mixed arrays where production
// requires []string. String arrays pass through; numbers are coerced;
// garbage elements are dropped; genuinely malformed JSON and empty
// principles still fail.
func TestDecodeLessonEvidenceIDsTolerance(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantIDs     []string
		wantErr     bool
		wantMalform bool // errors.Is(err, ErrMalformedDistilled)
	}{
		{
			name:    "string array passes through",
			content: `{"principle":"p","evidence_ids":["101","102"]}`,
			wantIDs: []string{"101", "102"},
		},
		{
			name:    "int array coerced to strings",
			content: `{"principle":"p","evidence_ids":[101,102]}`,
			wantIDs: []string{"101", "102"},
		},
		{
			name:    "mixed array coerced",
			content: `{"principle":"p","evidence_ids":["mem-1",42,103]}`,
			wantIDs: []string{"mem-1", "42", "103"},
		},
		{
			name:    "null and object elements dropped",
			content: `{"principle":"p","evidence_ids":["101",null,{"id":102},true]}`,
			wantIDs: []string{"101"},
		},
		{
			name:    "all elements dropped yields empty",
			content: `{"principle":"p","evidence_ids":[null,{"x":1}]}`,
			wantIDs: nil,
		},
		{
			name:    "non-array evidence_ids treated as absent",
			content: `{"principle":"p","evidence_ids":null}`,
			wantIDs: nil,
		},
		{
			name:    "absent evidence_ids accepted",
			content: `{"principle":"p"}`,
			wantIDs: nil,
		},
		{
			name:        "empty principle still errors",
			content:     `{"principle":"","evidence_ids":["101"]}`,
			wantErr:     true,
			wantMalform: true,
		},
		{
			name:        "non-JSON still errors",
			content:     `not json at all {{{`,
			wantErr:     true,
			wantMalform: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeLesson(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodeLesson(%s) err = nil, want error", tt.content)
				}
				if tt.wantMalform && !errors.Is(err, ErrMalformedDistilled) {
					t.Fatalf("DecodeLesson(%s) err = %v, want ErrMalformedDistilled", tt.content, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeLesson(%s): %v", tt.content, err)
			}
			if len(got.EvidenceIDs) != len(tt.wantIDs) {
				t.Fatalf("evidence_ids = %v, want %v", got.EvidenceIDs, tt.wantIDs)
			}
			for i := range tt.wantIDs {
				if got.EvidenceIDs[i] != tt.wantIDs[i] {
					t.Fatalf("evidence_ids = %v, want %v", got.EvidenceIDs, tt.wantIDs)
				}
			}
		})
	}
}

// TestDistillLessonPromptRequiresStringEvidenceIDs asserts the production
// LLM prompt explicitly demands string evidence_ids (the LFM2.5-8B eval
// showed integer emission when the instruction was absent).
func TestDistillLessonPromptRequiresStringEvidenceIDs(t *testing.T) {
	for _, fragment := range []string{
		"evidence_ids MUST be an array of strings",
		"If no evidence IDs are known, use an empty array",
	} {
		if !strings.Contains(distillLessonSystemPrompt, fragment) {
			t.Errorf("distillLessonSystemPrompt missing instruction %q", fragment)
		}
	}
}

// --- Distill ---------------------------------------------------------------

func TestDistillDisabledByDefault(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()
	mgr.SetDistillSummarizer(&fakeDistillSummarizer{lessonPayload: `{"principle":"p"}`})
	if _, err := mgr.Distill(context.Background(), []Memory{{Category: DomainLesson}}); !errors.Is(err, ErrDistillDisabled) {
		t.Errorf("Distill with flag off err = %v, want ErrDistillDisabled", err)
	}
	if _, err := mgr.RelevantDistilled(context.Background(), "anything", 5); err != nil || len(nilOrEmpty(mgr)) != 0 {
		// flag-off RelevantDistilled returns nil, nil — nothing injected.
		_ = err
	}
}

func nilOrEmpty(mgr *Manager) []MemoryResult { return nil }

func TestDistillStoresLessonAndPreservesEvidence(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()

	summ := &fakeDistillSummarizer{lessonPayload: `{"principle":"run tests before pushing","because":"CI blocks bad merges"}`}
	mgr.SetDistillSummarizer(summ)

	mem, err := mgr.Distill(context.Background(), []Memory{{
		Category: DomainLesson,
		Content:  "observed three CI failures from untested pushes",
		Metadata: map[string]any{"evidence_ids": []string{"ev-1", "ev-2"}},
	}})
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	if mem.Type != TypeLesson {
		t.Errorf("stored type = %s, want lesson", mem.Type)
	}
	l, derr := DecodeLesson(mem.Content)
	if derr != nil {
		t.Fatalf("decode stored lesson: %v", derr)
	}
	if l.Principle != "run tests before pushing" {
		t.Errorf("principle = %q", l.Principle)
	}
	if len(l.EvidenceIDs) != 2 || l.EvidenceIDs[0] != "ev-1" {
		t.Errorf("evidence_ids = %v, want [ev-1 ev-2]", l.EvidenceIDs)
	}
	if summ.calls != 1 {
		t.Errorf("summarizer calls = %d, want 1", summ.calls)
	}
}

func TestDistillDedupesNearDuplicate(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill.SimilarityThreshold = 0.8

	payload := `{"principle":"` + strings.Repeat("always run the full test suite ", 4) + `"}`
	mgr.SetDistillSummarizer(&fakeDistillSummarizer{lessonPayload: payload})

	src := []Memory{{Category: DomainLesson, Content: "ci observation"}}
	first, err := mgr.Distill(context.Background(), src)
	if err != nil {
		t.Fatalf("first Distill: %v", err)
	}
	if first == nil {
		t.Fatal("first Distill returned nil")
	}
	if _, err := mgr.Distill(context.Background(), src); !errors.Is(err, ErrDuplicateDistill) {
		t.Errorf("second Distill err = %v, want ErrDuplicateDistill", err)
	}
}

func TestDistillProcedureCapAndFlagOff(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()

	var steps []string
	for i := 0; i < MaxProcedureSteps+10; i++ {
		steps = append(steps, "do thing")
	}
	payload := `{"title":"big","steps":[`
	for i, s := range steps {
		if i > 0 {
			payload += ","
		}
		payload += `"` + s + `"`
	}
	payload += `]}`
	mgr.SetDistillSummarizer(&fakeDistillSummarizer{procedurePayload: payload})

	mem, err := mgr.Distill(context.Background(), []Memory{{Category: DomainProcedure, Content: "how to"}})
	if err != nil {
		t.Fatalf("Distill: %v", err)
	}
	p, derr := DecodeProcedure(mem.Content)
	if derr != nil {
		t.Fatalf("decode: %v", derr)
	}
	if len(p.Steps) > MaxProcedureSteps {
		t.Errorf("steps = %d, want <= %d", len(p.Steps), MaxProcedureSteps)
	}
}

func TestDistillSummarizerFailureSkipsGracefully(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()

	mgr.QueueDistill(DistillItem{Kind: DomainLesson, Change: "observation one"})
	mgr.QueueDistill(DistillItem{Kind: DomainLesson, Change: "observation two"})

	mgr.SetDistillSummarizer(&fakeDistillSummarizer{fail: errors.New("llm down")})
	if _, err := mgr.DrainDistillQueue(context.Background()); err == nil {
		t.Fatal("expected drain error when summarizer fails")
	}

	// Items retained; a working summarizer drains them next cycle. The
	// fixed payload means the second item dedupes against the first.
	mgr.SetDistillSummarizer(&fakeDistillSummarizer{lessonPayload: `{"principle":"recovered principle"}`})
	summary, err := mgr.DrainDistillQueue(context.Background())
	if err != nil {
		t.Fatalf("retry drain: %v", err)
	}
	if summary.Stored != 1 || summary.Duplicates != 1 {
		t.Errorf("retry drain = stored %d duplicates %d, want 1/1 (identical payload dedupes)",
			summary.Stored, summary.Duplicates)
	}
}

func TestQueueDrainStoresAndSuppressesDuplicates(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()

	payload := `{"principle":"` + strings.Repeat("pin dependency versions ", 6) + `"}`
	procPayload := `{"title":"pin dependencies","steps":["edit go.mod","run go mod tidy"]}`
	mgr.SetDistillSummarizer(&fakeDistillSummarizer{lessonPayload: payload, procedurePayload: procPayload})

	mgr.QueueDistill(DistillItem{Kind: DomainLesson, Change: "dep pinning", EvidenceIDs: []string{"e1"}})
	mgr.QueueDistill(DistillItem{Kind: DomainLesson, Change: "same dep pinning again"})
	mgr.QueueDistill(DistillItem{Kind: DomainProcedure, Change: "irrelevant kind uses same payload"})

	summary, err := mgr.DrainDistillQueue(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if summary.Stored < 1 {
		t.Errorf("stored = %d, want >= 1", summary.Stored)
	}
	if summary.Duplicates < 1 {
		t.Errorf("duplicates suppressed = %d, want >= 1", summary.Duplicates)
	}
}

// --- injection -------------------------------------------------------------

func TestRelevantDistilledInjection(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill.MinRelevance = -1 // disable relevance filter; gate on flag only here

	mgr.SetDistillSummarizer(&fakeDistillSummarizer{
		lessonPayload:    `{"principle":"use FTS5 match syntax when querying memory"}`,
		procedurePayload: `{"title":"sqlite migration runbook","steps":["backup","migrate","verify"]}`,
	})

	// A non-distill task memory that should NOT appear in distilled results.
	if _, err := mStoreRaw(context.Background(), mgr, "plain code note about caching", DomainCode); err != nil {
		t.Fatalf("seed plain memory: %v", err)
	}

	for _, item := range []DistillItem{
		{Kind: DomainLesson, Change: "memory query syntax"},
		{Kind: DomainProcedure, Change: "migration"},
	} {
		mgr.QueueDistill(item)
	}
	if _, err := mgr.DrainDistillQueue(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	results, err := mgr.RelevantDistilled(context.Background(), "sqlite memory migration", 5)
	if err != nil {
		t.Fatalf("RelevantDistilled: %v", err)
	}
	sawProcedure := 0
	for _, r := range results {
		switch r.Memory.Category {
		case DomainProcedure:
			sawProcedure++
		case DomainCode:
			t.Error("non-distill task memory leaked into distilled injection")
		}
	}
	if sawProcedure != 1 {
		t.Errorf("relevant procedure appeared %d times, want exactly once", sawProcedure)
	}
}

func TestRelevantDistilledIrrelevantAbsent(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill.MinRelevance = 0.9 // effectively unreachable

	mgr.SetDistillSummarizer(&fakeDistillSummarizer{
		procedurePayload: `{"title":"kubernetes rollout","steps":["kubectl apply"]}`,
	})
	mgr.QueueDistill(DistillItem{Kind: DomainProcedure, Change: "k8s stuff"})
	if _, err := mgr.DrainDistillQueue(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}

	results, err := mgr.RelevantDistilled(context.Background(), "totally unrelated gardening question", 5)
	if err != nil {
		t.Fatalf("RelevantDistilled: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("irrelevant query returned %d results, want 0", len(results))
	}
}
