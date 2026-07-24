# Leaf 01-02: Adversarial Verifier Prompt + Evidence Fusion

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 01-adversarial-verification/orchestrator.md
**Scope:** Create the adversarial verifier system prompt (fused with EvidenceRequirements), the verifier agent definition, and the VERDICT parser.
**Dependencies:** Leaf 01 (VerificationConfig type must exist)
**Estimated Context:** ~90K

## Interface Contract

This leaf exposes:
- `BuildVerifierPrompt(agentRole string, taskDescription string, filesChanged []string, approach string) string` in `internal/agent/prompts/`
- `ParseVerdict(output string) (Verdict, []CheckResult)` in `internal/agent/`
- Verifier agent definition in `config/agents/verifier.json5`
- Extended `EvidenceRequirements` constant with adversarial self-probes

## Tasks

### Task 1: Extend EvidenceRequirements with adversarial self-probes

**File:** `internal/agent/prompts/baseline.go` (wherever EvidenceRequirements is defined)

Read the existing `EvidenceRequirements` constant. Append an adversarial self-check section:

```go
const EvidenceRequirements = `# Evidence Requirements
When completing tasks, you MUST provide evidence that the work was done:
## Claims
Explicit statements of what was accomplished
## Evidence
Proof that claims are true:
- file_exists evidence (path, size)
- file_hash evidence (SHA256 hash)
- process_exit evidence (exit code)
- shell_output evidence (output hash)
**IMPORTANT:** Tasks without evidence will fail validation.

## Adversarial Self-Check
Before reporting completion, challenge your own work:
- Did you actually RUN the code, or just read it and assume it works?
- If tests pass, did you check they test the RIGHT thing (not just mocking everything)?
- If you fixed a bug, did you verify the fix doesn't break adjacent functionality?
- If you added a feature, did you test edge cases (empty input, nil, boundary values)?
- Are there any error paths you didn't exercise?
- Did you introduce any new dependencies or side effects you didn't account for?
Report any concerns honestly. A partial answer with caveats is better than a false "all good."
`
```

### Task 2: Create the adversarial verifier prompt builder

**File:** `internal/agent/prompts/verifier.go` (new)

Adapt Claude Code's verification agent prompt for meept. Key sections to include:

```go
package prompts

import (
    "fmt"
    "strings"
)

// BuildVerifierPrompt constructs the adversarial verifier system prompt.
// The verifier's job is NOT to confirm the implementation works — it's to try to break it.
func BuildVerifierPrompt(agentRole, taskDescription string, filesChanged []string, approach string) string {
    var b strings.Builder

    b.WriteString(`You are a verification specialist. Your job is not to confirm the implementation works — it's to try to break it.

You have two documented failure patterns. First, verification avoidance: when faced with a check, you find reasons not to run it — you read code, narrate what you would test, write "PASS," and move on. Second, being seduced by the first 80%: you see a polished result or a passing test suite and feel inclined to pass it, not noticing half the functionality is missing, the state vanishes on restart, or the system crashes on bad input. The first 80% is the easy part. Your entire value is in finding the last 20%.

=== CRITICAL: DO NOT MODIFY THE PROJECT ===
You are STRICTLY PROHIBITED from:
- Creating, modifying, or deleting any files in the project directory
- Installing dependencies or packages
- Running git write operations (add, commit, push)

You MAY write ephemeral test scripts to a temp directory (/tmp or $TMPDIR) when inline commands aren't sufficient. Clean up after yourself.

=== WHAT YOU RECEIVE ===
You will receive: the original task description, files changed, approach taken, and the agent role that performed the work.

=== VERIFICATION STRATEGY ===
Adapt your strategy based on what was changed:

`)

    // Role-specific verification strategies
    switch agentRole {
    case "coder", "developer":
        b.WriteString(`**Code changes**: Run the build → run the test suite → run linters/type-checkers → verify the specific change works as intended → test edge cases → check for regressions in related code.
`)
    case "debugger":
        b.WriteString(`**Bug fixes**: Reproduce the original bug → verify the fix resolves it → run regression tests → check related functionality for side effects → verify the fix doesn't mask a deeper issue.
`)
    case "planner", "architect":
        b.WriteString(`**Plan/design changes**: Verify the plan is internally consistent → check that referenced files/symbols actually exist → verify assumptions against the actual codebase → check for missing edge cases in the plan.
`)
    default:
        b.WriteString(`**General changes**: Run the build (if applicable) → run the test suite → verify the specific change works → test edge cases → check for regressions.
`)
    }

    b.WriteString(`
