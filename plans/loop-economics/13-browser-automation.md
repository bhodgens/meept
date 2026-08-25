# Browser Automation Tools (chromedp) - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Native Go browser tool family via chromedp: navigate/click/type/read/screenshot/close, SSRF-guarded, managed Chrome lifecycle.
- **Deps:** 05-ssrf-guards | **Context:** 80K | **Group:** E

## Goal

Close the biggest functional gap: no browser tooling exists. Provide a compact tool family over chromedp (headless Chrome DevTools protocol) with: URL checks through the ssrf.Guard before every navigation INCLUDING redirects (guard at CDP layer via intercepting navigation events where feasible; minimum: pre-nav check + post-nav location verify), text extraction with size caps, screenshots as evidence-attached images, per-session singleton Chrome process managed like other runtimes.

## Context

chromedp is the only NEW dependency this tree adds (go get github.com/chromedp/chromedp). Tool conventions from internal/tools/builtin (streaming interface if progress matters — follow webfetch patterns). SecurityEngine rules: add browser.* prefix table entries mirroring leaf-08-of-containment-tree style — navigate=medium, click/type=high, read/screenshot=low.

Key files: new internal/tools/builtin/browser*.go + internal/browser/manager.go (Chrome lifecycle); security engine rule addition; config [browser] {enabled=false default, chrome_path="", headless=true, max_pages=3}.

## Interface Contracts (From Parent)

```go
// internal/browser/manager.go:
type Manager struct{ /* session -> *chromedp.Context, singleton chrome per daemon */ }
func NewManager(cfg Config, guard *ssrf.Guard, logger *slog.Logger) (*Manager, error)
// Launches discovered chrome/chromium binary headless w/ --no-first-run;
// error if binary missing AND enabled=true.
func (m *Manager) Navigate(ctx context.Context, sessionID, rawURL string) (finalURL string, title string, err error)
// guard.CheckURL BEFORE; after nav, verify final location host still allowed
// (redirect escape hatch) else Close page + error.
func (m *Manager) Click(ctx, sessionID, selector string) error        // css selector only
func (m *Manager) Type(ctx, sessionID, selector, text string) error
func (m *Manager) ReadText(ctx, sessionID, selector string) (string, error) // cap 64KB
func (m *Manager) Screenshot(ctx, sessionID) (png []byte, err error)   // viewport, cap 5MB
func (m *Manager) CloseSession(ctx, sessionID) error
```

Tools thin wrappers registering under names browser_navigate etc.; Evidence: screenshot attaches image ref like existing vision-capable paths (locate pattern via search_files "image" in tools/builtin evidence usage; if none, return base64 data URL truncated note).

## Tasks
1. Failing tests manager against REAL headless chrome IF available (skip pattern like docker_test hasDocker): navigate example.com-data-url? use local httptest server serving fixture HTML for determinism; redirect to private IP blocked; ReadText returns fixture text; Screenshot non-empty PNG header.
2. Non-chrome-environment tests: guard denial before launch attempted; disabled config -> tools absent from registry.
3. Tool wrapper tests: arg validation, session scoping, error surfacing.
4. Engine rules + config plumbing + docs/workflows page (browser-automation.md new) incl. install notes + security notes (scheme gating, caps).
5. `make build` still green with new dep; go.mod tidy committed in leaf commit.

## Self-Verification Checklist
- [ ] Skips not fakes when chrome absent (CI-safe)
- [ ] No JS injection surface: selectors are CSS-only (validate reject "javascript:")
- [ ] -race green touched pkgs

## Review Checklist
- [ ] Post-nav host recheck proven by test (open-redirect to 169.254 blocked)
- [ ] Process cleanup on daemon shutdown hook
- [ ] Conventions per orchestrator; AGENTS.md wiring rule satisfied (tools registered = reachable)

Output: APPROVED or gaps. Notes: ARIA-snapshot budgeting (atomic-agent idea) deferred — read_text covers v1; note as future work in docs.
