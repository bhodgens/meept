// Package builtin: browser automation tools over internal/browser.Manager.
//
// The family is browser_navigate, browser_click, browser_type,
// browser_read_text, browser_screenshot, and browser_close. All are thin
// wrappers around browser.Manager; the manager enforces SSRF guarding, size
// caps, and per-session Chrome lifecycle. Tools are only registered when the
// [browser] config is enabled.
package builtin

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/caimlas/meept/internal/browser"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// BrowserTool is the shared base for the browser.* tool family. Session
// scoping comes from the context via ContextWithSessionID (falling back to a
// shared default session).
type BrowserTool struct {
	tools.ToolDefaults
	mgr *browser.Manager
}

func newBrowserTool(mgr *browser.Manager) BrowserTool {
	return BrowserTool{mgr: mgr}
}

// sessionIDFromCtx extracts the agent session ID for browser session scoping.
func sessionIDFromCtx(ctx context.Context) string {
	if sid, ok := ctx.Value(sessionIDContextKey).(string); ok && sid != "" {
		return sid
	}
	return "default"
}

// argString reads a required string argument.
func argString(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}

// BrowserNavigateTool navigates the session's browser tab to a URL.
type BrowserNavigateTool struct{ BrowserTool }

// NewBrowserNavigateTool builds the browser_navigate tool.
func NewBrowserNavigateTool(mgr *browser.Manager) *BrowserNavigateTool {
	return &BrowserNavigateTool{newBrowserTool(mgr)}
}

func (t *BrowserNavigateTool) Name() string { return "browser_navigate" }
func (t *BrowserNavigateTool) Category() string {
	return "web"
}
func (t *BrowserNavigateTool) Description() string {
	return "Navigate the headless browser to a URL (http/https only; SSRF-guarded including redirects). Returns the final URL and page title."
}

func (t *BrowserNavigateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropURL: {Type: schemaTypeString, Description: "The URL to navigate to."},
		},
		Required: []string{"url"},
	}
}

func (t *BrowserNavigateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawURL, err := argString(args, "url")
	if err != nil {
		return nil, err
	}
	finalURL, title, err := t.mgr.Navigate(ctx, sessionIDFromCtx(ctx), rawURL)
	if err != nil {
		return nil, err
	}
	return tools.NewSuccessResult(map[string]any{
		"final_url": finalURL,
		"title":     title,
	}), nil
}

var _ tools.Tool = (*BrowserNavigateTool)(nil)

// BrowserClickTool clicks an element by CSS selector.
type BrowserClickTool struct{ BrowserTool }

// NewBrowserClickTool builds the browser_click tool.
func NewBrowserClickTool(mgr *browser.Manager) *BrowserClickTool {
	return &BrowserClickTool{newBrowserTool(mgr)}
}

func (t *BrowserClickTool) Name() string { return "browser_click" }
func (t *BrowserClickTool) Category() string {
	return "web"
}
func (t *BrowserClickTool) Description() string {
	return "Click an element in the headless browser by CSS selector (CSS selectors only)."
}

func (t *BrowserClickTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropSelector: {Type: schemaTypeString, Description: "CSS selector of the element to click."},
		},
		Required: []string{"selector"},
	}
}

func (t *BrowserClickTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sel, err := argString(args, "selector")
	if err != nil {
		return nil, err
	}
	if err := t.mgr.Click(ctx, sessionIDFromCtx(ctx), sel); err != nil {
		return nil, err
	}
	return tools.NewSuccessResult(map[string]any{"clicked": sel}), nil
}

var _ tools.Tool = (*BrowserClickTool)(nil)

// BrowserTypeTool types text into an element by CSS selector.
type BrowserTypeTool struct{ BrowserTool }

// NewBrowserTypeTool builds the browser_type tool.
func NewBrowserTypeTool(mgr *browser.Manager) *BrowserTypeTool {
	return &BrowserTypeTool{newBrowserTool(mgr)}
}

func (t *BrowserTypeTool) Name() string { return "browser_type" }
func (t *BrowserTypeTool) Category() string {
	return "web"
}
func (t *BrowserTypeTool) Description() string {
	return "Type text into a form field in the headless browser, selected by CSS selector."
}

func (t *BrowserTypeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropSelector: {Type: schemaTypeString, Description: "CSS selector of the input element."},
			schemaPropText:     {Type: schemaTypeString, Description: "The text to type."},
		},
		Required: []string{"selector", "text"},
	}
}

