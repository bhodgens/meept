package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestDefaultConfig_MultiUserDisabled verifies multi-user access is OFF by
// default and the users store path carries its documented default value.
func TestDefaultConfig_MultiUserDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MultiUser.Enabled {
		t.Error("MultiUser.Enabled should be false by default")
	}
	if cfg.MultiUser.UsersFile != "~/.meept/users.json5" {
		t.Errorf("MultiUser.UsersFile = %q, want ~/.meept/users.json5", cfg.MultiUser.UsersFile)
	}
}

// TestMultiUserConfig_TOMLRoundTrip verifies both fields survive a TOML
// marshal/unmarshal round trip with the documented keys.
func TestMultiUserConfig_TOMLRoundTrip(t *testing.T) {
	in := MultiUserConfig{
		Enabled:   true,
		UsersFile: "~/.meept/team-users.json5",
	}

	data, err := toml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out MultiUserConfig
	if err := toml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !out.Enabled || out.UsersFile != in.UsersFile {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

// TestLoad_MultiUserKeys verifies the real loader honors the documented TOML
// keys and expands tildes in users_file on load.
func TestLoad_MultiUserKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")

	content := `
[multiuser]
enabled = true
users_file = "~/meept-test-users.json5"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.MultiUser.Enabled {
		t.Error("MultiUser.Enabled = false, want true from loaded config")
	}
	homeDir, _ := os.UserHomeDir()
	wantUsersFile := filepath.Join(homeDir, "meept-test-users.json5")
	if cfg.MultiUser.UsersFile != wantUsersFile {
		t.Errorf("UsersFile = %q, want %q", cfg.MultiUser.UsersFile, wantUsersFile)
	}
}

// TestLoad_MultiUserDefaultWhenAbsent verifies a config file without the
// [multiuser] table loads with defaults (disabled) rather than zero values.
func TestLoad_MultiUserDefaultWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	if err := os.WriteFile(path, []byte("[daemon]\nlog_level = \"INFO\"\n"), 0o600); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MultiUser.Enabled {
		t.Error("MultiUser.Enabled should default to false when absent from file")
	}
	if cfg.MultiUser.UsersFile == "" {
		t.Error("MultiUser.UsersFile should keep its default when absent from file")
	}
}
