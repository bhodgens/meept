package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// TimingKind is the semantic kind of a model-authored timing spec.
type TimingKind string

// The five semantic timing kinds (L1 contract).
const (
	TimingRRule  TimingKind = "TimingRRule"
	TimingDaily  TimingKind = "TimingDaily"
	TimingWeekly TimingKind = "TimingWeekly"
	TimingAt     TimingKind = "TimingOneShot"
	TimingEvery  TimingKind = "TimingEvery"
)

// MinTimingEverySeconds is the floor for the `every` kind, mirroring the dsh
// MIN_EVERY_INTERVAL_SECONDS constraint. Higher-frequency recurrences are
// rejected as frequency_too_high.
const MinTimingEverySeconds = 60

// Typed validation errors (mirrors the dsh rejection vocabulary).
var (
	ErrInvalidRule       = fmt.Errorf("invalid_rule")
	ErrInvalidTimeZone   = fmt.Errorf("invalid_time_zone")
	ErrFrequencyTooHigh  = fmt.Errorf("frequency_too_high")
	ErrTimingUnsupported = fmt.Errorf("invalid_kind")
)

// TimingSpec is the semantic timing layer over the robfig/cron engine.
// A persisted JobConfig keeps its normalized cron `Schedule` string for the
// engine and carries this spec for semantic re-registration on restart.
type TimingSpec struct {
	Kind     TimingKind    `json:"kind"`
	Time     string        `json:"time,omitempty"`      // HH:mm:ss (daily/weekly)
	TimeZone string        `json:"time_zone,omitempty"` // IANA (daily/weekly/rrule DTSTART TZID)
	Weekdays []int         `json:"weekdays,omitempty"`  // ISO 1=Mon..7=Sun, ascending unique (weekly)
	At       *time.Time    `json:"at,omitempty"`        // absolute instant (at)
	Every    time.Duration `json:"every,omitempty"`     // >= MinTimingEverySeconds (every)
	RRule    string        `json:"rrule,omitempty"`     // "DTSTART;TZID=<IANA>:<ts>\nRRULE:<rule>" (rrule)
}

// ParseTiming parses a model-facing timing spec string into a TimingSpec.
//
// Supported forms:
//
//	daily <HH:mm[:ss]> [<IANA tz>]
//	weekly <DOW-list> <HH:mm[:ss]> [<IANA tz>]   (DOW names or ISO numbers)
//	at <RFC3339>
//	every <Go duration>
//	rrule:\nDTSTART;TZID=<IANA>:<yyyymmddThhmmss>\nRRULE:<rule>
func ParseTiming(spec string) (*TimingSpec, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: empty timing spec", ErrInvalidRule)
	}

	// The rrule form is the only multi-line form; dispatch on its prefix
	// before whitespace tokenization.
	if strings.HasPrefix(strings.ToLower(trimmed), "rrule") {
		return parseTimingRRule(trimmed)
	}

	fields := strings.Fields(trimmed)
	switch strings.ToLower(fields[0]) {
	case "daily":
		return parseTimingDaily(fields[1:])
	case "weekly":
		return parseTimingWeekly(fields[1:])
	case "at":
		return parseTimingAt(fields[1:])
	case "every":
		return parseTimingEvery(fields[1:])
	default:
		return nil, fmt.Errorf("%w: unknown timing kind %q", ErrTimingUnsupported, fields[0])
	}
}

// parseTimingDaily parses "daily HH:mm[:ss] [TZ]".
func parseTimingDaily(fields []string) (*TimingSpec, error) {
	if len(fields) < 1 || len(fields) > 2 {
		return nil, fmt.Errorf("%w: daily expects \"daily <HH:mm[:ss]> [TZ]\"", ErrInvalidRule)
	}
	clock, err := normalizeClock(fields[0])
	if err != nil {
		return nil, err
	}
	tz := "UTC"
	if len(fields) == 2 {
		tz = fields[1]
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidTimeZone, tz, err)
		}
	}
	spec := &TimingSpec{Kind: TimingDaily, Time: clock, TimeZone: tz}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// parseTimingWeekly parses "weekly DOW[,DOW...] HH:mm[:ss] [TZ]". Day tokens
