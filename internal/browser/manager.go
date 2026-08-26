// Package browser manages per-session headless Chrome instances driven
// through the Chrome DevTools Protocol (chromedp), with SSRF guarding on
// every navigation including post-redirect location verification.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/chromedp/chromedp"
)

// Limits applied by the manager.
const (
	// MaxReadTextBytes caps text extraction at 64 KB.
	MaxReadTextBytes = 64 * 1024
	// MaxScreenshotBytes caps screenshots at 5 MB.
	MaxScreenshotBytes = 5 * 1024 * 1024
	// defaultSessionID is used when callers do not supply one.
	defaultSessionID = "default"
)

// Config configures the browser manager.
type Config struct {
	// Enabled gates the whole browser tool family. When false,
	// NewManager succeeds but reports Disabled and the tools are not
	// registered.
	Enabled bool
	// ChromePath overrides binary discovery. Empty means auto-discover.
	ChromePath string
	// Headless launches without a visible window. Default true.
	Headless bool
	// MaxPages caps concurrent sessions.
	MaxPages int
}

// ErrDisabled is returned by operations on a disabled manager.
var ErrDisabled = errors.New("browser: disabled")

// ErrNoSession is returned when operating on an unknown session.
var ErrNoSession = errors.New("browser: unknown session")

// session bundles the chromedp contexts for one logical agent session.
type session struct {
	allocCtx context.Context
	allocCxl context.CancelFunc
	ctx      context.Context
	cxl      context.CancelFunc
}

// Manager owns headless Chrome lifecycles keyed by session ID. A singleton
// Chrome instance backs each session.
type Manager struct {
	cfg    Config
	guard  *ssrf.Guard
	logger *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
}

// discoverChrome locates a usable Chrome/Chromium binary.
func discoverChrome(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("browser: chrome_path %q: %w", override, err)
		}
		return override, nil
	}
	candidates := []string{
		"google-chrome", "google-chrome-stable", "chromium",
		"chromium-browser", "chrome",
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("browser: no chrome/chromium binary found")
}

// NewManager creates a Manager. When cfg.Enabled is true it fails fast if no
// Chrome binary can be discovered. When Enabled is false the returned manager
// is inert and all operations fail with ErrDisabled; callers treat that as
// "tools not registered".
func NewManager(cfg Config, guard *ssrf.Guard, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if !cfg.Enabled {
		return &Manager{cfg: cfg, guard: guard, logger: logger}, nil
	}
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = 3
	}
	if _, err := discoverChrome(cfg.ChromePath); err != nil {
		return nil, err
	}
	if guard == nil {
		guard = ssrf.DefaultGuard()
	}
	return &Manager{
		cfg:      cfg,
		guard:    guard,
		logger:   logger,
		sessions: make(map[string]*session),
	}, nil
}

// Disabled reports whether the manager was constructed with enabled=false.
func (m *Manager) Disabled() bool { return !m.cfg.Enabled }

