// Package render provides markdown and syntax highlighting for the TUI.
package render

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
)

// MarkdownRenderer renders markdown content for terminal display.
type MarkdownRenderer struct {
	renderer *glamour.TermRenderer
	width    int
	darkMode bool
	mu       sync.RWMutex
}

// NewMarkdownRenderer creates a new markdown renderer.
func NewMarkdownRenderer(width int, darkMode bool) (*MarkdownRenderer, error) {
	style := DarkStyleConfig()
	if !darkMode {
		style = LightStyleConfig()
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return nil, err
	}

	return &MarkdownRenderer{
		renderer: r,
		width:    width,
		darkMode: darkMode,
	}, nil
}

// Render renders markdown content to styled terminal output.
func (m *MarkdownRenderer) Render(markdown string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rendered, err := m.renderer.Render(markdown)
	if err != nil {
		return markdown, err
	}

	// Trim trailing newlines that glamour adds
	return strings.TrimSuffix(rendered, "\n"), nil
}

// SetWidth updates the word wrap width and recreates the renderer.
func (m *MarkdownRenderer) SetWidth(width int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if width == m.width {
		return nil
	}

	style := DarkStyleConfig()
	if !m.darkMode {
		style = LightStyleConfig()
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return err
	}

	m.renderer = r
	m.width = width
	return nil
}

// DetectMarkdown checks if content contains markdown formatting.
func DetectMarkdown(content string) bool {
	patterns := []string{
		"```",    // Fenced code block
		"\n# ",   // Heading
		"\n## ",  // Heading
		"\n### ", // Heading
		"\n- ",   // Unordered list
		"\n* ",   // Unordered list
		"\n1. ",  // Ordered list
		"\n> ",   // Blockquote
		"**",     // Bold
		"__",     // Bold
		"~~",     // Strikethrough
		"[",      // Link start (basic check)
	}

	// Check if starts with heading
	if strings.HasPrefix(content, "# ") ||
		strings.HasPrefix(content, "## ") ||
		strings.HasPrefix(content, "### ") {
		return true
	}

	// Check if starts with list
	if strings.HasPrefix(content, "- ") ||
		strings.HasPrefix(content, "* ") ||
		strings.HasPrefix(content, "1. ") {
		return true
	}

	for _, p := range patterns {
		if strings.Contains(content, p) {
			return true
		}
	}

	// Check for inline code (single backticks, but not triple)
	if strings.Contains(content, "`") && !strings.Contains(content, "```") {
		return true
	}

	return false
}

