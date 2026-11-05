# Verified Anchors - branch 05-schedule (verified at 40f6fa5a)

Grep-verified. Use these exact shapes; do not invent APIs.

- `internal/scheduler/claims.go:52` — `type claimStore struct`; `:96` `(cs *claimStore) ClaimTick(jobID string, tick time.Time) (bool, error)`; `:417` `(s *Scheduler) ClaimTick(jobID string, tick time.Time) (claimed bool, err error)`
- `internal/scheduler/jobs.go:70` — `type AgentJobConfig struct`; `:179` `type AgentJob struct`; `:212` `(j *AgentJob) Execute(ctx context.Context) error` (currently publishes `chat.request` fire-and-forget — the F13b gap your delivery tests must pin)
- `internal/scheduler/scheduler.go:184-185` — `cron.WithLocation(loc)`, `cron.WithSeconds()` (robfig/cron v3 engine; DST seam)
- `internal/scheduler/persistence.go:14` — `type Store struct` (jobs.json); `:68 Load() ([]JobConfig, error)`; `:109 Save(jobs []JobConfig) error`; `:151 Get(id string) (JobConfig, bool)`; `:160 Add(job JobConfig) error`
- `internal/agent/handler.go:804-814` — idempotent TurnID registration (the dedup seam F13b restart-redelivery rides on)

New symbols your tests reference (from contracts L1/L2 — undefined today, that IS the RED):
- `ParseTiming(string) (*TimingSpec, error)` with kinds TimingAt/TimingEvery/TimingCron/TimingRRule; `(*TimingSpec).CronExpr() (string, error)`; typed errors `invalid_rule` / `invalid_time_zone` / `frequency_too_high`; `MinTimingEverySeconds = 60`
- receipts: `ReceiptStore` bounded history, `ReceiptStatus` transitions (pending->delivered->acked), `RecoverInFlight` re-delivery with the SAME MessageID
- `chat.request` payload gains `session_id` (F13b delivery target)

RED-gate convention (repo precedent commit 537c92fd): if a test file's
undefined-symbol errors break `go vet`/`go build` of the NON-test package,
park the file as `<name>_test.go.staged` (excluded from build) and note it in
your report. If `go build ./internal/scheduler` stays green and only
`go test` fails on undefined symbols, keep the file live.

e2e: suite dir `e2e/suites/scheduler/` already exists (existing suite files —
ADD new scenario files, do not rewrite existing ones; check the manifest
entry for `scheduler` already exists before touching `e2e/manifest.json`).
Build tag `//go:build e2e`, import `github.com/caimlas/meept/e2e/harness`.
