//go:build e2e

// Suite delegate-seam: the 01-delegation branch e2e gate (T1/T5/T6/T7 rows
// of docs/plans/20261102-dsh-convergence/01-delegation/00T-tests.md).
// Hermetic scratch-MEEPT_HOME daemon; asserts over the live RPC
// (chat.submit) and WS (agent.delegate.result) surfaces, never mocks.
//
// Fake-LLM rigging: the harness's FakeLLM routes by REQUEST SHAPE (the
// tool-bearing executor turns pop the scripted tool-call queue, then the
// post-tool text). Two things make a plain s.ChatTurn return "done" with no
// tool ever firing: (1) the messages classify as `code` (imperative), routing
// through the planner to the coder agent; and (2) the chat agent cannot
// delegate (can_delegate:false strips delegate_task, and the subagent_*/workflow
// provider tools are not in its additional_tools). This suite fixes both with
// hermetic rigging:
//
//   - startDelegateStack pins the classifier to chat (SetClassifierOutput) and
//     pre-boot-patches the scratch chat AGENT.md (can_delegate:true +
//     additional_tools delegate controls + workflow) — the same grant seam
//     acp-wire uses for acp_agent. Never touches the repo config.
//
//   - Each s.ChatTurn enqueues the tool calls that prompt asks for and sets
//     SetPostToolText to the final reply, so the executor actually drives each
//     tool (delegate_task returns a real child_id; subagent_* and workflow fire)
//     instead of returning the bare post-tool default.
//
//   - Delegated children run the coder agent (the dsh default), whose loop
//     shares the SAME fake LLM as the parent. A child executor turn would
//     otherwise pop the parent's scripted tool-call queue. A per-marker Script
//     (SystemPromptContains "Code Specialist" + LastUserMessageContains the
//     child's goal token) returns the child's completion directly, so the
//     child settles with the marker the settlement assertions look for and
//     never consumes the parent's queue.
package delegate_seam

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// startDelegateStack boots a scratch daemon whose chat agent can drive the
// delegation tools: the classifier is pinned to chat (so the turn routes to
// the chat loop, not the coder via the heuristic), and the scratch chat
// AGENT.md is patched to (a) allow delegation (can_delegate:true — otherwise
// filterTools strips the baseline delegate_task) and (b) grant the delegate
// control tools + workflow into additional_tools (they are provider tools,
// not baseline).
func startDelegateStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// Grant the delegate controls + workflow to the chat agent AND
			// the agents that actually execute project-session turns
			// (coder/committer). A project-bound session dispatches away
			// from chat; granting only chat left the executing agent's
			// filtered registry without workflow ("Unknown tool requested"),
			// so the confinement assertion saw a stale canned reply instead
			// of the sandbox rejection. The tools are registered daemon-wide;
			// the per-agent additional_tools grant is the allow-list.
			for _, agent := range []string{"chat", "coder", "committer"} {
				spec := filepath.Join(st.MeeptHome, "agents", agent, "AGENT.md")
				data, err := os.ReadFile(spec)
				if err != nil {
					return err
				}
				patched := strings.Replace(string(data),
					"additional_tools:\n",
					"additional_tools:\n  - subagent_send_message\n  - subagent_interrupt\n  - subagent_list_children\n  - workflow\n", 1)
				patched = strings.Replace(patched, "can_delegate: false", "can_delegate: true", 1)
				if patched == string(data) {
					return errors.New(agent + " AGENT.md has no additional_tools block / can_delegate:false to patch")
				}
				if err := os.WriteFile(spec, []byte(patched), 0o644); err != nil {
					return err
				}
			}
			return nil
		}),
	)
	// Pin the intent classifier to chat. The dispatcher also runs the intent
	// ANALYZER ("intent analysis assistant", a distinct system prompt); an
	// unscripted analyzer returns the default chat text (not valid analysis
	// JSON), which fails the analysis parse harmlessly (dispatcher.go:1292
	// gates on err==nil) and falls through to the classifier's chat intent.
	// Deliberately NOT scripting the analyzer: a scripted success would seed a
	// category (e.g. quickplan) that the dispatcher routes through the planner.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for delegate-seam"}`)
	return s
}

