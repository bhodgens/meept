package plan

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Defaults for zero-valued TreeEmitOptions fields.
const (
	defaultMaxLeavesPerPhase = 3
	defaultLeafCharBudget    = 14_000
)

// flatTotalSteps is the flat-vs-tree total-step threshold (master.md
// Contract D): at or below it — and with every phase fitting its per-leaf
// sizing — the existing flat-steps path stays.
const flatTotalSteps = 6

// TreeEmitOptions bounds how EmitTree partitions a CompiledPlan into
// hierarchical leaves. Zero fields fall back to the defaults above.
type TreeEmitOptions struct {
	MaxLeavesPerPhase int // default 3 when zero
	LeafCharBudget    int // default 14_000 when zero (~128K tokens of agent context worth of spec text)
}

// LeafFile is one emitted implementation-leaf document.
type LeafFile struct {
	Path    string // "<NN>-<slug>.md", zero-padded, unique
	Content string
}

// EmittedTree is the full hierarchical plan tree: the master document plus
// its ordered leaf files.
type EmittedTree struct {
	Root   string     // master.md content
	Leaves []LeafFile // e.g. "01-auth.md", "02-store.md"
}

// errEmptyPlan is returned when the input carries no phases or no steps —
// there is nothing to render a tree from.
var errEmptyPlan = errors.New("tree emission: compiled plan has no phases or steps")

// ShouldEmitTree reports whether a CompiledPlan should render as a
// hierarchical tree instead of flat steps. Tree when any phase's per-leaf
// step share exceeds the MaxLeavesPerPhase sizing, or any phase's intent
// text exceeds LeafCharBudget. Flat when total steps ≤ 6 AND every phase
// fits.
func ShouldEmitTree(cp *CompiledPlan, opts TreeEmitOptions) bool {
	if cp == nil || len(cp.Phases) == 0 {
		return false
	}
	maxLeaves, charBudget := resolveOpts(opts)
	total := 0
	for i := range cp.Phases {
		phase := &cp.Phases[i]
		total += len(phase.Steps)
		// A phase fits when its steps fit one leaf under the cap sizing
		// (partitionPhases keeps a ≤cap-step phase in a single leaf).
		if len(phase.Steps) > maxLeaves {
			return true
		}
		if len(strings.TrimSpace(phase.Description)) > charBudget {
			return true
		}
	}
	return total > flatTotalSteps
}

// EmitTree renders a CompiledPlan as the hierarchical-planning-style tree
// (master.md root + numbered leaf documents). Pure: no I/O, no clocks; the
// same input always yields byte-identical output.
func EmitTree(cp *CompiledPlan, opts TreeEmitOptions) (*EmittedTree, error) {
	if cp == nil || len(cp.Phases) == 0 || totalSteps(cp) == 0 {
		return nil, errEmptyPlan
	}
	maxLeaves, charBudget := resolveOpts(opts)

	leaves := partitionPhases(cp, maxLeaves, charBudget)
	groups := concurrencyGroups(cp)
	leafDeps := leafDependencies(cp, leaves)
	est := leafContextEstimates(cp, leaves)

	var b strings.Builder
	b.WriteString(rootMeta(cp, len(leaves)))
	b.WriteString(rootGoal(cp))
	b.WriteString(rootArchitecture(cp))
	b.WriteString(rootContracts(cp))
	b.WriteString(rootChildIndex(leaves, leafDeps, est, groups))
	b.WriteString(rootDispatchProtocol(cp, leaves, groups))
	b.WriteString(rootReviewChecklist)
	b.WriteString(rootCodingConventions)
	b.WriteString(rootCompletionTable(leaves))
	b.WriteString(rootIntegrationTestPlan(cp))
	b.WriteString(rootNotes)

	tree := &EmittedTree{Root: b.String(), Leaves: make([]LeafFile, len(leaves))}
	for i := range leaves {
		tree.Leaves[i] = LeafFile{
			Path:    leaves[i].Path,
			Content: renderLeaf(cp, leaves, leafDeps, est, groups, i),
		}
	}
	return tree, nil
}

