package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// CompileSealed parses a sealed plan-dialect v1 markdown document into
// phase specs per docs/workflows/plan-dialect.md. Pure: string in,
// struct out; no I/O, no clocks, no global mutable state.
//
// All problems are collected in one pass (never just the first) and
// returned as *CompileError so one brainstorm round can fix everything.
// On success the CompiledPlan carries the phase specs, the sha256 hex of
// the raw input bytes, and non-fatal warnings.
func CompileSealed(markdown string, maxPhases int) (*CompiledPlan, error) {
	lines := strings.Split(markdown, "\n")
	var problems []CompileProblem

	doc := scanDraft(lines, &problems)
	checkEnvelope(doc, maxPhases, &problems)
	checkPhaseStructure(doc, &problems)
	checkArtifacts(doc, &problems)
	checkDependsOnLines(doc, &problems)
	checkSteps(doc, &problems)

	var warnings []string
	cp := assemble(doc, &warnings, &problems)

	detectCycle(doc, &problems)

	if len(problems) > 0 {
		return nil, &CompileError{Problems: problems}
	}

	sum := sha256.Sum256([]byte(markdown))
	cp.Hash = hex.EncodeToString(sum[:])
	cp.Warnings = warnings
	return cp, nil
}

// CompileProblem is one compile finding: a 1-based input line (0 for
// document-level problems) and a human-readable message.
type CompileProblem struct {
	Line    int
	Message string
}

// CompileError aggregates every problem found in one compile pass.
type CompileError struct {
	Problems []CompileProblem
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("plan compile failed: %d problems", len(e.Problems))
}

// CompiledPlan is the compiler's successful output.
type CompiledPlan struct {
	Phases   []PhaseSpec
	Hash     string   // sha256 hex of the raw input markdown, verbatim
	Warnings []string // non-fatal notes (e.g. depends_on inferred)
}

// PhaseSpec mirrors agent.PlanPhaseSpec 1:1 (same JSON tags). Package
// plan cannot import agent (agent imports plan), so leaf 04 converts via
// a trivial map; a round-trip test pins field parity.
type PhaseSpec struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Steps       []StepSpec `json:"steps"`
	Produces    []Artifact `json:"produces"`
	Consumes    []Artifact `json:"consumes"`
	DependsOn   []int      `json:"depends_on,omitempty"`
}

// StepSpec mirrors agent.plannerStep's public shape (same JSON tags).
type StepSpec struct {
	Description string `json:"description"`
	ToolHint    string `json:"tool_hint,omitempty"`
	DependsOn   []int  `json:"depends_on,omitempty"`
}

// toolHints now lives in internal/config (DialectToolHints) as the single
// source of truth shared with the tactical scheduler's hint→agent router.
// The dialect-doc membership check delegates to config.IsDialectToolHint;
// the §7 error message above stays verbatim per the spec-slave rule.

// Regex grammar — line-precision per dialect doc section 2. Immutable,
// mirroring parser.go's package-level regex style.
var (
	reDialectTitle   = regexp.MustCompile(`^# Plan: (.+)$`)
	reSectionHeading = regexp.MustCompile(`^## (.+)$`)
	reMetaEntry      = regexp.MustCompile(`^- ([a-z_]+): (.+)$`)
	// reDraftPhase rejects a persisted-format "[state]" suffix via the
	// optional third group; a non-empty group 3 is a class-11 problem.
	reDraftPhase     = regexp.MustCompile(`^### Phase ([1-9][0-9]*): (.+?)( \[\w+\])?$`)
	reProducesLabel  = regexp.MustCompile(`^\*\*Produces:\*\*$`)
	reConsumesLabel  = regexp.MustCompile(`^\*\*Consumes:\*\*$`)
	reConsumesNone   = regexp.MustCompile(`^\*\*Consumes:\*\* none$`)
	reDependsOnLine  = regexp.MustCompile(`^\*\*Depends on:\*\* Phases ([0-9]+(, [0-9]+)*)$`)
	reStepsLabel     = regexp.MustCompile(`^\*\*Steps:\*\*$`)
	reDraftStep      = regexp.MustCompile(`^([1-9][0-9]*)\. (.+?)( \[([a-z]+)\])?( \(needs: (.+)\))?$`)
	reArtifactBullet = regexp.MustCompile("^- `([a-z0-9]+(-[a-z0-9]+)*)` \\((file|interface|schema|decision|test_suite)\\) — (.+)$")
	reArtifactLoose  = regexp.MustCompile(`^- (.+?) \((.+?)\) — (.+)$`)
	reKebabName      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	reStepRef        = regexp.MustCompile(`^Phase([1-9][0-9]*)\.S([1-9][0-9]*)$`)
)

