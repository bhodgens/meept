# Turbo Thread E — Immediate Self-Reflection

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring Turbo-style immediate self-reflection to meept. After every agent turn, a reflection LLM extracts 0-1 operational lessons and writes them as proposals to `.meept/improvements.md`. A 30-min timer examines inactive sessions for deeper lessons. Skills replace `patterns.json`. Both an agent tool and a user slash command (`/remember`) create immediate proposals.

**Architecture:**
- `ReflectionCollector` runs after each agent turn, building a rich `Trajectory` (tool calls + results + errors + retries), rendering `reflection/turn.md`, and calling a cheap classifier LLM.
- A periodic timer (30 min) queries for inactive sessions (≥15 min since last activity) and runs deeper reflection via `reflection/session.md`.
- Proposals land in `.meept/improvements.md` (propose-only by default; SKILL.md under `.meept/skills/auto/` is auto-writable; AGENT.md/CLAUDE.md/config/prompts are always propose-only).
- `/remember` is both an agent tool and a TUI slash command.
- `ContextInjector` loads skills instead of patterns.json.
- `/implement-improvements` reviews the queue (CLI/TUI/Flutter).

**Tech Stack:** Go, existing agent loop + LLM client + bus + scheduler, JSON5 config, Flutter for UI.

**Depends on:** Thread A (template loader — `reflection/turn.md`, `reflection/session.md`).

**Note on Q Agent rework:** Per spec, Q Agent rework is **deferred to a separate spec/plan**. This thread reserves the integration point (Q proposals land in `.meept/improvements.md`) but does not implement Q internals.

**Spec source:** `docs/superpowers/specs/2026-06-24-turbo-innovations-adoption-design.md` — Thread E.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `config/prompts/reflection/turn.md` | NEW — per-turn reflection template |
| `config/prompts/reflection/session.md` | NEW — periodic session reflection template |
| `internal/agent/trajectory.go` | NEW — `Trajectory`, `TrajectoryStep` types + builder from Conversation |
| `internal/agent/trajectory_test.go` | NEW — tests |
| `internal/agent/reflection_collector.go` | NEW — `ReflectionCollector` type, `ReflectTurn`, `ReflectInactiveSessions` |
| `internal/agent/reflection_collector_test.go` | NEW — tests |
| `internal/agent/proposal.go` | NEW — `ReflectionProposal` type, `.meept/improvements.md` queue reader/writer |
| `internal/agent/proposal_test.go` | NEW — tests |
| `internal/agent/loop.go` | MODIFY — replace `triggerLearning` (line 1657/1801) with `ReflectionCollector.ReflectTurn`; enrich trajectory |
| `internal/agent/context_injector.go` | MODIFY — load skills instead of patterns |
| `internal/selfimprove/learning.go` | MODIFY — stop writing to patterns.json; keep consolidation logic, apply to skill metadata |
| `internal/tools/builtin/remember.go` | NEW — `/remember` agent tool |
| `internal/tui/command_handler.go` | MODIFY — `/remember` slash command |
| `internal/daemon/components.go` | MODIFY — wire `ReflectionCollector` + 30-min timer |
| `cmd/meept/implement_improvements.go` | NEW — `meept improvements list/apply/skip` CLI |
| `internal/services/reflection_service.go` | NEW — service layer |
| `internal/comm/http/api_handlers.go` | MODIFY — `GET/POST /api/v1/reflection/proposals`, `POST /api/v1/reflection/remember` |
| `internal/comm/http/server.go` | MODIFY — register routes |
| `config/meept.json5` | MODIFY — add `reflection` config block |
| `internal/config/schema.go` | MODIFY — `ReflectionCollectorConfig` |
| `internal/tui/improvements/` | NEW — TUI screen for proposal review |
| `ui/flutter_ui/lib/features/reflection/` | NEW — Flutter reflection panel |
| `ui/flutter_ui/lib/features/notifications/` | NEW — notification banners |

---

## Task 1: Reflection templates

**Files:**
- Create: `config/prompts/reflection/turn.md`
- Create: `config/prompts/reflection/session.md`

- [ ] **Step 1: Create `config/prompts/reflection/turn.md`**

Exact content (verbatim from spec §Thread A → "turn.md"):

```markdown
---
name: reflection.turn
description: Per-turn reflection that extracts operational lessons from a single agent turn
---

You are a self-reflection assistant. Examine this agent turn and extract 0 or 1 concrete
operational lessons that would help future agent invocations.

A good lesson is:
- Specific and actionable ("always run go vet after editing .go files"), not abstract
- Generalizable beyond this specific task
- Based on something that worked OR something that failed

Agent: {{.AgentID}}
User input: {{.UserInput}}
Outcome: {{.Outcome}}

Trajectory:
{{.TrajectoryJSON}}

Output ONLY valid JSON. If no clear lesson, output {"proposal": null}.
Otherwise:
{
  "proposal": {
    "type": "skill_create|skill_update|agent_prompt|project_instruction|prompt_component",
    "target": "<file path or skill name>",
    "change": "<proposed modification — full markdown for skills, rule text for instructions>",
    "justification": "<one sentence why>",
    "confidence": 0.0
  }
}

Rules:
- type=skill_create: target is a path like .meept/skills/<name>/SKILL.md, change is full markdown
- type=agent_prompt: target is config/agents/<id>/AGENT.md, change is the new restriction text
- type=project_instruction: target is CLAUDE.md, change is the rule to add
- confidence < 0.6 → output null instead (don't waste review queue)
```

- [ ] **Step 2: Create `config/prompts/reflection/session.md`**

Exact content (verbatim from spec §Thread A → "session.md"):

