package tools

import "errors"

// ErrToolTimeout is the sentinel a tool-execution timeout classifies through:
// when a declared TimeoutMs budget expires, the executor returns an error
// wrapping this sentinel (routeable via errors.Is) with the machine-readable
// "TOOL_TIMEOUT" ErrCode, never a raw context.DeadlineExceeded. Upstream /
// turn-level cancellation is NOT masked through this sentinel — only the
// tool's own declared timer substitutes it (dsh convergence leaf
// 01-tool-timeout, contract B1).
var ErrToolTimeout = errors.New("tool call timed out")
