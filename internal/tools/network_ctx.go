package tools

import (
	"context"
	"strings"
)

// networkClassCtxKey carries the agent's resolved network class into tool
// execution. Empty / absent means unrestricted (legacy unmanaged posture).
type networkClassCtxKey struct{}

// ContextWithNetworkClass returns ctx carrying the resolved network class.
// A blank class stores nothing so callers that omit the class stay on the
// unrestricted path.
func ContextWithNetworkClass(ctx context.Context, class string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	class = strings.TrimSpace(class)
	if class == "" {
		return ctx
	}
	return context.WithValue(ctx, networkClassCtxKey{}, class)
}

// NetworkClassFromContext returns the network class stamped for this tool
// call, or "" when absent.
func NetworkClassFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	class, _ := ctx.Value(networkClassCtxKey{}).(string)
	return class
}
