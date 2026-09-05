package builtin

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// buildPDF assembles a minimal uncompressed single- or multi-page PDF from
// raw content-stream bodies (one page per entry) with a byte-accurate xref
// table, so github.com/ledongthuc/pdf can parse the fixture without any
// files on disk. Object layout: 1 Catalog, 2 Pages, 3..2+n Page objects,
// 3+n..2+2n content streams, 3+2n Font.
func buildPDF(contentStreams ...string) []byte {
	var buf bytes.Buffer
	offsetByNum := make(map[int]int64)

	writeObj := func(num int, body string) {
		offsetByNum[num] = int64(buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	streamObj := func(body string) string {
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body)
	}

	buf.WriteString("%PDF-1.4\n")
	n := len(contentStreams)
	fontNum := 3 + 2*n

	var kids strings.Builder
	for i := range contentStreams {
		if i > 0 {
			kids.WriteString(" ")
		}
		fmt.Fprintf(&kids, "%d 0 R", 3+i)
	}
	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), n))
	for i, body := range contentStreams {
		writeObj(3+i, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", 3+n+i, fontNum))
		writeObj(3+n+i, streamObj(body))
	}
	writeObj(fontNum, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefOffset := int64(buf.Len())
	fmt.Fprintf(&buf, "xref\n0 %d\n", fontNum+1)
	buf.WriteString("0000000000 65535 f \n")
	for num := 1; num <= fontNum; num++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsetByNum[num])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", fontNum+1, xrefOffset)
	return buf.Bytes()
}

const (
	testPageOneText = "Hello World from page one"
	testPageTwoText = "Second page has different content"
)

func testTextStream(s string) string {
	return fmt.Sprintf("BT /F1 24 Tf 72 720 Td (%s) Tj ET", s)
}

func TestExtractPDFTextSinglePage(t *testing.T) {
	data := buildPDF(testTextStream(testPageOneText))
	res, err := extractPDFText(bytes.NewReader(data), PDFExtractOptions{})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if res.TotalPages != 1 || res.PagesRead != 1 {
		t.Errorf("TotalPages=%d PagesRead=%d, want 1/1", res.TotalPages, res.PagesRead)
	}
	if res.Truncated {
		t.Errorf("Truncated=true, want false")
	}
	if !strings.Contains(res.Text, "Hello World") {
		t.Errorf("Text=%q, want it to contain %q", res.Text, "Hello World")
	}
}

func TestExtractPDFTextTwoPagesAllPages(t *testing.T) {
	data := buildPDF(testTextStream(testPageOneText), testTextStream(testPageTwoText))
	res, err := extractPDFText(bytes.NewReader(data), PDFExtractOptions{})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if res.TotalPages != 2 || res.PagesRead != 2 {
		t.Errorf("TotalPages=%d PagesRead=%d, want 2/2", res.TotalPages, res.PagesRead)
	}
	if !strings.Contains(res.Text, testPageOneText) || !strings.Contains(res.Text, testPageTwoText) {
		t.Errorf("Text=%q, want both page texts present", res.Text)
	}
}

func TestExtractPDFTextPageRange(t *testing.T) {
	data := buildPDF(testTextStream(testPageOneText), testTextStream(testPageTwoText))

	res, err := extractPDFText(bytes.NewReader(data), PDFExtractOptions{PageRange: "2-"})
	if err != nil {
		t.Fatalf("extractPDFText(2-): %v", err)
	}
	if res.PagesRead != 1 || res.TotalPages != 2 {
		t.Errorf("PagesRead=%d TotalPages=%d, want 1/2", res.PagesRead, res.TotalPages)
	}
	if !strings.Contains(res.Text, testPageTwoText) {
		t.Errorf("Text=%q, want it to contain %q", res.Text, testPageTwoText)
	}
	if strings.Contains(res.Text, testPageOneText) {
		t.Errorf("Text=%q, want it to exclude %q", res.Text, testPageOneText)
	}

	res, err = extractPDFText(bytes.NewReader(data), PDFExtractOptions{PageRange: "1,2"})
	if err != nil {
		t.Fatalf("extractPDFText(1,2): %v", err)
	}
	if res.PagesRead != 2 {
		t.Errorf("PagesRead=%d, want 2", res.PagesRead)
	}
	if !strings.Contains(res.Text, testPageOneText) || !strings.Contains(res.Text, testPageTwoText) {
		t.Errorf("Text=%q, want both page texts present", res.Text)
	}
}

