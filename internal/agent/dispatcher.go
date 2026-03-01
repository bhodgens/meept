package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
)

// Intent represents the classified intent of a user message.
type Intent struct {
	// Type is the high-level intent category.
	Type string `json:"type"`
	// Confidence is the confidence score [0.0, 1.0].
	Confidence float64 `json:"confidence"`
	// AgentType is the specialist agent to route to.
	AgentType string `json:"agent_type"`
	// MemoryRefs are relevant memory IDs to pass along.
	MemoryRefs []string `json:"memory_refs,omitempty"`
	// RequiresPlanning indicates if the task needs planning first.
	RequiresPlanning bool `json:"requires_planning"`
	// Summary is a brief description of the intent.
	Summary string `json:"summary,omitempty"`
}

// DispatchResult is the result of dispatching a request.
type DispatchResult struct {
	// Task is the created task if any.
	Task *task.Task `json:"task,omitempty"`
	// AgentID is the agent that will handle the request.
	AgentID string `json:"agent_id"`
	// Intent is the classified intent.
	Intent *Intent `json:"intent"`
	// Response is the direct response if no agent delegation needed.
	Response string `json:"response,omitempty"`
	// MemoryContext are memories retrieved for context.
	MemoryContext []memory.MemoryResult `json:"memory_context,omitempty"`
}

// Dispatcher handles intake classification and routing of requests.
type Dispatcher struct {
	registry      *AgentRegistry
	memvid        *memvid.Client
	memoryMgr     *memory.Manager
	taskStore     *task.Store
	skillRegistry *skills.Registry
	skillExecutor *skills.Executor
	logger        *slog.Logger
	classifiers   []IntentClassifier
}

// IntentClassifier is an interface for classifying intents.
type IntentClassifier interface {
	Classify(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error)
}

// DispatcherConfig holds configuration for creating a Dispatcher.
type DispatcherConfig struct {
	Registry      *AgentRegistry
	MemvidClient  *memvid.Client
	MemoryMgr     *memory.Manager
	TaskStore     *task.Store
	SkillRegistry *skills.Registry
	SkillExecutor *skills.Executor
	Logger        *slog.Logger
}

// NewDispatcher creates a new dispatcher.
func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	d := &Dispatcher{
		registry:      cfg.Registry,
		memvid:        cfg.MemvidClient,
		memoryMgr:     cfg.MemoryMgr,
		taskStore:     cfg.TaskStore,
		skillRegistry: cfg.SkillRegistry,
		skillExecutor: cfg.SkillExecutor,
		logger:        cfg.Logger,
	}

	// Add default keyword-based classifier
	d.classifiers = append(d.classifiers, &KeywordClassifier{})

	return d
}