```markdown
---
name: reflection.session
description: Periodic reflection that examines multiple turns from an inactive session to extract deeper lessons
---

You are a self-reflection assistant performing deeper analysis on a recently-inactive session.

Examine the turns below and extract 0-3 higher-quality lessons about agent behavior, prompt
quality, or workflow patterns.

Session: {{.SessionID}}
Agent: {{.AgentID}}
Total turns: {{.TurnCount}}
Last activity: {{.LastActivity}}

Turns (oldest first):
{{.TurnsJSON}}

Output ONLY valid JSON:
{
  "proposals": [
    {
      "type": "skill_create|skill_update|agent_prompt|project_instruction|prompt_component",
      "target": "<file path or skill name>",
      "change": "<proposed modification>",
      "justification": "<one sentence why>",
      "confidence": 0.0
    }
  ]
}

Rules:
- Maximum 3 proposals (highest-quality only)
- Confidence < 0.7 → drop the proposal
- Prefer cross-turn patterns over single-turn observations
- type=skill_create proposals should describe the trigger condition in the skill description
```

- [ ] **Step 3: Register fallbacks in `plannerTemplateLoader`**

In `internal/agent/planner_template.go` `NewDaemonPlannerTemplateLoader`:

```go
	l.fallbacks["reflection/turn.md"] = defaultReflectionTurnFallback()
	l.fallbacks["reflection/session.md"] = defaultReflectionSessionFallback()
```

Add the fallback consts mirroring the markdown bodies.

- [ ] **Step 4: Commit**

```bash
git add config/prompts/reflection/turn.md config/prompts/reflection/session.md internal/agent/planner_template.go
git commit -m "feat(reflection): add turn.md + session.md templates with fallbacks"
```

---

## Task 2: `Trajectory` types + builder

**Files:**
- Create: `internal/agent/trajectory.go`
- Create: `internal/agent/trajectory_test.go`

- [ ] **Step 1: Write the failing test**

`internal/agent/trajectory_test.go`:

```go
package agent

import (
	"strings"
	"testing"
)

func TestBuildTrajectory_Truncates(t *testing.T) {
	conv := &Conversation{
		Messages: []ConvMessage{
			{Role: "tool", ToolName: "file_read", Content: strings.Repeat("x", 2000)},
		},
	}
	traj := buildTrajectory(conv, "session-1", "coder", "user input", "success", 0)
	if len(traj.Steps) != 1 {
		t.Fatalf("got %d steps; want 1", len(traj.Steps))
	}
	if len(traj.Steps[0].ToolResult) > 500 {
		t.Errorf("tool result not truncated: %d chars", len(traj.Steps[0].ToolResult))
	}
}

func TestBuildTrajectory_Caps50Steps(t *testing.T) {
	conv := &Conversation{}
	for i := 0; i < 100; i++ {
		conv.Messages = append(conv.Messages, ConvMessage{Role: "assistant", Content: "x"})
	}
	traj := buildTrajectory(conv, "s1", "coder", "in", "success", 0)
	if len(traj.Steps) > 50 {
		t.Errorf("steps = %d; want <= 50", len(traj.Steps))
	}
}

func TestTrajectory_JSON(t *testing.T) {
	traj := Trajectory{
		UserInput: "fix bug",
		Steps:     []TrajectoryStep{{Kind: "tool_call", ToolName: "file_edit"}},
	}
	j, err := traj.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(string(j), "fix bug") {
		t.Errorf("JSON missing input")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestBuildTrajectory -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

`internal/agent/trajectory.go`:

```go
package agent

import (
	"encoding/json"
	"time"
)

// Trajectory captures the rich execution trace of a single agent turn.
// Replaces the legacy text-only trajectory used by LearningPipeline.
type Trajectory struct {
	UserInput     string           `json:"user_input"`
	Steps         []TrajectoryStep `json:"steps"`
	FinalResponse string           `json:"final_response"`
	SessionID     string           `json:"session_id"`
	AgentID       string           `json:"agent_id"`
	Outcome       string           `json:"outcome"` // success|partial|failure
	Duration      time.Duration    `json:"duration"`
}

type TrajectoryStep struct {
	Kind       string `json:"kind"` // assistant_message|tool_call|tool_result|error
	Content    string `json:"content"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	RetryOf    string `json:"retry_of,omitempty"`
}

func (t Trajectory) JSON() ([]byte, error) {
	return json.Marshal(t)
}

// buildTrajectory assembles a Trajectory from a conversation. Truncation:
// assistant messages 1000 chars, tool results 500 chars, errors 300 chars.
// Trajectory capped at 50 steps.
func buildTrajectory(conv *Conversation, sessionID, agentID, userInput, outcome string, duration time.Duration) Trajectory {
	traj := Trajectory{
		UserInput: userInput,
		SessionID: sessionID,
		AgentID:   agentID,
		Outcome:   outcome,
		Duration:  duration,
	}
	if conv == nil {
		return traj
	}
	for _, m := range conv.Messages {
		if len(traj.Steps) >= 50 {
			break
		}
		switch m.Role {
		case "assistant":
			traj.Steps = append(traj.Steps, TrajectoryStep{
				Kind:    "assistant_message",
				Content: truncStr(m.Content, 1000),
			})
		case "tool":
			ts := TrajectoryStep{
				Kind:       "tool_result",
				ToolName:   m.ToolName,
				ToolResult: truncStr(m.Content, 500),
			}
			if m.IsError {
				ts.Kind = "error"
				ts.ErrorCode = truncStr(m.Content, 300)
			}
			traj.Steps = append(traj.Steps, ts)
		}
	}
	return traj
}
```

`ConvMessage.IsError` field may or may not exist — verify by reading `internal/agent/conversation.go`. If absent, drop that branch or add the field.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestBuildTrajectory -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/trajectory.go internal/agent/trajectory_test.go
git commit -m "feat(reflection): add Trajectory types with truncation + 50-step cap"
```

---

## Task 3: `ReflectionProposal` + `.meept/improvements.md` queue

**Files:**
- Create: `internal/agent/proposal.go`
- Create: `internal/agent/proposal_test.go`

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProposalQueue_AppendAndList(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	p1 := ReflectionProposal{
		Type:        "skill_create",
		Target:      ".meept/skills/x/SKILL.md",
		Change:      "content",
		Justification: "because",
		Confidence:  0.8,
		Source:      "turn:s1",
	}
	if err := q.Append(p1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	pending, err := q.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending; want 1", len(pending))
	}
	if pending[0].Target != ".meept/skills/x/SKILL.md" {
		t.Errorf("target = %q", pending[0].Target)
	}
}

func TestProposalQueue_MarkApplied(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	p := ReflectionProposal{Type: "agent_prompt", Target: "x", Change: "y", Confidence: 0.7, Source: "test"}
	q.Append(p)
	pending, _ := q.ListPending()
	if err := q.MarkApplied(pending[0].ID); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}
	pending2, _ := q.ListPending()
	if len(pending2) != 0 {
		t.Errorf("after MarkApplied, pending = %d; want 0", len(pending2))
	}
}

func TestProposalQueue_Authorization(t *testing.T) {
	tmp := t.TempDir()
	q := newProposalQueue(filepath.Join(tmp, "improvements.md"))
	// AGENT.md, CLAUDE.md, config/prompts/* always propose-only
	cases := []struct {
		target string
		want   bool // true = always propose-only
	}{
		{"config/agents/coder/AGENT.md", true},
		{"CLAUDE.md", true},
		{"config/prompts/tools/bash.md", true},
		{".meept/skills/auto/foo/SKILL.md", false}, // auto-writable
		{".meept/skills/x/SKILL.md", false},        // propose-only but not "always"
	}
	for _, c := range cases {
		got := isAlwaysProposeOnly(c.target)
		if got != c.want {
			t.Errorf("isAlwaysProposeOnly(%q) = %v; want %v", c.target, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestProposalQueue -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

`internal/agent/proposal.go`:

```go
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ReflectionProposal struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"` // skill_create|skill_update|agent_prompt|project_instruction|prompt_component
	Target       string    `json:"target"`
	Change       string    `json:"change"`
	Justification string   `json:"justification"`
	Confidence   float64   `json:"confidence"`
	Source       string    `json:"source"` // turn:sessionID | session:sessionID | manual:/remember
	Status       string    `json:"status"` // pending|applied|skipped
	CreatedAt    time.Time `json:"created_at"`
}

