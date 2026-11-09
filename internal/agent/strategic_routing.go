package agent

import (
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// RoutingTable maps intent types to actor/reviewer agent IDs.
// It replaces the hardcoded selectActorAgent/selectReviewerAgent switch
// statements in strategic.go.
type RoutingTable struct {
	actor            map[string]string // intent → actor agent ID
	reviewer         map[string]string // intent → reviewer agent ID
	fallbackActor    string
	fallbackReviewer string
}

// NewDefaultRoutingTable returns the routing table matching current hardcoded
// behavior in selectActorAgent/selectReviewerAgent (strategic.go:761-782).
func NewDefaultRoutingTable() *RoutingTable {
	rt := &RoutingTable{
		actor:            make(map[string]string),
		reviewer:         make(map[string]string),
		fallbackActor:    config.AgentIDCoder,
		fallbackReviewer: config.AgentIDPlanner,
	}
	// Code and compound intents route to coder/planner.
	rt.actor[string(IntentCode)] = config.AgentIDCoder
	rt.actor[string(IntentCompound)] = config.AgentIDCoder
	rt.reviewer[string(IntentCode)] = config.AgentIDPlanner
	rt.reviewer[string(IntentCompound)] = config.AgentIDPlanner

	// Debug intent routes to debugger/analyst.
	rt.actor[string(IntentDebug)] = config.AgentIDDebugger
	rt.reviewer[string(IntentDebug)] = config.AgentIDAnalyst

	return rt
}

// ActorFor returns the actor agent ID for the given intent, or the fallback
// actor when no explicit route exists.
func (rt *RoutingTable) ActorFor(intent string) string {
	if rt == nil {
		return config.AgentIDCoder
	}
	if id, ok := rt.actor[intent]; ok {
		return id
	}
	return rt.fallbackActor
}

// ReviewerFor returns the reviewer agent ID for the given intent, or the
// fallback reviewer when no explicit route exists.
func (rt *RoutingTable) ReviewerFor(intent string) string {
	if rt == nil {
		return config.AgentIDPlanner
	}
	if id, ok := rt.reviewer[intent]; ok {
		return id
	}
	return rt.fallbackReviewer
}

// SetRoute allows overriding (or adding) a route for a specific intent.
// Pass empty strings for actorID or reviewerID to leave that side unchanged.
func (rt *RoutingTable) SetRoute(intent, actorID, reviewerID string) {
	if rt == nil {
		return
	}
	if actorID != "" {
		rt.actor[intent] = actorID
	}
	if reviewerID != "" {
		rt.reviewer[intent] = reviewerID
	}
}

// maxHintDescriptionLen is the truncation limit for agent descriptions in
// the planner prompt hint section.
const maxHintDescriptionLen = 80

// BuildPlannerPromptHint generates the "Available tool hints" section of the
// planner prompt from the agent registry, replacing the hardcoded list that
// previously lived in the plannerPromptTemplate const. The hint lists each
// enabled executor agent (excluding the planner itself) with its description
// or purpose truncated to 80 characters.
//
// Returns an empty string if registry is nil, so the caller can omit the
// section entirely when no registry is available.
func BuildPlannerPromptHint(registry *AgentRegistry) string {
	if registry == nil {
		return ""
	}

	specs := registry.ListSpecs()
	if len(specs) == 0 {
		return ""
	}

	var b strings.Builder
	for _, spec := range specs {
		if spec == nil {
			continue
		}
		// Only include enabled executor agents.
		if !spec.Enabled || spec.Role != RoleExecutor {
			continue
		}
		// Skip the planner itself — it shouldn't route to itself.
		if spec.ID == config.AgentIDPlanner {
			continue
		}

		desc := spec.Description
		if desc == "" {
			desc = spec.Purpose
		}
		if len(desc) > maxHintDescriptionLen {
			desc = desc[:maxHintDescriptionLen]
		}

		b.WriteString("- \"")
		b.WriteString(spec.ID)
		b.WriteString("\" → ")
		b.WriteString(desc)
		b.WriteString("\n")
	}

	return b.String()
}

// SecurityKeywords returns the list of keywords that trigger pair-session
// routing for code/debug intents. This centralizes the list hardcoded at
// strategic.go:689-697 so it can be reused and kept in sync.
func SecurityKeywords() []string {
	return []string{
		"security", "authentication", "authorization",
		"encryption", "credential", "password", "token",
		"vulnerable", "vulnerability", "cve",
	}
}
