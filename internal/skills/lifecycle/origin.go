package lifecycle

import (
	"errors"
	"strings"
)

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

// ErrUserClobber is returned by Writer.WriteSkillWithOrigin when a
// non-user funnel (agent/evolver/review/unstamped) attempts to overwrite
// a skill whose on-disk frontmatter declares `origin: user` (bughunt
// config-tools F-4). The candidate content is NOT lost: the writer
// mirrors it to <SKILL.md>.new for a human merge. errors.Is-compatible.
var ErrUserClobber = errors.New("skill declares origin: user; overwrite refused")

// skillFileOrigin reads the declared Origin from raw SKILL.md bytes
// without a full parse: the top-level frontmatter `origin:` key only
// (indented lines are nested values, not the key). Anything unreadable,
// absent, unknown-valued, or legacy/unstamped reads as OriginUnknown —
// so the guard fires ONLY on a proven `origin: user` declaration.
func skillFileOrigin(data []byte) Origin {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return OriginUnknown
	}
	rest := strings.TrimPrefix(text, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return OriginUnknown
	}
	for _, l := range strings.Split(rest[:idx], "\n") {
		if l == "" {
			continue
		}
		if l[0] == ' ' || l[0] == '	' {
			continue // nested under another key, not the top-level origin
		}
		if l == "origin:" || strings.HasPrefix(l, "origin: ") {
			v := strings.TrimSpace(strings.TrimPrefix(l, "origin:"))
			switch Origin(v) {
			case OriginUser:
				return OriginUser
			case OriginAgent:
				return OriginAgent
			case OriginEvolver:
				return OriginEvolver
			case OriginReview:
				return OriginReview
			default:
				return OriginUnknown
			}
		}
	}
	return OriginUnknown
}
