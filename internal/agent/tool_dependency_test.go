package agent

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// makeToolCall is a test helper that constructs an llm.ToolCall.
func makeToolCall(id, name, arguments string) llm.ToolCall {
	return llm.ToolCall{
		ID:   id,
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      name,
			Arguments: arguments,
		},
	}
}

func newTestInferrer() *DependencyInferrer {
	return NewDependencyInferrer(NewPlaceholderToolRegistry(), slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError})))
}

// ---------------------------------------------------------------------------
// DependencyInferrer tests
// ---------------------------------------------------------------------------

func TestDependencyInferrer_FilePathOverlap(t *testing.T) {
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("call_write", "file_write", `{"file":"/a/x"}`),
		makeToolCall("call_read", "file_read", `{"file":"/a/x"}`),
	}

	graph := inferrer.InferDependencies(calls)

	deps := graph.GetDependencies("call_read")
	if len(deps) != 1 {
		t.Fatalf("expected call_read to have 1 dependency, got %d: %v", len(deps), deps)
	}
	if deps[0] != "call_write" {
		t.Errorf("expected call_read to depend on call_write, got %s", deps[0])
	}

	// The write call should have no dependencies.
	writeDeps := graph.GetDependencies("call_write")
	if len(writeDeps) != 0 {
		t.Errorf("expected call_write to have 0 dependencies, got %v", writeDeps)
	}
}

func TestDependencyInferrer_FilePathOverlapEquivalentPaths(t *testing.T) {
	// filepath.Clean should normalize /a/./x and /a/x to the same path.
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("call_write", "file_write", `{"file":"/a/./x"}`),
		makeToolCall("call_read", "file_read", `{"path":"/a/x"}`),
	}

	graph := inferrer.InferDependencies(calls)

	deps := graph.GetDependencies("call_read")
	if len(deps) != 1 || deps[0] != "call_write" {
		t.Fatalf("expected call_read to depend on call_write after Clean, got %v", deps)
	}
}

func TestDependencyInferrer_IndependentCalls(t *testing.T) {
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("read_a", "file_read", `{"file":"/a"}`),
		makeToolCall("read_b", "file_read", `{"file":"/b"}`),
	}

	graph := inferrer.InferDependencies(calls)

	if deps := graph.GetDependencies("read_a"); len(deps) != 0 {
		t.Errorf("read_a should have no dependencies, got %v", deps)
	}
	if deps := graph.GetDependencies("read_b"); len(deps) != 0 {
		t.Errorf("read_b should have no dependencies, got %v", deps)
	}
}

func TestDependencyInferrer_SameResourceWrites(t *testing.T) {
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("write_1", "file_write", `{"file":"/a/x"}`),
		makeToolCall("write_2", "file_write", `{"file":"/a/x"}`),
	}

	graph := inferrer.InferDependencies(calls)

	deps := graph.GetDependencies("write_2")
	if len(deps) != 1 {
		t.Fatalf("expected write_2 to depend on write_1, got %v", deps)
	}
	if deps[0] != "write_1" {
		t.Errorf("expected write_2 to depend on write_1, got %s", deps[0])
	}

	// write_1 should have no dependencies.
	if deps := graph.GetDependencies("write_1"); len(deps) != 0 {
		t.Errorf("write_1 should have no dependencies, got %v", deps)
	}
}

func TestDependencyInferrer_ExplicitReference(t *testing.T) {
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("call_A", "web_search", `{"query":"test"}`),
		makeToolCall("call_B", "memory_store", `{"key":"result","value":"$call_A.result"}`),
	}

	graph := inferrer.InferDependencies(calls)

	deps := graph.GetDependencies("call_B")
	if len(deps) != 1 {
		t.Fatalf("expected call_B to depend on call_A, got %v", deps)
	}
	if deps[0] != "call_A" {
		t.Errorf("expected call_B to depend on call_A, got %s", deps[0])
	}
}

func TestDependencyInferrer_ShellCommandSubstitution(t *testing.T) {
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("prev_cmd", "shell", `{"command":"ls -la"}`),
		makeToolCall("next_cmd", "shell", `{"command":"echo $(prev_cmd)"}`),
	}

	graph := inferrer.InferDependencies(calls)

	deps := graph.GetDependencies("next_cmd")
	if len(deps) != 1 {
		t.Fatalf("expected next_cmd to depend on prev_cmd, got %v", deps)
	}
	if deps[0] != "prev_cmd" {
		t.Errorf("expected next_cmd to depend on prev_cmd, got %s", deps[0])
	}
}

