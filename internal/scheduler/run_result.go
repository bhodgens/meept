package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// JobExecutionResult is a settled execution attempt in the scheduler's result
// lane. Output is NOT stored here: it spills to a file under
// $MEEPT_HOME/spill/jobs/<handle>.out and this row only keeps the small
// bookkeeping needed to find and bound it. Keys are JobID (the handle); a
// recurring job's successive runs overwrite by design (job_status takes only
// a handle, so it can only report the latest attempt).
type JobExecutionResult struct {
	JobID        string    `json:"job_id"`
	ExitCode     int       `json:"exit_code"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	OutputPath   string    `json:"output_path,omitempty"`
	OutputBytes  int64     `json:"output_bytes,omitempty"`
	OutputSHA256 string    `json:"output_sha256,omitempty"`
	// TruncatedTo is reserved for a bounded inline view. The file-backed
	// lane never truncates the spill file, so writers leave it at 0;
	// readers report how many bytes they actually returned.
	TruncatedTo int64  `json:"truncated_to,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Settled result statuses written to the lane.
const (
	JobStatusSucceeded = "succeeded"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
)

// Unix-consistent exit codes exposed to other agents (contract from the
// job-tools design). N>1 process exits are preserved verbatim.
const (
	ExitCodeOK      = 0
	ExitCodeFailure = 1
	ExitCodeSIGKILL = 137 // SIGKILL-equivalent: group kill or timeout kill
	ExitCodeSIGTERM = 143 // SIGTERM-equivalent: grace-period kill
	ExitCodeUnknown = -1  // unset / signal death with no mapping
)

// DefaultJobResultOutputBytes is the bounded window job_status reads back
// from a spill file on demand.
const DefaultJobResultOutputBytes = 4096

// resultLaneCap bounds the persisted result lane (FIFO eviction). Matches the
// workflow job cap used elsewhere in the daemon.
const resultLaneCap = 256

// ShellCommandGuard is the two-phase execution-time fence for shell jobs.
// Phase 1 (CheckCommand) is risk/permission classification and runs before
// work-dir resolution; phase 2 (CheckCommandInDir) is the fence/Tirith/
// network-class check and runs after resolution. Both must run BEFORE the
// command is spawned. The sibling security leaf implements this interface;
// this package deliberately does not reimplement risk logic.
type ShellCommandGuard interface {
	CheckCommand(command string) error
	CheckCommandInDir(ctx context.Context, command, workDir string) error
}

// ResultSink is the minimal result-lane surface ShellJob needs. *Store cannot
// implement it directly because its existing Get(id) (JobConfig, bool) method
// collides with Get(handle) (JobExecutionResult, bool); resultStore adapts it
// without overloading that method.
type ResultSink interface {
	Put(result JobExecutionResult)
	Get(handle string) (JobExecutionResult, bool)
}

// resultStore adapts a Store's result lane to ResultSink.
type resultStore struct{ s *Store }

func (r *resultStore) Put(result JobExecutionResult) { r.s.PutResult(result) }
func (r *resultStore) Get(handle string) (JobExecutionResult, bool) {
	return r.s.GetResult(handle)
}

// executionHandle is one in-flight execution keyed by handle. cancel is a
// per-execution CancelFunc (never the scheduler-global runNowCancel). cmd is
// published by ShellJob.Execute so CancelJob can escalate to a group kill.
type executionHandle struct {
	cancel  context.CancelFunc
	started time.Time
	done    chan struct{}

	mu  sync.Mutex
	cmd *exec.Cmd
}

func (h *executionHandle) setCmd(cmd *exec.Cmd) {
	h.mu.Lock()
	h.cmd = cmd
	h.mu.Unlock()
}

// killGroup escalates a cancelled execution to a process-group kill, then
// falls back to the process itself. Mirrors pty.KillProcessTree but is kept
// inline so the low-level scheduler package gains no import edge.
func (h *executionHandle) killGroup() error {
	h.mu.Lock()
	cmd := h.cmd
	h.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if pid > 0 && cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		// A failed group kill (ESRCH) is benign: Process.Kill below is the
		// authoritative termination step.
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	return cmd.Process.Kill()
}

// executionHandleKey carries the in-flight handle through the execution
// context so ShellJob.Execute can publish its *exec.Cmd for CancelJob.
type executionHandleKey struct{}

// upsertResults merges incoming settled results into the lane by JobID
// (latest wins), preserving insertion order and capping to resultLaneCap.
// Used by Import to round-trip the lane.
func upsertResults(existing, incoming []JobExecutionResult) []JobExecutionResult {
	merged := append([]JobExecutionResult(nil), existing...)
	for _, in := range incoming {
		found := false
		for i := range merged {
			if merged[i].JobID == in.JobID {
				merged[i] = in
				found = true
				break
			}
		}
		if !found {
			merged = append(merged, in)
		}
	}
	if over := len(merged) - resultLaneCap; over > 0 {
		merged = merged[over:]
	}
	return merged
}

// PutResult appends or replaces a settled result and persists the lane. When
// an entry is replaced or evicted past the FIFO cap its spill file is deleted
// best-effort AFTER the mutex is released (no I/O under lock).
func (s *Store) PutResult(result JobExecutionResult) {
	s.mu.Lock()
	var evict []string
	replaced := false
	for i, existing := range s.results {
		if existing.JobID == result.JobID {
			if p := existing.OutputPath; p != "" && p != result.OutputPath {
				evict = append(evict, p)
			}
			s.results[i] = result
			replaced = true
			break
		}
	}
	if !replaced {
		s.results = append(s.results, result)
	}
	if over := len(s.results) - resultLaneCap; over > 0 {
		for _, r := range s.results[:over] {
			if r.OutputPath != "" {
				evict = append(evict, r.OutputPath)
			}
		}
		s.results = s.results[over:]
	}
	// Persist under the same lock that guards s.results; mirrors the existing
	// full-snapshot saveUnlocked path. Spill-file deletion happens below,
	// outside the lock.
	if err := s.persistUnlocked(); err != nil { //nolint:mutexio // snapshot persist mirrors existing Add/Save; file deletes are post-unlock
		slog.Error("scheduler: failed to persist result lane", "error", err)
	}
	s.mu.Unlock()

	for _, p := range evict {
		_ = os.Remove(p) // best-effort cleanup of evicted spill files
	}
}

// GetResult returns the latest settled result for a handle.
func (s *Store) GetResult(handle string) (JobExecutionResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return lookupResultUnlocked(s.results, handle)
}

func lookupResultUnlocked(results []JobExecutionResult, handle string) (JobExecutionResult, bool) {
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].JobID == handle {
			return results[i], true
		}
	}
	return JobExecutionResult{}, false
}

