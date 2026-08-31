package daemon

import (
	"context"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/eval"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/selfimprove"
)

// evalStoreDir returns the absolute eval store directory for the given home.
func evalStoreDir(homeDir string) string {
	return filepath.Join(homeDir, ".meept", "eval")
}

// evalHomeDir returns the user's home directory, best effort. Failure yields
// an empty string; the DiskStore then reports a clear error on first use.
func evalHomeDir() string {
	homeDir, _ := os.UserHomeDir()
	return homeDir
}

// registerEvalRPCHandlers registers eval.run / eval.show / eval.list
// directly on the RPC server, sharing one DiskStore with the HTTP
// /api/v1/eval/* routes (see http.EvalHandler). Direct registration, no
// bus proxy. Lives in the daemon package to avoid an import cycle, same as
// registerThreadRPCHandlers.
func registerEvalRPCHandlers(server *rpc.Server, h *http.EvalHandler) {
	if server == nil || h == nil {
		return
	}
	for method, handler := range h.EvalRPCHandlers() {
		server.RegisterHandler(method, handler)
	}
}

// newEvalJudgmentLoader builds the C7 judgment loader (harness-eval leaf 08)
// for the learning pipeline: it reads the persisted eval.TrajectoryJudgment
// for a trajectory ID from the same DiskStore directory the HTTP and RPC
// eval handlers use. Lives here because the daemon package can import both
// eval and selfimprove, which cannot import each other.
func newEvalJudgmentLoader() func(ctx context.Context, trajectoryID string) (selfimprove.JudgmentOutcome, error) {
	store := eval.NewDiskStore(evalStoreDir(evalHomeDir()))
	return func(ctx context.Context, trajectoryID string) (selfimprove.JudgmentOutcome, error) {
		j, err := store.LoadJudgment(ctx, trajectoryID)
		if err != nil {
			return selfimprove.JudgmentOutcome{}, err
		}
		return selfimprove.JudgmentOutcome{Passed: j.Passed}, nil
	}
}
