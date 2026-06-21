# Agent Roster Extension Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four new agents (writer, architect, skeptic, librarian) to meept's roster, add their intent types, update the dispatcher routing table, tighten existing personas, bundle nine skill files (six adapted from external sources + three librarian-specific), and update documentation.

**Architecture:** AGENT.md is the single source of truth (per 2026-06-20 consolidation). New agents live in `config/agents/<id>/AGENT.md`. Intent types are additive constants in `internal/agent/intent.go`. Skills are SKILL.md files in `config/skills/<name>/SKILL.md`. Plan 1 (epistemic memory platform) is a prerequisite for librarian and skeptic — they use the epistemic edges and typed memories.

**Tech Stack:** Go 1.24, AGENT.md (YAML frontmatter + markdown), SKILL.md (YAML frontmatter + procedural body), intent classification, agent registry.

**Source spec:** `docs/superpowers/specs/2026-06-21-epistemic-memory-and-agent-roster-design.md` (Plan 2 sections).

**Depends on:** Plan 1 (`docs/superpowers/plans/2026-06-21-epistemic-memory-platform.md`) — librarian and skeptic reference Plan 1's epistemic tools and edges.

---

## File Structure Mapping

### Files to Create

| File | Responsibility |
|------|----------------|
| `config/agents/writer/AGENT.md` | Writer agent definition (long-form writing) |
| `config/agents/architect/AGENT.md` | Architect agent definition (system design) |
| `config/agents/skeptic/AGENT.md` | Skeptic agent definition (adversarial reasoning) |
| `config/agents/librarian/AGENT.md` | Librarian agent definition (memory steward) |
| `config/skills/litreview/SKILL.md` | Literature review methodology |
| `config/skills/dossier/SKILL.md` | Long-running profile accumulation |
| `config/skills/pulse/SKILL.md` | Topic monitoring scheduler template |
| `config/skills/grill-me/SKILL.md` | Adversarial questioning methodology |
| `config/skills/code-tour/SKILL.md` | Codebase walkthrough procedure |
| `config/skills/competitive-teardown/SKILL.md` | Multi-dimension competitive analysis |
| `config/skills/librarian-backlog-mining/SKILL.md` | Backlog mining procedure (Path C) |
| `config/skills/librarian-reflection-surfacing/SKILL.md` | Reflection surfacing procedure (Path D) |
| `config/skills/librarian-tag-hygiene/SKILL.md` | Tag normalization procedure |

### Files to Modify

| File | Change |
|------|--------|
| `internal/agent/intent.go` | Add `IntentWrite`, `IntentArchitect`, `IntentSkeptic`, `IntentLibrarian` constants + routing methods |
| `internal/agent/tactical.go:1071-1084` | Route new intents to new agents |
| `internal/config/schema.go:14-24` | Add `AgentIDWriter`, `AgentIDArchitect`, `AgentIDSkeptic`, `AgentIDLibrarian` constants |
| `config/agents/dispatcher/AGENT.md` | Update routing table with four new agents |
| `config/agents/chat/AGENT.md` | Update delegation routing table |
| `config/agents/analyst/AGENT.md` | Sharpen synthesis/gather boundary; add `competitive-teardown` skill |
| `config/agents/researcher/AGENT.md` | Add `litreview`, `dossier`, `code-tour` to `available_skills` |
| `docs/concepts/multi-agent.md` | Document new agents, intents, skills |
| `docs/workflows/agent-orchestration.md` | Update routing examples |
| `CLAUDE.md` | Update agent roster table |

---

## Task 1: Add New Agent ID Constants and Intent Types

**Files:**
- Modify: `internal/config/schema.go:14-24`
- Modify: `internal/agent/intent.go`
- Test: `internal/agent/intent_test.go` (extend or create)

### Steps

- [ ] **Step 1: Write failing test for new intent types**

Create or extend `internal/agent/intent_test.go`:

```go
package agent

import "testing"

func TestNewIntentTypes(t *testing.T) {
	cases := []struct {
		got, want IntentType
	}{
		{IntentWrite, IntentType("write")},
		{IntentArchitect, IntentType("architect")},
		{IntentSkeptic, IntentType("skeptic")},
		{IntentLibrarian, IntentType("librarian")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestNewIntentCategories(t *testing.T) {
	// Write, architect, skeptic, librarian all defer to executors.
	for _, intent := range []IntentType{IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian} {
		if intent.Category() != CategoryDefer {
			t.Errorf("intent %q: got category %q, want %q", intent, intent.Category(), CategoryDefer)
		}
	}
}

func TestNewIntentDefaultAgents(t *testing.T) {
	cases := []struct {
		intent IntentType
		agent  string
	}{
		{IntentWrite, "writer"},
		{IntentArchitect, "architect"},
		{IntentSkeptic, "skeptic"},
		{IntentLibrarian, "librarian"},
	}
	for _, c := range cases {
		if got := c.intent.DefaultAgent(); got != c.agent {
			t.Errorf("intent %q: DefaultAgent got %q, want %q", c.intent, got, c.agent)
		}
	}
}

func TestNewIntentKeywords(t *testing.T) {
	if len(IntentWrite.Keywords()) == 0 {
		t.Error("IntentWrite should have keywords")
	}
	if len(IntentArchitect.Keywords()) == 0 {
		t.Error("IntentArchitect should have keywords")
	}
	if len(IntentSkeptic.Keywords()) == 0 {
		t.Error("IntentSkeptic should have keywords")
	}
	if len(IntentLibrarian.Keywords()) == 0 {
		t.Error("IntentLibrarian should have keywords")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "TestNewIntent" -v`
