//go:build e2e

// Package toolsdocs covers the document tools (spreadsheet_write, pdf_read).
//
// Coverage map (manifest scenarios):
//
//	tools-docs-01  spreadsheet_write xlsx+csv       — deferred (t.Skip)
//	tools-docs-02  pdf_read page range + scanned    — deferred (t.Skip)
//	tools-docs-03  pdf_read http SSRF-guarded       — deferred (t.Skip)
//
// All three scenarios require tools that NO roster agent grants, so the
// per-agent FilteredToolRegistry never exposes them to a real turn: a scripted
// call dies as "unknown tool: <name>" before the tool logic runs. The skips
// document the exact missing wiring so the scenarios light up as soon as a
// grant lands.
package toolsdocs

import "testing"

// tools-docs-01: spreadsheet_write produces a real xlsx + csv in the session
// workdir. DEFERRED: no roster grant.
func TestSpreadsheetWriteProducesCSVAndXLSX(t *testing.T) {
	t.Skip("deferred: spreadsheet_write is not granted to any roster agent " +
		"(config/agents/*/AGENT.md additional_tools); the filtered per-agent tool " +
		"registry answers a scripted call with `unknown tool: spreadsheet_write`. " +
		"Add a grant (e.g. to coder or doc-keeper) to enable this scenario — the " +
		"assertions are ready: file exists in the project dir, CSV bytes match the " +
		"scripted rows, xlsx magic bytes present, rows_written in the tool result.")
}

// tools-docs-02: pdf_read page-range filter on a real PDF; scanned-PDF note.
// DEFERRED: no roster grant.
func TestPDFReadPageRangeAndScannedNote(t *testing.T) {
	t.Skip("deferred: pdf_read is not granted to any roster agent; the filtered " +
		"registry answers a scripted call with `unknown tool: pdf_read`. With a " +
		"grant the scenario drives file_read-style scripting: seed a fixture PDF " +
		"(the uncompressed buildPDF layout), request pages \"2\" and assert " +
		"pages_read=2 total_pages=2 text in the tool result, then a no-text-layer " +
		"PDF and assert the exact scanned-PDF note.")
}

// tools-docs-03: pdf_read http(s) path is SSRF-guarded before download.
// DEFERRED: no roster grant (same root cause as tools-docs-02).
func TestPDFReadHTTPSSRFGuarded(t *testing.T) {
	t.Skip("deferred: pdf_read is not granted to any roster agent. With a grant " +
		"the scenario binds a local httptest server, counts hits, scripts " +
		"pdf_read{path: http://127.0.0.1:PORT/doc.pdf} and asserts the guard " +
		"error text plus zero server hits — the same shape as the tools-web-ssrf " +
		"loopback test.")
}
