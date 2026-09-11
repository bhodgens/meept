# Wave-5 Confusion Analysis — where the 13-intent centroid leaks

Corpus: 389 classified cases (358 gold + 31 base overlap dedup).
Head: centroid margin 0.030 (champion). Analysis over ALL classified
rows (not per-fold): 61 wrong routes total.

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

1. **Margin 0.030 is doing its job.** Raising it further buys almost
   nothing (the 6 leaks above it are few); lowering it imports ~55
   additional wrong routes into direct-routing. The gate is at/near
   its optimum for this corpus.
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

- None for the gate. Margin stays 0.030.
- Priority stays with the outcome loop: real correction data will show
  which of the 55 gated near-misses the chain gets wrong — those
  become corpus anchors with evidence, replacing guesswork.
