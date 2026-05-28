package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WritePlanMarkdown generates a complete plan.md file from a Plan and its
// parsed phases. It creates parent directories as needed.
func WritePlanMarkdown(filePath string, plan *Plan, phases []ParsedPhase) error {
	var b strings.Builder

	// Title
	fmt.Fprintf(&b, "# Plan: %s\n\n", plan.Title)

	// Meta section
	b.WriteString("## Meta\n\n")
	fmt.Fprintf(&b, "- plan_id: %s\n", plan.ID)
	if plan.ProjectID != "" {
		fmt.Fprintf(&b, "- project: %s\n", plan.ProjectID)
	}
	fmt.Fprintf(&b, "- created: %s\n", plan.CreatedAt.Format("2006-01-02"))
	fmt.Fprintf(&b, "- status: %s\n", plan.State)
	b.WriteString("\n")

	// Summary section
	b.WriteString("## Summary\n\n")
	if plan.Description != "" {
		b.WriteString(plan.Description)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Phases
	for _, phase := range phases {
		fmt.Fprintf(&b, "## Phase %d: %s [%s]\n\n", phase.Sequence, phase.Name, phase.State)

		for _, step := range phase.Steps {
			desc := step.Description
			status := step.State
			if status == "" {
				status = StepStatusPending
			}

			var line string
			if status == StepStatusCompleted {
				line = fmt.Sprintf("%d. ~~%s~~ [%s]", step.Number, desc, status)
			} else {
				line = fmt.Sprintf("%d. %s [%s]", step.Number, desc, status)
			}

			if len(step.DependsOn) > 0 {
				deps := make([]string, len(step.DependsOn))
				for i, d := range step.DependsOn {
					deps[i] = fmt.Sprintf("%d", d)
				}
				line += fmt.Sprintf(" (depends: %s)", strings.Join(deps, ", "))
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Notes section
	b.WriteString("## Notes\n\n")

	if err := mkdirAndWrite(filePath, b.String()); err != nil {
		return fmt.Errorf("write plan markdown: %w", err)
	}
	return nil
}

// UpdatePlanStatus updates an existing plan.md file with new plan state and
// phase progress. It re-parses the file, patches the status and step states,
// then writes the file back.
func UpdatePlanStatus(filePath string, planState PlanState, phases []PlanPhase) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read plan file for update: %w", err)
	}

	parsed, err := ParsePlanContent(string(data))
	if err != nil {
		return fmt.Errorf("parse plan for update: %w", err)
	}

	// Update the status in the parsed plan.
	parsed.Status = string(planState)

	// Build a lookup from phase sequence to progress info.
	phaseProgress := make(map[int]PlanPhase)
	for _, ph := range phases {
		phaseProgress[ph.Sequence] = ph
	}

	// Update phase states and step statuses.
	for i := range parsed.Phases {
		pp := &parsed.Phases[i]
		progress, ok := phaseProgress[pp.Sequence]
		if !ok {
			continue
		}

		pp.State = progress.State

		total := progress.TotalSteps
		completed := progress.CompletedSteps
		if total == 0 {
			total = len(pp.Steps)
		}

		for j := range pp.Steps {
			step := &pp.Steps[j]
			if completed >= total {
				// All steps completed.
				step.State = StepStatusCompleted
			} else if step.Number <= completed {
				step.State = StepStatusCompleted
			} else {
				step.State = StepStatusPending
			}
		}
	}

	// Write back using the round-trip writer.
	if err := WritePlanFromParsed(filePath, parsed); err != nil {
		return fmt.Errorf("write updated plan: %w", err)
	}
	return nil
}

// WritePlanFromParsed writes a plan.md from a ParsedPlan, enabling
// round-trip (parse -> modify -> write) workflows.
func WritePlanFromParsed(filePath string, parsed *ParsedPlan) error {
	var b strings.Builder

	// Title
	fmt.Fprintf(&b, "# Plan: %s\n\n", parsed.Title)

	// Meta section
	b.WriteString("## Meta\n\n")
	if parsed.PlanID != "" {
		fmt.Fprintf(&b, "- plan_id: %s\n", parsed.PlanID)
	}
	if parsed.Project != "" {
		fmt.Fprintf(&b, "- project: %s\n", parsed.Project)
	}
	if parsed.Status != "" {
		fmt.Fprintf(&b, "- status: %s\n", parsed.Status)
	}
	b.WriteString("\n")

	// Summary section
	b.WriteString("## Summary\n\n")
	if parsed.Summary != "" {
		b.WriteString(parsed.Summary)
		if !strings.HasSuffix(parsed.Summary, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Phases
	for _, phase := range parsed.Phases {
		state := phase.State
		if state == "" {
			state = PhasePending
		}
		fmt.Fprintf(&b, "## Phase %d: %s [%s]\n\n", phase.Sequence, phase.Name, state)

		for _, step := range phase.Steps {
			desc := step.Description
			status := step.State
			if status == "" {
				status = StepStatusPending
			}

			var line string
			if status == StepStatusCompleted {
				line = fmt.Sprintf("%d. ~~%s~~ [%s]", step.Number, desc, status)
			} else {
				line = fmt.Sprintf("%d. %s [%s]", step.Number, desc, status)
			}

			if len(step.DependsOn) > 0 {
				deps := make([]string, len(step.DependsOn))
				for i, d := range step.DependsOn {
					deps[i] = fmt.Sprintf("%d", d)
				}
				line += fmt.Sprintf(" (depends: %s)", strings.Join(deps, ", "))
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Notes section
	b.WriteString("## Notes\n\n")
	for _, note := range parsed.Notes {
		fmt.Fprintf(&b, "- %s\n", note)
	}
	if len(parsed.Notes) > 0 {
		b.WriteString("\n")
	}

	if err := mkdirAndWrite(filePath, b.String()); err != nil {
		return fmt.Errorf("write parsed plan: %w", err)
	}
	return nil
}

// mkdirAndWrite creates parent directories and writes content to filePath.
func mkdirAndWrite(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}
