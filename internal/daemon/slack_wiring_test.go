package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/comm/slack"
	"github.com/caimlas/meept/internal/config"
)

// loadConfigJSON5 parses a meept.json5 snippet through the REAL loader
// into a DefaultConfig, so the wiring tests exercise the schema-backed
// [comm.slack] path end to end (parse -> Config.Slack -> bridge), not a
// hand-built struct.
func loadConfigJSON5(t *testing.T, content string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := config.LoadJSON5(path, cfg); err != nil {
		t.Fatalf("load json5: %v", err)
	}
	return cfg
}

const slackEnabledCfg = `{
  // slack approval channel (hitl-notifications leaf 01)
  "slack": {
    "enabled": true,
    "bot_token": "xoxb-test",
    "signing_secret": "shhh",
    "channel_id": "C123",
    "allowed_user_ids": ["U123ABC"],
    "api_base_url": "http://127.0.0.1:1"
  }
}`

const slackAbsentCfg = `{
  "daemon": { "data_dir": "/tmp/meept-test" }
}`

func TestWireSlackChannelDefaultOff(t *testing.T) {
	// THE default-off invariant (T4): with no [comm.slack] block the
	// schema default is disabled, and the wiring produces NO channel and
	// NO handler - nothing registers, no route is served.
	if cfg := config.DefaultConfig(); cfg.Slack.Enabled {
		t.Fatal("DefaultConfig: comm.slack must be disabled")
	}
	cfg := loadConfigJSON5(t, slackAbsentCfg)
	ch, h := wireSlackChannel(cfg, slog.Default())
	if ch != nil || h != nil {
		t.Fatalf("default-off wiring returned (%v, %v), want (nil, nil)", ch, h)
	}
}

func TestWireSlackChannelDisabledExplicit(t *testing.T) {
	cfg := loadConfigJSON5(t, `{"slack": {"enabled": false, "bot_token": "x", "signing_secret": "y", "channel_id": "C"}}`)
	ch, h := wireSlackChannel(cfg, slog.Default())
	if ch != nil || h != nil {
		t.Fatalf("enabled=false wiring returned (%v, %v), want (nil, nil)", ch, h)
	}
}

func TestWireSlackChannelEnabledRegisters(t *testing.T) {
	// Enabled wiring builds exactly one channel + handler pair (T4), fed
	// through the schema-backed config field.
	cfg := loadConfigJSON5(t, slackEnabledCfg)
	ch, h := wireSlackChannel(cfg, slog.Default())
	if ch == nil || h == nil {
		t.Fatal("enabled wiring returned nil channel/handler")
	}
	if got := h.Config(); got.ChannelID != "C123" || len(got.AllowedUserIDs) != 1 {
		t.Errorf("handler config = %+v", got)
	}
}

func TestWireSlackChannelInvalidEnabledDegradesOff(t *testing.T) {
	// enabled=true with missing signing_secret is the C2 validation
	// failure; wiring degrades to disabled with a warning.
	cfg := loadConfigJSON5(t, `{"slack": {"enabled": true, "bot_token": "xoxb", "channel_id": "C"}}`)
	ch, h := wireSlackChannel(cfg, slog.Default())
	if ch != nil || h != nil {
		t.Fatalf("invalid enabled wiring returned (%v, %v), want (nil, nil)", ch, h)
	}
}

func TestReadCommSlackConfigNilSafe(t *testing.T) {
	// nil Config -> zero (disabled), never an error.
	got, err := readCommSlackConfig(nil)
	if err != nil || got.Enabled {
		t.Fatalf("readCommSlackConfig(nil) = (%+v, %v), want disabled + nil error", got, err)
	}
}

// countingNotifier records fanout deliveries.
type countingNotifier struct {
	mu       sync.Mutex
	reqs     int
	resolved int
}

func (c *countingNotifier) NotifyPermissionRequested(p acp.PendingPermission) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs++
	return true
}

func (c *countingNotifier) NotifyPermissionResolved(p acp.PendingPermission, decision, reason string, a acp.Approver) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resolved++
}

func TestFanSlackNotifierIntoRelay(t *testing.T) {
	mgr := &acp.Manager{}
	relay := acp.NewRelay()
	base := &countingNotifier{}
	mgr.SetPermissionRelay(relay, base)

	ch := slack.NewSlackChannel(slack.SlackConfig{Enabled: true}, slog.Default())
	fanSlackNotifierIntoRelay(mgr, ch)

	// A disabled relay (no SetPermissionRelay) must stay untouched.
	bare := &acp.Manager{}
	fanSlackNotifierIntoRelay(bare, ch) // must not panic / must be a no-op
}

func TestWireSlackResolverNilSafe(t *testing.T) {
	// nil handler or nil manager: no-op, no panic.
	wireSlackResolver(nil, nil)
	h := slack.NewInteractionsHandler(slack.SlackConfig{Enabled: true}, nil, slog.Default())
	wireSlackResolver(h, nil)
}
