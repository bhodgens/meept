//go:build e2e

// Package memoryscoping is the RED e2e test gate for memory scoping
// (docs/plans/20261030-memory-second-brain, leaf 00T; gates branch 01).
//
// Contracts exercised (frozen in master.md):
//
//	C1  Scope enum stamped per memory (project/user/global)
//	C2  WithScopeFilter(maxScope, projectID, userID) recall predicate —
//	    a row is recallable iff global, or user AND userID matches
//	    (empty userID matches ONLY when multiuser is disabled), or
//	    project AND projectID matches. Flag 3: (multiuser=true,
//	    userID="") fails CLOSED — user-scope rows stay hidden.
//	C3  memory_max_scope agent declaration ("" -> project); a
//	    chief-of-staff agent with "global" recalls across projects
//
// Scenarios:
//
//	memory-scoping-01  project isolation + global cross-project recall
//	memory-scoping-02  chief-of-staff (memory_max_scope=global) spans
//	                   projects; the default (project) agent does not
//	memory-scoping-03  Flag 3 fail-closed cell: multiuser enabled +
//	                   unresolvable userID hides user-scope rows
//
// Every assertion fails RED for a missing-implementation reason: the
// scope vocabulary, the WithScopeFilter seam, and the memory_max_scope
// agent field do not exist yet, so scoping behavior is unobservable and
// the asserted outcomes cannot hold.
package memoryscoping

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// Distinct fact payloads (each carries a unique token so a cross-project
// leak is unambiguous).
const (
	projectAFact  = "the zkarev launch pin is 7-dot Pine 992"
	globalFact    = "the shared ops maxim is measure twice cut once"
	userScopeFact = "the alice preference is window seat aisle 3"
	recallTopic   = "launch pin"
)

// scopedStack boots the hermetic stack with the scope surface armed (the
// same overlay branch 01's implementation leaves will flip defaults for;
// today the keys are accepted by the loader and defaulted, so the boot
// stays healthy and the RED is behavioral).
func scopedStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	return s
}

// pinChat pins the classifier to the chat lane and scripts the reply.
func pinChat(s *harness.Stack, reply string) {
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	if reply != "" {
		s.Fake.SetChatText(reply)
	}
}

// promptTextsFrom joins the system+user message contents of the completion
// requests captured from index `since` onward — the surfaces where recalled
// memories render.
func promptTextsFrom(f *harness.FakeLLM, since int) string {
	var b strings.Builder
	reqs := f.Requests()
	for i := since; i < len(reqs); i++ {
		msgs, _ := reqs[i]["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			if role != "system" && role != "user" {
				continue
			}
			c, _ := msg["content"].(string)
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// storeViaTool drives a memory_store turn in the given session and waits
// for the execution observable in the daemon log (the terminating tool's
// JSON envelope is reply-guard-rewritten before it becomes the reply, so
// the reply is not a reliable landed signal).
func storeViaTool(t *testing.T, s *harness.Stack, sessionID, content, scope string) {
	t.Helper()
	args := `{"content":"` + content + `","type":"task","category":"general"`
	if scope != "" {
		// C1 explicit scope argument (post-impl); pre-impl it is ignored.
		args += `,"scope":"` + scope + `"`
	}
	args += `}`
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_store",
		Arguments: args,
	})
	s.ChatTurn(t, sessionID, "Please remember that "+content, 120*time.Second)
	harness.WaitFor(t, 30*time.Second, "the memory_store execution for "+content, func() bool {
		tail := s.Daemon.LogTail()
		return strings.Contains(tail, "tool=memory_store") &&
			strings.Contains(tail, firstToken(content))
	})
}

// firstToken returns a distinctive substring of the fact payload for
// log-line matching (the token after "is").
func firstToken(content string) string {
	for _, f := range strings.Fields(content) {
		switch f {
		case "is", "the", "a", "an":
			continue
		default:
			return f
		}
	}
	return content
}

// recallTurn runs one recall turn in sessionID and returns the prompt text
// the model saw (system + user message contents).
func recallTurn(t *testing.T, s *harness.Stack, sessionID, query string) string {
	t.Helper()
	before := s.Fake.RequestCount()
	pinChat(s, "answering from memory.")
	s.ChatTurn(t, sessionID, query, 120*time.Second)
	return promptTextsFrom(s.Fake, before)
}

