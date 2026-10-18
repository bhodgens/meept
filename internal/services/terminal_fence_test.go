package services

import (
	"context"
	"fmt"
	"testing"
)

// stubFenceChecker is a minimal FenceChecker that rejects a marker command.
type stubFenceChecker struct {
	rejectCmd string
}

func (f *stubFenceChecker) CheckPath(path string, op string) error {
	return nil
}

func (f *stubFenceChecker) CheckCommand(cmd string, workDir string) error {
	if cmd == f.rejectCmd {
		return fmt.Errorf("fence: command %q rejected", cmd)
	}
	return nil
}

func TestTerminalService_FenceRejection(t *testing.T) {
	// Before the fix, NewTerminalService never wires SetFenceChecker,
	// so the shell tool has no fence checker and the marker command
	// would NOT be rejected.
	//
	// After the fix, we can call SetFenceChecker on the TerminalService
	// to wire a fence checker.

	termSvc := NewTerminalService("", nil, nil)
	fence := &stubFenceChecker{rejectCmd: "MARKER_REJECT_ME"}
	termSvc.SetFenceChecker(fence)

	// Execute the marker command - it should be rejected by the fence.
	_, err := termSvc.ExecuteCommand(context.Background(), "MARKER_REJECT_ME", "")
	if err == nil {
		t.Fatal("expected fence rejection error, got nil")
	}
	if !testContains(err.Error(), "fence") {
		t.Fatalf("expected error to contain 'fence', got: %v", err)
	}
}

func testContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstr(s, substr)))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}