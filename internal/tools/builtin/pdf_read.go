package builtin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/caimlas/meept/internal/security/taint"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

const (
	// pdfFetchTimeout is the HTTP timeout for URL-based pdf_read fetches.
	pdfFetchTimeout = 30 * time.Second
	// pdfMaxConnsPerHost caps idle/active connections per host on the
	// fetch client, mirroring web_fetch.go.
	pdfMaxConnsPerHost = 8
	// pdfMagic is the %PDF- header every PDF must start with; used for
	// content sniffing of both local files and HTTP responses.
	pdfMagic = "%PDF-"
)

// PDFReadTool reads a PDF's text layer from a local path or URL
// (Contract A, plan 20260905-research-audit-tools). Extraction is
// delegated to the leaf-01 seam extractPDFText; this tool handles
// source resolution (disk vs guarded HTTP), content sniffing, and the
// Contract A result map.
type PDFReadTool struct {
	tools.ToolDefaults
	maxBytes int64
	client   *http.Client
	secOrch  *intsecurity.Orchestrator
	// allowPrivateRanges disables the SSRF private/loopback IP filter.
	// Production code never sets this; it exists for unit tests that run
	// against httptest.NewServer (which binds to 127.0.0.1).
	allowPrivateRanges bool
	// guard is the centralized SSRF guard ([security.ssrf] enabled,
	// default). When non-nil it supersedes the legacy checkURL /
	// ssrfDialContext path.
	guard *ssrf.Guard

	// clientMu guards client, guard, and allowPrivateRanges against a
	// race with concurrent Execute reads.
	clientMu sync.Mutex
}

// NewPDFReadTool creates a new pdf_read tool. maxBytes caps extracted
// text (0 = unlimited); guard is the centralized SSRF guard — a nil
// guard falls back to the legacy checkURL/ssrfDialContext path, the
// same contract as WebFetchTool. See Contract A in
// docs/plans/20260905-research-audit-tools/master.md.
func NewPDFReadTool(maxBytes int64, guard *ssrf.Guard) *PDFReadTool {
	t := &PDFReadTool{
		maxBytes: maxBytes,
		guard:    guard,
	}
	t.client = &http.Client{
		Timeout: pdfFetchTimeout,
		Transport: &http.Transport{
			MaxConnsPerHost: pdfMaxConnsPerHost,
			DialContext:     ssrfDialContext(false),
		},
		CheckRedirect: t.checkRedirect,
	}
	return t
}

func (t *PDFReadTool) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("too many redirects")
	}
	if !t.allowPrivateRanges {
		if err := t.checkURLGuarded(req.URL.String()); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
	}
	return nil
}

// checkURLGuarded validates raw against the centralized SSRF guard when
// one is installed, falling back to the legacy package-level checkURL.
func (t *PDFReadTool) checkURLGuarded(raw string) error {
	t.clientMu.Lock()
	g := t.guard
	t.clientMu.Unlock()
	if g != nil {
		return g.CheckURL(raw)
	}
	return checkURL(raw)
}

// SetSSRFGuard installs the centralized SSRF guard, rebuilding the
// client via Guard.WrapClient (per-hop redirect re-validation and
// dial-time IP re-checks), superseding the legacy path. Follows the
// typed-nil guard pattern: a nil g leaves legacy behavior in place.
func (t *PDFReadTool) SetSSRFGuard(g *ssrf.Guard) {
	if g == nil {
		return
	}
	t.clientMu.Lock()
	defer t.clientMu.Unlock()
	t.guard = g
	t.client = g.WrapClient(&http.Client{
		Timeout:   pdfFetchTimeout,
		Transport: &http.Transport{MaxConnsPerHost: pdfMaxConnsPerHost},
	})
}

// SetAllowPrivateRanges disables SSRF protection for private/loopback
// IPs. Intended only for unit tests against httptest.NewServer;
// production callers must never invoke it. Also removes any installed
// centralized SSRF guard (legacy disabled behavior).
func (t *PDFReadTool) SetAllowPrivateRanges(allow bool) {
	t.clientMu.Lock()
	defer t.clientMu.Unlock()
	t.allowPrivateRanges = allow
	if allow {
		t.guard = nil
	}
	t.client.Transport = &http.Transport{
		MaxConnsPerHost: pdfMaxConnsPerHost,
		DialContext:     ssrfDialContext(allow),
	}
}

// SetSecurityOrchestrator sets the security orchestrator for
// taint/exfil URL checks. Follows the typed-nil interface guard pattern
// mandated by CLAUDE.md.
func (t *PDFReadTool) SetSecurityOrchestrator(orch *intsecurity.Orchestrator) {
	if orch != nil {
		t.secOrch = orch
	}
}

func (t *PDFReadTool) Name() string { return "pdf_read" }

func (t *PDFReadTool) Category() string { return "web" }

