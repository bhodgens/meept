package lint

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// TreeSitterLinter provides syntax validation using tree-sitter error detection.
type TreeSitterLinter struct{}

// NewTreeSitterLinter creates a new tree-sitter linter.
func NewTreeSitterLinter() *TreeSitterLinter {
	return &TreeSitterLinter{}
}

// Lint performs tree-sitter based linting on the given file content.
// It detects syntax errors by parsing the content and checking for ERROR nodes.
func (ts *TreeSitterLinter) Lint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	lang := detectLanguageFromPath(filePath)
	if lang == "" {
		return nil, nil // Skip unknown languages
	}

	tsLang := GetTreeSitterLanguage(lang)
	if tsLang == nil {
		return nil, nil // Skip if language not supported
	}

	parser := sitter.NewParser()
	parser.SetLanguage(tsLang)

	tree, err := parser.ParseCtx(ctx, nil, []byte(content))
	if err != nil {
		return []LinterResult{{
			File:     filePath,
			Line:     0,
			Message:  fmt.Sprintf("Parse error: %v", err),
			Severity: "error",
		}}, nil
	}
	defer tree.Close()

	// Check for ERROR nodes in the parse tree
	var results []LinterResult
	root := tree.RootNode()
	traverseTreeForErrors(root, &results, filePath, content)

	return results, nil
}

// detectLanguageFromPath determines the language from file extension.
// Returns the language string used by the lint package.
func detectLanguageFromPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi", ".pyw":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".rb", ".rake":
		return "ruby"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	default:
		return ""
	}
}

// traverseTreeForErrors recursively walks the parse tree to find ERROR nodes.
// It handles both ERROR nodes (syntax errors) and missing nodes.
func traverseTreeForErrors(node *sitter.Node, results *[]LinterResult, file, content string) {
	if node == nil {
		return
	}

	// Check for ERROR nodes (syntax errors)
	if node.IsError() || node.IsMissing() {
		startPos := node.StartPoint()
		endPos := node.EndPoint()

		// Get more context about the error
		var message string
		if node.IsMissing() {
			message = fmt.Sprintf("Missing %s at line %d, column %d", node.Type(), startPos.Row+1, startPos.Column+1)
		} else {
			// Get the text content around the error for better context
			startByte := int(node.StartByte())
			endByte := int(node.EndByte())
			contentLen := len(content)
			if startByte < contentLen && endByte <= contentLen {
				errorText := string(content[startByte:endByte])
				if len(errorText) > 50 {
					errorText = errorText[:50] + "..."
				}
				message = fmt.Sprintf("Syntax error at line %d, column %d: %s", startPos.Row+1, startPos.Column+1, errorText)
			} else {
				message = fmt.Sprintf("Syntax error at line %d, column %d", startPos.Row+1, startPos.Column+1)
			}
		}

		*results = append(*results, LinterResult{
			File:      file,
			Line:      int(startPos.Row), // 0-based
			Column:    int(startPos.Column),
			EndLine:   int(endPos.Row),
			EndColumn: int(endPos.Column),
			Message:   message,
			Severity:  "error",
			Rule:      "tree-sitter-syntax",
		})
	}

	// Recurse into children
	childCount := int(node.ChildCount())
	for i := range childCount {
		child := node.Child(i)
		if child != nil {
			traverseTreeForErrors(child, results, file, content)
		}
	}
}

// GetTreeSitterLanguage returns the tree-sitter language for the given language name.
// This is a package-level function for use by the Registry.
func GetTreeSitterLanguage(lang string) *sitter.Language {
	getter, ok := LanguageFromLang[lang]
	if !ok {
		return nil
	}
	return getter()
}