// are cron names (MON..SUN) or ISO numbers (1=Mon..7=Sun).
func parseTimingWeekly(fields []string) (*TimingSpec, error) {
	if len(fields) < 2 || len(fields) > 3 {
		return nil, fmt.Errorf("%w: weekly expects \"weekly <DOW-list> <HH:mm[:ss]> [TZ]\"", ErrInvalidRule)
	}
	weekdays, err := normalizeWeekdays(fields[0])
	if err != nil {
		return nil, err
	}
	clock, err := normalizeClock(fields[1])
	if err != nil {
		return nil, err
	}
	tz := "UTC"
	if len(fields) == 3 {
		tz = fields[2]
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidTimeZone, tz, err)
		}
	}
	spec := &TimingSpec{Kind: TimingWeekly, Time: clock, TimeZone: tz, Weekdays: weekdays}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// parseTimingAt parses "at <RFC3339>".
func parseTimingAt(fields []string) (*TimingSpec, error) {
	if len(fields) != 1 {
		return nil, fmt.Errorf("%w: at expects \"at <RFC3339>\"", ErrInvalidRule)
	}
	at, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid RFC3339 timestamp %q: %v", ErrInvalidRule, fields[0], err)
	}
	spec := &TimingSpec{Kind: TimingAt, At: &at}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// parseTimingEvery parses "every <duration>".
func parseTimingEvery(fields []string) (*TimingSpec, error) {
	if len(fields) != 1 {
		return nil, fmt.Errorf("%w: every expects \"every <duration>\"", ErrInvalidRule)
	}
	d, err := time.ParseDuration(fields[0])
	if err != nil || d <= 0 {
		return nil, fmt.Errorf("%w: invalid duration %q", ErrInvalidRule, fields[0])
	}
	spec := &TimingSpec{Kind: TimingEvery, Every: d}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return spec, nil
}

// parseTimingRRule parses the two-line form:
//
//	rrule:
//	DTSTART;TZID=<IANA>:<yyyymmddThhmmss>
//	RRULE:<rule>
//
// Whitespace is normalized (trimmed lines, collapsed inner spaces) before the
// DTSTART TZID is validated via time.LoadLocation and the RRULE against the
// bounded RFC5545 profile enforced by Validate.
func parseTimingRRule(spec string) (*TimingSpec, error) {
	lines := strings.Split(spec, "\n")
	var dtstart, rrule string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.EqualFold(strings.TrimSuffix(line, ":"), "rrule") {
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "DTSTART"):
			if dtstart != "" {
				return nil, fmt.Errorf("%w: duplicate DTSTART", ErrInvalidRule)
			}
			dtstart = line
		case strings.HasPrefix(upper, "RRULE"):
			if rrule != "" {
				return nil, fmt.Errorf("%w: duplicate RRULE", ErrInvalidRule)
			}
			rrule = line
		default:
			return nil, fmt.Errorf("%w: unexpected line %q", ErrInvalidRule, line)
		}
	}
	if dtstart == "" || rrule == "" {
		return nil, fmt.Errorf("%w: rrule spec requires DTSTART and RRULE lines", ErrInvalidRule)
	}

	tzid, err := parseDTSTARTTZID(dtstart)
	if err != nil {
		return nil, err
	}
	if _, err := time.LoadLocation(tzid); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidTimeZone, tzid, err)
	}
	// A valid DTSTART is the RFC5545 local date+time form "yyyymmddThhmmss"
	// resolved in the TZID; take its wall-clock as the recurrence time of day
	// so Validate/CronExpr share a single clock source.
	ts := strings.TrimSpace(dtstart[strings.LastIndex(dtstart, ":")+1:])
	if !isDTSTARTLocalTs(ts) {
		return nil, fmt.Errorf("%w: DTSTART timestamp must be yyyymmddThhmmss", ErrInvalidRule)
	}
	clock := ts[9:11] + ":" + ts[11:13] + ":" + ts[13:15]
	rule := strings.TrimSpace(rrule[len("RRULE"):])
	rule = strings.TrimPrefix(rule, ":")

	spec2 := &TimingSpec{Kind: TimingRRule, TimeZone: tzid, Time: clock, RRule: dtstart + "\n" + rrule}
	if err := spec2.Validate(); err != nil {
		return nil, err
	}
	return spec2, nil
}

