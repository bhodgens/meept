# Intent Routing Reference — meept Agent Dispatcher

How your message becomes work. Every message is classified into an
**intent**; the intent picks an **agent** and a **planning mode**. This
document is the authoritative map, including what happens when nothing
matches.

## The routing pipeline

The pipeline is three doors, cheapest first. Each message falls to the
next door only when the current one declines; Door 3 always responds.
Full component detail, resource costs, and the observability story:
`docs/workflows/classification-architecture.md`.

```
your message
  │
  ├─ short/simple guard ──────► chat (greetings, "2", single words)
  │
  ├─ DOOR 1: embedding gate ──► direct route on confident match
  │  (qwen3 embed + 13-centroid store + margin logic; ~0.1s, free)
  │
  ├─ DOOR 2: LLM chain ───────► intent + agent + planning mode
  │  (LFM2.5-8B + intent analyzer; ~1-2s; ~87% lab / ~84% live;
  │   ~13% misrouted but recovered downstream, never lost)
  │
  ├─ heuristic fallback ──────► intent from keyword tables
  │
  └─ DOOR 3: quickplan ───────► clarify (if ambiguous) → plan → execute
```

**Nothing is ever dropped.** Every path terminates in an agent that
responds to you. The final fall-through is the quickplan agent: if the
classifier cannot determine intent, quickplan clarifies what you need
(up-front questions only), plans the work, and executes it
autonomously — no approval pauses after that point.

### The emittable lane list has one source of truth

The LLM classifier can only emit lanes it is told about. That list lives in
ONE place — `classifierLanes` in `internal/agent/llm_classifier.go` — and it
drives the classifier system prompt, the per-lane description list in the
user prompt, and the `isValidIntent` gate. The list used to be duplicated as
a literal in three of those places, and they drifted. `quickplan` had a
dispatcher route, an agent mapping and 58 gold evaluation cases while being
absent from every list, so the LLM classifier could never emit it;
`research` was missing from the `isValidIntent` gate, which made the
research lane unreachable from the classifier entirely. Adding a lane to
`classifierLanes` is now the whole change on the **emit** side.

The lane-to-**agent** destination is no longer a Go table; it is derived from
the agent definitions. Each `config/agents/*/AGENT.md` declares the lanes it
owns in an `intents:` frontmatter list — for example `intents: [code,
review, tooluse]` on the coder, `intents: [plan]` on the planner. At load
time those lists build the lane-to-agent index
(`BuildLaneAgentIndexFromDir` → `AgentRegistry.publishLaneIndex`), which
`agentForIntent` consults first. **Adding a new specialist is a
frontmatter-only change**: create `config/agents/<id>/AGENT.md` with
`intents: [<lane>]` and the lane routes to it with no Go edit.

`agentForIntent` fallback order (first match wins):

1. the frontmatter `intents:` index — any agent that declares the lane;
2. the static `agentMapping` table in `internal/agent/llm_classifier.go`;
3. the lane's own `IntentType.DefaultAgent`;
4. `chat`, the final safety net.

Steps 2-4 keep every lane routable when no agent declares it (for example
`quickplan`, whose destination is the orchestrator and which no AGENT.md
declares), so the dynamic path can be adopted incrementally without
regressing existing routing.

Guards, in `internal/agent/classifier_lanes_test.go` — the build fails when
the lists drift again: every lane used by the gold corpora is emittable,
every corpus `expected_agent` equals `agentForIntent`, every advertised lane
has a description and a route, a brand-new AGENT.md in a temp directory
becomes routable purely from its `intents:` frontmatter, an undeclared lane
still falls back to the static tables, and every lane the shipped
`config/agents` declares matches the route the static tables produced before
`intents:` existed.

## Intent decision table

Answer one question: **"what do I want to exist when this is done?"**

| I want... | intent | agent | example |
|---|---|---|---|
| changed/created code or a repo artifact | `code` | coder | "add pagination to the API" |
| one **named** defect fixed | `debug` | debugger | "fix the nil pointer in handler.go" |
| a verdict on quality — findings **only, no changes** | `review` | coder* | "is this migration script correct?" |
| understanding of something ambiguous (compare, tradeoffs, why) | `analyze` | analyst | "compare the top 3 vector DBs" |
| external information located | `search` | analyst | "find the RFC for HTTP signatures" |
| an approach/architecture designed **before** building (I approve it) | `plan` | planner | "design the notification service" |
| a sequence of work **planned and executed immediately** — clarify up front if needed, no approval pauses | `quickplan` | orchestrator | "review the daemon for bugs and correct them as you find them" |
| commits/PRs/branches/merges | `git` | committer | "rebase on main" |
| reminders, timers, calendar, recurring tasks | `schedule` | scheduler | "remind me at 3pm" |
| a summary of what happened | `report` | chat | "summarize what was done today" |
| something from a **past** conversation | `recall` | chat | "what did we decide about the queue library?" |
| conversation itself (greetings, thanks, social) | `chat` | chat | "good morning" |
| questions about meept itself (capabilities, tools, setup) | `platform` | chat | "how do I enable the prefilter?" |

