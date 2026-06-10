package lint

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/caimlas/meept/internal/lint/languages"
)

// LanguageFromLang maps our language strings to tree-sitter language getters
var LanguageFromLang = map[string]func() *sitter.Language{
	"go":         golang.GetLanguage,
	"python":     python.GetLanguage,
	"javascript": javascript.GetLanguage,
	"typescript": typescript.GetLanguage,
}

// LinterResult represents the output of a linting run
type LinterResult struct {
	File      string
	Line      int // 0-based line number
	Column    int // 0-based column (optional)
	EndLine   int // 0-based end line (optional)
	EndColumn int // 0-based end column (optional)
	Message   string
	Severity  string // "error" | "warning" | "info"
	Rule      string // Lint rule identifier
}

// HasErrors returns true if any error-severity issues found
func (r LinterResult) HasErrors() bool {
	return r.Severity == "error"
}

// LinterFunc defines the signature for language-specific linters
type LinterFunc func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error)

// Registry manages linters by language
type Registry struct {
	mu           sync.RWMutex
	linters      map[string][]LinterFunc // language → linters
	globalLinter LinterFunc              // catch-all
	knownLangs   map[string]bool
}

// NewRegistry creates a new linter registry
func NewRegistry() *Registry {
	r := &Registry{
		linters:    make(map[string][]LinterFunc),
		knownLangs: make(map[string]bool),
	}
	r.registerDefaults()
	return r
}

// Register adds a linter for a specific language
func (r *Registry) Register(lang string, fn LinterFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.linters[lang] = append(r.linters[lang], fn)
	r.knownLangs[lang] = true
}

// SetGlobalLinter sets a catch-all linter for unknown languages
func (r *Registry) SetGlobalLinter(fn LinterFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalLinter = fn
}

// Lint runs all registered linters for the given file
func (r *Registry) Lint(ctx context.Context, lang, filePath, relPath, content string) ([]LinterResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var allResults []LinterResult

	// Run language-specific linters
	linters := r.linters[lang]
	for _, fn := range linters {
		results, err := fn(ctx, filePath, relPath, content)
		if err != nil {
			return nil, fmt.Errorf("linter %s failed: %w", lang, err)
		}
		allResults = append(allResults, results...)
	}

	// Run global linter if no language-specific linters
	if len(linters) == 0 && r.globalLinter != nil {
		results, err := r.globalLinter(ctx, filePath, relPath, content)
		if err != nil {
			return nil, fmt.Errorf("global linter failed: %w", err)
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// registerDefaults sets up built-in linters
func (r *Registry) registerDefaults() {
	// Create language-specific linter instances
	goLinter := languages.NewGoLinter()
	pythonLinter := languages.NewPythonLinter()
	jsLinter := languages.NewJSLinter()

	// Go linter chain
	r.Register("go", r.goTreeSitterLint)
	r.Register("go", func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
		langResults, err := goLinter.CompileCheck(ctx, filePath, relPath, content)
		return convertLinterResults(langResults), err
	})
	r.Register("go", func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
		langResults, err := goLinter.Vet(ctx, filePath)
		return convertLinterResults(langResults), err
	})

	// Python linter chain
	r.Register("python", r.pythonTreeSitterLint)
	r.Register("python", func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
		langResults, err := pythonLinter.CompileCheck(ctx, filePath, relPath, content)
		return convertLinterResults(langResults), err
	})
	r.Register("python", func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
		langResults, err := pythonLinter.Flake8(ctx, filePath, relPath, content)
		return convertLinterResults(langResults), err
	})

	// JavaScript/TypeScript
	r.Register("javascript", r.jsTreeSitterLint)
	r.Register("javascript", func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
		langResults, err := jsLinter.CompileCheck(ctx, filePath, relPath, content)
		return convertLinterResults(langResults), err
	})
	r.Register("typescript", r.tsTreeSitterLint)
	r.Register("typescript", func(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
		langResults, err := jsLinter.CompileCheck(ctx, filePath, relPath, content)
		return convertLinterResults(langResults), err
	})

	// Global fallback: tree-sitter syntax check
	r.SetGlobalLinter(r.treeSitterBasicLint)
}

// convertLinterResults converts from languages.LinterResult to lint.LinterResult
func convertLinterResults(results []languages.LinterResult) []LinterResult {
	if results == nil {
		return nil
	}
	converted := make([]LinterResult, len(results))
	for i, r := range results {
		converted[i] = LinterResult{
			File:     r.File,
			Line:     r.Line,
			Column:   r.Column,
			Message:  r.Message,
			Severity: r.Severity,
			Rule:     r.Rule,
		}
	}
	return converted
}

// detectLanguage determines the language from file extension
func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescript"
	case ".jsx":
		return "javascript"
	default:
		return ""
	}
}

// getTreeSitterLanguage returns the tree-sitter language for the given language name
func getTreeSitterLanguage(lang string) *sitter.Language {
	getter, ok := LanguageFromLang[lang]
	if !ok {
		return nil
	}
	return getter()
}

// treeSitterBasicLint checks for syntax errors using tree-sitter
func (r *Registry) treeSitterBasicLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	lang := detectLanguage(filePath)
	if lang == "" {
		return nil, nil // Skip unknown languages
	}

	tsLang := getTreeSitterLanguage(lang)
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
	walkTreeForErrors(root, &results, filePath)

	return results, nil
}

// walkTreeForErrors recursively walks the parse tree to find ERROR nodes
func walkTreeForErrors(node *sitter.Node, results *[]LinterResult, file string) {
	if node == nil {
		return
	}

	// Check for ERROR nodes (syntax errors)
	if node.IsError() || node.IsMissing() {
		startPos := node.StartPoint()

		*results = append(*results, LinterResult{
			File:     file,
			Line:     int(startPos.Row), // 0-based
			Column:   int(startPos.Column),
			Message:  fmt.Sprintf("Syntax error: unexpected token at line %d, column %d", startPos.Row+1, startPos.Column+1),
			Severity: "error",
		})
	}

	// Recurse into children
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		if child != nil {
			walkTreeForErrors(child, results, file)
		}
	}
}

// goTreeSitterLint is Go-specific tree-sitter linting
func (r *Registry) goTreeSitterLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	return r.treeSitterBasicLint(ctx, filePath, relPath, content)
}

// pythonTreeSitterLint is Python-specific tree-sitter linting
func (r *Registry) pythonTreeSitterLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	return r.treeSitterBasicLint(ctx, filePath, relPath, content)
}

// jsTreeSitterLint is JavaScript-specific tree-sitter linting
func (r *Registry) jsTreeSitterLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	return r.treeSitterBasicLint(ctx, filePath, relPath, content)
}

// tsTreeSitterLint is TypeScript-specific tree-sitter linting
func (r *Registry) tsTreeSitterLint(ctx context.Context, filePath, relPath, content string) ([]LinterResult, error) {
	return r.treeSitterBasicLint(ctx, filePath, relPath, content)
}
