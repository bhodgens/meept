package activity

import (
	"context"
	"time"
)

// taskStepRecords maps task steps to ActivityRecords (kind "task_step"),
// enriched with the parent task's name and assigned agent. At is the step's
// UpdatedAt (its latest activity moment). Summary is the envelope-stripped
// result when present, else the step description — never machine-shaped.
func (a *Aggregator) taskStepRecords(ctx context.Context, q Query) ([]ActivityRecord, error) {
	steps, err := a.steps.ListSteps()
	if err != nil {
		return nil, err
	}
	taskNames := map[string]TaskRow{}
	if a.tasks != nil {
		if tasks, err := a.tasks.ListTasks(); err == nil {
			for _, t := range tasks {
				taskNames[t.ID] = t
			}
		}
	}
	records := make([]ActivityRecord, 0, len(steps))
	for _, s := range steps {
		at := s.UpdatedAt
		if at.IsZero() {
			continue
		}
		task := taskNames[s.TaskID]
		summary := firstNonEmpty(stripEnvelopeProse(s.Result), stripEnvelopeProse(s.Description), "step "+s.State)
		rec := ActivityRecord{
			Kind:    KindTaskStep,
			At:      at,
			AgentID: firstNonEmpty(s.AgentID, task.AssignedAgent),
			TaskID:  s.TaskID,
			StepID:  s.ID,
			State:   s.State,
			Summary: truncateSummary(summary),
			Detail: map[string]string{
				"task_name": task.Name,
			},
		}
		records = append(records, rec)
	}
	return records, nil
}

// sessionMessageRecords maps session messages to ActivityRecords (kind
// "session_message"). At is the message timestamp. Summary is the
// envelope-stripped, single-line content.
func (a *Aggregator) sessionMessageRecords(q Query) ([]ActivityRecord, error) {
	sessions, err := a.sessions.List()
	if err != nil {
		return nil, err
	}
	employeeBySession := make(map[string]string, len(sessions))
	for _, s := range sessions {
		employeeBySession[s.ID] = s.EmployeeID
	}
	records := make([]ActivityRecord, 0, 64)
	for _, s := range sessions {
		msgs, err := a.sessions.Messages(s.ID, 0, 0)
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if m.Timestamp.IsZero() {
				continue
			}
			agentID := firstNonEmpty(m.AgentID, employeeBySession[m.SessionID])
			summary := stripEnvelopeProse(m.Content)
			if summary == "" {
				summary = m.Role + " message"
			}
			records = append(records, ActivityRecord{
				Kind:      KindSessionMessage,
				At:        m.Timestamp,
				AgentID:   agentID,
				SessionID: m.SessionID,
				State:     m.Role,
				Summary:   truncateSummary(summary),
			})
		}
	}
	return records, nil
}

// auditRecords maps hash-chained audit records to ActivityRecords (kind
// "audit_record"). Severity is carried from the payload when the record
// carries one; plain records stay severity-less.
func (a *Aggregator) auditRecords(ctx context.Context) ([]ActivityRecord, error) {
	recs, err := a.chain.ListRecords(ctx, 0)
	if err != nil {
		return nil, err
	}
	records := make([]ActivityRecord, 0, len(recs))
	for _, r := range recs {
		if r.At.IsZero() {
			continue
		}
		summary := firstNonEmpty(r.Payload["summary"], r.Payload["action"], r.Payload["violated_rule"], "audit "+r.Type)
		detail := map[string]string{"seq": itoa(uint64(r.Seq))}
		for _, k := range []string{"action", "plan_id", "task_id", "session_id", "violated_rule"} {
			if v, ok := r.Payload[k]; ok {
				detail[k] = v
			}
		}
		records = append(records, ActivityRecord{
			Kind:      KindAuditRecord,
			At:        r.At,
			AgentID:   r.EmployeeID,
			PlanID:    r.Payload["plan_id"],
			TaskID:    r.Payload["task_id"],
			SessionID: r.Payload["session_id"],
			Severity:  r.Payload["severity"],
			State:     r.Type,
			Summary:   truncateSummary(summary),
			Detail:    detail,
		})
	}
	return records, nil
}

// findingRecords maps employee audit findings to ActivityRecords (kind
// "audit_record" with the finding's own plan_id and severity carried). At is
// DetectedAt; open findings state "open", resolved ones "resolved".
func (a *Aggregator) findingRecords(ctx context.Context, q Query) ([]ActivityRecord, error) {
	since := q.Since
	if since.IsZero() && a.opts.Now != nil {
		// Default window: nothing — findings are not windowed by default.
		since = time.Time{}
	}
	rows, err := a.findings.ListFindings(ctx, "", "", since, 0)
	if err != nil {
		return nil, err
	}
	records := make([]ActivityRecord, 0, len(rows))
	for _, f := range rows {
		if f.DetectedAt.IsZero() {
			continue
		}
		state := "open"
		if f.Resolved {
			state = "resolved"
		}
		summary := firstNonEmpty(f.ViolatedRule, stripEnvelopeProse(f.Evidence), "audit finding")
		records = append(records, ActivityRecord{
			Kind:     KindAuditRecord,
			At:       f.DetectedAt,
			AgentID:  f.EmployeeID,
			PlanID:   f.PlanID,
			State:    state,
			Severity: f.Severity,
			Summary:  truncateSummary(summary),
			Detail:   map[string]string{"finding_id": f.ID},
		})
	}
	return records, nil
}

// itoa formats n in decimal without importing strconv for one call site.
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
