package builtin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/security/ssrf"
	"github.com/caimlas/meept/internal/tools"
)

// pdfReadResultMap unwraps a pdf_read Execute result into the Contract A
// result map, accepting both the plain map and the tools.ToolResult envelope.
func pdfReadResultMap(res any) (map[string]any, bool) {
	switch v := res.(type) {
	case map[string]any:
		return v, true
	case tools.ToolResult:
		m, ok := v.Result.(map[string]any)
		return m, ok
	}
	return nil, false
}

// assertPDFReadFields checks the typed fields of the Contract A result map.
func assertPDFReadFields(t *testing.T, got map[string]any, wantRead, wantTotal int, wantTruncated bool) {
	t.Helper()
	if got["pages_read"] != wantRead {
		t.Errorf("pages_read = %v, want %d", got["pages_read"], wantRead)
	}
	if got["total_pages"] != wantTotal {
		t.Errorf("total_pages = %v, want %d", got["total_pages"], wantTotal)
	}
	if got["truncated"] != wantTruncated {
		t.Errorf("truncated = %v, want %v", got["truncated"], wantTruncated)
	}
	if _, ok := got["text"].(string); !ok {
		t.Errorf("text = %v (%T), want string", got["text"], got["text"])
	}
	if _, ok := got["note"].(string); !ok {
		t.Errorf("note = %v (%T), want string", got["note"], got["note"])
	}
}

func TestPDFReadTool(t *testing.T) {
	pdfBytes := buildPDF(testTextStream(testPageOneText), testTextStream(testPageTwoText))

	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "fixture.pdf")
	if err := os.WriteFile(fixturePath, pdfBytes, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	notPDFPath := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(notPDFPath, []byte("just some text, definitely not a pdf"), 0o600); err != nil {
		t.Fatalf("write non-PDF fixture: %v", err)
	}

	// httptest stub serving the same fixture bytes (binds to 127.0.0.1).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdfBytes)
	}))
	t.Cleanup(srv.Close)

	readAllPages := func(t *testing.T, got map[string]any) {
		t.Helper()
		assertPDFReadFields(t, got, 2, 2, false)
		if text, _ := got["text"].(string); !strings.Contains(text, testPageOneText) || !strings.Contains(text, testPageTwoText) {
			t.Errorf("text missing fixture page text: %q", text)
		}
		if note, _ := got["note"].(string); note != "" {
			t.Errorf("note = %q, want empty", note)
		}
	}

	cases := []struct {
		name         string
		maxBytes     int64
		args         map[string]any
		ctxDir       string // when non-empty, injected as the session working dir
		allowPrivate bool   // legacy loopback permit for the httptest stub
		wantErr      string // error substring; empty means success expected
		verify       func(t *testing.T, got map[string]any)
	}{
		{
			name:     "local file relative to session working dir",
			maxBytes: 100000,
			args:     map[string]any{"path": "fixture.pdf"},
			ctxDir:   dir,
			verify:   readAllPages,
		},
		{
			name:         "url via httptest",
			maxBytes:     100000,
			args:         map[string]any{"path": srv.URL + "/fixture.pdf"},
			allowPrivate: true, // legacy path must permit the 127.0.0.1 stub
			verify:       readAllPages,
		},
		{
			name:     "missing path param",
			maxBytes: 100000,
			args:     map[string]any{},
			wantErr:  "path",
		},
		{
			name:     "nonexistent file",
			maxBytes: 100000,
			args:     map[string]any{"path": filepath.Join(dir, "missing.pdf")},
			wantErr:  "missing.pdf",
		},
		{
			name:     "non-PDF bytes",
			maxBytes: 100000,
			args:     map[string]any{"path": notPDFPath},
			wantErr:  "not a PDF",
		},
		{
			name:     "pages param selects page 1",
			maxBytes: 100000,
			args:     map[string]any{"path": fixturePath, "pages": "1"},
			verify: func(t *testing.T, got map[string]any) {
				assertPDFReadFields(t, got, 1, 2, false)
				if text, _ := got["text"].(string); !strings.Contains(text, testPageOneText) || strings.Contains(text, testPageTwoText) {
					t.Errorf("text = %q, want only page-one content", text)
				}
			},
		},
		{
			name:     "truncated at small maxBytes",
			maxBytes: 10,
			args:     map[string]any{"path": fixturePath},
			verify: func(t *testing.T, got map[string]any) {
				assertPDFReadFields(t, got, 2, 2, true)
				if text, _ := got["text"].(string); !strings.Contains(text, "[TRUNCATED at cap]") {
					t.Errorf("text missing truncation marker: %q", text)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.ctxDir != "" {
				ctx = tools.ContextWithWorkingDir(ctx, tc.ctxDir)
			}
			tool := NewPDFReadTool(tc.maxBytes, nil)
			if tc.allowPrivate {
				tool.SetAllowPrivateRanges(true)
			}

			res, err := tool.Execute(ctx, tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Execute succeeded, want error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			got, ok := pdfReadResultMap(res)
			if !ok {
				t.Fatalf("result type %T, want Contract A result map", res)
			}
			tc.verify(t, got)
		})
	}
}

// TestPDFReadTool_SSRFGuardBlocksLoopbackStub proves the guard-supplied via
// the constructor actually gates URL fetches, mirroring the web_fetch wiring
// test in ssrf_guard_wiring_test.go.
func TestPDFReadTool_SSRFGuardBlocksLoopbackStub(t *testing.T) {
	pdfBytes := buildPDF(testTextStream(testPageOneText))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pdfBytes)
	}))
	t.Cleanup(srv.Close)

	g, err := ssrf.NewGuard(ssrf.GuardConfig{})
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	tool := NewPDFReadTool(100000, g)

	_, err = tool.Execute(context.Background(), map[string]any{"path": srv.URL})
	if err == nil {
		t.Fatal("pdf_read against 127.0.0.1 stub succeeded with SSRF guard enabled")
	}
	if !errors.Is(err, ssrf.ErrBlockedAddress) {
		t.Fatalf("error = %v, want ssrf.ErrBlockedAddress", err)
	}
}
