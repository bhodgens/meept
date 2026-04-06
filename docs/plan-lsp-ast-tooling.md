# AST and LSP Tooling Implementation Plan

This document outlines a complete implementation plan for adding AST (Abstract Syntax Tree) parsing and LSP (Language Server Protocol) client tooling to meept.

## Overview

Add multi-language code intelligence capabilities to meept through:
1. **Tree-sitter AST parsing** - Fast, incremental parsing for 13+ languages
2. **LSP client integration** - Connect to external language servers (gopls, rust-analyzer, pyright, etc.)
3. **Agent tools** - Expose capabilities as tools for the coder/debugger agents

## Current State

- No AST parsing capabilities (no go/ast, tree-sitter)
- No LSP client/server implementation
- Only Chroma lexer for syntax highlighting (display only)
- Agents rely on shell execution for grep/ripgrep code search
- Well-defined tool interface in `internal/tools/interface.go`

## Architecture

```
internal/code/
├── ast/                      # Tree-sitter AST layer
│   ├── types.go              # Symbol, Range, Node types
│   ├── languages.go          # Language registry with grammars
│   ├── parser.go             # ParserManager (multi-language)
│   ├── symbols.go            # Symbol extraction per language
│   ├── query.go              # Tree-sitter query execution
│   └── cache.go              # Parse result caching
│
├── lsp/                      # LSP client layer
│   ├── protocol.go           # LSP types (Location, Range, Diagnostic)
│   ├── transport/
│   │   ├── transport.go      # Transport interface
│   │   ├── stdio.go          # Stdio transport (subprocess)
│   │   └── tcp.go            # TCP transport (remote)
│   ├── client.go             # Single-server LSP client
│   ├── manager.go            # Multi-server lifecycle manager
│   ├── document.go           # Document sync (didOpen/didChange/didClose)
│   └── capabilities.go       # Server capability detection
│
└── tools/                    # Agent-facing tools
    ├── ast_parse.go          # ast_parse tool
    ├── ast_symbols.go        # ast_symbols tool
    ├── ast_query.go          # ast_query tool
    ├── lsp_definition.go     # lsp_goto_definition tool
    ├── lsp_references.go     # lsp_find_references tool
    ├── lsp_hover.go          # lsp_hover tool
    ├── lsp_symbols.go        # lsp_workspace_symbols tool
    ├── lsp_diagnostics.go    # lsp_diagnostics tool
    └── registration.go       # Tool registration helpers
```

## Tools to Implement

### AST Tools (tree-sitter based, no external dependencies)

| Tool | Description | Use Case |
|------|-------------|----------|
| `ast_parse` | Parse source into syntax tree | Understand code structure |
| `ast_symbols` | Extract functions, classes, methods | Code navigation, indexing |
| `ast_query` | Run tree-sitter S-expression queries | Find specific patterns |

### LSP Tools (require external language servers)

| Tool | Description | LSP Method |
|------|-------------|------------|
| `lsp_goto_definition` | Find symbol definition | `textDocument/definition` |
| `lsp_find_references` | Find all usages | `textDocument/references` |
| `lsp_hover` | Get type/doc info | `textDocument/hover` |
| `lsp_workspace_symbols` | Search symbols globally | `workspace/symbol` |
| `lsp_diagnostics` | Get errors/warnings | `textDocument/publishDiagnostics` |

## Dependencies to Add

```go
// go.mod additions
github.com/tree-sitter/go-tree-sitter v0.24.0

// Language grammars (tree-sitter)
github.com/tree-sitter/tree-sitter-go
github.com/tree-sitter/tree-sitter-python
github.com/tree-sitter/tree-sitter-javascript
github.com/tree-sitter/tree-sitter-typescript
github.com/tree-sitter/tree-sitter-rust
github.com/tree-sitter/tree-sitter-c
github.com/tree-sitter/tree-sitter-cpp
github.com/tree-sitter/tree-sitter-java
github.com/tree-sitter/tree-sitter-ruby
github.com/tree-sitter/tree-sitter-yaml
github.com/tree-sitter/tree-sitter-json
github.com/tree-sitter/tree-sitter-toml
github.com/tree-sitter/tree-sitter-bash
```

