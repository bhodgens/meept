package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/secrets"
)

// wireGitHubTools registers the github_* tools when [github].enabled with
// a resolvable token source. It runs from NewComponents right after the
// builtin registration; a registration failure (pool construction, the
// builtin factory seam being absent from this binary) logs and degrades to
// no github tools — the daemon's remaining surface is untouched.
//
// Token-source precedence (documented on GitHubConfig):
//  1. TokenSecret names an existing regular file (after ~ / MEEPT_HOME
//     expansion) with owner-only perms: FileTokenSource — the token is
//     read from disk at request time.
//  2. Otherwise TokenSecret is a named secret: SecretsTokenSource against
//     the [secrets] broker when one was wired (wireSecretsProxy,
//     daemon.go). When the broker is absent the tools still register
//     (membership) but fail closed at Execute with an error naming the
//     missing secret source — never an unauthenticated request.
func (c *Components) wireGitHubTools() {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cfg := c.Config.GitHub
	if !cfg.Enabled {
		logger.Debug("github tools not registered", "reason", "github block disabled")
		return
	}
	if cfg.TokenSecret == "" {
		logger.Warn("github block enabled but token_secret is empty; no github tools registered")
		return
	}

	src, source := c.resolveGitHubTokenSource(cfg.TokenSecret, logger)
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  src,
		BaseURL: config.ExpandMeeptPath(cfg.BaseURL),
	})
	if err != nil {
		logger.Error("github tool registration failed: pool construction", "error", err)
		return
	}

	var sink githubsvc.ReArmSink
	if c.InstructionStore != nil {
		sink = githubsvc.NewInstructionReArmSink(c.InstructionStore, logger.With("component", "github-rearm"))
	} else {
		// Parking still returns its parked result (with its waitID); the
		// ScheduleReArm call is skipped and the tool logs the skip.
		logger.Warn("github_ci_wait re-arm sink unavailable: instruction store not wired; parked CI waits will not re-arm automatically")
	}

	if err := githubsvc.RegisterGitHubTools(c.ToolRegistry, githubsvc.RegistrationOptions{
		Pool:         pool,
		DefaultOwner: cfg.DefaultOwner,
		DefaultRepo:  cfg.DefaultRepo,
		Ledger:       c.EffectsLedger,
		Sink:         sink,
	}); err != nil {
		logger.Error("github tool registration failed", "error", err)
		return
	}
	c.GitHubPool = pool
	logger.Info("github tools registered",
		"token_source", source,
		"default_owner", cfg.DefaultOwner,
		"default_repo", cfg.DefaultRepo,
	)
}

// ensureSecretBroker returns the daemon's secrets broker, building it on
// demand from [secrets].sources when the egress proxy has not already
// constructed one (wireSecretsProxy only builds the broker with the proxy
// enabled). A broker load failure (missing env/file for a declared source)
// is logged at Error with the broker's aggregated reason and returns nil;
// the caller then fails closed with a nameable error. No-op when sources
// are empty (nothing to resolve a named token against).
func (c *Components) ensureSecretBroker(logger *slog.Logger) *secrets.Broker {
	if c.SecretBroker != nil {
		return c.SecretBroker
	}
	if c.Config == nil || len(c.Config.Secrets.Sources) == 0 {
		return nil
	}
	broker, err := secrets.NewBroker(secrets.Config(c.Config.Secrets.Sources), logger.With("component", "secret-broker"))
	if err != nil {
		logger.Error("secret broker load failed; github named-secret token source unavailable", "error", err)
		return nil
	}
	c.SecretBroker = broker
	logger.Info("secrets broker built for github token resolution", "secrets", len(c.Config.Secrets.Sources))
	return broker
}

