package agent

// executor_m3_commandscan_pins_test.go — bughunt 2026-11-27 F2, M3
// (command-scan over-block) partial-mitigation pins.
//
// Audit S-5: the fence's CheckCommand classifies any token containing ".."
// as path-like (internal/security/fence.go extractPathFromToken) and Abss
// RELATIVE tokens against the daemon process cwd, so `git log
// --format=%h..%s`, `echo a..b` and `'X=Y..Z'` were denied whenever the
// daemon cwd sat outside the session fence.
//
// The H2 fix (executor injects the turn dir into details["working_dir"],
// pinned in executor_fence_shell_pins_test.go) also fixes the RELATIVE
// half of M3: CheckCommand now receives the turn's session dir as workDir,
// so relative path-like tokens resolve INSIDE the jail and pass. These
// pins lock that posture on the real production fence
// (composeLoopFence -> FenceChecker.CheckCommand).
//
// STILL OPEN (blocked by file ownership — internal/security/fence.go is
// outside this lane's scope): `2>/dev/null` and other absolute-target
// redirects remain denied ("/dev/null" is outside every jail). The exact
// wiring needed is recorded in
// .hermes/audits/2026-11-27-fixwave/F2-fence-shell.md.

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// TestExecutor_CommandScanM3RelativeShapesPassInJail: the audit's three
// over-block shapes with relative ".." tokens must PASS through the real
// fence once the working_dir the command runs in is the session jail —
// pre-H2 the tokens Abs'd against the daemon cwd and the whole command was
// refused.
func TestExecutor_CommandScanM3RelativeShapesPassInJail(t *testing.T) {
	e := f2PinnedExecutor(t, "shell")
	jail := f2JailDir(t)
	ctx := tools.ContextWithWorkingDir(context.Background(), jail)

	shape := func(id, cmd string) {
		t.Run(cmd, func(t *testing.T) {
			res := e.Execute(ctx, llm.ToolCall{
				ID: id, Type: "function",
				Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command": "` + cmd + `"}`},
			})
			if res.Success {
				return
			}
			t.Fatalf("command over-blocked by the fence scan: %v", res.Error)
		})
	}

	shape("m3_fmt", "git log --format=%h..%s -5")
	shape("m3_dots", "echo a..b")
	shape("m3_env", "env X=Y..Z printenv X")
}

// TestExecutor_CommandScanTraversalStillDenied: the guard must NOT weaken —
// a genuine traversal token that escapes the jail is still refused. This is
// the non-vacuity control for the pins above: they prove relative ".."
// tokens are judged in path position, not blanket-allowed.
func TestExecutor_CommandScanTraversalStillDenied(t *testing.T) {
	e := f2PinnedExecutor(t, "shell")
	jail := f2JailDir(t)

	res := e.Execute(tools.ContextWithWorkingDir(context.Background(), jail), llm.ToolCall{
		ID: "m3_trav", Type: "function",
		Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command": "cat ../../../../etc/passwd"}`},
	})
	if res.Success {
		t.Fatalf("traversal command allowed — the M3 mitigation must not weaken the traversal guard")
	}
}