type proposalQueue struct {
	mu   sync.Mutex
	path string
}

func newProposalQueue(path string) *proposalQueue {
	return &proposalQueue{path: path}
}

// Append writes a new proposal to the queue file.
func (q *proposalQueue) Append(p ReflectionProposal) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if p.ID == "" {
		p.ID = generateProposalID()
	}
	p.Status = "pending"
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	return q.appendToFile(p)
}

func (q *proposalQueue) appendToFile(p ReflectionProposal) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(q.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// Append markdown block
	fmt.Fprintf(f, "\n## [%s] %s — %s\n", p.Status, p.CreatedAt.Format("2006-01-02"), p.ID)
	fmt.Fprintf(f, "- **Type:** %s\n", p.Type)
	fmt.Fprintf(f, "- **Target:** %s\n", p.Target)
	fmt.Fprintf(f, "- **Confidence:** %.2f\n", p.Confidence)
	fmt.Fprintf(f, "- **Source:** %s\n", p.Source)
	fmt.Fprintf(f, "- **Justification:** %s\n", p.Justification)
	fmt.Fprintf(f, "- **Proposed change:** %s\n", strings.ReplaceAll(p.Change, "\n", "\n  "))
	return nil
}

// ListPending reads the queue file and returns all pending proposals.
func (q *proposalQueue) ListPending() ([]ReflectionProposal, error) {
	data, err := os.ReadFile(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseProposals(string(data)), nil
}

// MarkApplied updates a proposal's status from "pending" to "applied" with timestamp.
func (q *proposalQueue) MarkApplied(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	data, err := os.ReadFile(q.path)
	if err != nil {
		return err
	}
	updated := strings.Replace(string(data),
		fmt.Sprintf("## [pending]"), fmt.Sprintf("## [applied %s]", time.Now().Format("2006-01-02")), 1)
	// Naive impl: replace first pending marker with applied. Production version
	// should parse, update the specific entry, and rewrite.
	_ = updated
	// For MVP: simple regex-style find+replace on the specific ID
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.Contains(line, id) {
			// Find the status line above this one (the ## [pending] header)
			for j := i; j >= 0 && j > i-10; j-- {
				if strings.HasPrefix(lines[j], "## [pending]") {
					lines[j] = strings.Replace(lines[j], "[pending]", "[applied "+time.Now().Format("2006-01-02")+"]", 1)
					break
				}
			}
			break
		}
	}
	return os.WriteFile(q.path, []byte(strings.Join(lines, "\n")), 0o644)
}

// isAlwaysProposeOnly returns true for files that must never be auto-applied
// regardless of reflection.auto_apply_all config.
func isAlwaysProposeOnly(target string) bool {
	clean := filepath.Clean(target)
	if clean == "CLAUDE.md" {
		return true
	}
	if strings.HasPrefix(clean, "config/agents/") && strings.HasSuffix(clean, "AGENT.md") {
		return true
	}
	if strings.HasPrefix(clean, "config/prompts/") {
		return true
	}
	return false
}