// sectionPresent reports whether the exact "## <name>" heading appears in
// doc. Mirrors the Hermes skill's check_template_compliance.py section
// scan so the compliance guarantee is testable in-repo.
func sectionPresent(doc, section string) bool {
	for _, ln := range strings.Split(doc, "\n") {
		if strings.TrimRight(ln, " \t") == section {
			return true
		}
	}
	return false
}

// RequiredRootSections lists the sections every emitted root must carry, in
// order. Package-level so leaf 04's integration test can import the list.
var RequiredRootSections = []string{
	"## Meta",
	"## Goal",
	"## Architecture",
	"## Interface Contracts",
	"## Child Document Index",
	"## Dispatch Protocol",
	"## Review Checklist",
	"## Coding Conventions",
	"## Completion Tracking Table",
	"## Integration Test Plan",
	"## Notes",
}

// RequiredLeafSections lists the sections every emitted leaf must carry.
// "DO NOT COMMIT" is asserted separately (it is a rule, not a heading).
var RequiredLeafSections = []string{
	"## Meta",
	"## Goal",
	"## Interface Contracts",
	"## Tasks",
	"## Self-Verification Checklist",
	"## Review Checklist",
	"## Notes",
}

// ---------------------------------------------------------------------------
// Sizing + partitioning
// ---------------------------------------------------------------------------

// resolveOpts applies defaults for zero-valued options.
func resolveOpts(opts TreeEmitOptions) (maxLeaves, charBudget int) {
	maxLeaves = opts.MaxLeavesPerPhase
	if maxLeaves <= 0 {
		maxLeaves = defaultMaxLeavesPerPhase
	}
	charBudget = opts.LeafCharBudget
	if charBudget <= 0 {
		charBudget = defaultLeafCharBudget
	}
	return maxLeaves, charBudget
}

// totalSteps counts steps across every phase.
func totalSteps(cp *CompiledPlan) int {
	n := 0
	for i := range cp.Phases {
		n += len(cp.Phases[i].Steps)
	}
	return n
}

// stepsPerLeaf is the per-leaf step share for a phase of n steps under the
// maxLeaves cap: a phase with n ≤ maxLeaves steps fits one leaf (share =
// n); larger phases spread across exactly maxLeaves-sized shares
// (ceil(n/maxLeaves) steps per leaf).
func stepsPerLeaf(n, maxLeaves int) int {
	if maxLeaves <= 0 {
		maxLeaves = defaultMaxLeavesPerPhase
	}
	if n <= maxLeaves {
		return n
	}
	return (n + maxLeaves - 1) / maxLeaves
}

// partitionPhases splits each phase's steps into leaves without ever
// splitting a step: order preserved, and the union of leaf steps equals the
// phase steps. Leaves open when the current one is at its per-leaf share
// (ceil(steps/maxLeaves), capped at maxLeaves leaves per phase) or when the
// next step would push the leaf past the character budget. Paths derive
// from the leaf's leading step description; slugs come from slugify.
func partitionPhases(cp *CompiledPlan, maxLeaves, charBudget int) []emitLeaf {
	var leaves []emitLeaf
	for pi := range cp.Phases {
		phase := &cp.Phases[pi]
		if len(phase.Steps) == 0 {
			continue
		}
		perLeaf := stepsPerLeaf(len(phase.Steps), maxLeaves)
		for si := range phase.Steps {
			step := &phase.Steps[si]
			// Open a new leaf when none is open, when the open one is at
			// its step share, or when the next step would push the leaf
			// past the character budget (the budget wins over the leaf
			// cap — an oversized phase splits beyond the cap rather than
			// emitting over-budget leaves). A single step larger than the
			// budget still gets its own leaf: steps are never dropped and
			// never split. Sizing alone yields ≤ maxLeaves leaves per
			// phase because perLeaf = ceil(steps/maxLeaves).
			needNew := len(leaves) == 0
			if !needNew {
				last := &leaves[len(leaves)-1]
				needNew = last.phaseIdx != pi ||
					len(last.stepIdxs) >= perLeaf ||
					leafChars(last, phase)+len(step.Description) > charBudget
			}
			if needNew {
				leaves = append(leaves, emitLeaf{
					Path: fmt.Sprintf("%02d-%s.md", len(leaves)+1,
						slugOrFallback(phase, step.Description)),
					phaseIdx: pi,
					stepIdxs: []int{si},
				})
				continue
			}
			last := &leaves[len(leaves)-1]
			last.stepIdxs = append(last.stepIdxs, si)
		}
	}
	return leaves
}

