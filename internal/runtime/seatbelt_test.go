package runtime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the OS-independent policy model: path validation, subtree
// matching, and exact profile rendering. They run on every platform, so CI
// (ubuntu-latest) verifies the rule shape even though the kernel enforcement
// tests in e2e/suites/seatbelt skip off-darwin.

func TestSeatbeltActive(t *testing.T) {
	assert.False(t, SeatbeltActive(nil), "nil policy must be the legacy posture")
	assert.False(t, SeatbeltActive(&SeatbeltConfig{}), "zero value must be the legacy posture")
	assert.False(t, SeatbeltActive(&SeatbeltConfig{DenyRead: []string{"/etc"}}),
		"rules without Enabled must not arm confinement")
	assert.True(t, SeatbeltActive(&SeatbeltConfig{Enabled: true}))
}

func TestValidateSeatbeltPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{"empty", "", "empty path"},
		{"relative", "not-an-absolute-path", "is not absolute"},
		{"tilde not expanded", "~/.ssh", "is not absolute"},
		{"dot relative", "./keys", "is not absolute"},
		{"double quote", `/etc/"evil"`, "cannot quote"},
		{"backslash", `/etc/evil\path`, "cannot quote"},
		{"newline", "/etc/evil\npath", "cannot quote"},
		{"critical sh", "/bin/sh", "covers the confined launch target"},
		{"critical bash", "/bin/bash", "covers the confined launch target"},
		{"critical dash", "/bin/dash", "covers the confined launch target"},
		{"critical env", "/usr/bin/env", "covers the confined launch target"},
		{"root covers everything", "/", "covers the confined launch target"},
		{"bin covers sh", "/bin", "covers the confined launch target"},
		{"usr covers env", "/usr", "covers the confined launch target"},
		{"ok etc/ssh", "/etc/ssh", ""},
		{"ok usr/bin/true", "/usr/bin/true", ""},
		{"ok usr/bin/sudo", "/usr/bin/sudo", ""},
		{"ok dev/null", "/dev/null", ""},
		// The prefix test is path-component aware: /bin/shadow is not under
		// /bin/sh, and /binsh is not under /bin.
		{"sibling not covered sh", "/bin/shadow", ""},
		{"sibling not covered bin", "/binsh/evil", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSeatbeltPath(tt.path)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCoversSubtree(t *testing.T) {
	tests := []struct {
		root, candidate string
		want            bool
	}{
		{"/", "/anything", true},
		{"/", "/", true},
		{"/bin/sh", "/bin/sh", true},
		{"/bin", "/bin/sh", true},
		{"/private/tmp/x", "/private/tmp/x/y/z", true},
		{"/private/tmp/x", "/private/tmp/y", false},
		{"/bin/sh", "/bin/shadow", false},
		{"/bin", "/binsh/x", false},
		{"/tmp", "/private/tmp/x", false},
	}
	for _, tt := range tests {
		t.Run(tt.root+"~"+tt.candidate, func(t *testing.T) {
			assert.Equal(t, tt.want, coversSubtree(tt.root, tt.candidate))
		})
	}
}

func TestBuildSeatbeltProfile_InactiveIsLegacy(t *testing.T) {
	t.Run("no class no policy launches bare", func(t *testing.T) {
		got, err := BuildSeatbeltProfile("", nil)
		require.NoError(t, err)
		assert.Equal(t, "", got)
	})
	t.Run("restrictive class no policy is the legacy constant byte for byte", func(t *testing.T) {
		got, err := BuildSeatbeltProfile(NetworkClassDomainAllowlist, nil)
		require.NoError(t, err)
		assert.Equal(t, noNetworkProfile, got)
	})
	t.Run("enabled false ignores every rule", func(t *testing.T) {
		cfg := &SeatbeltConfig{
			DenyRead:         []string{"/etc/ssh"},
			AllowWrite:       []string{"/private/tmp/box"},
			BlockProcessInfo: true,
			DenyExec:         []string{"/usr/bin/true"},
		}
		got, err := BuildSeatbeltProfile("", cfg)
		require.NoError(t, err)
		assert.Equal(t, "", got, "a disabled policy must not confine anything")
	})
}

// rulePos returns the byte offset of a rule fragment in the profile, or -1.
func rulePos(profile, fragment string) int { return strings.Index(profile, fragment) }

func TestBuildSeatbeltProfile_RuleOrderAndShape(t *testing.T) {
	cfg := &SeatbeltConfig{
		Enabled:          true,
		DenyRead:         []string{"/etc/ssh"},
		AllowWrite:       []string{"/private/tmp/box"},
		DenyWrite:        []string{"/private/tmp/box/protected"},
		BlockProcessInfo: true,
		DenyExec:         []string{"/usr/bin/true"},
	}
	got, err := BuildSeatbeltProfile(NetworkClassDomainAllowlist, cfg)
	require.NoError(t, err)

	// Every rule present.
	assert.Contains(t, got, "(version 1)(allow default)")
	assert.Contains(t, got, "(deny network*)")
	assert.Contains(t, got, `(deny file-read* (subpath "/etc/ssh"))`)
	assert.Contains(t, got, "(deny file-write*)")
	assert.Contains(t, got, `(allow file-write-data (literal "/dev/null"))`)
	assert.Contains(t, got, `(allow file-write* (subpath "/private/tmp/box"))`)
	assert.Contains(t, got, `(deny file-write* (subpath "/private/tmp/box/protected"))`)
	assert.Contains(t, got, "(deny process-info*)")
	assert.Contains(t, got, `(deny process-exec* (subpath "/usr/bin/true"))`)

	// Seatbelt is last-match-wins: order is load-bearing, not cosmetic.
	// The global write-deny must precede the allow-list, and every
	// deny_write hole must follow the allows that would otherwise cover it.
	posDenyAll := rulePos(got, "(deny file-write*)")
	posAllowBox := rulePos(got, `(allow file-write* (subpath "/private/tmp/box"))`)
	posHole := rulePos(got, `(deny file-write* (subpath "/private/tmp/box/protected"))`)
	require.NotEqual(t, -1, posDenyAll)
	require.NotEqual(t, -1, posAllowBox)
	require.NotEqual(t, -1, posHole)
	assert.Less(t, posDenyAll, posAllowBox, "deny file-write* must come before the allow-list")
	assert.Less(t, posAllowBox, posHole, "deny_write holes must be emitted after the allows")

	// Network deny rides right after the header so later rules can still
	// narrow it.
	assert.Less(t, rulePos(got, "(version 1)(allow default)"), rulePos(got, "(deny network*)"))
}

func TestBuildSeatbeltProfile_WriteUnconfinedKeepsDeviceWrites(t *testing.T) {
	cfg := &SeatbeltConfig{Enabled: true, DenyRead: []string{"/etc/ssh"}}
	got, err := BuildSeatbeltProfile("", cfg)
	require.NoError(t, err)
	assert.NotContains(t, got, "(deny file-write*)",
		"without an allow-list writes stay default-allowed")
	assert.NotContains(t, got, "file-write-data",
		"device exceptions only matter once writes are denied")
	assert.Contains(t, got, `(deny file-read* (subpath "/etc/ssh"))`)
}

func TestBuildSeatbeltProfile_DenyWriteWithoutAllowList(t *testing.T) {
	cfg := &SeatbeltConfig{Enabled: true, DenyWrite: []string{"/private/tmp/box/protected"}}
	got, err := BuildSeatbeltProfile("", cfg)
	require.NoError(t, err)
	assert.NotContains(t, got, "(deny file-write*)")
	assert.Contains(t, got, `(deny file-write* (subpath "/private/tmp/box/protected"))`)
}

func TestBuildSeatbeltProfile_EmptyEnabledPolicyIsNoOp(t *testing.T) {
	// Enabled with no rules produces a profile that cannot confine anything;
	// the builder must report it unconfigured rather than spawn a wrapper
	// sandbox-exec that changes nothing.
	got, err := BuildSeatbeltProfile("", &SeatbeltConfig{Enabled: true})
	require.NoError(t, err)
	assert.Equal(t, "", got)
}

func TestBuildSeatbeltProfile_BadRuleFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		cfg  SeatbeltConfig
		want string
	}{
		{
			name: "deny_read relative",
			cfg:  SeatbeltConfig{Enabled: true, DenyRead: []string{"relative/path"}},
			want: "seatbelt deny_read",
		},
		{
			name: "allow_write covers sh",
			cfg:  SeatbeltConfig{Enabled: true, AllowWrite: []string{"/bin"}},
			want: "seatbelt allow_write",
		},
		{
			name: "deny_write relative",
			cfg:  SeatbeltConfig{Enabled: true, DenyWrite: []string{"relative/path"}},
			want: "seatbelt deny_write",
		},
		{
			name: "deny_exec covers sh",
			cfg:  SeatbeltConfig{Enabled: true, DenyExec: []string{"/bin/sh"}},
			want: "seatbelt deny_exec",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildSeatbeltProfile("", &tt.cfg)
			require.Error(t, err, "a malformed rule must refuse the profile")
			assert.Contains(t, err.Error(), tt.want)
			assert.Equal(t, "", got, "a refused build must not return a partial profile")
		})
	}
}