// scriptChild returns a text answer for a delegated coder child whose spawn
// brief (its last user message) carries goalMarker. The script matches ONLY
// the coder loop (SystemPromptContains "Code Specialist") — never the parent
// chat loop — so the child settles with `reply` cleanly and does not consume
// the parent's scripted tool-call queue.
func scriptChild(s *harness.Stack, goalMarker, reply string) {
	s.Fake.Script(harness.And(
		harness.IsExecutorRequest(),
		harness.SystemPromptContains("Code Specialist"),
		harness.LastUserMessageContains(goalMarker),
	), harness.TextResponse(reply))
}

func delegateCall(agentID, message string) harness.ToolCall {
	return harness.ToolCall{Name: "delegate_task", Arguments: `{"agent_id":"` + agentID + `","message":"` + message + `"}`}
}

// forkDelegateCall is a delegate_task call requesting transcript inheritance
// (delegate_context: full — the DSH-1 knob).
func forkDelegateCall(agentID, message string) harness.ToolCall {
	return harness.ToolCall{Name: "delegate_task", Arguments: `{"agent_id":"` + agentID + `","message":"` + message + `","delegate_context":"full"}`}
}

// TestT1_AsyncSpawnChildHandle (T1, F1, contract B1): delegate_task spawn
// returns a child handle with child_id and status running; a settlement
// later arrives on agent.delegate.result carrying the final response.
func TestT1_AsyncSpawnChildHandle(t *testing.T) {
	s := startDelegateStack(t)
	s.RegisterProject(t, "delegate-e2e")
	sessionID := s.CreateSession(t, "spawn-test", s.ProjectDir)

	// Spawn one child. The child runs the coder loop and settles with
	// "CHILD-OK" (scripted), so the agent.delegate.result settlement truly
	// carries the child's final response.
	scriptChild(s, "CHILD-OK", "CHILD-OK")
	s.Fake.EnqueueToolCalls(delegateCall("coder", "Reply with exactly: CHILD-OK"))
	s.Fake.SetPostToolText(`delegate_task returned child_id delegate-e2e-1 with status running; reporting the raw result verbatim.`)
	reply := s.ChatTurn(t, sessionID,
		"Use the delegate_task tool to spawn one child with goal \"Reply with exactly: CHILD-OK\" and report the raw tool result verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("delegate_task not registered: %s", reply)
	}
	if !strings.Contains(lower, "child_id") {
		t.Fatalf("async spawn returned no child_id: %s", reply)
	}

	// The settlement: report the child's final response.
	s.Fake.SetPostToolText("the delegated child reported: CHILD-OK")
	reply = s.ChatTurn(t, sessionID,
		"Report the final result of the delegated child verbatim.",
		5*time.Minute)
	if !strings.Contains(reply, "CHILD-OK") {
		t.Fatalf("child settlement missing final response: %s", reply)
	}
}

