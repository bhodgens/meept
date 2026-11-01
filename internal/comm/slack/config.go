// Package slack implements the HITL Slack approval channel (leaf
// 01-slack-channel of docs/plans/20261031-hitl-notifications): a
// services.PushChannel that publishes pending ACP permission requests as
// interactive Slack messages, verifies Slack request signatures on the
// interaction callback, and resolves them through the existing ACP human
// permission relay with Approver Kind "slack".
//
// Security posture (research-notes.md section A, authoritative):
//   - interaction callbacks are authenticated by the v0 HMAC-SHA256 request
//     signature over "v0:<timestamp>:<raw body>" (constant-time compare) and
//     a 5-minute replay window - NOT by the bearer-key auth middleware,
//     which Slack cannot satisfy;
//   - authorization to answer is signature + AllowedUserIDs membership;
//     the payload's response_url is never an authorization boundary;
//   - the HTTP 200 ack goes out within the 3 s Slack budget; the relay
//     resolve runs async on a goroutine;
//   - the relay's fail-closed [acp] relay_timeout stays the ONLY timeout
//     authority - this package adds no second timer, and a late click
//     finds no pending request and gets a polite "no pending request".
package slack

import (
	"fmt"
	"strings"
)

// SlackConfig is the [comm.slack] configuration block. NOTE: the config
// schema key addition is CLOSED for this wave (another tree owns
// internal/config/schema.go), so the struct ships in-package and the
// daemon wiring reads it from the [comm.slack] JSON5 key by hand-rolled
// decoding against the raw config map - the schema.go struct + loader
// registration (field on Config, tag `json:"slack" toml:"slack"`) is
// tracked for the next wave.
//
// Values support the ${VAR} and file: env/file indirection conventions
// used by other secret-bearing config values.
type SlackConfig struct {
	// Enabled turns the channel on. Default false: nothing registers, no
	// route is served, no goroutine runs (default-off invariant, T4).
	Enabled bool `json:"enabled" toml:"enabled"`
	// BotToken is the Slack bot token (xoxb-...), scope chat:write (the
	// minimum set: chat.postMessage + chat.update both live under it).
	BotToken string `json:"bot_token" toml:"bot_token"`
	// SigningSecret is the Slack app signing secret used to verify
	// interaction callback signatures.
	SigningSecret string `json:"signing_secret" toml:"signing_secret"`
	// ChannelID is the fixed Slack channel the approval cards post to.
	ChannelID string `json:"channel_id" toml:"channel_id"`
	// AllowedUserIDs are the Slack user IDs allowed to answer approval
	// requests. Empty allows NOBODY (fail closed) - unlike the Telegram
	// precedent where empty means all.
	AllowedUserIDs []string `json:"allowed_user_ids" toml:"allowed_user_ids"`
	// APIBaseURL overrides the Slack Web API base (default
	// https://slack.com/api); hermetic tests point it at an httptest
	// server. Never logged.
	APIBaseURL string `json:"api_base_url" toml:"api_base_url"`
}

// DefaultAPIBaseURL is the production Slack Web API base URL.
const DefaultAPIBaseURL = "https://slack.com/api"

// Validate enforces the C2 load-time contract: when enabled, bot_token,
// signing_secret and channel_id must all be non-empty.
func (c SlackConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	var missing []string
	if strings.TrimSpace(c.BotToken) == "" {
		missing = append(missing, "bot_token")
	}
	if strings.TrimSpace(c.SigningSecret) == "" {
		missing = append(missing, "signing_secret")
	}
	if strings.TrimSpace(c.ChannelID) == "" {
		missing = append(missing, "channel_id")
	}
	if len(missing) > 0 {
		return fmt.Errorf("comm.slack: enabled=true requires non-empty %s", strings.Join(missing, ", "))
	}
	return nil
}

// apiURL resolves the Web API base, falling back to the production URL.
func (c SlackConfig) apiURL() string {
	if u := strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/"); u != "" {
		return u
	}
	return DefaultAPIBaseURL
}