// sessionFor returns (creating if needed) the session's Chrome context.
func (m *Manager) sessionFor(ctx context.Context, sessionID string) (*session, error) {
	if m.cfg.Enabled && sessionID == "" {
		sessionID = defaultSessionID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[sessionID]; ok {
		return s, nil
	}
	if len(m.sessions) >= m.cfg.MaxPages {
		return nil, fmt.Errorf("browser: session limit reached (%d)", m.cfg.MaxPages)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	if m.cfg.Headless {
		opts = append(opts, chromedp.Headless)
	}
	if m.cfg.ChromePath != "" {
		opts = append(opts, chromedp.ExecPath(m.cfg.ChromePath))
	}
	allocCtx, allocCxl := chromedp.NewExecAllocator(ctx, opts...)
	cdpCtx, cdpcxl := chromedp.NewContext(allocCtx)
	// Force the browser process to launch now so failures surface here.
	// chromedp.Run blocks on process spawn — run OUTSIDE m.mu via a copy of
	// the fields it needs; the session map insert happens after under lock.
	launchCtx := cdpCtx
	if err := func() error {
		m.mu.Unlock()
		defer m.mu.Lock()
		return chromedp.Run(launchCtx) //nolint:mutexio // intentional: launch outside the sessions-map lock
	}(); err != nil {
		cdpcxl()
		allocCxl()
		return nil, fmt.Errorf("browser: launch failed: %w", err)
	}
	s := &session{allocCtx: allocCtx, allocCxl: allocCxl, ctx: cdpCtx, cxl: cdpcxl}
	m.sessions[sessionID] = s
	return s, nil
}

func (m *Manager) getSession(sessionID string) (*session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// validateSelector enforces CSS-only selectors: any "scheme:"-shaped string
// (e.g. "javascript:alert(1)") is rejected.
func validateSelector(sel string) error {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return errors.New("browser: empty selector")
	}
	for _, part := range strings.Fields(sel) {
		if i := strings.Index(part, ":"); i > 0 {
			head := strings.ToLower(part[:i])
			// Pseudo-classes/pseudo-elements attach directly to a tag,
			// class, or id character sequence; anything that looks like a
			// URI scheme (letters followed by colon then more of the same)
			// with a slash or semicolon later is treated as hostile.
			if (strings.Contains(part[i:], "/") || strings.Contains(part, ";")) &&
				isSchemeLike(head) {
				return fmt.Errorf("browser: selector %q is not a css selector", sel)
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(sel), "javascript:") ||
		strings.Contains(strings.ToLower(sel), "javascript:") {
		return fmt.Errorf("browser: selector %q is not a css selector", sel)
	}
	return nil
}

func isSchemeLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			(r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// checkURLEntry validates a URL before navigation: scheme allowlist and IP
// blocklist enforcement through the SSRF guard.
func (m *Manager) checkURLEntry(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("browser: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("browser: scheme %q not allowed", u.Scheme)
	}
	return m.guard.CheckURL(rawURL)
}

// checkURLFinal re-validates the post-navigation location. This closes the
// redirect-escape window where Chrome follows a redirect the guard never saw.
func (m *Manager) checkURLFinal(finalURL string) error {
	u, err := url.Parse(finalURL)
	if err != nil {
		return fmt.Errorf("browser: invalid final url %q: %w", finalURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("browser: final scheme %q not allowed", u.Scheme)
	}
	if err := m.guard.CheckURL(finalURL); err != nil {
		return fmt.Errorf("browser: redirected location blocked: %w", err)
	}
	return nil
}

// Navigate navigates the session's tab to rawURL and returns the final
// location and page title. The URL is checked against the SSRF guard both
// before navigation and again on the final location (redirect defense).
func (m *Manager) Navigate(ctx context.Context, sessionID, rawURL string) (string, string, error) {
	if m.Disabled() {
		return "", "", ErrDisabled
	}
	if err := m.checkURLEntry(rawURL); err != nil {
		return "", "", fmt.Errorf("navigation blocked: %w", err)
	}
	s, err := m.sessionFor(ctx, sessionID)
	if err != nil {
		return "", "", err
	}
	var finalURL, title string
	runErr := chromedp.Run(s.ctx,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body"),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	)
	if runErr != nil {
		// Fail closed: even on a failed load, verify wherever the tab
		// actually ended up. A disallowed final location tears the session
		// down (redirect-escape defense).
		if locErr := chromedp.Run(s.ctx, chromedp.Location(&finalURL)); locErr != nil {
			slog.Debug("browser: post-failure location probe failed", "error", locErr)
		}
		if ferr := m.checkURLFinal(finalURL); ferr != nil {
			m.CloseSession(context.Background(), sessionID)
			return "", "", fmt.Errorf("navigation failed and %v", ferr)
		}
		return "", "", fmt.Errorf("browser: navigate failed: %w", runErr)
	}
	if err := m.checkURLFinal(finalURL); err != nil {
		// Redirect escape attempt: tear down the page so the session does
		// not keep pointing at a disallowed origin.
		m.CloseSession(context.Background(), sessionID)
		return "", "", err
	}
	return finalURL, title, nil
}

// Click clicks the element matched by a CSS-only selector.
func (m *Manager) Click(ctx context.Context, sessionID, selector string) error {
	if m.Disabled() {
		return ErrDisabled
	}
	if err := validateSelector(selector); err != nil {
		return err
	}
	s, ok := m.getSession(sessionID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoSession, sessionID)
	}
	if err := chromedp.Run(s.ctx, chromedp.Click(selector, chromedp.NodeVisible)); err != nil {
		return fmt.Errorf("browser: click %q: %w", selector, err)
	}
	return nil
}

// Type types text into the element matched by a CSS-only selector.
func (m *Manager) Type(ctx context.Context, sessionID, selector, text string) error {
	if m.Disabled() {
		return ErrDisabled
	}
	if err := validateSelector(selector); err != nil {
		return err
	}
	s, ok := m.getSession(sessionID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoSession, sessionID)
	}
	if err := chromedp.Run(s.ctx, chromedp.SetValue(selector, text, chromedp.ByQuery)); err != nil {
		return fmt.Errorf("browser: type into %q: %w", selector, err)
	}
	return nil
}

// ReadText extracts visible text from the selector (whole body when empty),
// capped at MaxReadTextBytes.
func (m *Manager) ReadText(ctx context.Context, sessionID, selector string) (string, error) {
	if m.Disabled() {
		return "", ErrDisabled
	}
	sel := selector
	if strings.TrimSpace(sel) == "" {
		sel = "body"
	} else if err := validateSelector(sel); err != nil {
		return "", err
	}
	s, ok := m.getSession(sessionID)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoSession, sessionID)
	}
	// Extract text via innerText: deterministic and avoids visibility-
	// polling edge cases on elements like <body>.
	selJSON, _ := json.Marshal(sel)
	var text *string
	err := chromedp.Run(s.ctx, chromedp.Evaluate(
		fmt.Sprintf("(function(){var el=document.querySelector(%s);return el?el.innerText:null})()", selJSON),
		&text,
	))
	if err != nil {
		return "", fmt.Errorf("browser: read text from %q: %w", sel, err)
	}
	if text == nil {
		return "", fmt.Errorf("browser: selector %q not found", sel)
	}
	truncated := false
	out := *text
	if len(out) > MaxReadTextBytes {
		out = out[:MaxReadTextBytes]
		truncated = true
	}
	if truncated {
		m.logger.Debug("browser: read_text truncated", "session", sessionID, "cap", MaxReadTextBytes)
	}
	return out, nil
}

// Screenshot captures the viewport as PNG, capped at MaxScreenshotBytes.
func (m *Manager) Screenshot(ctx context.Context, sessionID string) ([]byte, error) {
	if m.Disabled() {
		return nil, ErrDisabled
	}
	s, ok := m.getSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoSession, sessionID)
	}
	var png []byte
	if err := chromedp.Run(s.ctx, chromedp.CaptureScreenshot(&png)); err != nil {
		return nil, fmt.Errorf("browser: screenshot: %w", err)
	}
	if len(png) > MaxScreenshotBytes {
		return nil, fmt.Errorf("browser: screenshot %d bytes exceeds %d byte cap", len(png), MaxScreenshotBytes)
	}
	return png, nil
}

// CloseSession shuts down the session's Chrome instance.
func (m *Manager) CloseSession(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoSession, sessionID)
	}
	delete(m.sessions, sessionID)
	s.cxl()
	s.allocCxl()
	return nil
}

// Close tears down every session. Call on daemon shutdown.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.cxl()
		s.allocCxl()
		delete(m.sessions, id)
	}
}