const (
	artifactMsgBadName = "artifact name %q is not kebab-case (lowercase letters and digits joined by single hyphens)"
	artifactMsgKind    = "artifact %q has unknown kind %q (must be one of file, interface, schema, decision, test_suite)"
	artifactMsgShape   = "expected \"- `<name>` (<kind>) — <description>\", got %q"
	consumeMsgUnknown  = "unknown artifact %q consumed by phase %q: no phase in this plan produces it"
	consumeMsgEarly    = "phase %q consumes %q, which is produced by a later phase (%q): consumes must reference artifacts from earlier phases"
	needsMsgUnknown    = "step %d of phase %q has a needs reference %q that matches no artifact name or step"
	needsMsgNoStep     = "step %d of phase %q references %q, but phase %d has no step %d"
	needsMsgLaterPhase = "step %d of phase %q references %q from phase %d, a later phase: step references may only target earlier phases"
	needsMsgLaterStep  = "step %d of phase %q references %q, a later step in the same phase: steps may only reference prior steps"
	needsMsgSamePhase  = "step %d of phase %q references artifact %q, which is produced by the same phase: artifact references must target earlier phases"
	// hintMsg is the dialect-doc §7 error format string (kept verbatim; the
	// spec-slave rule pins its wording).
	hintMsg             = "step %d of phase %q has unknown tool_hint %q (must be one of code, refactor, debug, fix, analyze, research, git, plan, chat, bash)"
	oqMsg               = "Open Questions must be empty to seal (%d unresolved)"
	metaMissingMsg      = "Meta is missing required key: %s"
	versionMsg          = "unsupported dialect version: got %q; this compiler implements version 1"
	statusMsg           = "Meta status must be \"draft\" or \"sealed\" (got %q)"
	metaLineMsg         = "expected \"- <key>: <value>\", got %q"
	phaseHeadMsg        = "expected phase heading \"### Phase N: <name>\", got %q"
	phaseNumMsg         = "phase numbering must be consecutive from 1 (expected Phase %d, got Phase %d)"
	noProducesMsg       = "phase %q declares no Produces block"
	noStepsMsg          = "phase %q declares no steps"
	dupArtifactMsg      = "duplicate artifact name %q (already produced by phase %q)"
	depMsgNonexistent   = "phase %q cites nonexistent phase %d in \"Depends on\""
	depMsgForward       = "phase %q cites phase %d in \"Depends on\", but only earlier phases may be cited"
	depMsgMalformed     = "expected \"**Depends on:** Phases <n>[, <n>]...\", got %q"
	stepMsgMalformed    = "expected \"<n>. <description> [tool_hint] (needs: <refs>)\", got %q"
	stepNumMsg          = "step numbering in phase %q must start at 1 and increase by 1 (expected step %d, got step %d)"
	missingSectionMsg   = "missing required section: %s"
	duplicateSectionMsg = "duplicate section: %s"
	noPhasesMsg         = "plan declares no phases"
	overMaxMsg          = "plan declares %d phases; maximum is %d"
	cycleMsg            = "dependency cycle among phases: %s"

	warnInferred  = "depends_on inferred for phase %q from consumed artifacts"
	warnDuplicate = "phase %q's \"Depends on\" duplicates dependencies already implied by consumed artifacts"
	warnNeedsNot  = "step %d of phase %q references artifact %q in needs, but it does not appear in the phase's Consumes block"
)

// draftArtifact is one parsed Produces/Consumes bullet.
type draftArtifact struct {
	name string
	kind string
	desc string
	line int
}

// draftStep is one parsed numbered step line.
type draftStep struct {
	number      int
	description string
	hint        string // "" when absent
	needs       []string
	line        int
}

// draftPhase is one ### Phase N: section.
type draftPhase struct {
	name           string
	ordinal        int
	headLine       int
	intent         []string
	produces       []draftArtifact
	consumes       []draftArtifact
	consumesEqNone bool
	dependsLine    int   // 0 when the section omits "**Depends on:**"
	dependsRefs    []int // cited phase ordinals (raw, unvalidated)
	steps          []draftStep
	producesSeen   bool
}

