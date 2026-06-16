package ast

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// ASTRewrite represents a proposed AST-based text rewrite.
type ASTRewrite struct {
	FilePath      string
	Query         string
	MatchCount    int
	ProposedEdits []ProposedEdit
}

// ProposedEdit represents a single text edit proposal.
type ProposedEdit struct {
	StartLine int
	StartChar int
	EndLine   int
	EndChar   int
	// StartByte/EndByte, when non-zero, let ApplyEdits skip the O(n) line
	// scan in positionToByte and use the byte offsets directly (S2-12).
	StartByte int
	EndByte   int
	OldText   string
	NewText   string
	NodeKind  string
	Captures  map[string]string
}

// RewriteTemplate handles template-based rewrites with capture placeholders.
type RewriteTemplate struct {
	Template     string
	CaptureNames []string
}

// ParseRewriteTemplate parses a template string with {{capture}} placeholders.
func ParseRewriteTemplate(template string) (*RewriteTemplate, error) {
	re := regexp.MustCompile(`\{\{(\w+)\}\}`)
	matches := re.FindAllStringSubmatch(template, -1)

	captureNames := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			captureNames = append(captureNames, match[1])
		}
	}

	return &RewriteTemplate{
		Template:     template,
		CaptureNames: captureNames,
	}, nil
}