\* the review intent routes to the coder, who reads and reports
findings without applying changes; dedicated read-only reviewer agents
(code-reviewer, debug-reviewer, analyst-reviewer) audit executor work
as verification gates.

## QuickPlan

Quickplan is a work order, not a question: the orchestrator **plans the
work, asks you up-front clarification questions only if the request is
ambiguous, then executes autonomously to completion.** After the
clarification step there are no approval pauses — you get the finished
result.

**The cue rule.** A quickplan classification requires **orchestration
evidence** in the message: subagents, task lists, waves/leaves,
`plan.md` references, correction clauses ("...and correct them as you
find them"), or multi-step sequences ("in order", "one at a time",
"then"). Without that evidence, the message falls to the LLM
classification chain. Rationale: quickplan-vs-code is a **session-state
judgment** the orchestrator makes at execution time using active-plan
and task context — not something message text alone can decide.

Worked examples:

- "review the daemon for bugs and correct them as you find them" →
  **quickplan** (correction clause: autonomous multi-site execution)
- "review the json files for completeness" → **review** (verdict only,
  findings come back to you, nothing modified)
- "add pagination to the API" → **code** (one concrete artifact to
  change)

**"Help me…" phrasing.** Messages that open with "help me" ("help me
organize this data", "help me understand why X fails") signal compound
or complex work: the request implies investigation plus action, not a
single known operation. Treat "help me" as a quickplan signal — the
orchestrator chunks the work and executes. If the help request is
purely informational ("help me understand X" with no artifact), it
routes to analyze instead.

## The three-way boundary that matters most

**"Look at X"** resolves by what happens after the look:

| after the look | intent |
|---|---|
| agents fix autonomously, many unknown sites, orchestration | `quickplan` |
| findings come back to **you**; guaranteed no changes | `review` |
| understanding of something not necessarily broken | `analyze` |
| one **named** defect, fix follows directly | `debug` |

Test cases:

- "review the daemon for bugs, and correct them as you find them" →
  **quickplan** (correction clause = autonomous execution)
- "review the json files to make sure they're complete" → **review**
  (verdict only, nothing modified)
- "investigate how #1 and #2 can be solved" → **analyze**
  (understanding of a contention, not a quality verdict)
- "why is this test failing?" → **debug** (one named defect)

## Special intents (system-level)

| intent | agent | notes |
|---|---|---|
| `compound` | orchestrator | multiple intents in one message; each sub-intent dispatched separately |
| `clarify` | chat | daemon asked you a question; your reply is processed against it |
| `instruction` | chat | you defined a standing rule ("always...", "every day at...") → instruction parser, not a task |
| `explore` | explore | read-only codebase search |
| `research` | researcher | deep multi-source research |
| `write` | writer | prose/documentation creation |
| `architect` | architect | system design at spec depth |
| `skeptic` | skeptic | adversarial challenge of a claim or design |
| `librarian` | librarian | memory/tag curation |
| `security` | chat | security policy questions |
| `tooluse` | coder | direct tool invocation |
| `skill` | skill | named skill invocation |
| `pair` / `collaborate` | analyst | dual-agent sessions |
| `image_gen` / `video_gen` / `image_id` | respective media agents | media creation/ID |

## Fall-through guarantee

1. **Short/simple guard**: messages under a length/simple-content
   threshold skip classification entirely → chat.
2. **Stage-0 gate**: confident unanimous matches route instantly,
   skipping the LLM chain (latency win; falls through on any doubt).
3. **LLM chain**: primary classification. Inside the chain, three
   arbitration gates discard verdicts that contradict the input's own
   structure: a platform/schedule/git verdict on a work-status recall
   question becomes recall; a platform/schedule verdict on an
   imperative becomes a fall-through; and (since the 2026-09-15 bench
   gate, issue #46) a **git verdict on a git-verb-free imperative
   falls through** — the 350M prompt-router routes "create a file in
   the repository root" to git because of the word "repository", and a
   git verdict without commit/push/merge/branch/rebase/checkout/stash
   in the input is discarded (`git_verb_agreement_veto`; disable with
   dispatcher config `git_verb_agreement_veto=false` to measure).
4. **Heuristic fallback**: keyword tables when the chain fails.
5. **Final fallback**: **quickplan** — clarifies ambiguity with you
   first (only if needed), then plans and executes to completion.
6. **Agent-resolution safety**: if a classified agent doesn't exist in
   the registry, the chat agent answers instead.

Compound messages (two intents, one message) are split by the
orchestrator into separate dispatched tasks; genuinely entangled
multi-intent work is planned and executed under the compound flow.