func TestDependencyInferrer_NoFalseDependencyOnDifferentPaths(t *testing.T) {
	inferrer := newTestInferrer()
	calls := []llm.ToolCall{
		makeToolCall("write_x", "file_write", `{"file":"/a/x"}`),
		makeToolCall("write_y", "file_write", `{"file":"/a/y"}`),
	}

	graph := inferrer.InferDependencies(calls)

	if deps := graph.GetDependencies("write_x"); len(deps) != 0 {
		t.Errorf("write_x should have no deps, got %v", deps)
	}
	if deps := graph.GetDependencies("write_y"); len(deps) != 0 {
		t.Errorf("write_y should have no deps, got %v", deps)
	}
}

// ---------------------------------------------------------------------------
// ToolDependencyGraph tests
// ---------------------------------------------------------------------------

func TestToolDependencyGraph_IndependentGroups(t *testing.T) {
	graph := NewToolDependencyGraph()

	call1 := makeToolCall("call_1", "file_read", `{"file":"/a"}`)
	call2 := makeToolCall("call_2", "file_read", `{"file":"/b"}`)
	call3 := makeToolCall("call_3", "file_write", `{"file":"/a"}`)
	call4 := makeToolCall("call_4", "file_write", `{"file":"/b"}`)

	graph.AddCall(call1)
	graph.AddCall(call2)
	graph.AddCall(call3)
	graph.AddCall(call4)

	// call3 depends on call1 (write after read on /a)
	graph.AddDependency("call_3", "call_1")
	// call4 depends on call2 (write after read on /b)
	graph.AddDependency("call_4", "call_2")

	groups := graph.IndependentGroups()

	// Expect 2 waves:
	//   wave 0: call_1 and call_2 (independent)
	//   wave 1: call_3 and call_4 (independent of each other, but depend on wave 0)
	if len(groups) != 2 {
		t.Fatalf("expected 2 independent groups, got %d: %+v", len(groups), groups)
	}

	wave0IDs := callIDsFromGroup(groups[0])
	wave1IDs := callIDsFromGroup(groups[1])

	if len(wave0IDs) != 2 {
		t.Fatalf("expected wave 0 to have 2 calls, got %d: %v", len(wave0IDs), wave0IDs)
	}
	if !sliceContains(wave0IDs, "call_1") || !sliceContains(wave0IDs, "call_2") {
		t.Errorf("wave 0 should contain call_1 and call_2, got %v", wave0IDs)
	}

	if len(wave1IDs) != 2 {
		t.Fatalf("expected wave 1 to have 2 calls, got %d: %v", len(wave1IDs), wave1IDs)
	}
	if !sliceContains(wave1IDs, "call_3") || !sliceContains(wave1IDs, "call_4") {
		t.Errorf("wave 1 should contain call_3 and call_4, got %v", wave1IDs)
	}
}

func TestToolDependencyGraph_IndependentGroupsChain(t *testing.T) {
	// A linear chain: call3 -> call2 -> call1 (3 waves of 1 each).
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("c1", "file_read", `{}`))
	graph.AddCall(makeToolCall("c2", "file_read", `{}`))
	graph.AddCall(makeToolCall("c3", "file_read", `{}`))
	graph.AddDependency("c2", "c1")
	graph.AddDependency("c3", "c2")

	groups := graph.IndependentGroups()

	if len(groups) != 3 {
		t.Fatalf("expected 3 groups for linear chain, got %d", len(groups))
	}
	if len(groups[0]) != 1 || groups[0][0].ID != "c1" {
		t.Errorf("wave 0 should be [c1], got %+v", groups[0])
	}
	if len(groups[1]) != 1 || groups[1][0].ID != "c2" {
		t.Errorf("wave 1 should be [c2], got %+v", groups[1])
	}
	if len(groups[2]) != 1 || groups[2][0].ID != "c3" {
		t.Errorf("wave 2 should be [c3], got %+v", groups[2])
	}
}

