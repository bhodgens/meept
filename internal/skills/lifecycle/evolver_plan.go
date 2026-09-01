package lifecycle

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/caimlas/meept/internal/plan"
)

// Evolver plan provenance. Machine-originated (evolver) plans carry three
// extra Meta lines in the EXISTING plan.md `- key: value` format (the same
// shape internal/plan/writer.go WritePlanMarkdown serializes and
// internal/plan/parser.go parseMetaLine consumes) so the approval actuator
// (leaf 03) can find and dispatch them without a sidecar file or a parallel
// format:
//
//   - origin: skill-evolver
//   - proposal_id: <evolver proposal id>
//   - action: archive | refine | create
//
// Human-authored plans never carry these keys, so the IsEvolverPlan accessors
// distinguish the two populations exactly. The encoding lives here — next to
// the evolver's CreatePlan call site — so internal/plan stays untouched.
const (
	// EvolverPlanOrigin is the fixed origin marker stamped into every
	// evolver-created plan.
	EvolverPlanOrigin = "skill-evolver"

	// metaKeyOrigin / metaKeyProposalID / metaKeyAction are the Meta keys
	// used in the plan.md provenance block.
	metaKeyOrigin     = "origin"
	metaKeyProposalID = "proposal_id"
	metaKeyAction     = "action"
)

// evolverPlanProvenance is the in-memory provenance record for one evolver
// plan. The plan.md Meta block is the durable source of truth; this record
// lets same-process callers query a *plan.Plan without re-reading the file.
type evolverPlanProvenance struct {
	origin     string
	proposalID string
	action     string
}

// evolverProvenanceMu guards evolverProvenanceByPlanID. The evolver runs
// cycles from its scheduler goroutine; the mutex keeps the registry safe if
// cycles ever overlap (manual `meept skills evolve` + scheduled tick).
var (
	evolverProvenanceMu       sync.RWMutex
	evolverProvenanceByPlanID = make(map[string]evolverPlanProvenance)
)

// StampEvolverPlan marks p as an evolver-originated plan produced from
// proposalID with the given action ("archive" | "refine" | "create"). It
// registers the in-memory record and, when p.FilePath names an existing plan
// file, injects the provenance Meta lines into its `## Meta` section so the
// provenance round-trips through the plan file format. Nil p is a no-op;
// stamping is idempotent (an already-stamped file is left unchanged).
func StampEvolverPlan(p *plan.Plan, proposalID, action string) {
	if p == nil || p.ID == "" {
		return
	}
	evolverProvenanceMu.Lock()
	evolverProvenanceByPlanID[p.ID] = evolverPlanProvenance{
		origin:     EvolverPlanOrigin,
		proposalID: proposalID,
		action:     action,
	}
	evolverProvenanceMu.Unlock()

	if p.FilePath != "" {
		// The plan file already exists at the park site (CreatePlan wrote
		// it); enrich its Meta section in place. A missing file is not an
		// error — the in-memory record still answers the accessors.
		if err := stampEvolverPlanFile(p.FilePath, proposalID, action); err != nil && !os.IsNotExist(err) {
			// Best-effort enrichment: provenance also lives in memory, and
			// callers treat stamping as non-fatal bookkeeping.
			return
		}
	}
}

// stampEvolverPlanFile injects the provenance Meta lines into the `## Meta`
// section of the plan file at path, using the existing `- key: value` plan
// file format. Idempotent: a file already carrying an evolver origin line is
// left byte-identical.
func stampEvolverPlanFile(path, proposalID, action string) error {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the PlanManager-written plan
	if err != nil {
		return fmt.Errorf("read plan file for provenance stamp: %w", err)
	}
	content := string(data)
	if evolverOriginFromContent(content) != "" {
		return nil // already stamped
	}
	lines := []string{
		"- " + metaKeyOrigin + ": " + EvolverPlanOrigin,
		"- " + metaKeyProposalID + ": " + proposalID,
		"- " + metaKeyAction + ": " + action,
	}
	stamped, err := injectMetaLines(content, lines)
	if err != nil {
		return fmt.Errorf("stamp plan provenance: %w", err)
	}
	if err := os.WriteFile(path, []byte(stamped), 0o644); err != nil { //nolint:gosec // plan file, user-readable by design
		return fmt.Errorf("write stamped plan file: %w", err)
	}
	return nil
}

// injectMetaLines inserts lines immediately after the `## Meta` heading,
// preserving the rest of the document byte-for-byte. Errors when the content
// has no `## Meta` section (never expected for plans written by
// internal/plan's writer).
func injectMetaLines(content string, lines []string) (string, error) {
	idx := strings.Index(content, "## Meta")
	if idx < 0 {
		return "", fmt.Errorf("no `## Meta` section found")
	}
	lineEnd := strings.IndexByte(content[idx:], '\n')
	if lineEnd < 0 {
		// `## Meta` is the last line; append after it.
		return content[:idx+len("## Meta")] + "\n" +
			strings.Join(lines, "\n") + "\n", nil
	}
	insertAt := idx + lineEnd + 1
	return content[:insertAt] + strings.Join(lines, "\n") + "\n" + content[insertAt:], nil
}

