package agent

// IntentType represents a classified user intent.
type IntentType string

const (
	// Unknown
	IntentUnknown IntentType = "unknown"

	// Conversational (inline handling)
	IntentChat     IntentType = "chat"
	IntentReport   IntentType = "report"
	IntentRecall   IntentType = "recall"
	IntentPlatform IntentType = "platform"
	IntentStatus   IntentType = "status"

	// Execution (async to orchestrator)
	IntentCode     IntentType = "code"
	IntentDebug    IntentType = "debug"
	IntentReview   IntentType = "review"
	IntentPlan     IntentType = "plan"
	IntentGit      IntentType = "git"
	IntentSchedule IntentType = "schedule"

	// Analysis (inline)
	IntentAnalyze  IntentType = "analyze"
	IntentSearch   IntentType = "search"
	IntentResearch IntentType = "research"

	// Security / Tool control
	IntentSecurity IntentType = "security"
	IntentToolUse  IntentType = "tooluse"

	// Skill invocation
	IntentSkill IntentType = "skill"

	// Compound (multi-intent)
	IntentCompound IntentType = "compound"
)

// IntentCategory groups intents by routing behavior.
type IntentCategory string

const (
	CategoryInline IntentCategory = "inline"
	CategoryDefer  IntentCategory = "defer"
)

// Category returns the routing category for an intent.
func (t IntentType) Category() IntentCategory {
	switch t {
	case IntentChat, IntentReport, IntentRecall, IntentPlatform, IntentStatus,
		IntentAnalyze, IntentSearch, IntentResearch:
		return CategoryInline
	case IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit, IntentSchedule:
		return CategoryDefer
	case IntentCompound:
		return CategoryDefer
	case IntentSkill:
		return CategoryInline
	case IntentSecurity, IntentToolUse:
		return CategoryDefer
	default:
		return CategoryInline
	}
}

// DefaultAgent returns the default agent for an intent.
func (t IntentType) DefaultAgent() string {
	switch t {
	case IntentChat, IntentReport, IntentRecall, IntentPlatform, IntentStatus:
		return "chat"
	case IntentCode, IntentReview:
		return "coder"
	case IntentDebug:
		return "debugger"
	case IntentPlan:
		return "planner"
	case IntentAnalyze, IntentSearch, IntentResearch:
		return "analyst"
	case IntentGit:
		return "committer"
	case IntentSchedule:
		return "scheduler"
	case IntentSecurity:
		return "chat"
	case IntentToolUse:
		return "coder"
	case IntentSkill:
		return "skill"
	case IntentCompound:
		return "orchestrator"
	default:
		return "chat"
	}
}

// RequiresPlanning returns true if the intent benefits from orchestration.
func (t IntentType) RequiresPlanning() bool {
	switch t {
	case IntentCode, IntentPlan, IntentCompound:
		return true
	default:
		return false
	}
}

// ShouldCreateTask returns true if the intent should create a trackable task.
func (t IntentType) ShouldCreateTask() bool {
	switch t {
	case IntentCode, IntentDebug, IntentPlan, IntentSchedule, IntentGit, IntentCompound:
		return true
	default:
		return false
	}
}

// ShouldDispatchAsync returns true if the intent should be dispatched asynchronously.
func (t IntentType) ShouldDispatchAsync(requiresPlanning bool) bool {
	switch t {
	case IntentCode, IntentDebug, IntentPlan, IntentGit, IntentCompound:
		return true
	case IntentSchedule:
		// Only dispatch async for schedule if it requires planning
		return requiresPlanning
	default:
		return false
	}
}

// IsValid checks if a string is a valid intent type.
func IsValidIntentType(s string) bool {
	switch IntentType(s) {
	case IntentChat, IntentReport, IntentRecall, IntentPlatform, IntentStatus,
		IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit,
		IntentSchedule, IntentAnalyze, IntentSearch, IntentResearch,
		IntentSecurity, IntentToolUse, IntentSkill, IntentCompound:
		return true
	}
	return false
}

// Keywords returns common trigger phrases for documentation/logging.
func (t IntentType) Keywords() []string {
	switch t {
	case IntentChat:
		return []string{"hello", "hi", "thanks", "help"}
	case IntentReport:
		return []string{"report", "what did you", "summary", "progress"}
	case IntentRecall:
		return []string{"remember", "recall", "last time"}
	case IntentPlatform:
		return []string{"capabilities", "what can you", "platform"}
	case IntentStatus:
		return []string{"status", "how are things", "check status"}
	case IntentCode:
		return []string{"implement", "create", "add feature", "refactor"}
	case IntentDebug:
		return []string{"fix bug", "error", "broken", "not working"}
	case IntentReview:
		return []string{"review pr", "check code", "code review"}
	case IntentGit:
		return []string{"commit", "push", "pull", "merge", "branch"}
	case IntentSchedule:
		return []string{"remind", "schedule", "alarm", "at "}
	case IntentPlan:
		return []string{"plan", "design", "architect", "how should i"}
	case IntentAnalyze, IntentSearch:
		return []string{"research", "analyze", "explain", "search"}
	case IntentResearch:
		return []string{"research", "investigate", "deep dive", "study"}
	case IntentSecurity:
		return []string{"security", "vulnerability", "exploit", "safety"}
	case IntentToolUse:
		return []string{"use tool", "execute", "run command"}
	case IntentSkill:
		return []string{"/skill", "invoke", "run skill"}
	case IntentCompound:
		return []string{"and also", "as well as", "plus"}
	default:
		return nil
	}
}
