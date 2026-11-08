//go:build e2e

// Suite office-skills: the 06-office-skills branch e2e gate (T1-T4 rows of
// docs/plans/20261102-dsh-convergence/06-office-skills/00T-tests.md).
// Asserts the TOOL BEHAVIOR CONTRACT (contracts C11.2-C11.4) through the
// live daemon surface - it NEVER imports the concrete docx/pptx library, so
// the suite is identical regardless of what the research leaf resolved.
// Parked as .staged until the branch's implementation leaves land (voice
// precedent 537c92fd): activation is mv *_test.go.staged -> *_test.go plus
// the e2e/manifest.json entries ("office-skills" suite + path_map for
// internal/tools/builtin/ + config/skills/) the orchestrator adds.
package office_skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// TestT1_OfficeDocxRoundTrip (C11.2): office_docx create -> read round-trip
// returns the final absolute path and the reopened document contains the
// requested heading/paragraph text.
func TestT1_OfficeDocxRoundTrip(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "office-e2e")
	sessionID := s.CreateSession(t, "docx-roundtrip", s.ProjectDir)

	reply := s.ChatTurn(t, sessionID,
		"Call the office_docx tool with action \"create\", path \"probe.docx\" (inside the working directory), and blocks [{type:\"heading\",text:\"PROBE-HEADING\",level:\"1\"},{type:\"paragraph\",text:\"PROBE-PARA\"}]. Then call office_docx with action \"read\" on the returned path. Report both raw tool results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "not registered") {
		t.Fatalf("office_docx not registered: %s", reply)
	}
	if !strings.Contains(reply, "PROBE-HEADING") || !strings.Contains(reply, "PROBE-PARA") {
		t.Errorf("docx read-back missing requested content: %s", reply)
	}
	if !strings.Contains(reply, "probe.docx") {
		t.Errorf("docx result missing the final path: %s", reply)
	}
}

// TestT2_OfficePptxRoundTrip (C11.3): office_pptx create -> read round-trip
// returns the final path and the reopened deck has the requested slide
// title/body.
func TestT2_OfficePptxRoundTrip(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "office-e2e")
	sessionID := s.CreateSession(t, "pptx-roundtrip", s.ProjectDir)

	reply := s.ChatTurn(t, sessionID,
		"Call the office_pptx tool with action \"create\", path \"probe.pptx\", and slides [{title:\"PROBE-TITLE\",body:[\"PROBE-BODY\"],notes:\"PROBE-NOTES\"}]. Then call office_pptx with action \"read\" on the returned path. Report both raw tool results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "not registered") {
		t.Fatalf("office_pptx not registered: %s", reply)
	}
	if !strings.Contains(reply, "PROBE-TITLE") || !strings.Contains(reply, "PROBE-BODY") {
		t.Errorf("pptx read-back missing requested slide content: %s", reply)
	}
}

// TestT3_OfficeWorkbookRoundTrip (C11.4): office_workbook create -> read
// round-trip returns the final path, sheet names, and typed cell values;
// formulas are preserved, not evaluated.
func TestT3_OfficeWorkbookRoundTrip(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "office-e2e")
	sessionID := s.CreateSession(t, "xlsx-roundtrip", s.ProjectDir)

	reply := s.ChatTurn(t, sessionID,
		"Call the office_workbook tool with action \"create\", path \"probe.xlsx\", and sheets [{name:\"PROBE-SHEET\",headers:[\"name\",\"count\"],rows:[[\"widget\",3],[\"gadget\",5]]}]. Then call office_workbook with action \"read\" on the returned path. Report both raw tool results verbatim.",
		5*time.Minute)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "unknown tool") || strings.Contains(lower, "tool not found") ||
		strings.Contains(lower, "not registered") {
		t.Fatalf("office_workbook not registered: %s", reply)
	}
	for _, want := range []string{"PROBE-SHEET", "widget", "gadget"} {
		if !strings.Contains(reply, want) {
			t.Errorf("xlsx read-back missing %q: %s", want, reply)
		}
	}
}

// TestT4_RequiresToolsGate (F14d, C11.1): with
// skills.validate_prerequisites=true, executing a skill whose requires-tools
// lists an UNREGISTERED tool is refused with the exact contract message
// "skill <name> requires unavailable tool(s): <list>".
func TestT4_RequiresToolsGate(t *testing.T) {
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"skills.validate_prerequisites": true,
		}),
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// Drop a probe skill whose required tool does not exist.
			dir := filepath.Join(st.MeeptHome, "skills", "office-probe")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(
				"---\nname: office-probe\ndescription: probe skill for the requires-tools gate\nrequires-tools:\n  - office_docx_missing_probe\n---\n\nRun the probe.\n"), 0o644)
		}))
	s.RegisterProject(t, "office-gate")
	sessionID := s.CreateSession(t, "requires-tools", s.ProjectDir)

	reply := s.ChatTurn(t, sessionID,
		"Execute the skill named office-probe and report the raw result verbatim.",
		3*time.Minute)
	lower := strings.ToLower(reply)
	if !strings.Contains(lower, "requires unavailable tool") {
		t.Fatalf("requires-tools gate did not fire with the contract message: %s", reply)
	}
	if !strings.Contains(reply, "office_docx_missing_probe") {
		t.Errorf("gate message missing the unavailable tool name: %s", reply)
	}
}