## Configuration

Add to `internal/config/schema.go`:

```go
// CodeIntelConfig holds code intelligence settings
type CodeIntelConfig struct {
    AST ASTConfig `toml:"ast"`
    LSP LSPConfig `toml:"lsp"`
}

type ASTConfig struct {
    Enabled          bool     `toml:"enabled"`
    EnableCache      bool     `toml:"enable_cache"`
    CacheMaxEntries  int      `toml:"cache_max_entries"`
    CacheTTLSeconds  int      `toml:"cache_ttl_seconds"`
    EnabledLanguages []string `toml:"enabled_languages"`
}

type LSPConfig struct {
    Enabled         bool              `toml:"enabled"`
    Servers         []LSPServerConfig `toml:"servers"`
    StartupTimeout  int               `toml:"startup_timeout_seconds"`
    ShutdownTimeout int               `toml:"shutdown_timeout_seconds"`
    AutoStart       bool              `toml:"auto_start"`
}

type LSPServerConfig struct {
    Name        string            `toml:"name"`
    Languages   []string          `toml:"languages"`
    Command     []string          `toml:"command"`
    TCP         string            `toml:"tcp"`
    Env         map[string]string `toml:"env"`
    RootPath    string            `toml:"root_path"`
    InitOptions map[string]any    `toml:"init_options"`
}
```

Add `CodeIntel CodeIntelConfig` to `Config` struct.

Example `meept.toml` configuration:

```toml
[codeintel.ast]
enabled = true
enable_cache = true
cache_max_entries = 1000
cache_ttl_seconds = 300

[codeintel.lsp]
enabled = true
auto_start = true

[[codeintel.lsp.servers]]
name = "gopls"
languages = ["go"]
command = ["gopls", "serve"]

[[codeintel.lsp.servers]]
name = "pyright"
languages = ["python"]
command = ["pyright-langserver", "--stdio"]

[[codeintel.lsp.servers]]
name = "typescript"
languages = ["typescript", "javascript"]
command = ["typescript-language-server", "--stdio"]

[[codeintel.lsp.servers]]
name = "rust-analyzer"
languages = ["rust"]
command = ["rust-analyzer"]
```

## Key Type Definitions

### AST Types (`internal/code/ast/types.go`)

```go
type Language string

const (
    LangGo         Language = "go"
    LangPython     Language = "python"
    LangTypeScript Language = "typescript"
    LangJavaScript Language = "javascript"
    LangRust       Language = "rust"
    LangC          Language = "c"
    LangCpp        Language = "cpp"
    LangJava       Language = "java"
    LangRuby       Language = "ruby"
    LangYAML       Language = "yaml"
    LangJSON       Language = "json"
    LangTOML       Language = "toml"
    LangBash       Language = "bash"
)

type Symbol struct {
    Name       string     `json:"name"`
    Kind       SymbolKind `json:"kind"`
    Language   Language   `json:"language"`
    FilePath   string     `json:"file_path"`
    Range      Range      `json:"range"`
    Signature  string     `json:"signature,omitempty"`
    DocComment string     `json:"doc_comment,omitempty"`
    Children   []Symbol   `json:"children,omitempty"`
}

type SymbolKind int // Matches LSP SymbolKind values

type Range struct {
    StartLine   int `json:"start_line"`
    StartColumn int `json:"start_column"`
    EndLine     int `json:"end_line"`
    EndColumn   int `json:"end_column"`
}
```

### LSP Types (`internal/code/lsp/protocol.go`)

```go
type Location struct {
    URI   string `json:"uri"`
    Range Range  `json:"range"`
}

type HoverResult struct {
    Contents MarkupContent `json:"contents"`
    Range    *Range        `json:"range,omitempty"`
}

type Diagnostic struct {
    Range    Range              `json:"range"`
    Severity DiagnosticSeverity `json:"severity"`
    Code     any                `json:"code,omitempty"`
    Source   string             `json:"source,omitempty"`
    Message  string             `json:"message"`
}
```

