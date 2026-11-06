package lifecycle

// Origin identifies the provenance of a skill write: which funnel authored
// or last modified the SKILL.md content. It is stamped into the
// frontmatter (origin key) by Writer.WriteSkillWithOrigin so curation and
// trust ordering can reason about where each skill came from.
//
// Trust ordering (documented, not enforced): user > evolver > agent >
// review. user-authored skills are trusted most; evolver refinements are
// machine-authored from observed usage; agent skills are created or
// patched by the authoring tools; review is a human-reviewed revision.
// unknown is the legacy/unstamped read (bare WriteSkill, pre-origin
// skills, or hand-edited files).
type Origin string

// The pinned C2 value set. Gate tests reference these constants and their
// string values directly - do not rename.
const (
	OriginUnknown Origin = "unknown"
	OriginUser    Origin = "user"
	OriginAgent   Origin = "agent"
	OriginEvolver Origin = "evolver"
	OriginReview  Origin = "review"
)
