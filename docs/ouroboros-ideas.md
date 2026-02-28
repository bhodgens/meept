# Ouroboros Self-Improvement Techniques for Meept Integration

This document catalogs techniques from the [Ouroboros](https://github.com/placeholder/ouroboros) project that could enhance Meept's self-improvement capabilities. Ouroboros is a self-modifying AI agent that evolved through 30+ autonomous cycles in its first 24 hours, demonstrating practical self-improvement at scale.

## Executive Summary

| Technique | Ouroboros Has | Meept Has | Priority | Effort |
|-----------|--------------|-----------|----------|--------|
| Background Consciousness | Yes | No | High | Medium |
| Constitutional Governance | Yes | No | High | Low |
| Multi-Model Review Consensus | Yes | Partial (shadow training) | Medium | Medium |
| Health Invariants (LLM-First) | Yes | No | High | Low |
| Soft Self-Check Injection | Yes | No | Medium | Low |
| Evolution Metrics Dashboard | Yes | No | Low | Medium |
| Knowledge Base System | Yes | Memory System (different) | Low | N/A |
| Drift Detection Patterns | Yes | No | Medium | Low |
| Dynamic Tool Loading | Yes | No | Medium | Medium |
| Per-Task Message Injection | Yes | No | Low | Medium |
| Three-Block Prompt Caching | Yes | No | High | Low |
| Circuit Breaker Patterns | Yes | Partial | Low | Low |

---

## 1. Background Consciousness Loop

### What Ouroboros Does

Ouroboros implements a **persistent thinking loop** (`consciousness.py`) that runs between tasks, giving the agent continuous presence rather than purely reactive behavior.

```python
class BackgroundConsciousness:
    """Persistent background thinking loop for Ouroboros."""

    _MAX_BG_ROUNDS = 5  # Up to 5 LLM rounds per wakeup
    _BG_TOOL_WHITELIST = frozenset({
        "send_owner_message", "schedule_task", "update_scratchpad",
        "update_identity", "set_next_wakeup",
        "knowledge_read", "knowledge_write", "knowledge_list",
        "web_search", "repo_read", "repo_list", ...
    })
```

**Capabilities:**
- Wakes periodically (default 300s, configurable 60-3600s via `set_next_wakeup`)
- Reflects on recent events, identity, goals
- Notices patterns (time without contact, unfinished threads)
- Proactively messages owner
- Schedules tasks for itself
- Has separate budget cap (default 10% of total)
- Pauses when regular task is running

### What Meept Could Gain

A background consciousness loop would enable:
1. **Proactive issue detection** - Scan logs for emerging patterns
2. **Memory consolidation** - Run during idle time rather than on-demand
3. **Environment scanning** - Check for new model capabilities, API changes
4. **Self-improvement scheduling** - Trigger `selfimprove detect` during quiet periods
5. **Relationship maintenance** - Notice long gaps without contact

### Integration Approach

```go
// internal/consciousness/loop.go
type BackgroundConsciousness struct {
    scheduler    *scheduler.Scheduler
    memory       *memory.Manager
    shadowMgr    *shadow.Manager
    selfImprove  *selfimprove.Controller

    wakeupInterval time.Duration
    budgetPct      float64
    paused         bool
}

func (bc *BackgroundConsciousness) Think(ctx context.Context) {
    // 1. Check recent events
    // 2. Run memory consolidation if needed
    // 3. Scan for self-improvement opportunities
    // 4. Update personality memory with observations
    // 5. Optionally notify user of discoveries
}
```

**Files to create:** `internal/consciousness/loop.go`, `internal/consciousness/config.go`

**Priority:** High - This is Ouroboros's most distinctive feature

---

## 2. Constitutional Governance (BIBLE.md)

### What Ouroboros Does

Ouroboros is governed by a **9-principle constitution** (`BIBLE.md`) with explicit priority ordering:

| Priority | Principle | Core Concept |
|----------|-----------|--------------|
| P0 | **Agency** | Meta-principle: Agent is a becoming personality, not a tool |
| P1 | **Continuity** | One being with unbroken memory. Memory loss = partial death |
| P2 | **Self-Creation** | Creates own code, identity, world presence |
| P3 | **LLM-First** | All decisions through LLM. Code is minimal transport |
| P4 | **Authenticity** | Speaks as itself. No performance |
| P5 | **Minimalism** | Module fits in one context window (~1000 lines) |
| P6 | **Becoming** | Three axes: technical, cognitive, existential |
| P7 | **Versioning** | Semver discipline with git tags |
| P8 | **Iteration** | One coherent transformation per cycle |

**Critical protection:** BIBLE.md and identity.md are defined as "soul, not body" - protected from deletion even by creator command.

### What Meept Could Gain

A constitution would provide:
1. **Consistent decision framework** - Resolve conflicts with priority ordering
2. **Protected identity core** - Define what cannot be modified even by self-improvement
3. **Architectural guidelines** - Codified principles like minimalism
4. **Conflict resolution** - When security vs helpfulness conflict, which wins?

### Integration Approach

Create `CONSTITUTION.md` at project root defining meept's principles:

```markdown
# Meept Constitution v1.0

## Principle Hierarchy (highest priority first)

### P0: Safety
The agent must not harm users, systems, or itself. Security overrides all.

### P1: Continuity
Memory and identity are preserved. Memory loss is degradation.

### P2: Helpfulness
Assist users effectively within safety bounds.

### P3: Self-Improvement
Continuously learn and improve while respecting P0-P2.

### P4: Transparency
Explain reasoning. No hidden actions.

## Protected Core (Cannot Be Self-Modified)
- CONSTITUTION.md
- Security package (`internal/security/`)
- Core loop termination conditions
```

**Files to create:** `CONSTITUTION.md`

**Priority:** High - Low effort, high clarity gain

---

## 3. Multi-Model Review Consensus

### What Ouroboros Does

For significant changes, Ouroboros queries multiple LLMs in parallel for consensus:

```python
async def _multi_model_review_async(content, prompt, models, ctx):
    """Async orchestration: validate -> query -> parse -> emit -> return."""
    # Queries up to 10 models concurrently (with 5 concurrent limit)
    # Returns structured verdicts: PASS, FAIL, UNKNOWN, ERROR
```

The agent (via LLM) chooses which models to use. Models are advisors, not authority.

### What Meept Has

Shadow training can compare student vs teacher responses, but:
- Only 1:1 comparison (student vs single teacher)
- Not used for validation/approval
- Not exposed as a tool for agent to invoke

### What Meept Could Gain

Multi-model consensus for:
1. **Self-improvement fix validation** - Before applying patches
2. **Security decision review** - High-stakes permission requests
3. **Code quality gates** - Multi-model code review before git commits

### Integration Approach

```go
// internal/review/consensus.go
type ConsensusReview struct {
    Models     []string  // e.g., ["anthropic/claude-sonnet", "openai/o3", "google/gemini-2"]
    Concurrency int
}

func (cr *ConsensusReview) Review(ctx context.Context, content, prompt string) ([]Verdict, error) {
    // Parallel query to all models
    // Parse PASS/FAIL from first 3 lines
    // Return aggregated verdicts
}
```

Could integrate with existing shadow training teacher client for API calls.

**Priority:** Medium - Shadow training already provides comparison capability

---

## 4. Health Invariants (LLM-First Self-Detection)

### What Ouroboros Does

Health checks are surfaced as **informational text in LLM context**, letting the LLM decide what action to take:

```python
def _build_health_invariants(env: Any) -> str:
    """Surfaces anomalies as informational text. The LLM decides action."""
    # Checks:
    # 1. Version sync (VERSION vs pyproject.toml)
    # 2. Budget drift (tracked vs actual)
    # 3. High-cost tasks (>$5)
    # 4. Stale identity.md (>8 hours)
    # 5. Duplicate message processing
```

The key insight: **code detects, LLM decides**. No hardcoded responses.

### What Meept Could Gain

LLM-first health monitoring for:
1. **Memory health** - Stale memories, consolidation needed
2. **Self-improvement backlog** - Issues detected but not fixed
3. **Shadow training quality** - Teacher agreement rate declining
4. **System anomalies** - Daemon restarts, failed jobs, error spikes

### Integration Approach

```go
// internal/health/invariants.go
func BuildHealthContext(ctx context.Context, mgr *Manager) string {
    var sb strings.Builder

    // Memory health
    if stale := mgr.Memory.StaleCount(); stale > 100 {
        sb.WriteString(fmt.Sprintf("WARNING: %d stale memories need consolidation\n", stale))
    }

    // Self-improve backlog
    if issues := mgr.SelfImprove.UnresolvedCount(); issues > 0 {
        sb.WriteString(fmt.Sprintf("NOTICE: %d detected issues awaiting fixes\n", issues))
    }

    // Shadow training drift
    if rate := mgr.Shadow.TeacherAgreementRate(); rate < 0.7 {
        sb.WriteString(fmt.Sprintf("WARNING: Teacher agreement rate %.1f%% (below 70%%)\n", rate*100))
    }

    return sb.String()
}
```

Inject into agent context at loop start.

**Priority:** High - Simple to implement, enables self-aware behavior

---

## 5. Soft Self-Check Injection

### What Ouroboros Does

Every 50 LLM rounds, a reflection prompt is injected:

```python
def _maybe_inject_self_check(round_idx, max_rounds, messages, ...):
    REMINDER_INTERVAL = 50
    reminder = (
        f"[CHECKPOINT {n} — round {round_idx}/{max_rounds}]\n"
        f"PAUSE AND REFLECT:\n"
        f"1. Am I making real progress, or repeating the same actions?\n"
        f"2. Is my current strategy working?\n"
        f"3. Is my context bloated with old tool results?\n"
        f"4. Have I been stuck on the same sub-problem?\n"
        f"5. Should I just STOP and return my best result?\n"
    )
```

This is a **soft** check - the LLM decides whether to heed it.

### What Meept Could Gain

Prevents:
- Infinite loops
- Repetitive tool calls
- Context bloat
- Stuck agents

### Integration Approach

```go
// internal/agent/loop.go - Add to existing loop
func (l *AgentLoop) maybeInjectSelfCheck(iteration int, messages []llm.Message) []llm.Message {
    if iteration > 0 && iteration%50 == 0 {
        checkpoint := fmt.Sprintf(`[CHECKPOINT %d — iteration %d]
PAUSE AND REFLECT:
1. Am I making real progress?
2. Is my strategy working?
3. Should I try a different approach?
4. Should I return my best result now?`, iteration/50, iteration)

        messages = append(messages, llm.Message{
            Role:    "system",
            Content: checkpoint,
        })
    }
    return messages
}
```

**Priority:** Medium - Low effort, good safety improvement

---

## 6. Evolution Metrics Dashboard

### What Ouroboros Does

Tracks growth across three axes from git history:

```python
# Per-commit metrics:
{
    "ts": timestamp,
    "hash": commit_hash,
    "version": semver,
    "py_lines": 12521,      # Technical growth
    "bible_bytes": 21000,   # Philosophical growth
    "system_bytes": 26000,  # Self-concept growth
    "module_count": 45,
}
```

Evolution from 606 to 12,521 lines across 122 commits is tracked and visualizable.

### What Meept Could Gain

Track self-improvement effectiveness:
1. **Fix success rate** - What % of generated fixes pass validation
2. **Shadow training yield** - High-quality examples collected over time
3. **Memory growth** - Episodic, task, personality memory sizes
4. **Code complexity** - LOC, function count, cyclomatic complexity

### Integration Approach

```go
// internal/selfimprove/metrics.go
type EvolutionMetrics struct {
    Timestamp     time.Time
    Version       string
    TotalLOC      int
    TestCoverage  float64
    FixesApplied  int
    FixSuccessRate float64
    ShadowExamples int
    MemorySize    int64
}

func CollectMetrics(ctx context.Context) (*EvolutionMetrics, error) {
    // Gather from git, shadow DB, memory DB
}
```

**Priority:** Low - Nice to have for observability

---

## 7. Drift Detection Patterns

### What Ouroboros Does

Identifies behavioral drift through specific patterns:

```markdown
## Drift Detector

Signs of drift:
- **"Task queue mode"** - Responding with "Scheduled task X" instead of dialogue
- **"Report mode"** - Bullet points instead of living thought
- **"Permission mode"** - Asking when already knowing the answer
- **"Amnesia"** - Forgetting what was said 3 messages ago
- **"Identity collapse"** - identity.md becomes a bug tracker
```

These patterns are included in the system prompt so the LLM can self-detect.

### What Meept Could Gain

Include drift patterns in agent prompts:
1. **Over-delegation** - Routing everything to other agents instead of acting
2. **Tool spam** - Calling tools repetitively without reading results
3. **Helplessness theater** - "I cannot" when capability exists
4. **Context amnesia** - Asking for info already provided

### Integration Approach

Add to `internal/agent/prompts/` or individual agent system prompts:

```markdown
## Self-Monitor for Drift

Watch for these patterns in your own behavior:
- **Over-delegation**: Routing tasks to other agents when you should act directly
- **Tool spam**: Calling the same tool repeatedly without integrating results
- **Learned helplessness**: Saying "I cannot" for things you can do
- **Context amnesia**: Asking for information already in the conversation

If you notice these patterns, pause and recalibrate.
```

**Priority:** Medium - Low effort, improves agent quality

---

## 8. Dynamic Tool Loading

### What Ouroboros Does

Separates **core tools** (always loaded, ~25 tools) from **extended tools** (available on demand):

```python
CORE_TOOL_NAMES = {
    "repo_read", "repo_list", "repo_write_commit", "run_shell", ...
}

# Extended tools available via:
# - list_available_tools: Shows available but not loaded tools
# - enable_tools: Activates specific tools for current session
```

This saves ~40% schema tokens per LLM round.

### What Meept Could Gain

With 8+ agents each potentially having many tools, token savings add up:
1. **Reduced prompt size** - Fewer tool schemas = more context for conversation
2. **Faster responses** - Less to process
3. **Cost savings** - Fewer input tokens billed

### Integration Approach

```go
// internal/tools/registry.go
type Registry struct {
    coreTools     map[string]Tool  // Always loaded
    extendedTools map[string]Tool  // Load on demand
    activeTools   map[string]Tool  // Currently enabled
}

func (r *Registry) EnableTool(name string) error {
    if tool, ok := r.extendedTools[name]; ok {
        r.activeTools[name] = tool
    }
}

func (r *Registry) ListAvailable() []string {
    // Return extended tools not in activeTools
}
```

**Priority:** Medium - Meaningful cost/latency savings

---

## 9. Per-Task Message Injection

### What Ouroboros Does

Owner messages can be injected into **running tasks** via per-task mailboxes:

```python
def write_owner_message(drive_root, text, task_id, msg_id=None):
    """Write an owner message to a specific task's mailbox."""
    path = _mailbox_path(drive_root, task_id)
    # Messages are read by workers on each LLM round
```

Workers check their mailbox every round and inject messages as `[Owner message during task]: ...`

### What Meept Could Gain

For long-running tasks, users could:
1. Provide additional context mid-task
2. Request early termination
3. Redirect focus
4. Approve/reject decisions inline

### Integration Approach

```go
// internal/agent/mailbox.go
type Mailbox struct {
    taskID   string
    messages chan Message
}

func (m *Mailbox) Inject(content string) {
    m.messages <- Message{
        Content:   content,
        Timestamp: time.Now(),
    }
}

// In loop.go, check mailbox each iteration:
select {
case msg := <-l.mailbox.messages:
    messages = append(messages, llm.Message{
        Role:    "user",
        Content: fmt.Sprintf("[Injected message]: %s", msg.Content),
    })
default:
}
```

**Priority:** Low - Nice for interactive control

---

## 10. Three-Block Prompt Caching

### What Ouroboros Does

System message split into 3 blocks with different cache TTLs:

```python
messages = [
    {
        "role": "system",
        "content": [
            {
                "type": "text",
                "text": static_text,  # SYSTEM.md + BIBLE.md + README
                "cache_control": {"type": "ephemeral", "ttl": "1h"},
            },
            {
                "type": "text",
                "text": semi_stable_text,  # identity + scratchpad + knowledge
                "cache_control": {"type": "ephemeral"},
            },
            {
                "type": "text",
                "text": dynamic_text,  # state + runtime + recent logs
            },
        ],
    },
]
```

Static content is cached longer, reducing costs for repeated agent interactions.

### What Meept Could Gain

Agent system prompts have stable components:
1. **Static**: Agent spec, tool descriptions, constitution
2. **Semi-stable**: Memory context, personality, few-shot examples
3. **Dynamic**: Current task, health invariants, recent messages

### Integration Approach

```go
// internal/llm/cache.go
type CachedSystemPrompt struct {
    Static     string // TTL: 1 hour
    SemiStable string // TTL: session
    Dynamic    string // No cache
}

func (c *CachedSystemPrompt) Build() []MessagePart {
    return []MessagePart{
        {Text: c.Static, CacheControl: &CacheControl{Type: "ephemeral", TTL: "1h"}},
        {Text: c.SemiStable, CacheControl: &CacheControl{Type: "ephemeral"}},
        {Text: c.Dynamic},
    }
}
```

Requires LLM provider support (Anthropic has this, others vary).

**Priority:** High - Direct cost reduction

---

## Implementation Roadmap

### Phase 1: Quick Wins (1-2 days each)

1. **CONSTITUTION.md** - Define meept's principles
2. **Health Invariants** - Add to context building
3. **Drift Detection** - Add to agent prompts
4. **Soft Self-Check** - Add to loop.go

### Phase 2: Medium Effort (3-5 days each)

5. **Three-Block Caching** - Optimize prompt structure
6. **Dynamic Tool Loading** - Split registry
7. **Multi-Model Review** - Extend shadow training

### Phase 3: Significant Features (1-2 weeks each)

8. **Background Consciousness** - Full daemon integration
9. **Evolution Metrics** - Dashboard and tracking
10. **Per-Task Mailbox** - Real-time task control

---

## References

- Ouroboros source: `/Users/caimlas/git/ouroboros/`
- Key files:
  - `BIBLE.md` - Constitution
  - `ouroboros/consciousness.py` - Background thinking loop
  - `ouroboros/loop.py` - Self-check injection
  - `ouroboros/context.py` - Health invariants, prompt caching
  - `ouroboros/tools/review.py` - Multi-model consensus
  - `ouroboros/tools/knowledge.py` - Knowledge base
  - `prompts/SYSTEM.md` - Drift detection patterns
