package sharedclient

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// ============================================================================
// Test CLI binary --session flag (end-to-end)
// ============================================================================

func TestCLIChatSessionFlag(t *testing.T) {
	// Skip if binary doesn't exist
	binaryPath := "../../bin/meept"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("meept binary not found, skipping e2e test")
	}

	// Test --help to verify flag exists
	cmd := exec.Command(binaryPath, "chat", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chat --help failed: %v", err)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "--session") {
		t.Error("--session flag not found in help output")
	}
	if !strings.Contains(outputStr, "target specific session") {
		t.Error("--session flag description not found in help output")
	}
}

// ============================================================================
// Test CLI oneshot_responses documentation in help
// ============================================================================

func TestCLIChatOneshotResponsesDocumentation(t *testing.T) {
	binaryPath := "../../bin/meept"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("meept binary not found, skipping e2e test")
	}

	cmd := exec.Command(binaryPath, "chat", "--help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("chat --help failed: %v", err)
	}

	outputStr := string(output)
	// Verify oneshot_responses is mentioned in examples
	if !strings.Contains(outputStr, "oneshot_responses") {
		t.Error("oneshot_responses not mentioned in help output")
	}
}