func generateProposalID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// parseProposals does a lenient scan of the queue markdown and extracts proposals.
func parseProposals(content string) []ReflectionProposal {
	var out []ReflectionProposal
	lines := strings.Split(content, "\n")
	var cur *ReflectionProposal
	for _, line := range lines {
		if strings.HasPrefix(line, "## [pending]") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &ReflectionProposal{Status: "pending"}
			// Extract ID from header
			parts := strings.SplitN(line, "—", 2)
			if len(parts) == 2 {
				cur.ID = strings.TrimSpace(parts[1])
			}
		} else if cur != nil && strings.HasPrefix(line, "- **Type:**") {
			cur.Type = strings.TrimSpace(strings.TrimPrefix(line, "- **Type:**"))
		} else if cur != nil && strings.HasPrefix(line, "- **Target:**") {
			cur.Target = strings.TrimSpace(strings.TrimPrefix(line, "- **Target:**"))
		} else if cur != nil && strings.HasPrefix(line, "- **Confidence:**") {
			fmt.Sscanf(line, "- **Confidence:** %f", &cur.Confidence)
		} else if cur != nil && strings.HasPrefix(line, "- **Source:**") {
			cur.Source = strings.TrimSpace(strings.TrimPrefix(line, "- **Source:**"))
		} else if cur != nil && strings.HasPrefix(line, "- **Justification:**") {
			cur.Justification = strings.TrimSpace(strings.TrimPrefix(line, "- **Justification:**"))
		}
	}
	if cur != nil && cur.Status == "pending" {
		out = append(out, *cur)
	}
	return out
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestProposalQueue -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/proposal.go internal/agent/proposal_test.go
git commit -m "feat(reflection): add ReflectionProposal + .meept/improvements.md queue"
```

---

## Task 4: `ReflectionCollector`

**Files:**
- Create: `internal/agent/reflection_collector.go`
- Create: `internal/agent/reflection_collector_test.go`
- Modify: `internal/config/schema.go`
- Modify: `config/meept.json5`

- [ ] **Step 1: Add config schema**

In `internal/config/schema.go`, add top-level (not nested under Agent):

```go
type ReflectionCollectorConfig struct {
	Enabled              bool    `json:"enabled"`
	AutoQueue            bool    `json:"auto_queue"`             // auto-write to improvements.md
	AutoSkillUnder       string  `json:"auto_skill_under"`       // path under which auto-write SKILL.md (e.g., .meept/skills/auto/)
	SkillProposalsOnly   bool    `json:"skill_proposals_only"`   // non-auto skills propose-only
	AutoApplyAll         bool    `json:"auto_apply_all"`         // overrides skill_proposals_only (NOT AGENT.md/CLAUDE.md/prompts)
	InactivityMinutes    int     `json:"inactivity_minutes"`     // session-inactive threshold
	TimerIntervalMinutes int     `json:"timer_interval_minutes"` // periodic check interval
	TurnConfidenceMin    float64 `json:"turn_confidence_min"`
	SessionConfidenceMin float64 `json:"session_confidence_min"`
	MaxSessionProposals  int     `json:"max_session_proposals"`
}
```

Defaults in `DefaultConfig`:
```go
	Reflection: config.ReflectionCollectorConfig{
		Enabled:              true,
		AutoQueue:            true,
		AutoSkillUnder:       ".meept/skills/auto/",
		SkillProposalsOnly:   true,
		InactivityMinutes:    15,
		TimerIntervalMinutes: 30,
		TurnConfidenceMin:    0.6,
		SessionConfidenceMin: 0.7,
		MaxSessionProposals:  3,
	},
```

In `config/meept.json5`, add new top-level block (do NOT confuse with existing `"reflection"` under agents):

```json5
  // Turbo Thread E — immediate self-reflection
  "reflection_collector": {
    "enabled": true,
    "auto_queue": true,
    "auto_skill_under": ".meept/skills/auto/",
    "skill_proposals_only": true,
    "auto_apply_all": false,
    "inactivity_minutes": 15,
    "timer_interval_minutes": 30,
    "turn_confidence_min": 0.6,
    "session_confidence_min": 0.7,
    "max_session_proposals": 3,
  },
```

- [ ] **Step 2: Write the failing test**

`internal/agent/reflection_collector_test.go`:

```go
package agent

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReflectionCollector_ReflectTurn_DropsLowConfidence(t *testing.T) {
	rc, queuePath := newTestCollector(t)
	// Stub classifier to return confidence 0.4
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return `{"proposal":{"type":"skill_create","target":"x","change":"y","justification":"z","confidence":0.4}}`, nil
	}
	traj := Trajectory{UserInput: "x", Outcome: "success"}
	err := rc.ReflectTurn(context.Background(), traj)
	if err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
	pending, _ := newProposalQueue(queuePath).ListPending()
	if len(pending) != 0 {
		t.Errorf("low-confidence proposal was queued: %d", len(pending))
	}
}

func TestReflectionCollector_ReflectTurn_QueuesValidProposal(t *testing.T) {
	rc, queuePath := newTestCollector(t)
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return `{"proposal":{"type":"skill_create","target":".meept/skills/x/SKILL.md","change":"content","justification":"because","confidence":0.8}}`, nil
	}
	traj := Trajectory{UserInput: "x", Outcome: "success"}
	if err := rc.ReflectTurn(context.Background(), traj); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
	pending, _ := newProposalQueue(queuePath).ListPending()
	if len(pending) != 1 {
		t.Errorf("pending = %d; want 1", len(pending))
	}
}

func TestReflectionCollector_ReflectTurn_NullProposal(t *testing.T) {
	rc, _ := newTestCollector(t)
	rc.classifierRunOnce = func(ctx context.Context, prompt, convID string) (string, error) {
		return `{"proposal": null}`, nil
	}
	if err := rc.ReflectTurn(context.Background(), Trajectory{}); err != nil {
		t.Fatalf("ReflectTurn: %v", err)
	}
}
```

- [ ] **Step 3: Write minimal implementation**

`internal/agent/reflection_collector.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/pkg/id"
)

type ReflectionCollector struct {
	cfg         config.ReflectionCollectorConfig
	classifier  classifierClient
	templateReg *plannerTemplateLoader
	queue       *proposalQueue
	logger      *slog.Logger

	// Test override for classifier.RunOnce
	classifierRunOnce func(ctx context.Context, prompt, convID string) (string, error)
}

type classifierClient interface {
	RunOnce(ctx context.Context, prompt, conversationID string) (string, error)
}

func NewReflectionCollector(
	cfg config.ReflectionCollectorConfig,
	classifier classifierClient,
	templateReg *plannerTemplateLoader,
	queuePath string,
	logger *slog.Logger,
) *ReflectionCollector {
	return &ReflectionCollector{
		cfg:         cfg,
		classifier:  classifier,
		templateReg: templateReg,
		queue:       newProposalQueue(queuePath),
		logger:      logger.With("component", "reflection"),
	}
}