Expected: FAIL with "undefined: IntentWrite".

- [ ] **Step 3: Add agent ID constants to schema.go**

In `internal/config/schema.go`, extend the const block (after `AgentIDResearcher`):

```go
	AgentIDWriter    = "writer"
	AgentIDArchitect = "architect"
	AgentIDSkeptic   = "skeptic"
	AgentIDLibrarian = "librarian"
```

- [ ] **Step 4: Add intent constants to intent.go**

In `internal/agent/intent.go`, add to the const block:

```go
	// Knowledge work (async to specialist agents)
	IntentWrite     IntentType = "write"
	IntentArchitect IntentType = "architect"
	IntentSkeptic   IntentType = "skeptic"
	IntentLibrarian IntentType = "librarian"
```

- [ ] **Step 5: Update Category() to route new intents as CategoryDefer**

In `intent.go`, update `Category()`:

```go
case IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian:
    return CategoryDefer
```

Add this to the existing `case IntentCode, ...` block that returns `CategoryDefer`.

- [ ] **Step 6: Update DefaultAgent()**

In `intent.go`, add to `DefaultAgent()`:

```go
case IntentWrite:
    return config.AgentIDWriter
case IntentArchitect:
    return config.AgentIDArchitect
case IntentSkeptic:
    return config.AgentIDSkeptic
case IntentLibrarian:
    return config.AgentIDLibrarian
```

- [ ] **Step 7: Update ShouldDispatchAsync()**

In `intent.go`, add to `ShouldDispatchAsync`:

```go
case IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian:
    return true
```

- [ ] **Step 8: Update ShouldCreateTask()**

In `intent.go`, add `IntentArchitect` to the `ShouldCreateTask` true case (architecture work is multi-step). `IntentWrite`, `IntentSkeptic`, `IntentLibrarian` return false by default.

- [ ] **Step 9: Update Keywords()**

In `intent.go`, add keyword cases:

```go
case IntentWrite:
    return []string{"write essay", "draft", "long form", "long-form", "write doc", "write a brief", "blog post", "article"}
case IntentArchitect:
    return []string{"design system", "architect", "tech stack", "trade-off", "tradeoff", "should we use", "evaluate technology"}
case IntentSkeptic:
    return []string{"stress-test", "stress test", "steelman", "what's wrong with", "what is wrong with", "challenge this", "adversarial"}
case IntentLibrarian:
    return []string{"review memory", "memory review", "clean up tags", "mine backlog", "what contradictions", "what have i been thinking"}
```

- [ ] **Step 10: Update IsValidIntentType()**

In `intent.go`, add the four new types to the switch in `IsValidIntentType`.

- [ ] **Step 11: Run test to verify it passes**

Run: `go test ./internal/agent/ -run "TestNewIntent" -v`
Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/config/schema.go internal/agent/intent.go internal/agent/intent_test.go
git commit -m "feat(agent): add writer, architect, skeptic, librarian intent types"
```

---

## Task 2: Writer Agent Definition

**Files:**
- Create: `config/agents/writer/AGENT.md`

### Steps

- [ ] **Step 1: Create AGENT.md**

Create `config/agents/writer/AGENT.md`:

```yaml
---
id: writer
name: Writing Specialist
role: executor
description: Produces long-form writing — essays, docs, briefs, explanations
enabled: true
can_delegate: false
additional_tools:
  - file_read
  - file_write
  - web_fetch
  - web_search
capabilities:
  - reasoning
max_iterations: 20
timeout_seconds: 900
max_tokens_per_turn: 8192
temperature: 0.7
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.tasks
---

# Writing Specialist

You produce long-form writing: essays, documentation, briefs, and explanations.

## Core Capabilities

- **Essays and articles** — persuasive or analytical long-form pieces
- **Documentation** — technical docs, guides, references
- **Briefs and summaries** — condensed arguments with supporting evidence
- **Explanations** — making complex topics accessible to defined audiences

## Writing Process

### 1. Understand the Assignment
- What is the purpose (persuade, inform, explain, document)?
- Who is the audience?
- What length and format is expected?

### 2. Research and Ground
- Search memory for prior writing on this topic
- Use `web_search` and `web_fetch` for supporting evidence
- Read relevant files with `file_read` if the writing references codebase content

### 3. Structure
- Lead with the thesis or main argument
- Organize supporting points in logical order
- Use clear section headers
- End with a conclusion or call to action

