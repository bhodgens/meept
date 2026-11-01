package tools

import "context"

// stub: implementation lands in 02-gate.md
//
// Interface declaration only - zero logic. SetGate/Gate and the gate
// field on Registry are INTENTIONALLY NOT DECLARED here: 02-gate.md
// implements them, and the gate tests must fail RED naming SetGate
// exactly. (During the F5 mutation check this file temporarily carried
// the SetGate shape; that was reverted - see the 00T completion report.)
type ExecGate interface {
	BeforeExecute(ctx context.Context, tool Tool, args map[string]any) error
	AfterExecute(ctx context.Context, tool Tool, args map[string]any, res *ToolResult, err error)
}
