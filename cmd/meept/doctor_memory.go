package main

// doctor_memory.go: the `meept doctor` memory-pipeline checks (leaf 06 of
// docs/plans/20260930-memory-second-brain → 20261030-memory-second-brain).
//
// Report-only (doctor convention + user normative rule): these checks read
// files and read-only SQLite connections, never write, install, or mutate.
// Every degraded pipeline state is a WARN, never a failure — doctor must
// stay green on a models-less host, a fresh home with no turns yet, and a
// daemon that is currently down.
//
// Checks (names are the leaf contract, verbatim):
//   memory-capture  — l0_enabled + latest l0 file date + line count
//   memory-distill  — pending l1/l2 job counts + dead-letter count
//                     (queue stats path; ro-SQL fallback when the daemon
//                     is down) + memory_model resolvability
//   memory-recall   — recall_outcomes.jsonl presence + last-row age
//   memory-extras   — knowledge enabled + page count; bridges enabled
//
// A fifth line, "memory-pipeline", is the gate-visible summary (F7 asserts
// a line naming both "memory-pipeline" and the l0 status).

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/transport"
	_ "modernc.org/sqlite" //nolint:revive // blank import for the "sqlite" driver
)

// memoryDoctorConfigHook is the config seam the memory checks read through
// (injected by tests; production loads the default config). Same pattern as
// groupsDoctorConfigHook.
var memoryDoctorConfigHook = func() *config.Config {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil
	}
	return cfg
}

// memoryQueueTypeScanLimit caps the pending-jobs list the stats path scans
// for the per-type split. The queue list caps at this too (detail says "at
// least" when the backlog truncates), so the number is reported honestly.
const memoryQueueTypeScanLimit = 500

// memoryJobCounts is the distilled-queue view memory-distill reports.
type memoryJobCounts struct {
	pendingL1  int
	pendingL2  int
	pendingAll int
	dead       int
	scanLimit  bool // pending list truncated at memoryQueueTypeScanLimit
}

// checkMemoryDoctor assembles the memory-pipeline check block.
func checkMemoryDoctor() []doctorCheck {
	cfg := memoryDoctorConfigHook()
	if cfg == nil {
		return []doctorCheck{{
			name:   "memory-pipeline",
			ok:     true,
			warn:   true,
			detail: "no config loaded; memory pipeline checks skipped",
		}}
	}

	capture := checkMemoryCaptureDoctor(cfg)
	distill := checkMemoryDistillDoctor(cfg)
	recall := checkMemoryRecallDoctor(cfg)
	extras := checkMemoryExtrasDoctor(cfg)

	anyWarn := capture.warn || distill.warn || recall.warn || extras.warn
	summary := doctorCheck{
		name: "memory-pipeline",
		ok:   true,
		warn: anyWarn,
		detail: fmt.Sprintf("l0 %s; distill dead=%d; recall %s; knowledge %s; bridges %s",
			l0StatePhrase(cfg.Memory.L0Enabled, capture.detail),
			memoryDistillDeadFromDetail(distill.detail),
			recallPhrase(recall.detail),
			knowledgePhrase(extras.detail),
			bridgesPhrase(extras.detail),
		),
	}

	return []doctorCheck{summary, capture, distill, recall, extras}
}

// --- memory-capture ---------------------------------------------------------

// checkMemoryCaptureDoctor reports l0 capture state: armed or disabled, the
// latest daily file's date, and the total line count. Enabled-but-no-file-today
// and enabled-but-no-files-at-all are warns (armed-but-silent pipeline), never
// failures.
func checkMemoryCaptureDoctor(cfg *config.Config) doctorCheck {
	dataDir := cfg.Memory.DataDir
	if dataDir == "" {
		dataDir = config.MeeptPath("memory")
	}
	if !cfg.Memory.L0Enabled {
		return doctorCheck{
			name:   "memory-capture",
			ok:     true,
			detail: "l0 capture disabled (memory.l0_enabled=false)",
		}
	}
	latest, lines, files, err := l0CaptureStats(filepath.Join(dataDir, "l0"))
	if err != nil {
		return doctorCheck{
			name:   "memory-capture",
			ok:     true,
			warn:   true,
			detail: "l0 armed but capture dir unreadable (" + err.Error() + ")",
		}
	}
	if files == 0 {
		return doctorCheck{
			name:   "memory-capture",
			ok:     true,
			warn:   true,
			detail: "l0 armed but no capture files yet (no turns recorded since enablement)",
		}
	}
	today := memory.LocalDayKey(time.Now())
	if latest != today {
		return doctorCheck{
			name: "memory-capture",
			ok:   true,
			warn: true,
			detail: fmt.Sprintf("l0 armed but no capture today; latest %s, %d line(s) across %d file(s)",
				latest, lines, files),
		}
	}
	return doctorCheck{
		name: "memory-capture",
		ok:   true,
		detail: fmt.Sprintf("l0 armed; latest capture today (%s), %d line(s) across %d file(s)",
			latest, lines, files),
	}
}