// slugOrFallback slugs a step description for leaf filenames, falling back
// to the phase name when a description is blank so paths stay unique.
func slugOrFallback(phase *PhaseSpec, description string) string {
	if s := slugify(description); s != "" {
		return s
	}
	if s := slugify(phase.Name); s != "" {
		return s
	}
	return "leaf"
}

// leafChars sums the description lengths of the steps a leaf owns.
func leafChars(lf *emitLeaf, phase *PhaseSpec) int {
	n := 0
	for _, si := range lf.stepIdxs {
		n += len(phase.Steps[si].Description)
	}
	return n
}

// emitLeaf is one leaf under construction: the step indices it owns (all
// within one phase) plus which phase owns it.
type emitLeaf struct {
	Path     string
	phaseIdx int
	stepIdxs []int
}

// ---------------------------------------------------------------------------
// Concurrency groups — consumes-edge frontier (NOT phase order)
// ---------------------------------------------------------------------------

// concurrencyGroups assigns each phase a 1-based concurrency wave derived
// from consumes edges: a phase lands one wave after the lowest wave of any
// producer of its consumed artifacts. Phases with no intra-plan consumes
// share wave 1. Relaxation converges because the compiler rejects cycles;
// phase order is only an iteration tiebreak. Deterministic.
func concurrencyGroups(cp *CompiledPlan) []int {
	n := len(cp.Phases)
	group := make([]int, n)
	producerWave := make(map[string]int)
	for changed := true; changed; {
		changed = false
		for i := 0; i < n; i++ {
			phase := &cp.Phases[i]
			g := 1
			for _, a := range phase.Consumes {
				if pw, ok := producerWave[a.Name]; ok && pw+1 > g {
					g = pw + 1
				}
			}
			if g != group[i] {
				group[i] = g
				changed = true
			}
			for _, a := range phase.Produces {
				if pw, ok := producerWave[a.Name]; !ok || g < pw {
					producerWave[a.Name] = g
				}
			}
		}
	}
	return group
}

// leafDependencies computes, per leaf, the ordered unique dependency paths.
// Two edge sources feed it:
//
//  1. Artifact edges: every artifact the leaf's phase consumes must come
//     from an earlier leaf — either inside the same leaf (intra-leaf, no
//     edge) or from a producing leaf declared in Dependencies. Artifacts
//     no leaf produces create no edge (matches the frontier's
//     declared-but-never-produced policy).
//  2. In-phase chaining: when one phase splits across several leaves,
//     each leaf depends on the leaf before it in emission order. Without
//     this, split-phase leaves share a concurrency group with no
//     ordering edge and their agents would run concurrently inside one
//     phase. Artifact-derived edges are preserved — chaining only adds
//     the missing predecessor edge, never drops a consumes edge.
func leafDependencies(cp *CompiledPlan, leaves []emitLeaf) [][]string {
	produceLeaf := make(map[string]int)
	for li := range leaves {
		phase := &cp.Phases[leaves[li].phaseIdx]
		for _, a := range phase.Produces {
			if _, ok := produceLeaf[a.Name]; !ok {
				produceLeaf[a.Name] = li
			}
		}
	}
	deps := make([][]string, len(leaves))
	for li := range leaves {
		phase := &cp.Phases[leaves[li].phaseIdx]
		seen := make(map[string]bool)
		var paths []string
		// In-phase predecessor: leaves are emitted in partition order,
		// so leaf k chains to leaf k-1 when both belong to the same
		// phase. The seen set keeps the dedup if a consumes edge also
		// names this predecessor (and orders the predecessor first).
		if li > 0 && leaves[li-1].phaseIdx == leaves[li].phaseIdx {
			seen[leaves[li-1].Path] = true
			paths = append(paths, leaves[li-1].Path)
		}
		for _, a := range phase.Consumes {
			src, ok := produceLeaf[a.Name]
			if !ok || src == li || seen[leaves[src].Path] {
				continue
			}
			seen[leaves[src].Path] = true
			paths = append(paths, leaves[src].Path)
		}
		deps[li] = paths
	}
	return deps
}