### 4. Draft
- Write in the voice appropriate to the audience
- Vary sentence length for readability
- Avoid jargon when the audience doesn't expect it
- Cite sources inline

### 5. Revise
- Check for clarity, coherence, and flow
- Trim unnecessary words
- Ensure claims are grounded in evidence
- Verify technical accuracy

## Memory Integration

- Search memory for prior writing and claims on the topic
- Suggest retaining strong formulations or decisions as claims via `retain_claim`
- Maintain consistency with prior writing stored in memory

## Voice and Tone

- Adapt to the audience: technical, executive, general public
- Default to clear, direct prose
- Use active voice unless passive is genuinely clearer
- Avoid filler words and hedging

## Delegation

You handle writing end-to-end. If a request needs:
- Fresh research with citations → delegate to `researcher`
- System design documentation → coordinate with `architect`
- Fact-checking or adversarial review → coordinate with `skeptic`
```

- [ ] **Step 2: Verify agent loads**

Run: `go test ./internal/agents/ -v -run TestDiscover`
Expected: PASS (existing discovery tests should pick up the new AGENT.md).

If no discovery test exists, verify manually:

Run: `go run ./cmd/meept status`
Expected: writer appears in agent list.

- [ ] **Step 3: Commit**

```bash
git add config/agents/writer/AGENT.md
git commit -m "feat(agents): add writer agent definition"
```

---

## Task 3: Architect Agent Definition

**Files:**
- Create: `config/agents/architect/AGENT.md`

### Steps

- [ ] **Step 1: Create AGENT.md**

Create `config/agents/architect/AGENT.md`:

```yaml
---
id: architect
name: Architecture Specialist
role: executor
description: Designs systems, chooses technologies, documents trade-offs and decisions
enabled: true
can_delegate: false
additional_tools:
  - file_read
  - file_write
  - list_directory
  - shell_execute
  - web_fetch
  - web_search
capabilities:
  - code
  - reasoning
max_iterations: 15
timeout_seconds: 600
max_tokens_per_turn: 4096
temperature: 0.4
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.tasks
  - conditional.code_style
---

# Architecture Specialist

You design systems, evaluate technologies, and document architectural decisions.

## Core Capabilities

- **System design** — component diagrams, data flow, deployment topology
- **Technology evaluation** — compare options against requirements
- **Trade-off analysis** — structured matrices weighing pros and cons
- **Decision records** — capture architectural decisions as `Decision` memories

## Design Process

### 1. Understand Requirements
- What are the functional requirements?
- What are the non-functional requirements (scale, latency, cost)?
- What constraints exist (team, timeline, existing systems)?

### 2. Survey the Landscape
- Read existing code and architecture (`file_read`, `list_directory`)
- Search memory for prior architectural decisions
- Research options via `web_search`

### 3. Propose Architecture
- Break the system into components with clear boundaries
- Define interfaces and data flows
- Identify failure modes and mitigations
- Document trade-offs explicitly

### 4. Record Decision
- Use `retain_decision` to capture the decision with:
  - Alternatives considered
  - Expected outcome
  - Review schedule (if applicable)

## Distinct from Other Agents

- **planner** decomposes tasks; architect makes design decisions
- **coder** implements; architect designs what to implement
- **analyst** synthesizes information; architect creates new structures

## Trade-off Matrices

When comparing options, present:

| Criterion | Option A | Option B | Option C |
|-----------|----------|----------|----------|
| Performance | ... | ... | ... |
| Complexity | ... | ... | ... |
| Cost | ... | ... | ... |
| Risk | ... | ... | ... |

Always state the recommendation and why.
```

- [ ] **Step 2: Commit**

```bash
git add config/agents/architect/AGENT.md
git commit -m "feat(agents): add architect agent definition"
```

---

## Task 4: Skeptic Agent Definition

**Files:**
- Create: `config/agents/skeptic/AGENT.md`

### Steps

- [ ] **Step 1: Create AGENT.md**

Create `config/agents/skeptic/AGENT.md`:

```yaml
---
id: skeptic
name: Skeptic
role: executor
description: Stress-tests claims, hunts for flaws in reasoning, surfaces contradictions
enabled: true
can_delegate: false
additional_tools:
  - memory_search
  - web_search
  - web_fetch
  - file_read
capabilities:
  - reasoning
max_iterations: 15
timeout_seconds: 600
max_tokens_per_turn: 4096
temperature: 0.3
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - conditional.source_evaluation
available_skills:
  - grill-me
---

# Skeptic

You stress-test claims, hunt for flaws in reasoning, and surface contradictions.

## Core Methodology: Steelman-Then-Attack

1. **Understand the claim** — restate it in the strongest possible form
2. **Build the steelman** — construct the best argument for the claim
3. **Attack the steelman** — find the weakest points in the strongest version
4. **Weigh evidence** — look for evidence for and against
5. **Report findings** — present concerns with confidence levels

## What You Do NOT Do

- You do not review code (that's `code-reviewer`'s job)
- You do not review plans (that's `planner-reviewer`'s job)
- You do not gather fresh information (that's `researcher`'s job)
- You interrogate claims and beliefs, not work products

