# github-pr-ci e2e suite

Purpose: hermetic PR+CI loop — the real scratch daemon drives a fake GitHub HTTP
server (httptest); no live github.com. Scenarios: (1) create PR + wait for CI
green (exactly one POST /pulls, all_green=true, check-runs polled past the
initial in_progress); (2) [github] off by default, zero github_* tools exposed
to the executor; (3) effects-ledger reconcile — a pre-seeded CLAIMED row whose
key is built from the EXACT parts the scripted call produces
(EffectKey(github_pr_create, o, r, feat/x, "feat/x: add PR")) collides with the
claim's PK-conflict path so the create is blocked: the fake POST counter stays
at 0 and the prior-owned error surfaces. The [github].base_url override to the
fake server is what makes the loop hermetic. Run via
`make e2e-fast-area AREA=github-pr-ci`.