func TestToolDependencyGraph_Cycle(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("A", "file_read", `{}`))
	graph.AddCall(makeToolCall("B", "file_read", `{}`))
	// A depends on B, B depends on A — cycle.
	graph.AddDependency("A", "B")
	graph.AddDependency("B", "A")

	groups := graph.IndependentGroups()

	// Should not infinite loop. The cycle nodes should appear as a fallback
	// group. Total groups: 0 normal waves (neither has in-degree 0) + 1
	// fallback = 1 group containing both A and B.
	if len(groups) != 1 {
		t.Fatalf("expected 1 fallback group for cycle, got %d groups", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Errorf("fallback group should contain both cycle nodes, got %d", len(groups[0]))
	}
}

func TestToolDependencyGraph_CycleWithPrecedingIndependent(t *testing.T) {
	// call_X is independent; A and B form a cycle.
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("X", "file_read", `{}`))
	graph.AddCall(makeToolCall("A", "file_read", `{}`))
	graph.AddCall(makeToolCall("B", "file_read", `{}`))
	graph.AddDependency("A", "B")
	graph.AddDependency("B", "A")

	groups := graph.IndependentGroups()

	// Expect 2 groups: [X] then fallback [A, B].
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (1 normal + 1 fallback), got %d", len(groups))
	}
	if len(groups[0]) != 1 || groups[0][0].ID != "X" {
		t.Errorf("wave 0 should be [X], got %+v", groups[0])
	}
	if len(groups[1]) != 2 {
		t.Errorf("fallback group should contain A and B, got %d", len(groups[1]))
	}
}

func TestToolDependencyGraph_GetDependencies(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("A", "file_read", `{}`))
	graph.AddCall(makeToolCall("B", "file_read", `{}`))
	graph.AddCall(makeToolCall("C", "file_read", `{}`))
	graph.AddDependency("B", "A")
	graph.AddDependency("B", "C")

	deps := graph.GetDependencies("B")
	if len(deps) != 2 {
		t.Fatalf("expected B to have 2 dependencies, got %d: %v", len(deps), deps)
	}
	if !sliceContains(deps, "A") || !sliceContains(deps, "C") {
		t.Errorf("expected B to depend on A and C, got %v", deps)
	}

	// A has no dependencies.
	aDeps := graph.GetDependencies("A")
	if len(aDeps) != 0 {
		t.Errorf("A should have no dependencies, got %v", aDeps)
	}

	// Unknown ID returns nil.
	unknownDeps := graph.GetDependencies("nonexistent")
	if len(unknownDeps) != 0 {
		t.Errorf("unknown ID should have nil/empty deps, got %v", unknownDeps)
	}
}

func TestToolDependencyGraph_Dependents(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("A", "file_read", `{}`))
	graph.AddCall(makeToolCall("B", "file_read", `{}`))
	graph.AddCall(makeToolCall("C", "file_read", `{}`))
	graph.AddDependency("B", "A")
	graph.AddDependency("C", "A")

	dependents := graph.Dependents("A")
	if len(dependents) != 2 {
		t.Fatalf("expected A to have 2 dependents, got %d: %v", len(dependents), dependents)
	}
	if !sliceContains(dependents, "B") || !sliceContains(dependents, "C") {
		t.Errorf("expected A's dependents to include B and C, got %v", dependents)
	}

	// B has no dependents.
	bDependents := graph.Dependents("B")
	if len(bDependents) != 0 {
		t.Errorf("B should have no dependents, got %v", bDependents)
	}
}

func TestToolDependencyGraph_AddDependencyIdempotent(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("A", "file_read", `{}`))
	graph.AddCall(makeToolCall("B", "file_read", `{}`))

	graph.AddDependency("B", "A")
	graph.AddDependency("B", "A") // duplicate
	graph.AddDependency("B", "A") // duplicate

	deps := graph.GetDependencies("B")
	if len(deps) != 1 {
		t.Errorf("expected duplicate AddDependency to be deduplicated, got %d deps: %v", len(deps), deps)
	}
}

func TestToolDependencyGraph_SelfDependencyRejected(t *testing.T) {
	graph := NewToolDependencyGraph()
	graph.AddCall(makeToolCall("A", "file_read", `{}`))

	graph.AddDependency("A", "A")

	deps := graph.GetDependencies("A")
	if len(deps) != 0 {
		t.Errorf("self-dependency should be rejected, got %v", deps)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func callIDsFromGroup(group []llm.ToolCall) []string {
	ids := make([]string, len(group))
	for i, c := range group {
		ids[i] = c.ID
	}
	return ids
}

func sliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
