package builtin

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/runtime"
)

// refusingBackend implements runtime.ExecutionBackend by REFUSING every
// execution. It is installed on the shell tool when require_sandbox=true and
// no qualifying sandbox backend exists: commands fail closed (error wrapping
// runtime.ErrSandboxRequired) instead of silently running unsandboxed via
// direct exec.
type refusingBackend struct {
	reason error
}

// NewRefusingBackend constructs a refusing backend around a resolve error.
func NewRefusingBackend(reason error) runtime.ExecutionBackend {
	return &refusingBackend{reason: reason}
}

// Name identifies the backend in logs and progress messages.
func (r *refusingBackend) Name() string { return "refusing" }

// Execute always refuses, wrapping the original resolution error so callers
// can errors.Is against runtime.ErrSandboxRequired.
func (r *refusingBackend) Execute(context.Context, runtime.Command) (*runtime.CommandResult, error) {
	return nil, fmt.Errorf("command refused: %w", r.reason)
}

// Close is a no-op; the refuser holds no resources.
func (r *refusingBackend) Close() error { return nil }
