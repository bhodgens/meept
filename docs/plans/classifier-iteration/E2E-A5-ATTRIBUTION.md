# E2E A5 Residual — Attribution Note

Date: 2026-09-11. Wave: e2e naive-user fix loop (runs 1-14), 38 commits
since 5dd477cd. Authoritative final run: `/tmp/e2e-final3-20260911.log`
(workdir meept-e2e.t3OXfx, --keep): **15 pass / 1 fail / 1 skip**, sole
failure A5.

## The contract

A5 (session continuity): after T1 creates hello.txt, T3 "did the change
get made? where is the file?" must reference the artifact. This is the
chat-dispatch-ux leaf A5 acceptance.

## What is fixed (code — all landed with pins)

Every code-reachable defect on the T3 path is fixed:

- T3 routing: landed on a different wrong intent nearly every run
  (platform → roster dump, schedule, git → contextless committer, chat).
  Three arbitration rules now cover the platform/schedule/git costumes
  (`c6e6f336`, `16f1f8a2`, `38fcb08f`) — T3 now routes to **recall**
  (inline chat) with `platform_recall_arbitration` provenance.
- Session digest reaches executing agents (`6eea450a`), plan requests
  (`69bebb03`), and direct-mode steps (`be7000c9`).
- Reply quality: single choke-point catalog guard (`60052632`).
- A4 (the other half of continuity) is fully green in the final run:
  file on disk, reply names the full path, validation gate passes
  (`d80b1295` heuristic-approval validation + `58a2ff42` gate wiring).

## The residual

In the final run, T3 routed correctly to recall and the inline chat
loop held the session digest (daemon log: `session_digest_used=true`)
with T1's completed task in the store — and the model still answered
"ok", not citing the artifact. The reply is honest, correctly routed,
and correctly guarded; the model simply chooses not to use the context
available to it.

## Attribution

This is answer-quality of the local stack (LFM2.5-8B-A1B-MLX-4bit
general; lfm2.5-1.2b-combined-sft classifier), not daemon code:

- The same infrastructure passes A5 whenever the classifier lands T3 on
  any context-bearing route and the model engages with the digest
  (observed in runs 7 and 9 via different routes).
- Run-to-run verdict variance on identical input is a documented
  property of this stack (classifier A/B notes, May benchmark).
- Per platform policy, the classifier/model is not swapped or tuned
  without explicit A/B evidence and owner sign-off.

## Pass criteria for the future fix

A5 passes when the recall-routed reply cites the artifact (file name or
path). Two candidate levers, both requiring the owner's A/B decision:
(a) prompt-side — make the digest's artifact line more salient in the
recall-path system prompt; (b) model-side — a general-model slot with
stronger context-following for inline recall turns.
