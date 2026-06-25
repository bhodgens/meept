package agent

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/preferences"
)

// InstructionParser extracts structured instructions from natural language input.
type InstructionParser struct {
	logger logger
}

type logger interface { //nolint:unused -- reserved for future instruction parsing
	Debug(string, ...any)
	Warn(string, ...any)
	Info(string, ...any)
}

// NewInstructionParser creates a new parser with optional logging.
func NewInstructionParser() *InstructionParser {
	return &InstructionParser{}
}

// Parse extracts a structured instruction from natural language input.
func (p *InstructionParser) Parse(ctx context.Context, input string) (*preferences.ParsedInstruction, error) {
	instr := &preferences.ParsedInstruction{
		RawInput:   input,
		Confidence: 0.0,
		Trigger:    preferences.TriggerConfig{Conditions: make(map[string]string)},
		Action:     preferences.ActionConfig{Args: make(map[string]any)},
		Scope:      "global",
		Priority:   "normal",
		CreatedAt:  time.Now(),
	}

	// Extract trigger
	trigger, confidence := p.extractTrigger(input)
	instr.Trigger = trigger
	instr.Confidence = confidence

	// Extract action
	action := p.extractAction(input)
	instr.Action = action

	// Extract scope
	instr.Scope = p.extractScope(input)

	// Extract priority
	instr.Priority = p.extractPriority(input)

	// Boost confidence if we matched clear patterns
	if confidence > 0.7 && instr.Action.Tool != "" {
		instr.Confidence = minFloat64(1.0, confidence+0.1)
	}

	return instr, nil
}

// extractTrigger identifies the trigger type and pattern from input.
func (p *InstructionParser) extractTrigger(input string) (preferences.TriggerConfig, float64) {
	input = strings.ToLower(input)

	// Cron patterns
	cronPatterns := []struct {
		regex   string
		keyword string
	}{
		{`every (?:day|day) at (\d{1,2})(?::(\d{2}))?\s*(am|pm)?`, "daily"},
		{`every (monday|tuesday|wednesday|thursday|friday|saturday|sunday) at (\d{1,2})(?::(\d{2}))?\s*(am|pm)?`, "weekly"},
		{`at (\d{1,2}):(\d{2})\s*(am|pm)? (?:every day|daily)`, "daily"},
	}

	for _, cp := range cronPatterns {
		re := regexp.MustCompile(cp.regex)
		if matches := re.FindStringSubmatch(input); matches != nil {
			cronExpr := p.parseToCron(matches, cp.keyword)
			return preferences.TriggerConfig{
				Type:    "cron",
				Pattern: cronExpr,
			}, 0.85
		}
	}

	// Post-hook patterns
	postHookPatterns := []struct {
		regex   string
		tool    string
		pattern string
	}{
		{`after (?:i |you )?(write|save|edit|touch|modify)(?:d)? (?:.*?\.)?(go|ts|js|py|rs|md|txt) files?`, "write_file", "*.$2"},
		{`after (?:i |you )?(?:run|execute|commit)`, "tool_complete", "*"},
		{`when (?:i |you )?(?:start|begin)`, "session_start", "*"},
		{`whenever`, "event", "*"},
	}

	for _, pp := range postHookPatterns {
		re := regexp.MustCompile(pp.regex)
		if matches := re.FindStringSubmatch(input); matches != nil {
			return preferences.TriggerConfig{
				Type:    "post_hook",
				Pattern: pp.tool + ":" + pp.pattern,
			}, 0.75
		}
	}

	// Git hook patterns
	gitPatterns := []struct {
		regex string
		hook  string
	}{
		{`(?:before|pre)[-_ ]commit`, "pre_commit"},
		{`(?:after|post)[-_ ]commit`, "post_commit"},
		{`(?:before|pre)[-_ ]push`, "pre_push"},
		{`(?:after|post)[-_ ]push`, "post_push"},
	}

	for _, gp := range gitPatterns {
		re := regexp.MustCompile(gp.regex)
		if re.MatchString(input) {
			return preferences.TriggerConfig{
				Type:    "git",
				Pattern: gp.hook,
			}, 0.80
		}
	}

	// Intent patterns
	intentPatterns := []struct {
		regex  string
		intent string
	}{
		{`(?:when|whenever) (?:i |you )?(ask|research|search)`, "research"},
		{`(?:when|whenever) (?:i |you )?(write|code|implement)`, "code"},
		{`(?:when|whenever) (?:i |you )?(fix|debug|troubleshoot)`, "debug"},
	}

	for _, ip := range intentPatterns {
		re := regexp.MustCompile(ip.regex)
		if re.MatchString(input) {
			return preferences.TriggerConfig{
				Type:    "intent",
				Pattern: ip.intent,
			}, 0.70
		}
	}

	return preferences.TriggerConfig{
		Type:    "manual",
		Pattern: "",
	}, 0.3
}

