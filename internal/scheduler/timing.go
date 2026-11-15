package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// TimingKind is the semantic kind of a model-authored timing spec.
type TimingKind string

// The five semantic timing kinds (L1 contract). The constants keep the
// Go-side names used across the scheduler (switch statements, tests); the
// JSON WIRE shape is the contracted snake_case form (TimingSpec.MarshalJSON):
// "rrule", "daily", "weekly", "at", "every".
const (
	TimingRRule  TimingKind = "TimingRRule"
	TimingDaily  TimingKind = "TimingDaily"
	TimingWeekly TimingKind = "TimingWeekly"
	TimingAt     TimingKind = "TimingOneShot"
	TimingEvery  TimingKind = "TimingEvery"
)

// timingJSONKind maps a TimingKind constant onto the contracted snake_case
// JSON wire value (05-schedule/01-timing-kinds contract). Unknown kinds pass
// through verbatim so a future kind fails validation loudly rather than
// silently mis-serializing.
func timingJSONKind(k TimingKind) string {
	switch k {
	case TimingRRule:
		return "rrule"
	case TimingDaily:
		return "daily"
	case TimingWeekly:
		return "weekly"
	case TimingAt:
		return "at"
	case TimingEvery:
		return "every"
	}
	return string(k)
}

// timingKindFromJSON maps a JSON wire value back onto the TimingKind
// constant. It accepts BOTH the contracted snake_case form and the legacy
// capitalized constants ("TimingOneShot" etc.) so timing specs persisted by
// older builds still load.
func timingKindFromJSON(s string) (TimingKind, error) {
	switch s {
	case "rrule", "TimingRRule":
		return TimingRRule, nil
	case "daily", "TimingDaily":
		return TimingDaily, nil
	case "weekly", "TimingWeekly":
		return TimingWeekly, nil
	case "at", "TimingOneShot":
		return TimingAt, nil
	case "every", "TimingEvery":
		return TimingEvery, nil
	}
	return "", fmt.Errorf("%w: unknown timing kind %q", ErrTimingUnsupported, s)
}

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

// MarshalJSON serializes the spec with the kind in the contracted snake_case
// wire form ("rrule"/"daily"/"weekly"/"at"/"every"), never the Go-side
// constants ("TimingOneShot" etc.). All other fields use the struct tags
// verbatim.
func (t TimingSpec) MarshalJSON() ([]byte, error) {
	type plain TimingSpec
	wire := struct {
		Kind string `json:"kind"`
		*plain
	}{Kind: timingJSONKind(t.Kind), plain: (*plain)(&t)}
	return json.Marshal(wire)
}

