package task

import "time"

// GetSecurityChecklist returns a security-focused checklist for code changes.
func GetSecurityChecklist() *Checklist {
	return &Checklist{
		Items: []ChecklistItem{
			{Text: "No SQL injection vectors (parameterized queries used)", Completed: false},
			{Text: "No command injection vectors (user input sanitized)", Completed: false},
			{Text: "No hardcoded secrets or credentials in code", Completed: false},
			{Text: "Input validation present for all external inputs", Completed: false},
			{Text: "Authentication/authorization checks in place", Completed: false},
			{Text: "Sensitive data encrypted at rest and in transit", Completed: false},
			{Text: "Error messages do not leak sensitive information", Completed: false},
		},
	}
}

// GetPerformanceChecklist returns a performance-focused checklist.
func GetPerformanceChecklist() *Checklist {
	return &Checklist{
		Items: []ChecklistItem{
			{Text: "No unnecessary allocations in hot paths", Completed: false},
			{Text: "No N+1 queries or redundant loops", Completed: false},
			{Text: "Appropriate data structures used for access patterns", Completed: false},
			{Text: "Database queries are indexed appropriately", Completed: false},
			{Text: "Caching strategy considered for repeated operations", Completed: false},
			{Text: "Memory leaks prevented (defer cleanup, close resources)", Completed: false},
		},
	}
}

// GetDeploymentChecklist returns a deployment readiness checklist.
func GetDeploymentChecklist() *Checklist {
	return &Checklist{
		Items: []ChecklistItem{
			{Text: "All tests pass (unit, integration, e2e)", Completed: false},
			{Text: "Database migrations written and tested", Completed: false},
			{Text: "Configuration changes documented and reviewed", Completed: false},
			{Text: "Rollback plan documented", Completed: false},
			{Text: "Monitoring/alerting updated for new functionality", Completed: false},
			{Text: "Documentation updated (API docs, README, etc.)", Completed: false},
			{Text: "Feature flags configured (if applicable)", Completed: false},
			{Text: "Load testing completed for high-traffic endpoints", Completed: false},
		},
	}
}

// GetCodeReviewChecklist returns a general code review checklist.
func GetCodeReviewChecklist() *Checklist {
	return &Checklist{
		Items: []ChecklistItem{
			{Text: "Logic matches intent (no off-by-one errors, correct conditions)", Completed: false},
			{Text: "Edge cases handled (empty input, nil/null, overflow)", Completed: false},
			{Text: "Error paths handled correctly (errors propagated, not swallowed)", Completed: false},
			{Text: "Names are clear and consistent with project conventions", Completed: false},
			{Text: "Complex logic has comments explaining 'why', not 'what'", Completed: false},
			{Text: "Function length is reasonable (under 50 lines)", Completed: false},
			{Text: "Happy path covered by tests", Completed: false},
			{Text: "Error paths covered by tests", Completed: false},
			{Text: "Edge cases tested", Completed: false},
			{Text: "Errors wrapped with context: fmt.Errorf(\"...: %w\", err)", Completed: false},
			{Text: "Resources closed with defer (response bodies, files, etc.)", Completed: false},
			{Text: "Context propagation on all I/O functions", Completed: false},
		},
	}
}

// ApplyTemplate applies a checklist template to a step, optionally merging with existing items.
func (s *TaskStep) ApplyTemplate(template *Checklist, merge bool) {
	if template == nil {
		return
	}
	if !merge || s.Checklist == nil {
		s.Checklist = &Checklist{
			Items: make([]ChecklistItem, len(template.Items)),
		}
		copy(s.Checklist.Items, template.Items)
	} else {
		// Merge: add template items that don't already exist
		existingTexts := make(map[string]bool)
		for _, item := range s.Checklist.Items {
			existingTexts[item.Text] = true
		}
		for _, item := range template.Items {
			if !existingTexts[item.Text] {
				s.Checklist.Items = append(s.Checklist.Items, item)
			}
		}
	}
	s.UpdatedAt = time.Now().UTC()
}

// GetChecklistTemplate returns a checklist template by category.
func GetChecklistTemplate(category string) *Checklist {
	switch category {
	case "security":
		return GetSecurityChecklist()
	case "performance":
		return GetPerformanceChecklist()
	case "deployment":
		return GetDeploymentChecklist()
	case "code-review":
		return GetCodeReviewChecklist()
	default:
		return nil
	}
}