// ClassifyAndRoute is the main entry point for the dispatcher.
func (d *Dispatcher) ClassifyAndRoute(ctx context.Context, input string, sessionID string) (*DispatchResult, error) {
	d.logger.Debug("Dispatching request", "session", sessionID, "input_len", len(input))

	// Check for explicit skill invocation (/skill-name)
	if strings.HasPrefix(input, "/") {
		skillName, skillInput := d.parseSkillInvocation(input)
		if skill := d.getSkill(skillName); skill != nil {
			d.logger.Info("Skill invocation detected",
				"skill", skillName,
				"session", sessionID,
			)
			return d.executeSkill(ctx, skill, skillInput, sessionID)
		}
		// Not a valid skill, fall through to normal routing
	}

	// 1. Search memory for relevant context using graph-aware search
	var memoryContext []memory.MemoryResult
	if d.memvid != nil && d.memvid.IsAvailable(ctx) {
		results, err := d.memvid.Search(ctx, input, 10)
		if err != nil {
			d.logger.Warn("Memory search failed", "error", err)
		} else {
			for _, r := range results {
				memoryContext = append(memoryContext, memory.MemoryResult{
					Memory: memory.Memory{
						ID:        r.Memory.ID,
						Content:   r.Memory.Content,
						CreatedAt: r.Memory.CreatedAt,
					},
					RelevanceScore: r.RelevanceScore,
					Source:         r.Memory.Zone,
				})
			}
		}
	} else if d.memoryMgr != nil {
		// Use graph-aware search for better ranking (alpha=0.3 = 30% PageRank influence)
		results, err := d.memoryMgr.SearchWithGraph(ctx, memory.MemoryQuery{
			Query: input,
			Limit: 10,
		}, 0.3)
		if err != nil {
			d.logger.Warn("Graph-aware memory search failed, falling back", "error", err)
			// Fallback to regular search
			results, err = d.memoryMgr.Search(ctx, memory.MemoryQuery{
				Query: input,
				Limit: 10,
			})
			if err != nil {
				d.logger.Warn("Local memory search failed", "error", err)
			} else {
				memoryContext = results
			}
		} else {
			memoryContext = results
		}
	}

	// 2. Classify intent
	intent, err := d.classifyIntent(ctx, input, memoryContext)
	if err != nil {
		d.logger.Error("Intent classification failed", "error", err)
		// Default to chat agent
		intent = &Intent{
			Type:       "chat",
			Confidence: 0.5,
			AgentType:  "chat",
		}
	}

	// 3. Extract memory refs for context continuity
	intent.MemoryRefs = d.extractMemoryRefs(memoryContext)

	// 4. Create task if needed (for trackable work)
	var createdTask *task.Task
	if d.shouldCreateTask(intent) && d.taskStore != nil {
		createdTask = d.createTask(ctx, input, intent, sessionID)
	}

	// 5. Determine routing
	result := &DispatchResult{
		Task:          createdTask,
		AgentID:       intent.AgentType,
		Intent:        intent,
		MemoryContext: memoryContext,
	}

	d.logger.Info("Dispatched request",
		"agent", intent.AgentType,
		"intent_type", intent.Type,
		"confidence", intent.Confidence,
		"memory_refs", len(intent.MemoryRefs),
		"has_task", createdTask != nil,
	)

	return result, nil
}

// classifyIntent uses classifiers to determine intent.
func (d *Dispatcher) classifyIntent(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error) {
	var bestIntent *Intent
	var bestConfidence float64

	for _, classifier := range d.classifiers {
		intent, err := classifier.Classify(ctx, input, context)
		if err != nil {
			d.logger.Warn("Classifier failed", "error", err)
			continue
		}

		if intent != nil && intent.Confidence > bestConfidence {
			bestIntent = intent
			bestConfidence = intent.Confidence
		}
	}

	if bestIntent == nil {
		// Default fallback
		return &Intent{
			Type:       "chat",
			Confidence: 0.5,
			AgentType:  "chat",
			Summary:    "General conversation",
		}, nil
	}

	return bestIntent, nil
}

// extractMemoryRefs extracts memory IDs from search results.
func (d *Dispatcher) extractMemoryRefs(results []memory.MemoryResult) []string {
	refs := make([]string, 0, len(results))
	for _, r := range results {
		if r.RelevanceScore > 0.3 { // Only include reasonably relevant memories
			refs = append(refs, r.Memory.ID)
		}
	}
	return refs
}

// shouldCreateTask determines if a task should be created.
func (d *Dispatcher) shouldCreateTask(intent *Intent) bool {
	// Create tasks for work that should be trackable
	switch intent.Type {
	case "code", "debug", "plan", "schedule":
		return true
	default:
		return intent.RequiresPlanning
	}
}

// createTask creates a new task for the request.
func (d *Dispatcher) createTask(ctx context.Context, input string, intent *Intent, sessionID string) *task.Task {
	// Create task summary
	summary := intent.Summary
	if summary == "" {
		summary = truncateString(input, 100)
	}

	t := task.NewTask(summary, input)
	t.LinkSession(sessionID)

	// Store task
	if d.taskStore != nil {
		if err := d.taskStore.Create(t); err != nil {
			d.logger.Error("Failed to create task", "error", err)
			return nil
		}
	}

	return t
}

