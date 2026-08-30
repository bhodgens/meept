package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/eval"
)

// DefaultEvalRunTimeout bounds each oracle attempt in an eval run.
const DefaultEvalRunTimeout = 60 * time.Second

// EvalHandler serves the eval-run HTTP surface under /api/v1/eval/* and the
// eval.* RPC handler funcs. The wire shape is the frozen C1 RunRecord JSON
// (snake_case); TUI/GUI/menubar clients consume these paths exactly.
type EvalHandler struct {
	store *eval.DiskStore

	// runTimeout bounds each oracle attempt. Zero uses DefaultEvalRunTimeout.
	runTimeout time.Duration

	// newRun is a test seam for the run constructor.
	newRun func(kind eval.Kind, taskID, modelID string, k int) *eval.RunRecord

	logger *slog.Logger
}

// NewEvalHandler creates an eval handler reading/writing dir (conventionally
// <home>/.meept/eval). dir is resolved and injected by the caller.
func NewEvalHandler(dir string, logger *slog.Logger) *EvalHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &EvalHandler{
		store:  eval.NewDiskStore(dir),
		logger: logger,
	}
}

// RegisterRoutes wires the eval routes on the ServeMux.
func (h *EvalHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/eval/runs", h.handleRun)
	mux.HandleFunc("GET /api/v1/eval/runs", h.handleList)
	mux.HandleFunc("GET /api/v1/eval/runs/", h.handleGet)
}

// evalErrorResponse is the JSON error body for eval endpoints.
type evalErrorResponse struct {
	Error string `json:"error"`
}

// writeEvalError writes a JSON error with a lowercase message.
func writeEvalError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(evalErrorResponse{Error: msg})
}

// EvalTrajectoryStep is one optional per-tool step in a run request. When
// the client supplies a trajectory, the run attaches a TrajectoryJudgment
// beside the run record; otherwise the run is unjudged.
type EvalTrajectoryStep struct {
	Name   string `json:"name"`
	Err    string `json:"err,omitempty"`
	Failed bool   `json:"failed"`
}

// EvalRunParams is the shared payload for POST /api/v1/eval/runs and the
// eval.run RPC method (identical JSON per the frozen contract).
type EvalRunParams struct {
	TaskID   string `json:"task_id"`
	ModelID  string `json:"model_id"`
	K        int    `json:"k"`
	Command  string `json:"command"`
	Workdir  string `json:"workdir"`
	ToolList string `json:"tool_list,omitempty"`
	Prompt   string `json:"prompt,omitempty"`

	// Trajectory is optional per-tool step data; non-empty triggers a
	// judgment saved as <run-id>.judgment.json beside the run record.
	Trajectory []EvalTrajectoryStep `json:"trajectory,omitempty"`
}

// validateRunParams checks a run request. Returned errors are lowercase and
// safe to surface verbatim to HTTP (400) and RPC callers alike.
func (h *EvalHandler) validateRunParams(req *EvalRunParams) error {
	if req.K <= 0 {
		return errors.New("k must be positive")
	}
	if req.Command == "" {
		return errors.New("command is required")
	}
	if req.Workdir == "" {
		return errors.New("workdir is required")
	}
	return nil
}

// oracleTimeout returns the per-attempt timeout: DefaultEvalRunTimeout
// unless the test seam set runTimeout.
func (h *EvalHandler) oracleTimeout() time.Duration {
	if h.runTimeout > 0 {
		return h.runTimeout
	}
	return DefaultEvalRunTimeout
}

// runRecord executes the oracle K times synchronously against req and
// returns the completed, unsaved RunRecord. pass^k short-circuits on the
// first failed attempt. Shared by the HTTP and RPC run paths.
func (h *EvalHandler) runRecord(ctx context.Context, req *EvalRunParams) (*eval.RunRecord, error) {
	timeout := h.oracleTimeout()

	oracleName := "shell:" + req.Command
	newRun := h.newRun
	if newRun == nil {
		newRun = eval.NewRun
	}
	rec := newRun(eval.KindPassK, req.TaskID, req.ModelID, req.K)
	rec.HarnessHash = eval.HarnessHash(req.Prompt, req.ToolList, req.Command)
	rec.OracleName = oracleName

	for i := 0; i < req.K; i++ {
		attempt := eval.Attempt{Index: i, ModelID: req.ModelID}
		res, err := eval.ShellOracle{
			OracleName: oracleName,
			Command:    req.Command,
			Timeout:    timeout,
		}.Check(ctx, req.Workdir)
		if err != nil {
			// Launch failure (e.g. missing workdir) fails this attempt
			// closed and is recorded — not fatal to the run.
			res = eval.OracleResult{Passed: false, Err: err.Error()}
		}
		attempt.Oracle = res
		attempt.Passed = res.Passed
		rec.AddAttempt(attempt)
		// No short-circuit: the contract runs the oracle exactly K times.
		// pass^k then requires all K attempts to pass (last k == all k).
	}
	rec.Passed = eval.PassK(rec.Attempts, rec.K)
	return rec, nil
}

// stepsFromEvalParams converts the optional request trajectory into judge
// steps. The request already carries the minimal per-step shape, so this is
// a straight element-wise copy.
func stepsFromEvalParams(req *EvalRunParams) []eval.Step {
	steps := make([]eval.Step, 0, len(req.Trajectory))
	for _, s := range req.Trajectory {
		steps = append(steps, eval.Step{Name: s.Name, Err: s.Err, Failed: s.Failed})
	}
	return steps
}

