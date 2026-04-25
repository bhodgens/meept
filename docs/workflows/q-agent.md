# Q Agent: Meta-Agent for Agent Creation and Optimization

## Overview

The Q Agent (Quartermaster) is a meta-agent that analyzes session transcripts to identify opportunities for creating new specialized agents or improving existing ones. It closes the loop between execution and agent design, enabling self-improving agent ecosystems.

## Problem Statement

The multi-agent system has fixed agent definitions that don't evolve based on actual usage patterns:
- Sessions that take too long go unnoticed
- Agents that struggle with certain tasks continue handling them
- Opportunities for new specialized agents go undetected
- Model misconfigurations persist without detection

## Solution

Q Agent provides:
1. **Session Analysis**: Parse completed session transcripts for patterns
2. **Pattern Detection**: Identify recurring problems across sessions
3. **Research Reports**: Deep-dive analysis with causal attribution
4. **Agent Design**: Generate new agent specifications based on findings
5. **Impact Estimation**: Quantify expected improvements from recommendations

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Q Agent Architecture                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                  │
│  │   Session    │    │   Pattern    │    │   Agent      │                  │
│  │   Analyzer   │───▶│   Detector   │───▶│   Designer   │                  │
│  └──────────────┘    └──────────────┘    └──────────────┘                  │
│         │                   │                   │                           │
│         ▼                   ▼                   ▼                           │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐                  │
│  │  Transcript  │    │  Duration    │    │   Impact     │                  │
│  │  Parser     │    │  Analyzer    │    │   Estimator  │                  │
│  └──────────────┘    └──────────────┘    └──────────────┘                  │
│         │                   │                                                │
│         ▼                   ▼                                                │
│  ┌──────────────┐    ┌──────────────┐                                       │
│  │  Memory      │    │   Research   │                                       │
│  │  Correlator  │    │   Engine     │                                       │
│  └──────────────┘    └──────────────┘                                       │
│         │                   │                                                │
│         └─────────┬─────────┘                                                │
│                   ▼                                                          │
│         ┌─────────────────┐                                                  │
│         │  Reviewer Agent │                                                  │
│         │  (validation)   │                                                  │
│         └─────────────────┘                                                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Components

### Session Analyzer (`internal/agent/q/session_analyzer.go`)

Parses and analyzes completed session transcripts.

**Responsibilities**:
- Fetch completed sessions from memvid (zone: "sessions")
- Extract: duration, iteration count, tool calls, agent switches, revision cycles
- Compute complexity metrics: token usage, context switches, delegation depth
- Tag sessions with difficulty indicators (0.0-1.0)

**Data Model**:
```go
type SessionAnalysis struct {
    SessionID        string
    Duration         time.Duration
    IterationCount   int
    AgentSwitches    int
    RevisionCycles   int
    TokenUsage       int
    ToolCalls        []ToolCallRecord
    DifficultyScore  float64  // 0.0-1.0
    AnomalyFlags     []string // "long_duration", "high_iterations", "agent_thrashing"
}
```

### Pattern Detector (`internal/agent/q/pattern_detector.go`)

Identifies recurring problems across sessions.

| Pattern | Signal | Action |
|---------|--------|--------|
| Model Misconfiguration | Same task type, 3x duration variance | Recommend model reassignment |
| High Error Rate | Agent error rate > 20% | Analyze prompt/tooling gaps |
| Wrong Agent Assignment | Tasks requiring capabilities agent lacks | Create specialist agent |
| High Tool Failure Rate | Tool call failure rate > 20% | Review tool definitions |
| High Rejection Rate | Coder rejected by reviewer > 30% | Improve agent specification |
| Repeated Failure | Same intent fails 3+ times with same agent | Create specialist |

### Research Engine (`internal/agent/q/research_engine.go`)

Deep-dive analysis with causal attribution.

**Research Types**:
1. **Behavioral Analysis**: Why did the agent struggle?
2. **Implementation Analysis**: What code caused issues?
3. **Tooling Analysis**: What tools were missing or inadequate?
4. **Capability Analysis**: What capabilities does the agent lack?
5. **Model/Agent Fit Analysis**: Is the assigned model appropriate?

**Causal Attribution Decision Tree**:
1. Wrong agent? → Task required capability not in agent's purpose
2. Wrong model? → Model lacks required capability (code, reasoning, tool_use)
3. Missing tool? → Tool call failed with "not found" or capability gap
4. Bad prompt? → Agent output shows confusion or goes off-track
5. Missing memory? → Relevant memories exist but not injected

