package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSkillIndexEntry_HasCapability(t *testing.T) {
	entry := &SkillIndexEntry{
		Name:     "test-skill",
		Requires: []string{"code", "reasoning"},
	}

	tests := []struct {
		cap  string
		want bool
	}{
		{"code", true},
		{"Code", true}, // case insensitive
		{"reasoning", true},
		{"tool_use", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.cap, func(t *testing.T) {
			if got := entry.HasCapability(tt.cap); got != tt.want {
				t.Errorf("HasCapability(%q) = %v, want %v", tt.cap, got, tt.want)
			}
		})
	}
}

func TestSkillIndexEntry_HasTag(t *testing.T) {
	entry := &SkillIndexEntry{
		Name: "test-skill",
		Tags: []string{"coding", "automation"},
	}

	tests := []struct {
		tag  string
		want bool
	}{
		{"coding", true},
		{"Coding", true}, // case insensitive
		{"automation", true},
		{"testing", false},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := entry.HasTag(tt.tag); got != tt.want {
				t.Errorf("HasTag(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestSkillIndex_Index(t *testing.T) {
	idx := NewSkillIndex()

	entry := &SkillIndexEntry{
		Name:        "test-skill",
		Description: "A test skill",
		Requires:    []string{"code"},
		Tags:        []string{"testing"},
		Path:        "/path/to/skill.md",
		Priority:    0,
	}

	idx.Index(entry)

	if idx.Count() != 1 {
		t.Errorf("Count() = %d, want 1", idx.Count())
	}

	got := idx.Get("test-skill")
	if got == nil {
		t.Fatal("Get(test-skill) returned nil")
	}
	if got.Name != "test-skill" {
		t.Errorf("Get(test-skill).Name = %q, want %q", got.Name, "test-skill")
	}
}

func TestSkillIndex_Get_CaseInsensitive(t *testing.T) {
	idx := NewSkillIndex()

	idx.Index(&SkillIndexEntry{
		Name: "Test-Skill",
		Path: "/path/to/skill.md",
	})

	tests := []string{
		"Test-Skill",
		"test-skill",
		"TEST-SKILL",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			got := idx.Get(name)
			if got == nil {
				t.Errorf("Get(%q) returned nil", name)
			}
		})
	}
}

func TestSkillIndex_IndexAll(t *testing.T) {
	idx := NewSkillIndex()

	entries := []*SkillIndexEntry{
		{Name: "skill-1", Path: "/path/1.md"},
		{Name: "skill-2", Path: "/path/2.md"},
		{Name: "skill-3", Path: "/path/3.md"},
	}

	idx.IndexAll(entries)

	if idx.Count() != 3 {
		t.Errorf("Count() = %d, want 3", idx.Count())
	}
}

func TestSkillIndex_FindByTag(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Tags: []string{"coding", "automation"}},
		{Name: "skill-2", Tags: []string{"coding"}},
		{Name: "skill-3", Tags: []string{"testing"}},
	})

	// Find by coding tag
	coding := idx.FindByTag("coding")
	if len(coding) != 2 {
		t.Errorf("FindByTag(coding) returned %d entries, want 2", len(coding))
	}

	// Find by testing tag
	testingSkills := idx.FindByTag("testing")
	if len(testingSkills) != 1 {
		t.Errorf("FindByTag(testing) returned %d entries, want 1", len(testingSkills))
	}

	// Find non-existent tag
	none := idx.FindByTag("nonexistent")
	if len(none) != 0 {
		t.Errorf("FindByTag(nonexistent) returned %d entries, want 0", len(none))
	}
}

func TestSkillIndex_FindByCapability(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Requires: []string{"code", "reasoning"}},
		{Name: "skill-2", Requires: []string{"code"}},
		{Name: "skill-3", Requires: []string{"tool_use"}},
	})

	// Find by code capability
	code := idx.FindByCapability("code")
	if len(code) != 2 {
		t.Errorf("FindByCapability(code) returned %d entries, want 2", len(code))
	}

	// Find by reasoning capability
	reasoning := idx.FindByCapability("reasoning")
	if len(reasoning) != 1 {
		t.Errorf("FindByCapability(reasoning) returned %d entries, want 1", len(reasoning))
	}
}

