// slack_wiring.go wires the Slack approval channel (hitl-notifications
// leaf 01, Contract C1) into the daemon:
//
//   - the channel registers on the push registry ONLY when [comm.slack]
//     enabled=true (default-off invariant, T4);
//   - the interaction handler is stored on Components so the HTTP server
//     construction site (internal/daemon/daemon.go) can wire it via
//     http.WithSlackInteractions;
//   - the channel's PermissionNotifier fans into the relay through
//     acp.Manager.AppendPermissionNotifier, and the resolve path rides the
//     manager's ResolvePermissionWithApprover seam with Approver Kind
//     "slack".
//
// Config note: the [comm.slack] schema key addition is CLOSED this wave
// (schema.go is owned by another tree), so this wiring reads the block
// from the raw config source file and decodes it into slack.SlackConfig.
// The schema.go field (`Slack SlackConfig` with tag `json:"slack"
// toml:"slack"`) is the next wave's job; once it lands, switch
// readCommSlackConfig to return cfg.Slack directly.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/comm/slack"
	"github.com/caimlas/meept/internal/config"
)

// SlackConfigKey is the JSON5/TOML top-level key for the Slack block.
const SlackConfigKey = "slack"

// readCommSlackConfig decodes the [comm.slack] block (top-level "slack"
// key) from the daemon's config source. The main Config struct does not
// carry the field yet (schema.go closed this wave), so this re-reads the
// source file and pulls the single key. A missing file or missing key is
// the zero config (disabled) - never an error.
func readCommSlackConfig(cfg *config.Config) (slack.SlackConfig, error) {
	var out slack.SlackConfig
	if cfg == nil {
		return out, nil
	}
	path := config.MeeptPath("meept.json5")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("slack: read config source: %w", err)
	}
	var probe map[string]json.RawMessage
	if err := config.LoadJSON5Bytes(raw, &probe); err != nil {
		return out, fmt.Errorf("slack: decode config source: %w", err)
	}
	block, ok := probe[SlackConfigKey]
	if !ok {
		return out, nil
	}
	if err := json.Unmarshal(block, &out); err != nil {
		return out, fmt.Errorf("slack: decode %s block: %w", SlackConfigKey, err)
	}
	return out, nil
}

// wireSlackChannel builds and registers the Slack push channel when
// enabled. Returns (channel, interactionsHandler); both are nil when the
// channel is disabled or the config is invalid/absent - the default-off
// contract is "nothing registered, no route served".
func wireSlackChannel(cfg *config.Config, logger *slog.Logger) (*slack.SlackChannel, *slack.InteractionsHandler) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		return nil, nil
	}
	slackCfg, err := readCommSlackConfig(cfg)
	if err != nil {
		logger.Warn("slack: config block ignored", "error", err)
		return nil, nil
	}
	if err := slackCfg.Validate(); err != nil {
		// enabled=true with missing required keys fails at load time per
		// C2; here it degrades to disabled with a warning (the config
		// loader cannot validate the block until the schema key lands).
		logger.Warn("slack: disabled (invalid config)", "error", err)
		return nil, nil
	}
	if !slackCfg.Enabled {
		return nil, nil
	}

	ch := slack.NewSlackChannel(slackCfg, logger.With("component", "push-channel-slack"))
	handler := slack.NewInteractionsHandler(slackCfg, nil, logger.With("component", "slack-interactions"))
	return ch, handler
}

// wireSlackResolver connects the interaction handler's resolve seam to the
// ACP manager. Called AFTER applyACPFromConfig in NewComponents; a nil
// manager leaves the seam nil (handler acks but cannot resolve - matches
// the "relay not wired" fail-closed lane upstream).
func wireSlackResolver(handler *slack.InteractionsHandler, acpMgr *acp.Manager) {
	if handler == nil || acpMgr == nil {
		return
	}
	handler.SetResolver(func(id string, verb string, approver acp.Approver) error {
		// context.Background(): the resolve runs on its own goroutine
		// after the 200 ack - there is no request context to inherit (and
		// inheriting one would let an HTTP client cancel a human answer).
		return acpMgr.ResolvePermissionWithApprover(context.Background(), id, verb, approver)
	})
}

// fanSlackNotifierIntoRelay appends the Slack channel's notifier to the
// armed relay's fanout. Called only when the channel is enabled AND the
// relay is armed; a disabled/absent relay leaves the manager untouched.
func fanSlackNotifierIntoRelay(acpMgr *acp.Manager, ch *slack.SlackChannel) {
	if acpMgr == nil || ch == nil || !acpMgr.HasPermissionRelay() {
		return
	}
	acpMgr.AppendPermissionNotifier(ch.Notifier())
}