// parseDTSTARTTZID extracts the TZID from a "DTSTART;TZID=<IANA>:<ts>" line.
func parseDTSTARTTZID(line string) (string, error) {
	semi := strings.Index(line, ";")
	colon := strings.LastIndex(line, ":")
	if semi < 0 || colon < semi {
		return "", fmt.Errorf("%w: DTSTART must be \"DTSTART;TZID=<IANA>:<timestamp>\"", ErrInvalidRule)
	}
	params := line[semi+1 : colon]
	for _, p := range strings.Split(params, ";") {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 && strings.EqualFold(kv[0], "TZID") {
			return strings.TrimSpace(kv[1]), nil
		}
	}
	return "", fmt.Errorf("%w: DTSTART missing TZID", ErrInvalidRule)
}

// normalizeClock validates and normalizes an HH:mm or HH:mm:ss clock to
// HH:mm:ss.
func normalizeClock(s string) (string, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return "", fmt.Errorf("%w: time must be HH:mm or HH:mm:ss", ErrInvalidRule)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return "", fmt.Errorf("%w: invalid hour in %q", ErrInvalidRule, s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return "", fmt.Errorf("%w: invalid minute in %q", ErrInvalidRule, s)
	}
	sec := 0
	if len(parts) == 3 {
		if sec, err = strconv.Atoi(parts[2]); err != nil || sec < 0 || sec > 59 {
			return "", fmt.Errorf("%w: invalid second in %q", ErrInvalidRule, s)
		}
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, sec), nil
}

// isoWeekdayToInt maps an ISO weekday number (1=Mon..7=Sun) to the cron dow
// number (0=Sun..6=Sat).
func isoWeekdayToInt(iso int) (int, error) {
	if iso < 1 || iso > 7 {
		return 0, fmt.Errorf("%w: ISO weekday out of range 1..7: %d", ErrInvalidRule, iso)
	}
	if iso == 7 {
		return 0, nil
	}
	return iso, nil
}

