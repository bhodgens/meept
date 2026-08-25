package runtime

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// helper to build "K=V" env slices
func TestBuildChildEnv(t *testing.T) {
	parentEnv := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/Users/tester",
		"TMPDIR=/var/folders/tmp",
		"MEEPT_SENTINEL_SECRET=topsecret",
		"MY_EXTRA=extra-value",
	}

	tests := []struct {
		name             string
		cfg              EnvPolicyConfig
		cmdEnv           map[string]string
		wantContains     []string
		wantNotContains  []string
		wantStripped     []string
		wantInheritExact bool // inherit mode: expect full parent + cmdEnv, stripped nil
	}{
		{
			name:            "sentinel secret stripped in allowlist mode",
			cfg:             EnvPolicyConfig{Mode: EnvModeAllowlist},
			wantContains:    []string{"PATH=", "HOME="},
			wantNotContains: []string{"MEEPT_SENTINEL_SECRET"},
			wantStripped:    []string{"MEEPT_SENTINEL_SECRET", "MY_EXTRA"},
		},
		{
			name: "sentinel secret present in inherit mode",
			cfg:  EnvPolicyConfig{Mode: EnvModeInherit},
			wantContains: []string{
				"PATH=/usr/bin:/bin",
				"MEEPT_SENTINEL_SECRET=topsecret",
				"MY_EXTRA=extra-value",
			},
			wantNotContains:  []string{},
			wantStripped:     nil,
			wantInheritExact: true,
		},
		{
			name:         "base keys survive allowlist",
			cfg:          EnvPolicyConfig{Mode: EnvModeAllowlist},
			wantContains: []string{"PATH=", "HOME=", "TMPDIR="},
			// Keys absent from parentEnv must NOT be invented:
			wantNotContains: []string{"LANG", "SHELL", "LC_ALL"},
		},
		{
			name: "cmdEnv override wins over parent for allowed key",
			cfg:  EnvPolicyConfig{Mode: EnvModeAllowlist},
			cmdEnv: map[string]string{
				"PATH": "/custom/bin",
			},
			wantContains:    []string{"PATH=/custom/bin"},
			wantNotContains: []string{"PATH=/usr/bin:/bin"},
		},
		{
			name: "cmdEnv key denied by glob is dropped even though requested",
			cfg:  EnvPolicyConfig{Mode: EnvModeAllowlist, DenyGlobs: []string{"*KEY*"}},
			cmdEnv: map[string]string{
				"API_KEY":   "should-not-pass",
				"OTHER_VAR": "ok",
			},
			wantContains:    []string{"PATH="},
			// OTHER_VAR is not in BaseEnvKeys/Allowlist -> stripped too.
			wantNotContains: []string{"API_KEY", "OTHER_VAR"},
			wantStripped:    []string{"MEEPT_SENTINEL_SECRET", "MY_EXTRA", "API_KEY", "OTHER_VAR"},
		},
		{
			name: "extra allowlisted key included when present in parent",
			cfg:  EnvPolicyConfig{Mode: EnvModeAllowlist, Allowlist: []string{"MY_EXTRA"}},
			wantContains: []string{
				"MY_EXTRA=extra-value",
			},
			// No DenyGlobs configured in this case, but the sentinel is
			// stripped anyway: it is not allowlisted.
			wantNotContains: []string{"MEEPT_SENTINEL_SECRET"},
			wantStripped:    []string{"MEEPT_SENTINEL_SECRET"},
		},
		{
			name: "placeholder value passes even if name denied",
			cfg:  EnvPolicyConfig{Mode: EnvModeAllowlist, DenyGlobs: []string{"*SECRET*"}},
			cmdEnv: map[string]string{
				"MEEPT_SENTINEL_SECRET": "MEEPT_SECRET:resolved-by-daemon",
			},
			wantContains:    []string{"MEEPT_SENTINEL_SECRET=MEEPT_SECRET:resolved-by-daemon"},
			wantNotContains: []string{"topsecret"},
		},
		{
			name:        "empty cmdEnv in inherit appends nothing",
			cfg:         EnvPolicyConfig{Mode: EnvModeInherit},
			cmdEnv:      map[string]string{},
			wantStripped: nil,
		},
		{
			name: "unknown allowlist key absent from parent is not invented",
			cfg:  EnvPolicyConfig{Mode: EnvModeAllowlist, Allowlist: []string{"NOT_IN_PARENT"}},
			wantContains:    []string{"PATH="},
			wantNotContains: []string{"NOT_IN_PARENT"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, stripped := BuildChildEnv(tt.cfg, parentEnv, tt.cmdEnv)

			if tt.wantInheritExact {
				want := append([]string(nil), parentEnv...)
				for _, k := range sortedKeys(tt.cmdEnv) {
					want = append(want, k+"="+tt.cmdEnv[k])
				}
				if !reflect.DeepEqual(env, want) {
					t.Fatalf("inherit env mismatch:\n got %v\nwant %v", env, want)
				}
			}

			envJoined := "\x00" + strings.Join(env, "\n") + "\x00"
			for _, want := range tt.wantContains {
				if !strings.Contains(envJoined, want) {
					t.Errorf("env missing %q; got %v", want, env)
				}
			}
			for _, bad := range tt.wantNotContains {
				if strings.Contains(envJoined, bad+"=") || (bad != "PATH" && strings.Contains(envJoined, bad)) {
					t.Errorf("env must not contain %q; got %v", bad, env)
				}
			}

			if tt.wantStripped != nil && !equalUnordered(stripped, tt.wantStripped) {
				t.Errorf("stripped = %v, want %v (unordered)", stripped, tt.wantStripped)
			}
		})
	}
}