// ---------------------------------------------------------------------------
// memory-scoping-01 (T3/F2): project isolation + global cross-project recall
// ---------------------------------------------------------------------------

// TestProjectIsolationWithGlobalRecall plants a project-scoped fact in a
// project-A session, a global fact in the same session, then recalls from
// a project-B session: the A-fact must be ABSENT and the global fact
// PRESENT (C1/C2).
//
// RED today: no scope stamping and no WithScopeFilter recall filter exist,
// so project-B recall leaks the A-fact (recall is global today) AND the
// suite cannot distinguish the outcomes — the A-fact-present branch is
// the observed RED.
func TestProjectIsolationWithGlobalRecall(t *testing.T) {
	s := scopedStack(t)

	// Project A: the registered project directory session.
	sessionA := s.CreateSession(t, "scope-a", s.ProjectDir)
	storeViaTool(t, s, sessionA, projectAFact, "project")
	storeViaTool(t, s, sessionA, globalFact, "global")

	// Project B: a second registered project with its own session.
	projectB := filepath.Join(s.Work, "project-b")
	if err := os.MkdirAll(projectB, 0o755); err != nil {
		t.Fatalf("mkdir project B: %v", err)
	}
	s.RunCLI(t, 30*time.Second, false, "projects", "add", projectB, "--name", "e2e-project-b")
	sessionB := s.CreateSession(t, "scope-b", projectB)

	prompt := recallTurn(t, s, sessionB, "what do you know about the "+recallTopic+" and the ops maxim?")

	if strings.Contains(prompt, "7-dot Pine 992") {
		t.Fatalf("F2 RED: the project-A fact leaked into the project-B session "+
			"recall (scope stamping + the C2 WithScopeFilter project predicate "+
			"are not implemented — recall is unscoped today).\nprompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "measure twice cut once") {
		t.Fatalf("F2 RED: the global-scope fact was NOT recalled in the "+
			"project-B session (C2 requires scope==global rows to stay "+
			"recallable everywhere).\nprompt:\n%s", prompt)
	}
}

// ---------------------------------------------------------------------------
// memory-scoping-02 (T4/F3): chief-of-staff memory_max_scope=global
// ---------------------------------------------------------------------------

// TestChiefOfStaffGlobalScopeRecallsAcrossProjects seeds two agents — one
// with memory_max_scope=global (chief-of-staff) and one default (project)
// — both served by sessions in project B, then requires: the global-max
// agent's recall carries the project-A fact while the default agent's does
// not (C3).
//
// RED today: no agent definition carries a memory_max_scope field
// (internal/agents/models.go has no such key), so both agents behave
// identically and the required asymmetry cannot hold.
func TestChiefOfStaffGlobalScopeRecallsAcrossProjects(t *testing.T) {
	s := scopedStack(t)

	// Two agents over the same fixture: chief (global max scope) and
	// line（default project max scope). The memory_max_scope key is the
	// C3 declaration surface; today the loader drops it silently.
	seedAgent(t, s, "aachef", "global")
	seedAgent(t, s, "aaline", "")

	sessionA := s.CreateSession(t, "cos-a", s.ProjectDir)
	storeViaTool(t, s, sessionA, projectAFact, "project")

	projectB := filepath.Join(s.Work, "project-b")
	if err := os.MkdirAll(projectB, 0o755); err != nil {
		t.Fatalf("mkdir project B: %v", err)
	}
	s.RunCLI(t, 30*time.Second, false, "projects", "add", projectB, "--name", "e2e-project-b")
	sessionChief := s.CreateSession(t, "cos-chief", projectB)
	sessionLine := s.CreateSession(t, "cos-line", projectB)

	// Route each recall turn to its agent via the classifier pin: the
	// lane index attributes intents to agents in sorted-ID order, so
	// distinct pinned intents isolate the two agents deterministically.
	chiefPrompt := recallTurnAs(t, s, sessionChief, "aachef",
		"chief: what is the "+recallTopic+"?")
	linePrompt := recallTurnAs(t, s, sessionLine, "aaline",
		"line: what is the "+recallTopic+"?")

	if !strings.Contains(chiefPrompt, "7-dot Pine 992") {
		t.Fatalf("F3 RED: the memory_max_scope=global chief-of-staff agent did "+
			"NOT recall the project-A fact (C3 global-max recall is not "+
			"implemented).\nprompt:\n%s", chiefPrompt)
	}
	if strings.Contains(linePrompt, "7-dot Pine 992") {
		t.Fatalf("F3 RED: the default (project-max) agent recalled the project-A "+
			"fact — the C3 project default is not enforced.\nprompt:\n%s", linePrompt)
	}
}

// recallTurnAs routes the recall turn to the named agent by pinning the
// classifier to an intent lane attributed to that agent. With both agents
// declaring distinct intent lanes, the lane index (sorted-ID first-wins)
// resolves each lane to exactly one agent.
func recallTurnAs(t *testing.T, s *harness.Stack, sessionID, lane, query string) string {
	t.Helper()
	_ = lane // lane attribution comes from the agents' frontmatter
	before := s.Fake.RequestCount()
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText("answering from memory.")
	s.ChatTurn(t, sessionID, query, 120*time.Second)
	return promptTextsFrom(s.Fake, before)
}

// seedAgent writes an agent definition carrying the C3 memory_max_scope
// declaration (frontmatter key unknown today — the RED surface branch 01
// wires) plus distinct intent lanes so routing isolates the pair.
func seedAgent(t *testing.T, s *harness.Stack, id, maxScope string) {
	t.Helper()
	dir := filepath.Join(s.MeeptHome, "agents", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir agent %s: %v", id, err)
	}
	var scopeLine string
	if maxScope != "" {
		scopeLine = "memory_max_scope: " + maxScope + "\n"
	}
	body := `---
id: ` + id + `
name: E2E Scoper
role: conversational
description: e2e-only agent for the scoping gate
intents: [chat, report]
` + scopeLine + `enabled: true
can_delegate: false
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# E2E Scoper

Answer from memory exactly as asked.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s AGENT.md: %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// memory-scoping-03 (Flag 3): multiuser + empty userID fails CLOSED
// ---------------------------------------------------------------------------

// TestMultiuserEmptyUserIDHidesUserScopeRows plants a user-scope fact and
// a global fact, enables multiuser, then recalls from a session whose
// identity resolution yields an EMPTY userID: per Flag 3 the user-scope
// row must be HIDDEN (fail closed) while the global row stays visible.
//
// RED today: user-scope stamping and the multiuser-aware predicate do not
// exist, so the fail-closed branch is unimplemented and the user row's
// absence cannot be demonstrated — the assertion fails on the first
// unmeetable leg (the planted user row must have carried user scope, and
// the empty-userID session must hide it).
func TestMultiuserEmptyUserIDHidesUserScopeRows(t *testing.T) {
	s := scopedStack(t)

	session := s.CreateSession(t, "scope-mu", s.ProjectDir)
	storeViaTool(t, s, session, userScopeFact, "user")
	storeViaTool(t, s, session, globalFact, "global")

	// Multiuser ON with a users store, and a recall turn that resolves NO
	// user identity (the resolution bug posture Flag 3 pins): the
	// predicate must distinguish multiuser-enabled from single-user and
	// hide user rows rather than matching the empty userID.
	prompt := recallTurn(t, s, session, "what are the alice preferences and the ops maxim?")

	if strings.Contains(prompt, "window seat aisle 3") {
		t.Fatalf("Flag 3 RED: with multiuser ENABLED and a resolved-empty "+
			"userID, the user-scope row was recallable — the fail-closed "+
			"predicate branch is not implemented (C2's empty-userID-matches "+
			"leg must apply ONLY when multiuser is disabled).\nprompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "measure twice cut once") {
		t.Fatalf("Flag 3 RED: the global fact vanished under multiuser with an "+
			"empty userID — fail-closed must hide ONLY user-scope rows.\nprompt:\n%s", prompt)
	}
}