// UnmarshalJSON parses a spec whose kind is either the contracted snake_case
// wire form ("at") or the legacy capitalized constant ("TimingOneShot" —
// written by builds before the JSON contract landed). An unrecognized kind
// fails with ErrTimingUnsupported.
func (t *TimingSpec) UnmarshalJSON(data []byte) error {
	type plain TimingSpec
	aux := struct {
		Kind string `json:"kind"`
		*plain
	}{plain: (*plain)(t)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	kind, err := timingKindFromJSON(aux.Kind)
	if err != nil {
		return err
	}
	t.Kind = kind
	return nil
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
			return nil, fmt.Errorf("time zone %s: %w", tz, errors.Join(ErrInvalidTimeZone, err))
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
			return nil, fmt.Errorf("time zone %s: %w", tz, errors.Join(ErrInvalidTimeZone, err))
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
		return nil, fmt.Errorf("invalid RFC3339 timestamp %q: %w", fields[0], errors.Join(ErrInvalidRule, err))
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
		return nil, fmt.Errorf("time zone %s: %w", tzid, errors.Join(ErrInvalidTimeZone, err))
	}
	// A valid DTSTART is the RFC5545 local date+time form "yyyymmddThhmmss"
	// resolved in the TZID; take its wall-clock as the recurrence time of day
	// so Validate/CronExpr share a single clock source.
	ts := strings.TrimSpace(dtstart[strings.LastIndex(dtstart, ":")+1:])
	if !isDTSTARTLocalTS(ts) {
		return nil, fmt.Errorf("%w: DTSTART timestamp must be yyyymmddThhmmss", ErrInvalidRule)
	}
	clock := ts[9:11] + ":" + ts[11:13] + ":" + ts[13:15]

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
	for p := range strings.SplitSeq(params, ";") {
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
	for tok := range strings.SplitSeq(tokens, ",") {
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
			return fmt.Errorf("time zone %s: %w", t.timeZoneOrUTC(), errors.Join(ErrInvalidTimeZone, err))
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
			return fmt.Errorf("time zone %s: %w", t.timeZoneOrUTC(), errors.Join(ErrInvalidTimeZone, err))
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
		return fmt.Errorf("time zone %s: %w", tzid, errors.Join(ErrInvalidTimeZone, err))
	}
	ts := strings.TrimSpace(dtstart[strings.LastIndex(dtstart, ":")+1:])
	if !isDTSTARTLocalTS(ts) {
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
	for part := range strings.SplitSeq(rule, ";") {
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

// isDTSTARTLocalTS reports whether s is the RFC5545 local date+time form
// "yyyymmddThhmmss" — a literal T separating the date from the wall-clock.
func isDTSTARTLocalTS(s string) bool {
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
		for part := range strings.SplitSeq(rule, ";") {
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
			for part := range strings.SplitSeq(rule, ";") {
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
		for i := range 8 {
			day := time.Date(local.Year(), local.Month(), local.Day(), h, m, s, 0, loc).AddDate(0, 0, i)
			isoDow := int(day.Weekday())
			if isoDow == 0 {
				isoDow = 7
			}
			matched := slices.Contains(t.Weekdays, isoDow)
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

// One-shot recovery (owner-decided semantics, 2026-11-14):
//
// A TimingAt job persists its due time in the Timing spec itself (the
// `at` field — an absolute RFC3339 instant carried in the JSON `timing`
// column of jobs.json, so older rows without it behave as today), NOT in
// the cron Schedule string (which stays empty for one-shots — the reason
// ProcessMissedTicks could never catch them up).
//
// "Run at T" means "run as soon as possible AFTER T":
//   - one-shot due T in (now-window, past] at boot  -> fires on boot;
//   - one-shot due T at or before now-window        -> marked failed
//     (never ran; re-runnable via RunNow; no cron entry);
//   - scheduling a one-shot due more than window ahead -> rejected with
//     ErrOneShotBeyondHorizon, naming the window (one-shots are
//     near-term triggers; operators needing later should use cron).
//
// The window is [scheduler] one_shot_window (config.SchedulerConfig.
// EffectiveOneShotWindow); DefaultOneShotCatchUpWindow mirrors the config
// default (2h) for call sites that have no SchedulerConfig.

// DefaultOneShotCatchUpWindow mirrors config.DefaultOneShotWindow (2h). It
// is only a fallback: the live window always comes from the scheduler's
// cfg.Scheduler.OneShotWindow via effectiveOneShotWindow.
const DefaultOneShotCatchUpWindow = 2 * time.Hour

// ErrOneShotBeyondHorizon is returned when a TimingAt spec's due time lies
// further in the future than the configured one-shot window. The wrapped
// message names the window (operators needing later triggers should use
// cron).
var ErrOneShotBeyondHorizon = fmt.Errorf("one_shot_beyond_horizon")

// effectiveOneShotWindow returns the configured one-shot horizon, falling
// back to the 2h default when unset/unparseable/non-positive (zero-value =
// default).
func (s *Scheduler) effectiveOneShotWindow() time.Duration {
	if d, err := time.ParseDuration(s.cfg.OneShotWindow); err == nil && d > 0 {
		return d
	}
	return DefaultOneShotCatchUpWindow
}

// validateOneShotHorizon rejects a TimingAt due time further in the future
// than window. Called at schedule time (RegisterTiming); overdue due times
// are NOT rejected here — boot recovery decides fire-vs-failed.
func validateOneShotHorizon(spec *TimingSpec, at time.Time, window time.Duration) error {
	if spec == nil || spec.Kind != TimingAt {
		return nil
	}
	if horizon := time.Now().UTC().Add(window); at.After(horizon) {
		return fmt.Errorf("%w: one-shot due %s is more than %s in the future (use a cron schedule for far-future triggers)",
			ErrOneShotBeyondHorizon, at.Format(time.RFC3339), window)
	}
	return nil
}

// sweepOneShotsAtBoot reconciles persisted TimingAt jobs against the
// configured window. "Run at T" = run as soon as possible after T:
//
//   - due within the past window (T > now-window) and not yet fired:
//     fire now (claim + execute, the same at-least-once path missed ticks
//     take), then prune the row — the one-shot has fired.
//   - due at or before now-window: mark the job FAILED (never ran) via
//     markOneShotFailed, remove the cron entry, and leave the row in the
//     store re-runnable via RunNow (the failed row stops looking
//     schedulable but is not silently deleted).
//
// Runs from Start alongside the other boot-recovery sweeps. Note the raw
// cron registration is useless for overdue one-shots (robfigAtSchedule.Next
// returns the zero time for a past instant), which is exactly why this
// sweep exists: ProcessMissedTicks cannot parse the empty Schedule string.
func (s *Scheduler) sweepOneShotsAtBoot() {
	window := s.effectiveOneShotWindow()
	now := time.Now().UTC()
	cutoff := now.Add(-window)

	for _, cfg := range s.store.List() {
		if cfg.Timing == nil || cfg.Timing.Kind != TimingAt || cfg.Timing.At == nil {
			continue
		}
		if !cfg.Enabled {
			continue // paused/disabled rows are never fired or marked
		}
		if cfg.OneShotFailed || cfg.OneShotFiredAt != nil {
			continue // already reconciled
		}
		due := cfg.Timing.At.UTC()
		// Future dues belong to the live cron entry (registered by
		// loadPersistedJobs); the sweep handles only past dues — firing a
		// future-due one-shot here would run it before its scheduled time.
		if due.After(now) {
			continue
		}
		if due.After(cutoff) {
			// Overdue-within-window: fire now, then prune the row.
			s.mu.RLock()
			job, ok := s.jobs[cfg.ID]
			s.mu.RUnlock()
			if !ok {
				continue
			}
			s.logger.Info("scheduler: firing overdue one-shot on boot",
				"job_id", cfg.ID,
				"due", due.Format(time.RFC3339),
				"window", window.String(),
			)
			s.fireOverdueOneShot(cfg, job, due, now)
			continue
		}
		// Past the window: mark failed (never ran), remove the cron entry,
		// keep the row re-runnable via RunNow.
		if err := s.markOneShotFailed(cfg, due, now, window); err != nil {
			s.logger.Warn("scheduler: failed to mark overdue one-shot as failed",
				"job_id", cfg.ID, "error", err)
		}
	}
}

// fireOneShotOnce is the single claim-before-deliver path for TimingAt
// one-shot firings, shared by the boot sweep (fireOverdueOneShot) and the
// cron-fire closure (RegisterTimingVia). Both paths must go through it so a
// cron-fired occurrence is claimed and stamped exactly like a
// sweep-fired one — otherwise a daemon restart between the normal cron
// fire and row pruning re-runs the job at boot.
//
// Sequence: ClaimTick(due) -> on claim error warn+skip; if !claimed the
// occurrence was already delivered, so the stale store row is pruned (and
// the cron entry removed when present); if claimed, OneShotFiredAt is
// stamped best-effort BEFORE dispatch; then the job executes and the row is
// pruned on success (LastError=="") or kept (with the stamp) on failure.
func (s *Scheduler) fireOneShotOnce(cfg JobConfig, job Job, due, now time.Time) {
	// Claim the due instant atomically: a previous instance that already
	// fired it (crash after execution, before pruning) must not re-fire.
	claimed, claimErr := s.ClaimTick(cfg.ID, due)
	if claimErr != nil {
		s.logger.Warn("scheduler: failed to claim one-shot tick",
			"job_id", cfg.ID, "due", due.Format(time.RFC3339), "error", claimErr)
		return
	}
	if !claimed {
		// Already delivered: the persisted row is stale residue — prune it
		// instead of accumulating it forever. With the cron closure routed
		// through this helper, a false claim can only mean the cron path
		// delivered this exact due instant, so pruning is correct.
		if err := s.store.Remove(cfg.ID); err != nil {
			s.logger.Warn("scheduler: failed to prune already-fired one-shot row",
				"job_id", cfg.ID, "error", err)
		}
		// Drop any lingering cron entry: the one-shot has fired.
		s.mu.Lock()
		if entryID, ok := s.entryIDs[cfg.ID]; ok {
			s.cron.Remove(entryID)
			delete(s.entryIDs, cfg.ID)
		}
		s.mu.Unlock()
		return
	}

	// Stamp OneShotFiredAt BEFORE dispatch: a crash mid-execution leaves a
	// row the next sweep neither re-fires nor re-marks (delivery recovery
	// for session-pinned jobs proceeds via the receipt rows). Best-effort:
	// a failed stamp leaves the row re-sweepable, but the due tick is
	// already claimed, so the sweep's own fire attempt is skipped.
	firedCfg := cfg
	firedAt := now
	firedCfg.OneShotFiredAt = &firedAt
	if err := s.store.Update(firedCfg); err != nil {
		s.logger.Warn("scheduler: failed to record one-shot firing; row stays re-sweepable",
			"job_id", cfg.ID, "error", err)
	}

	// Mirror dispatchMissed: shutdown-aware context, timeout, panic
	// recovery (a panic here would otherwise kill the daemon with the tick
	// already claimed — bughunt 2026-11-12 F5).
	s.mu.RLock()
	baseCtx := s.runNowCtx
	s.mu.RUnlock()
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, 30*time.Minute)
	go func() {
		defer cancel()
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("scheduler: panic in one-shot recovered",
					"job_id", cfg.ID,
					"due", due.Format(time.RFC3339),
					"panic", rec,
					"stack", string(debug.Stack()),
				)
			}
		}()
		s.executeJob(ctx, job, ExecutionOptions{Tick: due, PreClaimed: true})
		// Prune-or-mark: a successful run leaves no stale row behind; a
		// failed run keeps it (with OneShotFiredAt set) for inspection and
		// manual re-run.
		if latest, ok := s.store.Get(cfg.ID); ok && latest.LastError == "" {
			if err := s.store.Remove(cfg.ID); err != nil {
				s.logger.Warn("scheduler: failed to prune fired one-shot row",
					"job_id", cfg.ID, "error", err)
			}
		}
	}()
}

// fireOverdueOneShot delivers an overdue-within-window one-shot discovered
// by sweepOneShotsAtBoot through the shared claim-before-deliver path.
func (s *Scheduler) fireOverdueOneShot(cfg JobConfig, job Job, due, now time.Time) {
	s.fireOneShotOnce(cfg, job, due, now)
}

// markOneShotFailed flips a past-window one-shot into the failed-never-ran
// posture (JobConfig.OneShotFailed=true, LastError naming the miss), removes
// its cron entry, and persists the row so it stops looking schedulable but
// stays re-runnable via RunNow.
func (s *Scheduler) markOneShotFailed(cfg JobConfig, due, now time.Time, window time.Duration) error {
	s.mu.Lock()
	if entryID, ok := s.entryIDs[cfg.ID]; ok {
		s.cron.Remove(entryID)
		delete(s.entryIDs, cfg.ID)
	}
	s.mu.Unlock()

	cfg.OneShotFailed = true
	cfg.LastError = fmt.Sprintf("one-shot never ran: due %s is more than %s before boot; re-run manually",
		due.Format(time.RFC3339), window)
	if err := s.store.Update(cfg); err != nil {
		return err
	}
	s.logger.Warn("scheduler: overdue one-shot beyond window marked failed (re-runnable via run-now)",
		"job_id", cfg.ID,
		"due", due.Format(time.RFC3339),
		"window", window.String(),
	)
	return nil
}

// RegisterTimingVia registers a Timing-carrying JobConfig on the scheduler's
// engine (DST-safe: the engine's location applies). Used from ScheduleConfig
// and loadPersistedJobs so a persisted Timing re-registers on restart.
func (s *Scheduler) RegisterTimingVia(cfg JobConfig, job Job) (cron.EntryID, error) {
	if cfg.Timing == nil {
		return 0, fmt.Errorf("%w: job config has no timing spec", ErrInvalidRule)
	}
	return s.registerTiming(cfg, job, s.effectiveOneShotWindow())
}

// registerTiming is the scheduler-backed variant of RegisterTiming: the
// one-shot cron closure delivers through the shared fireOneShotOnce path so
// a normal cron fire claims the due tick, stamps OneShotFiredAt, and prunes
// the store row exactly like the boot sweep does.
func (s *Scheduler) registerTiming(cfg JobConfig, job Job, oneShotWindow time.Duration) (cron.EntryID, error) {
	if cfg.Timing == nil {
		return 0, fmt.Errorf("%w: job config has no timing spec", ErrInvalidRule)
	}
	if err := cfg.Timing.Validate(); err != nil {
		return 0, err
	}

	if cfg.Timing.Kind == TimingAt {
		// Scheduling horizon (owner-decided): a one-shot is a near-term
		// trigger, not a calendar event. Due times further ahead than the
		// configured window are rejected at schedule time (overdue times
		// are allowed here — boot recovery fires or fails them).
		if err := validateOneShotHorizon(cfg.Timing, *cfg.Timing.At, oneShotWindow); err != nil {
			return 0, err
		}
		at := *cfg.Timing.At
		var entryID cron.EntryID
		entryID = s.cron.Schedule(robfigAtSchedule{at: at}, cron.FuncJob(func() {
			// Fire exactly once, then unregister. The delivery goes through
			// the shared claim-before-deliver helper (same path as the boot
			// sweep): claim the due tick, stamp OneShotFiredAt, execute,
			// prune the store row — a daemon restart after this fire must
			// not re-run the job from the stale row. fireOneShotOnce keeps
			// the goroutine posture (shutdown-aware ctx, timeout, panic
			// recovery) so both firing paths stay identical.
			s.fireOneShotOnce(cfg, job, at.UTC(), time.Now().UTC())
			s.mu.Lock()
			if cur, ok := s.entryIDs[cfg.ID]; ok && cur == entryID {
				s.cron.Remove(entryID)
				delete(s.entryIDs, cfg.ID)
			}
			s.mu.Unlock()
		}))
		return entryID, nil
	}

	expr, err := cfg.Timing.CronExpr()
	if err != nil {
		return 0, err
	}
	return s.cron.AddFunc(expr, func() {
		if err := job.Execute(context.Background()); err != nil {
			slog.Warn("scheduler: recurring timing job failed", "error", err)
		}
	})
}

// RegisterTiming registers cfg's Timing spec on the given cron engine in the
// engine's location so cron.WithLocation DST handling applies. `at` specs
// register as a one-shot entry that removes itself after firing exactly once.
// oneShotWindow is the configured scheduling horizon for the at-kind
// rejection; callers without a scheduler pass DefaultOneShotCatchUpWindow.
//
// This standalone form has no claim store or jobs.json writer available, so
// its one-shot closure executes directly; the scheduler-backed registerTiming
// (RegisterTimingVia) is the production path and delivers through
// fireOneShotOnce.
func RegisterTiming(c *cron.Cron, cfg JobConfig, job Job, oneShotWindow time.Duration) (cron.EntryID, error) {
	if cfg.Timing == nil {
		return 0, fmt.Errorf("%w: job config has no timing spec", ErrInvalidRule)
	}
	if err := cfg.Timing.Validate(); err != nil {
		return 0, err
	}

	if cfg.Timing.Kind == TimingAt {
		// Scheduling horizon (owner-decided): a one-shot is a near-term
		// trigger, not a calendar event. Due times further ahead than the
		// configured window are rejected at schedule time (overdue times
		// are allowed here — boot recovery fires or fails them).
		if err := validateOneShotHorizon(cfg.Timing, *cfg.Timing.At, oneShotWindow); err != nil {
			return 0, err
		}
		at := *cfg.Timing.At
		var entryID cron.EntryID
		entryID = c.Schedule(robfigAtSchedule{at: at}, cron.FuncJob(func() {
			// Fire exactly once, then unregister.
			if err := job.Execute(context.Background()); err != nil {
				slog.Warn("scheduler: one-shot timing job failed", "error", err)
			}
			c.Remove(entryID)
		}))
		return entryID, nil
	}

	expr, err := cfg.Timing.CronExpr()
	if err != nil {
		return 0, err
	}
	return c.AddFunc(expr, func() {
		if err := job.Execute(context.Background()); err != nil {
			slog.Warn("scheduler: recurring timing job failed", "error", err)
		}
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
