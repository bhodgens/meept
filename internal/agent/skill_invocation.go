package agent

// 04-skill-invocation leaf: the explicit `/<skill-name>` direct-invocation
// gesture. A chat token shaped ` /name` (start/whitespace before, whitespace
// or end after) makes that skill's body inject as an explicit "user asked
// for this skill" instruction before the LLM call — no fuzzy-keyword
// routing involved. Unknown names and skills whose frontmatter sets
// disable-model-invocation stay plain prose (dsh 1d pre-step semantics:
// only the user's own claimed input is scanned, never tool or assistant
// content, so external text cannot forge the gesture).
//
// This is DISTINCT from the named-AGENT route (named_agent_route.go), which
// targets a roster agent by name; the gesture never routes turns, it only
// enriches the current turn's context.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/skills"
)

// skillGestureRe is the dsh gesture grammar (packages/skill/tool-skill/src/
// index.ts:429): the name is kebab-case lowercase alphanumerics with single
// hyphens, delimited by whitespace/string boundaries on both sides of the
// leading slash. Go's regexp has no lookahead, so the trailing boundary is
// enforced by post-match check (matched name followed by a delimiter or
// end-of-string); the leading boundary comes from the (^|\s) group. Paths
// (/usr/bin), fractions (5/8), and word/word compounds cannot match: a
// second slash or an uppercase letter breaks the name class.
var skillGestureRe = regexp.MustCompile(`(^|\s)/([a-z0-9]+(?:-[a-z0-9]+)*)`)

// gestureTrailingOK reports whether the character after a matched name is a
// valid delimiter (whitespace or end of string). Go regexp lacks lookahead,
// so invokedSkillNames applies this per match; the check is what keeps
// "/review-code/" (trailing slash = path-like, not a bare gesture) and
// "/review-code2" (name run-on) from matching.
func gestureTrailingOK(text string, loc []int) bool {
	end := loc[5] // end index of group 2 (the name)
	if end >= len(text) {
		return true
	}
	switch text[end] {
	case ' ', '	', '\n', '\r':
		return true
	default:
		return false
	}
}

// skillBodyLoader is the slice of LazySkillLoader the gesture consumes (the
// loaded-body lookup decides — not an unparsed token). *skills.LazySkillLoader
// satisfies it structurally.
type skillBodyLoader interface {
	Load(ctx context.Context, name string) (*skills.Skill, error)
}

// SkillInvocationBlock is one resolved gesture: the skill plus the rendered
// user-role instruction block ready for context injection.
type SkillInvocationBlock struct {
	// Name is the invoked skill's canonical name.
	Name string
	// Body is the full rendered instruction block ("user asked for this
	// skill: <name>" + the skill body).
	Body string
}

// invokedSkillNames scans userText for /name gesture tokens. Returns unique
// names in first-seen order; kebab-case only. Never matches filesystem
// paths (/usr/bin), fractions (5/8), or slash-delimited words (foo/bar) —
// the delimiter rules in skillGestureRe exclude them.
func invokedSkillNames(userText string) []string {
	if userText == "" {
		return nil
	}
	matches := skillGestureRe.FindAllStringSubmatchIndex(userText, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	names := make([]string, 0, len(matches))
	for _, loc := range matches {
		if !gestureTrailingOK(userText, loc) {
			continue
		}
		name := userText[loc[4]:loc[5]]
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// resolveSkillInvocationBlocks resolves each gesture name through the
// LOADED-body lookup. A name with no loadable skill (unknown) or whose
// loaded definition sets DisableModelInvocation produces NO block and NO
// error — the token remains plain prose in the user's message. Known,
// user-invocable skills produce one SkillInvocationBlock each, in input
// order.
func resolveSkillInvocationBlocks(ctx context.Context, loader skillBodyLoader, names []string) []SkillInvocationBlock {
	if loader == nil || len(names) == 0 {
		return nil
	}
	blocks := make([]SkillInvocationBlock, 0, len(names))
	for _, name := range names {
		skill, err := loader.Load(ctx, name)
		if err != nil || skill == nil {
			// Unknown name: stays prose (dsh 1d).
			continue
		}
		if skill.DisableModelInvocation {
			// Model-invocation-disabled: the gesture is provider-disabled
			// for this skill (dsh: "provider disables the gesture for
			// non-user-invocable skills"). Stays prose.
			continue
		}
		blocks = append(blocks, SkillInvocationBlock{
			Name: skill.Name,
			Body: renderSkillInvocationBlock(skill),
		})
	}
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

// renderSkillInvocationBlock renders one skill as an explicit user-role
// instruction block. The framing sentence makes the provenance explicit
// (the USER asked for this skill — not a fuzzy match), and the full body
// follows so the model can execute it.
func renderSkillInvocationBlock(skill *skills.Skill) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<direct-skill-invocation>\n")
	fmt.Fprintf(&sb, "[System note: the user explicitly invoked the skill %q with its /name gesture. Follow these instructions for this turn.]\n\n", skill.Name)
	fmt.Fprintf(&sb, "user asked for this skill: %s\n\n", skill.Name)
	if skill.Description != "" {
		fmt.Fprintf(&sb, "%s\n\n", skill.Description)
	}
	if skill.Body != "" {
		sb.WriteString(skill.Body)
		sb.WriteString("\n")
	}
	sb.WriteString("</direct-skill-invocation>")
	return sb.String()
}

// injectInvokedSkills is the turn-seam binding: scan the claimed user text
// for /name gestures, resolve each through the loaded-body lookup (the
// LazySkillLoader the loop already owns), and append the rendered bodies as
// user-role messages to the conversation — closest to the model's answer
// material. No-op without a loader or when no gesture resolves. Errors are
// logged at debug and never fail the turn: the gesture is an affordance,
// not a contract.
func (l *AgentLoop) injectInvokedSkills(ctx context.Context, conv *Conversation, userText string) {
	names := invokedSkillNames(userText)
	if len(names) == 0 {
		return
	}
	l.mu.RLock()
	loader := l.skillLoader
	l.mu.RUnlock()
	if loader == nil {
		return
	}
	blocks := resolveSkillInvocationBlocks(ctx, loader, names)
	if len(blocks) == 0 {
		l.logger.Debug("skill gesture: no resolvable invocations",
			"names", names)
		return
	}
	resolved := make([]string, 0, len(blocks))
	for _, b := range blocks {
		conv.AddUserMessage(b.Body)
		resolved = append(resolved, b.Name)
	}
	l.logger.Info("Skill gesture resolved",
		"requested", names,
		"invoked", resolved,
	)
}
