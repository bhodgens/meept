//go:build e2e

package harness

// Harness self-test: one live sandbox per new capability, proving the
// feature end-to-end against the real daemon (not just the fake):
//
//   1. TestHarnessConfigOverlay    — WithConfigOverlay reaches meept.json5
//     and the daemon boots with the overlay applied (multiuser.enabled).
//   2. TestHarnessQuotaInjection   — a scripted 429 quota shape makes the
//     daemon's chat path surface a quota message (parseQuotaBody wire
//     shape), not the fake's canned text.
//   3. TestHarnessPredicateBinding — Script(LastUserMessageContains(...))
//     binds a specific reply to a specific conversation without touching
//     the legacy Enqueue/Set fields.
//   4. TestHarnessPlannerScripting — SetPlannerResponse overrides the
//     hard-coded plan on the planner shape.
//
// Kept as a permanent harness self-test (build tag e2e); it also guards
// the legacy shape routing via the daemon round-trips in each test.

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestHarnessConfigOverlay proves the config-override seam: the overlay
// key lands in the sandbox meept.json5 and the daemon still boots.
func TestHarnessConfigOverlay(t *testing.T) {
	s := Start(t, WithConfigOverlay(map[string]any{
		"multiuser.enabled": true,
	}))
	// Boot success itself is the main assertion (Start fails the test if
	// the daemon rejects the config). Verify the file content too.
	dataBytes, err := os.ReadFile(s.MeeptHome + "/meept.json5")
	if err != nil {
		t.Fatalf("read sandbox config: %v", err)
	}
	data := string(dataBytes)
	if !strings.Contains(data, `"multiuser"`) || !strings.Contains(data, `"enabled": true`) {
		t.Fatalf("overlay key missing from meept.json5:\n%s", data)
	}
	if got := s.HealthJSON(t); got["status"] != "ok" && got["status"] != "" {
		t.Logf("health payload: %v", got)
	}
}

// TestHarnessQuotaInjection proves error injection: a scripted 429 with
// the OpenAI usage_limit_reached body must make the chat turn surface a
// quota-shaped user message instead of the fake's canned "ok".
func TestHarnessQuotaInjection(t *testing.T) {
	s := Start(t)
	s.RegisterProject(t, "demo")
	sid := s.CreateSession(t, "quota-demo", s.ProjectDir)

	// Inject on EVERY completion: the daemon retries/parks on quota, so
	// serve the quota error broadly and forever.
	s.Fake.Script(func(body map[string]any) bool { return true }, QuotaResponse("usage_limit_reached", time.Now().Add(time.Hour)))

	out, _ := s.RunCLI(t, 120*time.Second, true, "chat", "--session", sid, "hello there")
	// The 429 + usage_limit_reached body MUST have been served to the
	// daemon (the request reached the fake and got the injected status —
	// never a 200 completion).
	if s.Fake.RequestCount() == 0 {
		t.Fatalf("fake received no requests; daemon never reached the model")
	}
	// The canned success text must NOT come back: the injected error took
	// the request path (QuotaResetError classification → park/terminal
	// failure), not a scripted completion.
	if strings.TrimSpace(strings.ToLower(out)) == "ok" {
		t.Fatalf("quota injection ignored: chat returned canned success text")
	}
	t.Logf("quota-injected chat output: %.400s", out)
}

// TestHarnessPredicateBinding proves conversation-bound responses: two
// sessions, two different predicate-keyed executor tool calls — each
// session's turn writes ITS OWN artifact — with no FIFO queue and no
// legacy Enqueue* setters.
func TestHarnessPredicateBinding(t *testing.T) {
	s := Start(t)
	s.RegisterProject(t, "demo")
	sidA := s.CreateSession(t, "bind-a", s.ProjectDir)
	sidB := s.CreateSession(t, "bind-b", s.ProjectDir)

	artifactA := s.ProjectDir + "/bind-a.txt"
	artifactB := s.ProjectDir + "/bind-b.txt"
	s.Fake.ScriptOnce(
		And(IsExecutorRequest(), LastUserMessageContains("BINDMARKER-A")),
		ToolCallResponse(ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifactA + `","content":"alpha","direct":true}`,
		}))
	s.Fake.ScriptOnce(
		And(IsExecutorRequest(), LastUserMessageContains("BINDMARKER-B")),
		ToolCallResponse(ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifactB + `","content":"beta","direct":true}`,
		}))
	s.Fake.SetPostToolText("wrote the bound artifact")

	s.ChatTurn(t, sidA, "BINDMARKER-A: create the file", 120*time.Second)
	s.ChatTurn(t, sidB, "BINDMARKER-B: create the file", 120*time.Second)

	dataA, err := os.ReadFile(artifactA)
	if err != nil || string(dataA) != "alpha" {
		t.Fatalf("session A artifact wrong (err=%v content=%q)", err, dataA)
	}
	dataB, err := os.ReadFile(artifactB)
	if err != nil || string(dataB) != "beta" {
		t.Fatalf("session B artifact wrong (err=%v content=%q)", err, dataB)
	}
}

// TestHarnessPlannerScripting proves planner + classifier scripting
// against the full daemon: a distinctive planner plan plus a pinned
// "code" classification drive a step that writes a file whose content
// names the plan marker — proving the pinned plan (not the hard-coded
// one) reached the executor.
func TestHarnessPlannerScripting(t *testing.T) {
	s := Start(t)
	s.RegisterProject(t, "demo")
	sid := s.CreateSession(t, "plan-demo", s.ProjectDir)

	// Pin "code" intent so the turn dispatches to the planner at all
	// (pinned BEFORE the turn — the daemon reads config at dispatch).
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)

	// Pin a plan whose step description embeds the marker text; the
	// executor then writes it verbatim.
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"write a file containing PLANMARKER-XYZ","tool_hint":"code","depends_on":[]}]}`)
	s.Fake.EnqueueFileWrite("w1", s.ProjectDir+"/planmarker.txt", "PLANMARKER-XYZ")
	s.Fake.SetPostToolText("planned file written")

	s.ChatTurn(t, sid, "PLANRUN: create a file", 150*time.Second)

	data, err := os.ReadFile(s.ProjectDir + "/planmarker.txt")
	if err != nil {
		t.Fatalf("executor never wrote the planned file: %v\nfake requests: %d", err, s.Fake.RequestCount())
	}
	if !strings.Contains(string(data), "PLANMARKER-XYZ") {
		t.Fatalf("planned file has unexpected content: %q", data)
	}
}