func TestSkillIndex_FindByCapabilities(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Requires: []string{"code", "reasoning"}},
		{Name: "skill-2", Requires: []string{"code"}},
		{Name: "skill-3", Requires: []string{}}, // no requirements
	})

	// Find entries satisfiable by ["code"]
	codeOnly := idx.FindByCapabilities([]string{"code"})
	if len(codeOnly) != 2 { // skill-2 and skill-3 (no requirements)
		t.Errorf("FindByCapabilities([code]) returned %d entries, want 2", len(codeOnly))
	}

	// Find entries satisfiable by ["code", "reasoning"]
	both := idx.FindByCapabilities([]string{"code", "reasoning"})
	if len(both) != 3 { // all three
		t.Errorf("FindByCapabilities([code,reasoning]) returned %d entries, want 3", len(both))
	}
}

func TestSkillIndex_Match(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "code-review", Description: "Review code for quality"},
		{Name: "code-format", Description: "Format code files"},
		{Name: "test-runner", Description: "Run tests"},
	})

	tests := []struct {
		query    string
		wantName string
	}{
		{"code-review", "code-review"}, // exact match
		{"code", "code-review"},        // prefix match (or highest scoring)
		{"review", "code-review"},      // contains match
		{"format code", "code-format"}, // word match
		{"nonexistent", ""},            // no match
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := idx.Match(tt.query)
			if tt.wantName == "" {
				if got != nil {
					t.Errorf("Match(%q) = %v, want nil", tt.query, got.Name)
				}
			} else {
				if got == nil || got.Name != tt.wantName {
					name := ""
					if got != nil {
						name = got.Name
					}
					t.Errorf("Match(%q) = %v, want %v", tt.query, name, tt.wantName)
				}
			}
		})
	}
}

func TestSkillIndex_Clear(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "skill-1", Tags: []string{"test"}},
		{Name: "skill-2", Requires: []string{"code"}},
	})

	if idx.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", idx.Count())
	}

	idx.Clear()

	if idx.Count() != 0 {
		t.Errorf("Count() after Clear() = %d, want 0", idx.Count())
	}

	if len(idx.AllTags()) != 0 {
		t.Errorf("AllTags() after Clear() = %v, want empty", idx.AllTags())
	}

	if len(idx.AllCapabilities()) != 0 {
		t.Errorf("AllCapabilities() after Clear() = %v, want empty", idx.AllCapabilities())
	}
}

func TestSkillIndex_Names(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "charlie"},
		{Name: "alpha"},
		{Name: "bravo"},
	})

	names := idx.Names()
	if len(names) != 3 {
		t.Fatalf("Names() returned %d names, want 3", len(names))
	}

	// Should be sorted
	if names[0] != "alpha" || names[1] != "bravo" || names[2] != "charlie" {
		t.Errorf("Names() = %v, want [alpha bravo charlie]", names)
	}
}

func TestSkillIndex_MatchAll(t *testing.T) {
	idx := NewSkillIndex()

	idx.IndexAll([]*SkillIndexEntry{
		{Name: "code-review", Description: "Review code"},
		{Name: "code-format", Description: "Format code"},
		{Name: "test-runner", Description: "Run tests"},
	})

	matches := idx.MatchAll("code")
	if len(matches) != 2 {
		t.Errorf("MatchAll(code) returned %d matches, want 2", len(matches))
	}

	// Should be sorted by score
	if len(matches) >= 2 && matches[0].Score < matches[1].Score {
		t.Error("MatchAll results not sorted by score descending")
	}
}

// ---------------------------------------------------------------------------
// Embedding-rank seam (token-efficiency branch 04, leaf 02; Q4: always rank).
// Stubs give fixed vectors so ordering is exact-math predictable; no network.
// ---------------------------------------------------------------------------