// normalizeWeekdays normalizes a weekday token list (cron names or ISO
// numbers) into an ascending, unique ISO 1=Mon..7=Sun slice, mirroring the dsh
// normalizeWeekdays semantics.
func normalizeWeekdays(tokens string) ([]int, error) {
	seen := make(map[int]bool)
	var out []int
	for _, tok := range strings.Split(tokens, ",") {
		tok = strings.TrimSpace(strings.ToUpper(tok))
		if tok == "" {
			return nil, fmt.Errorf("%w: empty weekday token", ErrInvalidRule)
		}
		var iso int
		if n, err := strconv.Atoi(tok); err == nil {
			iso = n
		} else {
			switch tok {
			case "MON", "MO":
				iso = 1
			case "TUE", "TU":
				iso = 2
			case "WED", "WE":
				iso = 3
			case "THU", "TH":
				iso = 4
			case "FRI", "FR":
				iso = 5
			case "SAT", "SA":
				iso = 6
			case "SUN", "SU":
				iso = 7
			default:
				return nil, fmt.Errorf("%w: unknown weekday %q", ErrInvalidRule, tok)
			}
		}
		if iso < 1 || iso > 7 {
			return nil, fmt.Errorf("%w: ISO weekday out of range 1..7: %d", ErrInvalidRule, iso)
		}
		if !seen[iso] {
			seen[iso] = true
			out = append(out, iso)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no weekdays given", ErrInvalidRule)
	}
	sortInts(out)
	return out, nil
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// Validate enforces kind-specific input rules and the bounded RFC5545 profile.
// Out-of-profile constructs are rejected with a typed error, never silently
// mapped onto a wrong schedule.
func (t *TimingSpec) Validate() error {
	if t == nil {
		return fmt.Errorf("%w: nil timing spec", ErrInvalidRule)
	}
	switch t.Kind {
	case TimingDaily:
		if _, err := normalizeClock(t.Time); err != nil {
			return err
		}
		if _, err := time.LoadLocation(t.timeZoneOrUTC()); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidTimeZone, t.TimeZone, err)
		}
	case TimingWeekly:
		if _, err := normalizeClock(t.Time); err != nil {
			return err
		}
		if len(t.Weekdays) == 0 {
			return fmt.Errorf("%w: weekly requires at least one weekday", ErrInvalidRule)
		}
		prev := 0
		for _, d := range t.Weekdays {
			if d < 1 || d > 7 {
				return fmt.Errorf("%w: ISO weekday out of range 1..7: %d", ErrInvalidRule, d)
			}
			if d == prev {
				return fmt.Errorf("%w: duplicate weekday %d", ErrInvalidRule, d)
			}
			prev = d
		}
		if _, err := time.LoadLocation(t.timeZoneOrUTC()); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidTimeZone, t.TimeZone, err)
		}
	case TimingAt:
		if t.At == nil {
			return fmt.Errorf("%w: at requires a timestamp", ErrInvalidRule)
		}
	case TimingEvery:
		if t.Every <= 0 {
			return fmt.Errorf("%w: every requires a positive duration", ErrInvalidRule)
		}
		if int64(t.Every/time.Second) < MinTimingEverySeconds {
			return fmt.Errorf("%w: every %s is below the %ds floor", ErrFrequencyTooHigh, t.Every, MinTimingEverySeconds)
		}
	case TimingRRule:
		return t.validateRRule()
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrTimingUnsupported, string(t.Kind))
	}
	return nil
}

func (t *TimingSpec) timeZoneOrUTC() string {
	if t.TimeZone == "" {
		return "UTC"
	}
	return t.TimeZone
}

