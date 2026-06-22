package tui

import (
	"context"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// ConfirmationInterceptor provides the two-phase confirmation protocol for
// destructive tool results in the TUI. When a tool returns a phase-1
// requires_confirmation response, the interceptor takes over the render
// loop to display a modal, captures the user's y/n response, and either
// re-executes the tool with confirmed=true or returns a declined result.
//
// The interceptor is intentionally decoupled from bubbletea's program
// runtime so the confirmation logic can be exercised in tests via
// HandleConfirmationResult without a TTY.
type ConfirmationInterceptor struct {
	// runModal runs a ConfirmationModel synchronously and returns the
	// final model state. Defaults to defaultRunModal, which uses
	// tea.NewProgram. Tests substitute a fake that returns a canned
	// confirmed/cancelled state.
	runModal func(model ConfirmationModel) ConfirmationModel
}

// NewConfirmationInterceptor constructs an interceptor with the default
// (tea.NewProgram-backed) modal runner.
func NewConfirmationInterceptor() *ConfirmationInterceptor {
	return &ConfirmationInterceptor{runModal: defaultRunModal}
}

// HandleConfirmationResult inspects a tool result map and, if it is a
// phase-1 confirmation request, shows the confirmation modal. Returns:
//   - (result, false, nil) when no confirmation is needed — caller should
//     use the result as-is.
//   - (nil, true, nil) when the user confirmed — caller should re-execute
//     the tool with args["confirmed"] = true.
//   - (declinedResult, true, nil) when the user declined — caller should
//     use declinedResult as the final tool result.
//   - (nil, false, err) on internal failure (e.g., TTY unavailable).
//
// The third return value (intercepted) is true whenever the interceptor
// took action, so the caller can short-circuit.
func (i *ConfirmationInterceptor) HandleConfirmationResult(result any) (finalResult any, intercepted bool, err error) {
	resultMap, ok := result.(map[string]any)
	if !ok {
		return result, false, nil
	}
	if !builtin.IsConfirmationRequest(resultMap) {
		return result, false, nil
	}

	if i == nil || i.runModal == nil {
		// No runner configured — treat as declined so we never silently
		// execute a destructive action.
		return builtin.DeclineResponse(resultMap), true, nil
	}

	model := i.runModal(NewConfirmationModel(resultMap))
	if model.IsConfirmed() {
		return nil, true, nil
	}
	return builtin.DeclineResponse(resultMap), true, nil
}

// ConfirmWithReexecute is the top-level helper for callers that have
// access to the tool and args. It runs HandleConfirmationResult and, on
// user confirmation, re-executes the tool with confirmed=true. The
// callback lets tests substitute a fake executor.
func (i *ConfirmationInterceptor) ConfirmWithReexecute(
	ctx context.Context,
	tool executeRepeater,
	args map[string]any,
	result any,
) (any, error) {
	final, intercepted, err := i.HandleConfirmationResult(result)
	if err != nil {
		return nil, err
	}
	if !intercepted {
		return result, nil
	}
	if final != nil {
		// Declined path — return the declined response map.
		return final, nil
	}
	// Confirmed path — re-execute with confirmed=true.
	reexec := map[string]any{}
	for k, v := range args {
		reexec[k] = v
	}
	reexec["confirmed"] = true
	return tool.Execute(ctx, reexec)
}

// executeRepeater is the minimal tool interface the interceptor needs.
type executeRepeater interface {
	Execute(ctx context.Context, args map[string]any) (any, error)
}

// SetRunner replaces the modal runner. Nil-safe per CLAUDE.md setter
// convention.
func (i *ConfirmationInterceptor) SetRunner(fn func(ConfirmationModel) ConfirmationModel) {
	if fn != nil {
		i.runModal = fn
	}
}

// defaultRunModal runs the ConfirmationModel in a new tea.Program and
// returns the final model state. If stdin is not a TTY, returns a
// cancelled model so destructive actions never silently proceed.
func defaultRunModal(m ConfirmationModel) ConfirmationModel {
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		m.cancelled = true
		return m
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		m.cancelled = true
		return m
	}
	if cm, ok := final.(ConfirmationModel); ok {
		return cm
	}
	m.cancelled = true
	return m
}