func TestParsePageRange(t *testing.T) {
	all := func(n int) []int {
		pages := make([]int, 0, n)
		for i := 1; i <= n; i++ {
			pages = append(pages, i)
		}
		return pages
	}

	tests := []struct {
		name    string
		spec    string
		total   int
		want    []int
		wantErr bool
	}{
		{name: "empty means all pages", spec: "", total: 9, want: all(9)},
		{name: "single page", spec: "3", total: 9, want: []int{3}},
		{name: "closed range", spec: "2-5", total: 9, want: []int{2, 3, 4, 5}},
		{name: "mixed list and range", spec: "1,3,7-9", total: 9, want: []int{1, 3, 7, 8, 9}},
		{name: "open ended range", spec: "2-", total: 5, want: []int{2, 3, 4, 5}},
		{name: "zero page", spec: "0", total: 9, wantErr: true},
		{name: "inverted range", spec: "9-2", total: 9, wantErr: true},
		{name: "non numeric", spec: "abc", total: 9, wantErr: true},
		{name: "page beyond total", spec: "4", total: 3, wantErr: true},
		{name: "range beyond total", spec: "1-4", total: 3, wantErr: true},
		{name: "open range past total", spec: "2-", total: 1, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePageRange(tc.spec, tc.total)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parsePageRange(%q, %d) = %v, want error", tc.spec, tc.total, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePageRange(%q, %d): %v", tc.spec, tc.total, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parsePageRange(%q, %d) = %v, want %v", tc.spec, tc.total, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("parsePageRange(%q, %d) = %v, want %v", tc.spec, tc.total, got, tc.want)
				}
			}
		})
	}
}

func TestExtractPDFTextTruncation(t *testing.T) {
	data := buildPDF(testTextStream(testPageOneText))

	full, err := extractPDFText(bytes.NewReader(data), PDFExtractOptions{})
	if err != nil {
		t.Fatalf("extractPDFText uncapped: %v", err)
	}
	if len(full.Text) <= 10 {
		t.Fatalf("fixture text %q too short to exercise truncation", full.Text)
	}
	if full.Truncated {
		t.Errorf("uncapped run reports Truncated=true")
	}

	const cap = 10
	res, err := extractPDFText(bytes.NewReader(data), PDFExtractOptions{MaxChars: cap})
	if err != nil {
		t.Fatalf("extractPDFText capped: %v", err)
	}
	want := full.Text[:cap] + "\n[TRUNCATED at cap]"
	if res.Text != want {
		t.Errorf("Text=%q, want %q", res.Text, want)
	}
	if !res.Truncated {
		t.Errorf("Truncated=false, want true")
	}
	if res.PagesRead != 1 || res.TotalPages != 1 {
		t.Errorf("PagesRead=%d TotalPages=%d, want 1/1", res.PagesRead, res.TotalPages)
	}
}

func TestExtractPDFTextNoTextLayer(t *testing.T) {
	// Content stream with no text-showing operators: the PDF parses fine
	// but has nothing extractable (the scanned-PDF signature).
	data := buildPDF("q Q")
	res, err := extractPDFText(bytes.NewReader(data), PDFExtractOptions{})
	if err != nil {
		t.Fatalf("extractPDFText: %v", err)
	}
	if res.Text != "" {
		t.Errorf("Text=%q, want empty", res.Text)
	}
	if res.Truncated {
		t.Errorf("Truncated=true, want false")
	}
	wantNote := "no text layer detected (scanned PDF?) - 1 pages, no extractable text"
	if res.Note != wantNote {
		t.Errorf("Note=%q, want %q", res.Note, wantNote)
	}
}

func TestExtractPDFTextCorruptInput(t *testing.T) {
	junk := []byte("this is definitely not a pdf \x01\x02\xff no markers anywhere in here")
	res, err := extractPDFText(bytes.NewReader(junk), PDFExtractOptions{})
	if err == nil {
		t.Fatalf("extractPDFText(corrupt) = %+v, want error", res)
	}
	if res != (PDFExtractResult{}) {
		t.Errorf("result = %+v, want zero value on error", res)
	}
	if !strings.HasPrefix(err.Error(), "pdf extract: ") {
		t.Errorf("error %q, want it to be wrapped with %q", err.Error(), "pdf extract: ")
	}
}
