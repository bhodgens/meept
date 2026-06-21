package preferences

import (
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/tools"
)

// InstructionVerifier validates instructions before they are persisted or executed.
type InstructionVerifier struct {
	toolRegistry *tools.Registry
	safeCommands []string
}

// VerificationResult holds the outcome of a verification check.
type VerificationResult struct {
	Valid              bool
	RiskLevel          string   // "low", "medium", "high"
	ConfirmationNeeded bool
	Errors             []string
	Warnings           []string
}

// NewInstructionVerifier creates a verifier with the given dependencies.
func NewInstructionVerifier(registry *tools.Registry) *InstructionVerifier {
	return &InstructionVerifier{
		toolRegistry: registry,
		safeCommands: []string{
			"go test ./...",
			"go build ./...",
			"go fmt ./...",
			"gofmt -w .",
			"git status",
			"git diff",
			"git log",
			"ls",
			"cat",
			"echo",
		},
	}
}

// Verify validates a parsed instruction and returns a result.
func (v *InstructionVerifier) Verify(instr *ParsedInstruction) VerificationResult {
	result := VerificationResult{
		Valid:    true,
		RiskLevel: "low",
		Errors:   []string{},
		Warnings: []string{},
	}

	if instr == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "instruction is nil")
		return result
	}
	if instr.Action.Tool == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "action tool is empty")
	}

	// Check tool existence against registry
	if !v.checkToolExists(instr.Action.Tool) {
		// Unknown tools are allowed for shell commands
		if instr.Action.Tool != "shell" && instr.Action.Tool != "notification" &&
			instr.Action.Tool != "memory_write" && instr.Action.Tool != "agent_trigger" {
			result.Warnings = append(result.Warnings, "tool may not be registered: "+instr.Action.Tool)
		}
	}

	// Assess risk level
	risk := v.assessRisk(instr)
	result.RiskLevel = risk

	if result.RiskLevel == "medium" {
		result.ConfirmationNeeded = true
	} else if result.RiskLevel == "high" {
		result.ConfirmationNeeded = true
	}

	return result
}

// checkToolExists verifies whether the given tool name is registered.
func (v *InstructionVerifier) checkToolExists(toolName string) bool {
	if toolName == "shell" || toolName == "notification" ||
		toolName == "memory_write" || toolName == "memory_retain" ||
		toolName == "agent_trigger" {
		return true
	}
	if v.toolRegistry != nil {
		return v.toolRegistry.Get(toolName) != nil
	}
	return true
}

// assessRisk evaluates the risk level of an instruction.
func (v *InstructionVerifier) assessRisk(instr *ParsedInstruction) string {
	if instr == nil {
		return "low"
	}

	tool := strings.ToLower(instr.Action.Tool)

	switch tool {
	case "agent_trigger", "git_commit", "git_push":
		return "medium"
	case "shell":
		return v.assessShellRisk(instr)
	default:
		return v.assessToolRisk(tool)
	}
}

// assessShellRisk evaluates risk for shell commands.
func (v *InstructionVerifier) assessShellRisk(instr *ParsedInstruction) string {
	if instr.Action.Args == nil {
		return "low"
	}

	if cmd, ok := instr.Action.Args["command"].(string); ok && cmd != "" {
		cmdLower := strings.ToLower(cmd)

		// Check known-safe commands
		for _, safe := range v.safeCommands {
			if strings.Contains(cmdLower, safe) {
				return "low"
			}
		}

		// Check high-risk shell patterns
		highRiskPatterns := []*regexp.Regexp{
			regexp.MustCompile(`rm\s+(-[a-z]*r(-f)?|-f(-r)?)`),
			regexp.MustCompile(`curl\s+.*\|\s*(ba)?sh`),
			regexp.MustCompile(`wget\s+.*\|\s*(ba)?sh`),
			regexp.MustCompile(`sudo\s+`),
			regexp.MustCompile(`chmod\s+777`),
			regexp.MustCompile(`dd\s+if=`),
			regexp.MustCompile(`>(/etc|/dev/)`),
			regexp.MustCompile(`mkfs|fdisk`),
		}

		for _, pat := range highRiskPatterns {
			if pat.MatchString(cmdLower) {
				return "high"
			}
		}

		// Medium risk: git operations that push or force
		mediumRiskPatterns := []*regexp.Regexp{
			regexp.MustCompile(`git\s+push`),
			regexp.MustCompile(`git\s+push\s+.*--force`),
			regexp.MustCompile(`git\s+reset\s+--hard`),
			regexp.MustCompile(`git\s+clean`),
			regexp.MustCompile(`chmod\s+\d+`),
			regexp.MustCompile(`chown`),
		}

		for _, pat := range mediumRiskPatterns {
			if pat.MatchString(cmdLower) {
				return "medium"
			}
		}

		// Low risk: safe tool invocations
		lowRiskPatterns := []*regexp.Regexp{
			regexp.MustCompile(`^(go\s+(test|build|fmt|vet|run)\s*)`),
			regexp.MustCompile(`^git\s+(status|diff|log|commit|pull|fetch)\s*`),
			regexp.MustCompile(`^(ls|cat|echo|head|tail|wc|grep|find)\s+`),
		}

		for _, pat := range lowRiskPatterns {
			if pat.MatchString(cmdLower) {
				return "low"
			}
		}

		// Default for unknown shell commands: medium
		return "medium"
	}

	return "low"
}

// assessToolRisk evaluates risk for non-shell tools.
func (v *InstructionVerifier) assessToolRisk(tool string) string {
	switch tool {
	case "file_write", "delete_file", "web_fetch", "web_search":
		return "medium"
	case "shell":
		return "medium"
	default:
		return "low"
	}
}

// GetKnownSafeCommands returns a copy of the safe commands list.
func (v *InstructionVerifier) GetKnownSafeCommands() []string {
	result := make([]string, len(v.safeCommands))
	copy(result, v.safeCommands)
	return result
}

// SetKnownSafeCommands replaces the known-safe commands list.
func (v *InstructionVerifier) SetKnownSafeCommands(cmds []string) {
	v.safeCommands = make([]string, len(cmds))
	copy(v.safeCommands, cmds)
}