// l0CaptureStats scans the daily YYYY-MM-DD.jsonl files under dir (the frozen
// l0 layout) and returns the newest day key, the total line count, and the
// file count. Read-only.
func l0CaptureStats(dir string) (latest string, lines int, files int, err error) {
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", 0, 0, nil // no dir yet: caller renders the armed-but-empty warn
		}
		return "", 0, 0, readErr
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dateStr, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok {
			continue
		}
		if _, parseErr := time.Parse("2006-01-02", dateStr); parseErr != nil {
			continue // not a daily file; skip
		}
		files++
		if latest == "" || dateStr > latest {
			latest = dateStr
		}
		n, countErr := countLines(filepath.Join(dir, e.Name()))
		if countErr != nil {
			return "", 0, 0, countErr
		}
		lines += n
	}
	return latest, lines, files, nil
}

// --- memory-distill ---------------------------------------------------------

// checkMemoryDistillDoctor reports pending l1/l2 job counts, the dead-letter
// count, and memory_model resolvability. Dead-lettered jobs and an
// unresolvable memory_model are warns (the pipeline degrades, it does not
// fail): extraction is skipped with one Warn per cycle and L0 lines are
// retained, so nothing is lost.
func checkMemoryDistillDoctor(cfg *config.Config) doctorCheck {
	counts, err := memoryQueueDoctorCounts(cfg)
	if err != nil {
		return doctorCheck{
			name:   "memory-distill",
			ok:     true,
			warn:   true,
			detail: "queue stats unavailable (" + err.Error() + ")",
		}
	}
	detail := fmt.Sprintf("pending l1=%d l2=%d (of %d pending), dead=%d",
		counts.pendingL1, counts.pendingL2, counts.pendingAll, counts.dead)
	if counts.scanLimit {
		detail += " [pending list truncated at scan limit]"
	}
	warn := counts.dead > 0
	if warn {
		detail += "; dead-lettered jobs need attention (`meept queue` to inspect)"
	}

	modelWarn, modelNote := memoryModelDoctorNote()
	if modelWarn {
		warn = true
	}
	detail += "; " + modelNote

	return doctorCheck{name: "memory-distill", ok: true, warn: warn, detail: detail}
}

// memoryQueueDoctorCounts reads the distill queue through the stats path:
// the daemon's queue.stats + queue.list RPC when it is reachable (the same
// surfaces `meept queue status` uses — no second store handle), falling back
// to a read-only SQLite peek over <daemon.data_dir>/queue.db when the daemon
// is down (doctor must work daemon-down; mode=ro over the daemon's WAL db is
// the same pattern `meept knowledge stats` uses).
func memoryQueueDoctorCounts(cfg *config.Config) (*memoryJobCounts, error) {
	client, err := connectDaemon()
	if err == nil {
		counts, statsErr := memoryQueueCountsFromStats(client)
		client.Close()
		if statsErr == nil {
			return counts, nil
		}
		// fall through to the ro-SQL path
	}

	dbPath := cfg.Queue.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(cfg.Daemon.DataDir, "queue.db")
	}
	return memoryQueueCountsFromDB(dbPath)
}

// memoryQueueCountsFromStats derives the counts from the live queue stats +
// list RPCs. A truncated job list is reported, not hidden.
func memoryQueueCountsFromStats(client transport.Client) (*memoryJobCounts, error) {
	stats, err := client.GetQueueStats()
	if err != nil {
		return nil, err
	}
	counts := &memoryJobCounts{dead: stats.DeadCount}
	counts.pendingAll = stats.ByState["pending"]
	jobs, err := client.ListQueueJobs("pending", memoryQueueTypeScanLimit)
	if err != nil {
		// Stats still gave the totals; the per-type split is best-effort.
		counts.scanLimit = true
		return counts, nil
	}
	for _, j := range jobs.Jobs {
		if j.State != "pending" {
			continue
		}
		switch j.Type {
		case "memory.l1_extract":
			counts.pendingL1++
		case "memory.l2_scene":
			counts.pendingL2++
		}
	}
	if counts.pendingAll > memoryQueueTypeScanLimit {
		counts.scanLimit = true
	}
	return counts, nil
}