// RouteToAgent routes a dispatch result to the appropriate agent.
func (d *Dispatcher) RouteToAgent(ctx context.Context, result *DispatchResult, conversationID string) (string, error) {
	if d.registry == nil {
		return "", fmt.Errorf("no agent registry configured")
	}

	// Build context message with memory refs
	contextMsg := d.buildContextMessage(result)

	// Get the agent
	agent, err := d.registry.Get(result.AgentID)
	if err != nil {
		d.logger.Warn("Agent not found, falling back to chat", "agent", result.AgentID, "error", err)
		agent, err = d.registry.Get("chat")
		if err != nil {
			return "", fmt.Errorf("fallback agent not found: %w", err)
		}
	}

	// Run the agent
	response, err := agent.RunOnce(ctx, contextMsg, conversationID)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	// Record memory of this interaction
	if d.memvid != nil && d.memvid.IsAvailable(ctx) {
		go d.recordInteraction(context.Background(), result, response)
	}

	return response, nil
}

// buildContextMessage builds a message with memory context injected.
func (d *Dispatcher) buildContextMessage(result *DispatchResult) string {
	var parts []string

	// Add relevant memory context
	if len(result.MemoryContext) > 0 {
		parts = append(parts, "## Relevant Context\n")
		for i, m := range result.MemoryContext {
			if i >= 5 { // Limit context
				break
			}
			parts = append(parts, fmt.Sprintf("- %s\n", truncateString(m.Memory.Content, 200)))
		}
		parts = append(parts, "\n---\n\n")
	}

	// Add task context if available
	if result.Task != nil {
		parts = append(parts, fmt.Sprintf("Task ID: %s\n", result.Task.ID))
	}

	// Add the original input
	parts = append(parts, result.Intent.Summary)

	return strings.Join(parts, "")
}

// recordInteraction records the interaction to memory.
func (d *Dispatcher) recordInteraction(ctx context.Context, result *DispatchResult, response string) {
	if d.memvid == nil {
		return
	}

	content := fmt.Sprintf("User intent: %s\nAgent: %s\nResponse summary: %s",
		result.Intent.Summary,
		result.AgentID,
		truncateString(response, 500),
	)

	metadata := map[string]any{
		"intent_type": result.Intent.Type,
		"agent_id":    result.AgentID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}

	if result.Task != nil {
		metadata["task_id"] = result.Task.ID
	}

	// Use episodic zone
	episodicClient := d.memvid.WithZone("episodic")
	_, err := episodicClient.Store(ctx, content, metadata)
	if err != nil {
		d.logger.Warn("Failed to record interaction", "error", err)
	}
}

// KeywordClassifier is a simple keyword-based intent classifier.
type KeywordClassifier struct{}

// Classify classifies intent based on keywords.
func (c *KeywordClassifier) Classify(ctx context.Context, input string, context []memory.MemoryResult) (*Intent, error) {
	lower := strings.ToLower(input)

	// Define keyword patterns and their mappings
	patterns := []struct {
		keywords   []string
		intentType string
		agentType  string
		confidence float64
		planning   bool
	}{
		// Platform introspection (highest priority - matches first)
		{[]string{"what are your capabilities", "what can you do", "what tools", "what agents", "what kind of systems", "help me understand", "system access", "platform status"}, "platform", "chat", 0.9, false},

		// Code-related
		{[]string{"fix bug", "debug", "error", "exception", "crash", "not working"}, "debug", "debugger", 0.8, false},
		{[]string{"write code", "implement", "create function", "add feature", "refactor"}, "code", "coder", 0.8, false},
		{[]string{"code review", "review pr", "check code"}, "review", "coder", 0.75, false},

		// Git operations
		{[]string{"commit", "push", "pull", "merge", "branch", "git"}, "git", "committer", 0.8, false},

		// Scheduling
		{[]string{"remind", "schedule", "alarm", "timer", "at ", "tomorrow", "next week"}, "schedule", "scheduler", 0.8, false},

		// Planning
		{[]string{"plan", "design", "architect", "how should i", "break down", "decompose"}, "plan", "planner", 0.8, true},

		// Analysis/Research
		{[]string{"research", "analyze", "summarize", "explain", "what is", "how does"}, "analyze", "analyst", 0.7, false},
		{[]string{"search", "find", "look up", "google"}, "search", "analyst", 0.7, false},

		// General chat (lower priority)
		{[]string{"hello", "hi", "hey", "thanks", "thank you", "help"}, "chat", "chat", 0.6, false},
	}

	var bestMatch *Intent
	bestScore := 0.0

	for _, p := range patterns {
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				// Score based on keyword length and position
				score := p.confidence * (float64(len(kw)) / float64(len(input)+1))
				if strings.HasPrefix(lower, kw) {
					score *= 1.2 // Boost for prefix matches
				}

				if score > bestScore {
					bestScore = score
					bestMatch = &Intent{
						Type:             p.intentType,
						Confidence:       p.confidence,
						AgentType:        p.agentType,
						RequiresPlanning: p.planning,
						Summary:          extractSummary(input),
					}
				}
			}
		}
	}

	return bestMatch, nil
}

