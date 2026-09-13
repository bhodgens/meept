# Wave-5 Confusion Analysis — where the 13-intent centroid leaks

Corpus: 389 classified cases, composition computed from the committed
loader `H.load_cases()` (not hand-typed): base 139 + adversarial 250 =
389; non-OOD 361 / OOD 28; 0 duplicate texts. (An earlier revision of
this line said "358 gold + 31 base overlap dedup" — wrong: 358 was the
pre-wave-5 total and there is no 31-case dedup block. Corrected
2026-09-12.)
Head: centroid margin 0.030 (champion). Analysis over ALL classified
rows (not per-fold): 61 wrong routes total.

> **IN-SAMPLE CAVEAT (F36).** Every case is scored with class centroids
> that INCLUDE that case (line 24 of `wave5_confusion.py` builds each
> class mean from all its cases; line 33 scores the same vectors), so
> each case's own-class similarity is inflated and the wrong-route
> margins are optimistic. This is NOT the campaign's per-fold protocol
> (5-fold with the tracked fold cache) used for every other number, so
> the "gate is at/near its optimum" conclusion below is UNVALIDATED
> until the analysis is re-run per fold (score each case with its test
> fold's centroids).

## Confusion structure (top pairs)

| true -> predicted | count | shape |
|---|---|---|
| code -> quickplan | 11 | "implement Task N" / compound work reads as orchestrated |
| code -> debug | 8 | fix-verbs overlap ("correct them") |
| quickplan -> code | 5 | plan-execution with heavy code vocab |
| debug -> analyze | 4 | "why is X failing" reads as investigation |
| quickplan -> git | 4 | "commit, push, then..." sequences |
| quickplan -> plan | 3 | "work out how to sequence" |
| rest | 24 | scattered 1-2s |

## The key number

**55 of 61 wrong routes (90%) have margin < 0.030** — the gate ALREADY
gates them. The centroid head's wrong routes are almost entirely
inside the abstention band; they only appear in this analysis because
it inspects the argwin regardless of the gate. The actual direct-route
leak is the 6 wrong routes with margin >= 0.030:

- median wrong margin: 0.0076 (well inside the band)
- max wrong margin: 0.0782 (the only confident-leak candidates:
  ~2-3 routes)

## Implications

1. **Margin 0.030 is doing its job (IN-SAMPLE claim — see caveat
   above).** Raising it further buys almost nothing (the 6 leaks above
   it are few); lowering it imports ~55 additional wrong routes into
   direct-routing. The gate is at/near its optimum for this corpus —
   but this rests on margins computed WITH each case inside its own
   class centroid, so it is unvalidated for the no-change decision
   until re-run per fold.
2. **The residual confusion is semantic, not geometric.** code/
   quickplan/debug/analyze share vocabulary by nature ("fix", 
   "implement", "correct"). Centroid distance cannot separate intents
   whose surface text is identical and whose difference is session
   state — the already-recorded ceiling (iter-20).
3. **Corpus anchors have diminishing returns here.** The 55 gated
   near-misses never route, so anchoring them only moves the boundary
   for cases that are already abstaining. The exception: if the
   outcome loop shows chain answers to those gated cases being wrong,
   THAT is signal for new anchors (the wrong-door-to-right-door ratio,
   not the confusion matrix alone).
4. **The tfidf-veto and cue-guard are the right shape of fix** for the
   confident-leak band: they add a second signal (agreement, cue
   evidence) instead of moving the same geometric threshold.

## Action items

- None for the gate. Margin stays 0.030 (in-sample optimum claim, see
  caveat above; a per-fold re-run is the precondition for treating this
  as measured).
- Priority stays with the outcome loop: real correction data will show
  which of the 55 gated near-misses the chain gets wrong — those
  become corpus anchors with evidence, replacing guesswork.
