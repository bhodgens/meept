package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/rpc"
)

// health_rpc.go registers daemon.health and daemon.shutdown RPC methods
// (loop-economics/06-doctor-lifecycle).
//
// daemon.health -> {ok, checks:[{name,ok,detail}...], version, uptime_s}
// daemon.shutdown {drain_timeout_s?} -> {accepted} schedules a graceful stop.

// HealthRPCDeps carries what the handlers need from the Daemon. Kept as an
// interface-free struct of fields so tests can construct it directly.
type HealthRPCDeps struct {
	SocketPath string
	PIDFile    string
	StateDir   string
	DBPaths    []string
	ConfigPath string
	StartTime  time.Time
	RuntimeProcsFunc
}

// RegisterHealthMethods wires daemon.health and daemon.shutdown onto server.
// shutdownFn (optional) schedules the graceful stop; when nil, daemon.shutdown
// falls back to sending SIGTERM to the current process, which Run() handles.
func RegisterHealthMethods(server *rpc.Server, deps HealthRPCDeps, shutdownFn func(drain time.Duration)) {
	if server == nil {
		return
	}

	server.RegisterHandler("daemon.health", func(ctx context.Context, params json.RawMessage) (any, error) {
		checks := RunHealthChecks(HealthOptions{
			SocketPath:   deps.SocketPath,
			PIDFile:      deps.PIDFile,
			StateDir:     deps.StateDir,
			DBPaths:      deps.DBPaths,
			ConfigPath:   deps.ConfigPath,
			StartTime:    deps.StartTime,
			RuntimeProcs: listRuntimeProcs,
			Now:          time.Now,
		})
		ok := true
		for _, c := range checks {
			if c.Status == StatusFail {
				ok = false
				break
			}
		}
		return map[string]any{
			"ok":       ok,
			"checks":   checks,
			"version":  rpcVersion,
			"uptime_s": int64(time.Since(deps.StartTime).Seconds()),
		}, nil
	})

	server.RegisterHandler("daemon.shutdown", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			DrainTimeoutS float64 `json:"drain_timeout_s"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		drain := DefaultDrainTimeout
		if req.DrainTimeoutS > 0 {
			drain = time.Duration(req.DrainTimeoutS * float64(time.Second))
		}
		if shutdownFn != nil {
			go shutdownFn(drain)
		} else {
			// Graceful-stop fallback: SIGTERM to self; the Run loop performs
			// the full shutdown sequence.
			go func() {
				p, err := os.FindProcess(os.Getpid())
				if err == nil {
					if sigErr := p.Signal(syscall.SIGTERM); sigErr != nil {
						slog.Debug("graceful stop: self-signal failed", "error", sigErr)
					}
				}
			}()
		}
		return map[string]any{"accepted": true}, nil
	})
}

// rpcVersion matches the version reported by the builtin daemon.status handler.
const rpcVersion = "0.2.0-go"

// defaultConfigFilePath returns the default user config path used for the
// config-parse health check.
func defaultConfigFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".meept", "meept.json5")
}

// healthDBPaths lists sqlite databases in stateDir worth quick_check-ing.
func healthDBPaths(stateDir string, components *Components) []string {
	candidates := []string{
		filepath.Join(stateDir, "metrics.db"),
	}
	if components != nil {
		if pq, ok := components.Queue.(*queue.PersistentQueue); ok && pq != nil && pq.Store() != nil {
			candidates = append(candidates, filepath.Join(stateDir, "queue.db"))
		}
	}
	var out []string
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