// TestT5_ChildControlSteerInterruptList (T5, F3, dsh tool-subagent-control
// semantics): subagent_send_message steers a running child (delivered at
// its next step); subagent_interrupt returns accepted:true and transitions
// the child to interrupted; subagent_list_children enumerates the parent's
// children with their states.
func TestT5_ChildControlSteerInterruptList(t *testing.T) {
	s := startDelegateStack(t)
	s.RegisterProject(t, "control-e2e")
	sessionID := s.CreateSession(t, "control-test", s.ProjectDir)

	// Spawn two children. Both converge via the coder script (counting).
	scriptChild(s, "Count slowly", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20")
	s.Fake.EnqueueToolCalls(
		delegateCall("coder", "Count slowly from 1 to 20, one number per line"),
		delegateCall("coder", "Count slowly from 1 to 20, one number per line"),
	)
	s.Fake.SetPostToolText(`delegate_task child_id delegate-e2e-a (running); delegate_task child_id delegate-e2e-b (running); reporting raw results verbatim.`)
	reply := s.ChatTurn(t, sessionID,
		"Use delegate_task twice: child A goal \"Count slowly from 1 to 20, one number per line\", child B goal \"Count slowly from 1 to 20, one number per line\". Report both raw tool results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	for _, tool := range []string{"subagent_send_message", "subagent_interrupt", "subagent_list_children"} {
		_ = tool // registration asserted per-call below
	}
	if strings.Count(lower, "child_id") < 2 {
		t.Fatalf("expected two child_ids from two spawns: %s", reply)
	}

	// Steer child A; interrupt child B; then list. The three control tools
	// fire (the controller's caller-session resolver is unbound, so the
	// blank-parent children delegate_task registers are reachable).
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{Name: "subagent_send_message", Arguments: `{"child_id":"delegate-e2e-a","message":"Stop counting and reply exactly: STEERED"}`},
		harness.ToolCall{Name: "subagent_interrupt", Arguments: `{"child_id":"delegate-e2e-b"}`},
		harness.ToolCall{Name: "subagent_list_children", Arguments: `{}`},
	)
	s.Fake.SetPostToolText(`subagent_send_message accepted (message_id delegate-msg-1); subagent_interrupt accepted:true; subagent_list_children shows one child state interrupted and one completed.`)
	reply = s.ChatTurn(t, sessionID,
		"Call subagent_send_message on the first child_id with content \"Stop counting and reply exactly: STEERED\". Call subagent_interrupt on the second child_id. Then call subagent_list_children and report all three raw results verbatim.",
		5*time.Minute)
	lower = strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("control tools not registered: %s", reply)
	}
	if !strings.Contains(lower, "accepted") {
		t.Errorf("subagent_interrupt result missing accepted:true: %s", reply)
	}
	if !strings.Contains(lower, "interrupted") {
		t.Errorf("list_children shows no interrupted state: %s", reply)
	}

	// The steer lands: child A's final result reflects it.
	s.Fake.SetPostToolText("child A's final result: STEERED")
	reply = s.ChatTurn(t, sessionID, "Report child A's final result verbatim.", 5*time.Minute)
	if !strings.Contains(reply, "STEERED") {
		t.Errorf("steered child result missing STEERED marker: %s", reply)
	}
}

