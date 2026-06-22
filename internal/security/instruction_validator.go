package security

import (
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/preferences"
)

// InstructionValidator validates user instructions for security risks.
type InstructionValidator struct {
	engine             *Engine
	safeCommands       []string
	highRiskPatterns   []*regexp.Regexp
	mediumRiskPatterns []*regexp.Regexp
}

// NewInstructionValidator creates a new instruction validator.
func NewInstructionValidator(engine *Engine) *InstructionValidator {
	return &InstructionValidator{
		engine: engine,
		safeCommands: []string{
			"go test ./...",
			"go build ./...",
			"go fmt ./...",
			"gofmt -w .",
			"git status",
			"git diff",
			"git log",
			"ls ", "cat ", "echo ",
			"head ", "tail ", "wc ",
			"grep ", "find ",
		},
		highRiskPatterns: []*regexp.Regexp{
			regexp.MustCompile(`rm\s+(-[a-z]*r(-f)?|-f(-r)?)`),
			regexp.MustCompile(`curl\s+.*\|\s*(ba)?sh`),
			regexp.MustCompile(`wget\s+.*\|\s*(ba)?sh`),
			regexp.MustCompile(`sudo\s+`),
			regexp.MustCompile(`chmod\s+777`),
			regexp.MustCompile(`dd\s+if=`),
			regexp.MustCompile(`>(/etc|/dev/)`),
			regexp.MustCompile(`mkfs|fdisk`),
		},
		mediumRiskPatterns: []*regexp.Regexp{
			regexp.MustCompile(`git\s+push`),
			regexp.MustCompile(`git\s+push\s+.*--force`),
			regexp.MustCompile(`git\s+reset\s+--hard`),
			regexp.MustCompile(`git\s+clean`),
			regexp.MustCompile(`chmod\s+\d+`),
			regexp.MustCompile(`chown`),
		},
	}
}

// ValidationResult holds the result of instruction validation.
type ValidationResult struct {
	Valid              bool
	RiskLevel          string   // "low", "medium", "high"
	ConfirmationNeeded bool
	Errors             []string
	Warnings           []string
}

// Validate validates a parsed instruction and returns the result.
func (v *InstructionValidator) Validate(instr *preferences.ParsedInstruction) ValidationResult {
	result := ValidationResult{
		Valid:     true,
		RiskLevel: "low",
		Errors:    []string{},
		Warnings:  []string{},
	}

	if instr == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "instruction is nil")
		return result
	}

	if instr.Action.Tool == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "action tool is empty")
		return result
	}

	// Assess risk based on action type
	risk := v.assessRisk(instr)
	result.RiskLevel = risk

	// Medium and high risk require confirmation
	if risk == "medium" || risk == "high" {
		result.ConfirmationNeeded = true
	}

	return result
}

// assessRisk evaluates the risk level of an instruction.
func (v *InstructionValidator) assessRisk(instr *preferences.ParsedInstruction) string {
	if instr == nil {
		return "low"
	}

	tool := strings.ToLower(instr.Action.Tool)

	switch tool {
	case "shell_execute", "shell":
		return v.assessShellRisk(instr)
	case "agent_trigger":
		return "medium"
	case "git_commit", "git_push":
		return "medium"
	case "file_write", "file_delete":
		return "medium"
	case "web_fetch", "web_search":
		return "low"
	case "memory_retain", "memory_write":
		return "low"
	case "notification":
		return "low"
	default:
		return "low"
	}
}

// assessShellRisk evaluates risk for shell execute actions.
func (v *InstructionValidator) assessShellRisk(instr *preferences.ParsedInstruction) string {
	if instr.Action.Args == nil {
		return "low"
	}

	cmd, ok := instr.Action.Args["command"].(string)
	if !ok || cmd == "" {
		return "low"
	}

	cmdLower := strings.ToLower(cmd)

	// Check known-safe commands first
	for _, safe := range v.safeCommands {
		if strings.Contains(cmdLower, safe) {
			return "low"
		}
	}

	// Check high-risk patterns
	for _, pat := range v.highRiskPatterns {
		if pat.MatchString(cmdLower) {
			return "high"
		}
	}

	// Check medium-risk patterns
	for _, pat := range v.mediumRiskPatterns {
		if pat.MatchString(cmdLower) {
			return "medium"
		}
	}

	// Check for low-risk patterns (build, test, git read operations)
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

	// Default for unknown commands: medium risk
	return "medium"
}

// IsHighRiskCommand checks if a command matches high-risk patterns.
func (v *InstructionValidator) IsHighRiskCommand(cmd string) bool {
	cmdLower := strings.ToLower(cmd)
	for _, pat := range v.highRiskPatterns {
		if pat.MatchString(cmdLower) {
			return true
		}
	}
	return false
}

// IsKnownSafeCommand checks if a command is in the known-safe allowlist.
func (v *InstructionValidator) IsKnownSafeCommand(cmd string) bool {
	cmdLower := strings.ToLower(cmd)
	for _, safe := range v.safeCommands {
		if strings.Contains(cmdLower, safe) {
			return true
		}
	}
	return false
}