## Tool Registration

Modify `internal/daemon/components.go`:

```go
func registerBuiltinTools(...) {
    // ... existing tools ...

    // Code intelligence tools
    if cfg.CodeIntel.AST.Enabled {
        parserMgr := ast.NewParserManager(ast.ParserConfig{
            EnableCache:     cfg.CodeIntel.AST.EnableCache,
            CacheMaxEntries: cfg.CodeIntel.AST.CacheMaxEntries,
            CacheTTL:        time.Duration(cfg.CodeIntel.AST.CacheTTLSeconds) * time.Second,
            Logger:          logger.With("component", "ast"),
        })
        registry.Register(codetools.NewASTParseTool(parserMgr, checker))
        registry.Register(codetools.NewASTSymbolsTool(parserMgr, checker))
        registry.Register(codetools.NewASTQueryTool(parserMgr, checker))
    }

    if cfg.CodeIntel.LSP.Enabled {
        lspMgr := lsp.NewManager(lsp.ManagerConfig{
            StartupTimeout:  time.Duration(cfg.CodeIntel.LSP.StartupTimeout) * time.Second,
            ShutdownTimeout: time.Duration(cfg.CodeIntel.LSP.ShutdownTimeout) * time.Second,
            Logger:          logger.With("component", "lsp"),
        })
        // Auto-start configured servers
        for _, serverCfg := range cfg.CodeIntel.LSP.Servers {
            _ = lspMgr.StartServer(ctx, serverCfg)
        }
        registry.Register(codetools.NewLSPDefinitionTool(lspMgr, checker))
        registry.Register(codetools.NewLSPReferencesTool(lspMgr, checker))
        registry.Register(codetools.NewLSPHoverTool(lspMgr, checker))
        registry.Register(codetools.NewLSPWorkspaceSymbolsTool(lspMgr, checker))
        registry.Register(codetools.NewLSPDiagnosticsTool(lspMgr, checker))
    }
}
```

## Security Considerations

1. **Path validation** - All file paths validated through `security.PermissionChecker`
2. **LSP server allowlist** - Only configured servers can be started (no dynamic spawning)
3. **Resource limits**:
   - AST: 10MB max file size, depth limit for tree traversal
   - LSP: 30s request timeout, cache eviction policies
4. **Input sanitization** - Validate tree-sitter queries, sanitize URIs

Map new tools to existing permission actions in `internal/agent/executor.go`:

```go
"ast_parse":             "file_read",
"ast_symbols":           "file_read",
"ast_query":             "file_read",
"lsp_goto_definition":   "file_read",
"lsp_find_references":   "file_read",
"lsp_hover":             "file_read",
"lsp_workspace_symbols": "file_read",
"lsp_diagnostics":       "file_read",
```

## Implementation Phases

### Phase 1: AST Foundation
- [ ] Add tree-sitter dependencies to go.mod
- [ ] Implement `internal/code/ast/types.go`
- [ ] Implement `internal/code/ast/languages.go` with language registry
- [ ] Implement `internal/code/ast/parser.go` (ParserManager)
- [ ] Implement `internal/code/ast/symbols.go` (Go, Python, TypeScript extractors)
- [ ] Implement `internal/code/ast/query.go`
- [ ] Unit tests for AST components

### Phase 2: AST Tools
- [ ] Implement `internal/code/tools/ast_parse.go`
- [ ] Implement `internal/code/tools/ast_symbols.go`
- [ ] Implement `internal/code/tools/ast_query.go`
- [ ] Add `CodeIntelConfig` to `internal/config/schema.go`
- [ ] Integrate with `internal/daemon/components.go`
- [ ] Integration tests