// IsEvolverPlan reports whether p was created by the skill evolver, using the
// in-memory provenance registry. Nil-safe; returns false for plain
// (human-authored) plans. A plan with an empty ID never matches — registry
// entries are keyed by generated plan IDs, so a zero-value plan (as used in
// tests and by callers probing un-persisted plans) is never an evolver plan.
func IsEvolverPlan(p *plan.Plan) bool {
	if p == nil || p.ID == "" {
		return false
	}
	evolverProvenanceMu.RLock()
	defer evolverProvenanceMu.RUnlock()
	rec, ok := evolverProvenanceByPlanID[p.ID]
	return ok && rec.origin == EvolverPlanOrigin
}

// EvolverPlanProposalID returns the evolver proposal id a plan was created
// from, or "" for non-evolver plans. Nil-safe; an empty-ID plan returns "".
func EvolverPlanProposalID(p *plan.Plan) string {
	if p == nil || p.ID == "" {
		return ""
	}
	evolverProvenanceMu.RLock()
	defer evolverProvenanceMu.RUnlock()
	rec, ok := evolverProvenanceByPlanID[p.ID]
	if !ok {
		return ""
	}
	return rec.proposalID
}

// EvolverPlanAction returns the proposal action ("archive" | "refine" |
// "create") a plan was created from, or "" for non-evolver plans. Nil-safe;
// an empty-ID plan returns "".
func EvolverPlanAction(p *plan.Plan) string {
	if p == nil || p.ID == "" {
		return ""
	}
	evolverProvenanceMu.RLock()
	defer evolverProvenanceMu.RUnlock()
	rec, ok := evolverProvenanceByPlanID[p.ID]
	if !ok {
		return ""
	}
	return rec.action
}

// reEvolverMetaKV matches a provenance Meta line in the existing plan file
// format: `- key: value`. Mirrors internal/plan's reMetaKV shape.
var reEvolverMetaKV = regexp.MustCompile(`^-\s+(origin|proposal_id|action):\s+(.+)$`)

// evolverPlanMeta holds the provenance values recovered from a plan file's
// Meta section. Empty strings mean "not present".
type evolverPlanMeta struct {
	origin     string
	proposalID string
	action     string
}

// parseEvolverPlanMeta extracts evolver provenance from plan.md content by
// scanning the `## Meta` section for `- origin:` / `- proposal_id:` /
// `- action:` lines. Non-Meta sections are ignored, matching the scoping
// rules of internal/plan's parser.
func parseEvolverPlanMeta(content string) evolverPlanMeta {
	var meta evolverPlanMeta
	inMeta := false
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "## "):
			inMeta = line == "## Meta"
		case inMeta:
			if m := reEvolverMetaKV.FindStringSubmatch(line); m != nil {
				switch m[1] {
				case metaKeyOrigin:
					meta.origin = strings.TrimSpace(m[2])
				case metaKeyProposalID:
					meta.proposalID = strings.TrimSpace(m[2])
				case metaKeyAction:
					meta.action = strings.TrimSpace(m[2])
				}
			}
		}
	}
	return meta
}

// evolverOriginFromContent returns the origin value from a plan file's Meta
// section, or "" when the content carries no evolver origin line.
func evolverOriginFromContent(content string) string {
	return parseEvolverPlanMeta(content).origin
}

// IsEvolverPlanFile reports whether the plan file at path was created by the
// skill evolver (durable, cross-process check used by the approval actuator).
func IsEvolverPlanFile(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // caller-supplied plan path
	if err != nil {
		return false
	}
	return evolverOriginFromContent(string(data)) == EvolverPlanOrigin
}

// EvolverPlanProposalIDFile returns the evolver proposal id recorded in the
// plan file at path, or "" when absent/unreadable.
func EvolverPlanProposalIDFile(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // caller-supplied plan path
	if err != nil {
		return ""
	}
	return parseEvolverPlanMeta(string(data)).proposalID
}

// EvolverPlanActionFile returns the proposal action recorded in the plan file
// at path, or "" when absent/unreadable.
func EvolverPlanActionFile(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // caller-supplied plan path
	if err != nil {
		return ""
	}
	return parseEvolverPlanMeta(string(data)).action
}

// evolverProposalIDCounter sequences proposal ids within the process so each
// stamped plan carries a distinct, sortable identifier.
var evolverProposalIDCounter atomic.Uint64

// evolverProposalID derives a stable proposal identifier for an evolution
// proposal. EvolutionProposal has no inherent id field, so the identifier is
// composed from the skill name and action plus a process-wide sequence —
// deterministic for a given proposal within a run and unique across plans.
func evolverProposalID(p EvolutionProposal) string {
	seq := evolverProposalIDCounter.Add(1)
	return fmt.Sprintf("evo-%s-%s-%06d", p.Action, sanitizeIDPart(p.SkillName), seq)
}

// sanitizeIDPart makes a skill name safe for embedding in a proposal id:
// lowercase, non-alphanumerics collapsed to hyphens, trimmed.
func sanitizeIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastHyphen := true // suppress leading hyphen
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