// PutResult writes a settled result into the scheduler's result lane.
// Exported for crash-recovery callers (markOneShotFailed) and CancelJob's
// synthetic cancelled result.
func (s *Scheduler) PutResult(result JobExecutionResult) {
	if s.store != nil {
		s.store.PutResult(result)
	}
}

// GetJobResult returns the latest settled result for a handle. Exported
// scheduler-facing accessor used by job_status (and crash recovery).
func (s *Scheduler) GetJobResult(handle string) (JobExecutionResult, bool) {
	if s.store == nil {
		return JobExecutionResult{}, false
	}
	return s.store.GetResult(handle)
}

// JobResultOutput reads at most maxBytes from a settled job's spill file. A
// non-positive maxBytes uses DefaultJobResultOutputBytes. The returned slice
// is the bounded on-demand window; the full output stays on disk until the
// lane entry is evicted.
func (s *Scheduler) JobResultOutput(handle string, maxBytes int) ([]byte, error) {
	if s.store == nil {
		return nil, fmt.Errorf("no scheduler result store")
	}
	result, ok := s.store.GetResult(handle)
	if !ok {
		return nil, fmt.Errorf("no result for job: %s", handle)
	}
	if result.OutputPath == "" {
		return nil, nil
	}
	if maxBytes <= 0 {
		maxBytes = DefaultJobResultOutputBytes
	}
	f, err := os.Open(result.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("open job output %s: %w", result.OutputPath, err)
	}
	defer f.Close()
	out, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return nil, fmt.Errorf("read job output %s: %w", result.OutputPath, err)
	}
	return out, nil
}