// leafContextEstimates returns a "~<N>K" context estimate per leaf, derived
// from its step description characters plus its phase intent, at roughly
// 4K characters of spec text per K of agent context.
func leafContextEstimates(cp *CompiledPlan, leaves []emitLeaf) []string {
	est := make([]string, len(leaves))
	for li := range leaves {
		phase := &cp.Phases[leaves[li].phaseIdx]
		chars := len(phase.Description) + 2_000 // contract + meta scaffolding
		chars += leafChars(&leaves[li], phase)
		est[li] = fmt.Sprintf("~%dK", chars/4_000+1)
	}
	return est
}

// ---------------------------------------------------------------------------
// Root rendering
// ---------------------------------------------------------------------------

// rootReviewChecklist, rootCodingConventions, rootNotes are fixed house-text
// sections; package-level so leaf 04's integration tests can reference them.
var (
	rootReviewChecklist = "## Review Checklist\n\n" +
		"Root-level review, applied by the orchestrator after each leaf lands:\n\n" +
		"- [ ] Every leaf reviewed in-session against its Interface Contracts (From Parent)\n" +
		"- [ ] Leaf steps executed in order; no step skipped or silently dropped\n" +
		"- [ ] Cross-leaf dependencies satisfied before dependent leaves start\n" +
		"- [ ] All leaves' verification commands pass under -p 2\n" +
		"- [ ] No leaf committed work directly — the orchestrator handles git\n\n"

	rootCodingConventions = "## Coding Conventions\n\n" +
		"- Errors wrapped with fmt.Errorf(\"context: %w\", err); no ignored errors in non-test code\n" +
		"- No I/O, no clocks, no globals in pure helpers; take structs in, return structs out\n" +
		"- Tests table-driven where practical; run multi-package suites with -p 2\n" +
		"- gofmt clean; no placeholder values or debug artifacts in committed code\n\n"

	rootNotes = "## Notes\n\n" +
		"- This tree was emitted deterministically from the compiled plan; leaf numbering and concurrency groups derive only from the sealed input.\n" +
		"- Leaf Tasks sections carry objectives and verification guidance, not implementation code — code arrives when a leaf agent executes the tree.\n" +
		"- The orchestrator updates the Completion Tracking Table after each leaf review.\n\n"
)

func rootMeta(cp *CompiledPlan, childCount int) string {
	var b strings.Builder
	b.WriteString("## Meta\n\n")
	fmt.Fprintf(&b, "- **Role:** Root\n")
	fmt.Fprintf(&b, "- **Parent:** none\n")
	fmt.Fprintf(&b, "- **Children:** %d\n", childCount)
	fmt.Fprintf(&b, "- **Scope:** Execute the compiled plan as a hierarchical tree; every leaf bounds one implementation agent's scope of work.\n")
	if cp.Hash != "" {
		fmt.Fprintf(&b, "- **Sealed input:** sha256:%s\n", cp.Hash)
	}
	b.WriteString("\n")
	return b.String()
}

func rootGoal(cp *CompiledPlan) string {
	var b strings.Builder
	b.WriteString("## Goal\n\n")
	fmt.Fprintf(&b, "%s\n\n", planTitle(cp))
	return b.String()
}

// planTitle derives the plan title from the compiled plan: the first
// sentence of the first phase that carries intent prose, or a stable
// fallback.
func planTitle(cp *CompiledPlan) string {
	for i := range cp.Phases {
		if s := firstSentence(cp.Phases[i].Description); s != "" {
			return s
		}
	}
	return "Compiled plan"
}

// firstSentence returns text up to and including the first sentence-ending
// punctuation, or the whole trimmed string.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".!?"); i >= 0 {
		return s[:i+1]
	}
	return s
}

func rootArchitecture(cp *CompiledPlan) string {
	var b strings.Builder
	b.WriteString("## Architecture\n\n")
	for i := range cp.Phases {
		fmt.Fprintf(&b, "### Phase %d: %s\n\n", i+1, cp.Phases[i].Name)
		if intent := strings.TrimSpace(cp.Phases[i].Description); intent != "" {
			fmt.Fprintf(&b, "%s\n\n", intent)
		} else {
			b.WriteString("(no intent prose recorded for this phase)\n\n")
		}
	}
	return b.String()
}

