package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestHandoffDepthReachesReportRouter is the regression pin for the 2026-10-08
// runaway: 513M task rows (150 GB) and a 145 GB meept.log at ~10,000-14,000
// rows/second, filling a 927 GB disk.
//
// ROOT CAUSE: internal/agent/report_router.go:80 already had the cycle guard —
// `if params.Depth >= r.maxDepth { ForceNotify: true }` — but the dispatcher's
// only report-router call site passed a hardcoded literal `Depth: 0`
// (dispatcher.go:3152). Depth therefore never grew, the guard was permanently
// false, and two agents that name each other as SuggestedNextAgent recursed
// without bound. Each hop created a task row through createTask ->
// taskStore.Create (dispatcher.go:2447), which was the only non-test producer of
// task rows.
//
// WHY THIS TEST IS AN AST PIN RATHER THAN A BEHAVIOURAL TEST: the first version
// of this pin drove ReportRouter.Route directly and PASSED with the fix reverted,
// because the router honours whatever depth it is handed. The defect was in the
// dispatcher's plumbing, not the router, so only a test that reads the
// dispatcher's actual call site can catch it. The behavioural half of the guard
// is already covered by report_router_test.go and is re-asserted below.
func TestHandoffDepthReachesReportRouter(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join(repoRoot(t), "internal", "agent", "dispatcher.go")
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// Find every RouteParams composite literal and record the Depth argument.
	var depthArgs []string
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if !isRouteParamsType(cl.Type) {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Depth" {
				continue
			}
			switch v := kv.Value.(type) {
			case *ast.BasicLit:
				depthArgs = append(depthArgs, "literal:"+v.Value)
			case *ast.SelectorExpr:
				depthArgs = append(depthArgs, "expr:"+exprString(v))
			case *ast.BinaryExpr:
				depthArgs = append(depthArgs, "expr:"+exprString(v))
			default:
				depthArgs = append(depthArgs, "expr:"+exprString(kv.Value))
			}
		}
		return true
	})

	if len(depthArgs) == 0 {
		t.Fatal("no RouteParams literal with a Depth field found in dispatcher.go; " +
			"the routing call site moved and this pin must be updated")
	}

	// A literal 0 here is the exact pre-fix defect: the guard can never trip.
	for _, arg := range depthArgs {
		if arg == "literal:0" {
			t.Fatalf("dispatcher.go passes a hardcoded Depth: 0 to the report router. "+
				"The router's cycle guard (report_router.go:80, `params.Depth >= r.maxDepth`) "+
				"can then never fire, so mutual agent routing recurses without bound and "+
				"writes one task row per hop. Pass result.HandoffDepth instead. found: %v",
				depthArgs)
		}
	}

	// At least one site must read the threaded counter.
	threaded := false
	for _, arg := range depthArgs {
		if arg == "expr:result.HandoffDepth" {
			threaded = true
		}
	}
	if !threaded {
		t.Fatalf("no RouteParams literal reads result.HandoffDepth; the depth is "+
			"never threaded into the router. found: %v", depthArgs)
	}
}

// TestHandoffCarriesDepthPlusOne proves the recursive hop increments the counter.
// Without the +1 the chain would stay at the parent's depth and a mutual pair
// would still recurse (each hop seeing the same depth).
func TestHandoffCarriesDepthPlusOne(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "agent", "dispatcher.go"))
	if err != nil {
		t.Fatalf("read dispatcher.go: %v", err)
	}
	if !strings.Contains(string(src), "HandoffDepth: result.HandoffDepth + 1") {
		t.Fatal("dispatcher.go does not carry HandoffDepth+1 into the child " +
			"DispatchResult; a mutual routing pair would never advance depth")
	}
	// And the recursion must go through RouteToAgent, which is what re-enters.
	if !strings.Contains(string(src), "return d.RouteToAgent(ctx, nextResult, conversationID)") {
		t.Fatal("expected the recursive handoff call to RouteToAgent in dispatcher.go")
	}
}

// TestRouterGuardTerminatesMutualPair is the behavioural half: the router's
// existing guard really does stop an unbounded chain once depth is threaded.
func TestRouterGuardTerminatesMutualPair(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{MaxDepth: 0}) // default maxDepth
	if router.maxDepth != defaultMaxRouteDepth {
		t.Fatalf("router maxDepth = %d, want %d", router.maxDepth, defaultMaxRouteDepth)
	}

	// The exact incident shape: two agents naming each other, each hop carrying
	// parent+1 as the dispatcher now does.
	result := &DispatchResult{AgentID: "agent-a", HandoffDepth: 0}
	hops := 0
	for hops < 1000 {
		other := "agent-b"
		if result.AgentID == "agent-b" {
			other = "agent-a"
		}
		rr := router.Route(t.Context(), RouteParams{
			Action:  RouteActionRoute,
			AgentID: result.AgentID,
			Depth:   result.HandoffDepth,
		})
		if rr.ForceNotify {
			break
		}
		result = &DispatchResult{AgentID: other, HandoffDepth: result.HandoffDepth + 1}
		hops++
	}
	if hops != defaultMaxRouteDepth {
		t.Fatalf("mutual routing terminated after %d hops, want %d", hops, defaultMaxRouteDepth)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// isRouteParamsType reports whether a composite literal's type is RouteParams.
// The call site uses the unqualified name (`RouteParams{...}`), so both the bare
// ident and a qualified selector are accepted.
func isRouteParamsType(expr ast.Expr) bool {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name == "RouteParams"
	case *ast.SelectorExpr:
		return v.Sel.Name == "RouteParams"
	}
	return false
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.Ident:
		return v.Name
	case *ast.BinaryExpr:
		return exprString(v.X) + " + " + exprString(v.Y)
	case *ast.BasicLit:
		return v.Value
	default:
		return "?"
	}
}