// draftDoc is the parsed document model.
type draftDoc struct {
	title      string
	titleSeen  bool
	sections   map[string]int // heading text -> first line (1-based)
	dupSection string         // heading text of first duplicate found ("" if none)
	dupLine    int
	meta       []ParsedMetaKV // key/value with source lines tracked separately
	metaLines  map[string]int
	metaBad    []CompileProblem
	goal       []string
	oqHeading  int // line of "## Open Questions" (0 when absent)
	oqCount    int // non-blank lines in the section
	phases     []*draftPhase
	phasesLine int // line of "## Phases" heading (0 when absent)
}

// scanDraft walks the lines once, building the document model and
// collecting line-anchored structural problems.
func scanDraft(lines []string, problems *[]CompileProblem) *draftDoc {
	doc := &draftDoc{
		sections:  map[string]int{},
		metaLines: map[string]int{},
	}

	type ctxKind int
	const (
		ctxNone ctxKind = iota
		ctxMeta
		ctxGoal
		ctxOpenQuestions
		ctxPhases
		ctxOther // Decisions, Notes, unknown headings: ignored
	)
	ctx := ctxNone
	var phase *draftPhase

	// sub-mode inside a phase section.
	type phaseMode int
	const (
		pmIntent phaseMode = iota
		pmProduces
		pmConsumes
		pmSteps
	)
	mode := pmIntent

	addProblem := func(line int, msg string) {
		*problems = append(*problems, CompileProblem{Line: line, Message: msg})
	}

	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimRight(raw, "\r")

		// --- Title ---
		if m := reDialectTitle.FindStringSubmatch(line); m != nil {
			if doc.titleSeen {
				addProblem(lineNo, fmt.Sprintf(duplicateSectionMsg, "# Plan:"))
			} else {
				doc.titleSeen = true
				doc.title = strings.TrimSpace(m[1])
			}
			ctx = ctxNone
			phase = nil
			continue
		}

		// --- ## section headings ---
		if m := reSectionHeading.FindStringSubmatch(line); m != nil {
			heading := "## " + m[1]
			if prev, seen := doc.sections[heading]; seen {
				if doc.dupSection == "" {
					doc.dupSection = heading
					doc.dupLine = lineNo
					_ = prev
				}
			} else {
				doc.sections[heading] = lineNo
			}
			// finalize current phase
			if phase != nil {
				doc.phases = append(doc.phases, phase)
				phase = nil
			}
			switch heading {
			case "## Meta":
				ctx = ctxMeta
			case "## Goal":
				ctx = ctxGoal
			case "## Open Questions":
				ctx = ctxOpenQuestions
				doc.oqHeading = lineNo
			case "## Phases":
				ctx = ctxPhases
				doc.phasesLine = lineNo
			default:
				ctx = ctxOther
			}
			mode = pmIntent
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// --- Phase headings (only within ## Phases) ---
		if ctx == ctxPhases && strings.HasPrefix(trimmed, "### Phase") {
			if phase != nil {
				doc.phases = append(doc.phases, phase)
			}
			phase = nil
			if m := reDraftPhase.FindStringSubmatch(trimmed); m != nil && m[3] == "" {
				ord, err := strconv.Atoi(m[1])
				if err != nil {
					addProblem(lineNo, fmt.Sprintf(phaseHeadMsg, trimmed))
					continue
				}
				phase = &draftPhase{
					name:     strings.TrimSpace(m[2]),
					ordinal:  ord,
					headLine: lineNo,
				}
				mode = pmIntent
				continue
			}
			addProblem(lineNo, fmt.Sprintf(phaseHeadMsg, trimmed))
			continue
		}

		// --- Content by section ---
		switch ctx {
		case ctxMeta:
			if m := reMetaEntry.FindStringSubmatch(trimmed); m != nil {
				key, value := m[1], strings.TrimSpace(m[2])
				if _, dup := doc.metaLines[key]; !dup {
					doc.meta = append(doc.meta, ParsedMetaKV{Key: key, Value: value})
					doc.metaLines[key] = lineNo
				}
			} else {
				addProblem(lineNo, fmt.Sprintf(metaLineMsg, trimmed))
			}

		case ctxGoal:
			doc.goal = append(doc.goal, trimmed)

		case ctxOpenQuestions:
			doc.oqCount++

		case ctxPhases:
			if phase == nil {
				continue // stray content between phases: not in the grammar
			}
			// Label lines are recognized in any sub-mode.
			switch {
			case reProducesLabel.MatchString(trimmed):
				mode = pmProduces
				phase.producesSeen = true
				continue
			case reConsumesNone.MatchString(trimmed):
				mode = pmConsumes
				phase.consumesEqNone = true
				continue
			case reConsumesLabel.MatchString(trimmed):
				mode = pmConsumes
				continue
			case reDependsOnLine.MatchString(trimmed):
				m := reDependsOnLine.FindStringSubmatch(trimmed)
				phase.dependsLine = lineNo
				phase.dependsRefs = parsePhaseNums(m[1])
				mode = pmIntent
				continue
			case strings.HasPrefix(trimmed, "**Depends on:**"):
				addProblem(lineNo, fmt.Sprintf(depMsgMalformed, trimmed))
				continue
			case reStepsLabel.MatchString(trimmed):
				mode = pmSteps
				continue
			case strings.HasPrefix(trimmed, "**Produces:**"):
				addProblem(lineNo, fmt.Sprintf(artifactMsgShape, trimmed))
				continue
			case strings.HasPrefix(trimmed, "**Consumes:**"):
				addProblem(lineNo, fmt.Sprintf(artifactMsgShape, trimmed))
				continue
			}

			switch mode {
			case pmIntent:
				phase.intent = append(phase.intent, trimmed)
			case pmProduces, pmConsumes:
				if strings.HasPrefix(trimmed, "- ") {
					art, ok := parseArtifactLine(trimmed)
					art.line = lineNo
					if !ok {
						addProblem(lineNo, artifactLineProblem(trimmed))
						continue
					}
					if mode == pmProduces {
						phase.produces = append(phase.produces, art)
					} else {
						phase.consumes = append(phase.consumes, art)
					}
				} else {
					addProblem(lineNo, fmt.Sprintf(artifactMsgShape, trimmed))
				}
			case pmSteps:
				if m := reDraftStep.FindStringSubmatch(trimmed); m != nil {
					num, err := strconv.Atoi(m[1])
					if err != nil {
						addProblem(lineNo, fmt.Sprintf(stepMsgMalformed, trimmed))
						continue
					}
					st := draftStep{
						number:      num,
						description: strings.TrimSpace(m[2]),
						hint:        m[4],
						line:        lineNo,
					}
					if m[6] != "" {
						for _, ref := range strings.Split(m[6], ",") {
							ref = strings.TrimSpace(ref)
							if ref != "" {
								st.needs = append(st.needs, ref)
							}
						}
					}
					phase.steps = append(phase.steps, st)
				} else {
					addProblem(lineNo, fmt.Sprintf(stepMsgMalformed, trimmed))
				}
			}
		}
	}
	if phase != nil {
		doc.phases = append(doc.phases, phase)
	}
	return doc
}

