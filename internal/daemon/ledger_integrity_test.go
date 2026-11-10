package daemon

// Tests for plan 20261109-employee-ledger leaf 04-ops/01 — the
// ledger-integrity scheduler job + [ledger] config table. Gate matrix rows
// (04-ops/orchestrator.md Test Gate):
//   - [ledger] config parses (auto_repair bool, interval_hours=168 default)
//   - Integrity clean -> single INFO; divergent -> ERROR, NO repair default
//   - auto_repair=true -> RepairState called, count logged
//   - nil store -> job never registers (degrade posture guard)

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // raw corrupt handles need the driver registered

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/scheduler"
)

// liCaptureHandler records "LEVEL message key=value ..." lines including
// slog attrs — needed to assert both message text AND log-field content
// (payload never leaks, per-employee keys do appear). Distinct from
// tjCaptureHandler (message-only) and captureHandler (message-only).
type liCaptureHandler struct {
	mu    sync.Mutex
	lines []string
}

func (h *liCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *liCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Level.String())
	sb.WriteString(" ")
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines = append(h.lines, sb.String())
	return nil
}
func (h *liCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *liCaptureHandler) WithGroup(string) slog.Handler      { return h }

func (h *liCaptureHandler) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.lines))
	copy(out, h.lines)
	return out
}

func (h *liCaptureHandler) count(substr string) int {
	n := 0
	for _, l := range h.snapshot() {
		if strings.Contains(l, substr) {
			n++
		}
	}
	return n
}

func (h *liCaptureHandler) countLevel(level slog.Level) int {
	n := 0
	for _, l := range h.snapshot() {
		if strings.HasPrefix(l, level.String()+" ") {
			n++
		}
	}
	return n
}

