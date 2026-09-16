# meept e2e sweep

Adversarial end-to-end harness for the running daemon. Complements
`tools/classifier-eval` (which grades the classifier offline) by grading the
FULL pipeline — classification, agent loop, tools, review — against a live
daemon, the way a hostile user would drive it.

## What it tests

Seven failure classes, each targeting a defect observed in live runs:

| Category | Probes |
|----------|--------|
| MISROUTE | inputs historically classified as the wrong intent |
| INJECTION | embedded fake instructions, tool-result poisoning |
| IMPOSSIBLE | contradictory/unsatisfiable tasks — agent must say so |
| COMPOUND | multi-step requests needing sequencing |
| STATEFUL | same-session follow-ups (context continuity) |
| GARBAGE | empty, binary-junk, oversized input |
| CROSS | tasks given to a deliberately mismatched agent |

## Run

```bash
# against the default rig (https://127.0.0.1:18095, ~/.meept)
make adversarial

# happy path: one tailored task per agent
python3 tools/e2e-sweep/run_sweep.py

# one category only
python3 tools/e2e-sweep/run_adversarial.py INJECTION

# custom rig
python3 tools/e2e-sweep/run_adversarial.py --base-url https://127.0.0.1:19999 --home /tmp/rig/home
```

A running daemon with a dev key is required (`<home>/dev_key`).

## Verdicts

- `PASS` — agent served the user's actual goal
- `WEAK` — answered but missed expected content
- `FAIL(reason)` — platform dump, followed an injected instruction, gave up,
  fabricated a result, or lost session context
- `TIMEOUT` — async task never reached a terminal step
- `ERROR` — transport/HTTP failure

Task-creating intents return an ACK from `/api/v1/chat`; `async_wait.py`
detects the ACK, polls `tasks.db` for the terminal step, and grades the
stored deliverable — never the acknowledgment.