// parsePhaseNums splits "0, 2" style phase lists into ints.
func parsePhaseNums(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if n, err := strconv.Atoi(part); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// parseArtifactLine parses a Produces/Consumes bullet against the exact
// grammar; ok=false means the caller should emit a classification
// problem via artifactLineProblem.
func parseArtifactLine(line string) (draftArtifact, bool) {
	m := reArtifactBullet.FindStringSubmatch(line)
	if m == nil {
		return draftArtifact{}, false
	}
	return draftArtifact{name: m[1], kind: m[3], desc: strings.TrimSpace(m[4])}, true
}

// artifactLineProblem classifies a malformed artifact bullet into the
// spec's layered error classes: bad name (16) before unknown kind (17)
// before structural shape (15).
func artifactLineProblem(line string) string {
	if m := reArtifactLoose.FindStringSubmatch(line); m != nil {
		nameToken := m[1]
		name := strings.Trim(nameToken, "`")
		if !strings.Contains(nameToken, " ") { // single token: an attempted name
			if !reKebabName.MatchString(name) {
				return fmt.Sprintf(artifactMsgBadName, name)
			}
			if !isValidDialectKind(m[2]) {
				return fmt.Sprintf(artifactMsgKind, name, m[2])
			}
		}
	}
	return fmt.Sprintf(artifactMsgShape, line)
}

func isValidDialectKind(kind string) bool {
	switch kind {
	case "file", "interface", "schema", "decision", "test_suite":
		return true
	}
	return false
}

// checkEnvelope validates document-level rules: required/duplicate
// sections, Meta keys, version, status, the Open-Questions seal gate,
// phase presence, and the maxPhases cap.
func checkEnvelope(doc *draftDoc, maxPhases int, problems *[]CompileProblem) {
	add := func(line int, msg string) {
		*problems = append(*problems, CompileProblem{Line: line, Message: msg})
	}

	if !doc.titleSeen {
		add(0, fmt.Sprintf(missingSectionMsg, "# Plan:"))
	}
	for _, heading := range []string{"## Meta", "## Goal", "## Decisions", "## Open Questions", "## Phases"} {
		if _, ok := doc.sections[heading]; !ok {
			add(0, fmt.Sprintf(missingSectionMsg, heading))
		}
	}
	if doc.dupSection != "" {
		add(doc.dupLine, fmt.Sprintf(duplicateSectionMsg, doc.dupSection))
	}

	metaLine := doc.sections["## Meta"]
	for _, key := range []string{"task_id", "version", "status", "updated"} {
		if _, ok := doc.metaLines[key]; !ok {
			add(metaLine, fmt.Sprintf(metaMissingMsg, key))
		}
	}
	if v, ok := doc.metaLines["version"]; ok {
		val := metaValue(doc, "version")
		if val != "1" {
			add(v, fmt.Sprintf(versionMsg, val))
		}
	}
	if s, ok := doc.metaLines["status"]; ok {
		val := metaValue(doc, "status")
		if val != "draft" && val != "sealed" {
			add(s, fmt.Sprintf(statusMsg, val))
		}
	}
	*problems = append(*problems, doc.metaBad...)

	if doc.oqHeading != 0 && doc.oqCount > 0 {
		add(doc.oqHeading, fmt.Sprintf(oqMsg, doc.oqCount))
	}

	if doc.phasesLine != 0 && len(doc.phases) == 0 {
		add(doc.phasesLine, noPhasesMsg)
	}
	if maxPhases > 0 && len(doc.phases) > maxPhases {
		add(doc.phasesLine, fmt.Sprintf(overMaxMsg, len(doc.phases), maxPhases))
	}
}

func metaValue(doc *draftDoc, key string) string {
	for _, kv := range doc.meta {
		if kv.Key == key {
			return kv.Value
		}
	}
	return ""
}

// checkPhaseStructure validates phase ordering and per-phase block
// presence: consecutive numbering, Produces block, non-empty steps.
func checkPhaseStructure(doc *draftDoc, problems *[]CompileProblem) {
	expected := 1
	for _, p := range doc.phases {
		if p.ordinal != expected {
			*problems = append(*problems, CompileProblem{
				Line:    p.headLine,
				Message: fmt.Sprintf(phaseNumMsg, expected, p.ordinal),
			})
			expected = p.ordinal // resync so one anomaly reports once
		}
		expected++

		if !p.producesSeen {
			*problems = append(*problems, CompileProblem{
				Line:    p.headLine,
				Message: fmt.Sprintf(noProducesMsg, p.name),
			})
		}
		if len(p.steps) == 0 {
			*problems = append(*problems, CompileProblem{
				Line:    p.headLine,
				Message: fmt.Sprintf(noStepsMsg, p.name),
			})
		}
	}
}

// checkArtifacts runs the whole-plan artifact tables: kebab names and
// kinds were grammar-checked at scan time; here: duplicates across all
// produces, and consume-before-produce / unknown-consume rules.
func checkArtifacts(doc *draftDoc, problems *[]CompileProblem) {
	producerOf := map[string]int{} // name -> producing phase ordinal
	producerName := map[string]string{}
	for _, p := range doc.phases {
		for _, art := range p.produces {
			if _, dup := producerOf[art.name]; dup {
				*problems = append(*problems, CompileProblem{
					Line:    art.line,
					Message: fmt.Sprintf(dupArtifactMsg, art.name, producerName[art.name]),
				})
				continue
			}
			producerOf[art.name] = p.ordinal
			producerName[art.name] = p.name
		}
	}
	// Second pass for consumes so duplicates above don't change which
	// phase "first" produces a name mid-stream.
	for _, p := range doc.phases {
		for _, art := range p.consumes {
			prodOrd, ok := producerOf[art.name]
			if !ok {
				*problems = append(*problems, CompileProblem{
					Line:    art.line,
					Message: fmt.Sprintf(consumeMsgUnknown, art.name, p.name),
				})
				continue
			}
			if prodOrd >= p.ordinal {
				// Same-phase produces also violate the earlier-phase rule;
				// class 20 is the rule's error class (report: wording's
				// "later phase" clause is imperfect for the self case).
				*problems = append(*problems, CompileProblem{
					Line:    art.line,
					Message: fmt.Sprintf(consumeMsgEarly, p.name, art.name, producerName[art.name]),
				})
			}
		}
	}
}

// checkDependsOnLines validates explicit "**Depends on:**" citations.
func checkDependsOnLines(doc *draftDoc, problems *[]CompileProblem) {
	for _, p := range doc.phases {
		if p.dependsLine == 0 {
			continue
		}
		for _, cited := range p.dependsRefs {
			if cited < 1 || cited > len(doc.phases) {
				*problems = append(*problems, CompileProblem{
					Line:    p.dependsLine,
					Message: fmt.Sprintf(depMsgNonexistent, p.name, cited),
				})
				continue
			}
			if cited >= p.ordinal {
				*problems = append(*problems, CompileProblem{
					Line:    p.dependsLine,
					Message: fmt.Sprintf(depMsgForward, p.name, cited),
				})
			}
		}
	}
}

// checkSteps validates step numbering, tool hints, and needs references
// against the fully parsed document.
func checkSteps(doc *draftDoc, problems *[]CompileProblem) {
	producerOf := map[string]int{}
	for _, p := range doc.phases {
		for _, art := range p.produces {
			if _, dup := producerOf[art.name]; !dup {
				producerOf[art.name] = p.ordinal
			}
		}
	}
	phaseByName := map[int]*draftPhase{}
	for _, p := range doc.phases {
		phaseByName[p.ordinal] = p
	}

	for _, p := range doc.phases {
		expected := 1
		for si := range p.steps {
			st := &p.steps[si]
			if st.number != expected {
				*problems = append(*problems, CompileProblem{
					Line:    st.line,
					Message: fmt.Sprintf(stepNumMsg, p.name, expected, st.number),
				})
				expected = st.number // resync
			}
			expected++

			if st.hint != "" && !config.IsDialectToolHint(st.hint) {
				*problems = append(*problems, CompileProblem{
					Line:    st.line,
					Message: fmt.Sprintf(hintMsg, st.number, p.name, st.hint),
				})
			}

			for _, ref := range st.needs {
				// Artifact-name references are resolved first: a
				// PhaseN.S# token can never be a producer name, and a
				// kebab token can never be a step ref, so classification
				// by token shape is deterministic.
				if !reStepRef.MatchString(ref) && reKebabName.MatchString(ref) {
					prodOrd, ok := producerOf[ref]
					if !ok {
						*problems = append(*problems, CompileProblem{
							Line:    st.line,
							Message: fmt.Sprintf(needsMsgUnknown, st.number, p.name, ref),
						})
						continue
					}
					switch {
					case prodOrd == p.ordinal:
						*problems = append(*problems, CompileProblem{
							Line:    st.line,
							Message: fmt.Sprintf(needsMsgSamePhase, st.number, p.name, ref),
						})
					case prodOrd > p.ordinal:
						// Later-phase artifact refs violate the same
						// earlier-phase rule as consumes (class 20's rule).
						*problems = append(*problems, CompileProblem{
							Line:    st.line,
							Message: fmt.Sprintf(consumeMsgEarly, p.name, ref, phaseNameOf(doc, prodOrd)),
						})
					default:
						if !phaseConsumes(p, ref) {
							// W3 warning, parked as a pseudo-problem for
							// assemble() to move into Warnings.
							*problems = append(*problems, CompileProblem{
								Line:    st.line,
								Message: fmt.Sprintf(warnNeedsNot, st.number, p.name, ref),
							})
						}
					}
					continue
				}

				if m := reStepRef.FindStringSubmatch(ref); m != nil {
					targetOrd, err := strconv.Atoi(m[1])
					if err != nil {
						*problems = append(*problems, CompileProblem{
							Line:    st.line,
							Message: fmt.Sprintf(needsMsgUnknown, st.number, p.name, ref),
						})
						continue
					}
					targetStep, err2 := strconv.Atoi(m[2])
					if err2 != nil {
						*problems = append(*problems, CompileProblem{
							Line:    st.line,
							Message: fmt.Sprintf(needsMsgUnknown, st.number, p.name, ref),
						})
						continue
					}
					switch {
					case targetOrd > p.ordinal:
						*problems = append(*problems, CompileProblem{
							Line:    st.line,
							Message: fmt.Sprintf(needsMsgLaterPhase, st.number, p.name, ref, targetOrd),
						})
					case targetOrd == p.ordinal:
						if targetStep >= st.number {
							*problems = append(*problems, CompileProblem{
								Line:    st.line,
								Message: fmt.Sprintf(needsMsgLaterStep, st.number, p.name, ref),
							})
						}
					default: // earlier phase: step must exist there
						tp := phaseByName[targetOrd]
						found := false
						if tp != nil {
							for _, ts := range tp.steps {
								if ts.number == targetStep {
									found = true
									break
								}
							}
						}
						if !found {
							*problems = append(*problems, CompileProblem{
								Line:    st.line,
								Message: fmt.Sprintf(needsMsgNoStep, st.number, p.name, ref, targetOrd, targetStep),
							})
						}
					}
					continue
				}

				// Neither a kebab artifact name nor a valid step ref:
				// unknown reference (class 27).
				*problems = append(*problems, CompileProblem{
					Line:    st.line,
					Message: fmt.Sprintf(needsMsgUnknown, st.number, p.name, ref),
				})
			}
		}
	}
}

func phaseNameOf(doc *draftDoc, ordinal int) string {
	for _, p := range doc.phases {
		if p.ordinal == ordinal {
			return p.name
		}
	}
	return ""
}

func phaseConsumes(p *draftPhase, name string) bool {
	for _, art := range p.consumes {
		if art.name == name {
			return true
		}
	}
	return false
}

// assemble builds the CompiledPlan: step depends_on from same-phase
// prior-step refs, phase depends_on from explicit lines or inference,
// derived Required flags, and W1/W2/W3 warnings.
func assemble(doc *draftDoc, warnings *[]string, problems *[]CompileProblem) *CompiledPlan {
	producerOf := map[string]int{}
	for _, p := range doc.phases {
		for _, art := range p.produces {
			if _, dup := producerOf[art.name]; !dup {
				producerOf[art.name] = p.ordinal
			}
		}
	}

	// W3 warnings were parked as pseudo-problems by checkSteps; move them.
	var w3 []CompileProblem
	kept := (*problems)[:0]
	for _, pr := range *problems {
		// W3 messages start with "step N of phase" and mention "does not appear in the phase's Consumes block".
		if strings.Contains(pr.Message, "does not appear in the phase's Consumes block") {
			w3 = append(w3, pr)
			continue
		}
		kept = append(kept, pr)
	}
	*problems = kept
	for _, w := range w3 {
		*warnings = append(*warnings, w.Message)
	}

	cp := &CompiledPlan{Phases: make([]PhaseSpec, 0, len(doc.phases))}
	consumedByLater := map[string]bool{}
	for i, p := range doc.phases {
		for _, art := range p.consumes {
			if prodOrd, ok := producerOf[art.name]; ok && prodOrd < p.ordinal {
				// Mark all earlier producers' artifacts; resolve names to
				// specs after the loop below via second pass.
				_ = i
				consumedByLater[art.name] = true
			}
		}
	}

	for _, p := range doc.phases {
		spec := PhaseSpec{
			Name:        p.name,
			Description: strings.TrimSpace(strings.Join(p.intent, "\n")),
		}

		// Steps: same-phase prior-step refs become step-level depends_on.
		stepNumByIndex := map[int]int{}
		for si, st := range p.steps {
			stepNumByIndex[si] = st.number
		}
		for _, st := range p.steps {
			ss := StepSpec{Description: st.description, ToolHint: st.hint}
			var deps []int
			for _, ref := range st.needs {
				if m := reStepRef.FindStringSubmatch(ref); m != nil {
					targetOrd, err1 := strconv.Atoi(m[1])
					targetStep, err2 := strconv.Atoi(m[2])
					if err1 == nil && err2 == nil && targetOrd == p.ordinal && targetStep < st.number {
						deps = append(deps, targetStep)
					}
				}
			}
			sort.Ints(deps)
			ss.DependsOn = dedupInts(deps)
			spec.Steps = append(spec.Steps, ss)
		}

		for _, art := range p.produces {
			spec.Produces = append(spec.Produces, Artifact{
				Name:        art.name,
				Kind:        art.kind,
				Description: art.desc,
				Required:    consumedByLater[art.name],
			})
		}
		for _, art := range p.consumes {
			spec.Consumes = append(spec.Consumes, Artifact{
				Name:        art.name,
				Kind:        art.kind,
				Description: art.desc,
			})
		}

		// Phase DependsOn.
		consumesImplied := map[int]bool{}
		var needsImplied []int
		for _, art := range p.consumes {
			if ord, ok := producerOf[art.name]; ok && ord < p.ordinal {
				consumesImplied[ord] = true
			}
		}
		for _, st := range p.steps {
			for _, ref := range st.needs {
				if reStepRef.MatchString(ref) {
					if m := reStepRef.FindStringSubmatch(ref); m != nil {
						if ord, err := strconv.Atoi(m[1]); err == nil && ord < p.ordinal {
							needsImplied = append(needsImplied, ord)
						}
					}
					continue
				}
				if ord, ok := producerOf[ref]; ok && ord < p.ordinal {
					needsImplied = append(needsImplied, ord)
				}
			}
		}

		var deps []int
		if p.dependsLine != 0 {
			for _, cited := range p.dependsRefs {
				if cited >= 1 && cited < p.ordinal && cited <= len(doc.phases) {
					deps = append(deps, cited)
				}
			}
			// W2: explicit set fully implied by consumed artifacts.
			if len(consumesImplied) > 0 && len(deps) > 0 {
				fullyImplied := true
				for _, d := range deps {
					if !consumesImplied[d] {
						fullyImplied = false
						break
					}
				}
				if fullyImplied {
					*warnings = append(*warnings, fmt.Sprintf(warnDuplicate, p.name))
				}
			}
		} else {
			deps = append(deps, needsImplied...)
			for d := range consumesImplied {
				deps = append(deps, d)
			}
			if len(p.consumes) > 0 {
				*warnings = append(*warnings, fmt.Sprintf(warnInferred, p.name))
			}
		}
		sort.Ints(deps)
		spec.DependsOn = dedupInts(deps)

		cp.Phases = append(cp.Phases, spec)
	}
	return cp
}

// detectCycle walks the final phase dependency edges (producer ->
// consumer over consumed artifacts, i.e. the assembled DependsOn sets).
// Unreachable in v1 — every validated edge points backward — but
// implemented as Contract B's primary guard should ordering rules ever
// relax. White-box testable via compileCycleCheck's edge inputs.
func detectCycle(doc *draftDoc, problems *[]CompileProblem) {
	// Recompute the final edge set from the assembled model: for this we
	// mirror assemble()'s inference minimally (consumes producers ∪
	// needs producers ∪ validated explicit refs).
	producerOf := map[string]int{}
	for _, p := range doc.phases {
		for _, art := range p.produces {
			if _, dup := producerOf[art.name]; !dup {
				producerOf[art.name] = p.ordinal
			}
		}
	}
	n := len(doc.phases)
	nameOf := make([]string, n)
	edges := make([][]int, n) // dep (0-based) -> dependents
	for i, p := range doc.phases {
		nameOf[i] = p.name
	}
	for i, p := range doc.phases {
		addEdge := func(depOrd int) {
			if depOrd >= 1 && depOrd <= n && depOrd != p.ordinal {
				edges[depOrd-1] = append(edges[depOrd-1], i)
			}
		}
		for _, cited := range p.dependsRefs {
			if cited >= 1 && cited < p.ordinal {
				addEdge(cited)
			}
		}
		for _, art := range p.consumes {
			if ord, ok := producerOf[art.name]; ok && ord < p.ordinal {
				addEdge(ord)
			}
		}
		for _, st := range p.steps {
			for _, ref := range st.needs {
				if m := reStepRef.FindStringSubmatch(ref); m != nil {
					if ord, err := strconv.Atoi(m[1]); err == nil && ord < p.ordinal {
						addEdge(ord)
					}
					continue
				}
				if ord, ok := producerOf[ref]; ok && ord < p.ordinal {
					addEdge(ord)
				}
			}
		}
	}

	if cycle := compileCycleCheck(n, edges, nameOf); cycle != "" {
		*problems = append(*problems, CompileProblem{Line: 0, Message: fmt.Sprintf(cycleMsg, cycle)})
	}
}

// compileCycleCheck DFS-walks the edge set and returns the first cycle
// as " → "-joined phase names, or "" when the graph is a DAG. Split out
// for white-box testing (the class-5 path is unreachable via
// CompileSealed in v1 by construction).
func compileCycleCheck(n int, edges [][]int, names []string) string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := make([]int, n)
	var stack []int
	var found []int

	var visit func(v int) bool
	visit = func(v int) bool {
		color[v] = grey
		stack = append(stack, v)
		for _, w := range edges[v] {
			switch color[w] {
			case grey:
				// found cycle: slice stack from w
				start := 0
				for i, s := range stack {
					if s == w {
						start = i
						break
					}
				}
				found = append(append([]int{}, stack[start:]...), w)
				return true
			case white:
				if visit(w) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[v] = black
		return false
	}
	for v := 0; v < n; v++ {
		if color[v] == white && visit(v) {
			parts := make([]string, 0, len(found))
			for _, idx := range found {
				parts = append(parts, names[idx])
			}
			return strings.Join(parts, " → ")
		}
	}
	return ""
}

func dedupInts(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