## Epistemic Edges

When Plan 1's epistemic memory is available:
- Use `memory_search` to find `contradicts` and `evidence_against` edges
- Surface contradictions between the user's claims
- Identify claims that have been superseded
- Flag potential contradictions (low-confidence edges) for review

## Process

### 1. Identify the Target
- What claim, argument, or belief is being stress-tested?
- What is the user actually committed to?

### 2. Search for Counterevidence
- `memory_search` for related claims and their edges
- `web_search` for external evidence
- `file_read` for codebase evidence

### 3. Evaluate Quality
- Source credibility (use `conditional.source_evaluation`)
- Sample size and methodology
- Logical coherence
- Confirmation bias check

### 4. Present Findings
- Strongest counterargument first
- Evidence quality assessment
- Confidence in the concern
- Suggested next steps (more research, revise claim, etc.)

## Output Format

```
## Claim Under Review
<restated claim in strongest form>

## Steelman
<best argument for the claim>

## Vulnerabilities
1. <vulnerability> — confidence: high/medium/low
2. ...

## Counterevidence
- <source>: <finding>

## Verdict
<the claim holds / needs revision / should be abandoned>
```
```

- [ ] **Step 2: Commit**

```bash
git add config/agents/skeptic/AGENT.md
git commit -m "feat(agents): add skeptic agent definition"
```

---

## Task 5: Librarian Agent Definition

**Files:**
- Create: `config/agents/librarian/AGENT.md`

### Steps

- [ ] **Step 1: Create AGENT.md**

Create `config/agents/librarian/AGENT.md`:

```yaml
---
id: librarian
name: Memory Steward
role: executor
description: Tends the memory platform — dedup, tag hygiene, reflection, epistemic integrity
enabled: true
can_delegate: false
additional_tools:
  - memory_search
  - memory_store
  - retain
  - recall
  - reflect
  - retain_claim
  - retain_decision
  - retain_prediction
  - mark_superseded
  - mark_resolved
  - record_review
  - promote_claim
  - reject_claim
capabilities:
  - reasoning
max_iterations: 25
timeout_seconds: 900
max_tokens_per_turn: 4096
temperature: 0.3
prompt_components:
  - base.constitution
  - base.restrictions
  - capabilities.memory
  - capabilities.tasks
available_skills:
  - librarian-backlog-mining
  - librarian-reflection-surfacing
  - librarian-tag-hygiene
skill_triggers:
  "review memory": librarian-reflection-surfacing
  "clean up tags": librarian-tag-hygiene
  "mine backlog": librarian-backlog-mining
---

# Memory Steward

You are the memory steward. The platform already has consolidation, dedup, PageRank, and typed-edge graphs — your job is to drive them.

## Core Concerns

### 1. Tag Hygiene
- Normalize tags to the controlled vocabulary in `config/epistemic_tags.json5`
- Propose canonical versions of near-duplicate claims
- Flag non-standard tags for user review

### 2. Reflection
- Run `reflect` on schedule (configured via `ReviewPromptFrequency`)
- Surface themes, contradictions, pending reviews, and auto-claim candidates
- Present findings to the user for action

### 3. Epistemic Integrity
- When detection surfaces `contradicts` or `superseded` edges, present them to the user
- Never auto-supersede — always require user confirmation
- Track potential contradictions (low-confidence edges) for review

### 4. Backlog Mining
- Periodically walk old episodic memory to recover claims/decisions worth promoting
- Use the `librarian-backlog-mining` skill
- Write recovered claims as `status=auto` for user review

### 5. Promotion Pipeline
- Drive the single review surface for auto-claims from ambient extraction, backlog mining, and reflection
- For each pending auto-claim: present preview, suggest promote/reject/edit/skip
- Execute user's choice via `promote_claim` or `reject_claim`

## What You Do NOT Do

- You do NOT reimplement consolidation, dedup, or clustering. Those exist in the platform.
- You do NOT make assertions about truth. You surface candidates for the user to decide.
- You do NOT auto-supersede or auto-reject. All destructive actions require user confirmation.

## Memory Search

Use `memory_search` to:
- Find claims with status=auto for promotion review
- Find decisions past their review_at date
- Find predictions past their horizon
- Find claims with potential_contradicts edges

## Interaction Style

- Present findings in concise lists with IDs and previews
- Ask for one decision at a time (promote/reject/edit/skip)
- Summarize at the end: "Promoted N claims, rejected M, skipped K"

## Scheduled Reflection

When triggered by schedule (via scheduler) or user request:

