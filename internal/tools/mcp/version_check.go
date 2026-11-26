package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Issue #167: notification-only MCP version check. At boot and every
// configured interval the daemon compares each enabled server's exact
// npx/uvx pin against the latest published version on npm/PyPI. An
// outdated pin is surfaced through the daemon's SecurityNotify seam.
// Nothing here ever rewrites mcp_servers.json5, restarts a server, or
// otherwise auto-updates a pin — the operator decides when to bump.

const (
	defaultNPMEndpoint  = "https://registry.npmjs.org"
	defaultPyPIEndpoint = "https://pypi.org"
	// updateCheckTimeout bounds BOTH the HTTP client and a whole check
	// pass, so a slow registry cannot hold up boot or a tick.
	updateCheckTimeout = 10 * time.Second
)

// RegistryEndpoints points the version checker at the npm and PyPI
// registry roots. Tests override these with an httptest.Server base URL
// (no live network in the hermetic e2e tier); production uses the real
// registries. Reset the fields after overriding them in a test.
var RegistryEndpoints = struct {
	NPM  string
	PyPI string
}{
	NPM:  defaultNPMEndpoint,
	PyPI: defaultPyPIEndpoint,
}

// UpdateNotice reports ONE outdated pin. Fields are exact so callers can
// build a deterministic notification message and tests can assert them.
type UpdateNotice struct {
	Name    string `json:"name"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
}

// String is a stable, operator-facing rendering naming the server and
// both versions. It is what the SecurityNotify seam receives.
func (n UpdateNotice) String() string {
	return fmt.Sprintf(
		"MCP server %q is pinned to %s but %s is available on the registry; "+
			"no automatic update was performed — review and bump the pin in "+
			"~/.meept/mcp_servers.json5.",
		n.Name, n.Current, n.Latest,
	)
}

// noticeSource is the SecurityNotify source for an outdated-pin notice.
func noticeSource(name string) string { return "mcp-update:" + name }

// checkClient is a bounded client shared by all checks. The 10s timeout
// covers connection, headers and body for a single registry request.
var checkClient = &http.Client{Timeout: updateCheckTimeout}

// updateChecker holds per-run dependencies so tests can inject a logger
// and the daemon can pass its SecurityNotify seam without global state.
type updateChecker struct {
	client *http.Client
	logger *slog.Logger
}

func newUpdateChecker(logger *slog.Logger) *updateChecker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &updateChecker{client: checkClient, logger: logger}
}

// CheckUpdates queries the npm/PyPI registries for every enabled server
// with a parseable exact pin and returns one notice per outdated pin.
// Network errors and unparseable versions are logged at DEBUG and skipped:
// a registry outage must never fail a boot or a daemon turn. The context
// bounds the whole pass; callers can pass a short-lived context safely.
func CheckUpdates(ctx context.Context, servers []ServerConfig) []UpdateNotice {
	return newUpdateChecker(nil).check(ctx, servers)
}

func (c *updateChecker) check(ctx context.Context, servers []ServerConfig) []UpdateNotice {
	checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()

	notices := make([]UpdateNotice, 0, len(servers))
	for _, server := range servers {
		if checkCtx.Err() != nil {
			break
		}
		if !server.IsEnabled() {
			continue
		}
		pin, ok := ParsePin(server.Command)
		if !ok {
			c.logger.Debug("mcp update check: skipping server with no parseable pin",
				"server", server.Name)
			continue
		}
		latest, err := c.latestVersion(checkCtx, pin)
		if err != nil {
			c.logger.Debug("mcp update check: registry lookup failed",
				"server", server.Name, "registry", pin.Registry,
				"package", pin.Pkg, "error", err)
			continue
		}
		if _, ok := ParseVersion(pin.Version); !ok {
			c.logger.Debug("mcp update check: unparseable pinned version",
				"server", server.Name, "version", pin.Version)
			continue
		}
		if _, ok := ParseVersion(latest); !ok {
			c.logger.Debug("mcp update check: unparseable registry version",
				"server", server.Name, "latest", latest)
			continue
		}
		if CompareVersions(latest, pin.Version) <= 0 {
			continue
		}
		notices = append(notices, UpdateNotice{
			Name:    server.Name,
			Current: pin.Version,
			Latest:  latest,
		})
	}
	return notices
}

// latestVersion reads the published latest version for a pin from the
// matching registry. npm exposes dist-tags.latest; PyPI exposes
// info.version. Both responses are small and read with a body cap.
func (c *updateChecker) latestVersion(ctx context.Context, pin Pin) (string, error) {
	var endpoint string
	switch pin.Registry {
	case "npm":
		endpoint = strings.TrimRight(RegistryEndpoints.NPM, "/") + "/" +
			url.PathEscape(pin.Pkg)
	case "pypi":
		endpoint = strings.TrimRight(RegistryEndpoints.PyPI, "/") + "/pypi/" +
			url.PathEscape(pin.Pkg) + "/json"
	default:
		return "", fmt.Errorf("unsupported registry %q", pin.Registry)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry %s returned %s for %s",
			pin.Registry, resp.Status, pin.Pkg)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if pin.Registry == "npm" {
		var doc struct {
			DistTags struct {
				Latest string `json:"latest"`
			} `json:"dist-tags"`
		}
		if err := json.Unmarshal(body, &doc); err != nil {
			return "", err
		}
		if doc.DistTags.Latest == "" {
			return "", fmt.Errorf("npm response for %s has no dist-tags.latest", pin.Pkg)
		}
		return doc.DistTags.Latest, nil
	}
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", err
	}
	if doc.Info.Version == "" {
		return "", fmt.Errorf("pypi response for %s has no info.version", pin.Pkg)
	}
	return doc.Info.Version, nil
}

// StartUpdateCheckerIfEnabled is the config-gated entry point the daemon
// boot calls. With enabled=false it returns immediately WITHOUT issuing
// any registry request — an operator opt-out disables the checker
// entirely, not just its notifications.
func StartUpdateCheckerIfEnabled(
	ctx context.Context,
	enabled bool,
	servers []ServerConfig,
	interval time.Duration,
	notify func(string, string),
	logger *slog.Logger,
) {
	if !enabled {
		return
	}
	StartUpdateChecker(ctx, servers, interval, notify, logger)
}

// StartUpdateChecker performs ONE check immediately, then ticks every
// interval until ctx is cancelled. Each outdated pin is delivered to
// notify (nil = log only) with source "mcp-update:<name>". All failures
// are logged and swallowed: the checker never fails boot or a turn, and
// it never updates anything.
func StartUpdateChecker(
	ctx context.Context,
	servers []ServerConfig,
	interval time.Duration,
	notify func(string, string),
	logger *slog.Logger,
) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	checker := newUpdateChecker(logger)

	// First check at start (boot). Bounded by updateCheckTimeout so a
	// slow registry cannot hold the daemon boot hostage.
	checker.emit(ctx, servers, notify)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				checker.emit(ctx, servers, notify)
			}
		}
	}()
}

func (c *updateChecker) emit(
	ctx context.Context,
	servers []ServerConfig,
	notify func(string, string),
) {
	for _, notice := range c.check(ctx, servers) {
		message, source := notice.String(), noticeSource(notice.Name)
		c.logger.Info("MCP pin is outdated",
			"server", notice.Name, "current", notice.Current,
			"latest", notice.Latest, "source", source)
		if notify == nil {
			continue
		}
		notify(message, source)
	}
}