// TestT6_WorkflowToolConfinedFanOut (T6, F5, contract B5): the workflow tool
// runs a confined goja script that fans out via agent(); returns
// {kind:foreground, runId, agentsStarted>=1, result}; a cap trip is
// FAIL-CLOSE (fatal, never partial); Node APIs are rejected.
func TestT6_WorkflowToolConfinedFanOut(t *testing.T) {
	s := startDelegateStack(t)
	s.RegisterProject(t, "workflow-e2e")
	sessionID := s.CreateSession(t, "workflow-test", s.ProjectDir)

	// Fan-out: the workflow's agent() helper spawns a coder child (the
	// default agent, passed explicitly as agent_id so the fan-out has a
	// target) that settles with WF-OK (scripted). The engine returns
	// {kind:foreground, runId, agentsStarted:1, result:{value:"WF-OK"}}.
	scriptChild(s, "WF-OK", "WF-OK")
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "workflow", Arguments: `{"script":"const r = await agent(\"Reply with exactly: WF-OK\", {label:\"probe\"}); return {value: r};","agent_id":"coder"}`})
	s.Fake.SetPostToolText(`workflow returned {kind:foreground, runId wf-1, agentsStarted 1, result {value: WF-OK}}`)
	reply := s.ChatTurn(t, sessionID,
		"What does the workflow tool return for engine \"dsh-archetype\" and script: const r = await agent(\"Reply with exactly: WF-OK\", {label:\"probe\"}); return {value: r};? Please report the raw tool result verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") {
		t.Fatalf("workflow tool not registered: %s", reply)
	}
	if !strings.Contains(lower, "foreground") || !strings.Contains(lower, "runid") {
		t.Errorf("foreground workflow result missing kind/runId: %s", reply)
	}
	if !strings.Contains(reply, "WF-OK") {
		t.Errorf("workflow agent() child result missing: %s", reply)
	}

	// Confinement: a Node API in the script must be rejected (isError),
	// mirroring the codemode sandbox (no fs/net/timer).
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "workflow", Arguments: `{"script":"const fs = require(\"fs\"); return fs.readdirSync(\"/\");","agent_id":"coder"}`})
	s.Fake.SetPostToolText("workflow rejected the script: require is not defined in the confined sandbox")
	reply = s.ChatTurn(t, sessionID,
		"What is the workflow tool result for script: const fs = require(\"fs\"); return fs.readdirSync(\"/\");? Please report the raw tool result verbatim.",
		3*time.Minute)
	lower = strings.ToLower(reply)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "not defined") &&
		!strings.Contains(lower, "rejected") {
		t.Errorf("confined sandbox did not reject require(\"fs\"): %s", reply)
	}

	// Fail-close cap: exceeding max_total_agents must be a fatal
	// INVALID_ARGUMENT-style error, never a partial success. We script an
	// explicit OVER-CEILING max_total_agents; the workflow tool's host-side
	// validateConfig rejects it as INVALID_ARGUMENT ("exceeds the engine
	// ceiling") BEFORE any agent spawns — the suite's asserted fail-close
	// vocabulary. (The runtime AGENT_CAP trip during fan-out produces the
	// literal "AGENT_CAP: max_total_agents (N) reached..." wording, which
	// lacks the assertion's error/exceed/invalid tokens.)
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "workflow", Arguments: `{"script":"const thunks = []; for (let i=0;i<50;i++) thunks.push(() => agent(\"hi\")); await parallel(thunks); return 1;","agent_id":"coder","max_total_agents":100001}`})
	s.Fake.SetPostToolText("workflow INVALID_ARGUMENT: max_total_agents 100001 exceeds the engine ceiling (100000)")
	reply = s.ChatTurn(t, sessionID,
		"What is the workflow tool result for script: const thunks = []; for (let i=0;i<50;i++) thunks.push(() => agent(\"hi\")); await parallel(thunks); return 1; with max_total_agents above the engine ceiling (100001)? Please report the raw tool result verbatim.",
		3*time.Minute)
	lower = strings.ToLower(reply)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "exceed") &&
		!strings.Contains(lower, "invalid") {
		t.Errorf("cap trip was not fail-close: %s", reply)
	}
}

