package employee

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
)

// resultTopicForTrigger is the round stamp. bus_event publishes on
// bot.result. Every other source, including empty, stamps employee.result.
// Callers copy this onto the PlanRef when the round builds it. They do not
// read GoalLoop.resultTopic later to choose the topic.
func resultTopicForTrigger(source string) string {
	if source == "bus_event" {
		return agent.TopicBotResult.Name
	}
	return agent.TopicEmployeeResult.Name
}

// normalizeResultTopic maps a stamp onto a legal publish topic.
// Empty means employee.result. The only other legal value is bot.result.
// An illegal value is not bot.result — the caller must not invent that tier.
func normalizeResultTopic(topic string) (normalized string, legal bool) {
	switch topic {
	case "":
		return agent.TopicEmployeeResult.Name, true
	case agent.TopicEmployeeResult.Name, agent.TopicBotResult.Name:
		return topic, true
	default:
		return agent.TopicEmployeeResult.Name, false
	}
}

// loadStampedResultTopic fills an empty PlanRef.ResultTopic from the stored
// plan. A stamp already on the ref wins: a later lookup must not retarget a
// round that started with a topic. No store means empty → employee.result,
// with no warning and no invented bot.result. A lookup error or a missing
// plan is a load miss: employee.result, plus a warning.
func (l *GoalLoop) loadStampedResultTopic(ctx context.Context, planRef PlanRef) PlanRef {
	if l == nil {
		planRef.ResultTopic = agent.TopicEmployeeResult.Name
		return planRef
	}
	logger := l.logger
	if planRef.ResultTopic != "" {
		planRef.ResultTopic = applyResultTopic(logger, planRef.ID, planRef.ResultTopic)
		return planRef
	}

	l.mu.Lock()
	lookup := l.planBriefLookup
	planner := l.planner
	l.mu.Unlock()

	var getter PlanResultTopicLookup
	if planner != nil {
		getter, _ = planner.(PlanResultTopicLookup)
	}
	if lookup == nil && getter == nil {
		planRef.ResultTopic = agent.TopicEmployeeResult.Name
		return planRef
	}

	if lookup != nil {
		brief, ok, err := lookup(ctx, planRef.ID)
		if err != nil || !ok {
			warnResultTopicMiss(logger, planRef.ID, ok, err)
			planRef.ResultTopic = agent.TopicEmployeeResult.Name
			return planRef
		}
		if brief.ResultTopic != "" {
			planRef.ResultTopic = applyResultTopic(logger, planRef.ID, brief.ResultTopic)
			return planRef
		}
		// The brief hit, but this lookup does not carry the column.
		// Fall through to the plan-row getter when one is wired.
		if getter == nil {
			planRef.ResultTopic = agent.TopicEmployeeResult.Name
			return planRef
		}
	}

	topic, found, err := getter.PlanResultTopic(ctx, planRef.ID)
	if err != nil || !found {
		warnResultTopicMiss(logger, planRef.ID, found, err)
		planRef.ResultTopic = agent.TopicEmployeeResult.Name
		return planRef
	}
	planRef.ResultTopic = applyResultTopic(logger, planRef.ID, topic)
	return planRef
}

// persistResultTopic writes the stamp onto the stored plan when the planner
// can store it. No setter means no store: skip, do not fail the round, and
// do not invent bot.result.
func persistResultTopic(ctx context.Context, planner PlanCreator, logger *slog.Logger, planID, topic string) {
	if planner == nil || planID == "" {
		return
	}
	setter, ok := planner.(PlanResultTopicSetter)
	if !ok {
		return
	}
	normalized, legal := normalizeResultTopic(topic)
	if !legal {
		warnIllegalResultTopic(logger, planID, topic)
	}
	if err := setter.SetPlanResultTopic(ctx, planID, normalized); err != nil && logger != nil {
		logger.Warn("plan: failed to persist result topic on plan row",
			"plan_id", planID, "error", err)
	}
}

func applyResultTopic(logger *slog.Logger, planID, topic string) string {
	normalized, legal := normalizeResultTopic(topic)
	if !legal {
		warnIllegalResultTopic(logger, planID, topic)
	}
	return normalized
}

func warnResultTopicMiss(logger *slog.Logger, planID string, found bool, err error) {
	if logger == nil {
		return
	}
	logger.Warn("result topic load miss; using employee.result",
		"plan_id", planID, "found", found, "error", err)
}

func warnIllegalResultTopic(logger *slog.Logger, planID, topic string) {
	if logger == nil {
		return
	}
	logger.Warn("result topic is not legal; using employee.result",
		"plan_id", planID, "result_topic", topic)
}
