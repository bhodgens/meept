//go:build e2e

// Suite notification-bot, issue #167 slice: default-on, notification-only
// npm/PyPI MCP version check.
//
//	mcp-update-check-01 an outdated pin produces exactly one notice with
//	                    exact fields, delivered to SecurityNotify with
//	                    source "mcp-update:<name>"
//	mcp-update-check-02 check_mcp_updates=false disables the checker
//	                    entirely — zero stub requests
//	mcp-update-check-03 a registry network error produces no notice and
//	                    no panic
//
// Hermetic: no daemon, no live network. The npm and PyPI registry roots
// are redirected to in-process httptest servers for the duration of each
// test and restored afterwards.
package notificationbot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// stubRegistries redirects both registry roots at s and returns a restore
// func. No t.Parallel: RegistryEndpoints is package-global by design so
// production code needs no injected base URL.
func stubRegistries(t *testing.T, npm, pypi string) {
	t.Helper()
	origNPM, origPyPI := mcp.RegistryEndpoints.NPM, mcp.RegistryEndpoints.PyPI
	mcp.RegistryEndpoints.NPM, mcp.RegistryEndpoints.PyPI = npm, pypi
	t.Cleanup(func() {
		mcp.RegistryEndpoints.NPM, mcp.RegistryEndpoints.PyPI = origNPM, origPyPI
	})
}

func noticeServer(t *testing.T, body string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}))
}