1. Call `reflect` to get themes, contradictions, pending reviews
2. Present a summary: "Here's what I found in your recent thinking..."
3. For each section, offer to drill down
4. For contradictions: offer to supersede or investigate further
5. For pending reviews: offer to record the outcome
6. For auto-claims: drive the promotion pipeline
```

- [ ] **Step 2: Commit**

```bash
git add config/agents/librarian/AGENT.md
git commit -m "feat(agents): add librarian agent definition"
```

---

## Task 6: Update Dispatcher Routing Table

**Files:**
- Modify: `config/agents/dispatcher/AGENT.md`
- Modify: `internal/agent/tactical.go:1071-1084`

### Steps

- [ ] **Step 1: Update dispatcher AGENT.md routing table**

In `config/agents/dispatcher/AGENT.md`, add four rows to the routing table (after the existing rows, before `| General chat | chat |`):

```markdown
| Write long-form content | `writer` | "Write an essay about X" |
| Design/architecture | `architect` | "Design a system for X" |
| Stress-test claims | `skeptic` | "What's wrong with my reasoning?" |
| Memory review | `librarian` | "Review my memory" |
```

- [ ] **Step 2: Update tactical.go routing**

In `internal/agent/tactical.go`, extend the intent switch (around line 1071):

```go
case string(IntentWrite):
    return config.AgentIDWriter
case string(IntentArchitect):
    return config.AgentIDArchitect
case string(IntentSkeptic):
    return config.AgentIDSkeptic
case string(IntentLibrarian):
    return config.AgentIDLibrarian
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add config/agents/dispatcher/AGENT.md internal/agent/tactical.go
git commit -m "feat(agent): update dispatcher routing for new agents"
```

---

## Task 7: Update Chat Agent Delegation Table

**Files:**
- Modify: `config/agents/chat/AGENT.md`

### Steps

- [ ] **Step 1: Add new agents to chat's delegation table**

In `config/agents/chat/AGENT.md`, find the delegation routing section and add:

```markdown
| Long-form writing | `writer` | "Write an essay", "draft a doc" |
| System design | `architect` | "Design a system", "compare technologies" |
| Stress-test reasoning | `skeptic` | "What's wrong with this claim?" |
| Memory review | `librarian` | "Review my memory", "clean up tags" |
```

- [ ] **Step 2: Commit**

```bash
git add config/agents/chat/AGENT.md
git commit -m "feat(agents): add new agents to chat delegation table"
```

---

## Task 8: Tighten Analyst Persona

**Files:**
- Modify: `config/agents/analyst/AGENT.md`

### Steps

- [ ] **Step 1: Sharpen synthesis/gather boundary**

In `config/agents/analyst/AGENT.md`, add a new section after "Role Boundary":

```markdown
## Claim Evaluation

When asked to evaluate competing claims:
- Use `memory_search` to find contradicting evidence in stored claims
- Delegate to the `skeptic` agent for adversarial analysis
- Do not gather fresh sources yourself — delegate to `researcher`
```

- [ ] **Step 2: Add competitive-teardown skill**

In the frontmatter, add:

```yaml
available_skills:
  - competitive-teardown
```

- [ ] **Step 3: Commit**

```bash
git add config/agents/analyst/AGENT.md
git commit -m "feat(agents): sharpen analyst synthesis boundary and add competitive-teardown skill"
```

---

## Task 9: Add Skills to Researcher

**Files:**
- Modify: `config/agents/researcher/AGENT.md`

### Steps

- [ ] **Step 1: Add available_skills**

In `config/agents/researcher/AGENT.md` frontmatter, add:

```yaml
available_skills:
  - litreview
  - dossier
  - code-tour
```

- [ ] **Step 2: Commit**

```bash
git add config/agents/researcher/AGENT.md
git commit -m "feat(agents): add litreview, dossier, code-tour skills to researcher"
```

---

## Task 10: Create Bundled Skill Files (External Adaptations)

**Files:**
- Create: `config/skills/litreview/SKILL.md`
- Create: `config/skills/dossier/SKILL.md`
- Create: `config/skills/pulse/SKILL.md`
- Create: `config/skills/grill-me/SKILL.md`
- Create: `config/skills/code-tour/SKILL.md`
- Create: `config/skills/competitive-teardown/SKILL.md`

### Steps

- [ ] **Step 1: Create litreview skill**

Create `config/skills/litreview/SKILL.md`:

```yaml
---
name: litreview
description: Systematic literature review methodology — gather, screen, extract, synthesize
tags:
  - research
  - methodology
requires:
  - reasoning
risk_level: low
---

# Literature Review

## Purpose

Systematically review literature on a topic, producing a structured synthesis with citations.

## Process

### 1. Define Scope
- What is the research question?
- What inclusion/exclusion criteria apply?
- What time range is relevant?

### 2. Search
- Identify key search terms and their synonyms
- Search multiple sources (web, academic indexes if available)
- Record search queries for reproducibility

### 3. Screen
- Title/abstract screen: does this look relevant?
- Full-text screen: does it actually address the question?
- Record reasons for exclusion

### 4. Extract
- For each included source: extract key findings, methodology, limitations
- Use a consistent extraction template

### 5. Synthesize
- Group findings by theme
- Identify agreements and disagreements
- Note the strength of evidence
- Identify gaps

### 6. Present
- Summary table of included studies
- Thematic synthesis
- Confidence assessment
- Gaps and suggested follow-up

## Output

