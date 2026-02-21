package render

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// SyntaxHighlighter provides syntax highlighting for code blocks.
type SyntaxHighlighter struct {
	style     *chroma.Style
	formatter chroma.Formatter
}

// NewSyntaxHighlighter creates a new syntax highlighter.
func NewSyntaxHighlighter() *SyntaxHighlighter {
	return &SyntaxHighlighter{
		style:     styles.Get("monokai"),
		formatter: formatters.Get("terminal256"),
	}
}

// NewSyntaxHighlighterWithStyle creates a highlighter with a specific style.
func NewSyntaxHighlighterWithStyle(styleName string) *SyntaxHighlighter {
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Get("monokai")
	}

	return &SyntaxHighlighter{
		style:     style,
		formatter: formatters.Get("terminal256"),
	}
}

// Highlight applies syntax highlighting to code.
func (s *SyntaxHighlighter) Highlight(code, language string) (string, error) {
	lexer := s.getLexer(code, language)

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code, err
	}

	var buf bytes.Buffer
	err = s.formatter.Format(&buf, s.style, iterator)
	if err != nil {
		return code, err
	}

	return buf.String(), nil
}

// getLexer finds the appropriate lexer for the code.
func (s *SyntaxHighlighter) getLexer(code, language string) chroma.Lexer {
	// Try explicit language first
	if language != "" {
		lexer := lexers.Get(language)
		if lexer != nil {
			return chroma.Coalesce(lexer)
		}
	}

	// Try to analyse the content
	lexer := lexers.Analyse(code)
	if lexer != nil {
		return chroma.Coalesce(lexer)
	}

	// Fallback
	return chroma.Coalesce(lexers.Fallback)
}

// DetectLanguage attempts to detect the programming language from code.
func DetectLanguage(code string) string {
	lexer := lexers.Analyse(code)
	if lexer != nil {
		config := lexer.Config()
		if config != nil {
			return strings.ToLower(config.Name)
		}
	}
	return "text"
}

// ExtractCodeBlocks extracts fenced code blocks from markdown content.
func ExtractCodeBlocks(content string) []CodeBlock {
	var blocks []CodeBlock
	lines := strings.Split(content, "\n")

	var inBlock bool
	var currentBlock CodeBlock
	var codeLines []string

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inBlock {
				// End of code block
				currentBlock.Code = strings.Join(codeLines, "\n")
				blocks = append(blocks, currentBlock)
				inBlock = false
				codeLines = nil
			} else {
				// Start of code block
				inBlock = true
				currentBlock = CodeBlock{
					Language: strings.TrimPrefix(line, "```"),
				}
				currentBlock.Language = strings.TrimSpace(currentBlock.Language)
			}
		} else if inBlock {
			codeLines = append(codeLines, line)
		}
	}

	// Handle unclosed block
	if inBlock && len(codeLines) > 0 {
		currentBlock.Code = strings.Join(codeLines, "\n")
		blocks = append(blocks, currentBlock)
	}

	return blocks
}

// CodeBlock represents a fenced code block.
type CodeBlock struct {
	Language string
	Code     string
}

// SupportedLanguages returns a list of common supported language names.
func SupportedLanguages() []string {
	return []string{
		"go", "golang",
		"python", "py",
		"javascript", "js",
		"typescript", "ts",
		"rust", "rs",
		"c", "cpp", "c++",
		"java",
		"ruby", "rb",
		"php",
		"swift",
		"kotlin",
		"scala",
		"bash", "sh", "shell", "zsh",
		"sql",
		"json",
		"yaml", "yml",
		"toml",
		"xml",
		"html",
		"css",
		"markdown", "md",
		"dockerfile",
		"makefile",
		"diff",
		"git",
	}
}