// attachJudgment computes and persists the C7 TrajectoryJudgment for a run
// carrying a trajectory. Attachment is best-effort: the judgment reuses the
// run's final attempt verdict rather than re-running the command, and a save
// failure is logged, not fatal — the run record itself is already persisted.
// Runs without a trajectory are skipped (unjudged).
func (h *EvalHandler) attachJudgment(ctx context.Context, rec *eval.RunRecord, steps []eval.Step, workdir string) {
	if len(steps) == 0 {
		return
	}
	oracle := finalAttemptOracle(rec)
	j, err := eval.Judge(ctx, steps, oracle, workdir)
	if err != nil {
		h.logger.Error("eval: judge trajectory", "error", err, "run_id", rec.ID)
		return
	}
	j.TrajectoryID = rec.ID
	if err := h.store.SaveJudgment(ctx, j); err != nil {
		h.logger.Error("eval: save judgment", "error", err, "run_id", rec.ID)
		return
	}
	h.logger.Info("eval: judgment attached", "run_id", rec.ID, "passed", j.Passed, "first_error_step", j.FirstErrorStep)
}

// finalAttemptOracle returns an oracle whose Check replays the FINAL
// attempt's persisted verdict instead of re-executing the command: the
// K-attempt run already exercised it, and the judgment must describe the
// outcome that was actually recorded.
func finalAttemptOracle(rec *eval.RunRecord) eval.Oracle {
	res := eval.OracleResult{}
	if n := len(rec.Attempts); n > 0 {
		res = rec.Attempts[n-1].Oracle
	}
	return replayOracle{name: rec.OracleName, res: res}
}

// replayOracle is a fixed-verdict oracle; Check never executes anything.
type replayOracle struct {
	name string
	res  eval.OracleResult
}

func (o replayOracle) Name() string { return o.name }

func (o replayOracle) Check(_ context.Context, _ string) (eval.OracleResult, error) {
	return o.res, nil
}

// handleRun implements POST /api/v1/eval/runs: validate, run the oracle K
// times synchronously, persist, and return the RunRecord. Validation
// failures are 400 with a lowercase message.
func (h *EvalHandler) handleRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req EvalRunParams
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeEvalError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := h.validateRunParams(&req); err != nil {
		writeEvalError(w, http.StatusBadRequest, err.Error())
		return
	}

	rec, err := h.runRecord(r.Context(), &req)
	if err != nil {
		h.logger.Error("eval: run failed", "error", err)
		writeEvalError(w, http.StatusInternalServerError, "internal error: run failed")
		return
	}
	if err := h.store.Save(r.Context(), *rec); err != nil {
		h.logger.Error("eval: save run record", "error", err, "run_id", rec.ID)
		writeEvalError(w, http.StatusInternalServerError, "internal error: failed to save run record")
		return
	}

	// Best-effort judgment attachment for runs carrying a trajectory.
	h.attachJudgment(r.Context(), rec, stepsFromEvalParams(&req), req.Workdir)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rec)
}

// handleList implements GET /api/v1/eval/runs.
func (h *EvalHandler) handleList(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.List(r.Context())
	if err != nil {
		h.logger.Error("eval: list run records", "error", err)
		writeEvalError(w, http.StatusInternalServerError, "internal error: failed to list run records")
		return
	}
	if runs == nil {
		runs = []eval.RunRecord{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"runs": runs})
}

// handleGet implements GET /api/v1/eval/runs/{id}. Unknown ids are 404 via
// errors.Is(eval.ErrRunNotFound).
func (h *EvalHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/eval/runs/")
	if id == "" {
		writeEvalError(w, http.StatusNotFound, "run not found")
		return
	}

	rec, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, eval.ErrRunNotFound) {
			writeEvalError(w, http.StatusNotFound, "run not found: "+id)
			return
		}
		h.logger.Error("eval: get run record", "error", err, "run_id", id)
		writeEvalError(w, http.StatusInternalServerError, "internal error: failed to read run record")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rec)
}

// EvalRPCHandlers returns the eval.run / eval.show / eval.list handler
// funcs sharing this handler's DiskStore and run engine. The daemon wiring
// registers them directly on the RPC server (daemon/eval_rpc.go).
func (h *EvalHandler) EvalRPCHandlers() map[string]func(ctx context.Context, params json.RawMessage) (any, error) {
	return map[string]func(ctx context.Context, params json.RawMessage) (any, error){
		"eval.run":  h.rpcHandleRun(),
		"eval.show": h.rpcHandleShow(),
		"eval.list": h.rpcHandleList(),
	}
}

// rpcHandleRun implements eval.run: identical payload and semantics to POST
// /api/v1/eval/runs (synchronous, pass^k short-circuit), minus HTTP plumbing.
func (h *EvalHandler) rpcHandleRun() func(ctx context.Context, params json.RawMessage) (any, error) {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req EvalRunParams
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if err := h.validateRunParams(&req); err != nil {
			return nil, err
		}
		rec, err := h.runRecord(ctx, &req)
		if err != nil {
			return nil, fmt.Errorf("run failed: %w", err)
		}
		if err := h.store.Save(ctx, *rec); err != nil {
			return nil, fmt.Errorf("save run record: %w", err)
		}

		// Best-effort judgment attachment for runs carrying a trajectory.
		h.attachJudgment(ctx, rec, stepsFromEvalParams(&req), req.Workdir)

		return rec, nil
	}
}

// rpcHandleShow implements eval.show.
func (h *EvalHandler) rpcHandleShow() func(ctx context.Context, params json.RawMessage) (any, error) {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		rec, err := h.store.Get(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		return rec, nil
	}
}

// rpcHandleList implements eval.list.
func (h *EvalHandler) rpcHandleList() func(ctx context.Context, params json.RawMessage) (any, error) {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		runs, err := h.store.List(ctx)
		if err != nil {
			return nil, err
		}
		if runs == nil {
			runs = []eval.RunRecord{}
		}
		return map[string]any{"runs": runs}, nil
	}
}