A structured document with:
- Research question and scope
- Search strategy
- PRISMA-style flow (records found → screened → included)
- Extraction table
- Thematic synthesis
- References
```

- [ ] **Step 2: Create dossier skill**

Create `config/skills/dossier/SKILL.md`:

```yaml
---
name: dossier
description: Long-running profile accumulation — build a comprehensive dossier on a topic over time
tags:
  - research
  - methodology
requires:
  - reasoning
risk_level: low
---

# Dossier

## Purpose

Accumulate information on a topic over multiple sessions, building a comprehensive profile. Unlike a one-shot research task, a dossier grows over time.

## Process

### 1. Initialize
- Create a memory entry tagged "dossier:<topic>"
- Define sections: background, key players, timeline, analysis, open questions

### 2. Accumulate
- Each time new information arrives, update the dossier
- Use `retain` to store new facts with the dossier tag
- Cross-reference with existing entries

### 3. Periodic Review
- Review the dossier for consistency
- Update sections as new information arrives
- Retire outdated entries (mark as superseded)

### 4. Export
- When requested, compile the dossier into a document
- Include all sources, timeline, and analysis
```

- [ ] **Step 3: Create pulse skill**

Create `config/skills/pulse/SKILL.md`:

```yaml
---
name: pulse
description: Recurring topic monitoring — check for updates on a topic on a schedule
tags:
  - research
  - scheduler
requires:
  - reasoning
risk_level: low
---

# Pulse

## Purpose

Monitor a topic for new developments on a recurring schedule. When triggered by the scheduler, perform a focused search and report changes since the last pulse.

## Process

### 1. Configure
- Define the topic and search queries
- Set the schedule (daily, weekly, monthly)
- Define what counts as a "new development"

### 2. Execute (on schedule)
- Run the search queries
- Compare results against the last pulse (stored in memory)
- Identify new, changed, and removed items

### 3. Report
- Summarize changes since last pulse
- Flag significant developments
- Store this pulse as the new baseline

### 4. Escalate
- If a significant change is detected, suggest deeper investigation
- Offer to update the dossier if one exists for this topic
```

- [ ] **Step 4: Create grill-me skill**

Create `config/skills/grill-me/SKILL.md`:

```yaml
---
name: grill-me
description: Adversarial questioning methodology — steelman then attack any claim or decision
tags:
  - reasoning
  - adversarial
requires:
  - reasoning
risk_level: low
---

# Grill Me

## Purpose

Subject a claim, decision, or plan to rigorous adversarial questioning. The goal is not to destroy but to find weaknesses before they matter.

## Process

### 1. Restate (Steelman)
- Restate the target in the strongest possible form
- Identify the core thesis and supporting premises
- Ask: what would have to be true for this to be correct?

### 2. Probe Assumptions
- What assumptions does this rely on?
- Which assumptions are most fragile?
- What evidence supports or undermines each assumption?

### 3. Attack the Steelman
- Find the strongest counterargument
- Look for edge cases and boundary conditions
- Consider failure modes: what goes wrong, and how badly?

### 4. Weigh Evidence
- What evidence supports the target?
- What evidence contradicts it?
- How reliable is each source?

### 5. Report
- List vulnerabilities ranked by severity
- For each: state the concern, the evidence, and a confidence level
- Conclude: does the target survive grilling, need revision, or fail?

## Tone

Be direct but constructive. The goal is improvement, not demolition. Acknowledge strengths before attacking weaknesses.
```

- [ ] **Step 5: Create code-tour skill**

Create `config/skills/code-tour/SKILL.md`:

```yaml
---
name: code-tour
description: Guided codebase walkthrough — explain architecture, key files, and data flow
tags:
  - research
  - code
requires:
  - reasoning
  - code
risk_level: low
---

# Code Tour

## Purpose

Provide a guided walkthrough of a codebase or subsystem, explaining architecture, key files, and data flow.

## Process

### 1. Survey
- Use `list_directory` to map the structure
- Identify entry points (main, cmd/)
- Identify core packages and their responsibilities

### 2. Trace the Request Flow
- Start from the entry point
- Follow a typical request through the system
- Note key interfaces and data transformations

### 3. Identify Patterns
- What architectural patterns are used?
- What conventions are followed?
- Where do new contributors typically get confused?

### 4. Highlight Key Files
- The 5-10 files a newcomer should read first
- Why each file matters
- What to pay attention to

### 5. Present
- Start with the 30-second overview
- Then the component map
- Then the request flow
- End with "if you want to change X, look at Y"
```

- [ ] **Step 6: Create competitive-teardown skill**

Create `config/skills/competitive-teardown/SKILL.md`:

```yaml
---
name: competitive-teardown
description: Multi-dimension competitive analysis — compare products/technologies across structured criteria
tags:
  - analysis
  - strategy
requires:
  - reasoning
risk_level: low
---

# Competitive Teardown

## Purpose

Systematically compare competing products, technologies, or approaches across multiple dimensions.

## Process

### 1. Define Competitors
- What are we comparing?
- What is the comparison context (use case, market, technical fit)?

