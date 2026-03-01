package ast

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/sql"
	"github.com/smacker/go-tree-sitter/toml"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/yaml"
)

// LanguageInfo holds information about a supported language.
type LanguageInfo struct {
	ID         Language
	Name       string
	Extensions []string
	GetLang    func() *sitter.Language
}

// registry maps language IDs to their info.
var registry = map[Language]*LanguageInfo{}

// extensionMap maps file extensions to languages.
var extensionMap = map[string]Language{}

func init() {
	// Register all supported languages
	registerLanguage(LanguageInfo{
		ID:         LangGo,
		Name:       "Go",
		Extensions: []string{".go"},
		GetLang:    golang.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangPython,
		Name:       "Python",
		Extensions: []string{".py", ".pyi", ".pyw"},
		GetLang:    python.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangTypeScript,
		Name:       "TypeScript",
		Extensions: []string{".ts", ".tsx"},
		GetLang:    typescript.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangJavaScript,
		Name:       "JavaScript",
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		GetLang:    javascript.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangRust,
		Name:       "Rust",
		Extensions: []string{".rs"},
		GetLang:    rust.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangC,
		Name:       "C",
		Extensions: []string{".c", ".h"},
		GetLang:    c.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangCpp,
		Name:       "C++",
		Extensions: []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"},
		GetLang:    cpp.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangJava,
		Name:       "Java",
		Extensions: []string{".java"},
		GetLang:    java.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangRuby,
		Name:       "Ruby",
		Extensions: []string{".rb", ".rake", ".gemspec"},
		GetLang:    ruby.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangYAML,
		Name:       "YAML",
		Extensions: []string{".yaml", ".yml"},
		GetLang:    yaml.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangTOML,
		Name:       "TOML",
		Extensions: []string{".toml"},
		GetLang:    toml.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangBash,
		Name:       "Bash",
		Extensions: []string{".sh", ".bash", ".zsh"},
		GetLang:    bash.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangHTML,
		Name:       "HTML",
		Extensions: []string{".html", ".htm"},
		GetLang:    html.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangCSS,
		Name:       "CSS",
		Extensions: []string{".css"},
		GetLang:    css.GetLanguage,
	})

	registerLanguage(LanguageInfo{
		ID:         LangSQL,
		Name:       "SQL",
		Extensions: []string{".sql"},
		GetLang:    sql.GetLanguage,
	})
}

func registerLanguage(info LanguageInfo) {
	registry[info.ID] = &info
	for _, ext := range info.Extensions {
		extensionMap[ext] = info.ID
	}
}

// DetectLanguage determines the language from a file path.
func DetectLanguage(filePath string) Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	if lang, ok := extensionMap[ext]; ok {
		return lang
	}

	// Check for special filenames
	base := strings.ToLower(filepath.Base(filePath))
	switch base {
	case "makefile", "gnumakefile":
		return LangBash // Close enough for basic parsing
	case "dockerfile":
		return LangBash
	case "gemfile", "rakefile":
		return LangRuby
	}

	return LangUnknown
}

// GetLanguageGrammar returns the tree-sitter Language for parsing.
func GetLanguageGrammar(lang Language) *sitter.Language {
	if info, ok := registry[lang]; ok {
		return info.GetLang()
	}
	return nil
}

// GetLanguageInfo returns information about a language.
func GetLanguageInfo(lang Language) *LanguageInfo {
	return registry[lang]
}

// SupportedLanguages returns all supported language IDs.
func SupportedLanguages() []Language {
	langs := make([]Language, 0, len(registry))
	for id := range registry {
		langs = append(langs, id)
	}
	return langs
}

// IsSupported checks if a language is supported.
func IsSupported(lang Language) bool {
	_, ok := registry[lang]
	return ok
}

// LanguageFromString converts a string to a Language.
func LanguageFromString(s string) Language {
	lower := strings.ToLower(s)
	switch lower {
	case "go", "golang":
		return LangGo
	case "python", "py":
		return LangPython
	case "typescript", "ts":
		return LangTypeScript
	case "javascript", "js":
		return LangJavaScript
	case "rust", "rs":
		return LangRust
	case "c":
		return LangC
	case "cpp", "c++", "cxx":
		return LangCpp
	case "java":
		return LangJava
	case "ruby", "rb":
		return LangRuby
	case "yaml", "yml":
		return LangYAML
	case "toml":
		return LangTOML
	case "bash", "sh", "shell":
		return LangBash
	case "html":
		return LangHTML
	case "css":
		return LangCSS
	case "sql":
		return LangSQL
	default:
		return LangUnknown
	}
}