func TestBuildChildEnv_DeterministicOrdering(t *testing.T) {
	parentEnv := []string{"HOME=/h", "PATH=/bin", "TERM=xterm"}
	cfg := EnvPolicyConfig{Mode: EnvModeAllowlist, Allowlist: []string{"ZVAR", "AVAR", "MVAR"}}
	cmdEnv := map[string]string{"ZVAR": "z", "AVAR": "a", "MVAR": "m"}

	var first []string
	for i := 0; i < 20; i++ {
		env, _ := BuildChildEnv(cfg, parentEnv, cmdEnv)
		if i == 0 {
			first = env
			continue
		}
		if !reflect.DeepEqual(env, first) {
			t.Fatalf("ordering not deterministic:\nfirst: %v\ngot:   %v", first, env)
		}
	}
	// cmdEnv-only vars appended after parent-derived ones, sorted.
	idx := map[string]int{}
	for i, e := range first {
		k := strings.SplitN(e, "=", 2)[0]
		idx[k] = i
	}
	for _, pair := range [][2]string{{"ZVAR", "MVAR"}, {"MVAR", "AVAR"}} {
		if idx[pair[0]] <= idx[pair[1]] {
			t.Fatalf("expected %s before %s (sorted append); got %v", pair[1], pair[0], first)
		}
	}
}

func TestBuildChildEnv_NilParent(t *testing.T) {
	cfg := EnvPolicyConfig{Mode: EnvModeAllowlist}
	env, stripped := BuildChildEnv(cfg, nil, nil)
	if len(env) != 0 {
		t.Fatalf("nil parent should produce empty env, got %v", env)
	}
	if stripped != nil {
		t.Fatalf("stripped should be empty slice or nil, got %v", stripped)
	}
}

func TestBaseEnvKeys_ContainsEssentials(t *testing.T) {
	essential := []string{"PATH", "HOME", "TMPDIR"}
	set := map[string]bool{}
	for _, k := range BaseEnvKeys {
		set[k] = true
	}
	for _, k := range essential {
		if !set[k] {
			t.Errorf("BaseEnvKeys missing essential %q", k)
		}
	}
}

// equalUnordered compares two string slices ignoring order.
func equalUnordered(a, b []string) bool {
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