// extractSummary extracts a brief summary from input.
func extractSummary(input string) string {
	// Take first sentence or first 100 chars
	if idx := strings.IndexAny(input, ".!?"); idx > 0 && idx < 100 {
		return input[:idx+1]
	}
	return truncateString(input, 100)
}

// truncateString truncates a string to max length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// DispatcherStats returns statistics about the dispatcher.
type DispatcherStats struct {
	TotalDispatched int            `json:"total_dispatched"`
	ByAgent         map[string]int `json:"by_agent"`
	ByIntent        map[string]int `json:"by_intent"`
}

// MarshalJSON implements json.Marshaler for Intent.
func (i *Intent) MarshalJSON() ([]byte, error) {
	type Alias Intent
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(i),
	})
}

// parseSkillInvocation extracts skill name and input from a /skill-name invocation.
func (d *Dispatcher) parseSkillInvocation(input string) (string, string) {
	// Remove leading slash
	input = strings.TrimPrefix(input, "/")

	// Split on first whitespace
	parts := strings.SplitN(input, " ", 2)
	skillName := parts[0]
	skillInput := ""
	if len(parts) > 1 {
		skillInput = strings.TrimSpace(parts[1])
	}

	return skillName, skillInput
}

// getSkill retrieves a skill by name from the registry.
func (d *Dispatcher) getSkill(name string) *skills.Skill {
	if d.skillRegistry == nil {
		return nil
	}
	return d.skillRegistry.Get(name)
}

// executeSkill executes a skill and returns a dispatch result.
func (d *Dispatcher) executeSkill(ctx context.Context, skill *skills.Skill, input string, sessionID string) (*DispatchResult, error) {
	if d.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not configured")
	}

	// Execute the skill
	result, err := d.skillExecutor.Execute(ctx, skill, input)
	if err != nil {
		d.logger.Error("Skill execution failed",
			"skill", skill.Name,
			"error", err,
		)
		return nil, fmt.Errorf("skill execution failed: %w", err)
	}

	// Build dispatch result with skill response
	intent := &Intent{
		Type:       "skill",
		Confidence: 1.0,
		AgentType:  "skill:" + skill.Name,
		Summary:    fmt.Sprintf("Executed skill: %s", skill.Name),
	}

	return &DispatchResult{
		AgentID:  "skill:" + skill.Name,
		Intent:   intent,
		Response: result.Content,
	}, nil
}

// ShouldDispatchAsync returns true if the dispatch result should be handled
// asynchronously via the orchestrator pipeline rather than inline.
func (d *Dispatcher) ShouldDispatchAsync(result *DispatchResult) bool {
	if result == nil || result.Intent == nil {
		return false
	}

	// Skills are always handled inline
	if result.Response != "" {
		return false
	}

	// Check intent type
	switch result.Intent.Type {
	case "code", "debug", "plan", "schedule":
		return true
	default:
		return result.Intent.RequiresPlanning
	}
}

// GetSkillRegistry returns the skill registry for external access.
func (d *Dispatcher) GetSkillRegistry() *skills.Registry {
	return d.skillRegistry
}

// GetSkillExecutor returns the skill executor for external access.
func (d *Dispatcher) GetSkillExecutor() *skills.Executor {
	return d.skillExecutor
}
