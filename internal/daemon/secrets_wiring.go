package daemon

// secrets_wiring.go — Egress secret-injection proxy wiring
// (plans/containment-and-computer-use/04-egress-proxy.md).
//
// Constructs the secret Broker from [secrets.sources] and, when
// [secrets.proxy] enabled=true, starts the loopback egress proxy that
// resolves MEEPT_SECRET:<name> placeholders toward allowlisted hosts. The
// bound address is logged loudly and exposed via SecretsProxyStatus for the
// status surface.
//
// Fail-soft: disabled or misconfigured broker setups leave both component
// fields nil and never abort daemon startup (matches the shadow-manager
// precedent); failures are logged at Error level with the aggregated reason.
// Proxy bind failures DO return an error: an operator explicitly enabled the
// proxy, so silently continuing would leave placeholders permanently
// unresolvable.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/secrets"
)

// wireSecretsProxy builds the secret broker and starts the egress proxy when
// [secrets.proxy] enabled=true. Called from daemon.go alongside the other
// wire* steps.
func (c *Components) wireSecretsProxy(ctx context.Context) error {
	if c.Config == nil || !c.Config.Secrets.Proxy.Enabled {
		return nil
	}

	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}

	broker, err := secrets.NewBroker(secrets.Config(c.Config.Secrets.Sources), logger.With("component", "secret-broker"))
	if err != nil {
		// Fail soft: children keep receiving unresolvable placeholders
		// (pre-leaf-04 behavior) rather than losing the daemon. The
		// aggregated error names every failing source.
		logger.Error("secret broker load failed; egress proxy disabled", "error", err)
		return nil
	}
	c.SecretBroker = broker

	cfg := secrets.ProxyConfig{
		Enabled: true,
		Listen:  c.Config.Secrets.Proxy.Listen,
	}
	proxy := secrets.NewProxy(broker, cfg, logger.With("component", "secrets-proxy"))
	addr, err := proxy.Start(ctx)
	if err != nil {
		c.SecretBroker = nil
		return fmt.Errorf("start secrets egress proxy on %q: %w", cfg.Listen, err)
	}
	c.SecretsProxy = proxy

	logger.Info("secrets egress proxy started",
		"addr", addr,
		"hint", "route placeholder-bearing HTTP through this loopback address to resolve MEEPT_SECRET placeholders",
	)
	return nil
}

// SecretsProxyStatus returns the status-surface view of the egress proxy:
// {"enabled", "addr", "leak_attempts"}, or nil when disabled/unwired.
func (c *Components) SecretsProxyStatus() map[string]any {
	if c == nil || c.SecretsProxy == nil {
		return nil
	}
	return map[string]any{
		"enabled":       true,
		"addr":          c.SecretsProxy.Addr(),
		"leak_attempts": c.SecretsProxy.LeakAttempts(),
	}
}