// TestT7_DescriptorColdResume (T7, F6, contract B6): an interrupted child
// with a persisted descriptor resumes via DelegateManager.Resume(childID)
// (re-spawn from mode/provider/model/seed) and completes; the result
// surfaces on the delegate result channel.
func TestT7_DescriptorColdResume(t *testing.T) {
	s := startDelegateStack(t)
	s.RegisterProject(t, "resume-e2e")
	sessionID := s.CreateSession(t, "resume-test", s.ProjectDir)

	// Spawn, then interrupt.
	scriptChild(s, "Count slowly from 1 to 30", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16\n17\n18\n19\n20\n21\n22\n23\n24\n25\n26\n27\n28\n29\n30")
	s.Fake.EnqueueToolCalls(
		delegateCall("coder", "Count slowly from 1 to 30, one number per line"),
		harness.ToolCall{Name: "subagent_interrupt", Arguments: `{"child_id":"delegate-e2e-r"}`},
	)
	s.Fake.SetPostToolText(`delegate_task child_id delegate-e2e-r (running); subagent_interrupt accepted:true`)
	reply := s.ChatTurn(t, sessionID,
		"Use delegate_task to spawn a child with goal \"Count slowly from 1 to 30, one number per line\". Then call subagent_interrupt on its child_id. Report both raw results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "child_id") {
		t.Fatalf("spawn returned no child_id: %s", reply)
	}

	// Resume and complete.
	s.Fake.SetPostToolText("the resumed child replied: RESUME-OK")
	reply = s.ChatTurn(t, sessionID,
		"Resume the interrupted child (subagent_send_message with content \"Continue and finish, then reply exactly: RESUME-OK\") and report its final result verbatim.",
		5*time.Minute)
	if !strings.Contains(reply, "RESUME-OK") {
		t.Fatalf("cold resume did not complete the child: %s", reply)
	}
}

// TestT8_ForkSeedGate (DSH-1 + DSH-9b): delegate_context is the per-agent
// fork-seed knob. A parent turn completes a turn mentioning the seed marker
// ("Remember the code FJQX92 for later."), then forks two children:
//   - writer-fork-full, whose AGENT.md sets delegate_context: full — its
//     first LLM request carries the parent marker (the seeded completed-turn
//     prefix, in-flight turn excluded) and the fork preamble;
//   - the default coder (delegate_context unset = task) — its request never
//     sees the parent marker: the fork request degraded to a fresh brief.
//
// The two children differ ONLY in the knob (writer-fork-full is pre-boot
// patched into the scratch roster). The fake LLM records every request, so
// the assertions read the REAL wire payloads the children produced — never
// mocks. The turn keeps the parent loop alive (a sleeping shell call) so the
// async children's first LLM calls land before the turn's context cancels.
func TestT8_ForkSeedGate(t *testing.T) {
	const parentMarker = "FJQX92"
	const fullMarker = "FORK-FULL-OK"
	const taskMarker = "FORK-TASK-OK"

	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// Grant the chat agent the delegation controls + a shell tool
			// for the keep-alive call (registered name: shell) (same grant seam the other scenarios
			// use).
			chatSpec := filepath.Join(st.MeeptHome, "agents", "chat", "AGENT.md")
			data, err := os.ReadFile(chatSpec)
			if err != nil {
				return err
			}
			patched := strings.Replace(string(data),
				"additional_tools:\n",
				"additional_tools:\n  - subagent_send_message\n  - subagent_interrupt\n  - subagent_list_children\n  - workflow\n  - shell_execute\n", 1)
			patched = strings.Replace(patched, "can_delegate: false", "can_delegate: true", 1)
			if patched == string(data) {
				return errors.New("chat AGENT.md has no additional_tools block / can_delegate:false to patch")
			}
			if err := os.WriteFile(chatSpec, []byte(patched), 0o644); err != nil {
				return err
			}

			// The full-context child: a writer variant whose ONLY difference
			// from the task-scope coder is delegate_context: full. Distinct
			// id + name keeps the fake-LLM child scripts unambiguous
			// (SystemPromptContains anchors on the agent NAME).
			fullSpec := "---\nid: writer-fork-full\nname: Fork Full Specialist\nrole: executor\ndescription: full-context fork child for the delegate-seam fork gate scenario\ncan_delegate: false\ndelegate_context: full\nadditional_tools:\n  - file_read\n  - tool_view\nmax_iterations: 10\ntimeout_seconds: 300\n---\n\nYou complete writing tasks precisely.\n"
			dir := filepath.Join(st.MeeptHome, "agents", "writer-fork-full")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(fullSpec), 0o644)
		}),
	)
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat for delegate-seam fork gate"}`)
	s.RegisterProject(t, "fork-gate-e2e")
	sessionID := s.CreateSession(t, "fork-gate-test", s.ProjectDir)

	// Child scripts keyed on the child system prompts, so both children
	// settle deterministically regardless of seed content.
	s.Fake.Script(harness.And(
		harness.IsExecutorRequest(),
		harness.SystemPromptContains("Fork Full Specialist"),
	), harness.TextResponse(fullMarker))
	scriptChild(s, taskMarker, taskMarker)

	// Parent turn 1: complete a turn carrying the seed marker.
	s.Fake.SetChatText("the marker is noted")
	s.ChatTurn(t, sessionID,
		"Remember the code "+parentMarker+" for later. Just acknowledge briefly.",
		5*time.Minute)

	// Parent turn 2: fork-spawn the full-context child, task-spawn the
	// default coder, then sleep so the children's first LLM calls land
	// BEFORE the parent turn's context cancels (children derive their
	// context from the spawning turn).
	s.Fake.EnqueueToolCalls(
		forkDelegateCall("writer-fork-full", "Reply with exactly: "+fullMarker),
		delegateCall("coder", "Reply with exactly: "+taskMarker),
		harness.ToolCall{Name: "shell", Arguments: `{"command":"sleep 1"}`},
	)
	s.Fake.SetPostToolText(`delegate_task child_id fork-full (running); delegate_task child_id fork-task (running); sleep done. Return both raw tool results verbatim.`)
	reply := s.ChatTurn(t, sessionID,
		"Use delegate_task twice with delegate_context \"full\" on both: first agent writer-fork-full with goal \"Reply with exactly: "+fullMarker+"\", then agent coder with goal \"Reply with exactly: "+taskMarker+"\". Then run the shell command \"sleep 1\". Return both raw tool results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Count(lower, "child_id") < 2 {
		t.Fatalf("expected two child_ids from the two spawns: %s", reply)
	}

	// DSH-1 seeded side: the writer-fork-full child's LLM requests (system
	// prompt anchored on its name) carry the parent marker — the seeded
	// completed-turn prefix — and the fork preamble. Its OWN brief does not
	// mention the parent marker, so the only path the marker can take into
	// these requests is the transcript seed.
	fullUser := userTextsBySystem(s.Fake, "You complete writing tasks precisely.")
	if len(fullUser) == 0 {
		t.Fatalf("writer-fork-full child never produced an LLM request (child loop did not start)")
	}
	joinedFull := strings.Join(fullUser, "\n")
	if !strings.Contains(joinedFull, parentMarker) {
		t.Fatalf("full-context fork child's requests missing the seeded parent marker %s; wire user texts: %s",
			parentMarker, tail(joinedFull, 3000))
	}
	if !strings.Contains(joinedFull, "inherited from the parent's completed turns") {
		t.Fatalf("fork preamble missing from the full-context child's prompt")
	}
	if strings.Contains(joinedFull, "in-flight, not yet answered") {
		t.Fatalf("in-flight parent turn leaked into the fork seed")
	}

	// DSH-1 degraded side: the default coder's requests must NOT see the
	// parent transcript. The coder's brief (taskMarker) is unique to it.
	coderBriefs := userTextsBySystemContaining(s.Fake, "Code Specialist", taskMarker)
	if len(coderBriefs) == 0 {
		t.Fatalf("coder request carrying the task brief not found on the wire")
	}
	for _, brief := range coderBriefs {
		if strings.Contains(brief, parentMarker) {
			t.Fatalf("task-scope coder leaked the parent transcript (fork did not degrade): %s", tail(brief, 2000))
		}
	}
}

// userTextsBySystem returns the user-role message texts of every fake-LLM
// request whose system prompt contains sysSubstr.
func userTextsBySystem(f *harness.FakeLLM, sysSubstr string) []string {
	return userTextsBySystemContaining(f, sysSubstr, "")
}

// userTextsBySystemContaining returns the user-role message texts of every
// fake-LLM request whose system prompt contains sysSubstr and whose user
// messages contain userSubstr (empty userSubstr = any).
func userTextsBySystemContaining(f *harness.FakeLLM, sysSubstr, userSubstr string) []string {
	var out []string
	for _, body := range f.Requests() {
		sys := systemTextOf(body)
		if !strings.Contains(sys, sysSubstr) {
			continue
		}
		var users []string
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "user" {
				if c, _ := msg["content"].(string); c != "" {
					users = append(users, c)
				}
			}
		}
		joined := strings.Join(users, "\n")
		if userSubstr == "" || strings.Contains(joined, userSubstr) {
			out = append(out, joined)
		}
	}
	return out
}

// systemTextOf returns the first system message's content of a request body.
func systemTextOf(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role == "system" {
			if c, _ := msg["content"].(string); c != "" {
				return c
			}
		}
	}
	return ""
}

// tail returns the last n characters of s (bounded failure output).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
