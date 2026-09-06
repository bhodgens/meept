package skills

import (
	"strings"
	"testing"
)

// regressionKeywordEntry mirrors the shape of an installed skill whose name
// embeds codebase-ambient words (the real-world case was
// agent-daemon-benchmarking misrouting meept-bench prompts at skill@0.71-0.74).
var regressionKeywordEntry = &SkillIndexEntry{
	Name:        "agent-daemon-benchmarking",
	Description: "Build and debug benchmark/eval harnesses that drive an AI agent daemon",
	Tags:        []string{"benchmarking", "agents"},
	Examples:    []string{"benchmark the agent daemon"},
}

// TestExtractFromName_DropsCodebaseAmbientTokens pins the fix for skill-name
// token matches dispatching: the full skill name is kept as a keyword (so an
// explicit mention still routes), but its generic component tokens must be
// dropped — they are saturated across this codebase's vocabulary and match
// almost any prompt prose.
func TestExtractFromName_DropsCodebaseAmbientTokens(t *testing.T) {
	ke := NewKeywordExtractor()

	got := ke.extractFromName(regressionKeywordEntry.Name)

	nameLower := strings.ToLower(regressionKeywordEntry.Name)
	hasFullName := false
	for _, kw := range got {
		if kw == nameLower {
			hasFullName = true
		}
	}
	if !hasFullName {
		t.Errorf("extractFromName(%q) = %v, want full name %q kept", regressionKeywordEntry.Name, got, nameLower)
	}

	for _, generic := range []string{"agent", "daemon", "benchmarking", "benchmark", "bench"} {
		for _, kw := range got {
			if kw == generic {
				t.Errorf("extractFromName(%q) produced generic token %q; want dropped", regressionKeywordEntry.Name, generic)
			}
		}
	}
}

// TestExtractFromEntry_NoAmbientTokens pins the same rule across the whole
// ExtractFromEntry pipeline (name + tags + examples + description): the
// extractor must not emit agent/daemon/skill/benchmark/meept tokens from ANY
// source, since capabilities_builder feeds these straight into dispatch-time
// keyword tables.
func TestExtractFromEntry_NoAmbientTokens(t *testing.T) {
	ke := NewKeywordExtractor()

	ambient := map[string]bool{
		"agent": true, "agents": true,
		"daemon": true, "daemons": true,
		"skill": true, "skills": true,
		"benchmark": true, "benchmarks": true,
		"bench": true, "benches": true,
		"meept": true,
	}

	for _, kw := range ke.ExtractFromEntry(regressionKeywordEntry) {
		if ambient[kw.Keyword] {
			t.Errorf("ExtractFromEntry emitted codebase-ambient keyword %q (source %s)", kw.Keyword, kw.Source)
		}
	}
}

// TestExtractFromName_KeepsDistinctiveTokens guards against over-stopping: a
// skill whose name is genuinely distinctive must keep its meaningful tokens.
func TestExtractFromName_KeepsDistinctiveTokens(t *testing.T) {
	ke := NewKeywordExtractor()

	got := ke.extractFromName("clickhouse-ingest-dedup")
	joined := strings.Join(got, " ")
	for _, want := range []string{"clickhouse-ingest-dedup", "clickhouse", "ingest", "dedup"} {
		if !strings.Contains(joined, want) {
			t.Errorf("extractFromName missing distinctive keyword %q; got %v", want, got)
		}
	}
}

// TestStopWordSet_ContainsAmbientNouns pins that StopWordSet (the shared
// source of truth also consumed by internal/agent's domain gate) includes the
// ambient nouns, so the two consumers cannot drift apart.
func TestStopWordSet_ContainsAmbientNouns(t *testing.T) {
	stop := StopWordSet()
	for _, w := range []string{"agent", "daemon", "skill", "benchmark", "bench", "meept"} {
		if !stop[w] {
			t.Errorf("StopWordSet() missing %q", w)
		}
	}
}

// TestExtractFromName_DropsStopwordWholeName closes the sibling gap of the
// component-token fix: a skill NAMED a stopword ("bench", "agent") must not
// emit the bare word as a full-name keyword — it saturates every prompt and
// preempts the LLM classifier at the dispatcher's 0.7 gate. Distinctive
// multi-word names are unaffected (see KeepsDistinctiveTokens).
func TestExtractFromName_DropsStopwordWholeName(t *testing.T) {
	ke := NewKeywordExtractor()

	for _, name := range []string{"bench", "agent", "files", "writing", "users"} {
		got := ke.extractFromName(name)
		for _, kw := range got {
			if kw == name {
				t.Errorf("extractFromName(%q) emitted the stopword name itself as a keyword: %v", name, got)
			}
		}
	}
}
