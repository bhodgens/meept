package voice

// Gate tests for contract C7 (master.md, quoted verbatim): the day brief
// queries activity since LOCAL midnight (one calendar convention, never
// UTC-derived), caps summaries at 200 runes each, and PromptBlock at 2000
// runes total. Nil sources omit sections, never error.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/activity"
)

// cannedAggregator satisfies the day brief's activity query seam with canned
// rows and captures the query window for the local-midnight assertion. The
// seam shape is the activity aggregator read leaf 04 consumes; the fake
// satisfies it structurally.
type cannedAggregator struct {
	mu    sync.Mutex
	rows  []activity.ActivityRecord
	since time.Time
	until time.Time
	loc   *time.Location
}

func (c *cannedAggregator) Query(_ context.Context, since, until time.Time, _ int) ([]activity.ActivityRecord, error) {
	c.mu.Lock()
	c.since, c.until = since, until
	c.mu.Unlock()
	var out []activity.ActivityRecord
	for _, r := range c.rows {
		if !r.At.Before(since) && !r.At.After(until) {
			out = append(out, r)
		}
	}
	return out, nil
}

type cannedMemory struct{ block string }

func (m *cannedMemory) DurableBlock(_ context.Context) (string, error) { return m.block, nil }

type cannedGoals struct{ titles []string }

func (g *cannedGoals) ActiveGoalTitles(_ context.Context, _ string) ([]string, error) {
	return g.titles, nil
}

type cannedTranscripts struct{ list []CallTranscript }

func (s *cannedTranscripts) TodayTranscripts(_ context.Context, _ string) ([]CallTranscript, error) {
	return s.list, nil
}

// TestDayBriefLocalDay is the TZ differential (repo calendar-convention
// defect class): the SAME scenario runs under TZ=UTC and the host zone in
// ONE test via t.Setenv, and in both cases the query window must start at
// LOCAL midnight. An activity row at 23:30 local YESTERDAY is excluded when
// the brief is built at 00:30 local today.
func TestDayBriefLocalDay(t *testing.T) {
	zones := []string{"UTC", time.Now().Location().String()}
	for _, zone := range zones {
		t.Run("TZ="+zone, func(t *testing.T) {
			t.Setenv("TZ", zone)
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatalf("LoadLocation(%s): %v", zone, err)
			}

			// now = 00:30 local today; local midnight = 00:00 today local.
			nowLocal := time.Date(2026, 11, 2, 0, 30, 0, 0, loc)
			wantMidnight := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)

			yesterdayLate := time.Date(2026, 11, 1, 23, 30, 0, 0, loc)
			todayRow := time.Date(2026, 11, 2, 0, 10, 0, 0, loc)

			agg := &cannedAggregator{loc: loc, rows: []activity.ActivityRecord{
				{Kind: "session_message", At: yesterdayLate, Summary: "late yesterday row"},
				{Kind: "session_message", At: todayRow, Summary: "today row"},
			}}
			deps := Deps{Activity: agg}
			b, err := Build(context.Background(), deps, "emp-1", "+15551230001", nowLocal, loc)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			agg.mu.Lock()
			gotSince, gotUntil := agg.since, agg.until
			agg.mu.Unlock()

			if !gotSince.Equal(wantMidnight) {
				t.Errorf("query Since = %v, want local midnight %v (never UTC-derived)", gotSince, wantMidnight)
			}
			if gotUntil.Before(nowLocal.Add(-time.Second)) || gotUntil.After(nowLocal.Add(time.Second)) {
				t.Errorf("query Until = %v, want ~now %v", gotUntil, nowLocal)
			}
			for _, r := range b.Activity {
				if r.At.Before(wantMidnight) {
					t.Errorf("brief included pre-midnight row at %v (window must be local midnight..now)", r.At)
				}
			}
			found := false
			for _, r := range b.Activity {
				if r.Summary == "today row" {
					found = true
				}
			}
			if !found {
				t.Errorf("brief missing the 00:10 local row; window too narrow")
			}
		})
	}
}

// TestDayBriefCaps pins the C7 caps verbatim: every activity summary is
// capped at <=200 runes and PromptBlock() is <=2000 runes total.
func TestDayBriefCaps(t *testing.T) {
	huge := strings.Repeat("x", 5000)
	agg := &cannedAggregator{rows: []activity.ActivityRecord{
		{Kind: "task_step", At: time.Now(), Summary: huge},
		{Kind: "task_step", At: time.Now().Add(-time.Minute), Summary: huge},
	}}
	deps := Deps{
		Activity:    agg,
		Memory:      &cannedMemory{block: huge},
		Goals:       &cannedGoals{titles: []string{huge, "real goal"}},
		Transcripts: &cannedTranscripts{list: []CallTranscript{{Number: "+15551230001", At: time.Now(), Summary: huge}}},
	}
	b, err := Build(context.Background(), deps, "emp-1", "+15551230001", time.Now(), time.UTC)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i, r := range b.Activity {
		if n := utf8.RuneCountInString(r.Summary); n > 200 {
			t.Errorf("activity[%d] Summary = %d runes, want <= 200", i, n)
		}
	}
	block := b.PromptBlock()
	if n := utf8.RuneCountInString(block); n > 2000 {
		t.Errorf("PromptBlock = %d runes, want <= 2000", n)
	}
	if block == "" {
		t.Errorf("PromptBlock empty with non-empty sources")
	}
}

// TestDayBriefNilSources pins: a nil source omits its section, never an
// error (typed-nil guard).
func TestDayBriefNilSources(t *testing.T) {
	agg := &cannedAggregator{}
	b, err := Build(context.Background(), Deps{Activity: agg}, "emp-1", "+15551230001", time.Now(), time.UTC)
	if err != nil {
		t.Fatalf("Build with nil sources: %v", err)
	}
	_ = b.PromptBlock()
}