func (t *BrowserTypeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sel, err := argString(args, "selector")
	if err != nil {
		return nil, err
	}
	text, err := argString(args, "text")
	if err != nil {
		return nil, err
	}
	if err := t.mgr.Type(ctx, sessionIDFromCtx(ctx), sel, text); err != nil {
		return nil, err
	}
	return tools.NewSuccessResult(map[string]any{"typed_into": sel, "length": len(text)}), nil
}

var _ tools.Tool = (*BrowserTypeTool)(nil)

// BrowserReadTextTool extracts visible text from the current page.
type BrowserReadTextTool struct{ BrowserTool }

// NewBrowserReadTextTool builds the browser_read_text tool.
func NewBrowserReadTextTool(mgr *browser.Manager) *BrowserReadTextTool {
	return &BrowserReadTextTool{newBrowserTool(mgr)}
}

func (t *BrowserReadTextTool) Name() string { return "browser_read_text" }
func (t *BrowserReadTextTool) Category() string {
	return "web"
}
func (t *BrowserReadTextTool) IsReadOnly(map[string]any) bool { return true }

func (t *BrowserReadTextTool) Description() string {
	return "Read visible text from the headless browser's current page (whole body or a CSS selector). Capped at 64 KB."
}

func (t *BrowserReadTextTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropSelector: {Type: schemaTypeString, Description: "Optional CSS selector; defaults to the whole body."},
		},
	}
}

func (t *BrowserReadTextTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sel, _ := args["selector"].(string)
	text, err := t.mgr.ReadText(ctx, sessionIDFromCtx(ctx), sel)
	if err != nil {
		return nil, err
	}
	return tools.NewSuccessResult(map[string]any{"text": text, "bytes": len(text)}), nil
}

var _ tools.Tool = (*BrowserReadTextTool)(nil)

// BrowserScreenshotTool captures the viewport as PNG evidence.
type BrowserScreenshotTool struct{ BrowserTool }

// NewBrowserScreenshotTool builds the browser_screenshot tool.
func NewBrowserScreenshotTool(mgr *browser.Manager) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{newBrowserTool(mgr)}
}

func (t *BrowserScreenshotTool) Name() string { return "browser_screenshot" }
func (t *BrowserScreenshotTool) Category() string {
	return "web"
}
func (t *BrowserScreenshotTool) IsReadOnly(map[string]any) bool { return true }

func (t *BrowserScreenshotTool) Description() string {
	return "Capture a PNG screenshot of the headless browser's viewport. Returned as base64 data-URL evidence (capped at 5 MB)."
}

func (t *BrowserScreenshotTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: schemaTypeObject, Properties: map[string]llm.ParameterProperty{}}
}

func (t *BrowserScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	png, err := t.mgr.Screenshot(ctx, sessionIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	res := tools.NewSuccessResult(map[string]any{
		"image":      dataURL,
		"bytes":      len(png),
		"format":     "png",
		"session_id": sessionIDFromCtx(ctx),
	})
	res.Evidence = []models.Evidence{
		models.NewEvidence(models.EvidenceAPIResponse, "screenshot", fmt.Sprintf("png %d bytes", len(png)), t.Name()),
	}
	return res, nil
}

var _ tools.Tool = (*BrowserScreenshotTool)(nil)

// BrowserCloseTool shuts down the session's browser instance.
type BrowserCloseTool struct{ BrowserTool }

// NewBrowserCloseTool builds the browser_close tool.
func NewBrowserCloseTool(mgr *browser.Manager) *BrowserCloseTool {
	return &BrowserCloseTool{newBrowserTool(mgr)}
}

func (t *BrowserCloseTool) Name() string { return "browser_close" }
func (t *BrowserCloseTool) Category() string {
	return "web"
}
func (t *BrowserCloseTool) Description() string {
	return "Close this session's headless browser instance, freeing its resources."
}

func (t *BrowserCloseTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: schemaTypeObject, Properties: map[string]llm.ParameterProperty{}}
}

func (t *BrowserCloseTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if err := t.mgr.CloseSession(ctx, sessionIDFromCtx(ctx)); err != nil {
		return nil, err
	}
	return tools.NewSuccessResult(map[string]any{"closed": true}), nil
}

var _ tools.Tool = (*BrowserCloseTool)(nil)

// NewBrowserTools returns the full browser tool family, or nil when the
// manager is disabled (callers then skip registration entirely).
func NewBrowserTools(mgr *browser.Manager) []tools.Tool {
	if mgr == nil || mgr.Disabled() {
		return nil
	}
	return []tools.Tool{
		NewBrowserNavigateTool(mgr),
		NewBrowserClickTool(mgr),
		NewBrowserTypeTool(mgr),
		NewBrowserReadTextTool(mgr),
		NewBrowserScreenshotTool(mgr),
		NewBrowserCloseTool(mgr),
	}
}