// stubEmbedder returns a fixed vector per text and counts the texts it was
// asked to embed (per Embed call, batch order preserved). Unknown texts get
// a shared default vector, which makes them cosine-tied.
type stubEmbedder struct {
	vectors map[string][]float32
	calls   [][]string
	err     error
}

func (s *stubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	s.calls = append(s.calls, append([]string(nil), texts...))
	if s.err != nil {
		return nil, s.err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := s.vectors[t]; ok {
			out[i] = v
			continue
		}
		out[i] = []float32{0, 0} // unknown text: zero-norm -> cosine 0
	}
	return out, nil
}

func (s *stubEmbedder) totalEmbeds() int {
	n := 0
	for _, c := range s.calls {
		n += len(c)
	}
	return n
}

func skillText(name, desc string) string { return name + "\n" + desc }

// TestEmbedCacheReusesUntilFrontmatterChange pins T1: a skill's embedding is
// computed once; re-indexing the SAME name+description (e.g. a Refresh that
// sees no change) does not re-embed; changing the frontmatter text re-embeds.
func TestEmbedCacheReusesUntilFrontmatterChange(t *testing.T) {
	idx := NewSkillIndex()
	stub := &stubEmbedder{vectors: map[string][]float32{
		skillText("alpha", "Alpha skill"):   {1, 0},
		skillText("alpha", "Alpha changed"): {0, 1},
		"task one":                          {1, 0},
		"task two":                          {1, 0},
	}}
	idx.SetEmbedder(stub)
	idx.Index(&SkillIndexEntry{Name: "alpha", Description: "Alpha skill"})

	if got := idx.RankedNames("task one", []string{"alpha"}); len(got) != 1 {
		t.Fatalf("first rank: got %v", got)
	}
	before := stub.totalEmbeds() // 2: one skill vector + one task vector

	// Re-index byte-identical frontmatter, then rank with a new task: only
	// the task text may be embedded, never the unchanged skill.
	idx.Index(&SkillIndexEntry{Name: "alpha", Description: "Alpha skill"})
	idx.RankedNames("task two", []string{"alpha"})
	after := stub.totalEmbeds()
	if got := after - before; got != 1 {
		t.Errorf("unchanged re-index re-embedded %d texts, want 1 (task only)", got)
	}

	// Changed frontmatter -> the skill re-embeds exactly once.
	idx.Index(&SkillIndexEntry{Name: "alpha", Description: "Alpha changed"})
	idx.RankedNames("task two", []string{"alpha"})
	afterChange := stub.totalEmbeds()
	if got := afterChange - after; got != 2 {
		t.Errorf("changed re-index embedded %d texts, want 2 (skill + task)", got)
	}
}

// TestRankedNamesOrdersByCosine pins T2: exact cosine order from fixed
// stub vectors, complete set preserved (permutation, never a subset).
func TestRankedNamesOrdersByCosine(t *testing.T) {
	idx := NewSkillIndex()
	stub := &stubEmbedder{vectors: map[string][]float32{
		skillText("alpha", "Alpha skill"):     {0, 1}, // cos 0 vs task
		skillText("bravo", "Bravo skill"):     {1, 1}, // cos ~0.707
		skillText("charlie", "Charlie skill"): {1, 0}, // cos 1
		"deploy":                              {1, 0},
	}}
	idx.SetEmbedder(stub)
	idx.IndexAll([]*SkillIndexEntry{
		{Name: "alpha", Description: "Alpha skill"},
		{Name: "bravo", Description: "Bravo skill"},
		{Name: "charlie", Description: "Charlie skill"},
	})

	got := idx.RankedNames("deploy", []string{"alpha", "bravo", "charlie"})
	want := []string{"charlie", "bravo", "alpha"}
	if len(got) != len(want) {
		t.Fatalf("ranked set shrank: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cosine order: got %v want %v", got, want)
		}
	}
}