// parseToCron converts regex matches to a cron expression.
func (p *InstructionParser) parseToCron(matches []string, keyword string) string {
	switch keyword {
	case "daily":
		hour := matches[1]
		minute := "0"
		if len(matches) > 2 && matches[2] != "" {
			minute = matches[2]
		}
		h := mustParseInt(hour)
		if len(matches) > 3 && strings.ToLower(matches[3]) == "pm" && hour != "12" {
			h = h + 12
		} else if len(matches) > 3 && strings.ToLower(matches[3]) == "am" && hour == "12" {
			hour = "0"
		}
		_ = h // WIP: user-instructions feature, cron formatting incomplete
		return minute + " " + matches[1] + " * * *"
	case "weekly":
		day := matches[1]
		hour := matches[2]
		minute := "0"
		if len(matches) > 3 && matches[3] != "" {
			minute = matches[3]
		}
		weekday := dayOfWeek(day)
		return minute + " " + hour + " * * " + weekday
	}
	return "0 9 * * *"
}

func mustParseInt(s string) int {
	result := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		result = result*10 + int(c-'0')
	}
	return result
}

func dayOfWeek(day string) string {
	days := map[string]string{
		"sunday": "0", "monday": "1", "tuesday": "2", "wednesday": "3",
		"thursday": "4", "friday": "5", "saturday": "6",
	}
	if w, ok := days[strings.ToLower(day)]; ok {
		return w
	}
	return "1"
}

// extractAction identifies the action tool and arguments from input.
func (p *InstructionParser) extractAction(input string) preferences.ActionConfig {
	input = strings.ToLower(input)
	action := preferences.ActionConfig{Args: make(map[string]any)}

	shellPatterns := []struct {
		regex   string
		command string
	}{
		{`run tests?`, "go test ./..."},
		{`run linter|lint`, "golangci-lint run"},
		{`build`, "go build ./..."},
		{`format|fmt`, "gofmt -w ."},
		{`git status`, "git status"},
		{`git diff`, "git diff"},
	}

	for _, sp := range shellPatterns {
		re := regexp.MustCompile(sp.regex)
		if re.MatchString(input) {
			action.Tool = "shell_execute"
			action.Args["command"] = sp.command
			return action
		}
	}

	if strings.Contains(input, "remember") || strings.Contains(input, "save to memory") {
		action.Tool = "memory_retain"
		return action
	}

	if strings.Contains(input, "notify") || strings.Contains(input, "remind") {
		action.Tool = "notification"
		return action
	}

	agentPatterns := map[string]string{
		`coder`: "coder", `debugger`: "debugger", `planner`: "planner",
		`analyst`: "analyst", `researcher`: "researcher", `reviewer`: "code-reviewer",
	}

	for pattern, agentID := range agentPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(input) {
			action.Tool = "agent_trigger"
			action.AgentID = agentID
			return action
		}
	}

	runRe := regexp.MustCompile(`(?:run|execute|do)\s+(.+?)(?:\s+(?:after|when|whenever|before)|$)`)
	if matches := runRe.FindStringSubmatch(input); matches != nil {
		action.Tool = "shell_execute"
		action.Args["command"] = strings.TrimSpace(matches[1])
		return action
	}

	return action
}

// extractScope determines if the instruction is project-local or global.
func (p *InstructionParser) extractScope(input string) string {
	input = strings.ToLower(input)
	projectKeywords := []string{"in this project", "in this repo", "for this project", "here"}
	for _, kw := range projectKeywords {
		if strings.Contains(input, kw) {
			return "project"
		}
	}
	return "global"
}

// extractPriority determines the priority level from input.
func (p *InstructionParser) extractPriority(input string) string {
	input = strings.ToLower(input)
	priorityKeywords := map[string]string{
		"critical": "critical", "urgent": "critical",
		"important": "high", "high priority": "high",
		"low priority": "low", "when convenient": "low",
	}
	for kw, priority := range priorityKeywords {
		if strings.Contains(input, kw) {
			return priority
		}
	}
	return "normal"
}

func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