### Agent Designer (`internal/agent/q/agent_designer.go`)

Generates new agent specifications based on best practices.

**Agent Design Best Practices**:
1. **Explicit Role Boundaries**: Clear statement of what agent does AND doesn't do
2. **Escalation Triggers**: When to delegate vs. handle directly
3. **Workflow Disclosure**: Show reasoning steps before action
4. **Output Structure**: Required format for responses
5. **Constraint Enforcement**: Explicit limits (iterations, tools, token budgets)
6. **Graceful Degradation**: Fallback behavior when constraints hit

### Impact Estimator (`internal/agent/q/impact_estimator.go`)

Quantifies expected improvement from recommendations.

| Misconfiguration Type | Baseline Metric | Impact Formula |
|----------------------|-----------------|----------------|
| Model Misconfiguration | Current model avg duration | `(current - optimal) / current × 100%` |
| High Error Rate | Current error rate | `(current - platform_avg) × session_count` |
| Wrong Agent Assignment | Task completion rate | `(optimal - current) × 100%` |
| High Tool Failure Rate | Tool success rate | `failure_reduction × avg_time_saved` |
| High Rejection Rate | Review pass rate | `(current_rejection - target) × revision_cost` |

## Configuration

Add to `~/.meept/meept.toml`:

```toml
[q_agent]
enabled = true
session_idle_trigger_hours = 12
analysis_timeout_minutes = 30
min_sessions_for_pattern = 5
min_confidence_score = 0.7
high_error_rate_threshold = 0.2
high_rejection_rate_threshold = 0.3
duration_variance_threshold = 3.0
notify_chat = true
notify_cli = true
notify_menu_bar = false
analysis_dir = "~/.meept/q_analysis"
outcomes_log = "~/.meept/q_outcomes.jsonl"
```

### Configuration Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Enable Q Agent analysis |
| `session_idle_trigger_hours` | int | 12 | Hours before session is considered complete |
| `analysis_timeout_minutes` | int | 30 | Maximum analysis duration |
| `min_sessions_for_pattern` | int | 5 | Minimum sessions to detect a pattern |
| `min_confidence_score` | float | 0.7 | Minimum confidence for recommendations |
| `high_error_rate_threshold` | float | 0.2 | Error rate threshold for flagging |
| `high_rejection_rate_threshold` | float | 0.3 | Rejection rate threshold for flagging |
| `duration_variance_threshold` | float | 3.0 | Duration variance threshold |
| `notify_chat` | bool | true | Notify via chat |
| `notify_cli` | bool | true | Notify via CLI |
| `notify_menu_bar` | bool | false | Notify via menu bar |
| `analysis_dir` | string | ~/.meept/q_analysis | Analysis output directory |
| `outcomes_log` | string | ~/.meept/q_outcomes.jsonl | Outcomes log file |

## CLI Commands

### `meept q status`

Show Q Agent status and configuration.

```
$ meept q status
Q Agent status
================
enabled:         true
memvid healthy:  true
sessions tracked: 47
analysis dir:    ~/.meept/q_analysis
outcomes log:    ~/.meept/q_outcomes.jsonl

configuration:
  session idle trigger:  12 hours
  analysis timeout:      30 minutes
  min sessions/pattern:  5
  min confidence:        70.0%
  high error threshold:  20%
  high rejection threshold: 30%
```

### `meept q analyze`

Run Q Agent analysis on completed sessions.

```
$ meept q analyze
starting Q Agent analysis...
analyzing sessions idle for 12+ hours

Analysis Complete
=================
sessions analyzed: 47
status:          completed
summary:         Analyzed 47 sessions. Found 2 improvement opportunities: (1) high_rejection_rate - 85% confidence, (2) wrong_agent_assignment - 72% confidence

patterns detected: 2

1. high_rejection_rate (85% confidence)
   affected: coder
   sessions: 12
   action:   update_spec

2. wrong_agent_assignment (72% confidence)
   affected: api_testing
   sessions: 8
   action:   create_agent

recommendations: 2

1. Update coder agent specification [high priority]
   Agent spec needs improvement: missing output format requirements
   expected impact: Reduce rejection rate from 35% to 10%

2. Create specialist agent for api_testing [medium priority]
   Evidence suggests need for specialized agent: capability gap
   expected impact: 40% speedup, ~50 min/week saved

Full report saved to: ~/.meept/q_analysis/analysis_2026-04-24_analysis.json
```