// resolveGitHubTokenSource picks the token source per the precedence in
// wireGitHubTools and returns it with a short label for logging (never the
// token value).
func (c *Components) resolveGitHubTokenSource(tokenSecret string, logger *slog.Logger) (githubsvc.TokenSource, string) {
	// 1. Token file: an existing regular path (after ~ / MEEPT_HOME
	//    expansion).
	path := config.ExpandMeeptPath(tokenSecret)
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		if fi.Mode().Perm()&0o077 != 0 {
			// Not owner-only: the token would be readable by others. Refuse
			// it (fail closed) rather than hand a group/world-readable
			// token to every tool call.
			logger.Warn("github token file has group/other access bits; refusing it — chmod it to 0600 or use a [secrets] broker name",
				"path", path)
			return githubsvc.ErrTokenSource{Err: fmt.Errorf(
					"githubsvc: no GitHub token resolved: token file %s has group/other access bits; chmod it to 0600", path)},
				"file-unsafe-perms"
		}
		return githubsvc.FileTokenSource{Path: path}, "file"
	}

	// 2. Secrets broker child value. The broker is normally built by
	//    wireSecretsProxy when [secrets.proxy] is on; when it is off but
	//    [secrets].sources still declares the token's named secret, build
	//    the broker on demand (read-only after load, safe for the
	//    per-request token resolution that follows). No sources -> fail
	//    closed with a nameable error.
	if broker := c.ensureSecretBroker(logger); broker != nil {
		return githubsvc.SecretsTokenSource{Broker: broker, SecretName: tokenSecret}, "secrets-broker"
	}

	// 3. No broker: fail closed at request time with a nameable error.
	return githubsvc.ErrTokenSource{Err: fmt.Errorf(
			"githubsvc: no GitHub token resolved: secret %q configured but no secrets broker is wired ([secrets].proxy enabled=false)", tokenSecret)},
		"unresolved-secret"
}

// startGitHubReArmTicker launches the periodic instruction re-sync when the
// github tool set is registered with a live instruction scheduler. A
// parked github_ci_wait writes its cron re-arm row to the instruction store
// at park time; SyncCronInstructions only sees it on the next sync, and
// the start-time sync already ran before any park could happen. Without
// this ticker, a mid-life park would not re-check until the next daemon
// boot — the whole point of the park is the wait re-checking on its own,
// so the re-sync interval matters. 30s keeps one extra poll per park
// bounded without hammering the scheduler. No-op when either dependency
// is absent (github disabled, no scheduler).
func (c *Components) startGitHubReArmTicker() {
	if c.GitHubPool == nil || c.InstructionScheduler == nil {
		return
	}
	// Capture the channel in a local so the goroutine's deferred close can
	// never reach a channel a later start replaced the field with
	// (deferred expressions evaluate at exit time, and a double-close of
	// the shared field would panic).
	done := make(chan struct{})
	c.GitHubReArmTickerDone = done
	ticker := time.NewTicker(githubReArmTickInterval)
	go func() { //nolint:gosec // daemon-lifetime goroutine, exits via c.ctx like its siblings
		defer close(done)
		for {
			select {
			case <-ticker.C:
				if err := c.InstructionScheduler.SyncCronInstructions(); err != nil {
					c.Logger.Debug("github re-arm instruction re-sync", "error", err)
				}
			case <-c.ctx.Done():
				return
			}
		}
	}()
	c.Logger.Info("github instruction re-arm ticker started", "interval_seconds", int(githubReArmTickInterval.Seconds()))
}

// githubReArmTickInterval is how often the github re-arm ticker re-syncs
// cron instructions into the job scheduler.
const githubReArmTickInterval = 30 * time.Second

// stopGitHubReArmTicker stops the re-arm ticker and closes the GitHub pool.
// Safe to call when either was never started. The done channel is closed
// exactly once via sync.Once; it is NEVER nil'd, because a nil channel in a
// select blocks forever (the close-then-nil regression class this repo's
// channel-nilafterclose hook guards against).
func (c *Components) stopGitHubReArmTicker() {
	if c.GitHubReArmTickerDone != nil {
		ticker := time.NewTimer(5 * time.Second)
		select {
		case <-c.GitHubReArmTickerDone:
		case <-ticker.C:
			c.Logger.Warn("github re-arm ticker did not stop within 5s")
		}
	}
	if c.GitHubPool != nil {
		c.GitHubPool.Close()
		c.GitHubPool = nil
	}
}
