package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHTTPHookConfig_RetryCountPointerResolution: retry_count is a *int so
// the config surface can distinguish an omitted key from an explicit 0.
// Contract: omitted → nil (wiring resolves to 3), explicit 0 → pointer to 0
// (zero retries), explicit -1 → pointer to -1 (unlimited), explicit 3 → 3.
func TestHTTPHookConfig_RetryCountPointerResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := `
[hooks]

  [[hooks.http]]
  url = "http://example.internal/omitted"

  [[hooks.http]]
  url = "http://example.internal/zero"
  retry_count = 0

  [[hooks.http]]
  url = "http://example.internal/unlimited"
  retry_count = -1

  [[hooks.http]]
  url = "http://example.internal/five"
  retry_count = 5
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	hooks := cfg.Hooks.HTTP
	if len(hooks) != 4 {
		t.Fatalf("loaded %d hooks, want 4", len(hooks))
	}

	cases := []struct {
		name string
		url  string
		want *int // expected resolved value via the wiring rule (nil → 3)
	}{
		{"omitted resolves to default 3", "http://example.internal/omitted", intPtr(3)},
		{"explicit 0 stays 0", "http://example.internal/zero", intPtr(0)},
		{"explicit -1 stays -1", "http://example.internal/unlimited", intPtr(-1)},
		{"explicit 5 stays 5", "http://example.internal/five", intPtr(5)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hook *HTTPHookConfig
			for i := range hooks {
				if hooks[i].URL == tc.url {
					hook = &hooks[i]
					break
				}
			}
			if hook == nil {
				t.Fatalf("hook with url %q not found", tc.url)
			}

			got := 3 // wiring default for nil
			if hook.RetryCount != nil {
				got = *hook.RetryCount
			}
			if got != *tc.want {
				t.Errorf("retry_count = %d, want %d", got, *tc.want)
			}

			// The whole point of the pointer: omitted must be nil so the
			// wiring can tell it apart from an explicit 0.
			if tc.url == "http://example.internal/omitted" && hook.RetryCount != nil {
				t.Errorf("RetryCount = %v, want nil for omitted key", *hook.RetryCount)
			}
		})
	}
}

func intPtr(i int) *int { return &i }
