# Cluster B: LSP & Code Intelligence Enhancements

## Goal
Achieve parity with oh-my-pi's code intelligence, including barrel-file-aware renames and structural AST search.

## Background
Meept has a solid LSP foundation (15 tools) and AST query via tree-sitter. oh-my-pi goes further with:
- Workspace rename through `workspace/willRenameFiles` (barrel file updates)
- `ast_edit` for structural rewrites with preview-before-apply
- In-process ripgrep/glob/search (not relevant to cluster B)

## Feature Checklist

### 1. Verify Barrel-File Rename Handling
- oh-my-pi: rename triggers `workspace/willRenameFiles`, updating re-exports and aliased imports
- Meept: `lsp_rename.go` calls `client.Rename()` but doesn't check for `workspace/willRenameFiles` support
- We need to verify our rename handles:
  - Barrel/index files that re-export the renamed symbol
  - Aliased imports (`import { foo as bar }`)
  - Re-export changes (`export { foo }` -> `export { newFoo }`)

### 2. AST Structural Edit Tool (ast_edit)
- Use tree-sitter queries to target AST nodes, not lines
- Preview changes before application ( proposing card / resolve pattern)
- Support operations:
  - Replace all function bodies matching a pattern
  - Rename all variable declarations of a type
  - Insert code before/after specific AST node types

### 3. Enhanced AST Query (ast_grep)
- oh-my-pi: 50+ tree-sitter grammars, structural queries
- Meept: `ast_query` tool with S-expressions
- Enhancements:
  - Add YAML rule format (like ast-grep) for complex patterns
  - Support `rewrites` in query results
  - Report matches with surrounding context

## Implementation Plan

### Phase 1: Verify & Fix Rename
1. Check `internal/comm/lsp/client.go` for `Rename` implementation
2. Check if `WorkspaceEdit` includes `documentChanges`
3. If not, add `willRenameFiles` support where LSP server advertises it
4. Test with a Go project using index/barrel patterns

### Phase 2: AST Edit Tool
1. Create `internal/code/tools/ast_edit.go`
2. Accept parameters: `query` (tree-sitter), `file_path`, `rewrite_template`
3. Execute query to find nodes
4. Generate proposed edits (don't apply)
5. Return preview with count of affected files/lines
6. Requires a `resolve` tool to apply afterward

### Phase 3: Enhanced AST Query
1. Add YAML-based rules support to `ast_query`
2. Support `constraints`, `transform`, `fix` fields
3. Return structured match results with `before`/`after` context

## Files to Modify / Create
- `internal/code/lsp/client.go` — Rename with barrel awareness
- `internal/code/tools/lsp_rename.go` — Verify/test barrel handling
- `internal/code/tools/ast_edit.go` (new) — Structural edit tool
- `internal/code/ast/rewrite.go` (new) — AST rewrite engine
- `internal/code/tools/ast_query.go` — Enhanced query features

## Success Criteria
- [ ] Rename of exported symbol in Go updates all re-exports and imports
- [ ] `ast_edit` tool returns preview without modifying files
- [ ] `resolve` tool can apply/discard pending `ast_edit` proposals
- [ ] AST query supports YAML rules with constraints and transforms
