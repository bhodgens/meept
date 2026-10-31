package activity

import (
	"context"
	"sync"
)

// planIndex memoizes plan -> task resolutions across queries (inverted
// linkage: plans.task_id -> tasks.id). Grown by every stamp pass; guarded
// by mu because RPC handlers may query concurrently.
type planIndex struct {
	mu         sync.Mutex
	taskToPlan map[string]string
}

func (p *planIndex) merge(planID string, taskIDs []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.taskToPlan == nil {
		p.taskToPlan = map[string]string{}
	}
	for _, taskID := range taskIDs {
		if _, exists := p.taskToPlan[taskID]; !exists {
			p.taskToPlan[taskID] = planID
		}
	}
}

func (p *planIndex) lookup(taskID string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	planID, ok := p.taskToPlan[taskID]
	return planID, ok
}

func (p *planIndex) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.taskToPlan)
}

// stampPlanIDs stamps PlanID on task_step records whose TaskID belongs to a
// plan. The resolver answers plan -> tasks; plan IDs to resolve come from
// (1) plan IDs already carried by other records in the batch (findings and
// audit records carry their own plan_id) and (2) a.pendingPlanIDs — plan
// IDs the caller declared interest in via Query.PlanID. Resolutions are
// memoized so a later plan-less query still groups previously-seen tasks.
func (a *Aggregator) stampPlanIDs(ctx context.Context, records []ActivityRecord) {
	planIDs := make(map[string]bool)
	for planID := range a.pendingPlanIDs {
		planIDs[planID] = true
	}
	for _, r := range records {
		if r.PlanID != "" {
			planIDs[r.PlanID] = true
		}
	}
	for planID := range planIDs {
		taskIDs, err := a.opts.PlanResolver(ctx, planID)
		if err != nil {
			continue
		}
		a.plans.merge(planID, taskIDs)
	}
	if a.plans.size() == 0 {
		return
	}
	for i := range records {
		if records[i].Kind != KindTaskStep || records[i].PlanID != "" {
			continue
		}
		if planID, ok := a.plans.lookup(records[i].TaskID); ok {
			records[i].PlanID = planID
		}
	}
}