func (t *PDFReadTool) Description() string {
	return "Read a PDF's text layer from a local path or URL. Returns extracted text; scanned PDFs report 'no text layer' instead of failing."
}

func (t *PDFReadTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        schemaTypeString,
				Description: "The PDF to read: a filesystem path or an http(s) URL. Validated by %PDF- content sniff, not extension.",
			},
			"pages": {
				Type:        schemaTypeString,
				Description: `Optional 1-based page selector: "3", "2-5", "1,3,7-9", or "2-" (open-ended). Empty means all pages.`,
			},
		},
		Required: []string{"path"},
	}
}

func (t *PDFReadTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("no path specified: pdf_read requires a local file path or http(s) URL")
	}

	pages, _ := args["pages"].(string)

	var (
		body []byte
		err  error
	)
	if isHTTPURL(path) {
		body, err = t.fetchPDF(ctx, path)
	} else {
		body, err = readLocalPDF(ctx, path)
	}
	if err != nil {
		return nil, err
	}

	// Content sniff: reject anything that is not a PDF by magic bytes,
	// regardless of file extension or Content-Type.
	if !bytes.HasPrefix(body, []byte(pdfMagic)) {
		return nil, fmt.Errorf("not a PDF: %s", path)
	}

	result, err := extractPDFText(bytes.NewReader(body), PDFExtractOptions{
		PageRange: pages,
		MaxChars:  t.maxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("pdf_read: %w", err)
	}

	res := map[string]any{
		"pages_read":  result.PagesRead,
		"total_pages": result.TotalPages,
		"truncated":   result.Truncated,
		"text":        result.Text,
		"note":        result.Note,
	}

	// Sanitize extracted text for injection patterns if the security
	// orchestrator is available (mirrors web_fetch output handling).
	if t.secOrch != nil && t.secOrch.InputSanitizer() != nil {
		sanitizeResult := t.secOrch.InputSanitizer().Sanitize(result.Text)
		if sanitizeResult.WasModified || len(sanitizeResult.ThreatsDetected) > 0 {
			slog.Debug("PDF text sanitized",
				"path", path,
				"threats", len(sanitizeResult.ThreatsDetected),
				"modified", sanitizeResult.WasModified)
		}
		res["text"] = sanitizeResult.CleanText
	}

	// Web-sourced PDF text is externally tainted, mirroring web_fetch.
	taintLabel := taint.TaintNone
	if isHTTPURL(path) {
		taintLabel = taint.TaintExternal
	}

	evidence := []models.Evidence{
		models.NewEvidence(
			models.EvidenceFileExists,
			path,
			fmt.Sprintf("pages_read=%d,total_pages=%d,truncated=%t,bytes=%d", result.PagesRead, result.TotalPages, result.Truncated, len(body)),
			t.Name(),
		),
	}

	return tools.ToolResult{
		Success:    true,
		Result:     res,
		Evidence:   evidence,
		TaintLabel: taintLabel,
	}, nil
}

// isHTTPURL reports whether raw is an http(s) URL.
func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

// fetchPDF downloads the PDF bytes from an http(s) URL through the
// guard-wrapped client (30s timeout, MaxConnsPerHost 8; legacy
// checkURL/ssrfDialContext path when no guard is installed).
func (t *PDFReadTool) fetchPDF(ctx context.Context, rawURL string) ([]byte, error) {
	// Taint/exfiltration policy check, mirroring web_fetch.
	if t.secOrch != nil {
		if blocked, reason := t.secOrch.CheckWebFetch(rawURL); blocked {
			return nil, fmt.Errorf("pdf_read fetch blocked by security policy: %s", reason)
		}
	}

	// SSRF guard: refuse private/loopback/link-local targets before
	// constructing the request. Uses the centralized guard when
	// installed, legacy checkURL otherwise.
	if !t.allowPrivateRanges {
		if err := t.checkURLGuarded(rawURL); err != nil {
			return nil, fmt.Errorf("pdf_read blocked: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Meept/0.2 (autonomous assistant)")
	req.Header.Set("Accept", "application/pdf,*/*")

	t.clientMu.Lock()
	client := t.client
	t.clientMu.Unlock()

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timed out after %v", pdfFetchTimeout)
		}
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return body, nil
}

// readLocalPDF reads a PDF from disk, resolving ~ and relative paths
// against the session working directory injected via context, never the
// process working directory.
func readLocalPDF(ctx context.Context, path string) ([]byte, error) {
	resolved, err := resolvePath(ctx, path)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("cannot read PDF %s: %w", path, err)
	}
	return body, nil
}

// IsReadOnly reports that PDF reads are always read-only.
func (t *PDFReadTool) IsReadOnly(map[string]any) bool { return true }

// IsConcurrencySafe reports that PDF reads are safe for concurrent execution.
func (t *PDFReadTool) IsConcurrencySafe(map[string]any) bool { return true }

// Ensure PDFReadTool implements the Tool interface.
var _ tools.Tool = (*PDFReadTool)(nil)