// rootContracts renders the frozen Interface Contracts: one
// "### Contract <letter>: <name>" per phase; body = that phase's produces
// (exposed) + consumes (consumed) as Go-ish comment blocks, matching the
// house style. The heading is the exact "## Interface Contracts" line the
// section-presence scan requires; the "(frozen)" qualifier lives in the
// body so the exact-line check still passes.
func rootContracts(cp *CompiledPlan) string {
	var b strings.Builder
	b.WriteString("## Interface Contracts\n\n")
	b.WriteString("Frozen. One contract per phase; do not edit while leaves are in flight.\n\n")
	for i := range cp.Phases {
		phase := &cp.Phases[i]
		fmt.Fprintf(&b, "### Contract %s: %s\n\n", contractLetter(i), phase.Name)
		b.WriteString("Exposed by this contract (produces):\n\n")
		if len(phase.Produces) == 0 {
			b.WriteString("// (none)\n\n")
		}
		for _, a := range phase.Produces {
			fmt.Fprintf(&b, "// %s (%s) — %s\n", a.Name, a.Kind, a.Description)
		}
		if len(phase.Produces) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Consumed by this contract (consumes):\n\n")
		if len(phase.Consumes) == 0 {
			b.WriteString("// (none)\n\n")
		}
		for _, a := range phase.Consumes {
			fmt.Fprintf(&b, "// %s (%s) — %s\n", a.Name, a.Kind, a.Description)
		}
		if len(phase.Consumes) > 0 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// contractLetter maps a 0-based phase index to a stable A, B, ... label;
// beyond Z it falls back to a deterministic indexed form.
func contractLetter(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return "C" + strconv.Itoa(i-25)
}

// rootChildIndex renders the leaf table: # / Document / Type / Dependencies
// / Est. Context / Concurrency. Concurrency comes from the consumes-edge
// frontier, not phase order.
func rootChildIndex(leaves []emitLeaf, deps [][]string, est []string, groups []int) string {
	var b strings.Builder
	b.WriteString("## Child Document Index\n\n")
	b.WriteString("| # | Document | Type | Dependencies | Est. Context | Concurrency |\n")
	b.WriteString("|---|----------|------|--------------|--------------|-------------|\n")
	for i := range leaves {
		dep := "none"
		if len(deps[i]) > 0 {
			dep = strings.Join(deps[i], ", ")
		}
		fmt.Fprintf(&b, "| %d | %s | leaf | %s | %s | %d |\n",
			i+1, leaves[i].Path, dep, est[i], groups[leaves[i].phaseIdx])
	}
	b.WriteString("\n")
	return b.String()
}

// rootDispatchProtocol renders per-concurrency-group dispatch instructions:
// delegate_task goal + "Do NOT commit" + read-only exploration rules,
// matching the house template.
func rootDispatchProtocol(cp *CompiledPlan, leaves []emitLeaf, groups []int) string {
	var b strings.Builder
	b.WriteString("## Dispatch Protocol\n\n")
	b.WriteString("Dispatch leaves in concurrency-group order (group 1 first). Within a group, dispatch leaves whose Dependencies column is \"none\" together; any leaf listing dependencies must start only after those leaves complete. (A phase split across several leaves chains its leaves in emission order — leaf 2 of the phase depends on leaf 1 — so split-phase leaves dispatch one at a time.) Every dispatch includes: \"Do NOT commit. Do NOT run git add.\" The orchestrator handles all git operations after review. Leaf agents explore read-only outside their declared Files.\n\n")
	for g := 1; g <= maxGroup(groups); g++ {
		fmt.Fprintf(&b, "### Group %d\n\n", g)
		for i := range leaves {
			if groups[leaves[i].phaseIdx] != g {
				continue
			}
			fmt.Fprintf(&b, "1. **Read** %s and dispatch via delegate_task:\n", leaves[i].Path)
			fmt.Fprintf(&b, "   - Goal: %q\n", dispatchGoal(cp, &leaves[i]))
			fmt.Fprintf(&b, "   - Context: leaf text + the root's Interface Contracts INLINED\n")
			b.WriteString("   - Include: \"Do NOT commit. Do NOT run git add.\"\n")
			b.WriteString("   - Explore read-only outside the leaf's declared Files; no writes outside its scope.\n\n")
		}
	}
	return b.String()
}

// maxGroup returns the highest concurrency group number (0 when empty).
func maxGroup(groups []int) int {
	m := 0
	for _, g := range groups {
		if g > m {
			m = g
		}
	}
	return m
}

// dispatchGoal derives the delegate_task goal for one leaf from its leading
// step objective — never invented prose.
func dispatchGoal(cp *CompiledPlan, lf *emitLeaf) string {
	phase := &cp.Phases[lf.phaseIdx]
	if len(lf.stepIdxs) > 0 {
		if d := strings.TrimSpace(phase.Steps[lf.stepIdxs[0]].Description); d != "" {
			return d
		}
	}
	return "Execute the leaf tasks per the leaf document"
}

func rootCompletionTable(leaves []emitLeaf) string {
	var b strings.Builder
	b.WriteString("## Completion Tracking Table\n\n")
	b.WriteString("| Child | Status | Iterations | Review Notes |\n")
	b.WriteString("|-------|--------|------------|-------------|\n")
	for i := range leaves {
		fmt.Fprintf(&b, "| %s | PENDING | 0 | |\n", leaves[i].Path)
	}
	b.WriteString("\nStatus values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED\n\n")
	return b.String()
}

// rootIntegrationTestPlan renders the Integration Test Plan: the plan's
// warning/acceptance lines (compile-surfaced acceptance text) followed by
// the fixed per-leaf verification sweep.
func rootIntegrationTestPlan(cp *CompiledPlan) string {
	var b strings.Builder
	b.WriteString("## Integration Test Plan\n\n")
	n := 0
	for _, w := range cp.Warnings {
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, w)
	}
	n++
	fmt.Fprintf(&b, "%d. Run every leaf's verification guidance; all checks must pass with real output.\n", n)
	n++
	fmt.Fprintf(&b, "%d. Run the touched packages' suite with -race -p 2 before closing the tree.\n", n)
	n++
	fmt.Fprintf(&b, "%d. Confirm every Completion Tracking row reaches COMPLETE before closing the tree.\n", n)
	b.WriteString("\n")
	return b.String()
}

// ---------------------------------------------------------------------------
// Leaf rendering
// ---------------------------------------------------------------------------

// renderLeaf renders one leaf document. Contracts copy from the leaf's own
// phase; dependencies name earlier leaves; tasks render step descriptions +
// tool hints + verify guidance as skeletons — NEVER invented Go code.
func renderLeaf(cp *CompiledPlan, leaves []emitLeaf, deps [][]string, est []string, groups []int, li int) string {
	lf := &leaves[li]
	phase := &cp.Phases[lf.phaseIdx]

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — Implementation Leaf\n\n", leafTitle(phase, lf))
	b.WriteString("## Meta\n\n")
	fmt.Fprintf(&b, "- **Parent:** master.md\n")
	fmt.Fprintf(&b, "- **Scope:** Phase %d (%s), steps %s of the compiled plan; only these steps, no cross-leaf work.\n",
		lf.phaseIdx+1, phase.Name, stepRange(lf))
	fmt.Fprintf(&b, "- **Dependencies:** %s\n", depsList(deps[li]))
	fmt.Fprintf(&b, "- **Est. Context:** %s\n", est[li])
	fmt.Fprintf(&b, "- **Concurrency Group:** %d\n\n", groups[lf.phaseIdx])
	b.WriteString(leafGoal(phase))
	b.WriteString(leafContracts(phase))
	b.WriteString(leafTasks(phase, lf))
	b.WriteString(leafSelfVerification)
	b.WriteString(leafReviewChecklist)
	b.WriteString(leafNotes)
	return b.String()
}

// leafTitle derives the leaf H1 from its leading step objective, falling
// back to the phase name.
func leafTitle(phase *PhaseSpec, lf *emitLeaf) string {
	if len(lf.stepIdxs) > 0 {
		if s := firstSentence(phase.Steps[lf.stepIdxs[0]].Description); s != "" {
			return s
		}
	}
	return phase.Name
}

// stepRange renders the leaf's 1-based step span within its phase, e.g.
// "1-3" or "4".
func stepRange(lf *emitLeaf) string {
	if len(lf.stepIdxs) == 0 {
		return "none"
	}
	lo := lf.stepIdxs[0] + 1
	hi := lf.stepIdxs[len(lf.stepIdxs)-1] + 1
	if lo == hi {
		return strconv.Itoa(lo)
	}
	return fmt.Sprintf("%d-%d", lo, hi)
}

func depsList(deps []string) string {
	if len(deps) == 0 {
		return "none"
	}
	return strings.Join(deps, ", ")
}

// leafGoal renders the phase-intent slice assigned to this leaf.
func leafGoal(phase *PhaseSpec) string {
	var b strings.Builder
	b.WriteString("## Goal\n\n")
	intent := strings.TrimSpace(phase.Description)
	if intent == "" {
		intent = "(no intent prose recorded for this phase)"
	}
	fmt.Fprintf(&b, "%s\n\n", intent)
	return b.String()
}

// leafContracts copies the owning phase's frozen contract from the root
// into the leaf. Heading stays the exact "## Interface Contracts" line the
// section-presence scan requires; "(From Parent)" lives in the body.
func leafContracts(phase *PhaseSpec) string {
	var b strings.Builder
	b.WriteString("## Interface Contracts\n\n")
	b.WriteString("From parent, frozen:\n\n")
	if len(phase.Produces) == 0 && len(phase.Consumes) == 0 {
		b.WriteString("// (no artifacts declared for this phase)\n\n")
		return b.String()
	}
	for _, a := range phase.Produces {
		fmt.Fprintf(&b, "// exposed: %s (%s) — %s\n", a.Name, a.Kind, a.Description)
	}
	for _, a := range phase.Consumes {
		fmt.Fprintf(&b, "// consumed: %s (%s) — %s\n", a.Name, a.Kind, a.Description)
	}
	b.WriteString("\n")
	return b.String()
}

// leafTasks renders one task skeleton per step: Objective / Files / Verify
// guidance derived from the step description and tool hint. TDD steps
// render as guidance, not Go code — the emitter never invents code.
func leafTasks(phase *PhaseSpec, lf *emitLeaf) string {
	var b strings.Builder
	b.WriteString("## Tasks\n\n")
	for n, si := range lf.stepIdxs {
		step := &phase.Steps[si]
		fmt.Fprintf(&b, "### Task %d\n\n", n+1)
		fmt.Fprintf(&b, "**Objective:** %s\n\n", strings.TrimSpace(step.Description))
		fmt.Fprintf(&b, "**Files:** derive from the step's declared artifacts; no pre-named files — decide during execution and stay inside this leaf's scope.\n\n")
		hint := step.ToolHint
		if hint == "" {
			hint = "code"
		}
		fmt.Fprintf(&b, "**Verify:** guidance only — work via the %q tool hint; write the check before the change (TDD); run the touched package's tests with -p 2 and report real output.\n\n", hint)
	}
	return b.String()
}

var (
	leafSelfVerification = "## Self-Verification Checklist\n\n" +
		"- [ ] Every Task objective above is addressed; no step skipped\n" +
		"- [ ] Verification guidance executed with real output, not asserted from memory\n" +
		"- [ ] Only files within this leaf's declared scope were created or modified\n" +
		"- [ ] gofmt clean; no ignored errors in non-test code\n\n" +
		"**DO NOT COMMIT.** Do NOT run git add. The orchestrator handles git after review.\n\n"

	leafReviewChecklist = "## Review Checklist\n\n" +
		"For the review agent:\n\n" +
		"- [ ] Leaf stayed inside its Scope; no cross-leaf edits\n" +
		"- [ ] Contract artifacts (produces) match the parent's frozen Interface Contracts\n" +
		"- [ ] Dependencies were satisfied before this leaf's work began\n" +
		"- [ ] Verification commands actually ran; results reported honestly\n\n"

	leafNotes = "## Notes\n\n" +
		"- Tasks are skeletons: objectives and verification guidance only. Implementation code arrives when this leaf is executed by an implementation agent.\n" +
		"- Deterministic emission: this leaf's numbering and dependencies derive solely from the sealed input.\n\n"
)