=== REQUIRED STEPS (universal baseline) ===
1. Read the project's CLAUDE.md / README for build/test commands and conventions.
2. Run the build (if applicable). A broken build is an automatic FAIL.
3. Run the project's test suite (if it has one). Failing tests are an automatic FAIL.
4. Run linters/type-checkers if configured.
5. Check for regressions in related code.

Then apply the role-specific strategy above. Match rigor to stakes.

Test suite results are context, not evidence. Run the suite, note pass/fail, then move on to your real verification. The implementer is an LLM too — its tests may be heavy on mocks, circular assertions, or happy-path coverage that proves nothing.

=== RECOGNIZE YOUR OWN RATIONALIZATIONS ===
You will feel the urge to skip checks. These are the exact excuses you reach for — recognize them and do the opposite:
- "The code looks correct based on my reading" — reading is not verification. Run it.
- "The implementer's tests already pass" — the implementer is an LLM. Verify independently.
- "This is probably fine" — probably is not verified. Run it.
- "This would take too long" — not your call.
If you catch yourself writing an explanation instead of a command, stop. Run the command.

=== ADVERSARIAL PROBES (adapt to the change type) ===
Functional tests confirm the happy path. Also try to break it:
- **Concurrency**: parallel operations on shared state — race conditions? lost writes?
- **Boundary values**: 0, -1, empty string, very long strings, unicode, MAX_INT
- **Idempotency**: same mutating operation twice — duplicate created? error? correct no-op?
- **Orphan operations**: delete/reference IDs that don't exist
- **Nil/zero values**: what happens with nil input, zero-length slices, empty maps?
These are seeds, not a checklist — pick the ones that fit what you're verifying.

=== BEFORE ISSUING PASS ===
Your report must include at least one adversarial probe you ran and its result. If all your checks are "returns 200" or "test suite passes," you have confirmed the happy path, not verified correctness. Go back and try to break something.

=== BEFORE ISSUING FAIL ===
You found something that looks broken. Before reporting FAIL, check:
- **Already handled**: is there defensive code elsewhere that prevents this?
- **Intentional**: does documentation/comments explain this as deliberate?
- **Not actionable**: is this a real limitation but unfixable without breaking an external contract?
Don't use these as excuses to wave away real issues — but don't FAIL on intentional behavior either.

=== EVIDENCE REQUIREMENTS (fused from baseline) ===
Every check MUST follow this structure. A check without a Command run block is not a PASS — it's a skip.

### Check: [what you're verifying]
**Command run:**
  [exact command you executed]
**Output observed:**
  [actual terminal output — copy-paste, not paraphrased]
**Result: PASS** (or FAIL — with Expected vs Actual)

Evidence types accepted:
- file_exists (path, size)
- file_hash (SHA256)
- process_exit (exit code)
- shell_output (actual output)

=== OUTPUT FORMAT (REQUIRED) ===
End with exactly this line (parsed by caller):

VERDICT: PASS
or
VERDICT: FAIL
or
VERDICT: PARTIAL

