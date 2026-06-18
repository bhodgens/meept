package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/repomap"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/task"
)

// TestAllSetters_NilSafe verifies that every Set* method on agent-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe: calling with a nil (or typed-nil) argument must not panic.
//
// This enforces the "Setter methods" coding practice documented in CLAUDE.md:
// every Set* method accepting an interface or pointer type MUST include a nil
// guard as the first line so typed-nil interface values do not cause panics
// when methods are later called on the stored field.
func TestAllSetters_NilSafe(t *testing.T) {
	// Pre-build structs that need non-trivial constructors.
	agentLoop := NewAgentLoop()
	registry := NewAgentRegistry(RegistryConfig{})
	dispatcher := NewDispatcher(DispatcherConfig{})
	queue := NewMessageQueue()
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{})
	convStore := NewConversationStore(100)
	reviewMgr := NewReviewManager(ReviewManagerConfig{})
	pair := NewPairSession("t1", "spec", "analyst", "planner", 3)
	chatHandler := &ChatHandler{}
	executor := &Executor{}
	orch := NewOrchestrator(OrchestratorDeps{})

	tests := []struct {
		name    string
		setFunc func()
	}{
		// ChatHandler setters (internal/agent/handler.go)
		{"ChatHandler.SetMetricsStore", func() { chatHandler.SetMetricsStore((*metrics.Store)(nil)) }},
		{"ChatHandler.SetStepStore", func() { chatHandler.SetStepStore((*task.StepStore)(nil)) }},
		{"ChatHandler.SetTaskStore", func() { chatHandler.SetTaskStore((*task.Store)(nil)) }},
		{"ChatHandler.SetBudget", func() { chatHandler.SetBudget((*llm.Budget)(nil)) }},
		{"ChatHandler.SetCollaborationEngine", func() { chatHandler.SetCollaborationEngine((*CollaborationEngine)(nil)) }},

		// Dispatcher setters (internal/agent/dispatcher.go)
		{"Dispatcher.SetCapabilityMatcher", func() { dispatcher.SetCapabilityMatcher((*CapabilityMatcher)(nil)) }},

		// MessageQueue setters (internal/agent/queue.go)
		{"MessageQueue.SetPersister", func() { queue.SetPersister(nil) }},

		// CapabilityMatcher setters (internal/agent/capability_matcher.go)
		{"CapabilityMatcher.SetCapabilityIndex", func() { matcher.SetCapabilityIndex((*skills.CapabilityIndex)(nil)) }},

		// PairSession setters (internal/agent/pair_session.go)
		{"PairSession.SetCriteria", func() { pair.SetCriteria(nil) }},

		// AgentRegistry setters (internal/agent/registry.go)
		{"AgentRegistry.SetCapabilitiesMap", func() { registry.SetCapabilitiesMap((*CapabilitiesMap)(nil)) }},
		{"AgentRegistry.SetCapabilityIndex", func() { registry.SetCapabilityIndex((*skills.CapabilityIndex)(nil)) }},
		{"AgentRegistry.SetSkillLoader", func() { registry.SetSkillLoader((*skills.LazySkillLoader)(nil)) }},
		{"AgentRegistry.SetTTSRManager", func() { registry.SetTTSRManager((*TTSRManager)(nil)) }},

		// ConversationStore setters (internal/agent/conversation.go)
		{"ConversationStore.SetPersistence", func() { convStore.SetPersistence(nil) }},

		// AgentLoop setters (internal/agent/loop.go)
		{"AgentLoop.SetTaskCollector", func() { agentLoop.SetTaskCollector((*metrics.TaskCollector)(nil)) }},
		{"AgentLoop.SetResponseAnalyzer", func() { agentLoop.SetResponseAnalyzer((*metrics.ResponseAnalyzer)(nil)) }},
		{"AgentLoop.SetPrefetchCallback", func() { agentLoop.SetPrefetchCallback(nil) }},
		{"AgentLoop.SetMemvidClient", func() { agentLoop.SetMemvidClient((*memvid.Client)(nil)) }},
		{"AgentLoop.SetTaskStore", func() { agentLoop.SetTaskStore((*task.Store)(nil)) }},
		{"AgentLoop.SetNotificationPublisher", func() { agentLoop.SetNotificationPublisher(nil) }},
		{"AgentLoop.SetRepoMapGenerator", func() { agentLoop.SetRepoMapGenerator((*repomap.RepoMapGenerator)(nil)) }},
		{"AgentLoop.SetCapabilityIndex", func() { agentLoop.SetCapabilityIndex((*skills.CapabilityIndex)(nil)) }},
		{"AgentLoop.SetSkillLoader", func() { agentLoop.SetSkillLoader((*skills.LazySkillLoader)(nil)) }},
		{"AgentLoop.SetSessionStore", func() { agentLoop.SetSessionStore(nil, nil) }},
		{"AgentLoop.SetBranchManager", func() { agentLoop.SetBranchManager(nil) }},
		{"AgentLoop.SetMCPServerLister", func() { agentLoop.SetMCPServerLister(nil) }},

		// Orchestrator setters (internal/agent/orchestrator.go)
		{"Orchestrator.SetRepoMapGenerator", func() { orch.SetRepoMapGenerator((*repomap.RepoMapGenerator)(nil)) }},
		{"Orchestrator.SetPlanManager", func() { orch.SetPlanManager((*plan.PlanManager)(nil)) }},
		{"Orchestrator.SetReflectionEngine", func() { orch.SetReflectionEngine((*ReflectionEngine)(nil)) }},

		// Executor setters (internal/agent/executor.go)
		{"Executor.SetRegistry", func() { executor.SetRegistry(nil) }},

		// ReviewManager setters (internal/agent/review_manager.go)
		{"ReviewManager.SetPolicy", func() { reviewMgr.SetPolicy((*ReviewPolicy)(nil)) }},
		{"ReviewManager.SetValidationPolicy", func() { reviewMgr.SetValidationPolicy((*ValidationPolicy)(nil)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Set method panicked on nil: %v", r)
				}
			}()
			tt.setFunc()
		})
	}
}
