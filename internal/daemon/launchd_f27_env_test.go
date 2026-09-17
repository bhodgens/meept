package daemon

// Pin for audit finding F27: the primary launchd install (kardianos
// service.Config.EnvVars) must propagate MEEPT_HOME exactly like the fallback
// plist writer (plistEnvironmentVariables) does. Without it, a launchd-installed
// daemon in an isolated rig silently resolves the operator's ~/.meept instead
// of the rig home. When MEEPT_HOME is unset the EnvVars map must stay
// byte-identical to the historical PATH-only entry.

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestEnvVarsWithMeeptHome pins both halves of the contract: PATH is always
// present, MEEPT_HOME appears only when set, and the unset case is identical
// to the historical map.
func TestEnvVarsWithMeeptHome(t *testing.T) {
	t.Setenv(config.EnvMeeptHome, "")
	unset := envVarsWithMeeptHome()
	if len(unset) != 1 {
		t.Fatalf("MEEPT_HOME unset: EnvVars = %v, want exactly the PATH entry", unset)
	}
	if unset["PATH"] != DaemonPath() {
		t.Errorf("PATH = %q, want DaemonPath() %q", unset["PATH"], DaemonPath())
	}
	if _, ok := unset[config.EnvMeeptHome]; ok {
		t.Error("MEEPT_HOME unset must not appear in EnvVars")
	}

	tmp := t.TempDir()
	t.Setenv(config.EnvMeeptHome, tmp)
	set := envVarsWithMeeptHome()
	if set[config.EnvMeeptHome] != tmp {
		t.Errorf("MEEPT_HOME = %q, want %q propagated", set[config.EnvMeeptHome], tmp)
	}
	if set["PATH"] != DaemonPath() {
		t.Errorf("PATH = %q, want DaemonPath() %q (MEEPT_HOME must not displace PATH)", set["PATH"], DaemonPath())
	}
	if len(set) != 2 {
		t.Errorf("EnvVars = %v, want exactly PATH and MEEPT_HOME", set)
	}
}

// TestPlistAndPrimaryEnvVarsAgree pins that the primary install dictionary
// and the fallback plist renderer stay in sync: the fallback is the rendered
// form of exactly the same variables.
func TestPlistAndPrimaryEnvVarsAgree(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(config.EnvMeeptHome, tmp)

	primary := envVarsWithMeeptHome()
	if primary[config.EnvMeeptHome] != tmp {
		t.Fatalf("primary install EnvVars = %v, want MEEPT_HOME=%q", primary, tmp)
	}

	fallback := plistEnvironmentVariables()
	want := "<key>MEEPT_HOME</key><string>" + plistValueEscaper.Replace(tmp) + "</string>"
	if !strings.Contains(fallback, want) {
		t.Errorf("fallback plist = %q, want it to contain %q — the two install paths diverged", fallback, want)
	}
}
