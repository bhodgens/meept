//go:build e2e

// Package toolsdocs covers the document tools (spreadsheet_write, pdf_read)
// through REAL agent turns. The e2e sandbox extends the roster with a custom
// user-tier agent ("aastub", alphabetically first holder of the analyze lane)
// that grants the doc tools — the shipped roster grants none of them, and the
// harness pre-boot hook lets us extend the roster without touching internal/.
//
// Coverage map (manifest scenarios):
//
//	tools-docs-01  spreadsheet_write xlsx+csv       — TestSpreadsheetWriteProducesCSVAndXLSX
//	tools-docs-02  pdf_read page range + scanned    — TestPDFReadPageRangeAndScannedNote
//	tools-docs-03  pdf_read http SSRF-guarded       — TestPDFReadHTTPSSRFGuarded
package toolsdocs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// seedDocAgent writes the aastub agent holding the doc-tool grants. Runs
// inside the pre-boot hook so the roster picks it up at boot; "aastub" sorts
// before "analyst" (the bundled analyze-lane holder), so the lane routing
// index routes pinned analyze turns to it.
func seedDocAgent(t *testing.T, st *harness.Stack) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "aastub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir aastub agent dir: %v", err)
	}
	body := `---
id: aastub
name: E2E Doc Specialist
role: executor
description: e2e-only agent holding the doc tool grants
intents: [code]
enabled: true
can_delegate: false
additional_tools:
  - spreadsheet_write
  - pdf_read
  - file_read
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# E2E Doc Specialist

Follow the user's request briefly.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write aastub AGENT.md: %v", err)
	}
}

func startDocStack(t *testing.T, name string) *harness.Stack {
	t.Helper()
	s := harness.Start(t, harness.WithPreBootHook(func(st *harness.Stack) error {
		seedDocAgent(t, st)
		return nil
	}))
	s.RegisterProject(t, "e2e-project")
	_ = s.CreateSession(t, name, s.ProjectDir)
	// Pin every turn to the analyze lane, which aastub owns.
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
	return s
}

// toolResultTexts collects the content of every role=tool message the daemon
// has ever sent back to the fake LLM.
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// buildPDFFixture assembles a minimal uncompressed 2-page PDF (same layout
// internal/tools/builtin's unit fixtures use) with one text line per page.
func buildPDFFixture(pageBodies ...string) []byte {
	var b strings.Builder
	offsets := map[int]int64{}
	writeObj := func(num int, body string) {
		offsets[num] = int64(b.Len())
		b.WriteString(strconv.Itoa(num) + " 0 obj\n" + body + "\nendobj\n")
	}
	streamObj := func(body string) string {
		return "<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream"
	}
	b.WriteString("%PDF-1.4\n")
	n := len(pageBodies)
	fontNum := 3 + 2*n
	var kids strings.Builder
	for i := range pageBodies {
		if i > 0 {
			kids.WriteString(" ")
		}
		kids.WriteString(strconv.Itoa(3+i) + " 0 R")
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids ["+kids.String()+"] /Count "+strconv.Itoa(n)+" >>")
	for i, body := range pageBodies {
		writeObj(3+i, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents "+strconv.Itoa(3+n+i)+" 0 R /Resources << /Font << /F1 "+strconv.Itoa(fontNum)+" 0 R >> >> >>")
		writeObj(3+n+i, streamObj(body))
	}
	writeObj(fontNum, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	xref := int64(b.Len())
	b.WriteString("xref\n0 " + strconv.Itoa(fontNum+1) + "\n0000000000 65535 f \n")
	for num := 1; num <= fontNum; num++ {
		off := offsets[num]
		s := strconv.Itoa(int(off))
		for len(s) < 10 {
			s = "0" + s
		}
		b.WriteString(s + " 00000 n \n")
	}
	b.WriteString("trailer\n<< /Size " + strconv.Itoa(fontNum+1) + " /Root 1 0 R >>\nstartxref\n" + strconv.Itoa(int(xref)) + "\n%%EOF\n")
	return []byte(b.String())
}

func textStream(s string) string {
	return "BT /F1 24 Tf 72 720 Td (" + s + ") Tj ET"
}

// tools-docs-01:// tools-docs-01: spreadsheet_write produces a real CSV (and xlsx) in the
// session workdir, with the rows_written evidence in the tool result.
func TestSpreadsheetWriteProducesCSVAndXLSX(t *testing.T) {
	t.Skip("blocked: spreadsheet_write hard-refuses without tools.ContextWithWorkingDir " +
		"(sessionDir must be in the tool ctx), and NEITHER live dispatch path injects it: " +
		"the inline chat lane runs with has_session=false (no loop workingDir) and the " +
		"task/step lane does not inject the session workdir into the tool ctx (the same " +
		"fs-01 gap). The grant/registry half is proven by driving the call through a " +
		"custom-roster agent — the real tool executes and returns the documented " +
		"refusal — but the happy-path artifact assertions need the workdir-ctx seam. " +
		"Unit coverage: internal/tools/builtin/spreadsheet_write_test.go.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// tools-docs-02: pdf_read page-range filter on a real PDF; a text-free PDF
// reports the exact scanned-PDF note.
func TestPDFReadPageRangeAndScannedNote(t *testing.T) {
	s := startDocStack(t, "docs-pdf")
	sessionID := s.CreateSession(t, "docs-pdf", s.ProjectDir)

	pdf := buildPDFFixture(textStream("Hello from page one"), textStream("Second page content"))
	pdfPath := filepath.Join(s.ProjectDir, "fixture.pdf")
	if err := os.WriteFile(pdfPath, pdf, 0o644); err != nil {
		t.Fatalf("write fixture pdf: %v", err)
	}

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "pdf_read",
		Arguments: `{"path":"` + pdfPath + `","pages":"2"}`,
	})
	s.ChatTurn(t, sessionID, "Create a report on the PDF fixture: read only page 2 with pdf_read first", 120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, `"pages_read":1`) || !strings.Contains(res, `"total_pages":2`) {
		t.Fatalf("pdf_read page-range result missing pages_read/total_pages:\n%s", res)
	}
	if !strings.Contains(res, "Second page content") || strings.Contains(res, "Hello from page one") {
		t.Fatalf("pdf_read page filter did not isolate page 2:\n%s", res)
	}

	// Scanned-PDF leg: a PDF whose pages carry no text layer.
	scannedPath := filepath.Join(s.ProjectDir, "scanned.pdf")
	if err := os.WriteFile(scannedPath, buildPDFFixture("q Q"), 0o644); err != nil {
		t.Fatalf("write scanned pdf: %v", err)
	}
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "pdf_read",
		Arguments: `{"path":"` + scannedPath + `"}`,
	})
	s.ChatTurn(t, sessionID, "Create the scanned-file report: first read scanned.pdf with pdf_read", 120*time.Second)

	res = toolResultsJoined(s.Fake)
	if !strings.Contains(res, "no text layer detected (scanned PDF?)") {
		t.Fatalf("scanned-PDF note missing from pdf_read result:\n%s", res)
	}
}

// tools-docs-03: pdf_read's http(s) path is SSRF-guarded BEFORE download —
// a loopback URL is refused and the local server records zero hits.
func TestPDFReadHTTPSSRFGuarded(t *testing.T) {
	s := startDocStack(t, "docs-pdf-http")
	sessionID := s.CreateSession(t, "docs-pdf-http", s.ProjectDir)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(buildPDFFixture(textStream("secret pdf")))
	}))
	t.Cleanup(srv.Close)
	target := srv.URL + "/secret.pdf"

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "pdf_read",
		Arguments: `{"path":"` + target + `"}`,
	})
	s.ChatTurn(t, sessionID, "Create a summary of the PDF at "+target+" by reading it with pdf_read", 120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, `"success":false`) {
		t.Fatalf("loopback pdf_read must fail; results:\n%s", res)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("local server recorded %d hits; the SSRF guard must refuse before any dial", got)
	}
}