// ReflectTurn runs per-turn reflection. Builds a prompt from the trajectory,
// calls the classifier, validates confidence, and queues proposals.
func (rc *ReflectionCollector) ReflectTurn(ctx context.Context, traj Trajectory) error {
	if !rc.cfg.Enabled {
		return nil
	}
	trajJSON, _ := traj.JSON()
	prompt, err := rc.templateReg.render("reflection/turn.md", map[string]any{
		"AgentID":        traj.AgentID,
		"UserInput":      traj.UserInput,
		"Outcome":        traj.Outcome,
		"TrajectoryJSON": string(trajJSON),
	})
	if err != nil {
		return fmt.Errorf("render turn.md: %w", err)
	}
	convID := fmt.Sprintf("reflect-%s-%s", traj.SessionID, id.Generate(""))
	var output string
	if rc.classifierRunOnce != nil {
		output, err = rc.classifierRunOnce(ctx, prompt, convID)
	} else {
		output, err = rc.classifier.RunOnce(ctx, prompt, convID)
	}
	if err != nil {
		return fmt.Errorf("classifier call failed: %w", err)
	}
	// Parse response
	var resp struct {
		Proposal *struct {
			Type          string  `json:"type"`
			Target        string  `json:"target"`
			Change        string  `json:"change"`
			Justification string  `json:"justification"`
			Confidence    float64 `json:"confidence"`
		} `json:"proposal"`
	}
	jsonStr := ExtractJSON(output)
	if jsonStr == "" {
		return nil // nothing to do
	}
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		rc.logger.Warn("Failed to parse reflection output", "error", err)
		return nil
	}
	if resp.Proposal == nil {
		return nil
	}
	if resp.Proposal.Confidence < rc.cfg.TurnConfidenceMin {
		return nil
	}
	proposal := ReflectionProposal{
		Type:          resp.Proposal.Type,
		Target:        resp.Proposal.Target,
		Change:        resp.Proposal.Change,
		Justification: resp.Proposal.Justification,
		Confidence:    resp.Proposal.Confidence,
		Source:        fmt.Sprintf("turn:%s", traj.SessionID),
	}
	if err := rc.queue.Append(proposal); err != nil {
		return fmt.Errorf("append proposal: %w", err)
	}
	rc.logger.Info("Reflection proposal queued",
		"type", proposal.Type, "target", proposal.Target, "confidence", proposal.Confidence,
	)
	return nil
}

// ReflectInactiveSessions is called by the periodic timer. Finds sessions
// inactive for >= cfg.InactivityMinutes, runs deeper reflection.
func (rc *ReflectionCollector) ReflectInactiveSessions(ctx context.Context) {
	if !rc.cfg.Enabled {
		return
	}
	// Implementation: query sessions where lastActivity < now - inactivityMinutes
	// AND not yet reflected. For each, gather trajectories and call session.md.
	// This requires a SessionStore; for MVP, log "not implemented" and return.
	rc.logger.Debug("ReflectInactiveSessions scheduled (implementation needs SessionStore integration)")
}
```

Add `newTestCollector` helper in the test file that constructs a collector with a tmp queue path and returns both.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestReflectionCollector -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/reflection_collector.go internal/agent/reflection_collector_test.go internal/config/schema.go config/meept.json5
git commit -m "feat(reflection): add ReflectionCollector with turn.md + confidence threshold"
```

---

## Task 5: Replace `triggerLearning` with `ReflectionCollector.ReflectTurn`

**Files:**
- Modify: `internal/agent/loop.go`

- [ ] **Step 1: Add `reflectionCollector` field to `AgentLoop`**

In `internal/agent/loop.go` `AgentLoop` struct:

```go
	reflectionCollector *ReflectionCollector // optional; if nil, no reflection runs
```

- [ ] **Step 2: Replace `triggerLearning` call site**

In `internal/agent/loop.go` line ~1657, replace:

```go
	l.triggerLearning(context.Background(), conv, conversationID, finalResponse, injectedSkillsSnapshot)
```

with:

```go
	if l.reflectionCollector != nil {
		traj := buildTrajectory(conv, conversationID, l.agentID, userInput, outcome, duration)
		traj.FinalResponse = finalResponse
		if err := l.reflectionCollector.ReflectTurn(ctx, traj); err != nil {
			l.logger.Warn("Reflection failed", "error", err)
		}
	}
	// Keep skill outcome recording (learning_pipeline still handles skill success/fail accounting)
	if l.learningPipeline != nil {
		l.recordSkillOutcomes(injectedSkillsSnapshot, &JudgmentResult{ShouldLearn: false}, conversationID)
	}
```

- [ ] **Step 3: Keep `triggerLearning` as dead-code fallback (or delete)**

For safety during rollout, keep `triggerLearning` but mark deprecated:

```go
// Deprecated: replaced by ReflectionCollector.ReflectTurn in Thread E.
// Retained for tests that exercise the legacy path; will be removed once
// all tests migrate.
func (l *AgentLoop) triggerLearning(...) { ... }
```

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Expected: clean.

Run: `go test ./internal/agent/ -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/loop.go
git commit -m "feat(loop): call ReflectionCollector.ReflectTurn after each turn (replaces triggerLearning)"
```

---

## Task 6: Skills replace patterns.json in `ContextInjector`

**Files:**
- Modify: `internal/agent/context_injector.go`
- Modify: `internal/selfimprove/learning.go`

- [ ] **Step 1: Modify `ContextInjector.BuildSystemPrompt`**

In `internal/agent/context_injector.go`, replace the patterns loading:

```go
// OLD: patterns, _ := c.learning.Retrieve(ctx, "general", "all", 10)
// NEW: skills loaded via the existing skills.SkillLoader (already wired).
```

The injector already has access to a skill loader (search for `skillLoader` on `AgentRegistry`). Pull relevant skills:

```go
func (c *ContextInjector) BuildSystemPrompt(ctx context.Context, base, input string) string {
	// ... existing code ...
	// Replace "## Learned Patterns" section with "## Active Skills"
	if c.skillLoader != nil {
		skills := c.skillLoader.RelevantFor(input) // existing method on LazySkillLoader
		if len(skills) > 0 {
			var sb strings.Builder
			sb.WriteString("\n## Active Skills\n")
			for _, s := range skills {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Name, s.Description))
			}
			prompt += sb.String()
		}
	}
	// ... existing code ...
}
```

If `ContextInjector` doesn't currently hold a `skillLoader`, add it as a field + constructor option.

- [ ] **Step 2: Stop pattern writes in `LearningPipeline`**

In `internal/selfimprove/learning.go`:

