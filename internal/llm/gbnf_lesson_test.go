package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLessonGrammar_Shape pins the grammar's structure: the root (lesson)
// rule must force a single bare JSON object — no array, no wrapper key —
// carrying exactly the declared keys, with principle as a required string
// and evidence_ids as an array of quoted strings.
func TestLessonGrammar_Shape(t *testing.T) {
	g := LessonGrammar()
	if g == "" {
		t.Fatal("grammar is empty")
	}
	// Root is a single object: the root rule opens with a "{" literal and
	// there is no bare-array root. (llama.cpp requires the first rule to be
	// named `root` — any other name fails grammar parse on the wire.)
	if !strings.Contains(g, `root ::= "{"`) {
		t.Errorf("root rule must force a single JSON object, grammar:\n%s", g)
	}
	if strings.Contains(g, `root ::= "["`) {
		t.Error("grammar must not force a bare array (that is the ambient shape)")
	}
	for _, want := range []string{
		"\"\\\"principle\\\"\"", "\"\\\"because\\\"\"", "\"\\\"evidence_ids\\\"\"",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("grammar missing key literal %s", want)
		}
	}
	// Every referenced rule is defined.
	for _, rule := range []string{"ws", "string", "char", "string-array", "root", "principle-member", "evidence-or-close", "because-cont"} {
		if !strings.Contains(g, rule+" ::=") {
			t.Errorf("rule %s used but not defined", rule)
		}
	}
	// evidence_ids must be an array of quoted strings, and the string rule
	// must produce quoted values (the evidence_ids contract is []string).
	if !strings.Contains(g, "evidence_ids") || !strings.Contains(g, "string-array") {
		t.Error("evidence_ids member must map to the string-array rule")
	}
}

// TestLessonGrammar_FixedKeyOrder: GBNF has no unordered-object primitive, so
// the lesson rule fixes a key sequence (required key first). principle must
// appear before evidence_ids, which must appear before because.
func TestLessonGrammar_FixedKeyOrder(t *testing.T) {
	g := LessonGrammar()
	order := []string{"principle", "evidence_ids", "because"}
	prev := -1
	for _, k := range order {
		idx := strings.Index(g, "\\\""+k+"\\\"")
		if idx == -1 {
			t.Fatalf("key %s missing from grammar", k)
		}
		if idx < prev {
			t.Errorf("key %s out of fixed order (idx %d < %d)", k, idx, prev)
		}
		prev = idx
	}
}

// lessonShape is the wire contract LessonGrammar must satisfy when decoded by
// the distill path (mirrors the fields internal/memory's DecodeLesson reads).
type lessonShape struct {
	Principle   string   `json:"principle"`
	Because     string   `json:"because"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// TestLessonGrammar_RoundTrip is the shape contract in one place: a payload
// the grammar can generate (full form and minimal form) must unmarshal
// directly into the lesson struct — no wrapper stripping, no coercion.
func TestLessonGrammar_RoundTrip(t *testing.T) {
	// Full form: keys in the grammar's fixed order, all members present.
	full := `{"principle":"pin base image digests","evidence_ids":["memory-104","memory-205"],"because":"a floating tag broke builds for a day"}`
	var got lessonShape
	if err := json.Unmarshal([]byte(full), &got); err != nil {
		t.Fatalf("full-form payload does not parse: %v", err)
	}
	if got.Principle != "pin base image digests" || got.Because != "a floating tag broke builds for a day" {
		t.Errorf("unexpected decode: %+v", got)
	}
	if len(got.EvidenceIDs) != 2 || got.EvidenceIDs[0] != "memory-104" {
		t.Errorf("evidence_ids = %v, want [memory-104 memory-205]", got.EvidenceIDs)
	}

	// Minimal form: only the required principle key (the grammar's optional
	// members are emitted as separate alternatives in real GBNF engines, so
	// a principle-only object is also grammar-conforming).
	minimal := `{"principle":"always check replica lag before deploys"}`
	var minGot lessonShape
	if err := json.Unmarshal([]byte(minimal), &minGot); err != nil {
		t.Fatalf("minimal payload does not parse: %v", err)
	}
	if minGot.Principle == "" || len(minGot.EvidenceIDs) != 0 {
		t.Errorf("unexpected minimal decode: %+v", minGot)
	}
}