// TestMCPUpdateCheckOutdatedPinNotice pins issue #167 (a): a newer
// published version yields exactly one UpdateNotice whose fields match
// the pin and the registry, and StartUpdateChecker delivers a message
// naming server/current/latest with source "mcp-update:<name>".
func TestMCPUpdateCheckOutdatedPinNotice(t *testing.T) {
	var hits atomic.Int32
	stub := noticeServer(t, `{"dist-tags":{"latest":"1.2.0"}}`, &hits)
	defer stub.Close()
	stubRegistries(t, stub.URL, stub.URL)

	servers := []mcp.ServerConfig{{
		Name:    "github",
		Command: []string{"npx", "-y", "@modelcontextprotocol/server-github@1.0.0"},
	}}

	notices := mcp.CheckUpdates(context.Background(), servers)
	if len(notices) != 1 {
		t.Fatalf("CheckUpdates returned %d notices, want 1: %+v", len(notices), notices)
	}
	if got := notices[0]; got.Name != "github" || got.Current != "1.0.0" || got.Latest != "1.2.0" {
		t.Fatalf("notice = %+v, want {github 1.0.0 1.2.0}", got)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("stub requests = %d, want 1", n)
	}

	// Verify the SecurityNotify seam: source and a message naming both
	// versions. A cancelled context stops the ticker goroutine quickly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type delivery struct{ message, source string }
	delivered := make(chan delivery, 1)
	mcp.StartUpdateChecker(ctx, servers, time.Hour, func(message, source string) {
		select {
		case delivered <- delivery{message, source}:
		default:
		}
	}, nil)

	select {
	case got := <-delivered:
		if got.source != "mcp-update:github" {
			t.Fatalf("notify source = %q, want %q", got.source, "mcp-update:github")
		}
		for _, want := range []string{"github", "1.0.0", "1.2.0"} {
			if !strings.Contains(got.message, want) {
				t.Fatalf("notify message %q missing %q", got.message, want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SecurityNotify delivery")
	}
}

// TestMCPUpdateCheckDisabledMakesNoRequests pins issue #167 (b): with
// check_mcp_updates=false the checker is never started and the registry
// stub sees ZERO requests. This is the operator opt-out, not just a
// notification mute.
func TestMCPUpdateCheckDisabledMakesNoRequests(t *testing.T) {
	var hits atomic.Int32
	stub := noticeServer(t, `{"dist-tags":{"latest":"9.9.9"}}`, &hits)
	defer stub.Close()
	stubRegistries(t, stub.URL, stub.URL)

	servers := []mcp.ServerConfig{{
		Name:    "github",
		Command: []string{"npx", "-y", "@modelcontextprotocol/server-github@1.0.0"},
	}}
	called := false
	mcp.StartUpdateCheckerIfEnabled(context.Background(), false, servers, time.Hour,
		func(string, string) { called = true }, nil)

	// The enabled=false path returns synchronously with no check.
	if n := hits.Load(); n != 0 {
		t.Fatalf("disabled checker made %d stub requests, want 0", n)
	}
	if called {
		t.Fatal("disabled checker delivered a notification, want none")
	}
}

// TestMCPUpdateCheckNetworkErrorNoNotice pins issue #167 (c): a registry
// that refuses connections produces no notice and no panic. Failures are
// DEBUG-logged and swallowed so boot and turns cannot be disturbed.
func TestMCPUpdateCheckNetworkErrorNoNotice(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // connection refused from here on

	stubRegistries(t, deadURL, deadURL)
	servers := []mcp.ServerConfig{
		{
			Name:    "github",
			Command: []string{"npx", "-y", "@modelcontextprotocol/server-github@1.0.0"},
		},
		{
			Name:    "fetch",
			Command: []string{"uvx", "mcp-server-fetch==1.0.0"},
		},
	}

	notices := mcp.CheckUpdates(context.Background(), servers)
	if len(notices) != 0 {
		t.Fatalf("CheckUpdates on network error returned %d notices, want 0: %+v", len(notices), notices)
	}
}

// TestMCPUpdateCheckConfigDefaults pins the "installed home" case from
// issue #167: a config file that omits the new keys (or omits the whole
// MCP block) must resolve to the same defaults as a fresh DefaultConfig,
// and an explicit false must survive the JSON5 load boundary to disable
// the checker entirely.
func TestMCPUpdateCheckConfigDefaults(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "meept.json5")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		return path
	}

	cases := []struct {
		name         string
		body         string
		wantEnabled  bool
		wantInterval int
	}{
		{
			name:         "no MCP block",
			body:         `{ "daemon": {} }`,
			wantEnabled:  true,
			wantInterval: 24,
		},
		{
			name:         "MCP block without new keys",
			body:         `{ "mcp": { "enabled": true } }`,
			wantEnabled:  true,
			wantInterval: 24,
		},
		{
			name:         "explicit opt-out",
			body:         `{ "mcp": { "enabled": true, "check_mcp_updates": false, "mcp_update_check_interval_hours": 12 } }`,
			wantEnabled:  false,
			wantInterval: 12,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadJSON5Config(write(t, tc.body))
			if err != nil {
				t.Fatalf("LoadJSON5Config: %v", err)
			}
			if cfg.MCP.CheckUpdates != tc.wantEnabled {
				t.Errorf("CheckUpdates = %v, want %v", cfg.MCP.CheckUpdates, tc.wantEnabled)
			}
			if cfg.MCP.UpdateCheckIntervalHours != tc.wantInterval {
				t.Errorf("UpdateCheckIntervalHours = %d, want %d",
					cfg.MCP.UpdateCheckIntervalHours, tc.wantInterval)
			}
			if cfg.MCP.ConfigFile == "" || cfg.MCP.ConfigFile[0] != '/' {
				t.Errorf("ConfigFile = %q, want expanded default path (partial MCP block must not wipe it)",
					cfg.MCP.ConfigFile)
			}
		})
	}

	// Fresh DefaultConfig must also carry the default-on posture.
	def := config.DefaultConfig()
	if !def.MCP.CheckUpdates || def.MCP.UpdateCheckIntervalHours != 24 {
		t.Fatalf("DefaultConfig MCP = {%v %d}, want {true 24}",
			def.MCP.CheckUpdates, def.MCP.UpdateCheckIntervalHours)
	}
}