// TestRankedNamesTiesByAscendingName pins T2 determinism: equal cosine
// scores resolve by name ascending, never by map iteration order.
func TestRankedNamesTiesByAscendingName(t *testing.T) {
	idx := NewSkillIndex()
	same := []float32{1, 0}
	stub := &stubEmbedder{vectors: map[string][]float32{
		skillText("zulu", "Zulu skill"):   same,
		skillText("mike", "Mike skill"):   same,
		skillText("alpha", "Alpha skill"): same,
		"task":                            same,
	}}
	idx.SetEmbedder(stub)
	idx.IndexAll([]*SkillIndexEntry{
		{Name: "zulu", Description: "Zulu skill"},
		{Name: "mike", Description: "Mike skill"},
		{Name: "alpha", Description: "Alpha skill"},
	})

	want := []string{"alpha", "mike", "zulu"}
	for round := 0; round < 10; round++ {
		got := idx.RankedNames("task", []string{"zulu", "mike", "alpha"})
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("tie order not deterministic (round %d): got %v want %v", round, got, want)
		}
	}
}

// TestRankedNamesPreservesSet pins the order-only contract: fallback names
// with no vector (unknown to the index) are kept — sunk to the tail — never
// dropped; the output is a permutation of the input (minus duplicates).
func TestRankedNamesPreservesSet(t *testing.T) {
	idx := NewSkillIndex()
	stub := &stubEmbedder{vectors: map[string][]float32{
		skillText("known", "Known skill"): {1, 0},
		"task":                            {1, 0},
	}}
	idx.SetEmbedder(stub)
	idx.Index(&SkillIndexEntry{Name: "known", Description: "Known skill"})

	got := idx.RankedNames("task", []string{"ghost", "known"})
	if len(got) != 2 {
		t.Fatalf("set shrank: got %v, want 2 entries", got)
	}
	if got[0] != "known" || got[1] != "ghost" {
		t.Errorf("expected known (vector) before ghost (no vector): got %v", got)
	}
}

// TestRankedNamesFallbackNilEmbedder pins T4: with no embedder installed,
// RankedNames returns the fallback order untouched.
func TestRankedNamesFallbackNilEmbedder(t *testing.T) {
	idx := NewSkillIndex()
	idx.Index(&SkillIndexEntry{Name: "alpha", Description: "Alpha skill"})
	fallback := []string{"bravo", "alpha"}
	got := idx.RankedNames("task", fallback)
	if strings.Join(got, ",") != strings.Join(fallback, ",") {
		t.Errorf("nil embedder must keep fallback order: got %v want %v", got, fallback)
	}
}

// TestRankedNamesFallbackEmbedderError pins T4: an erroring embedder
// degrades to fallback order silently — no episode failure, no cap.
func TestRankedNamesFallbackEmbedderError(t *testing.T) {
	idx := NewSkillIndex()
	idx.SetEmbedder(&stubEmbedder{err: errors.New("embed endpoint down")})
	idx.Index(&SkillIndexEntry{Name: "alpha", Description: "Alpha skill"})

	fallback := []string{"bravo", "alpha"}
	got := idx.RankedNames("task", fallback)
	if strings.Join(got, ",") != strings.Join(fallback, ",") {
		t.Errorf("embedder error must keep fallback order: got %v want %v", got, fallback)
	}
}

// TestRankedNamesFallbackEmptyTask pins degradation on missing signal:
// empty/whitespace task text returns fallback order and embeds nothing.
func TestRankedNamesFallbackEmptyTask(t *testing.T) {
	idx := NewSkillIndex()
	stub := &stubEmbedder{}
	idx.SetEmbedder(stub)
	idx.Index(&SkillIndexEntry{Name: "alpha", Description: "Alpha skill"})

	got := idx.RankedNames("   ", []string{"alpha"})
	if strings.Join(got, ",") != "alpha" {
		t.Errorf("empty task must keep fallback order: got %v", got)
	}
	if stub.totalEmbeds() != 0 {
		t.Errorf("empty task must not embed, got %d texts", stub.totalEmbeds())
	}
}
