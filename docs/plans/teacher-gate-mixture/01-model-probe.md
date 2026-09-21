# Leaf 01 — Model Probe: CLI smoke + exact model-id resolution

DISPATCH INSTRUCTION: Any agent may implement this leaf. Do NOT commit.
Do NOT run `git add`. Write files, run verification commands, and report
results. The orchestrator handles all git operations.

**Parent:** `docs/plans/teacher-gate-mixture/master.md`
**Scope:** Prove the three model routes work end-to-end from this machine,
resolve the EXACT model ids, and measure rough per-call latency. No corpus
work. No repo file changes except the probe report (C6 dir).
**Dependencies:** none.
**Estimated context:** ~25K.

## Why this leaf exists

The sweep leaf (02) burns 144+ model calls. A wrong model id or broken
transport discovered mid-sweep wastes that. This leaf is the 15-minute
version of the same path: 3 calls, verified responses, ids pinned in a
tracked report.

## Environment facts (verified 2026-09-21 by the orchestrator — trust these)

- Working binary: `/Applications/OpenCode.app/Contents/MacOS/opencode-cli`
  (version 1.1.51). The brew wrapper `/opt/homebrew/bin/opencode` is BROKEN
  (node dyld: `libllhttp.9.3.dylib` not loaded). NEVER call the bare
  `opencode` command.
- Auth is already configured in `~/.local/share/opencode/auth.json`:
  providers `opencode` (OpenCode Zen), `zai-coding-plan`, `zai`,
  `openrouter`, `google` (oauth). Do NOT read, print, or copy that file.
- Candidate model ids (from the models.dev registry):
  - Worker A: `opencode-go/deepseek-v4-flash`
  - Worker B: `zai-coding-plan/glm-5.3-flash`
  - Judge: `opencode-go/deepseek-v4.1-flash`
- Hermes env key `$ZAI_API_KEY` is verified live against
  `https://api.z.ai/api/coding/paas/v4` (model list contains
  `glm-5.3-flash`). `$OPENCODE_API_KEY` exists in the env. Use these ONLY
  as HTTP fallback; never write them to disk or echo them.

## Tasks

### Task 1 — CLI smoke, one call per model

For each of the three ids, run:

```
/Applications/OpenCode.app/Contents/MacOS/opencode-cli run \
  'Classify this message into exactly one lane from this list: code, debug,
analyze, search, chat, platform, git, scheduling, planning, review,
reporting, recall, quickplan. Message: "figure out why search is not
working for hermes and fix it". Reply with ONLY a JSON object:
{"intent": "...", "confidence": 0.0-1.0, "reason": "<=15 words"}' \
  --model <PROVIDER>/<MODEL> 2>&1
```

with a 120s timeout. Record: exit code, wall time, and the verbatim stdout
(the JSON line). Expected: the correct lane is `debug`.

CLI invocation details: run from any directory (this task touches no repo
state). If the CLI prints extra prose around the JSON, that is fine — the
sweep's parser (leaf 02) extracts the JSON object with a brace-scan, so
record whatever shape comes back.

### Task 2 — Fallback transport check (only if a CLI call fails)

If any model fails through the CLI (non-zero exit, auth error, model-not-
found), try the direct HTTP route for that model's provider:

- Z.AI: `POST https://api.z.ai/api/coding/paas/v4/chat/completions` with
  `Authorization: Bearer $ZAI_API_KEY`, `model: glm-5.3-flash`.
- OpenCode Go: the CLI is the only documented transport for `opencode-go/*`
  ids — if the CLI fails for these, STOP that route and record it as
  UNAVAILABLE with the error text. Do not substitute other providers.

Record the fallback result per failed route in the same report.

### Task 3 — Write the probe report

Write `tools/classifier-eval/results/teacher-mix/PROBE.md` (create dirs)
with, per route: model id, transport used, exit code, latency seconds,
response (the JSON line or error text, truncated to 400 chars), verdict
OK / FALLBACK / UNAVAILABLE. Finish with a table:

```
| route | model id | transport | verdict |
|---|---|---|---|
| worker-a | ... | cli | OK |
| worker-b | ... | cli | OK |
| judge | ... | cli | OK |
```

NO credentials, NO auth.json content, NO env var VALUES in this file.

## Interface Contract (what this leaf exposes)

- `PROBE.md` with the route table above. Leaf 02 reads ONLY this table to
  bind its model ids and transports. The three route rows are the contract;
  their `model id` and `transport` column values are consumed verbatim.
- If any route is UNAVAILABLE: the table still exists; leaf 02 must abort
  on reading it, and the orchestrator escalates to the user.

## Self-Verification Checklist

- [ ] Three CLI calls executed; latencies and exit codes captured
- [ ] `debug` lane returned (any phrasing) by at least the two workers
- [ ] PROBE.md written with the exact 3-row route table shape
- [ ] `grep -c 'api_key\|sk-\|Bearer ' tools/classifier-eval/results/teacher-mix/PROBE.md`
      returns 0
- [ ] No files outside `tools/classifier-eval/results/teacher-mix/` touched

## Review Checklist (orchestrator)

- [ ] Route table present and parses per the contract
- [ ] Latencies recorded are plausible (< 60s each; flag > 30s)
- [ ] No secrets in PROBE.md
- [ ] UNAVAILABLE routes (if any) escalated, not silently substituted

Suggested commit (orchestrator, after review):
`git add tools/classifier-eval/results/teacher-mix/PROBE.md &&
git commit -m "docs(classifier-eval): teacher-mix model probe (leaf 01)"`
