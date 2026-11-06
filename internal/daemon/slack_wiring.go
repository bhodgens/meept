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
// Config note: the [comm.slack] block is a registered schema key
// (config.SlackConfig, json key "slack"); this wiring maps the schema
// struct into slack.SlackConfig at wiring time, per the ResolverConfig
// convention (config does not import internal/comm/slack).
package daemon

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/comm/slack"
	"github.com/caimlas/meept/internal/config"
)

// SlackConfigKey is the JSON5/TOML top-level key for the Slack block.
const SlackConfigKey = "slack"

// readCommSlackConfig maps the schema-backed [comm.slack] block
// (config.SlackConfig, parsed by the JSON5 loader into the daemon's
// Config) into slack.SlackConfig. A nil Config is the zero config
// (disabled) — never an error. An empty api_base_url stays empty here;
// slack.SlackConfig.apiURL() applies the production default.
func readCommSlackConfig(cfg *config.Config) (slack.SlackConfig, error) {
	var out slack.SlackConfig
	if cfg == nil {
		return out, nil
	}
	s := cfg.Slack
	out = slack.SlackConfig{
		Enabled:        s.Enabled,
		BotToken:       s.BotToken,
		SigningSecret:  s.SigningSecret,
		ChannelID:      s.ChannelID,
		AllowedUserIDs: append([]string(nil), s.AllowedUserIDs...),
		APIBaseURL:     s.APIBaseURL,
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
		// C2 (config.SlackConfig.ValidateEnabled is the schema-side twin);
		// here it degrades to disabled with a warning so a half-configured
		// block never hard-fails daemon boot.
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