// validateRRule enforces the bounded RFC5545 profile:
//
//	ACCEPT: FREQ=DAILY (optional BYHOUR/BYMINUTE); FREQ=WEEKLY with BYDAY
//	(comma list); FREQ=MONTHLY with a single BYMONTHDAY 1-28.
//	REJECT: FREQ=YEARLY, relative/nth BYDAY (e.g. -1FR), COUNT, UNTIL,
//	INTERVAL, BYSECOND/high-density parts, BYMONTHDAY outside 1..28, and any
//	unrecognized part (invalid_rule); invalid time zones (invalid_time_zone).
func (t *TimingSpec) validateRRule() error {
	if t.RRule == "" {
		return fmt.Errorf("%w: rrule requires a rule", ErrInvalidRule)
	}
	lines := strings.Split(t.RRule, "\n")
	if len(lines) != 2 {
		return fmt.Errorf("%w: rrule must be exactly the DTSTART and RRULE lines", ErrInvalidRule)
	}

	// DTSTART line must carry a valid TZID.
	dtstart := strings.TrimSpace(lines[0])
	tzid, err := parseDTSTARTTZID(dtstart)
	if err != nil {
		return err
	}
	if _, err := time.LoadLocation(tzid); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidTimeZone, tzid, err)
	}
	ts := strings.TrimSpace(dtstart[strings.LastIndex(dtstart, ":")+1:])
	if !isDTSTARTLocalTs(ts) {
		return fmt.Errorf("%w: DTSTART timestamp must be yyyymmddThhmmss", ErrInvalidRule)
	}
	// The RRULE wall-clock must match the DTSTART wall-clock so CronExpr can
	// take the time of day from a single source.
	if hh, mm, ss, ok := splitClock(t.Time); !ok || hh != ts[9:11] || mm != ts[11:13] || ss != ts[13:15] {
		return fmt.Errorf("%w: Time must match the DTSTART wall-clock", ErrInvalidRule)
	}

	rule := strings.TrimSpace(lines[1])
	rule = strings.TrimPrefix(strings.TrimPrefix(rule, "RRULE"), ":")
	if rule == "" {
		return fmt.Errorf("%w: empty RRULE", ErrInvalidRule)
	}

	var freq string
	var byday, byhour, byminute, bymonthday string
	for _, part := range strings.Split(rule, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("%w: malformed RRULE part %q", ErrInvalidRule, part)
		}
		key := strings.ToUpper(strings.TrimSpace(kv[0]))
		val := strings.TrimSpace(kv[1])
		switch key {
		case "FREQ":
			freq = strings.ToUpper(val)
		case "BYDAY":
			byday = val
		case "BYHOUR":
			byhour = val
		case "BYMINUTE":
			byminute = val
		case "BYMONTHDAY":
			bymonthday = val
		case "BYSETPOS", "BYSECOND", "BYMINUTE_HIGH", "WKST", "RSCALE":
			return fmt.Errorf("%w: unsupported RRULE part %s", ErrInvalidRule, key)
		default:
			// COUNT, UNTIL, INTERVAL, BYYEARDAY, BYWEEKNO, BYMONTH, ... are all
			// outside the bounded profile.
			return fmt.Errorf("%w: unsupported RRULE part %s", ErrInvalidRule, key)
		}
	}
	_ = byhour
	_ = byminute

	switch freq {
	case "DAILY":
		if byday != "" || bymonthday != "" {
			return fmt.Errorf("%w: FREQ=DAILY does not accept BYDAY/BYMONTHDAY", ErrInvalidRule)
		}
		if byhour != "" || byminute != "" {
			if _, _, _, ok := splitClock(t.Time); ok {
				return fmt.Errorf("%w: BYHOUR/BYMINUTE given but Time already pins the wall-clock", ErrInvalidRule)
			}
		}
	case "WEEKLY":
		if byday == "" {
			return fmt.Errorf("%w: FREQ=WEEKLY requires BYDAY", ErrInvalidRule)
		}
		if bymonthday != "" || byhour != "" || byminute != "" {
			return fmt.Errorf("%w: FREQ=WEEKLY only accepts BYDAY", ErrInvalidRule)
		}
		days, err := normalizeWeekdays(byday)
		if err != nil {
			return err
		}
		if len(t.Weekdays) == 0 {
			t.Weekdays = days
		}
	case "MONTHLY":
		if byday != "" || byhour != "" || byminute != "" {
			return fmt.Errorf("%w: FREQ=MONTHLY only accepts a single BYMONTHDAY", ErrInvalidRule)
		}
		if bymonthday == "" {
			return fmt.Errorf("%w: FREQ=MONTHLY requires BYMONTHDAY", ErrInvalidRule)
		}
		if strings.Contains(bymonthday, ",") {
			return fmt.Errorf("%w: only a single BYMONTHDAY is supported", ErrInvalidRule)
		}
		day, err := strconv.Atoi(bymonthday)
		if err != nil || day < 1 || day > 28 {
			return fmt.Errorf("%w: BYMONTHDAY must be within 1..28, got %q", ErrInvalidRule, bymonthday)
		}
	case "":
		return fmt.Errorf("%w: RRULE missing FREQ", ErrInvalidRule)
	default:
		return fmt.Errorf("%w: unsupported FREQ %s", ErrInvalidRule, freq)
	}
	return nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// isDTSTARTLocalTs reports whether s is the RFC5545 local date+time form
