package runtime

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
)

// SandboxOrder selects how the daemon chooses an execution backend.
type SandboxOrder string

const (
	// SandboxOrderAuto prefers docker > bwrap > local.
	SandboxOrderAuto SandboxOrder = "auto"
	// SandboxOrderBwrap restricts selection to the bwrap backend (falling to
	// local only when require_sandbox is false).
	SandboxOrderBwrap SandboxOrder = "bwrap"
	// SandboxOrderDocker restricts selection to the docker backend (falling
	// to local only when require_sandbox is false).
	SandboxOrderDocker SandboxOrder = "docker"
	// SandboxOrderLocal explicitly requests unsandboxed local execution.
	SandboxOrderLocal SandboxOrder = "local"
)

// ResolverConfig configures sandboxed-backend resolution for [runtime].
type ResolverConfig struct {
	Order          SandboxOrder `json:"sandbox_backend_order" toml:"sandbox_backend_order"`
	RequireSandbox bool         `json:"require_sandbox"       toml:"require_sandbox"`
}

// ErrSandboxRequired is returned by ResolveBackend when RequireSandbox is
// true and no qualifying backend is available. Callers must REFUSE command
// execution rather than degrade to local exec.
var ErrSandboxRequired = errors.New("runtime: sandbox required but no qualifying backend available")

// Qualifies reports whether a backend name provides OS-level confinement.
func Qualifies(name string) bool {
	return name == "bwrap" || name == "docker"
}

// dockerAvailableProbe reports whether a Docker execution environment can be
// reached. Injected into resolveBackend for deterministic tests.
type dockerAvailabilityProbe func(*ContainerManager) bool

// bwrapAvailabilityFunc reports whether the bwrap backend could be
// constructed on this platform. Injected into resolveBackend for tests.
type bwrapAvailabilityFunc func() bool

// bwrapConstructor builds the bwrap ExecutionBackend. Injected so resolver
// selection tests do not depend on a real binary or GOOS.
type bwrapConstructor func(mgr *ContainerManager, logger *slog.Logger) (ExecutionBackend, error)

// realBwrapConstructor adapts NewBwrapBackend to bwrapConstructor, wiring
// the manager's configured bwrap + env-policy settings so jail children get
// consistent env filtering with every other backend.
func realBwrapConstructor(mgr *ContainerManager, logger *slog.Logger) (ExecutionBackend, error) {
	var cfg BwrapConfig
	var policy EnvPolicyConfig
	if mgr != nil {
		cfg = mgr.config.Bwrap
		policy = mgr.config.EnvPolicy
	}
	if policy.Mode == "" {
		policy.Mode = EnvModeAllowlist
	}
	be, err := NewBwrapBackend(cfg, policy, logger)
	if err != nil {
		return nil, err
	}
	return be, nil
}

var (
	bwrapOnce    sync.Once
	bwrapPresent bool // guarded by bwrapOnce; written before first read completes
)

// probeBwrapBinary performs the real availability check exactly once per
// process and caches the verdict. It returns false on any non-Linux GOOS or
// when the bwrap binary cannot be found on PATH.
//
// NOTE: intentionally NOT mutex-protected — sync.Once provides the
// happens-before edge for the single write; all later reads are read-only.
func probeBwrapBinary() bool {
	bwrapOnce.Do(func() {
		if runtime.GOOS != "linux" {
			bwrapPresent = false
			return
		}
		if _, err := exec.LookPath("bwrap"); err != nil {
			bwrapPresent = false
			return
		}
		bwrapPresent = true
	})
	return bwrapPresent
}

// realDockerProbe mirrors the manager's own initialization semantics: the
// docker backend exists in the manager's registry only when it was actually
// constructed there (manager.go registers "docker" after newDockerBackend
// succeeds), so presence in the map is the availability signal.
func realDockerProbe(mgr *ContainerManager) bool {
	if mgr == nil {
		return false
	}
	return mgr.GetBackend("docker") != nil
}

// defaultProbes returns production probes. Extracted so tests can compare
// against injected fakes without duplicating wiring knowledge.
func defaultProbes() (dockerAvailabilityProbe, bwrapAvailabilityFunc, bwrapConstructor) {
	return realDockerProbe, probeBwrapBinary, realBwrapConstructor
}

