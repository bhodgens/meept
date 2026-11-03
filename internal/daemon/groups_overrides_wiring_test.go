// groups_overrides_wiring_test.go — T2 boot-wiring tests: a malformed
// overrides file FAILS NewComponents (named error, fail closed), a clean
// file with expired entries boots with the skip Warn, and the injected
// clock (MEEPT_GROUPS_NOW) drives expiry.
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// withOverridesHome points $MEEPT_HOME at a temp dir for the duration of
// the test (config.MeeptPath resolves through it).
func withOverridesHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	return home
}

func overridesBootConfig(tmpDir string) *config.Config {
	return &config.Config{
		Daemon: config.DaemonConfig{DataDir: tmpDir},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
			Groups: config.SecurityGroupsConfig{
				Policies: []config.GroupsPolicyConfig{
					{Name: "coders", ToolsAllowed: []string{"file_read"}},
				},
				Bindings: []config.GroupsBindingConfig{
					{Subject: "group:coders", Policy: "coders", Scope: "daemon"},
				},
			},
		},
	}
}

// TestGroupsOverridesMalformedFailsBoot pins the fail-closed posture: a
// malformed overrides entry aborts NewComponents with an error naming the
// defect.
func TestGroupsOverridesMalformedFailsBoot(t *testing.T) {
	home := withOverridesHome(t)
	if err := os.WriteFile(
		filepath.Join(home, groups.OverridesFileName),
		[]byte(`{"overrides":[{"subject":"agent:x","allow":["git_push"],"reason":"broken: no expires"}]}`),
		0o600,
	); err != nil {
		t.Fatalf("write overrides: %v", err)
	}
	msgBus := bus.New(nil, testLogger(t))
	_, err := NewComponents(t.Context(), overridesBootConfig(t.TempDir()), msgBus, testLogger(t))
	if err == nil {
		t.Fatalf("malformed overrides must fail boot")
	}
	if !strings.Contains(err.Error(), "agent:x") || !strings.Contains(err.Error(), "expires") {
		t.Fatalf("boot error does not name the defect: %v", err)
	}
}

// TestGroupsOverridesCleanBootWithExpiredWarn pins the load path: a file
// carrying one live + one expired entry boots fine with exactly one Warn
// naming the expired subject (clock injected through MEEPT_GROUPS_NOW).
func TestGroupsOverridesCleanBootWithExpiredWarn(t *testing.T) {
	home := withOverridesHome(t)
	entries := []groups.Override{
		{
			Subject: "agent:deploy-bot",
			Allow:   []string{"git_push"},
			Reason:  "deploy window",
			Expires: time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC),
		},
		{
			Subject: "agent:past-bot",
			Allow:   []string{"shell"},
			Reason:  "old window",
			Expires: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	if err := groups.SaveOverrides(filepath.Join(home, groups.OverridesFileName), entries); err != nil {
		t.Fatalf("save overrides: %v", err)
	}
	// Pin the boot clock AFTER the first expiry, BEFORE the second.
	t.Setenv("MEEPT_GROUPS_NOW", "2026-11-01T00:00:00Z")

	msgBus := bus.New(nil, testLogger(t))
	comps, err := NewComponents(t.Context(), overridesBootConfig(t.TempDir()), msgBus, testLogger(t))
	if err != nil {
		t.Fatalf("clean overrides boot failed: %v", err)
	}
	if comps == nil {
		t.Fatal("nil components")
	}
}

// TestGroupsOverridesNoFileBoots pins first-use: no overrides file boots
// clean (empty set).
func TestGroupsOverridesNoFileBoots(t *testing.T) {
	withOverridesHome(t)
	msgBus := bus.New(nil, testLogger(t))
	if _, err := NewComponents(t.Context(), overridesBootConfig(t.TempDir()), msgBus, testLogger(t)); err != nil {
		t.Fatalf("no-file boot failed: %v", err)
	}
}

// TestEnvGroupsNowPinned pins the clock seam: MEEPT_GROUPS_NOW overrides
// wall-clock; unset returns a live UTC time; a malformed value panics
// (never silently reverts to real time in a pinned test).
func TestEnvGroupsNowPinned(t *testing.T) {
	t.Setenv("MEEPT_GROUPS_NOW", "2026-11-05T17:00:00Z")
	if got, want := EnvGroupsNow(), time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("pinned clock = %v, want %v", got, want)
	}

	t.Setenv("MEEPT_GROUPS_NOW", "")
	before := time.Now().UTC()
	got := EnvGroupsNow()
	if got.Before(before.Add(-time.Minute)) || got.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("unset clock far from wall-clock: %v", got)
	}

	t.Setenv("MEEPT_GROUPS_NOW", "not-a-time")
	defer func() {
		if recover() == nil {
			t.Fatal("malformed MEEPT_GROUPS_NOW did not fail loudly")
		}
	}()
	EnvGroupsNow()
}
