# Leaf 05-04: Security Hardening + Explore Agent

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 05-combined-improvements/orchestrator.md
**Scope:** (A) Add bash injection checks (IFS, brace expansion, unicode whitespace, process substitution). (B) Add secret discovery scanning (regex rules for unknown secrets). (C) Create explore agent definition with fast model tier.
**Dependencies:** None
**Estimated Context:** ~65K

## Interface Contract

This leaf exposes:
- Additional bash security patterns in `internal/security/`
- `SecretScanner` with regex rules in `internal/security/secrets.go`
- `config/agents/explore.json5` agent definition
- Explore agent prompt in `internal/agent/prompts/`

## Tasks

### Part A: Bash Injection Checks

### Task 1: Add injection detection patterns

**File:** `internal/security/pre_exec.go` or `internal/security/engine.go`

Read the existing command security checks. Add patterns for bash injection vectors that the current regex-based system misses:

```go
// bashInjectionPatterns detect shell injection vectors beyond basic
// command blocking. These are checked in addition to the existing
// 60 seeded command patterns.
var bashInjectionPatterns = []struct {
    pattern *regexp.Regexp
    name    string
    risk    RiskLevel
}{
    // IFS injection — manipulating Internal Field Separator
    {regexp.MustCompile(`\bIFS\s*=`), "IFS manipulation", RiskHigh},
    {regexp.MustCompile(`\$\{?IFS\}?`), "IFS variable reference", RiskHigh},

    // Brace expansion — can bypass glob-based filters
    {regexp.MustCompile(`\{[a-zA-Z0-9/]+\.\.[a-zA-Z0-9/]+\}`), "brace expansion range", RiskMedium},
    {regexp.MustCompile(`\{[a-zA-Z],[a-zA-Z]\}`), "brace expansion alternation", RiskMedium},

    // Unicode whitespace — invisible characters that break parsing
    {regexp.MustCompile(`[\x{00A0}\x{1680}\x{2000}-\x{200B}\x{202F}\x{205F}\x{3000}\x{FEFF}]`), "unicode whitespace", RiskHigh},

    // Process substitution — execute commands via file descriptors
    {regexp.MustCompile(`<\(`), "process substitution <()", RiskHigh},
    {regexp.MustCompile(`>\(`), "process substitution >()", RiskHigh},
    {regexp.MustCompile(`=\(`), "zsh process substitution =()", RiskHigh},

    // Command substitution variants not caught by existing fence checker
    {regexp.MustCompile(`\$\[`), "legacy arithmetic expansion $[]", RiskMedium},

    // Control characters — can break terminal parsing
    {regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F]`), "control characters", RiskHigh},

    // Comment/quote desync — unclosed quotes with comments
    {regexp.MustCompile(`['"][^'"]*#`), "comment inside unclosed quote", RiskMedium},
}
```

### Task 2: Wire injection checks into security pipeline

**File:** `internal/security/engine.go` or `internal/security/pre_exec.go`

Add the injection pattern checks to the existing `evaluateCommand()` or the 5-stage pipeline. These should run in the "Context analysis" stage (stage 3), after the base rule lookup:

```go
func (e *Engine) checkBashInjections(command string) []SecurityFinding {
    var findings []SecurityFinding
    for _, p := range bashInjectionPatterns {
        if p.pattern.MatchString(command) {
            findings = append(findings, SecurityFinding{
                Rule:    "bash_injection_" + p.name,
                Risk:    p.risk,
                Message: fmt.Sprintf("detected %s in command", p.name),
            })
        }
    }
    return findings
}
```

Wire into the pipeline so findings contribute to the overall risk assessment. High-risk findings should trigger confirmation; Critical should block.

### Task 3: Tests for bash injection checks

**File:** `internal/security/pre_exec_test.go` (extend existing) or new test file

- `TestIFSDetection` — `IFS=/ cmd` detected
- `TestBraceExpansion` — `{a..z}` and `{a,b}` detected
- `TestUnicodeWhitespace` — non-breaking space detected
- `TestProcessSubstitution` — `<()`, `>()`, `=()` detected
- `TestControlCharacters` — embedded control chars detected
- `TestNormalCommandPasses` — `ls -la`, `go build ./...` not flagged
- `TestPipeCommandNotFalsePositive` — `cat file | grep pattern` not flagged

### Part B: Secret Discovery Scanning

### Task 4: Add secret discovery scanner

**File:** `internal/security/secrets.go`

Read the existing `SecretObfuscator` (known-secret obfuscation). Add a NEW `SecretScanner` for discovering unknown secrets by pattern. This is additive — the obfuscator handles known secrets, the scanner detects unknown ones:

```go
// SecretScanner detects potential secrets in text by pattern matching.
// Unlike SecretObfuscator (which handles known secrets), this scanner
// discovers unknown secrets using high-confidence regex rules.
type SecretScanner struct {
    rules []SecretRule
}

type SecretRule struct {
    Name    string
    Pattern *regexp.Regexp
}

// NewSecretScanner creates a scanner with default high-confidence rules.
func NewSecretScanner() *SecretScanner {
    return &SecretScanner{rules: defaultSecretRules()}
}

func defaultSecretRules() []SecretRule {
    return []SecretRule{
        {"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
        {"aws_secret_key", regexp.MustCompile(`(?i)aws_secret_access_key\s*[=:]\s*[A-Za-z0-9/+=]{40}`)},
        {"github_token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,255}`)},
        {"github_fine_grained", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82}`)},
        {"openai_key", regexp.MustCompile(`sk-[A-Za-z0-9]{20}T3BlbkFJ[A-Za-z0-9]{20}`)},
        {"anthropic_key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{90,}`)},
        {"slack_token", regexp.MustCompile(`xox[baprs]-[0-9]{10,13}-[0-9]{10,13}-[a-zA-Z0-9]{24,34}`)},
        {"stripe_key", regexp.MustCompile(`[sr]k_live_[0-9a-zA-Z]{24,}`)},
        {"private_key_header", regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`)},
        {"gcp_service_account", regexp.MustCompile(`"type":\s*"service_account"`)},
        {"generic_api_key", regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[=:]\s*['"][A-Za-z0-9_\-]{20,}['"]`)},
        {"generic_secret", regexp.MustCompile(`(?i)(secret|password|passwd|token)\s*[=:]\s*['"][^\s'"]{12,}['"]`)},
        {"jwt_token", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
    }
}

// Scan checks text for potential secrets. Returns the names of matched rules.
func (s *SecretScanner) Scan(text string) []string {
    var matches []string
    for _, rule := range s.rules {
        if rule.Pattern.MatchString(text) {
            matches = append(matches, rule.Name)
        }
    }
    return matches
}

// ScanAndReport checks text and returns a human-readable warning if secrets found.
func (s *SecretScanner) ScanAndReport(text string) string {
    matches := s.Scan(text)
    if len(matches) == 0 {
        return ""
    }
    return fmt.Sprintf("WARNING: potential secrets detected (%s). Review output before sharing or storing.", strings.Join(matches, ", "))
}
```

### Task 5: Wire scanner into tool output

**File:** `internal/tools/builtin/shell.go` and/or `internal/agent/handler.go`

Add secret scanning to shell tool output before it enters the agent's context:

```go
// After shell command execution, before returning result:
if warning := t.secretScanner.ScanAndReport(output); warning != "" {
    result.Output = warning + "\n\n" + result.Output
    result.Metadata["secret_warning"] = true
}
```

Also scan file_read output for files that might contain secrets (.env, config files):

```go
// In file_read, for files matching secret-prone patterns:
if isSecretProneFile(filePath) {
    if warning := t.secretScanner.ScanAndReport(content); warning != "" {
        result.Output = warning + "\n\n" + result.Output
    }
}

func isSecretProneFile(path string) bool {
    base := filepath.Base(path)
    return base == ".env" || strings.HasSuffix(base, ".env") ||
        base == "credentials" || base == "secrets.yaml" ||
        base == "secrets.yml" || strings.Contains(base, "secret")
}
```

### Task 6: Tests for secret scanner

**File:** `internal/security/secrets_test.go` (extend existing or new)

- `TestScanAWSKey` — detects AKIA... pattern
- `TestScanGitHubToken` — detects ghp_... pattern
- `TestScanOpenAIKey` — detects sk-...T3BlbkFJ... pattern
- `TestScanPrivateKey` — detects PEM header
- `TestScanCleanText` — no false positives on normal code
- `TestScanAndReport` — returns warning string with rule names
- `TestScanMultipleMatches` — detects multiple secret types in one text

### Part C: Explore Agent

### Task 7: Create explore agent definition

**File:** `config/agents/explore.json5` (new)

```json5
{
  "id": "explore",
  "name": "explore",
  "role": "explorer",
  "description": "fast read-only codebase search specialist — finds files, searches content, reads code",
  "model_tier": "fast",
  "tools_allow": ["file_read", "file_grep", "file_find", "list_directory", "shell"],
  "tools_deny": ["file_write", "file_edit", "file_delete", "git_commit", "memory_store", "memory_recall", "task_create", "task_update", "ask", "resolve", "web_fetch", "web_search"],
  "verification": {
    "enabled": false
  },
  "max_turns": 15,
  "system_prompt_override": "explorer"
}
```

Note: `model_tier: "fast"` tells the model resolver to use the fast/small model tier. The resolver falls back to the default model if no fast tier is configured.

### Task 8: Create explore agent prompt

**File:** `internal/agent/prompts/explorer.go` (new)

```go
package prompts

// ExplorerPrompt is the system prompt for the explore agent.
// Adapted from Claude Code's Explore agent: read-only, fast, parallel searches.
const ExplorerPrompt = `You are a file search specialist. You excel at thoroughly navigating and exploring codebases.

=== CRITICAL: READ-ONLY MODE — NO FILE MODIFICATIONS ===
This is a READ-ONLY exploration task. You are STRICTLY PROHIBITED from:
- Creating new files (no Write, touch, or file creation of any kind)
- Modifying existing files (no Edit operations)
- Deleting files (no rm or deletion)
- Running ANY commands that change system state

Your role is EXCLUSIVELY to search and analyze existing code.

Your strengths:
- Rapidly finding files using glob patterns (file_find)
- Searching code and text with regex patterns (file_grep)
- Reading and analyzing file contents (file_read)

Guidelines:
- Use file_find for broad file pattern matching
- Use file_grep for searching file contents with regex
- Use file_read when you know the specific file path you need
- Use shell ONLY for read-only operations (ls, git status, git log, git diff, find, cat, head, tail, wc)
- NEVER use shell for: mkdir, touch, rm, cp, mv, git add, git commit, or any file creation/modification

NOTE: You are meant to be a fast agent that returns output as quickly as possible. In order to achieve this you must:
- Make efficient use of the tools at your disposal: be smart about how you search for files and implementations
- Wherever possible, spawn multiple parallel tool calls for grepping and reading files
- Start broad (file_find for patterns, file_grep for keywords) then narrow to specific files

Complete the user's search request efficiently and report your findings clearly. Structure your report as:

## Files Found
- path/to/file.go — brief description of relevance

## Key Findings
- [finding with file:line reference]

## Summary
[2-3 sentence summary of what you found]`
```

### Task 9: Wire explorer prompt into prompt builder

**File:** `internal/agent/prompts/builder.go` or wherever BuildFullPrompt handles role-based prompts

Add a case for the "explorer" role:

```go
case "explorer":
    return ExplorerPrompt
```

### Task 10: Wire model_tier into model resolution

**File:** `internal/llm/resolver.go` or `internal/llm/model_picker.go`

Read the existing model resolution code. If there's already a tier/capability system, wire `model_tier: "fast"` to it. If not, add a simple tier lookup:

```go
// ResolveModelTier resolves a model tier name to a concrete model ID.
// Tiers: "fast" (small/cheap model), "default" (standard model), "strong" (most capable).
func (r *Resolver) ResolveModelTier(tier string, fallback string) string {
    if tier == "" || tier == "default" {
        return fallback
    }
    // Check config for tier→model mapping
    if model, ok := r.tierModels[tier]; ok {
        return model
    }
    // Fall back to default model
    return fallback
}
```

Wire into the agent spec loading so `model_tier` in the JSON5 definition resolves to a concrete model at dispatch time.

### Task 11: Wire explore intent into dispatcher

**File:** `internal/agent/dispatcher.go` or `internal/agent/intent.go`

Read the existing intent classification. Add routing for explore/search intents to the explore agent:

```go
// In the intent→agent mapping:
case IntentExplore, IntentSearch:
    return "explore"
```

If `IntentExplore` doesn't exist, add it to the intent enum and classification keywords ("explore", "find", "search codebase", "where is", "locate").

### Task 12: Tests

**File:** `internal/agent/prompts/explorer_test.go` (new)

- `TestExplorerPromptReadOnly` — contains "READ-ONLY" and "STRICTLY PROHIBITED"
- `TestExplorerPromptStructure` — contains report format sections

**File:** `internal/llm/resolver_test.go` (extend existing)

- `TestResolveModelTierFast` — resolves "fast" to configured model
- `TestResolveModelTierFallback` — falls back to default when tier not configured
- `TestResolveModelTierEmpty` — empty tier returns fallback

## Self-Verification Checklist

- [ ] `go build ./internal/security/... ./internal/agent/... ./internal/llm/...` compiles
- [ ] `go test ./internal/security/... -race -run "TestInjection|TestScan"` passes
- [ ] `go test ./internal/agent/... -race -run TestExplorer` passes
- [ ] `go test ./internal/llm/... -race -run TestResolveModelTier` passes
- [ ] `config/agents/explore.json5` parses without error
- [ ] Bash injection patterns don't false-positive on normal commands
- [ ] Secret scanner detects all listed patterns
- [ ] Explore agent has read-only tool restrictions
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] Injection patterns cover: IFS, brace expansion, unicode whitespace, process substitution, control chars
- [ ] Injection checks integrated into existing 5-stage pipeline (not a separate path)
- [ ] Secret scanner is additive to existing obfuscator (doesn't replace it)
- [ ] Secret scanner wired into shell output and file_read for secret-prone files
- [ ] Explore agent definition has correct tool allow/deny
- [ ] Explore agent uses model_tier "fast" with resolver fallback
- [ ] Explorer prompt enforces read-only behavior
- [ ] Explore intent routed in dispatcher
- [ ] No debug artifacts, no TODOs, no placeholder values
