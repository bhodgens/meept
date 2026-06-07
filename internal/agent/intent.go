package agent

import "github.com/caimlas/meept/internal/config"

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

	// Pair channel (dual-agent conversation)
	IntentPair IntentType = "pair"

	// Collaboration (peer/differential modes)
	IntentCollaborate IntentType = "collaborate"

	// Skill invocation
	IntentSkill IntentType = "skill"

	// Compound (multi-intent)
	IntentCompound IntentType = "compound"

	// Clarification (inline)
	IntentClarify IntentType = "clarify"
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
		IntentAnalyze, IntentSearch, IntentResearch, IntentClarify:
		return CategoryInline
	case IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit, IntentSchedule, IntentPair, IntentCollaborate:
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
		return config.AgentIDChat
	case IntentCode, IntentReview:
		return config.AgentIDCoder
	case IntentDebug:
		return config.AgentIDDebugger
	case IntentPlan:
		return config.AgentIDPlanner
	case IntentAnalyze, IntentSearch, IntentResearch:
		return config.AgentIDAnalyst
	case IntentPair, IntentCollaborate:
		return config.AgentIDAnalyst
	case IntentGit:
		return config.AgentIDCommitter
	case IntentSchedule:
		return config.AgentIDScheduler
	case IntentSecurity:
		return config.AgentIDChat
	case IntentToolUse:
		return config.AgentIDCoder
	case IntentSkill:
		return "skill"
	case IntentCompound:
		return "orchestrator"
	case IntentClarify:
		return config.AgentIDChat
	default:
		return config.AgentIDChat
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
	case IntentCode, IntentDebug, IntentPlan, IntentSchedule, IntentGit, IntentCompound, IntentCollaborate:
		return true
	case IntentPair:
		return false // pair sessions don't create step-based tasks
	default:
		return false
	}
}

// ShouldDispatchAsync returns true if the intent should be dispatched asynchronously.
func (t IntentType) ShouldDispatchAsync(requiresPlanning bool) bool {
	switch t {
	case IntentCode, IntentDebug, IntentPlan, IntentGit, IntentCompound, IntentPair, IntentCollaborate:
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
		IntentSecurity, IntentToolUse, IntentSkill, IntentPair, IntentCollaborate, IntentCompound, IntentClarify:
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
		return []string{string(IntentReport), "what did you", "summary", "progress"}
	case IntentRecall:
		return []string{"remember", string(IntentRecall), "last time"}
	case IntentPlatform:
		return []string{"capabilities", "what can you", string(IntentPlatform)}
	case IntentStatus:
		return []string{"status", "how are things", "check status"}
	case IntentCode:
		return []string{"implement", "create", "add feature", KeywordRefactor}
	case IntentDebug:
		return []string{KeywordFix + " bug", string(MessageTypeError), "broken", "not working"}
	case IntentReview:
		return []string{"review pr", "check code", "code review"}
	case IntentGit:
		return []string{KeywordCommit, "push", "pull", "merge", "branch"}
	case IntentSchedule:
		return []string{"remind", string(IntentSchedule), "alarm", "at "}
	case IntentPlan:
		return []string{string(IntentPlan), KeywordDesign, "architect", "how should i"}
	case IntentAnalyze, IntentSearch:
		return []string{"research", string(IntentAnalyze), KeywordExplain, "search"}
	case IntentResearch:
		return []string{"research", "investigate", "deep dive", "study"}
	case IntentSecurity:
		return []string{"security", "vulnerability", "exploit", "safety"}
	case IntentToolUse:
		return []string{"use tool", "execute", "run command"}
	case IntentSkill:
		return []string{"/skill", "invoke", "run skill"}
	case IntentPair:
		return []string{"debate", "brainstorm", "explore", "discuss", "pair", "collaborate"}
	case IntentCollaborate:
		return []string{"collaborate", "pair program", "debate", "a/b test", "differential", "compare approaches"}
	case IntentCompound:
		return []string{"and also", "as well as", "plus"}
	case IntentClarify:
		return []string{"clarify", "unsure", "ambiguous"}
	default:
		return nil
	}
}

// Action keyword constants used in routing tables, review policies, and capability builders.
// These represent keyword-level triggers, distinct from IntentType values.
const (
	KeywordRefactor = "refactor"
	KeywordCommit   = "commit"
	KeywordFix      = "fix"
	KeywordDesign   = "design"
	KeywordExplain  = "explain"
)