// memoryQueueCountsFromDB reads the same numbers the stats RPC would return,
// directly and read-only, from the persistent queue's SQLite file.
func memoryQueueCountsFromDB(dbPath string) (*memoryJobCounts, error) {
	if _, statErr := os.Stat(dbPath); statErr != nil {
		if os.IsNotExist(statErr) {
			// The daemon has never run on this home: nothing to report is
			// the truth, not a degraded state.
			return &memoryJobCounts{}, nil
		}
		return nil, statErr
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close() //nolint:errcheck // read-only stats peek

	counts := &memoryJobCounts{}
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state = 'pending'`).Scan(&counts.pendingAll); err != nil {
		return nil, fmt.Errorf("count pending: %w", err)
	}
	rows, err := db.Query(
		`SELECT type, COUNT(*) FROM jobs WHERE state = 'pending' AND type IN ('memory.l1_extract','memory.l2_scene') GROUP BY type`)
	if err != nil {
		return nil, fmt.Errorf("count pending by type: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int
		if scanErr := rows.Scan(&typ, &n); scanErr != nil {
			continue
		}
		switch typ {
		case "memory.l1_extract":
			counts.pendingL1 = n
		case "memory.l2_scene":
			counts.pendingL2 = n
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM dead_letter`).Scan(&counts.dead); err != nil {
		// dead_letter always exists (schema bootstrap); a failure here is a
		// genuinely unreadable db.
		return nil, fmt.Errorf("count dead letters: %w", err)
	}
	return counts, nil
}

// memoryModelDoctorNote resolves the memory_model slot the way the daemon
// does at boot: alias members first (llm providers config), then a direct
// provider/model reference in models.json5. Empty slot = the documented
// fallback to the general chat client (ok, not a warn).
func memoryModelDoctorNote() (warn bool, note string) {
	modelsCfg, err := config.LoadModelsConfigDefault()
	if err != nil {
		return true, "memory_model resolution unknown (models config unreadable: " + err.Error() + ")"
	}
	ref := strings.TrimSpace(modelsCfg.MemoryModel)
	if ref == "" {
		return false, "memory_model unset (distill uses the general chat client)"
	}

	// Alias path: the llm providers config carries model_aliases.
	if providersCfg, perr := llm.LoadProvidersConfigDefault(); perr == nil && providersCfg != nil {
		if entry, ok := providersCfg.ModelAliases[ref]; ok && len(entry.Models) > 0 {
			return false, fmt.Sprintf("memory_model %s resolves via alias (%d member(s))", ref, len(entry.Models))
		}
	}

	// Direct provider/model reference in models.json5.
	if parts := strings.SplitN(ref, "/", 2); len(parts) == 2 {
		if p, ok := modelsCfg.Providers[parts[0]]; ok {
			if _, ok = p.Models[parts[1]]; ok {
				return false, "memory_model " + ref + " resolves directly (models.json5)"
			}
		}
	}

	return true, fmt.Sprintf("memory_model %s unresolvable; distill jobs stay queued until a provider resolves (fallback: general chat client)", ref)
}

// --- memory-recall ----------------------------------------------------------

// checkMemoryRecallDoctor reports recall_outcomes.jsonl presence and the
// last row's age (telemetry armed). A missing file on a fresh install is the
// normal pre-first-turn state (ok); only an unreadable/unparseable file warns.
func checkMemoryRecallDoctor(cfg *config.Config) doctorCheck {
	dataDir := cfg.Memory.DataDir
	if dataDir == "" {
		dataDir = config.MeeptPath("memory")
	}
	path := filepath.Join(dataDir, "recall_outcomes.jsonl")
	rows, err := memory.OutcomeRows(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{
				name:   "memory-recall",
				ok:     true,
				detail: "no recall outcomes yet (telemetry armed; no turns with injected memories)",
			}
		}
		return doctorCheck{
			name:   "memory-recall",
			ok:     true,
			warn:   true,
			detail: "recall outcomes file unreadable (" + err.Error() + ")",
		}
	}
	if len(rows) == 0 {
		return doctorCheck{
			name:   "memory-recall",
			ok:     true,
			detail: "recall outcomes file present but empty (telemetry armed)",
		}
	}
	last := rows[len(rows)-1]
	ts, parseErr := time.Parse(time.RFC3339, last.TS)
	if parseErr != nil {
		return doctorCheck{
			name:   "memory-recall",
			ok:     true,
			warn:   true,
			detail: fmt.Sprintf("%d recall outcome row(s); last row timestamp unparseable", len(rows)),
		}
	}
	return doctorCheck{
		name:   "memory-recall",
		ok:     true,
		detail: fmt.Sprintf("%d recall outcome row(s); last recorded %s ago", len(rows), humanizeAge(ts)),
	}
}

