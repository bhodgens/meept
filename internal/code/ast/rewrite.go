package ast

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// EditProposal represents a single proposed text edit.
type EditProposal struct {
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_column"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_column"`
	OldText   string `json:"old_text,omitempty"`
	NewText   string `json:"new_text"`
}

// RewriteProposal holds a collection of proposed edits for preview/resolve.
type RewriteProposal struct {
	ID          string         `json:"id"`
	FilePath    string         `json:"file_path"`
	Query       string         `json:"query"`
	MatchCount  int            `json:"match_count"`
	Edits       []EditProposal `json:"edits"`
	PreviewText []string       `json:"preview_text,omitempty"`
}

// RewriteEngine generates structural edits from tree-sitter queries.
type RewriteEngine struct {
	parser *ParserManager
}

// NewRewriteEngine creates a new rewrite engine.
func NewRewriteEngine(parser *ParserManager) *RewriteEngine {
	return &RewriteEngine{parser: parser}
}

// OperationType defines the type of rewrite operation.
type OperationType string

const (
	OpReplace     OperationType = "replace"
	OpRename      OperationType = "rename"
	OpInsertAfter OperationType = "insert_after"
)

// GenerateProposal runs a query and generates edit proposals without applying them.
func (r *RewriteEngine) GenerateProposal(ctx context.Context, filePath string, lang Language, queryPattern string, operation OperationType, template string) (*RewriteProposal, error) {
	if lang == LangUnknown {
		return nil, fmt.Errorf("unknown language")
	}

	grammar := GetLanguageGrammar(lang)
	if grammar == nil {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	tree, err := r.parser.GetTree(ctx, source, lang)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	query, err := sitter.NewQuery([]byte(queryPattern), grammar)
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	cursor := sitter.NewQueryCursor()
	cursor.Exec(query, tree.RootNode())

	proposal := &RewriteProposal{
		FilePath: filePath,
		Query:    queryPattern,
		Edits:    make([]EditProposal, 0),
	}

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		match = cursor.FilterPredicates(match, source)

		for _, capture := range match.Captures {
			node := capture.Node
			captureName := query.CaptureNameForId(capture.Index)

			edit, preview, err := r.buildEdit(node, source, operation, template, captureName)
			if err != nil {
				continue // Skip unprocessable matches
			}

			proposal.Edits = append(proposal.Edits, edit)
			if preview != "" {
				proposal.PreviewText = append(proposal.PreviewText, preview)
			}
		}
	}

	proposal.MatchCount = len(proposal.Edits)
	return proposal, nil
}

func (r *RewriteEngine) buildEdit(node *sitter.Node, source []byte, op OperationType, template, captureName string) (EditProposal, string, error) {
	startLine := int(node.StartPoint().Row)
	startCol := int(node.StartPoint().Column)
	endLine := int(node.EndPoint().Row)
	endCol := int(node.EndPoint().Column)
	oldText := node.Content(source)

	var newText string
	var preview string

	switch op {
	case OpReplace:
		// Replace captured node with template (substituting @name placeholders)
		newText = substituteTemplate(template, captureName, oldText)
		preview = fmt.Sprintf("replace: %q → %q", truncate(oldText, 40), truncate(newText, 40))

	case OpRename:
		// Replace the captured identifier text
		newText = template
		preview = fmt.Sprintf("rename: %q → %q", truncate(oldText, 40), truncate(newText, 40))

	case OpInsertAfter:
		// Insert template after the captured node
		newText = oldText + template
		preview = fmt.Sprintf("insert after %q: +%q", truncate(oldText, 40), truncate(template, 40))

	default:
		return EditProposal{}, "", fmt.Errorf("unknown operation: %s", op)
	}

	edit := EditProposal{
		FilePath:  "",
		StartLine: startLine,
		StartCol:  startCol,
		EndLine:   endLine,
		EndCol:    endCol,
		OldText:   oldText,
		NewText:   newText,
	}

	return edit, preview, nil
}

// substituteTemplate replaces @CAPNAME placeholders in the template with the captured text.
func substituteTemplate(template, captureName, capturedText string) string {
	placeholder := "@" + captureName
	return strings.ReplaceAll(template, placeholder, capturedText)
}

// ApplyEdits applies proposed edits to a file on disk.
// Edits are applied in reverse order (bottom-up) to preserve line positions.
func ApplyEdits(filePath string, edits []EditProposal) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")

	// Apply in reverse order to preserve positions
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		if err := applyEditToLines(&lines, edit); err != nil {
			return fmt.Errorf("edit at line %d: %w", edit.StartLine, err)
		}
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0o644)
}

// applyEditToLines applies a single edit to a line slice.
func applyEditToLines(lines *[]string, edit EditProposal) error {
	// Build content as single string with positions
	var content strings.Builder
	for i, line := range *lines {
		if i > 0 {
			content.WriteString("\n")
		}
		content.WriteString(line)
	}

	s := content.String()

	// Calculate byte offsets for start and end positions
	startOffset := lineColToOffset(s, edit.StartLine, edit.StartCol)
	endOffset := lineColToOffset(s, edit.EndLine, edit.EndCol)

	if startOffset < 0 || endOffset < 0 || startOffset > len(s) || endOffset > len(s) || startOffset > endOffset {
		return fmt.Errorf("invalid edit range: (%d,%d)-(%d,%d)", edit.StartLine, edit.StartCol, edit.EndLine, edit.EndCol)
	}

	newContent := s[:startOffset] + edit.NewText + s[endOffset:]
	*lines = strings.Split(newContent, "\n")
	return nil
}

// lineColToOffset converts a line:column position to a byte offset in the string.
func lineColToOffset(s string, line, col int) int {
	offset := 0
	currentLine := 0
	for i := 0; i < len(s); i++ {
		if currentLine == line {
			// We're on the target line, count columns
			if i-offset >= col {
				return offset + col
			}
		}
		if s[i] == '\n' {
			currentLine++
			if currentLine == line {
				offset = i + 1
			}
		}
	}
	// Return end of string if past last line
	if currentLine == line {
		return offset + col
	}
	return len(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
