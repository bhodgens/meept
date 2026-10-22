# Subagent performance detail -- bughunt 2026-10-15

Sources: state.db `sessions` (source='subagent') + `session_model_usage`;
delegation manifests + live transcripts under ~/.hermes/profiles/socrates/cache/delegation/live/.

| child session | scope | wall_s | api_calls | in_tok | out_tok | rsn_tok | out tok/s | out+rsn tok/s | in tok/s (prefill) |
|---|---|---|---|---|---|---|---|---|---|
| 20261015_143153_1cd8ee | smoke-test | 80 | 4 | 62642 | 467 | 319 | 5.85 | 9.84 | 784.6 |
| 20261015_143930_13beb7 | agent-core | 20412 | 15 | 237569 | 5629 | 763 | 0.28 | 0.31 | 11.6 |
| 20261015_143930_f7e307 | llm-layer | 23963 | 35 | 495643 | 8879 | 1696 | 0.37 | 0.44 | 20.7 |
| 20261015_143930_fe6d48 | memory-session | 23980 | 28 | 687183 | 13557 | 2120 | 0.57 | 0.65 | 28.7 |
| 20261015_143930_49168c | daemon-comm | 22597 | 21 | 490118 | 12242 | 1522 | 0.54 | 0.61 | 21.7 |
| 20261015_144302_e45d0e | tools | 23741 | 48 | 240847 | 9654 | 1740 | 0.41 | 0.48 | 10.1 |
| 20261015_144302_3603b0 | employee-plan-queue | 23925 | 41 | 714534 | 9252 | 1293 | 0.39 | 0.44 | 29.9 |
| 20261015_144302_62fbc7 | tui-services | 23781 | 35 | 476614 | 13346 | 1910 | 0.56 | 0.64 | 20.0 |
| 20261015_144302_cb25aa | security-cli | 23548 | 39 | 900787 | 11189 | 927 | 0.48 | 0.51 | 38.3 |
| **total** |  | 186027 |  | 4305937 | 84215 | 12290 | 0.45 | 0.52 | 23.1 |

Notes:
- wall_s = last_activity_at - started_at (child lifetime, includes queue wait for the shared GPU).
- in_tok is cumulative prompt tokens across all API calls (re-prefilled every call); it dominates cost.
- processed tok/s = (in+out+rsn)/wall; output tok/s is what a reader sees.
- 4 children + parent shared one local GPU (socrates litellm, minicpm-v-4.7); the smoke test ran alone at 5.85 out tok/s.