### Phase 3: LSP Foundation
- [ ] Implement `internal/code/lsp/protocol.go`
- [ ] Implement `internal/code/lsp/transport/transport.go` (interface)
- [ ] Implement `internal/code/lsp/transport/stdio.go`
- [ ] Implement `internal/code/lsp/transport/tcp.go`
- [ ] Implement `internal/code/lsp/client.go`
- [ ] Implement `internal/code/lsp/document.go`
- [ ] Unit tests for LSP client

### Phase 4: LSP Manager & Tools
- [ ] Implement `internal/code/lsp/manager.go`
- [ ] Implement `internal/code/lsp/capabilities.go`
- [ ] Implement all LSP tools in `internal/code/tools/`
- [ ] Add LSP configuration to schema
- [ ] Integrate with daemon startup/shutdown
- [ ] Integration tests with real language servers (gopls)

### Phase 5: Caching & Performance
- [ ] Implement `internal/code/ast/cache.go` (LRU parse cache)
- [ ] Add LSP response caching
- [ ] Performance benchmarks
- [ ] Memory profiling and optimization

### Phase 6: Documentation & Polish
- [ ] Update CLAUDE.md with new tools
- [ ] Update features.md
- [ ] Add LSP server setup documentation
- [ ] End-to-end testing with coder/debugger agents

## Files to Create

| File | Purpose |
|------|---------|
| `internal/code/ast/types.go` | Core AST types (Symbol, Range, Node, Language) |
| `internal/code/ast/languages.go` | Language registry and grammar initialization |
| `internal/code/ast/parser.go` | ParserManager with multi-language support |
| `internal/code/ast/symbols.go` | Per-language symbol extraction |
| `internal/code/ast/query.go` | Tree-sitter query execution |
| `internal/code/ast/cache.go` | LRU cache for parsed trees |
| `internal/code/lsp/protocol.go` | LSP protocol types |
| `internal/code/lsp/transport/transport.go` | Transport interface |
| `internal/code/lsp/transport/stdio.go` | Stdio transport implementation |
| `internal/code/lsp/transport/tcp.go` | TCP transport implementation |
| `internal/code/lsp/client.go` | Single-server LSP client |
| `internal/code/lsp/manager.go` | Multi-server lifecycle manager |
| `internal/code/lsp/document.go` | Document synchronization |
| `internal/code/lsp/capabilities.go` | Capability detection |
| `internal/code/tools/ast_parse.go` | ast_parse tool |
| `internal/code/tools/ast_symbols.go` | ast_symbols tool |
| `internal/code/tools/ast_query.go` | ast_query tool |
| `internal/code/tools/lsp_definition.go` | lsp_goto_definition tool |
| `internal/code/tools/lsp_references.go` | lsp_find_references tool |
| `internal/code/tools/lsp_hover.go` | lsp_hover tool |
| `internal/code/tools/lsp_symbols.go` | lsp_workspace_symbols tool |
| `internal/code/tools/lsp_diagnostics.go` | lsp_diagnostics tool |
| `internal/code/tools/registration.go` | Registration helpers |

## Files to Modify

| File | Change |
|------|--------|
| `go.mod` | Add tree-sitter dependencies |
| `internal/config/schema.go` | Add `CodeIntelConfig` struct and defaults |
| `internal/daemon/components.go` | Register AST/LSP tools in `registerBuiltinTools()` |
| `internal/agent/executor.go` | Add tool-to-action mappings for permissions |
| `CLAUDE.md` | Document new tools |
| `docs/features.md` | Document code intelligence features |

## Verification

1. **Unit tests**: Run `go test ./internal/code/...`
2. **Integration tests**: Test with real language servers
3. **Manual testing**:
   ```bash
   # Test AST tools
   ./bin/meept chat "Use ast_symbols to list all functions in internal/agent/loop.go"

   # Test LSP tools (requires gopls running)
   ./bin/meept chat "Use lsp_goto_definition for AgentLoop at line 50 column 10 in internal/agent/loop.go"
   ```
4. **Verify coder agent** can use tools for code understanding tasks
