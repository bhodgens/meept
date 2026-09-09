package agent

import "regexp"

// QuickPlanCuePattern detects orchestration evidence in a user message.
//
// WHY: quickplan-vs-code/git is not decidable from message text alone
// (adjudicated campaign finding, iter-20; adjudication record
// 2026-09-09) - "do it all" phrasing is shared by both classes. The cue
// requires explicit orchestration evidence (subagents, task lists,
// waves, "as you go", etc.) before a quickplan direct route fires from
// the embedding prefilter. Deriving quickplan from session state
// happens at the orchestrator (leaf 02), not here.
//
// Consumers: prefilter vote() cue guard, LLM-chain post-check
// (leaf 02, optional).
var QuickPlanCuePattern = regexp.MustCompile(`(?i)\b(subagents?|tasks? \d|` +
	`task list|waves?|leaves?|leaf \d|plan\.md|handoff|checklist|in order|` +
	`one at a time|sealed plan|tracking table|as you (find|go)|, then\b|` +
	`and correct them|and fix them|without (asking|stopping)|no check-?ins?|` +
	`just (do|make|apply)|make it happen|to completion|finish the remaining|` +
	`carry on with the plan|execute (the|what)|implement (the|all) plan|` +
	`implement tasks?|work (through|items)|knock out|carry out|` +
	`complete the outstanding)\b`)
