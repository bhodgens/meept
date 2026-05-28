package plan

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ParsedPlan holds the structured data extracted from a plan.md file.
type ParsedPlan struct {
	Title   string
	PlanID  string
	Project string
	Status  string
	Summary string
	Phases  []ParsedPhase
	Notes   []string
}

// ParsedPhase holds a phase and its steps.
type ParsedPhase struct {
	Name     string
	Sequence int
	State    PhaseState
	Steps    []ParsedStep
}

// ParsedStep holds a single step from a plan.
type ParsedStep struct {
	Number      int
	Description string
	State       StepStatus
	DependsOn   []int // Step numbers this depends on
}

// StepStatus represents the status annotation on a step.
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

// Regex patterns for parsing plan.md lines.
var (
	// Matches: # Plan: Title Here
	rePlanTitle = regexp.MustCompile(`^#\s+Plan:\s+(.+)$`)

	// Matches: ## Meta
	reMetaHeading = regexp.MustCompile(`^##\s+Meta\s*$`)

	// Matches: ## Summary
	reSummaryHeading = regexp.MustCompile(`^##\s+Summary\s*$`)

	// Matches: ## Phase N: Name [state]
	rePhaseHeading = regexp.MustCompile(`^##\s+Phase\s+(\d+):\s+(.+?)\s*\[(\w+)\]\s*$`)

	// Matches: ## Notes
	reNotesHeading = regexp.MustCompile(`^##\s+Notes\s*$`)

	// Matches: ## Anything (generic heading)
	reHeading = regexp.MustCompile(`^##\s+`)

	// Matches: - key: value (meta key-value pair)
	reMetaKV = regexp.MustCompile(`^-\s+(\w[\w\s]*?):\s+(.+)$`)

	// Matches: N. ~~text~~ [status] (completed step with strikethrough)
	reStepStrikethrough = regexp.MustCompile(`^(\d+)\.\s+~~(.+?)~~\s*\[(\w+)\]\s*(?:\(depends:\s*([\d,\s]+)\))?\s*$`)

	// Matches: N. text [status] (depends: N, M)
	reStepWithDeps = regexp.MustCompile(`^(\d+)\.\s+(.+?)\s*\[(\w+)\]\s*\(depends:\s*([\d,\s]+)\)\s*$`)

	// Matches: N. text [status] (basic step)
	reStepBasic = regexp.MustCompile(`^(\d+)\.\s+(.+?)\s*\[(\w+)\]\s*$`)

	// Matches: - note text
	reNoteLine = regexp.MustCompile(`^-\s+(.+)$`)
)

// ParsePlan reads a plan.md file and parses its contents.
func ParsePlan(filePath string) (*ParsedPlan, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read plan file %s: %w", filePath, err)
	}
	return ParsePlanContent(string(data))
}

// ParsePlanContent parses plan.md content from a string.
func ParsePlanContent(content string) (*ParsedPlan, error) {
	p := &ParsedPlan{}

	// Parser state machine tracks which section we're in.
	type section int
	const (
		sNone section = iota
		sMeta
		sSummary
		sPhase
		sNotes
	)

	var (
		currentSection section
		currentPhase   *ParsedPhase
		summaryLines   []string
	)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// --- Top-level plan title ---
		if m := rePlanTitle.FindStringSubmatch(trimmed); m != nil {
			p.Title = strings.TrimSpace(m[1])
			currentSection = sNone
			continue
		}

		// --- Section headings ---
		if reHeading.MatchString(trimmed) {
			// Before switching section, finalize the previous one.
			if currentSection == sSummary && len(summaryLines) > 0 {
				p.Summary = strings.TrimSpace(strings.Join(summaryLines, "\n"))
				summaryLines = nil
			}
			if currentSection == sPhase && currentPhase != nil {
				p.Phases = append(p.Phases, *currentPhase)
				currentPhase = nil
			}

			switch {
			case reMetaHeading.MatchString(trimmed):
				currentSection = sMeta
			case reSummaryHeading.MatchString(trimmed):
				currentSection = sSummary
			case reNotesHeading.MatchString(trimmed):
				currentSection = sNotes
			default:
				if m := rePhaseHeading.FindStringSubmatch(trimmed); m != nil {
					seq, _ := strconv.Atoi(m[1])
					currentPhase = &ParsedPhase{
						Name:     strings.TrimSpace(m[2]),
						Sequence: seq,
						State:    PhaseState(m[3]),
					}
					currentSection = sPhase
				} else {
					currentSection = sNone
				}
			}
			continue
		}

		// --- Content lines by section ---
		switch currentSection {
		case sMeta:
			parseMetaLine(p, trimmed)

		case sSummary:
			// Collect all lines between ## Summary and the next ## heading.
			// Skip blank leading lines but preserve interior blank lines.
			if len(summaryLines) > 0 || trimmed != "" {
				summaryLines = append(summaryLines, line)
			}

		case sPhase:
			if currentPhase != nil {
				parseStepLine(currentPhase, trimmed)
			}

		case sNotes:
			if m := reNoteLine.FindStringSubmatch(trimmed); m != nil {
				p.Notes = append(p.Notes, strings.TrimSpace(m[1]))
			}
		}
	}

	// Finalize last section.
	if currentSection == sSummary && len(summaryLines) > 0 {
		p.Summary = strings.TrimSpace(strings.Join(summaryLines, "\n"))
	}
	if currentSection == sPhase && currentPhase != nil {
		p.Phases = append(p.Phases, *currentPhase)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning plan content: %w", err)
	}

	return p, nil
}

// parseMetaLine extracts key-value pairs from ## Meta section lines.
func parseMetaLine(p *ParsedPlan, line string) {
	m := reMetaKV.FindStringSubmatch(line)
	if m == nil {
		return
	}
	key := strings.TrimSpace(m[1])
	value := strings.TrimSpace(m[2])

	switch key {
	case "plan_id":
		p.PlanID = value
	case "project":
		p.Project = value
	case "status":
		p.Status = value
	}
}

// parseStepLine parses a numbered step line and appends it to the current phase.
func parseStepLine(phase *ParsedPhase, line string) {
	// Try strikethrough step first: N. ~~text~~ [status]
	if m := reStepStrikethrough.FindStringSubmatch(line); m != nil {
		num, _ := strconv.Atoi(m[1])
		step := ParsedStep{
			Number:      num,
			Description: strings.TrimSpace(m[2]),
			State:       StepStatus(m[3]),
		}
		if m[4] != "" {
			step.DependsOn = parseDepends(m[4])
		}
		phase.Steps = append(phase.Steps, step)
		return
	}

	// Try step with dependencies: N. text [status] (depends: N, M)
	if m := reStepWithDeps.FindStringSubmatch(line); m != nil {
		num, _ := strconv.Atoi(m[1])
		step := ParsedStep{
			Number:      num,
			Description: strings.TrimSpace(m[2]),
			State:       StepStatus(m[3]),
			DependsOn:   parseDepends(m[4]),
		}
		phase.Steps = append(phase.Steps, step)
		return
	}

	// Try basic step: N. text [status]
	if m := reStepBasic.FindStringSubmatch(line); m != nil {
		num, _ := strconv.Atoi(m[1])
		step := ParsedStep{
			Number:      num,
			Description: strings.TrimSpace(m[2]),
			State:       StepStatus(m[3]),
		}
		phase.Steps = append(phase.Steps, step)
		return
	}
}

// parseDepends splits "1, 2, 3" into []int{1, 2, 3}.
func parseDepends(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var deps []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if n, err := strconv.Atoi(p); err == nil {
			deps = append(deps, n)
		}
	}
	return deps
}
