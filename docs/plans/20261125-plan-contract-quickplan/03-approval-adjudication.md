# Leaf 03 — approval_requested adjudication (kills the regex)

Contract: master.md C-3. Decision D3 (A+B).

## Deliverables

1. internal/agent/intent_analyzer.go: TrueIntentAnalysis gains
   ApprovalRequested bool (`json:"approval_requested,omitempty"`);
   analyzer prompt instructs: true ONLY when the user explicitly asks
   to review/approve the plan before execution (list the frozen probe
   sentences as prompt examples); default absent = false.
2. B-fallback adjudication: when the (deleted) regex's suspected-veto
   surface would have fired — keep a NARROW cheap trigger pattern
   "veto-suspect" boolean (not a verdict) — issue ONE bounded
   small-model adjudication (classifier chatter slot, ctx deadline,
   failure => keep false) to resolve approval_requested. Adjudication
   NEVER parks or fails a turn.
3. Wire consumption at leaf 01's call site (coordinate the dispatcher.go
   overlap: leaf 01 lands the branch reading an intent field; this leaf
   produces it).
4. Delete autoSealNegatedExecute + PlanAutoSealFromIntent (strategic.go)
   and any remaining callers; the 6 audit probe sentences become table
   pins for the NEW mechanism (expected: only "implement only after I
   approve"-class sentences => ApprovalRequested true; "write a plan
   and create a markdown file" => false).

## Files owned

internal/agent/intent_analyzer.go, internal/agent/approval_
adjudication_test.go (new), internal/agent/strategic.go (regex +
function deletion), dispatcher.go (field production only — leaf 01 owns
the call site; sequence AFTER 01 or coordinate in-tree).

## Pins

- analyzer JSON with approval_requested:true/false/absent -> field
  lands / legacy parse byte-identical.
- veto-suspect trigger -> adjudication called once; adjudication error
  -> false, no turn degradation, one Debug line.
- probe-sentence table: 6 sentences from the bughunt report M6 entry
  with expected verdicts.
- old regex functions have zero callers (grep gate) then deleted.

## Gates

full agent suite; gofmt. ONE commit naming M6/D3.