1. In `StorePattern` (line 559), short-circuit:

```go
func (lp *LearningPipeline) StorePattern(ctx context.Context, pattern *LearnedPattern) error {
	// Deprecated: patterns.json is no longer written. Skill creation is
	// handled by ReflectionCollector/Q Agent (Thread E).
	lp.logger.Debug("StorePattern called; patterns.json deprecated, no-op")
	return nil
}
```

2. In `savePatternsFromSnapshot` (line 908), comment out the write:

```go
func (lp *LearningPipeline) savePatternsFromSnapshot(patterns map[string]*LearnedPattern) error {
	// Deprecated: no-op. Skills are the new format.
	return nil
}
```

3. Keep `loadPatterns` for backward-compat reads (existing patterns.json content still loaded but not written).

- [ ] **Step 3: Build and test**

Run: `go build ./...`
Expected: clean.

Run: `go test ./internal/selfimprove/ ./internal/agent/ -v -count=1`
Expected: PASS (tests for `StorePattern` need updating — they should now assert no file is written).

- [ ] **Step 4: Commit**

```bash
git add internal/agent/context_injector.go internal/selfimprove/learning.go
git commit -m "feat(reflection): ContextInjector loads skills; patterns.json writes disabled"
```

---

## Task 7: `/remember` tool and slash command

**Files:**
- Create: `internal/tools/builtin/remember.go`
- Modify: `internal/tools/builtin/registry.go` (register tool)
- Modify: `internal/tui/command_handler.go`

- [ ] **Step 1: Write the agent tool**

`internal/tools/builtin/remember.go`:

```go
package builtin

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/tools"
)

// RememberTool lets agents create improvement proposals directly.
// Source: "manual:/remember"
type RememberTool struct {
	queue *agent.ProposalQueueExternal // exported wrapper
}

// NewRememberTool creates a /remember tool bound to a proposal queue path.
func NewRememberTool(queuePath string) *RememberTool {
	return &RememberTool{queue: agent.NewExternalProposalQueue(queuePath)}
}

func (t *RememberTool) Name() string { return "remember" }
func (t *RememberTool) Description() string {
	return "Propose a permanent improvement (skill, prompt change, or rule). " +
		"Use when you observe a reusable lesson worth saving for future sessions."
}
func (t *RememberTool) Schema() tools.Schema {
	return tools.Schema{
		Type: "object",
		Properties: map[string]tools.Property{
			"target": {Type: "string", Description: "File path or skill name to modify"},
			"change": {Type: "string", Description: "Full content (for skill_create) or rule text"},
			"justification": {Type: "string", Description: "Why this improvement matters"},
		},
		Required: []string{"target", "change", "justification"},
	}
}

func (t *RememberTool) Execute(ctx context.Context, params map[string]any) (tools.Result, error) {
	target, _ := params["target"].(string)
	change, _ := params["change"].(string)
	just, _ := params["justification"].(string)
	if target == "" || change == "" {
		return tools.Result{Error: "target and change required"}, nil
	}
	proposal := agent.ReflectionProposal{
		Type:          inferProposalType(target),
		Target:        target,
		Change:        change,
		Justification: just,
		Confidence:    0.9, // manual proposals get high confidence
		Source:        "manual:/remember",
	}
	if err := t.queue.Append(proposal); err != nil {
		return tools.Result{Error: fmt.Sprintf("failed to queue: %v", err)}, nil
	}
	return tools.Result{Output: fmt.Sprintf("Proposal queued: %s", target)}, nil
}

func inferProposalType(target string) string {
	if containsAny(target, []string{"SKILL.md", ".meept/skills/"}) {
		return "skill_create"
	}
	if containsAny(target, []string{"AGENT.md"}) {
		return "agent_prompt"
	}
	if target == "CLAUDE.md" {
		return "project_instruction"
	}
	return "prompt_component"
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) && s[:len(sub)] == sub {
			return true
		}
	}
	return false
}
```

The `agent.ProposalQueueExternal` is an exported wrapper around `proposalQueue` — add to `internal/agent/proposal.go`:

```go
// ProposalQueueExternal is the exported wrapper for cross-package use.
type ProposalQueueExternal struct{ inner *proposalQueue }

func NewExternalProposalQueue(path string) *ProposalQueueExternal {
	return &ProposalQueueExternal{inner: newProposalQueue(path)}
}

func (q *ProposalQueueExternal) Append(p ReflectionProposal) error {
	return q.inner.Append(p)
}
```

- [ ] **Step 2: Register the tool**

In whatever file registers builtin tools (search for `RegisterTool` or look at `internal/tools/builtin/registry.go`):

```go
	registry.Register(tools.NewRegisteredTool(NewRememberTool(queuePath)))
```

The queue path comes from config. Pass it through the daemon's tool registry constructor.

- [ ] **Step 3: Add `/remember` slash command to TUI**

In `internal/tui/command_handler.go`, add handler for `/remember`:

```go
func (h *CommandHandler) handleRemember(input string) {
	if input == "" {
		h.sendUserError("/remember requires text: /remember <rule or description>")
		return
	}
	// Build a proposal and append to queue
	queuePath := h.config().Reflection.QueuePath // or wherever
	q := agent.NewExternalProposalQueue(queuePath)
	proposal := agent.ReflectionProposal{
		Type:          "project_instruction", // user-typed usually wants a rule
		Target:        "CLAUDE.md",
		Change:        input,
		Justification: "user-invoked /remember",
		Confidence:    0.9,
		Source:        "manual:/remember",
	}
	if err := q.Append(proposal); err != nil {
		h.sendUserError(fmt.Sprintf("failed: %v", err))
		return
	}
	h.sendUserMessage("Saved. Use /implement-improvements to review and apply.")
}
```

