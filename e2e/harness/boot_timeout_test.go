//go:build e2e

package harness

import (
	"os"
	"testing"
	"time"
)

// TestE2EBootTimeout covers matrix row F1 of the e2e-boot-hardening tree:
// the boot health-wait budget is env-tunable via MEEPT_E2E_BOOT_TIMEOUT with
// a 120s default. Unset/empty/unparseable values fall back to the default
// without panicking.
func TestE2EBootTimeout(t *testing.T) {
	const envKey = "MEEPT_E2E_BOOT_TIMEOUT"
	const def = 120 * time.Second

	cases := []struct {
		name string
		set  bool
		val  string
		want time.Duration
	}{
		{name: "unset env falls back to default", set: false, want: def},
		{name: "empty env falls back to default", set: true, val: "", want: def},
		{name: "45s parses", set: true, val: "45s", want: 45 * time.Second},
		{name: "2m parses", set: true, val: "2m", want: 2 * time.Minute},
		{name: "invalid value falls back to default", set: true, val: "banana", want: def},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(envKey, tc.val)
			} else {
				t.Setenv(envKey, "placeholder") // register restore
				os.Unsetenv(envKey)
			}
			if got := e2eBootTimeout(); got != tc.want {
				t.Fatalf("e2eBootTimeout() = %s, want %s", got, tc.want)
			}
		})
	}
}
