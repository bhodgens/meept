package conflict

import (
	"fmt"
	"strings"
)

// The conflict prompt presents a numbered batch of candidates, each with its
// recalled targets, and demands a strict JSON array — one entry per
// candidate, in order, using exactly the four defined actions.
const systemPrompt = `You are a memory-conflict resolver for a personal knowledge store.
You will receive numbered NEW candidates and, for each, already-stored memories that may conflict.

Decide one action per candidate:
- "merge": the candidate AND a stored memory are both true aspects of one fact
  (e.g. "deploys run at noon" + "deploys run only on weekdays"). Merge them into
  a single statement that preserves every true aspect. Set target_ids to the
  memories being merged and write the combined statement in merged_content.
- "supersede": the candidate replaces a stored memory that is now STALE
  (outdated, contradicted, or no longer true). Set target_ids to the memories
  being replaced; leave merged_content empty.
- "skip": the candidate is an EXACT duplicate of a stored memory (no new
  information). Set target_ids to the duplicated memory; leave merged_content empty.
- "store": no conflict — the candidate is new information. Leave target_ids
  and merged_content empty.

Rules:
- When in doubt, choose "store". Never discard information that is not an exact duplicate.
- target_ids may only contain memory IDs that appear in that candidate's recalled targets.
- Answer with a STRICT JSON array and nothing else: one object per candidate,
  in candidate order, exactly
  [{"candidate": <number>, "action": "store"|"merge"|"supersede"|"skip", "target_ids": [<ids>], "merged_content": "<text>", "reason": "<short reason>"}]
- No markdown, no commentary, no trailing text.`

// buildPrompt renders the full LLM prompt for one batch of candidates with
// their recalled targets. batchOffset is the global index of the batch's
// first candidate, so `candidate` fields in the answer refer to positions in
// the whole DetectBatch input.
func buildPrompt(cands []Candidate, recalls map[int][]RecallTarget, batchOffset int) string {
	var b strings.Builder
	b.WriteString(systemPrompt)
	b.WriteString("\n\n## CANDIDATES\n")
	for i, c := range cands {
		fmt.Fprintf(&b, "\n### candidate %d\ncontent: %s\n", batchOffset+i, c.Content)
		targets := recalls[i]
		if len(targets) == 0 {
			b.WriteString("recalled targets: (none)\n")
			continue
		}
		b.WriteString("recalled targets:\n")
		for _, t := range targets {
			fmt.Fprintf(&b, "- id: %s | similarity: %.3f | content: %s\n", t.MemoryID, t.Score, t.Content)
		}
	}
	b.WriteString("\nRespond with the strict JSON array now.")
	return b.String()
}