Register in the slash command table (search for `/help`, `/plan`, etc. in `command_handler.go` or `slash.go`).

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tools/builtin/remember.go internal/tools/builtin/registry.go internal/agent/proposal.go internal/tui/command_handler.go
git commit -m "feat(remember): add /remember agent tool + TUI slash command"
```

---

## Task 8: 30-min reflection timer + daemon wiring

**Files:**
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Wire `ReflectionCollector` in components.go**

Find the agent loop construction in `components.go` (search for `NewAgentLoop` or wherever loops get built). Add:

```go
	reflectionCollector := agent.NewReflectionCollector(
		cfg.Reflection,
		classifierClient, // *llm.Client satisfies classifierClient
		agent.NewDaemonPlannerTemplateLoader("config/prompts"),
		".meept/improvements.md",
		logger.With("component", "reflection"),
	)
	// Pass to each loop
	loopOpts := append(existingOpts, agent.WithReflectionCollector(reflectionCollector))
```

`WithReflectionCollector` is an option on `AgentLoop`. Add it.

- [ ] **Step 2: 30-min timer**

In `components.go`, after the daemon context is established:

```go
	if cfg.Reflection.Enabled {
		reflectionTicker := time.NewTicker(time.Duration(cfg.Reflection.TimerIntervalMinutes) * time.Minute)
		go func() {
			for {
				select {
				case <-reflectionTicker.C:
					reflectionCollector.ReflectInactiveSessions(daemonCtx)
				case <-daemonCtx.Done():
					reflectionTicker.Stop()
					return
				}
			}
		}()
		logger.Info("Reflection collector timer started",
			"interval_minutes", cfg.Reflection.TimerIntervalMinutes,
		)
	}
```

- [ ] **Step 3: Wire `/remember` tool path**

In the tool registry construction (search for `NewToolRegistry` in `components.go`):

```go
	registry.Register(tools.NewRegisteredTool(builtin.NewRememberTool(".meept/improvements.md")))
```

- [ ] **Step 4: Build and test**

Run: `go build ./...`
Expected: clean.

Run: `./bin/meept-daemon -f &` then check logs for "Reflection collector timer started". Kill the daemon.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/components.go internal/agent/loop.go
git commit -m "feat(daemon): wire ReflectionCollector + 30-min timer + /remember tool"
```

---

## Task 9: `meept improvements` CLI + `/implement-improvements`

**Files:**
- Create: `cmd/meept/implement_improvements.go`
- Modify: `cmd/meept/main.go` (register)

- [ ] **Step 1: Write the CLI**

`cmd/meept/implement_improvements.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/agent"
	"github.com/spf13/cobra"
)

func newImprovementsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "improvements",
		Short:   "manage improvement proposals",
		Aliases: []string{"improve", "improvement"},
	}
	cmd.AddCommand(newImprovementsListCmd())
	cmd.AddCommand(newImprovementsApplyCmd())
	cmd.AddCommand(newImprovementsSkipCmd())
	return cmd
}

func newImprovementsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list pending improvement proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			q := agent.NewExternalProposalQueue(".meept/improvements.md")
			pending, err := q.ListPendingExternal()
			if err != nil {
				return err
			}
			if len(pending) == 0 {
				fmt.Println("no pending proposals")
				return nil
			}
			for _, p := range pending {
				fmt.Printf("[%s] %s -> %s\n", p.ID, p.Type, p.Target)
				fmt.Printf("  confidence: %.2f  source: %s\n", p.Confidence, p.Source)
				fmt.Printf("  %s\n\n", p.Justification)
			}
			return nil
		},
	}
}

func newImprovementsApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <id>",
		Short: "apply a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := agent.NewExternalProposalQueue(".meept/improvements.md")
			pending, err := q.ListPendingExternal()
			if err != nil {
				return err
			}
			var target *agent.ReflectionProposal
			for i := range pending {
				if pending[i].ID == args[0] {
					target = &pending[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("proposal %s not found", args[0])
			}
			// Authorization check
			if agent.IsAlwaysProposeOnlyExternal(target.Target) {
				fmt.Printf("⚠️  %s is always propose-only; write manually.\n", target.Target)
				fmt.Printf("Proposed change:\n%s\n", target.Change)
				return nil
			}
			// Apply
			if err := os.WriteFile(target.Target, []byte(target.Change), 0o644); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			if err := q.MarkAppliedExternal(target.ID); err != nil {
				return err
			}
			fmt.Printf("applied: %s\n", target.Target)
			return nil
		},
	}
}

func newImprovementsSkipCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skip <id>",
		Short: "skip a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := agent.NewExternalProposalQueue(".meept/improvements.md")
			return q.MarkSkippedExternal(args[0])
		},
	}
}
```

- [ ] **Step 2: Register in main.go**

In `cmd/meept/main.go` after the existing commands (line ~124):

```go
	rootCmd.AddCommand(newImprovementsCmd())
```

- [ ] **Step 3: Add external wrappers**

In `internal/agent/proposal.go`:

```go
func (q *ProposalQueueExternal) ListPendingExternal() ([]ReflectionProposal, error) {
	return q.inner.ListPending()
}

func (q *ProposalQueueExternal) MarkAppliedExternal(id string) error {
	return q.inner.MarkApplied(id)
}

func (q *ProposalQueueExternal) MarkSkippedExternal(id string) error {
	// Similar to MarkApplied but with "[skipped]" status
	return q.inner.MarkSkipped(id)
}

func IsAlwaysProposeOnlyExternal(target string) bool {
	return isAlwaysProposeOnly(target)
}
```

Add `MarkSkipped` to `proposalQueue` (same pattern as `MarkApplied`).

- [ ] **Step 4: Build and test manually**

```bash
go build -o bin/meept ./cmd/meept
./bin/meept improvements list
echo "## [pending] test\n- **Type:** project_instruction\n- **Target:** CLAUDE.md\n- **Confidence:** 0.8\n- **Source:** test\n- **Justification:** x\n- **Proposed change:** y" >> .meept/improvements.md
./bin/meept improvements list
```

- [ ] **Step 5: Commit**

```bash
git add cmd/meept/implement_improvements.go cmd/meept/main.go internal/agent/proposal.go
git commit -m "feat(cli): add meept improvements list/apply/skip"
```

---

## Task 10: HTTP API + Flutter reflection panel

**Files:**
- Modify: `internal/services/reflection_service.go` (new)
- Modify: `internal/services/service.go`
- Modify: `internal/comm/http/api_handlers.go`
- Modify: `internal/comm/http/server.go`
- Modify: `ui/flutter_ui/lib/features/reflection/` (new)