// resolveBackend implements sandbox-aware backend selection over injectable
// probes (test seam); ResolveBackend is the public wrapper using real probes.
//
// Selection:
//   - auto: docker > bwrap > local
//   - explicit order: that backend only (no cross-consideration)
//   - local order: local directly (deliberate choice, no fallback warning)
//
// Failure semantics:
//   - require=true + nothing qualifying => ErrSandboxRequired (fail closed;
//     callers must refuse execution, never silently run locally)
//   - require=false + nothing qualifying => local WITH loud warning
func resolveBackend(
	mgr *ContainerManager,
	cfg ResolverConfig,
	logger *slog.Logger,
	dockerAvail dockerAvailabilityProbe,
	bwrapAvail bwrapAvailabilityFunc,
	newBwrap bwrapConstructor,
) (ExecutionBackend, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	order := cfg.Order
	switch order {
	case SandboxOrderAuto, "":
		order = SandboxOrderAuto
	case SandboxOrderDocker, SandboxOrderBwrap, SandboxOrderLocal:
	default:
		logger.Warn(fmt.Sprintf("runtime: unknown sandbox_backend_order %q, treating as auto", string(cfg.Order)))
		order = SandboxOrderAuto
	}

	// Candidate list per resolved order. When require_sandbox is false,
	// "local" is appended as the implicit final fallback so exhausted
	// explicit orders degrade WITH a loud warning rather than erroring.
	var candidates []string
	switch order {
	case SandboxOrderDocker:
		candidates = []string{"docker"}
	case SandboxOrderBwrap:
		candidates = []string{"bwrap"}
	case SandboxOrderLocal:
		candidates = []string{"local"}
	default: // auto: strongest containment first
		candidates = []string{"docker", "bwrap"}
	}
	if !cfg.RequireSandbox && order != SandboxOrderLocal {
		hasLocal := false
		for _, c := range candidates {
			if c == "local" {
				hasLocal = true
				break
			}
		}
		if !hasLocal {
			candidates = append(candidates, "local")
		}
	}

	for _, name := range candidates {
		switch name {
		case "local":
			// Local never qualifies as a sandbox. Under explicit local order
			// it is a deliberate configuration choice (no warning); under
			// auto/degraded fallback it is a posture downgrade (warn loudly).
			if cfg.RequireSandbox {
				// Contradiction (e.g. explicit local + require) or exhausted
				// candidates under require: refuse, never degrade silently.
				return nil, fmt.Errorf("%w: no qualifying backend for order %q", ErrSandboxRequired, string(cfg.Order))
			}
			var local ExecutionBackend
			if mgr != nil {
				local = mgr.GetBackend("local")
			}
			if local == nil {
				// Standalone/nil-manager use: build an isolated local backend
				// with the secure default policy.
				local = NewLocalBackend(Config{}, nil)
			}
			if order == SandboxOrderLocal {
				return local, nil
			}
			logger.Warn(fmt.Sprintf(
				"runtime: UNSANDBOXED local fallback selected (order=%q require_sandbox=false); "+
					"commands will execute WITHOUT OS-level confinement",
				string(cfg.Order)))
			return local, nil

		case "docker":
			if !dockerAvail(mgr) {
				continue
			}
			return mgr.GetBackend("docker"), nil

		case "bwrap":
			if !bwrapAvail() {
				continue
			}
			be, err := newBwrap(mgr, logger)
			if err != nil {
				if cfg.RequireSandbox {
					return nil, fmt.Errorf("%w: bwrap construction failed: %v", ErrSandboxRequired, err)
				}
				logger.Warn("runtime: bwrap construction failed; falling back", "error", err)
				continue
			}
			return be, nil
		}
	}

	// Unreachable given the candidate lists above, but keeps the compiler
	// honest if candidates ever change.
	return nil, fmt.Errorf("%w: no qualifying backend for order %q", ErrSandboxRequired, string(cfg.Order))
}

// ResolveBackend picks the execution backend per ResolverConfig. It is the
// public entry point used by daemon startup wiring.
func ResolveBackend(mgr *ContainerManager, cfg ResolverConfig, logger *slog.Logger) (ExecutionBackend, error) {
	dockerAvail, bwrapAvail, newBwrap := defaultProbes()
	return resolveBackend(mgr, cfg, logger, dockerAvail, bwrapAvail, newBwrap)
}
