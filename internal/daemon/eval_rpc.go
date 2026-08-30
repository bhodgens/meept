package daemon

import (
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/rpc"
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
