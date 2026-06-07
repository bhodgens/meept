package builtin

import (
	"context"

	"github.com/caimlas/meept/internal/code/ast"
)

// ASTBlockResolver adapts *ast.ParserManager to the BlockResolver interface
// used by FileEditTool for syntactic block operations.
type ASTBlockResolver struct {
	pm  *ast.ParserManager
	ctx context.Context
}

// NewASTBlockResolver creates a BlockResolver backed by a ParserManager.
func NewASTBlockResolver(pm *ast.ParserManager, ctx context.Context) *ASTBlockResolver {
	return &ASTBlockResolver{pm: pm, ctx: ctx}
}

// ResolveBlock implements BlockResolver by querying the AST for the syntactic
// block that starts at or contains the given line.
func (r *ASTBlockResolver) ResolveBlock(filePath string, lineNum int) (startLine int, endLine int, err error) {
	span, err := r.pm.FindBlockSpan(r.ctx, filePath, lineNum)
	if err != nil {
		return 0, 0, err
	}
	return span.StartLine, span.EndLine, nil
}

// Ensure ASTBlockResolver implements BlockResolver.
var _ BlockResolver = (*ASTBlockResolver)(nil)