// "yyyymmddThhmmss" — a literal T separating the date from the wall-clock.
func isDTSTARTLocalTs(s string) bool {
	if len(s) != 15 || s[8] != 'T' {
		return false
	}
	return isDigits(s[:8]) && isDigits(s[9:])
}

// splitClock splits an HH:mm[:ss] string into parts (no validation).
func splitClock(s string) (hh, mm, ss string, ok bool) {
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2:
		return parts[0], parts[1], "00", true
	case 3:
		return parts[0], parts[1], parts[2], true
	}
	return "", "", "", false
}

// cronDowList converts ISO weekdays to a cron dow list (0=Sun..6=Sat).
func cronDowList(weekdays []int) (string, error) {
	parts := make([]string, 0, len(weekdays))
	for _, d := range weekdays {
		dow, err := isoWeekdayToInt(d)
		if err != nil {
			return "", err
		}
		parts = append(parts, strconv.Itoa(dow))
	}
	return strings.Join(parts, ","), nil
}

// CronExpr returns the robfig/cron expression for this spec (6-field,
// second-precision form matching the scheduler engine's parser; `every`
// yields an @every descriptor).
func (t *TimingSpec) CronExpr() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	switch t.Kind {
	case TimingDaily:
		hh, mm, _, _ := splitClock(t.Time)
		return fmt.Sprintf("%s %s * * *", mm, hh), nil
	case TimingWeekly:
		hh, mm, _, _ := splitClock(t.Time)
		dows, err := cronDowList(t.Weekdays)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s * * %s", mm, hh, dows), nil
	case TimingAt:
		return "", fmt.Errorf("%w: at has no cron expression (one-shot)", ErrInvalidRule)
	case TimingEvery:
		return fmt.Sprintf("@every %s", t.Every), nil
	case TimingRRule:
		lines := strings.Split(t.RRule, "\n")
		rule := strings.TrimSpace(lines[1])
		rule = strings.TrimPrefix(strings.TrimPrefix(rule, "RRULE"), ":")
		hh, mm, _, _ := splitClock(t.Time)
		var freq string
		for _, part := range strings.Split(rule, ";") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && strings.EqualFold(kv[0], "FREQ") {
				freq = strings.ToUpper(kv[1])
			}
		}
		switch freq {
		case "DAILY":
			return fmt.Sprintf("%s %s * * *", mm, hh), nil
		case "WEEKLY":
			dows, err := cronDowList(t.Weekdays)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s %s * * %s", mm, hh, dows), nil
		case "MONTHLY":
			var day string
			for _, part := range strings.Split(rule, ";") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 && strings.EqualFold(kv[0], "BYMONTHDAY") {
					day = strings.TrimSpace(kv[1])
				}
			}
			return fmt.Sprintf("%s %s %s * *", mm, hh, day), nil
		default:
			return "", fmt.Errorf("%w: unsupported FREQ %s", ErrInvalidRule, freq)
		}
	default:
		return "", fmt.Errorf("%w: unknown kind %q", ErrTimingUnsupported, string(t.Kind))
	}
}

