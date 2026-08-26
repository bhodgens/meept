# Browser Automation Tools

Headless Chrome automation for agents via [chromedp](https://github.com/chromedp/chromedp),
implemented in `internal/browser` (Chrome lifecycle) and
`internal/tools/builtin/browser.go` (tool wrappers).

## Status

Disabled by default. Enable it in `meept.toml`:

```toml
[browser]
enabled = true
# chrome_path = "/usr/local/bin/chromium"   # optional; auto-discovered if empty
# headless = true                           # default true
# max_pages = 3                             # concurrent session limit
```

## Installation

The manager discovers a Chrome/Chromium binary automatically, in this order:

1. `chrome_path` from config (must exist)
2. `google-chrome`, `google-chrome-stable`, `chromium`, `chromium-browser`,
   `chrome` on `PATH`
3. `/Applications/Google Chrome.app/...` (macOS)

If no binary is found while `enabled = true`, daemon startup logs an error and
the tools are not registered.

- **macOS**: install Google Chrome, or `brew install --cask chromium`
- **Debian/Ubuntu**: `apt install chromium-browser` (or `wget -q -O -
  https://dl.google.com/linux/linux_signing_key.pub | sudo apt-key add -` and
  install google-chrome-stable)
- **Arch**: `pacman -S chromium`

## Tools

| Tool                 | Args                  | Risk | Notes                                   |
|----------------------|-----------------------|------|-----------------------------------------|
| `browser_navigate`   | `url`                 | HIGH | http/https only; returns final URL + title |
| `browser_click`      | `selector`            | HIGH | CSS selectors only                      |
| `browser_type`       | `selector`, `text`    | HIGH | sets input values                       |
| `browser_read_text`  | `selector?`           | LOW  | visible text of selector or body        |
| `browser_screenshot` | —                     | LOW  | viewport PNG, returned as base64 data URL with evidence attachment |
| `browser_close`      | —                     | LOW  | shuts down the session's Chrome         |

Sessions are scoped per agent session ID; each gets a singleton headless
Chrome process (max `max_pages` concurrent). All sessions are torn down on
daemon shutdown (`Manager.Close`).

## Security

### SSRF guarding

Every navigation passes through the centralized SSRF guard
(`internal/security/ssrf`, `[security.ssrf]` config):

1. **Pre-navigation check** — scheme allowlist (http/https only) plus IP
   blocklist (loopback, private, link-local, cloud metadata), with hostname
   resolution so DNS-based bypasses are caught.
2. **Post-navigation verification** — after Chrome follows any redirects, the
   *final* location is re-checked against the guard. If a redirect escaped to
   a disallowed host (e.g. an open redirect bouncing to `169.254.169.254`),
   the session's browser is torn down immediately and the navigation fails.
3. Failed loads are also fail-closed: wherever the tab actually landed is
   verified before the error is returned.

Note: unlike `web_fetch`'s HTTP client, chromedp drives a real Chrome process,
so the guard cannot intercept Chrome's own sockets at dial time. The pre-nav +
post-nav location checks are the enforced boundary — see "Limitations" below.

### Scheme gating

Only `http` and `https` URLs are navigable. `file://`, `data:`,
`javascript:` and other schemes are rejected before launch.

### Selector safety

Click/type/read selectors must be plain CSS selectors. Strings shaped like URI
schemes (e.g. `javascript:...`) are rejected.

### Size caps

- Text extraction: 64 KB (`browser.MaxReadTextBytes`)
- Screenshots: 5 MB (`browser.MaxScreenshotBytes`); larger captures error out
  rather than truncate

## Limitations / future work

- ARIA-snapshot budgeting (compact accessibility-tree output for cheaper
  agent context) is deferred; `browser_read_text` covers v1.
- Redirect interception happens post-hoc (location verify) rather than via CDP
  network interception; a hostile origin could still fetch internal resources
  from within the page itself (classic SSRF pivot). Run with network egress
  controls on the Chrome process for high-security deployments.
