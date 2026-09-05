package builtin

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDFExtractOptions controls a PDF text extraction run.
type PDFExtractOptions struct {
	// PageRange is a 1-based page selector: "" or omitted means all pages;
	// otherwise a comma-separated list of page numbers and inclusive ranges
	// (e.g. "3", "2-5", "1,3,7-9", "2-" for everything from page 2 on).
	PageRange string
	// MaxChars caps extracted text length in characters (bytes); 0 or
	// negative means unlimited. When the cap is hit, the text is cut at
	// the cap and "[TRUNCATED at cap]" is appended after a newline.
	MaxChars int64
}

// PDFExtractResult reports the outcome of one PDF text extraction.
type PDFExtractResult struct {
	// PagesRead is the number of pages actually read; TotalPages is the
	// document's page count.
	PagesRead, TotalPages int
	// Truncated reports whether the MaxChars cap cut the text short.
	Truncated bool
	// Text is the concatenated per-page plain text.
	Text string
	// Note carries an advisory message for special cases (e.g. scanned
	// PDFs with no extractable text); empty on a normal run.
	Note string
}

// PDF extraction seam (leaf 02-pdf-read/01, Contract A in the parent
// master plan): reads the plain text layer of the PDF stream in r without
// touching the filesystem or the network. A PDF with no text layer (a
// scan) parses fine but yields empty text; in that case extractPDFText
// returns (result, nil) with empty Text and a Note of exactly
// "no text layer detected (scanned PDF?) - N pages, no extractable text".
// Any parse or per-page read failure is wrapped as "pdf extract: ...".
func extractPDFText(r io.Reader, opts PDFExtractOptions) (PDFExtractResult, error) {
	var result PDFExtractResult

	// pdf.NewReader needs io.ReaderAt; io.Reader only guarantees forward
	// reads, so pull everything into memory first.
	data, err := io.ReadAll(r)
	if err != nil {
		return result, fmt.Errorf("pdf extract: reading input: %w", err)
	}

	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return result, fmt.Errorf("pdf extract: %w", err)
	}

	result.TotalPages = reader.NumPage()
	if result.TotalPages <= 0 {
		return result, fmt.Errorf("pdf extract: document reports %d pages", result.TotalPages)
	}

	pages, err := parsePageRange(opts.PageRange, result.TotalPages)
	if err != nil {
		return result, fmt.Errorf("pdf extract: %w", err)
	}

	var text strings.Builder
	for _, pageNum := range pages {
		page := reader.Page(pageNum)
		pageText, err := page.GetPlainText(nil)
		if err != nil {
			return result, fmt.Errorf("pdf extract: page %d: %w", pageNum, err)
		}
		text.WriteString(pageText)
		result.PagesRead++
	}

	result.Text = text.String()

	if opts.MaxChars > 0 && int64(len(result.Text)) > opts.MaxChars {
		result.Text = result.Text[:opts.MaxChars] + "\n[TRUNCATED at cap]"
		result.Truncated = true
	}

	if result.Text == "" {
		result.Note = fmt.Sprintf("no text layer detected (scanned PDF?) - %d pages, no extractable text", result.TotalPages)
	}

	return result, nil
}

// parsePageRange resolves a page-range spec against a 1-based total page
// count into the ordered list of pages to read. "" means all pages;
// otherwise the spec is a comma-separated list of single page numbers and
// inclusive "a-b" ranges, with "b-" allowed as open-ended. Out-of-range,
// zero, inverted, or non-numeric values are errors.
func parsePageRange(spec string, total int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		pages := make([]int, 0, total)
		for i := 1; i <= total; i++ {
			pages = append(pages, i)
		}
		return pages, nil
	}

	var pages []int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("invalid page range %q: empty element", spec)
		}

		var start, end int
		if before, after, found := strings.Cut(part, "-"); found {
			var err error
			start, err = strconv.Atoi(strings.TrimSpace(before))
			if err != nil {
				return nil, fmt.Errorf("invalid page range %q: %w", spec, err)
			}
			if after == "" {
				end = total
			} else {
				end, err = strconv.Atoi(strings.TrimSpace(after))
				if err != nil {
					return nil, fmt.Errorf("invalid page range %q: %w", spec, err)
				}
			}
		} else {
			var err error
			start, err = strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page range %q: %w", spec, err)
			}
			end = start
		}

		if start < 1 || start > total || end < start || end > total {
			return nil, fmt.Errorf("invalid page range %q: pages %d-%d out of bounds for %d-page document", spec, start, end, total)
		}

		for p := start; p <= end; p++ {
			pages = append(pages, p)
		}
	}
	return pages, nil
}