// CancelJob cancels the in-flight execution for handle, waits a 2s grace for
// it to settle, then escalates to a process-group kill. It is idempotent: an
// already-settled handle returns "already_done" as a successful no-op.
//
// status is one of "cancelled"/"succeeded"/"failed" (settled), "already_done"
// or "not_found". stopped reports whether an in-flight run was actually
// cancelled. exitCode is the settled exit code when available.
func (s *Scheduler) CancelJob(handle, reason string) (status string, stopped bool, exitCode int, err error) {
	s.execMu.Lock()
	h, ok := s.executions[handle]
	s.execMu.Unlock()
	if !ok {
		if _, settled := s.GetJobResult(handle); settled {
			return "already_done", false, ExitCodeOK, nil
		}
		return "not_found", false, ExitCodeUnknown, fmt.Errorf("job not found or not running: %s", handle)
	}

	h.cancel()
	select {
	case <-h.done:
		// Settled within the grace period; no escalation needed.
	case <-time.After(2 * time.Second):
		_ = h.killGroup()
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
			slog.Warn("scheduler: job did not settle after group kill", "job_id", handle)
		}
	}

	// Remove the execution entry on terminal (defensive: executeJob's defer
	// normally does this too; the identity check prevents deleting a newer run).
	s.execMu.Lock()
	if s.executions[handle] == h {
		delete(s.executions, handle)
	}
	s.execMu.Unlock()

	if result, settled := s.GetJobResult(handle); settled {
		return result.Status, true, result.ExitCode, nil
	}
	// Non-shell kinds do not write a result lane entry; synthesize a settled
	// cancelled result so job_status never shows a silent gap.
	s.PutResult(JobExecutionResult{
		JobID:      handle,
		Status:     JobStatusCancelled,
		ExitCode:   ExitCodeSIGTERM,
		StartedAt:  h.started,
		FinishedAt: time.Now().UTC(),
		Error:      reason,
	})
	return JobStatusCancelled, true, ExitCodeSIGTERM, nil
}

// jobSpillPath returns the per-handle spill path. filepath.Base keeps a
// malformed handle from escaping the spill directory.
func jobSpillPath(handle string) string {
	return filepath.Join(config.MeeptPath("spill", "jobs"), filepath.Base(handle)+".out")
}

// prepareSpill creates and truncates the spill file for a new attempt,
// returning the open handle and its path. Both stdout and stderr are pointed
// at this single handle by ShellJob.Execute.
func prepareSpill(handle string) (*os.File, string, error) {
	dir := config.MeeptPath("spill", "jobs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("create job spill dir: %w", err)
	}
	path := jobSpillPath(handle)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // G304/G703: path is under MEEPT_HOME/spill/jobs, handle is base-cleaned
	if err != nil {
		return nil, "", fmt.Errorf("create job spill file: %w", err)
	}
	return f, path, nil
}

// spillStats stats and streams a hash of a spill file without loading it into
// memory. Called after the file handle is closed so the size is final.
func spillStats(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// readSpillPreview reads a bounded preview of a spill file for the bus event,
// which must keep its historical JobResult shape (including Output) without
// pulling an unbounded command output into memory.
func readSpillPreview(path string, n int) string {
	if n <= 0 || path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(n)))
	if err != nil {
		return ""
	}
	return string(b)
}
