package skills

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrSkillNotFound is returned by ResolveTierPath when the named skill does
// not exist in any configured discovery tier. Callers can detect this case
// with errors.Is.
var ErrSkillNotFound = errors.New("skill not found in any discovery tier")

// ResolveTierPath returns the tier actually holding the named skill, per the
// same precedence discovery itself applies (project > user > claude > hermes >
// system; lower Priority value wins).
//
// It runs a fresh discovery pass (so the lookup reflects current disk state)
// and derives its answer from the discovery results' own Path/Priority/Source
// metadata — the tier list is never duplicated here.
//
// tierRoot is the skills directory of the winning tier (e.g. the resolved
// Claude tier directory on machines where the skill lives there); skillPath
// is the SKILL.md file inside it. The returned paths use the filesystem
// casing of the on-disk directory, not the requested skill name.
func (d *Discovery) ResolveTierPath(skillName string) (tierRoot, skillPath, source string, err error) {
	if strings.TrimSpace(skillName) == "" {
		return "", "", "", fmt.Errorf("%w: empty skill name", ErrSkillNotFound)
	}

	found, err := d.Discover()
	if err != nil {
		return "", "", "", fmt.Errorf("resolve tier path: discovery failed: %w", err)
	}

	key := normalizeName(skillName)
	for _, skill := range found {
		if normalizeName(skill.Name) != key {
			continue
		}

		// Discovery's merge logic already keeps only the highest-priority
		// (lowest Priority value) variant per name, so this match is exactly
		// the tier discovery itself would serve.
		// skillPath: <tier>/<name>/SKILL.md (directory layout) or
		// <tier>/<name>.md (flat layout).
		skillDir := filepath.Dir(skill.Path)
		tierRoot := skillDir
		if filepath.Base(skill.Path) == "SKILL.md" {
			// Directory layout: strip the skill-name directory to get the tier.
			tierRoot = filepath.Dir(skillDir)
		}
		return tierRoot, skill.Path, sourceForSkill(skill), nil
	}

	return "", "", "", fmt.Errorf("%w: %q", ErrSkillNotFound, skillName)
}

// sourceForSkill reports which tier system a discovered skill came from,
// preferring the explicit Source/SourceOrigin markers discovery sets and
// falling back to the priority constant.
func sourceForSkill(skill *Skill) string {
	if skill.Source != "" {
		return skill.Source
	}
	if skill.SourceOrigin != "" {
		return skill.SourceOrigin
	}
	switch skill.Priority {
	case PriorityProject:
		return "meept"
	case PriorityUser:
		return "meept"
	case PriorityClaude:
		return "claude"
	case PriorityHermes:
		return "hermes"
	case PrioritySystem:
		return "meept"
	default:
		return "meept"
	}
}
