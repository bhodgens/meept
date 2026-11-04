// Tool-registry context seam (token-efficiency branch 02 leaf 02).
//
// The executor injects the agent's ALREADY-filtered tool registry into the
// tool-execution context before invoking any tool (executeToolWithRetry).
// The codemode tool (internal/tools/builtin/codemode.go) reads it to build
// the sandbox's tool bridges strictly from that filtered set — it never
// widens the agent's tool access, only batches the calls the agent is
// already permitted to make.
package agent

import "context"

// toolRegistryContextKeyType is a dedicated typed context key (satisfies
// staticcheck SA1029 — no builtin string key).
type toolRegistryContextKeyType struct{}

var toolRegistryContextKey = toolRegistryContextKeyType{}

// ContextWithToolRegistry stores the agent's filtered tool registry under
// the typed key, replacing any prior value (the executor is the single
// injection point, so there is at most one value on a tool's execution
// context).
func ContextWithToolRegistry(ctx context.Context, reg ToolRegistry) context.Context {
	if reg == nil {
		return ctx
	}
	return context.WithValue(ctx, toolRegistryContextKey, reg)
}

// ToolRegistryFromContext returns the filtered tool registry the executor
// injected for this execution, or nil when no registry was carried
// (standalone tool use, tests, or an executor path that does not inject).
func ToolRegistryFromContext(ctx context.Context) ToolRegistry {
	if ctx == nil {
		return nil
	}
	reg, _ := ctx.Value(toolRegistryContextKey).(ToolRegistry)
	return reg
}