// --- memory-extras ----------------------------------------------------------

// checkMemoryExtrasDoctor reports the knowledge-asset store arm state and
// page count, and the bridges arm state. Both read-only (the page count is a
// mode=ro SQLite peek over the store's db — the same read `meept knowledge
// stats` does; the daemon holds the store open in WAL mode, so concurrent
// reads are safe).
func checkMemoryExtrasDoctor(cfg *config.Config) doctorCheck {
	kCfg := cfg.Memory.Knowledge
	bCfg := cfg.Memory.Bridges

	var detail string
	warn := false
	if !kCfg.Enabled {
		detail = "knowledge store disabled (memory.knowledge.enabled=false); "
	} else {
		dbPath := filepath.Join(cfg.Daemon.DataDir, "knowledge", "knowledge.db")
		pages, err := knowledgePageCount(dbPath)
		if err != nil {
			detail = fmt.Sprintf("knowledge store on; pages unknown (%s); ", err.Error())
			warn = true
		} else {
			detail = fmt.Sprintf("knowledge store on; pages: %d; ", pages)
		}
	}

	if bCfg.Enabled {
		detail += "bridges on"
	} else {
		detail += "bridges off (memory.bridges.enabled=false)"
	}

	return doctorCheck{name: "memory-extras", ok: true, warn: warn, detail: detail}
}

// knowledgePageCount reads the page count from the knowledge store db,
// read-only. A db that does not exist yet is zero pages (the store is
// created on the daemon's first consolidation pass), not an error.
func knowledgePageCount(dbPath string) (int, error) {
	if _, statErr := os.Stat(dbPath); statErr != nil {
		if os.IsNotExist(statErr) {
			return 0, nil
		}
		return 0, statErr
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close() //nolint:errcheck // read-only stats peek
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_pages`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count pages: %w", err)
	}
	return n, nil
}

// --- summary-line phrase helpers --------------------------------------------
//
// The summary doctorCheck renders one phrase per sub-check so the F7 gate
// can find a single "memory-pipeline" line naming the l0 status. The phrase
// helpers parse only the structured details their own check produced above
// (same file, same package — no cross-file contract).

func l0StatePhrase(enabled bool, captureDetail string) string {
	if !enabled {
		return "off"
	}
	if strings.Contains(captureDetail, "no capture files yet") || strings.Contains(captureDetail, "no capture today") {
		return "on (no capture today)"
	}
	return "on"
}

func recallPhrase(recallDetail string) string {
	if strings.Contains(recallDetail, "no recall outcomes yet") || strings.Contains(recallDetail, "empty") {
		return "no outcomes"
	}
	if strings.Contains(recallDetail, "row(s)") {
		return "recording"
	}
	return "unknown"
}

func knowledgePhrase(extrasDetail string) string {
	if strings.Contains(extrasDetail, "knowledge store disabled") {
		return "off"
	}
	if strings.Contains(extrasDetail, "knowledge store on") {
		return "on"
	}
	return "unknown"
}

func bridgesPhrase(extrasDetail string) string {
	if strings.HasSuffix(extrasDetail, "bridges off (memory.bridges.enabled=false)") {
		return "off"
	}
	if strings.HasSuffix(extrasDetail, "bridges on") {
		return "on"
	}
	return "unknown"
}

// memoryDistillDeadFromDetail extracts the dead-letter count the distill
// check's own detail carries ("... dead=N ..."). A parse miss renders 0 —
// the authoritative number always lives on the memory-distill line.
func memoryDistillDeadFromDetail(detail string) int {
	i := strings.Index(detail, "dead=")
	if i < 0 {
		return 0
	}
	rest := detail[i+len("dead="):]
	end := strings.IndexAny(rest, " ;")
	if end < 0 {
		end = len(rest)
	}
	n := 0
	for _, r := range rest[:end] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
