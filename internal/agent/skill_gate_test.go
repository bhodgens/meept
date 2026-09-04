package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// TestDomainAgrees verifies the domain-agreement gate used by
// discoverRelevantSkills: at least one non-stopword domain token from the
// query must appear (case-insensitive substring) in the skill's name, tags,
// or description; queries with no domain tokens pass through unconditionally.
// Leaf: docs/plans/chat-dispatch-ux/07-skill-discovery-gate.md Task 1.
func TestDomainAgrees(t *testing.T) {
	linter := SkillEntryView{Name: "go-linter-save-hook-revert",
		Tags:        []string{"go", "linting", "hooks"},
		Description: "revert linter save hooks"}
	water := SkillEntryView{Name: "python-project-local-setup",
		Tags:        []string{"python", "venv"},
		Description: "set up python projects for local testing"}

	tests := []struct {
		name  string
		query string
		entry SkillEntryView
		want  bool
	}{
		{"water ask vs linter skill", "remind me to drink water every 30 minutes while working", linter, false},
		{"linter ask vs linter skill", "the linter save hook reverts my go changes", linter, true},
		{"python ask vs python skill", "set up my python project for testing", water, true},
		{"generic greeting passes through", "hello there", linter, true},
		{"stopword-only query passes", "fix this for me please", water, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := domainAgrees(tc.query, tc.entry); got != tc.want {
				t.Errorf("domainAgrees(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// gateFakeUsageTracker implements lifecycleUsageTracker, recording
// low-match queries so tests can assert the Pass D gap-analysis path.
type gateFakeUsageTracker struct {
	lowMatchQueries []string
}

func (f *gateFakeUsageTracker) RecordInjection(skillName string) error { return nil }

func (f *gateFakeUsageTracker) RecordOutcome(skillName string, outcome LifecycleOutcome, sessionID string) error {
	return nil
}

func (f *gateFakeUsageTracker) RecordLowMatchQuery(ctx context.Context, query string, bestScore float64) error {
	f.lowMatchQueries = append(f.lowMatchQueries, query)
	return nil
}

// createGateTestCapabilityIndex builds a real CapabilityIndex with the
// wrong-domain linter skill and, optionally, the water-reminder skill. The
// linter entry carries an example phrase matching the water query, so the
// keyword matcher (which sees examples) ranks it highly for the water ask —
// the O1 observation — while the domain gate (which only sees name, tags,
// and description) must reject it.
func createGateTestCapabilityIndex(withWater bool) *skills.CapabilityIndex {
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "go-linter-save-hook-revert",
		Description: "revert linter save hooks",
		Tags:        []string{"go", "linting", "hooks"},
		Examples:    []string{"remind me to drink water every 30 minutes while working"},
	})
	if withWater {
		idx.Index(&skills.SkillIndexEntry{
			Name:        "water-reminder",
			Description: "remind the user to drink water on a schedule",
			Tags:        []string{"reminders", "hydration", "water"},
		})
	}
	return skills.BuildCapabilityIndex(idx)
}

const gateWaterQuery = "remind me to drink water every 30 minutes while working"

// TestDiscoverRelevantSkillsDomainGateFiltersWrongDomain verifies Task 2:
// discoverRelevantSkills filters rank matches through the domain gate before
// returning, so a confidently-ranked wrong-domain skill is dropped while the
// agreeing skill survives.
func TestDiscoverRelevantSkillsDomainGateFiltersWrongDomain(t *testing.T) {
	idx := createGateTestCapabilityIndex(true)

	// Precondition: without the gate, the matcher returns BOTH skills above
	// threshold (linter ranked top via its example keywords). If this fails,
	// the fixture no longer reproduces the O1 mismatch.
	raw := idx.MatchWithThreshold(gateWaterQuery, 0.5, 3)
	if len(raw) != 2 {
		t.Fatalf("fixture: expected matcher to return 2 skills above threshold, got %d", len(raw))
	}
	if raw[0].Entry.Name != "go-linter-save-hook-revert" {
		t.Fatalf("fixture: expected linter skill ranked first, got %s", raw[0].Entry.Name)
	}

	loop := NewAgentLoop("test-session", "/tmp")
	loop.SetCapabilityIndex(idx)

	result := loop.discoverRelevantSkills(context.Background(), gateWaterQuery, 0.5)
	if len(result) != 1 {
		t.Fatalf("expected only the domain-agreeing skill to survive, got %d matches", len(result))
	}
	if result[0].Entry.Name != "water-reminder" {
		t.Errorf("expected water-reminder to survive the gate, got %s", result[0].Entry.Name)
	}
}

// TestDiscoverRelevantSkillsDomainGateAllFilteredRecordsLowMatch verifies
// that when every match fails the domain gate, discoverRelevantSkills
// returns empty AND records the query via the existing low-match path.
func TestDiscoverRelevantSkillsDomainGateAllFilteredRecordsLowMatch(t *testing.T) {
	idx := createGateTestCapabilityIndex(false)

	// Precondition: the matcher alone would return the linter skill.
	raw := idx.MatchWithThreshold(gateWaterQuery, 0.5, 3)
	if len(raw) != 1 {
		t.Fatalf("fixture: expected matcher to return 1 skill above threshold, got %d", len(raw))
	}

	tracker := &gateFakeUsageTracker{}
	loop := NewAgentLoop("test-session", "/tmp", WithUsageTracker(tracker))
	loop.SetCapabilityIndex(idx)

	result := loop.discoverRelevantSkills(context.Background(), gateWaterQuery, 0.5)
	if len(result) != 0 {
		t.Fatalf("expected empty result when every match fails the domain gate, got %d", len(result))
	}
	if len(tracker.lowMatchQueries) != 1 || tracker.lowMatchQueries[0] != gateWaterQuery {
		t.Errorf("expected low-match query recorded once, got %v", tracker.lowMatchQueries)
	}
}