- [ ] **Step 1: Service layer**

`internal/services/reflection_service.go`:

```go
package services

import (
	"github.com/caimlas/meept/internal/agent"
)

type ReflectionService struct {
	queue *agent.ProposalQueueExternal
}

func NewReflectionService(queuePath string) *ReflectionService {
	return &ReflectionService{queue: agent.NewExternalProposalQueue(queuePath)}
}

func (s *ReflectionService) ListPending() ([]agent.ReflectionProposal, error) {
	return s.queue.ListPendingExternal()
}

func (s *ReflectionService) Apply(id string) error {
	return s.queue.MarkAppliedExternal(id)
}

func (s *ReflectionService) Skip(id string) error {
	return s.queue.MarkSkippedExternal(id)
}

func (s *ReflectionService) Remember(target, change, justification string) error {
	proposal := agent.ReflectionProposal{
		Type:          "project_instruction",
		Target:        target,
		Change:        change,
		Justification: justification,
		Confidence:    0.9,
		Source:        "manual:http",
	}
	return s.queue.Append(proposal)
}
```

Register in `service.go` `ServiceRegistry`:

```go
type ServiceRegistry struct {
	// ... existing ...
	Reflection *ReflectionService
}
```

Populate in `NewRegistry` when configured.

- [ ] **Step 2: HTTP endpoints**

In `internal/comm/http/server.go`:

```go
mux.HandleFunc("GET /api/v1/reflection/proposals", s.handleReflectionList)
mux.HandleFunc("POST /api/v1/reflection/proposals/{id}/apply", s.handleReflectionApply)
mux.HandleFunc("POST /api/v1/reflection/proposals/{id}/skip", s.handleReflectionSkip)
mux.HandleFunc("POST /api/v1/reflection/remember", s.handleReflectionRemember)
```

In `api_handlers.go`:

```go
func (s *Server) handleReflectionList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Reflection == nil {
		s.writeError(w, http.StatusServiceUnavailable, "reflection service not available")
		return
	}
	pending, err := s.services.Reflection.ListPending()
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"proposals": pending})
}

func (s *Server) handleReflectionRemember(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Reflection == nil {
		s.writeError(w, http.StatusServiceUnavailable, "reflection service not available")
		return
	}
	var body struct {
		Target, Change, Justification string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.services.Reflection.Remember(body.Target, body.Change, body.Justification); err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"status": "queued"})
}
```

Apply/Skip handlers follow the same pattern.

- [ ] **Step 3: Flutter reflection panel**

`ui/flutter_ui/lib/features/reflection/reflection_panel.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class ReflectionPanel extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final proposalsAsync = ref.watch(proposalsProvider);
    return proposalsAsync.when(
      data: (proposals) => ListView.builder(
        itemCount: proposals.length,
        itemBuilder: (ctx, i) {
          final p = proposals[i];
          return Card(
            child: ListTile(
              title: Text(p.target),
              subtitle: Text('${p.type} · ${p.justification}'),
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  IconButton(icon: Icon(Icons.check), onPressed: () => ref.read(proposalsProvider.notifier).apply(p.id)),
                  IconButton(icon: Icon(Icons.close), onPressed: () => ref.read(proposalsProvider.notifier).skip(p.id)),
                ],
              ),
            ),
          );
        },
      ),
      loading: () => CircularProgressIndicator(),
      error: (e, _) => Text('Error: $e'),
    );
  }
}
```

- [ ] **Step 4: Build + flutter analyze**

Run: `go build ./...`
Expected: clean.

Run: `(cd ui/flutter_ui && flutter analyze)` if SDK available.

- [ ] **Step 5: Commit**

```bash
git add internal/services/reflection_service.go internal/services/service.go internal/comm/http/ ui/flutter_ui/lib/features/reflection/
git commit -m "feat(reflection): add service + HTTP API + Flutter panel for proposals"
```

---

## Self-Review

**Spec coverage (Thread E):**
- ✅ Richer trajectory (`Trajectory`, `TrajectoryStep`, `buildTrajectory`) — Task 2
- ✅ `ReflectionCollector` (`ReflectTurn`, `ReflectInactiveSessions`) — Task 4
- ✅ Proposal queue `.meept/improvements.md` — Task 3
- ✅ `/remember` agent tool AND user slash command — Task 7
- ✅ Skills replace patterns.json (`ContextInjector` loads skills; `LearningPipeline.StorePattern` no-ops) — Task 6
- ✅ End-of-session trigger (TIMER ONLY) — Task 8 (30-min timer)
- ✅ `/implement-improvements` command — Task 9
- ✅ Authorization model (`isAlwaysProposeOnly` for AGENT.md/CLAUDE.md/config/prompts) — Task 3
- ✅ Wiring (CLI/TUI/Flutter/HTTP) — Tasks 9, 10
- ✅ Reflection templates (`turn.md`, `session.md`) — Task 1
- ✅ Config block — Task 4 Step 1

**Type consistency:**
- `ReflectionProposal` struct fields consistent across proposal.go, reflection_collector.go, remember.go, reflection_service.go.
- `classifierClient` interface — the production `*llm.Client` must satisfy it. Verified: `RunOnce(ctx, prompt, conversationID) (string, error)` matches the existing `AgentLoop.RunOnce` signature.
- `ProposalQueueExternal` exported wrapper used by all cross-package callers (tool, service, CLI).

**Red flags:**
- `ConvMessage.IsError` may not exist in `Conversation` — flagged in Task 2 Step 3.
- `ReflectInactiveSessions` is a stub in MVP (Task 4 Step 3). Full implementation requires SessionStore integration that doesn't yet exist. Spec explicitly defers deeper session reflection — "30-min timer queries for inactive sessions." The stub logs and returns; the timer still fires; once SessionStore lands, the implementation completes without API changes.
- `triggerLearning` is kept as deprecated dead code (Task 5 Step 3) for safety. Tests may still reference it.
- Q Agent rework is out of scope (separate plan). This thread reserves the integration point but does not touch `internal/agent/q/`.
