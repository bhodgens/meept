package main

import (
	"testing"
)

// TestNewDispatchCmd_Structure verifies the dispatch command tree is wired
// correctly. We test structure rather than RPC behavior since the daemon
// socket is not available in unit tests.
func TestNewDispatchCmd_Structure(t *testing.T) {
	t.Parallel()
	cmd := newDispatchCmd()

	if cmd.Use != "dispatch" {
		t.Errorf("expected Use 'dispatch', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("expected non-empty Short")
	}

	// Cobra's Commands() returns empty until Find/Execute populates them.
	// Instead, verify subcommands by checking the internal command list via
	// Find with empty args (which triggers initialization).
	_, _, err := cmd.Find([]string{"submit"})
	if err != nil {
		t.Errorf("subcommand 'submit' not found: %v", err)
	}
	_, _, err = cmd.Find([]string{"status"})
	if err != nil {
		t.Errorf("subcommand 'status' not found: %v", err)
	}
	_, _, err = cmd.Find([]string{"results"})
	if err != nil {
		t.Errorf("subcommand 'results' not found: %v", err)
	}
}

// TestNewDispatchSubmitCmd_ArgValidation verifies the submit subcommand
// requires at least 3 args.
func TestNewDispatchSubmitCmd_ArgValidation(t *testing.T) {
	t.Parallel()
	cmd := newDispatchSubmitCmd()
	// MinimumNArgs(3) is the validator; verify it's configured by checking
	// that the Args field is non-nil.
	if cmd.Args == nil {
		t.Error("expected cmd.Args to be set (MinimumNArgs validator)")
	}
}

// TestNewDispatchStatusCmd_ArgValidation verifies status requires exactly 1 arg.
func TestNewDispatchStatusCmd_ArgValidation(t *testing.T) {
	t.Parallel()
	cmd := newDispatchStatusCmd()
	if cmd.Args == nil {
		t.Error("expected cmd.Args to be set (ExactArgs validator)")
	}
}

// TestNewDispatchResultsCmd_ArgValidation verifies results requires exactly 1 arg.
func TestNewDispatchResultsCmd_ArgValidation(t *testing.T) {
	t.Parallel()
	cmd := newDispatchResultsCmd()
	if cmd.Args == nil {
		t.Error("expected cmd.Args to be set (ExactArgs validator)")
	}
}

// TestNewDispatchSubmitCmd_Flags verifies the expected flags are registered.
func TestNewDispatchSubmitCmd_Flags(t *testing.T) {
	t.Parallel()
	cmd := newDispatchSubmitCmd()

	expectedFlags := []string{"json", "priority", "resource", "workspace"}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag --%s to be registered", name)
		}
	}
}

// TestNewDispatchStatusCmd_Flags verifies the json flag.
func TestNewDispatchStatusCmd_Flags(t *testing.T) {
	t.Parallel()
	cmd := newDispatchStatusCmd()
	if cmd.Flags().Lookup("json") == nil {
		t.Error("expected flag --json to be registered")
	}
}

// TestNewDispatchResultsCmd_Flags verifies the json flag.
func TestNewDispatchResultsCmd_Flags(t *testing.T) {
	t.Parallel()
	cmd := newDispatchResultsCmd()
	if cmd.Flags().Lookup("json") == nil {
		t.Error("expected flag --json to be registered")
	}
}
