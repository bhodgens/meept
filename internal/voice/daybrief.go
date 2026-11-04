package voice

// Day brief (contract C7, master.md): the per-call context block giving a
// call today's prior activity, prior call transcripts for the number, the
// capped durable-memory block, and active goal titles. One calendar
// convention: the activity window starts at LOCAL midnight in the caller's
// location, never a UTC-derived day key. memory_search has no time filter
// and is deliberately not used here.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/caimlas/meept/internal/activity"
)

// Caps for the day brief (C7).
const (
	// ActivityLimit caps activity rows per brief (query Limit).
	ActivityLimit = 20
	// maxSummaryRunes re-clamps each summary defensively at output time;
	// the activity package already envelope-strips to this bound.
	maxSummaryRunes = 200
	// maxBlockRunes caps the whole PromptBlock.
	maxBlockRunes = 2000
)

// CallTranscript is one prior call with the same number, earlier today.
type CallTranscript struct {
	Number  string
	At      time.Time
	Summary string
}

// ActivitySource is the narrow activity read seam. It decouples the voice
// package from the full aggregator surface (whose Query also returns a
// total count); wiring adapts *activity.Aggregator via AggregatorSource.
type ActivitySource interface {
	Query(ctx context.Context, since, until time.Time, limit int) ([]activity.ActivityRecord, error)
}

// CallTranscriptSource lists prior call transcripts for a number (today's
// by leaf-12 store convention). Narrow interface: the store lands in leaf
// 12; this package stays compilable standalone.
type CallTranscriptSource interface {
	TodayTranscripts(ctx context.Context, number string) ([]CallTranscript, error)
}

// MemorySource returns the pre-capped durable memory block (fixed keys:
// name, preferences, standing rules). Narrow interface so voice never
// imports internal/memory.
type MemorySource interface {
	DurableBlock(ctx context.Context) (string, error)
}

// GoalSource returns active goal titles for an employee. Narrow interface
// wrapping the employee GoalStore ListActive shape: voice never imports
// internal/employee (it imports agent); wiring injects the adapter.
type GoalSource interface {
	ActiveGoalTitles(ctx context.Context, employeeID string) ([]string, error)
}

// Deps carries the day brief's sources. A nil source (including a typed
// nil) omits its section, never an error.
type Deps struct {
	Activity    ActivitySource
	Transcripts CallTranscriptSource
	Memory      MemorySource
	Goals       GoalSource
}

// DayBrief is one call's today-so-far context (C7).
type DayBrief struct {
	Activity             []activity.ActivityRecord
	PriorCallTranscripts []CallTranscript
	DurableMemory        string
	ActiveGoals          []string
}

// AggregatorSource adapts the real *activity.Aggregator (3-value Query
// with a struct argument) onto the narrow 2-value ActivitySource seam.
type AggregatorSource struct {
	Agg *activity.Aggregator
}

// Query runs the aggregator with a plain since/until/limit window.
func (s AggregatorSource) Query(ctx context.Context, since, until time.Time, limit int) ([]activity.ActivityRecord, error) {
	if s.Agg == nil {
		return nil, nil
	}
	records, _, err := s.Agg.Query(ctx, activity.Query{Since: since, Until: until, Limit: limit})
	return records, err
}

// isNilSource guards both untyped nil interfaces and typed nil pointers
// stored in an interface (repo typed-nil rule): either means "section
// omitted", never an error and never a panic.
func isNilSource(src any) bool {
	if src == nil {
		return true
	}
	v := reflect.ValueOf(src)
	switch v.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

// Build assembles the day brief. The activity window is [local midnight of
// now in loc, now]; rows are capped at ActivityLimit. Source failures do
// not abort the whole brief: each failing section is dropped and the
// errors are joined into the returned error.
func Build(ctx context.Context, deps Deps, employeeID, number string, now time.Time, loc *time.Location) (DayBrief, error) {
	var (
		brief DayBrief
		errs  []error
	)
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	if !isNilSource(deps.Activity) {
		rows, err := deps.Activity.Query(ctx, midnight, now, ActivityLimit)
		if err != nil {
			errs = append(errs, err)
		} else {
			for i, r := range rows {
				r.Summary = clampRunes(r.Summary, maxSummaryRunes)
				rows[i] = r
			}
			brief.Activity = rows
		}
	}

	if !isNilSource(deps.Transcripts) {
		list, err := deps.Transcripts.TodayTranscripts(ctx, number)
		if err != nil {
			errs = append(errs, err)
		} else {
			brief.PriorCallTranscripts = list
		}
	}

	if !isNilSource(deps.Memory) {
		block, err := deps.Memory.DurableBlock(ctx)
		if err != nil {
			errs = append(errs, err)
		} else {
			brief.DurableMemory = clampRunes(block, maxBlockRunes/4)
		}
	}

	if !isNilSource(deps.Goals) {
		titles, err := deps.Goals.ActiveGoalTitles(ctx, employeeID)
		if err != nil {
			errs = append(errs, err)
		} else {
			brief.ActiveGoals = titles
		}
	}

	return brief, errors.Join(errs...)
}

// PromptBlock renders the brief as labeled fixed-order sections, at most
// maxBlockRunes runes total, each summary re-clamped to maxSummaryRunes.
// Empty sections are omitted.
func (b DayBrief) PromptBlock() string {
	var sb strings.Builder
	writeSection := func(header string, lines []string) {
		if len(lines) == 0 {
			return
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(header)
		sb.WriteByte('\n')
		for _, line := range lines {
			if line == "" {
				continue
			}
			sb.WriteString("- ")
			sb.WriteString(clampRunes(line, maxSummaryRunes))
			sb.WriteByte('\n')
		}
	}

	activityLines := make([]string, 0, len(b.Activity))
	for _, r := range b.Activity {
		activityLines = append(activityLines, r.Summary)
	}
	writeSection("[activity today]", activityLines)

	callLines := make([]string, 0, len(b.PriorCallTranscripts))
	for _, c := range b.PriorCallTranscripts {
		callLines = append(callLines, c.At.Format("15:04")+" "+c.Number+": "+c.Summary)
	}
	writeSection("[prior calls today]", callLines)

	if b.DurableMemory != "" {
		var mem []string
		for _, line := range strings.Split(strings.TrimRight(b.DurableMemory, "\n"), "\n") {
			mem = append(mem, line)
		}
		writeSection("[durable memory]", mem)
	}

	writeSection("[active goals]", b.ActiveGoals)

	return clampRunes(strings.TrimRight(sb.String(), "\n"), maxBlockRunes)
}

// clampRunes truncates s to at most n runes without splitting one.
func clampRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}
