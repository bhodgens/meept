## Interface Contracts

Branch-owned contracts live here once. Leaves consume exact signatures only and
never restate full definitions.

### L1: Timing kinds (Owner: 01-timing-kinds.md)

```go
// internal/scheduler/timing.go (NEW)
type TimingKind string
const (
	TimingRRule  TimingKind = "rrule"
	TimingDaily  TimingKind = "daily"
	TimingWeekly TimingKind = "weekly"
	TimingAt     TimingKind = "at"
	TimingEvery  TimingKind = "every"
)
type TimingSpec struct {
	Kind     TimingKind    `json:"kind"`
	Time     string        `json:"time,omitempty"`      // HH:mm:ss (daily/weekly)
	TimeZone string        `json:"time_zone,omitempty"` // IANA, e.g. "America/Denver" (daily/weekly/rrule DTSTART TZID)
	Weekdays []int         `json:"weekdays,omitempty"`  // ISO 1=Mon..7=Sun, ascending unique (weekly)
	At       *time.Time    `json:"at,omitempty"`        // absolute instant (at)
	Every    time.Duration `json:"every,omitempty"`     // >= 60s (every)
	RRule    string        `json:"rrule,omitempty"`     // frozen two-line "DTSTART:...\nRRULE:..." (rrule)
}
func ParseTiming(spec string) (*TimingSpec, error)          // model-facing string -> spec
func (t *TimingSpec) Validate() error                        // kind-specific input rules + RFC5545 profile gate
func (t *TimingSpec) CronExpr() (string, error)              // robfig expression for s.cron (scheduler location)
func (t *TimingSpec) NextOccurrence(from time.Time) (time.Time, bool) // DST-safe via time.LoadLocation(t.TimeZone)
const MinTimingEverySeconds = 60   // mirrors dsh MIN_EVERY_INTERVAL_SECONDS floor
// JobConfig gains:      Timing *TimingSpec `json:"timing,omitempty"`
// scheduler.go gains:   func (s *Scheduler) RegisterTiming(cfg JobConfig, job Job) (cron.EntryID, error)
// rpc.go gains:         AddJobParams.Timing string `json:"timing,omitempty"` (model-authored spec string)
// tool_schedule_create.go: schedule_create tool gains optional "timing" string param (exclusive with "schedule")
// Owners: 01-timing-kinds. Consumers: 02 uses none of L1's types (delivery is kind-agnostic).
```

Supported RFC5545 profile (the "RFC5545 satisfied in meept" bound, enumerated
in 01-timing-kinds.md): `FREQ=DAILY|WEEKLY|MONTHLY` with only the constructs a
single robfig/cron expression can express (DAILY with at most `BYHOUR/BYMINUTE`;
WEEKLY with `BYDAY`; MONTHLY with a single `BYMONTHDAY` 1-28). Everything else
(`BYDAY`-relative last/nth weekday, `FREQ=YEARLY`, `COUNT`, `UNTIL`,
`BYSECOND`/high-density, `INTERVAL`) is REJECTED with a typed error
(`invalid_rule`/`frequency_too_high`/`invalid_time_zone`) so an unsupported
rule fails loudly rather than silently expanding to a wrong schedule. This
mirrors dsh's bounded-subset RRULE parser (rejectIneffectiveSetPositions,
COUNT<=1000 ceiling, no SECONDLY/single BYSECOND).

### L2: Session delivery inbox + receipts (Owner: 02-delivery-inbox.md)

```go
// internal/scheduler/receipts.go (NEW)
type DeliveryStatus string
const (
	DeliveryPending   DeliveryStatus = "pending"   // claim taken, enqueue started, session ack not yet confirmed
	DeliveryInFlight  DeliveryStatus = "in_flight" // chat.request published, awaiting session-store ack
	DeliveryDelivered DeliveryStatus = "delivered" // target session inbox acknowledged the message
)
type ScheduleDeliveryReceipt struct {
	ScheduleID      string        `json:"schedule_id"`
	ScheduledAt     time.Time     `json:"scheduled_at"`
	OccurrenceAt    time.Time     `json:"occurrence_at,omitempty"`
	DeliveredAt     time.Time     `json:"delivered_at,omitempty"`
	MessageID       string        `json:"message_id"`        // stable turn_id; idempotent across re-delivery
	TargetSessionID string        `json:"target_session_id"`
	Prompt          string        `json:"prompt,omitempty"`
	Status          DeliveryStatus `json:"status"`
}
type DeliveryHistory struct {
	Records              []ScheduleDeliveryReceipt `json:"records"`
	EarlierRecordsPruned bool                     `json:"earlier_records_pruned"`
}
const MaxDeliveryHistoryRecords = 200
const DeliveryHistoryRetentionDays = 30
// AgentJobConfig gains: TargetSessionID string `json:"target_session_id,omitempty"`
// JobConfig gains:       LastDelivery *ScheduleDeliveryReceipt `json:"last_delivery,omitempty"`
//                        DeliveryHistory *DeliveryHistory       `json:"delivery_history,omitempty"`
// jobs.go: AgentJob.Execute pins payload["session_id"]=TargetSessionID when set
// scheduler.go gains: func (s *Scheduler) recordDelivery(jobID string, r ScheduleDeliveryReceipt) error
//                     func (s *Scheduler) recoverInFlightDeliveries() error  // called in Start, after loadPersistedJobs
// Owners: 02-delivery-inbox. Consumers: none downstream (GUI/task-manager parity deferred to branch 04/Open Questions).
```

At-least-once across restart: the existing claim store already dedupes *ticks*
(`executeJob` calls `s.ClaimTick` before dispatch). That claim is the reason a
crash after claim+deliver but before the session ack loses the reminder
(fire-and-forget) - the tick will not re-fire. Recovery therefore scans *receipt
rows*, not ticks: any receipt stuck in `pending`/`in_flight` is re-delivered
with the same `MessageID`/`target_session_id` after Start; the `chat.request`
`turn_id` dedupe (internal/agent/handler.go:804-814, idempotent re-registration)
makes that re-delivery safe. Contract consumers read `LastDelivery` +
`DeliveryHistory` for the bounded history (retention 30 days / <=200 records).