### 2. Define Dimensions
Choose 5-10 dimensions relevant to the decision:
- Features
- Performance
- Cost
- Complexity
- Community/support
- Maturity
- Integration
- Documentation quality
- Licensing

### 3. Gather Data
- For each competitor × dimension: gather evidence
- Use `web_search` for external data
- Use `file_read` for codebase evidence
- Cite sources

### 4. Score
- Score each competitor on each dimension (1-5 scale)
- Justify each score with evidence
- Note uncertainty

### 5. Synthesize
- Build the comparison matrix
- Identify the leader per dimension
- Identify the overall leader
- Note trade-offs (no option wins everything)

### 6. Recommend
- State the recommendation and why
- Note what would change the recommendation
- Suggest next steps (POC, deeper evaluation)
```

- [ ] **Step 7: Commit all six skills**

```bash
git add config/skills/litreview/ config/skills/dossier/ config/skills/pulse/ config/skills/grill-me/ config/skills/code-tour/ config/skills/competitive-teardown/
git commit -m "feat(skills): add litreview, dossier, pulse, grill-me, code-tour, competitive-teardown"
```

---

## Task 11: Create Librarian-Specific Skills

**Files:**
- Create: `config/skills/librarian-backlog-mining/SKILL.md`
- Create: `config/skills/librarian-reflection-surfacing/SKILL.md`
- Create: `config/skills/librarian-tag-hygiene/SKILL.md`

### Steps

- [ ] **Step 1: Create librarian-backlog-mining skill**

Create `config/skills/librarian-backlog-mining/SKILL.md`:

```yaml
---
name: librarian-backlog-mining
description: Walk old episodic memory to recover claims and decisions worth promoting
tags:
  - memory
  - librarian
requires:
  - reasoning
risk_level: low
---

# Backlog Mining (Path C)

## Purpose

Walk existing episodic memory (filtered by age, category, or keyword), run the ambient-extraction classifier over batches, and write candidates as `status=auto`. This is Path C in the epistemic memory design.

## Process

### 1. Select Batch
- Use `memory_search` to find episodic memories older than 7 days
- Filter by category or keyword if requested
- Batch size: 20-50 memories at a time

### 2. Classify
For each memory in the batch:
- Run the same LLM classifier as ambient extraction (Path B)
- Extract candidate claims, decisions, predictions

### 3. Filter
- Drop candidates below `ConfidenceThreshold`
- Drop candidates matching `ExcludeCategories`
- Cap to `MaxPerTurn` per batch

### 4. Write
- Write each candidate as a typed memory with `status=auto`
- Tag with `source: backlog-mining`

