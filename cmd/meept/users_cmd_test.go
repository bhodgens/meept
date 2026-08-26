// Tests for the users/keys CLI layer: command structure, arg parsing, and
// store round-trips through the command functions.
package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// captureUsersStdout redirects os.Stdout for the duration of fn and returns
// everything written. Errors returned by command funcs are checked by callers
// asserting on output; fn discards them deliberately.
func captureUsersStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = saved
	w.Close()
	out, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}

// withTempStateDir points the package-global stateDir at a temp directory
// and restores it afterwards.
func withTempStateDir(t *testing.T) {
	t.Helper()
	prev := stateDir
	t.Cleanup(func() { stateDir = prev })
	stateDir = t.TempDir()
}

func TestParseExpiry(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantNil bool
		wantErr bool
	}{
		{name: "empty means never", spec: "", wantNil: true},
		{name: "explicit never", spec: "never", wantNil: true},
		{name: "valid RFC3339", spec: "2027-01-01T00:00:00Z"},
		{name: "valid RFC3339 offset", spec: "2027-01-01T12:34:56+02:00"},
		{name: "date only rejected", spec: "2027-01-01", wantErr: true},
		{name: "prose rejected", spec: "tomorrow", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExpiry(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseExpiry(%q) succeeded, want error", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseExpiry(%q): %v", tc.spec, err)
			}
			if (got == nil) != tc.wantNil {
				t.Fatalf("parseExpiry(%q) = %v, wantNil=%v", tc.spec, got, tc.wantNil)
			}
		})
	}
}

func TestUsersCommandStructure(t *testing.T) {
	cmd := newUsersCmd()
	if cmd.Use != "users" {
		t.Errorf("use = %q, want \"users\"", cmd.Use)
	}
	verbs := map[string]bool{}
	for _, sub := range cmd.Commands() {
		verbs[sub.Name()] = true
	}
	for _, want := range []string{"list", "add", "remove"} {
		if !verbs[want] {
			t.Errorf("missing `users %s` subcommand", want)
		}
	}
}

func TestKeysCommandStructure(t *testing.T) {
	cmd := newKeysCmd()
	if cmd.Use != "keys" {
		t.Errorf("use = %q, want \"keys\"", cmd.Use)
	}
	verbs := map[string]bool{}
	for _, sub := range cmd.Commands() {
		verbs[sub.Name()] = true
	}
	for _, want := range []string{"add", "revoke", "list"} {
		if !verbs[want] {
			t.Errorf("missing `keys %s` subcommand", want)
		}
	}

	addCmd := findSub(cmd, "add")
	if addCmd == nil {
		t.Fatal("missing keys add")
	}
	if addCmd.Flags().Lookup("label") == nil || addCmd.Flags().Lookup("expires") == nil {
		t.Error("keys add must accept --label and --expires")
	}
	removeCmd := findSub(cmd, "revoke")
	if removeCmd == nil {
		t.Fatal("missing keys revoke")
	}
	if removeCmd.Flags().Lookup("yes") == nil {
		t.Error("keys revoke should accept --yes")
	}
	usersRemove := findSub(newUsersCmd(), "remove")
	if usersRemove == nil {
		t.Fatal("missing users remove")
	}
	if usersRemove.Flags().Lookup("yes") == nil {
		t.Error("users remove should accept --yes")
	}
}

// findSub returns a direct child of cmd by name, or nil.
func findSub(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

func TestRunCLIStoreRoundTrip(t *testing.T) {
	withTempStateDir(t)

	// add user
	out := captureUsersStdout(t, func() { _ = runUsersAdd(usersStorePath(""), "carol") })
	var userID string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "created user: carol (") {
			userID = strings.TrimSuffix(strings.TrimPrefix(line, "created user: carol ("), ")")
		}
	}
	if userID == "" {
		t.Fatalf("users add output missing id: %q", out)
	}

	// keys add with expiry; raw key appears in output exactly once.
	expiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	storePath := usersStorePath("")
	out = captureUsersStdout(t, func() {
		_ = runKeysAdd(storePath, userID, "cli-test", expiry)
	})
	rawKey := ""
	rawCount := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 64 && strings.Trim(line, "0123456789abcdef") == "" {
			rawKey = line
			rawCount++
		}
	}
	if rawKey == "" || rawCount != 1 {
		t.Fatalf("expected one 64-hex raw key in output, got %d occurrences: %q", rawCount, out)
	}
	if strings.Contains(out, rawKey[:16]+rawKey[16:]) && rawCount > 1 {
		t.Fatal("raw key printed more than once")
	}

	// keys list shows metadata but never raw material.
	out = captureUsersStdout(t, func() { _ = runKeysList(storePath, "", false) })
	if strings.Contains(out, rawKey) {
		t.Fatal("raw key material appeared in keys list output")
	}
	if !strings.Contains(out, "cli-test") || !strings.Contains(out, expiry) {
		t.Fatalf("keys list output missing label/expiry: %q", out)
	}

	// users list shows the user with 1 key and local origin.
	out = captureUsersStdout(t, func() { _ = runUsersList(storePath, false) })
	if !strings.Contains(out, "carol") || !strings.Contains(out, "local") {
		t.Fatalf("users list output missing carol/local: %q", out)
	}

	// round-trip through a fresh store instance: raw key validates.
	st, err := openUsersStore(storePath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	idn, verr := st.Validate(rawKey, time.Now())
	if verr != nil {
		t.Fatalf("Validate(rawKey): %v", verr)
	}
	if idn.UserName != "carol" {
		t.Fatalf("identity name = %q, want carol", idn.UserName)
	}

	// remove without --yes aborts when input declines.
	out = captureUsersStdout(t, func() { _ = runUsersRemove(storePath, userID, false) })
	if !strings.Contains(out, "aborted") {
		t.Fatalf("declined removal did not report abort: %q", out)
	}
	// user still present
	found := false
	for _, u := range st.ListUsers() {
		if u.ID == userID {
			found = true
		}
	}
	if !found {
		t.Fatal("user removed despite declined confirmation")
	}

	// removal with --yes cascades keys.
	out = captureUsersStdout(t, func() { _ = runUsersRemove(storePath, userID, true) })
	if !strings.Contains(out, "removed user: carol") {
		t.Fatalf("removal not confirmed in output: %q", out)
	}
	reopened, err := openUsersStore(storePath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	for _, u := range reopened.ListUsers() {
		if u.ID == userID {
			t.Fatal("user still present after confirmed removal")
		}
	}
	if _, err := reopened.Validate(rawKey, time.Now()); err == nil {
		t.Fatal("removed user's key still authenticates after cascade removal")
	}
}

func TestUsersCommandsDefaultToStateDirPath(t *testing.T) {
	withTempStateDir(t)
	got := usersStorePath("")
	want := filepath.Join(stateDir, "users.json5")
	if got != want {
		t.Fatalf("usersStorePath(\"\") = %q, want %q", got, want)
	}
	override := filepath.Join(t.TempDir(), "custom.json5")
	if got := usersStorePath(override); got != override {
		t.Fatalf("usersStorePath(flag) = %q, want flag to win: %q", got, override)
	}
}
