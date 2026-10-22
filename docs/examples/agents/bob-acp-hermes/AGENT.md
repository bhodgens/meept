---
id: bob
name: bob
role: executor
description: ACP-backed executor persona — drives the hermes catalog agent under the borg profile
executor: acp
acp:
  agent: hermes
  persona: bob
  command_args: ["-p", "borg"]
  permission_mode: human
---

# bob (acp / hermes persona)

bob is an example agent definition that swaps the built-in LLM loop for an
ACP driver: instead of reasoning with meept's own model, bob's turns are
prompts to an external hermes agent subprocess, keyed as persona `bob` on
the shared `hermes` catalog entry. "have bob check that issue" routes to him
by name through the dispatcher like any other roster agent. The
`command_args` tail (`-p borg`) selects the hermes profile the subprocess
runs under; `permission_mode: human` puts every inbound
`session/requestPermission` in front of a person via the relay, overriding
the global `[acp] permission_mode`.

This is the design contract from `docs/workflows/acp-employees.md` (the
persona lane lands in code waves — see `docs/workflows/acp.md`, section
"personas and agent-definition executors").

## Prerequisites

Before bob can run:

1. The hermes profile `borg` must exist — check with `hermes profile list`.
   `command_args: ["-p", "borg"]` fails at spawn otherwise.
2. The catalog entry `hermes` must be enabled in `~/.meept/acp_agents.json5`
   (`enabled: true` on the entry).
3. ACP must be on globally: `[acp] enabled = true` in `~/.meept/meept.json5`.
   This is the kill switch — off means no subprocess starts for any persona.

## Trust note

`command_args` is operator-authored config, NOT model-writable — same trust
class as `command` in `acp_agents.json5`. Agent definitions are operator-only
files; a model must never edit this frontmatter to change what binary runs or
which flags it gets.

## Using this example

Copy this directory's `AGENT.md` into your roster
(`config/agents/bob/AGENT.md` in-repo, or the user agents dir), adjust the
persona/profile names, and restart the daemon. Verify with
`./bin/meept agents show bob` (renders the executor section: acp, catalog
agent, persona, permission mode).