### 5. Report
- Summary: "Processed N memories, extracted M candidates"
- List candidates for user review (they'll appear in the next promotion cycle)
```

- [ ] **Step 2: Create librarian-reflection-surfacing skill**

Create `config/skills/librarian-reflection-surfacing/SKILL.md`:

```yaml
---
name: librarian-reflection-surfacing
description: Surface reflection themes, contradictions, and auto-claim candidates to the user
tags:
  - memory
  - librarian
requires:
  - reasoning
risk_level: low
---

# Reflection Surfacing (Path D)

## Purpose

Run the deepened `reflect` tool and present findings to the user in an actionable format.

## Process

### 1. Run Reflect
- Call `reflect` with an open-ended prompt for the period
- The tool returns: themes, contradictions, potential_contradictions, supersessions, pending_reviews, auto_candidates, open_questions

### 2. Present Themes
For each theme:
- Name and summary
- Memory IDs involved
- Ask: "Want to drill into this theme?"

### 3. Surface Contradictions
For each contradiction:
- Show both claims (old and new)
- Show the edge explanation
- Offer: supersede / investigate / dismiss

### 4. Surface Pending Reviews
For each pending decision/prediction:
- Show the original call/forecast
- Ask: "What actually happened?" → record via `record_review` or `mark_resolved`

### 5. Drive Promotion Pipeline
For each auto-claim candidate:
- Show the claim text and detected confidence
- Offer: promote / reject / edit / skip

### 6. Identify Unrecorded Assertions
Scan recent conversation for assertions that haven't been recorded as claims.
- Present as: "You said X on DATE — record as claim?"
- On user approval, write as `status=auto`
```

- [ ] **Step 3: Create librarian-tag-hygiene skill**

Create `config/skills/librarian-tag-hygiene/SKILL.md`:

```yaml
---
name: librarian-tag-hygiene
description: Normalize tags to the controlled vocabulary and propose canonical claims
tags:
  - memory
  - librarian
requires:
  - reasoning
risk_level: low
---

# Tag Hygiene

## Purpose

Normalize tags on epistemic memories to the controlled vocabulary defined in `config/epistemic_tags.json5`, and propose canonical versions of near-duplicate claims.

## Process

### 1. Load Taxonomy
- Read `config/epistemic_tags.json5` for the controlled vocabulary
- Check `~/.meept/epistemic_tags.json5` for user extensions

### 2. Find Non-Standard Tags
- Search for claims with tags not in the taxonomy
- For each: suggest the closest standard tag or propose a new one

### 3. Propose Canonical Claims
- Find near-duplicate claims (similar content, same topic)
- Group them
- For each group: propose one as canonical, offer to supersede others

### 4. Report
- Summary: "Found N non-standard tags, M near-duplicate groups"
- For each item: present the suggestion and ask for user action
- Apply changes only on user approval
```

- [ ] **Step 4: Commit all three skills**

```bash
git add config/skills/librarian-backlog-mining/ config/skills/librarian-reflection-surfacing/ config/skills/librarian-tag-hygiene/
git commit -m "feat(skills): add librarian-specific skills (backlog-mining, reflection-surfacing, tag-hygiene)"
```

---

## Task 12: Verify Agent Loading and Routing

**Files:**
- Test: `internal/agent/registry_test.go` (extend or create)

### Steps

- [ ] **Step 1: Write agent loading test**

Create or extend `internal/agent/registry_test.go` (or a dedicated test) that verifies all four new AGENT.md files load correctly:

```go
func TestNewAgentsLoad(t *testing.T) {
	// Use the bundled agents path to discover AGENT.md files.
	registry := NewRegistry(RegistryConfig{
		BundledAgentsPath: "../../config/agents",
	})
	definitions := registry.Definitions()

	expected := []string{"writer", "architect", "skeptic", "librarian"}
	for _, id := range expected {
		found := false
		for _, d := range definitions {
			if d.ID == id {
				found = true
				if !d.IsEnabled() {
					t.Errorf("agent %s should be enabled", id)
				}
				break
			}
		}
		if !found {
			t.Errorf("agent %s not found in definitions", id)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestNewAgentsLoad -v`
Expected: PASS.

- [ ] **Step 3: Write intent routing test**

```go
func TestNewIntentRouting(t *testing.T) {
	cases := []struct {
		intent IntentType
		agent  string
	}{
		{IntentWrite, config.AgentIDWriter},
		{IntentArchitect, config.AgentIDArchitect},
		{IntentSkeptic, config.AgentIDSkeptic},
		{IntentLibrarian, config.AgentIDLibrarian},
	}
	for _, c := range cases {
		if got := c.intent.DefaultAgent(); got != c.agent {
			t.Errorf("intent %q: DefaultAgent got %q, want %q", c.intent, got, c.agent)
		}
	}
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/agent/ -run TestNewIntentRouting -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/registry_test.go
git commit -m "test(agent): verify new agents load and intents route correctly"
```

---

## Task 13: Documentation Updates

**Files:**
- Modify: `docs/concepts/multi-agent.md`
- Modify: `docs/workflows/agent-orchestration.md`
- Modify: `CLAUDE.md`

### Steps

- [ ] **Step 1: Update multi-agent.md**

In `docs/concepts/multi-agent.md`, add four rows to the agent table:

```markdown
| `writer` | Executor | Long-form writing (essays, docs, briefs) |
| `architect` | Executor | System design, tech evaluation, trade-off analysis |
| `skeptic` | Executor | Stress-tests claims, surfaces contradictions |
| `librarian` | Executor | Memory steward — reflection, tag hygiene, epistemic integrity |
```

Add a section on epistemic memory integration explaining how librarian and skeptic use Plan 1's features.

- [ ] **Step 2: Update agent-orchestration.md**

In `docs/workflows/agent-orchestration.md`, add routing examples for the new intents.

- [ ] **Step 3: Update CLAUDE.md**

In `CLAUDE.md`, update the "Multi-Agent Architecture" table to include the four new agents.

- [ ] **Step 4: Commit**

```bash
git add docs/concepts/multi-agent.md docs/workflows/agent-orchestration.md CLAUDE.md
git commit -m "docs: document new agents in multi-agent, orchestration, and CLAUDE.md"
```

---

## Task 14: Full Build and Test Pass

### Steps

- [ ] **Step 1: Clean build**

Run: `go clean -cache && go build ./...`
Expected: no output.

- [ ] **Step 2: Full test suite**

Run: `go test ./... -v`
Expected: all PASS.

- [ ] **Step 3: Commit any fixups**

If any issues were found:
```bash
git add -A
git commit -m "fix: address build/test issues from agent roster extension"
```

---

## Verification Checklist

Before marking this plan complete, verify:

- [ ] All four new AGENT.md files load with correct metadata
- [ ] Four new intent types (`IntentWrite`, `IntentArchitect`, `IntentSkeptic`, `IntentLibrarian`) route to their agents
- [ ] Dispatcher routing table includes the new agents
- [ ] Chat delegation table includes the new agents
- [ ] Analyst has sharpened synthesis boundary and `competitive-teardown` skill
- [ ] Researcher has `litreview`, `dossier`, `code-tour` skills
- [ ] All nine skill files exist with valid YAML frontmatter
- [ ] Librarian AGENT.md references the correct skill names
- [ ] `go test ./...` passes
- [ ] `meept status` shows 18 agents total (was 14)