// Apply applies the template with actual capture values.
func (rt *RewriteTemplate) Apply(captures map[string]string) string {
	result := rt.Template
	for name, value := range captures {
		placeholder := "{{" + name + "}}"
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// ASTRewriter performs AST-based rewrites.
type ASTRewriter struct {
	parser *ParserManager
}

// NewASTRewriter creates a new AST rewriter.
func NewASTRewriter(parser *ParserManager) *ASTRewriter {
	return &ASTRewriter{parser: parser}
}

// RewriteResult contains the result of an AST rewrite operation.
type RewriteResult struct {
	Rewrite *ASTRewrite
	Source  []byte
}

// RunRewrite executes an AST rewrite on source code.
func (r *ASTRewriter) RunRewrite(source []byte, lang Language, queryPattern, rewriteTemplate string) (*RewriteResult, error) {
	if lang == LangUnknown {
		return nil, fmt.Errorf("unknown language")
	}

	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	template, err := ParseRewriteTemplate(rewriteTemplate)
	if err != nil {
		return nil, fmt.Errorf("invalid rewrite template: %w", err)
	}

	tree, err := r.parser.GetTree(nil, source, lang)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	defer tree.Close()

	query, err := sitter.NewQuery([]byte(queryPattern), grammar)
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}
	defer query.Close()

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(query, tree.RootNode())

	rewrite := &ASTRewrite{
		Query:         queryPattern,
		MatchCount:    0,
		ProposedEdits: make([]ProposedEdit, 0),
	}

	type byteRange struct {
		start uint32
		end   uint32
	}
	appliedRanges := make([]byteRange, 0)

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		match = cursor.FilterPredicates(match, source)

		captures := make(map[string]string)
		var targetNode *sitter.Node
		for _, capture := range match.Captures {
			captureName := query.CaptureNameForId(capture.Index)
			nodeText := string(source[capture.Node.StartByte():capture.Node.EndByte()])
			captures[captureName] = nodeText

			if targetNode == nil {
				targetNode = capture.Node
			}
		}

		if targetNode == nil {
			continue
		}

		startByte := targetNode.StartByte()
		endByte := targetNode.EndByte()
		overlaps := false
		for _, br := range appliedRanges {
			if startByte < br.end && endByte > br.start {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		appliedRanges = append(appliedRanges, byteRange{start: startByte, end: endByte})

		newText := template.Apply(captures)
		oldText := string(source[startByte:endByte])

		startLine := bytes.Count(source[:startByte], []byte("\n"))
		lines := bytes.Split(source[:startByte], []byte("\n"))
		startChar := 0
		if len(lines) > 0 {
			startChar = len(lines[len(lines)-1])
		}

		endLine := bytes.Count(source[:endByte], []byte("\n"))
		lines = bytes.Split(source[:endByte], []byte("\n"))
		endChar := 0
		if len(lines) > 0 {
			endChar = len(lines[len(lines)-1])
		}

		edit := ProposedEdit{
			StartLine: startLine,
			StartChar: startChar,
			EndLine:   endLine,
			EndChar:   endChar,
			StartByte: int(startByte),
			EndByte:   int(endByte),
			OldText:   oldText,
			NewText:   newText,
			NodeKind:  targetNode.Type(),
			Captures:  captures,
		}

		rewrite.ProposedEdits = append(rewrite.ProposedEdits, edit)
		rewrite.MatchCount++
	}

	sort.Slice(rewrite.ProposedEdits, func(i, j int) bool {
		if rewrite.ProposedEdits[i].StartLine != rewrite.ProposedEdits[j].StartLine {
			return rewrite.ProposedEdits[i].StartLine > rewrite.ProposedEdits[j].StartLine
		}
		return rewrite.ProposedEdits[i].StartChar > rewrite.ProposedEdits[j].StartChar
	})

	return &RewriteResult{
		Rewrite: rewrite,
		Source:  source,
	}, nil
}

// RunRewriteOnFile executes an AST rewrite on a file.
func (r *ASTRewriter) RunRewriteOnFile(filePath, queryPattern, rewriteTemplate string) (*RewriteResult, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return r.RunRewrite(source, lang, queryPattern, rewriteTemplate)
}

// ApplyEdits applies the proposed edits to source code and returns the modified content.
// Edits are applied in reverse document order so earlier offsets remain valid.
// When StartByte/EndByte are populated (non-zero), the expensive linear scan
// in positionToByte is skipped (S2-12). If positionToByte reports an
// out-of-range position, the edit is skipped with a warning rather than
// silently clamping to EOF (S2-20).
func ApplyEdits(source []byte, edits []ProposedEdit) []byte {
	sortedEdits := make([]ProposedEdit, len(edits))
	copy(sortedEdits, edits)
	sort.Slice(sortedEdits, func(i, j int) bool {
		if sortedEdits[i].StartLine != sortedEdits[j].StartLine {
			return sortedEdits[i].StartLine > sortedEdits[j].StartLine
		}
		return sortedEdits[i].StartChar > sortedEdits[j].StartChar
	})

	result := make([]byte, len(source))
	copy(result, source)

	for _, edit := range sortedEdits {
		var startByte, endByte int
		var err error

		if edit.StartByte > 0 || edit.EndByte > 0 {
			startByte = edit.StartByte
			endByte = edit.EndByte
		} else {
			startByte, err = positionToByte(result, edit.StartLine, edit.StartChar)
			if err != nil {
				slog.Warn("skipping edit: start position out of range",
					"line", edit.StartLine, "char", edit.StartChar, "error", err)
				continue
			}
			endByte, err = positionToByte(result, edit.EndLine, edit.EndChar)
			if err != nil {
				slog.Warn("skipping edit: end position out of range",
					"line", edit.EndLine, "char", edit.EndChar, "error", err)
				continue
			}
		}

		if startByte < 0 || endByte < 0 || startByte > len(result) || endByte > len(result) || startByte > endByte {
			slog.Warn("skipping edit: byte offsets out of range or inverted",
				"start_byte", startByte, "end_byte", endByte, "source_len", len(result))
			continue
		}

		newResult := make([]byte, 0, len(result)-(endByte-startByte)+len(edit.NewText))
		newResult = append(newResult, result[:startByte]...)
		newResult = append(newResult, edit.NewText...)
		newResult = append(newResult, result[endByte:]...)
		result = newResult
	}

	return result
}

// positionToByte converts a (line, char) pair to a byte offset in source.
// Returns an error if the position is out of range instead of silently
// clamping to len(source) (S2-20).
func positionToByte(source []byte, line, char int) (int, error) {
	currentLine := 0
	currentChar := 0
	for i, b := range source {
		if currentLine == line && currentChar == char {
			return i, nil
		}
		if b == '\n' {
			currentLine++
			currentChar = 0
		} else {
			currentChar++
		}
	}
	// Check the end-of-source case: (line, char) may point at the position
	// right after the last byte.
	if currentLine == line && currentChar == char {
		return len(source), nil
	}
	return -1, fmt.Errorf("position %d:%d out of range for source of %d bytes", line, char, len(source))
}
