//go:build e2e

// Suite employee-markdown: an EMPLOYEE.md markdown definition file flows
// through `meept agents validate` / `agents create` / `agents update` to a
// persisted employee against the hermetic harness daemon.
//
// This is a RED gate: the employeemd parser and the CLI's .md dispatch
// (agents create/update/validate currently call readDefinitionFile, which
// JSON5-parses the file and therefore rejects markdown) do not exist yet, so
// every test fails RED today for the missing-implementation reason — never
// because of a rigging/assertion bug. The suites tests assert the target
// behavior (frontmatter -> employee definition, body -> prompt) so they go
// GREEN exactly when the .md path lands.
//
// Assertions read state back through the LIVE daemon surfaces only: the CLI
// (validate/list) and the RPC socket (agents.get). No implementation code is
// exercised or imported.
package employee_markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// fixture purpose expected to come back from agents.get constitution.purpose.
const companionPurpose = "Keep the operator company and report on recent work"

// companionPromptUpdated is the body of the update fixture, which the .md
// update flow must surface as the employee's prompt.
const companionPromptUpdated = "You are Companion, updated. Short sentences."

// fixturePath resolves a fixture file relative to this test package's source
// dir (go test runs with cwd = the package dir, per the e2e/suites/voice-local
// precedent).
func fixturePath(t testing.TB, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("employee-markdown: getwd: %v", err)
	}
	p := filepath.Join(cwd, "fixtures", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("employee-markdown: fixture %s missing: %v", name, err)
	}
	return p
}

// str is a two-value map access for a string; unknown/missing keys yield "".
func strOf(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// createFromMarkdown fires `agents create <companion.md>` against the daemon.
// With ignoreError=false the harness fails the test verbatim on the CLI error,
// so the RED run surfaces the missing-implementation reason directly.
func createFromMarkdown(t testing.TB, s *harness.Stack) string {
	t.Helper()
	defPath := fixturePath(t, "companion.md")
	out, _ := s.RunCLI(t, 60*time.Second, false, "agents", "create", defPath)
	return out
}

// getEmployee reads the persisted employee back over the RPC socket.
func getEmployee(t testing.TB, s *harness.Stack, id string) map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	got := client.CallResult("agents.get", map[string]any{"id": id})
	if strOf(got, "id") != id {
		t.Fatalf("agents.get returned id=%q, want %q (full: %v)", strOf(got, "id"), id, got)
	}
	return got
}

// TestValidateMarkdownDefinition: `agents validate companion.md` accepts the
// markdown definition and reports it valid, naming the employee id. Fails RED
// today because readDefinitionFile JSON5-parses the file -> reject.
func TestValidateMarkdownDefinition(t *testing.T) {
	defPath := fixturePath(t, "companion.md")
	s := harness.Start(t)
	out, _ := s.RunCLI(t, 30*time.Second, false, "agents", "validate", defPath)
	if !strings.Contains(out, "valid:") {
		t.Fatalf("agents validate did not report the definition as valid:\n%s", out)
	}
	if !strings.Contains(out, "companion") {
		t.Fatalf("agents validate output did not name the employee id 'companion':\n%s", out)
	}
}

// TestCreateEmployeeFromMarkdown: `agents create companion.md` registers the
// employee (visible in agents list) and persists a constitution whose purpose
// matches the fixture's frontmatter, read back over agents.get.
func TestCreateEmployeeFromMarkdown(t *testing.T) {
	s := harness.Start(t)
	createFromMarkdown(t, s)

	out, _ := s.RunCLI(t, 30*time.Second, false, "agents", "list")
	if !strings.Contains(out, "companion") {
		t.Fatalf("agents list does not contain the newcomer employee 'companion':\n%s", out)
	}

	got := getEmployee(t, s, "companion")
	constitution, ok := got["constitution"].(map[string]any)
	if !ok {
		t.Fatalf("agents.get companion has no constitution block: %v", got)
	}
	if purpose := strOf(constitution, "purpose"); purpose != companionPurpose {
		t.Fatalf("constitution.purpose = %q, want the companion.md fixture's %q", purpose, companionPurpose)
	}
}

// TestCreateConstitutionRoundTrip: the constitution blocks of the .md
// frontmatter survive create -> agents.get unchanged (purpose, autonomy_tier,
// constraints.risk_ceiling).
func TestCreateConstitutionRoundTrip(t *testing.T) {
	s := harness.Start(t)
	createFromMarkdown(t, s)

	got := getEmployee(t, s, "companion")
	constitution, ok := got["constitution"].(map[string]any)
	if !ok {
		t.Fatalf("agents.get companion has no constitution block: %v", got)
	}
	if purpose := strOf(constitution, "purpose"); purpose != companionPurpose {
		t.Fatalf("constitution.purpose = %q, want %q", purpose, companionPurpose)
	}
	if tier := strOf(constitution, "autonomy_tier"); tier != "tier_2_propose" {
		t.Fatalf("constitution.autonomy_tier = %q, want tier_2_propose", tier)
	}
	constraints, ok := constitution["constraints"].(map[string]any)
	if !ok {
		t.Fatalf("constitution.constraints missing: %v", constitution)
	}
	if ceiling := strOf(constraints, "risk_ceiling"); ceiling != "low" {
		t.Fatalf("constitution.constraints.risk_ceiling = %q, want low", ceiling)
	}
}

// TestUpdateEmployeeFromMarkdown: `agents update companion <companion_updated.md>`
// adopts the updated file's prompt (its body), read back over agents.get. Fails
// RED today because the CLI update path does not dispatch on the .md suffix.
func TestUpdateEmployeeFromMarkdown(t *testing.T) {
	s := harness.Start(t)
	createFromMarkdown(t, s)

	updatePath := fixturePath(t, "companion_updated.md")
	out, _ := s.RunCLI(t, 60*time.Second, false, "agents", "update", "companion", updatePath)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("agents update returned no confirmation for companion")
	}

	got := getEmployee(t, s, "companion")
	if prompt := strOf(got, "prompt"); !strings.Contains(prompt, companionPromptUpdated) {
		t.Fatalf("agents.update did not adopt companion_updated.md's prompt: got %q, want it to contain %q", prompt, companionPromptUpdated)
	}
}