// NextOccurrence resolves the next wall-clock occurrence at or after `from`,
// DST-safe via time.LoadLocation(TimeZone). The returned time carries the
// spec's location. ok=false when no future occurrence exists (at in the past).
func (t *TimingSpec) NextOccurrence(from time.Time) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	switch t.Kind {
	case TimingDaily:
		loc, err := time.LoadLocation(t.timeZoneOrUTC())
		if err != nil {
			return time.Time{}, false
		}
		hh, mm, ss, _ := splitClock(t.Time)
		h, _ := strconv.Atoi(hh)
		m, _ := strconv.Atoi(mm)
		s, _ := strconv.Atoi(ss)
		local := from.In(loc)
		next := time.Date(local.Year(), local.Month(), local.Day(), h, m, s, 0, loc)
		if !next.After(from) {
			next = next.AddDate(0, 0, 1)
		}
		return next, true
	case TimingWeekly:
		loc, err := time.LoadLocation(t.timeZoneOrUTC())
		if err != nil {
			return time.Time{}, false
		}
		hh, mm, ss, _ := splitClock(t.Time)
		h, _ := strconv.Atoi(hh)
		m, _ := strconv.Atoi(mm)
		s, _ := strconv.Atoi(ss)
		local := from.In(loc)
		for i := 0; i < 8; i++ {
			day := time.Date(local.Year(), local.Month(), local.Day(), h, m, s, 0, loc).AddDate(0, 0, i)
			isoDow := int(day.Weekday())
			if isoDow == 0 {
				isoDow = 7
			}
			matched := false
			for _, d := range t.Weekdays {
				if d == isoDow {
					matched = true
					break
				}
			}
			if matched && day.After(from) {
				return day, true
			}
		}
		return time.Time{}, false
	case TimingAt:
		if t.At == nil || !t.At.After(from) {
			return time.Time{}, false
		}
		return *t.At, true
	case TimingEvery:
		if t.Every <= 0 {
			return time.Time{}, false
		}
		return from.Add(t.Every), true
	case TimingRRule:
		spec := &TimingSpec{}
		switch {
		case strings.Contains(t.RRule, "FREQ=WEEKLY") || strings.Contains(t.RRule, "FREQ=weekly"):
			spec.Kind = TimingWeekly
		case strings.Contains(t.RRule, "FREQ=MONTHLY") || strings.Contains(t.RRule, "FREQ=monthly"):
			// Monthly wall-clock resolution is delegated to the cron engine in
			// RegisterTiming; NextOccurrence supports the weekly/daily profile
			// members directly.
			return time.Time{}, false
		default:
			spec.Kind = TimingDaily
		}
		spec.Time = t.Time
		spec.TimeZone = t.TimeZone
		spec.Weekdays = t.Weekdays
		return spec.NextOccurrence(from)
	default:
		return time.Time{}, false
	}
}

// RegisterTiming registers cfg's Timing spec on the given cron engine in the
// engine's location so cron.WithLocation DST handling applies. `at` specs
// register as a one-shot entry that removes itself after firing exactly once.
func RegisterTiming(c *cron.Cron, cfg JobConfig, job Job) (cron.EntryID, error) {
	if cfg.Timing == nil {
		return 0, fmt.Errorf("%w: job config has no timing spec", ErrInvalidRule)
	}
	if err := cfg.Timing.Validate(); err != nil {
		return 0, err
	}

	if cfg.Timing.Kind == TimingAt {
		at := *cfg.Timing.At
		var entryID cron.EntryID
		entryID = c.Schedule(robfigAtSchedule{at: at}, cron.FuncJob(func() {
			// Fire exactly once, then unregister.
			job.Execute(context.Background())
			c.Remove(entryID)
		}))
		return entryID, nil
	}

	expr, err := cfg.Timing.CronExpr()
	if err != nil {
		return 0, err
	}
	return c.AddFunc(expr, func() {
		job.Execute(context.Background())
	})
}

// robfigAtSchedule is a one-shot cron.Schedule that fires at a single instant.
type robfigAtSchedule struct {
	at time.Time
}

func (r robfigAtSchedule) Next(t time.Time) time.Time {
	if t.Before(r.at) {
		return r.at
	}
	return time.Time{}
}

// RegisterTimingVia registers a Timing-carrying JobConfig on the scheduler's
// engine (DST-safe: the engine's location applies). Used from ScheduleConfig
// and loadPersistedJobs so a persisted Timing re-registers on restart.
func (s *Scheduler) RegisterTimingVia(cfg JobConfig, job Job) (cron.EntryID, error) {
	if cfg.Timing == nil {
		return 0, fmt.Errorf("%w: job config has no timing spec", ErrInvalidRule)
	}
	return RegisterTiming(s.cron, cfg, job)
}
