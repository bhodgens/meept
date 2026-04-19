# Code Intelligence (AST+LSP)

## Overview
Meept provides advanced code understanding through tree-sitter parsing and Language Server Protocol (LSP) integration. This enables sophisticated code analysis, navigation, and refactoring capabilities across multiple programming languages.

## Problem
Basic text-based code analysis lacks semantic understanding. Code intelligence addresses:
- Accurate code structure parsing
- Cross-reference navigation
- Semantic code understanding
- Multi-language support

## Behavior

### Tree-Sitter Parsing Tools
| Tool | Description |
|------|-------------|
| `ast_parse` | Parse code into AST with tree-sitter |
| `ast_symbols` | Extract symbols (functions, classes, variables) |
| `ast_query` | Query AST with pattern matching |

### LSP Client Tools
| Tool | Description |
|------|-------------|
| `lsp_goto_definition` | Navigate to symbol definition |
| `lsp_find_references` | Find all references to symbol |
| `lsp_hover` | Get documentation on hover |
| `lsp_workspace_symbols` | Search symbols across workspace |
| `lsp_diagnostics` | Get code errors and warnings |

### Multi-Language Support
- **Go**: Native tree-sitter grammar
- **Python**: Comprehensive parsing support
- **JavaScript/TypeScript**: Full LSP integration
- **Multiple Languages**: Extensible architecture

### Integration Points
- **Code Analysis**: Understand code structure and dependencies
- **Refactoring Support**: Safe code modifications
- **Navigation**: Efficient code exploration
- **Error Detection**: Early problem identification

## Configuration

```toml
[code_intel]
enabled = true
ast_enabled = true
lsp_enabled = true

[code_intel.ast]
parsers = ["go", "python", "javascript", "typescript"]
max_file_size_mb = 10

[code_intel.lsp]
servers = [
  {language = "go", command = "gopls", args = ["serve"]},
  {language = "python", command = "pylsp", args = []},
  {language = "typescript", command = "typescript-language-server", args = ["--stdio"]}
]
timeout_seconds = 30
```

## Observability

### Logging
- AST parsing operations
- LSP server connections
- Code analysis events
- Error recovery attempts

### Metrics
- Parse success rates
- LSP response times
- Symbol extraction accuracy
- Multi-language support coverage

### Debug Info
- Active language servers
- Parser availability
- Current analysis sessions
- Error diagnostics

## Edge Cases

### Parser Failure
- Graceful fallback to text analysis
- Error recovery with partial parsing
- Logs parser issues for debugging

### LSP Server Unavailable
- Degraded functionality without LSP
- Basic AST parsing still available
- Automatic server restart attempts

### Large File Handling
- File size limits enforced
- Progressive parsing for large files
- Memory usage monitoring

### Language Support Gaps
- Clear error messages for unsupported languages
- Community contribution guidance
- Fallback to generic text analysis