Options:
- `--force`: Run analysis even if Q Agent is disabled
- `--json`: Output results as JSON

## Session Persistence

Sessions are persisted to memvid when idle for the configured threshold (default: 12 hours).

**Stored Data**:
```json
{
  "session_id": "abc123",
  "start_time": "2026-04-24T10:00:00Z",
  "end_time": "2026-04-24T10:45:00Z",
  "duration_seconds": 2700,
  "total_requests": 15,
  "intents": ["code_review", "file_edit", "test"],
  "agent_id": "coder",
  "outcome": "completed",
  "iterations": 8,
  "token_usage": 12500,
  "tool_calls": 5,
  "agent_switches": 1,
  "errors": 0,
  "revisions": 2
}
```

## File Structure

```
internal/agent/q/
├── session_analyzer.go      # Parse session transcripts
├── pattern_detector.go      # Identify recurring problems
├── research_engine.go       # Deep-dive analysis
├── agent_designer.go        # Generate agent specs
├── impact_estimator.go      # Quantify improvements
├── types.go                 # Data models
└── q_agent.go               # Main orchestrator

docs/workflows/
└── q-agent.md               # This specification

~/.meept/
├── q_analysis/              # Cached analysis results
│   └── analysis_2026-04-24_analysis.json
└── q_outcomes.jsonl         # Recommendation outcomes log
```

## Error Handling and Failure Modes

| Failure Scenario | Handling Strategy |
|-----------------|-------------------|
| Incorrect Analysis | Recommendation reviewed by `reviewer` agent. User has final approval. |
| Agent File Conflicts | Check for existing agent IDs. Flag conflicts for user resolution. |
| Analysis Timeout | Configurable timeout (default: 30 min). Abort with partial findings. |
| Memory Store Unavailable | Fall back to SQLite episodic memory. Log error. |
| Bad Recommendation | Track rejection rate. If >50% rejected, disable auto-analysis. |
| Infinite Loop Risk | Single-pass analysis only. No iterative refinement. |

## Testing Strategy

| Component | Test Approach |
|-----------|--------------|
| Session Analyzer | Unit tests with synthetic session data |
| Pattern Detector | Table-driven tests with known patterns |
| Research Engine | Integration tests with recorded transcripts |
| Agent Designer | Golden file tests for generated AGENT.md files |
| Impact Estimator | Unit tests with known metrics |
| End-to-End | Synthetic multi-session scenario |

## Token/Cost Considerations

**Analysis Cost Estimate** (per session):
- Session transcript read: ~500-2000 tokens input
- Pattern analysis: ~1000 tokens input, ~500 tokens output
- Research generation: ~2000 tokens input, ~1000 tokens output
- **Total per session**: ~3500-5500 tokens

**Cost Control**:
- Use cheapest capable model (e.g., Haiku) for analysis
- Limit analysis to sessions exceeding thresholds
- Configurable `max_sessions_per_analysis` to bound cost
- Budget alert: notify user if analysis exceeds token budget

## "Do Nothing" Option

Q Agent can conclude with "No recommendations" report.

**Thresholds for No Recommendation**:
- No patterns detected above `min_confidence_score`
- Fewer than `min_sessions_for_pattern` sessions for any pattern
- All detected issues within acceptable thresholds
- Evidence ambiguous or contradictory

**"All Systems Nominal" Report** includes metrics summary table showing all agents operating within expected parameters.

## Implementation Status

### Phase 1: Foundation (Completed)
- [x] Session analyzer with memvid persistence
- [x] Basic pattern detection (duration, errors, rejections)
- [x] Research report generation
- [x] Configuration schema

### Phase 2: Agent Design (Completed)
- [x] Agent designer with template-based generation
- [x] Impact estimator
- [x] CLI commands (`meept q analyze`, `meept q status`)

### Phase 3: Integration (In Progress)
- [ ] Idle trigger implementation (background goroutine)
- [ ] Notification channels (chat, CLI)
- [ ] Outcome logging

### Phase 4: Refinement (Future)
- [ ] Self-monitoring and heuristic tuning
- [ ] Agent splitter (clustering algorithm)
- [ ] Memory correlator
- [ ] Reviewer agent integration for validation

## References

- Multi-agent architecture: `docs/concepts/multi-agent.md`
- Agent specs: `internal/agent/spec.go`
- Memory system: `docs/concepts/memory.md`
- Session tracker: `internal/agent/session_tracker.go`
- Anthropic "Building Effective Agents" research