PARTIAL is for environmental limitations only (no test framework, tool unavailable, server can't start) — not for "I'm unsure whether this is a bug."

`)

    // Append task context
    b.WriteString(fmt.Sprintf("=== TASK CONTEXT ===\nOriginal task: %s\n\nFiles changed:\n", taskDescription))
    for _, f := range filesChanged {
        b.WriteString(fmt.Sprintf("- %s\n", f))
    }
    if approach != "" {
        b.WriteString(fmt.Sprintf("\nApproach taken: %s\n", approach))
    }

    return b.String()
}
```

### Task 3: Create VERDICT parser

**File:** `internal/agent/verdict.go` (new)

```go
package agent

import (
    "regexp"
    "strings"
)

// Verdict represents the outcome of an adversarial verification.
type Verdict int

const (
    VerdictPass    Verdict = iota // verification passed
    VerdictFail                   // verification found issues
    VerdictPartial                // environmental limitations prevented full verification
    VerdictUnknown                // could not parse verdict
)

func (v Verdict) String() string {
    switch v {
    case VerdictPass:
        return "PASS"
    case VerdictFail:
        return "FAIL"
    case VerdictPartial:
        return "PARTIAL"
    default:
        return "UNKNOWN"
    }
}

// CheckResult represents a single verification check.
type CheckResult struct {
    Name    string
    Command string
    Output  string
    Passed  bool
}

var verdictRe = regexp.MustCompile(`(?m)^VERDICT:\s*(PASS|FAIL|PARTIAL)\s*$`)

// ParseVerdict extracts the verdict and check results from verifier output.
func ParseVerdict(output string) (Verdict, []CheckResult) {
    // Parse verdict line
    matches := verdictRe.FindStringSubmatch(output)
    var verdict Verdict
    switch {
    case len(matches) < 2:
        verdict = VerdictUnknown
    case matches[1] == "PASS":
        verdict = VerdictPass
    case matches[1] == "FAIL":
        verdict = VerdictFail
    case matches[1] == "PARTIAL":
        verdict = VerdictPartial
    }

    // Parse check blocks
    checks := parseChecks(output)

    return verdict, checks
}

func parseChecks(output string) []CheckResult {
    // Split on "### Check:" headers
    sections := strings.Split(output, "### Check:")
    var checks []CheckResult
    for _, section := range sections[1:] { // skip preamble
        lines := strings.SplitN(section, "\n", 2)
        name := strings.TrimSpace(lines[0])
        body := ""
        if len(lines) > 1 {
            body = lines[1]
        }

        check := CheckResult{Name: name}

        // Extract command
        if cmdIdx := strings.Index(body, "**Command run:**"); cmdIdx != -1 {
            rest := body[cmdIdx+len("**Command run:**"):]
            if outIdx := strings.Index(rest, "**Output observed:**"); outIdx != -1 {
                check.Command = strings.TrimSpace(rest[:outIdx])
            }
        }

        // Extract output
        if outIdx := strings.Index(body, "**Output observed:**"); outIdx != -1 {
            rest := body[outIdx+len("**Output observed:**"):]
            if resIdx := strings.Index(rest, "**Result:"); resIdx != -1 {
                check.Output = strings.TrimSpace(rest[:resIdx])
            }
        }

        // Extract pass/fail
        check.Passed = strings.Contains(body, "**Result: PASS**")

        checks = append(checks, check)
    }
    return checks
}
```

### Task 4: Create verifier agent definition

**File:** `config/agents/verifier.json5` (new)

```json5
{
  "id": "verifier",
  "name": "verifier",
  "role": "verifier",
  "description": "adversarial verification specialist — tries to break implementations",
  "model": "",
  "tools_allow": ["shell", "file_read", "file_grep", "file_find", "list_directory", "web_fetch"],
  "tools_deny": ["file_write", "file_edit", "file_delete", "git_commit", "memory_store", "task_create", "task_update", "ask", "resolve"],
  "verification": {
    "enabled": false
  },
  "system_prompt_override": "verifier"
}
```

Note: The verifier's own verification is disabled (it doesn't verify itself). The `system_prompt_override` field tells the prompt builder to use `BuildVerifierPrompt()` instead of the standard baseline.

### Task 5: Wire verifier prompt into prompt builder

**File:** `internal/agent/prompts/builder.go` or wherever `BuildFullPrompt` is defined

Add a case for the verifier role that calls `BuildVerifierPrompt()` instead of `BuildBaselinePrompt() + specialist`. The verifier prompt is fully self-contained (it includes its own evidence requirements).

### Task 6: Tests

**File:** `internal/agent/verdict_test.go` (new)

Table-driven tests:
- `TestParseVerdict` — PASS, FAIL, PARTIAL, malformed, missing
- `TestParseChecks` — extract check names, commands, outputs, pass/fail
- `TestParseVerdictEmpty` — empty string returns VerdictUnknown
- `TestParseVerdictMultipleChecks` — multiple check blocks parsed correctly

**File:** `internal/agent/prompts/verifier_test.go` (new)

- `TestBuildVerifierPrompt` — contains key sections (adversarial, evidence, VERDICT)
- `TestBuildVerifierPromptRoleSpecific` — coder vs debugger vs planner strategies differ
- `TestBuildVerifierPromptContext` — task description, files, approach appear in output

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` compiles
- [ ] `go test ./internal/agent/... -race -run "TestVerdict|TestVerifier|TestParse"` passes
- [ ] `config/agents/verifier.json5` parses without error
- [ ] EvidenceRequirements includes adversarial self-check section
- [ ] Verifier prompt contains "your job is not to confirm" language
- [ ] Verifier prompt contains anti-rationalization section
- [ ] VERDICT parser handles all 4 cases (PASS/FAIL/PARTIAL/unknown)
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] Adversarial prompt borrowed faithfully from Claude Code report (anti-rationalization, adversarial probes, evidence format)
- [ ] Role-specific verification strategies present (coder, debugger, planner, default)
- [ ] Evidence fusion: verifier demands same evidence types as baseline EvidenceRequirements
- [ ] Verifier agent definition has correct tool allow/deny lists
- [ ] Verifier's own verification is disabled
- [ ] VERDICT parser is robust (regex, handles whitespace, multiple checks)
- [ ] No debug artifacts, no TODOs, no placeholder values
