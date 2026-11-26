package mcp

import "testing"

// Pure parsing/decision tests for the version pin helpers added in
// issue #167. No network: these exercise only ParsePin, ParseVersion and
// CompareVersions so a logic regression is caught without a registry.
func TestParsePinAndCompare(t *testing.T) {
	cases := []struct {
		name    string
		command []string
		want    Pin
		ok      bool
	}{
		{"npx scoped with -y", []string{"npx", "-y", "@scope/pkg@1.2.3"}, Pin{"npm", "@scope/pkg", "1.2.3"}, true},
		{"npx unscoped", []string{"npx", "pkg@0.4.5"}, Pin{"npm", "pkg", "0.4.5"}, true},
		{"uvx", []string{"uvx", "mcp-server-fetch==2.0.0"}, Pin{"pypi", "mcp-server-fetch", "2.0.0"}, true},
		{"unpinned npx", []string{"npx", "-y", "pkg"}, Pin{}, false},
		{"other binary", []string{"docker", "run", "pkg@1"}, Pin{}, false},
		{"empty", nil, Pin{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePin(tc.command)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("ParsePin(%v) = (%+v, %v), want (%+v, %v)",
					tc.command, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.0", "1.2.0-rc.1", 1},
		{"1.2.0-rc.1", "1.2.0", -1},
	}
	for _, tc := range cases {
		got := CompareVersions(tc.a, tc.b)
		if (got < 0) != (tc.want < 0) || (got > 0) != (tc.want > 0) {
			t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}