// openLedgerStoreAt opens a real ledger store in a temp dir and returns
// (store, path) so corruption helpers can grab a second raw handle (WAL
// + busy_timeout, the ledger_test.go idiom).
func openLedgerStoreAt(t *testing.T) (*ledger.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := ledger.Open(path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("ledger.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

// corruptStateAt raw-writes ledger_state away from the fold — the exact
// hand-corrupt idiom the 04 integration plan pins ("a hand-corrupt of
// ledger_state").
func corruptStateAt(t *testing.T, dbPath, employee, subject, status, summary string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("raw handle open: %v", err)
	}
	defer db.Close()
	res, err := db.ExecContext(context.Background(),
		`UPDATE ledger_state SET status = ?, summary = ? WHERE employee_id = ? AND subject = ?`,
		status, summary, employee, subject)
	if err != nil {
		t.Fatalf("raw corrupt UPDATE: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("raw corrupt UPDATE affected %d rows, want 1", n)
	}
}

// appendStatusRow appends one status-carrying row through the real API.
// The payload carries a marker string so the log-hygiene test can prove
// payload content never reaches the log.
func appendStatusRow(t *testing.T, s *ledger.Store, employee, subject string, status ledger.Status, summary string) *ledger.Record {
	t.Helper()
	rec, err := s.Append(context.Background(), ledger.DraftRecord{
		EmployeeID: employee,
		Kind:       ledger.KindCommitment,
		Subject:    subject,
		Payload:    []byte(`{"detail":"secret-payload-marker-5550100"}`),
		Source:     ledger.SourceVoice,
		SetStatus:  status,
		Summary:    summary,
	})
	if err != nil {
		t.Fatalf("Append(%s/%s): %v", employee, subject, err)
	}
	return rec
}

// newLedgerTestScheduler builds a real scheduler on a temp data dir —
// same construction the intake/notification wiring tests use (they sit
// behind a build tag, so this leaf keeps its own copy).
func newLedgerTestScheduler(t *testing.T) *scheduler.Scheduler {
	t.Helper()
	s, err := scheduler.NewScheduler(
		config.SchedulerConfig{Enabled: true},
		bus.New(bus.DefaultConfig(), slog.New(slog.DiscardHandler)),
		scheduler.WithDataDir(t.TempDir()),
		scheduler.WithLogger(slog.New(slog.DiscardHandler)),
	)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

// ---------------------------------------------------------------------------
// Gate row: [ledger] config parses (auto_repair bool, interval_hours=168 default)
// ---------------------------------------------------------------------------

func TestLedgerIntegrity_ConfigDefaults(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Ledger.AutoRepair {
		t.Error("DefaultConfig: Ledger.AutoRepair = true, want false (opt-in default)")
	}
	if cfg.Ledger.IntervalHours != 168 {
		t.Errorf("DefaultConfig: Ledger.IntervalHours = %d, want 168", cfg.Ledger.IntervalHours)
	}

	// Load-boundary normalization: non-positive interval clamps to 168.
	for _, in := range []int{0, -1, -168} {
		c := config.LedgerConfig{IntervalHours: in}
		config.NormalizeLedgerDefaults(&c)
		if c.IntervalHours != 168 {
			t.Errorf("NormalizeLedgerDefaults(%d): IntervalHours = %d, want 168", in, c.IntervalHours)
		}
	}
	// Explicit positive value survives untouched.
	c := config.LedgerConfig{AutoRepair: true, IntervalHours: 24}
	config.NormalizeLedgerDefaults(&c)
	if c.IntervalHours != 24 || !c.AutoRepair {
		t.Errorf("NormalizeLedgerDefaults must not mutate valid config: %+v", c)
	}
}

func TestLedgerIntegrity_ConfigParsesFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	body := "[ledger]\nauto_repair = true\ninterval_hours = 24\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !cfg.Ledger.AutoRepair {
		t.Error("loaded config: Ledger.AutoRepair = false, want true")
	}
	if cfg.Ledger.IntervalHours != 24 {
		t.Errorf("loaded config: Ledger.IntervalHours = %d, want 24", cfg.Ledger.IntervalHours)
	}

	// Omitted table -> shipped defaults survive the load boundary.
	path2 := filepath.Join(dir, "empty.toml")
	if err := os.WriteFile(path2, []byte("[daemon]\ndata_dir = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg2, err := config.Load(path2)
	if err != nil {
		t.Fatalf("config.Load(empty): %v", err)
	}
	if cfg2.Ledger.AutoRepair || cfg2.Ledger.IntervalHours != 168 {
		t.Errorf("omitted [ledger]: got %+v, want {AutoRepair:false IntervalHours:168}", cfg2.Ledger)
	}
}

// ---------------------------------------------------------------------------
// Gate row: Integrity clean -> single INFO; divergent -> ERROR, NO repair default
// ---------------------------------------------------------------------------

func TestLedgerIntegrity_CleanLogsInfo(t *testing.T) {
	store, _ := openLedgerStoreAt(t)
	appendStatusRow(t, store, "emp-a", "roof", ledger.StatusOpen, "needs quote")

	h := &liCaptureHandler{}
	job := NewLedgerIntegrityJob(store, config.LedgerConfig{IntervalHours: 168}, slog.New(h))
	report, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Divergent != 0 {
		t.Fatalf("Divergent = %d, want 0 on a healthy store", report.Divergent)
	}
	if report.Checked < 1 {
		t.Errorf("Checked = %d, want >= 1", report.Checked)
	}
	if got := h.count("ledger integrity clean"); got != 1 {
		t.Errorf("INFO 'ledger integrity clean' count = %d, want exactly one", got)
	}
	if got := h.countLevel(slog.LevelError); got != 0 {
		t.Errorf("ERROR records = %d, want 0 on clean pass", got)
	}
}

func TestLedgerIntegrity_DivergentLogsErrorNoRepairByDefault(t *testing.T) {
	store, dbPath := openLedgerStoreAt(t)
	appendStatusRow(t, store, "emp-b", "insurance", ledger.StatusDone, "filed")
	appendStatusRow(t, store, "emp-b", "roof", ledger.StatusOpen, "needs quote")
	corruptStateAt(t, dbPath, "emp-b", "roof", "cancelled", "TAMPERED")

	h := &liCaptureHandler{}
	// auto_repair left FALSE (the shipped default).
	job := NewLedgerIntegrityJob(store, config.LedgerConfig{IntervalHours: 168}, slog.New(h))
	report, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Divergent != 1 {
		t.Fatalf("Divergent = %d, want 1", report.Divergent)
	}
	if got := h.count("ledger integrity divergent"); got != 1 {
		t.Errorf("ERROR 'ledger integrity divergent' count = %d, want 1", got)
	}
	errLines := 0
	for _, l := range h.snapshot() {
		if strings.HasPrefix(l, "ERROR") {
			errLines++
		}
	}
	if errLines < 2 {
		t.Errorf("expected per-employee ERROR + per-diff ERROR, got %d ERROR lines: %v", errLines, h.snapshot())
	}
	if h.count("ledger integrity repaired") != 0 {
		t.Error("auto_repair=false must never invoke RepairState")
	}
	// No repair happened: a second verify still diverges.
	rep2, err := store.VerifyState(context.Background())
	if err != nil {
		t.Fatalf("re-verify: %v", err)
	}
	if rep2.Divergent != 1 {
		t.Errorf("after default RunOnce: Divergent = %d, want still 1 (no repair)", rep2.Divergent)
	}
}

func TestLedgerIntegrity_ErrorLogCarriesKeysNotPayload(t *testing.T) {
	store, dbPath := openLedgerStoreAt(t)
	appendStatusRow(t, store, "emp-c", "roof", ledger.StatusOpen, "needs quote")
	corruptStateAt(t, dbPath, "emp-c", "roof", "done", "TAMPERED")

	h := &liCaptureHandler{}
	job := NewLedgerIntegrityJob(store, config.LedgerConfig{}, slog.New(h))
	if _, err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	joined := strings.Join(h.snapshot(), "\n")
	if strings.Contains(joined, "secret-payload-marker") || strings.Contains(joined, "5550100") {
		t.Errorf("log leaked payload content: %v", h.snapshot())
	}
	// Identity keys ARE present: the per-employee diff count.
	if !strings.Contains(joined, "emp-c=1") {
		t.Errorf("log missing per-employee diff count key 'emp-c=1': %v", h.snapshot())
	}
}

// ---------------------------------------------------------------------------
// Gate row: auto_repair=true -> RepairState called, count logged
// ---------------------------------------------------------------------------

func TestLedgerIntegrity_AutoRepairInvokesRepair(t *testing.T) {
	store, dbPath := openLedgerStoreAt(t)
	appendStatusRow(t, store, "emp-d", "roof", ledger.StatusOpen, "needs quote")
	appendStatusRow(t, store, "emp-d", "gutter", ledger.StatusDone, "cleaned")
	corruptStateAt(t, dbPath, "emp-d", "roof", "cancelled", "TAMPERED")

	h := &liCaptureHandler{}
	job := NewLedgerIntegrityJob(store, config.LedgerConfig{AutoRepair: true, IntervalHours: 168}, slog.New(h))
	report, err := job.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Divergent != 1 {
		t.Fatalf("Divergent = %d, want 1 pre-repair", report.Divergent)
	}
	if got := h.count("ledger integrity repaired"); got != 1 {
		t.Errorf("INFO 'ledger integrity repaired' count = %d, want 1", got)
	}

	// Post-repair: the fold and the table agree again.
	rep2, err := store.VerifyState(context.Background())
	if err != nil {
		t.Fatalf("re-verify: %v", err)
	}
	if rep2.Divergent != 0 {
		t.Errorf("after auto-repair: Divergent = %d, want 0", rep2.Divergent)
	}
	st, ok, err := store.GetState(context.Background(), "emp-d", "roof")
	if err != nil || !ok {
		t.Fatalf("GetState after repair: ok=%v err=%v", ok, err)
	}
	if st.Status != ledger.StatusOpen || st.Summary != "needs quote" {
		t.Errorf("repaired state = %+v, want fold truth open/needs quote", st)
	}
}

// ---------------------------------------------------------------------------
// Guard: nil store -> job never registers; registration + interval default
// ---------------------------------------------------------------------------

func TestLedgerIntegrity_NilStoreRegistersNothing(t *testing.T) {
	c := &Components{
		Config:    config.DefaultConfig(),
		Scheduler: newLedgerTestScheduler(t),
		Logger:    slog.New(slog.DiscardHandler),
		// EmployeeLedger nil: the pinned buildLedger degrade posture.
	}
	c.registerLedgerIntegrityJob(c.Config, slog.New(slog.DiscardHandler))
	if jobs := c.Scheduler.ListJobs(); len(jobs) != 0 {
		t.Fatalf("nil store must register no job, got %d: %+v", len(jobs), jobs)
	}
}

func TestLedgerIntegrity_RegistersJobWithDefaultInterval(t *testing.T) {
	store, _ := openLedgerStoreAt(t)
	cfg := config.DefaultConfig()
	c := &Components{
		Config:         cfg,
		Scheduler:      newLedgerTestScheduler(t),
		Logger:         slog.New(slog.DiscardHandler),
		EmployeeLedger: store,
	}
	c.registerLedgerIntegrityJob(cfg, slog.New(slog.DiscardHandler))

	var found bool
	for _, j := range c.Scheduler.ListJobs() {
		if j.ID == "ledger-integrity" {
			found = true
			want := "@every " + (168 * time.Hour).String()
			if j.Schedule != want {
				t.Errorf("job schedule = %q, want %q", j.Schedule, want)
			}
		}
	}
	if !found {
		t.Fatalf("ledger-integrity job not registered: %+v", c.Scheduler.ListJobs())
	}
}

func TestLedgerIntegrity_ExecuteRunsPass(t *testing.T) {
	// Execute (the scheduler-facing path) must run one clean pass without
	// panicking and expose the pinned job identity.
	store, _ := openLedgerStoreAt(t)
	appendStatusRow(t, store, "emp-e", "lease", ledger.StatusOpen, "renewal due")
	job := &ledgerIntegrityJob{
		id:       "ledger-integrity",
		name:     "test",
		interval: time.Hour,
		runner:   NewLedgerIntegrityJob(store, config.LedgerConfig{}, slog.New(slog.DiscardHandler)),
		logger:   slog.New(slog.DiscardHandler),
	}
	if err := job.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := job.Schedule(); !strings.HasPrefix(got, "@every ") {
		t.Errorf("Schedule() = %q, want @every form", got)
	}
	if jcfg := job.Config(); jcfg.ID != "ledger-integrity" || jcfg.Type != scheduler.JobTypeInterval {
		t.Errorf("Config() = %+v, want id ledger-integrity / type interval", jcfg)
	}
}