// DarkStyleConfig returns a dark theme style configuration.
func DarkStyleConfig() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "",
				BlockSuffix: "",
			},
			Margin: uintPtr(0),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("#6B7280"),
			},
			Indent:      uintPtr(1),
			IndentToken: stringPtr("│ "),
		},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: stringPtr("#F97316"),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:   boolPtr(true),
				Color:  stringPtr("#F97316"),
				Prefix: "# ",
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:   boolPtr(true),
				Color:  stringPtr("#F59E0B"),
				Prefix: "## ",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:   boolPtr(true),
				Color:  stringPtr("#10B981"),
				Prefix: "### ",
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:   boolPtr(true),
				Color:  stringPtr("#3B82F6"),
				Prefix: "#### ",
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:   boolPtr(true),
				Color:  stringPtr("#8B5CF6"),
				Prefix: "##### ",
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:   boolPtr(true),
				Color:  stringPtr("#EC4899"),
				Prefix: "###### ",
			},
		},
		Strikethrough: ansi.StylePrimitive{
			CrossedOut: boolPtr(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
			Color:  stringPtr("#E5E7EB"),
		},
		Strong: ansi.StylePrimitive{
			Bold:  boolPtr(true),
			Color: stringPtr("#FFFFFF"),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color:  stringPtr("#374151"),
			Format: "─────────────────────────────────────────",
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			StylePrimitive: ansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},
		Link: ansi.StylePrimitive{
			Color:     stringPtr("#3B82F6"),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: stringPtr("#60A5FA"),
		},
		Image: ansi.StylePrimitive{
			Color:  stringPtr("#8B5CF6"),
			Format: "[image: {{.text}}]",
		},
		ImageText: ansi.StylePrimitive{
			Color:  stringPtr("#A78BFA"),
			Format: "{{.text}}",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           stringPtr("#10B981"),
				BackgroundColor: stringPtr("#1F2937"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: stringPtr("#E5E7EB"),
				},
				Margin: uintPtr(0),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: stringPtr("#E5E7EB"),
				},
				Error: ansi.StylePrimitive{
					Color:           stringPtr("#EF4444"),
					BackgroundColor: stringPtr("#7F1D1D"),
				},
				Comment: ansi.StylePrimitive{
					Color: stringPtr("#6B7280"),
				},
				CommentPreproc: ansi.StylePrimitive{
					Color: stringPtr("#F59E0B"),
				},
				Keyword: ansi.StylePrimitive{
					Color: stringPtr("#F472B6"),
				},
				KeywordReserved: ansi.StylePrimitive{
					Color: stringPtr("#F472B6"),
				},
				KeywordNamespace: ansi.StylePrimitive{
					Color: stringPtr("#F472B6"),
				},
				KeywordType: ansi.StylePrimitive{
					Color: stringPtr("#67E8F9"),
				},
				Operator: ansi.StylePrimitive{
					Color: stringPtr("#F472B6"),
				},
				Punctuation: ansi.StylePrimitive{
					Color: stringPtr("#E5E7EB"),
				},
				Name: ansi.StylePrimitive{
					Color: stringPtr("#E5E7EB"),
				},
				NameBuiltin: ansi.StylePrimitive{
					Color: stringPtr("#67E8F9"),
				},
				NameTag: ansi.StylePrimitive{
					Color: stringPtr("#F472B6"),
				},
				NameAttribute: ansi.StylePrimitive{
					Color: stringPtr("#A3E635"),
				},
				NameClass: ansi.StylePrimitive{
					Color: stringPtr("#A3E635"),
				},
				NameConstant: ansi.StylePrimitive{
					Color: stringPtr("#C4B5FD"),
				},
				NameDecorator: ansi.StylePrimitive{
					Color: stringPtr("#A3E635"),
				},
				NameFunction: ansi.StylePrimitive{
					Color: stringPtr("#A3E635"),
				},
				NameOther: ansi.StylePrimitive{
					Color: stringPtr("#E5E7EB"),
				},
				Literal: ansi.StylePrimitive{
					Color: stringPtr("#C4B5FD"),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: stringPtr("#C4B5FD"),
				},
				LiteralDate: ansi.StylePrimitive{
					Color: stringPtr("#C4B5FD"),
				},
				LiteralString: ansi.StylePrimitive{
					Color: stringPtr("#FDE047"),
				},
				LiteralStringEscape: ansi.StylePrimitive{
					Color: stringPtr("#C4B5FD"),
				},
				GenericDeleted: ansi.StylePrimitive{
					Color: stringPtr("#EF4444"),
				},
				GenericEmph: ansi.StylePrimitive{
					Italic: boolPtr(true),
				},
				GenericInserted: ansi.StylePrimitive{
					Color: stringPtr("#10B981"),
				},
				GenericStrong: ansi.StylePrimitive{
					Bold: boolPtr(true),
				},
				GenericSubheading: ansi.StylePrimitive{
					Color: stringPtr("#67E8F9"),
				},
				Background: ansi.StylePrimitive{
					BackgroundColor: stringPtr("#111827"),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
			CenterSeparator: stringPtr("┼"),
			ColumnSeparator: stringPtr("│"),
			RowSeparator:    stringPtr("─"),
		},
		DefinitionDescription: ansi.StylePrimitive{
			BlockPrefix: "\n",
		},
	}
}

// LightStyleConfig returns a light theme style configuration.
func LightStyleConfig() ansi.StyleConfig {
	// Start with dark config and adjust colors
	style := DarkStyleConfig()

	// Override colors for light mode
	style.Heading.Color = stringPtr("#C2410C")
	style.H1.Color = stringPtr("#C2410C")
	style.H2.Color = stringPtr("#B45309")
	style.H3.Color = stringPtr("#047857")
	style.Strong.Color = stringPtr("#000000")
	style.Emph.Color = stringPtr("#374151")
	style.Code.Color = stringPtr("#047857")
	style.Code.BackgroundColor = stringPtr("#F3F4F6")
	style.Link.Color = stringPtr("#1D4ED8")
	style.LinkText.Color = stringPtr("#2563EB")

	return style
}

// Helper functions for pointer types
func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func uintPtr(u uint) *uint {
	return &u
}
