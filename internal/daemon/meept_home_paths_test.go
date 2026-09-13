package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestLegacyControllerStateDirHonorsMeeptHome asserts the PID/state directory
// default lands under MEEPT_HOME instead of the operator's ~/.meept.
func TestLegacyControllerStateDirHonorsMeeptHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvMeeptHome, tmp)

	c, err := newLegacyController("")
	if err != nil {
		t.Fatalf("newLegacyController: %v", err)
	}
	if c.meeptDir != tmp {
		t.Errorf("meeptDir = %q, want %q", c.meeptDir, tmp)
	}
	if !strings.HasPrefix(c.meeptDir, tmp) {
		t.Errorf("meeptDir %q is not under MEEPT_HOME %q", c.meeptDir, tmp)
	}
}

// TestServiceManagerLogDirHonorsMeeptHome asserts the daemon log directory
// default lands under MEEPT_HOME.
func TestServiceManagerLogDirHonorsMeeptHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvMeeptHome, tmp)

	m, err := NewServiceManager(nil)
	if err != nil {
		t.Fatalf("NewServiceManager: %v", err)
	}
	if m.logDir != tmp {
		t.Errorf("logDir = %q, want %q", m.logDir, tmp)
	}
}

// TestDaemonDirDefaultsUnchangedWithoutMeeptHome asserts the historical
// ~/.meept defaults are untouched when MEEPT_HOME is unset.
func TestDaemonDirDefaultsUnchangedWithoutMeeptHome(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, "")
	want := config.MeeptHome()
	if !strings.HasSuffix(want, string(filepath.Separator)+".meept") {
		t.Fatalf("precondition: MeeptHome() = %q, want a ~/.meept suffix", want)
	}

	c, err := newLegacyController("")
	if err != nil {
		t.Fatalf("newLegacyController: %v", err)
	}
	if c.meeptDir != want {
		t.Errorf("meeptDir = %q, want %q", c.meeptDir, want)
	}

	m, err := NewServiceManager(nil)
	if err != nil {
		t.Fatalf("NewServiceManager: %v", err)
	}
	if m.logDir != want {
		t.Errorf("logDir = %q, want %q", m.logDir, want)
	}
}

// TestExplicitLogDirWinsOverMeeptHome asserts an explicit LogDir still wins.
func TestExplicitLogDirWinsOverMeeptHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvMeeptHome, tmp)
	explicit := filepath.Join(tmp, "logs")

	c, err := newLegacyController(explicit)
	if err != nil {
		t.Fatalf("newLegacyController: %v", err)
	}
	if c.meeptDir != explicit {
		t.Errorf("meeptDir = %q, want explicit %q", c.meeptDir, explicit)
	}

	m, err := NewServiceManager(&ServiceManagerConfig{LogDir: explicit})
	if err != nil {
		t.Fatalf("NewServiceManager: %v", err)
	}
	if m.logDir != explicit {
		t.Errorf("logDir = %q, want explicit %q", m.logDir, explicit)
	}
}

// TestPlistEnvironmentVariablesPropagatesMeeptHome asserts a launchd-installed
// daemon in an isolated rig keeps MEEPT_HOME (otherwise it would silently fall
// back to the operator's ~/.meept).
func TestPlistEnvironmentVariablesPropagatesMeeptHome(t *testing.T) {
	tmp := t.TempDir()

	t.Setenv(config.EnvMeeptHome, "")
	unset := plistEnvironmentVariables()
	if strings.Contains(unset, "MEEPT_HOME") {
		t.Errorf("MEEPT_HOME unset: got %q, want no MEEPT_HOME entry", unset)
	}
	if !strings.Contains(unset, "<key>PATH</key>") {
		t.Errorf("MEEPT_HOME unset: got %q, want a PATH entry", unset)
	}
	if !strings.HasPrefix(unset, "<dict>") || !strings.HasSuffix(unset, "</dict>") {
		t.Errorf("got %q, want a single <dict> wrapper", unset)
	}

	t.Setenv(config.EnvMeeptHome, tmp)
	set := plistEnvironmentVariables()
	if !strings.Contains(set, "<key>MEEPT_HOME</key><string>"+tmp+"</string>") {
		t.Errorf("got %q, want MEEPT_HOME=%s propagated", set, tmp)
	}
}
