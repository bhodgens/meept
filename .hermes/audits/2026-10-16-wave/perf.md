# Subagent performance detail -- bughunt 2026-10-16

Sources: state.db `sessions` (source='subagent'); delegation manifests + live
transcripts under ~/.hermes/profiles/socrates/cache/delegation/live/
(deleg_073ec3cc, deleg_018a1cdc). Prior-wave comparison:
.hermes/audits/2026-10-15-wave/perf.md.

Model: minicpm-v-4.7 via socrates litellm (shared local GPU). 8 read-only
auditors, 2 batches of 4, capped briefs (~25 tool calls, grep-first, report
written early). No smoke-test child this wave.

| child session | scope | wall_s | api_calls | in_tok | out_tok | rsn_tok | out tok/s | out+rsn tok/s | in tok/s (prefill) |
|---|---|---|---|---|---|---|---|---|---|
| 20261016_153803_8c73e8 | memory-session | 6380 | 39 | 660020 | 9915 | 2699 | 1.55 | 1.98 | 103.4 |
| 20261016_153803_4aa40e | llm-comm | 5693 | 29 | 435636 | 8397 | 2397 | 1.48 | 1.89 | 76.5 |
| 20261016_153803_f01525 | daemon-runtime | 6352 | 28 | 620228 | 6615 | 2104 | 1.04 | 1.37 | 97.6 |
| 20261016_153803_e957dc | agent-plan | 6188 | 20 | 863219 | 7263 | 2299 | 1.17 | 1.55 | 139.5 |
| 20261016_153842_cf7fd4 | security-auth | 6073 | 38 | 322996 | 8375 | 1367 | 1.38 | 1.60 | 53.2 |
| 20261016_153842_14c383 | services-ui | 6305 | 32 | 247259 | 9638 | 1960 | 1.53 | 1.84 | 39.2 |
| 20261016_153842_0d2759 | tools-skills | 3843 | 19 | 149447 | 6206 | 1125 | 1.62 | 1.91 | 38.9 |
| 20261016_153842_04af0e | infra-misc | 5462 | 22 | 395890 | 7211 | 1337 | 1.32 | 1.57 | 72.5 |
| **total** |  | 46296 | 227 | 3694695 | 63620 | 15288 | 1.37 | 1.70 | 81.5 |

Scope mapping caveat: state.db `title` is NULL for subagent sessions; scope
labels above were matched by api_call_count against the delegation batch
reports (exact or +-1). Mapping is high-confidence, not certain.

## Wave wall clock

- 2026-10-16: dispatch 15:38:03 -> last child activity 17:24:24 = **106 min**
  (invocation-to-consolidated-report ~2 h including phase-0 seeding and
  parent verification).
- 2026-10-15: dispatch 14:39:30 -> last child ~21:19 = **~6.6 h**
  (children ran 5.7-6.7 h each).
- Speedup: **~3.7x** end-to-end.

## Comparison vs 2026-10-15 wave

| metric | 2026-10-15 | 2026-10-16 | change |
|---|---|---|---|
| children | 7 audits + 1 smoke | 8 audits | same audit count |
| wave wall clock | ~23,900 s (~6.6 h) | 6,381 s (106 min) | 3.7x faster |
| mean child wall | 23,253 s (6.5 h) | 5,787 s (1.6 h) | 4.0x faster |
| aggregate out tok/s | 0.45 | 1.37 | 3.0x |
| aggregate out+rsn tok/s | 0.52 | 1.70 | 3.3x |
| aggregate processed tok/s | 23.1 | 81.5 | 3.5x |
| total input (prefill) tok | 4,305,937 | 3,694,695 | -14% |
| total output tok | 84,215 | 63,620 | -24% |
| per-child in_tok range | 237K-900K | 149K-863K | tighter tail |
| api_calls per child | 15-48 | 19-39 | capped by brief |

What produced the speedup (same model, same GPU, same parent):
1. Hard call cap in the brief (~25 calls) + "write the report at finding 3-5"
   -> children stopped accumulating context instead of drifting for hours.
2. grep-first + never-read-big-files rule kept per-call prompt growth low;
   prefill per child fell from up to 900K to 149K-863K total.
3. One shared BRIEF-RULES.md file instead of N prompt copies (less prefill,
   no drift).
4. Two batches of 4 instead of 4+4 staggered by 3.5 min -- GPU contention was
   already fine; contention is NOT the limiter, prefill volume is.

Decode-rate sanity: the 2026-10-15 uncontended smoke test decoded at 5.85
out tok/s alone. Eight children at 1.04-1.62 each = ~11 tok/s fleet decode,
consistent with a shared server under load. The 3x wave speedup is from
doing LESS prefill per call, not from faster decode.

## Reliability (finalization failures)

6 of 8 children reported status=failed while their report files landed intact:
- 2x "Response truncated due to output length limit" (final answer)
- 1x "Context compression timed out" (llm-comm, 29 calls, 435K in)
- 2x output_schema validation failure (after 1 retry)
- 1x report destroyed: memory-session child re-wrote its 5,885-byte report
  with a 253-byte schema stub when stopped mid-finalization. Report body
  lost; transcript summary survives.

Lesson (recorded in skill subagent-audit-timeboxing): for local-model waves,
the report FILE is the deliverable; the schema answer is decoration. Do not
stop a child between "report written" and "final answer emitted" -- it can
clobber the report with the stub.

## Cost shape

- 3.69M input tokens re-prefilled vs 63.6K output tokens: 58:1 in:out ratio.
  Every byte of child context costs ~58x what its findings cost.
- Cheapest child (tools-skills): 149K in, 19 calls, 64 min, 5 findings.
  Most expensive (agent-plan): 863K in, 20 calls, 103 min, 4 findings
  (2 of 4 later disproved parent-side). Call count is not the cost driver;
  context volume per call is.
