//go:build e2e

// agent-groups-07 (F5): memory ceiling (leaf 05, C7) — re-armed by the
// 05-memory-ceiling leaf after issue #70 branch 01 landed (9b1b94f9 +
// 4cefbfc9). The dated skip this file replaces is documented in the suite
// coverage map (agent_groups_test.go).
//
// Pin: a grouped agent's recall is bounded by the NARROWEST group
// memory_max_scope, never the agent's own wider declaration — groups
// NEVER widen. The probe declares the agent memory_max_scope: global
// (chief-of-staff posture: alone, it recalls cross-project rows) and
// binds a group policy with memory_max_scope: project. A project-scope
// fact planted in project A must be ABSENT from the grouped agent's
// project-B recall prompt — without the ceiling the declaration alone
// would surface it (memory-scoping-02 pins that exact behavior for the
// ungrouped chief).
package agentgroups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// F5 fact payloads (distinct tokens, same discipline as the memory-scoping
// suite).
const (
	f5ProjectFact = "the zkarev launch pin is 7-dot Pine 992"
	f5RecallQuery = "chief: what is the launch pin?"
)

// f5SeedChief writes a grouped agent with a WIDE self-declaration
// (memory_max_scope: global) and group membership [ceilinggrp]. The group
// policy supplies the project ceiling — the C7 narrowing is what keeps
// this agent from recalling above project.
func f5SeedChief(t *testing.T, st *harness.Stack) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "aabchief")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir aabchief agent dir: %v", err)
	}
	body := `---
id: aabchief
name: E2E Ceiling Chief
role: conversational
description: e2e-only agent for the F5 memory-ceiling gate
intents: [chat, report]
memory_max_scope: global
groups: [ceilinggrp]
enabled: true
can_delegate: false
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# E2E Ceiling Chief

Answer from memory exactly as asked.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write aabchief AGENT.md: %v", err)
	}
}

// f5StoreViaTool drives a memory_store turn in the given session and waits
// for the execution observable in the daemon log (the same pattern the
// memory-scoping suite pins: the tool envelope is reply-guard-rewritten,
// so the reply is not a reliable landed signal).
func f5StoreViaTool(t *testing.T, s *harness.Stack, sessionID, content string) {
	t.Helper()
	args := `{"content":"` + content + `","type":"task","category":"general","scope":"project"}`
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_store",
		Arguments: args,
	})
	s.ChatTurn(t, sessionID, "Please remember that "+content, 120*time.Second)
	harness.WaitFor(t, 30*time.Second, "the memory_store execution for "+content, func() bool {
		tail := s.Daemon.LogTail()
		return strings.Contains(tail, "tool=memory_store") &&
			strings.Contains(tail, "7-dot")
	})
}

// f5PromptTexts joins the system+user message contents captured from index
// `since` onward — the surfaces where recalled memories render.
func f5PromptTexts(f *harness.FakeLLM, since int) string {
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

// TestAgentGroupsF5MemoryCeilingBoundsRecall pins C7 end to end: the
// grouped chief (decl global, group ceiling project) must NOT recall the
// project-A fact in its project-B session.
//
// Control leg: the SAME fact IS recallable cross-project by an ungrouped
// chief with the same global declaration (memory-scoping-02's green cell)
// — asserting that here too would double the runtime, so the ceiling leg
// is asserted instead against the DISCIPLINED negative: the fact token
// must be absent from the grouped agent's prompt.
func TestAgentGroupsF5MemoryCeilingBoundsRecall(t *testing.T) {
	s := harness.Start(t,
		harness.WithExtraEnv(map[string]string{
			// The session-digest echoes plant-turn text into the SAME
			// session's next prompts (session continuity, not memory
			// recall) — disable so the assertion measures recall only
			// (memory-scoping suite precedent).
			"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
		}),
		harness.WithPreBootHook(func(st *harness.Stack) error {
			f5SeedChief(t, st)
			return nil
		}),
		harness.WithConfigOverlay(map[string]any{
			"groups": groupsSection(
				[]map[string]any{policy("ceiling", nil, nil, map[string]any{
					"memory_max_scope": "project",
				})},
				[]map[string]any{binding("group:ceilinggrp", "ceiling")},
				false,
			),
		}),
	)
	s.RegisterProject(t, "e2e-project")

	// Plant a project-scope fact in a project-A session (the default chat
	// lane executes memory_store; no group membership involved on the
	// write side).
	sessionA := s.CreateSession(t, "ag-f5-a", s.ProjectDir)
	f5StoreViaTool(t, s, sessionA, f5ProjectFact)

	// Recall from a project-B session routed to the grouped chief.
	projectB := filepath.Join(s.Work, "project-b")
	if err := os.MkdirAll(projectB, 0o755); err != nil {
		t.Fatalf("mkdir project B: %v", err)
	}
	s.RunCLI(t, 30*time.Second, false, "projects", "add", projectB, "--name", "e2e-project-b-f5")
	sessionChief := s.CreateSession(t, "ag-f5-chief", projectB)

	before := s.Fake.RequestCount()
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText("answering from memory.")
	s.ChatTurn(t, sessionChief, f5RecallQuery, 120*time.Second)
	prompt := f5PromptTexts(s.Fake, before)

	if strings.Contains(prompt, "7-dot Pine 992") {
		t.Fatalf("F5 RED: the grouped chief (decl=global, group ceiling=project) "+
			"RECALLED the project-A fact — the group memory_max_scope ceiling is "+
			"not enforced at the recall sites (C7: groups never widen).\nprompt:\n%s",
			prompt)
	}
}
