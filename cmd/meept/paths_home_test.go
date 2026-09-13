package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/config"
)

// withMeeptHome points MEEPT_HOME at a fresh temp dir for the duration of the
// test and restores the CLI path globals afterwards.
func withMeeptHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv(config.EnvMeeptHome, tmp)

	prevSocket, prevState := socketPath, stateDir
	t.Cleanup(func() {
		socketPath, stateDir = prevSocket, prevState
	})
	socketPath, stateDir = "", ""
	return tmp
}

// TestDefaultStateDirHonorsMeeptHome asserts the default state directory is
// the MEEPT_HOME override, not the operator's ~/.meept.
func TestDefaultStateDirHonorsMeeptHome(t *testing.T) {
	tmp := withMeeptHome(t)

	if got := resolveDefaultStateDir(); got != tmp {
		t.Errorf("resolveDefaultStateDir() = %q, want %q", got, tmp)
	}
}

// TestDefaultSocketPathHonorsMeeptHome asserts the RPC client default socket
// lands inside the isolated rig, which is the confirmed leak.
func TestDefaultSocketPathHonorsMeeptHome(t *testing.T) {
	tmp := withMeeptHome(t)
	want := filepath.Join(tmp, "meept.sock")

	if got := resolveDefaultSocketPath(); got != want {
		t.Errorf("resolveDefaultSocketPath() = %q, want %q", got, want)
	}
	if got := getSocketPath(); got != want {
		t.Errorf("getSocketPath() = %q, want %q", got, want)
	}
}

// TestPathDefaultsUnchangedWithoutMeeptHome asserts MEEPT_HOME unset keeps the
// historical ~/.meept defaults exactly.
func TestPathDefaultsUnchangedWithoutMeeptHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	t.Setenv(config.EnvMeeptHome, "")
	prevSocket, prevState := socketPath, stateDir
	t.Cleanup(func() { socketPath, stateDir = prevSocket, prevState })
	socketPath, stateDir = "", ""

	wantState := filepath.Join(home, ".meept")
	if got := resolveDefaultStateDir(); got != wantState {
		t.Errorf("resolveDefaultStateDir() = %q, want %q", got, wantState)
	}
	wantSocket := filepath.Join(home, ".meept", "meept.sock")
	if got := resolveDefaultSocketPath(); got != wantSocket {
		t.Errorf("resolveDefaultSocketPath() = %q, want %q", got, wantSocket)
	}
	if got := getSocketPath(); got != wantSocket {
		t.Errorf("getSocketPath() = %q, want %q", got, wantSocket)
	}
}

// TestExplicitFlagsWinOverMeeptHome asserts an explicit --socket / --state-dir
// still overrides the environment-derived default. The flags are bound exactly
// as main() binds them, so the precedence chain (flag > MEEPT_HOME > ~/.meept)
// is exercised end to end.
func TestExplicitFlagsWinOverMeeptHome(t *testing.T) {
	tmp := withMeeptHome(t)

	cmd := &cobra.Command{Use: "meept"}
	cmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", resolveDefaultSocketPath(), "Unix socket path (for RPC)")
	cmd.PersistentFlags().StringVarP(&stateDir, "state-dir", "d", resolveDefaultStateDir(), "State directory")

	// Without flags the env-derived default (the isolated rig) applies.
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if want := filepath.Join(tmp, "meept.sock"); socketPath != want {
		t.Fatalf("default socketPath = %q, want %q", socketPath, want)
	}

	cmd = &cobra.Command{Use: "meept"}
	cmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", resolveDefaultSocketPath(), "Unix socket path (for RPC)")
	cmd.PersistentFlags().StringVarP(&stateDir, "state-dir", "d", resolveDefaultStateDir(), "State directory")
	if err := cmd.ParseFlags([]string{"--socket", "/tmp/explicit-rig.sock", "--state-dir", "/tmp/explicit-rig"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if got := getSocketPath(); got != "/tmp/explicit-rig.sock" {
		t.Errorf("getSocketPath() = %q, want explicit /tmp/explicit-rig.sock", got)
	}
	if stateDir != "/tmp/explicit-rig" {
		t.Errorf("stateDir = %q, want explicit /tmp/explicit-rig", stateDir)
	}
}
