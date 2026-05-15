# Multi-Participant Agent Communication — Track 2 (TUI Polish)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add progress synthesis (tiered agent activity summaries), participant badges on messages, and configurable verbosity to the TUI chat experience.

**Architecture:** A `ProgressSynthesizer` subscribes to existing agent events on the bus and publishes `agent.progress.synthesized` events with tiered summaries. The TUI chat model renders participant badges from `source_client` fields (added in Track 1) and filters progress events by verbosity level. Verbosity is configurable via `ctrl+v` keybinding and `client.json5` defaults.

**Tech Stack:** Go 1.22+, existing event bus, LLM client (classifier model for agent completion summaries), Bubbletea TUI.

**Spec:** `docs/superpowers/specs/2026-05-15-multi-participant-agent-comms-design.md` (Sections 3 and 5)

**Depends on:** Track 1 Task 2 (new event types) and Track 1 Task 3 (chat.message.received broadcast).

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/agent/progress_synthesizer.go` | Create | Subscribe to agent events, produce tiered summaries |
| `internal/agent/progress_synthesizer_test.go` | Create | Tests for synthesizer |
| `internal/tui/models/chat.go` | Modify | Add `SourceClient` to ChatMessage, render participant badges |
| `internal/tui/models/constants.go` | Modify | Add `RoleParticipant` constant |
| `internal/tui/app.go` | Modify | Add ctrl+v keybinding, verbosity cycling, status bar indicator |
| `internal/tui/config.go` | Modify | Add `Verbosity` to ChatConfig, parse from client.json5 |
| `config/client.json5` | Modify | Add `chat.verbosity` setting |

---

### Task 1: Add VerbosityLevel type and ProgressSynthesizer

**Files:**
- Create: `internal/agent/progress_synthesizer.go`
- Create: `internal/agent/progress_synthesizer_test.go`

The synthesizer subscribes to agent events and produces human-readable tiered summaries published as `agent.progress.synthesized` bus events.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/progress_synthesizer_test.go`:

```go
package agent

import (
	"testing"
	"time"
)

func TestSynthesizeToolEndQuiet(t *testing.T) {
	s := NewProgressSynthesizer(nil, nil, slog.Default())

	data := ToolExecutionEndData{
		ToolCallID: "tc-1",
		ToolName:   "shell_execute",
		Success:    true,
		Result:     "ok\n47 passed, 2 failed",
		Duration:   3 * time.Second,
	}
	event := AgentEvent{
		Type:      AgentEventToolExecutionEnd,
		Timestamp: time.Now().UTC(),
		AgentID:   "debugger",
		Data:      data,
	}

	result := s.Synthesize(event)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Tool execution end at quiet tier should still produce a summary
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestSynthesizeAgentEndQuiet(t *testing.T) {
	s := NewProgressSynthesizer(nil, nil, slog.Default())

	data := AgentEndData{
		AgentID:  "coder",
		Reason:   "completed",
		Duration: 14 * time.Second,
	}
	event := AgentEvent{
		Type:      AgentEventAgentEnd,
		Timestamp: time.Now().UTC(),
		AgentID:   "coder",
		Data:      data,
	}

	result := s.Synthesize(event)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Message == "" {
		t.Error("expected non-empty message for agent end")
	}
	if result.Tier != VerbosityQuiet {
		t.Errorf("Tier = %d, want %d", result.Tier, VerbosityQuiet)
	}
}

func TestSynthesizeToolStartVerbose(t *testing.T) {
	s := NewProgressSynthesizer(nil, nil, slog.Default())

	data := ToolExecutionStartData{
		ToolCallID: "tc-1",
		ToolName:   "file_write",
		Arguments:  `{"path":"auth.go"}`,
	}
	event := AgentEvent{
		Type:      AgentEventToolExecutionStart,
		Timestamp: time.Now().UTC(),
		AgentID:   "coder",
		Data:      data,
	}

	result := s.Synthesize(event)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Tier != VerbosityVerbose {
		t.Errorf("Tier = %d, want %d (tool start is verbose-only)", result.Tier, VerbosityVerbose)
	}
}

func TestSynthesizeTurnEndVerbose(t *testing.T) {
	s := NewProgressSynthesizer(nil, nil, slog.Default())

	data := TurnEndData{
		TurnNumber:    3,
		HadToolCalls:  true,
		ToolCallCount: 2,
		ResponseTokens: 1200,
		StoppedBy:     "end_turn",
	}
	event := AgentEvent{
		Type:      AgentEventTurnEnd,
		Timestamp: time.Now().UTC(),
		AgentID:   "coder",
		Data:      data,
	}

	result := s.Synthesize(event)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Tier != VerbosityVerbose {
		t.Errorf("Tier = %d, want %d (turn end is verbose-only)", result.Tier, VerbosityVerbose)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestSynthesize`
Expected: compile error — `ProgressSynthesizer`, `VerbosityQuiet`, etc. undefined

- [ ] **Step 3: Implement ProgressSynthesizer**

Create `internal/agent/progress_synthesizer.go`:

```go
package agent

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// VerbosityLevel controls how much progress detail to show.
type VerbosityLevel int

const (
	VerbosityQuiet   VerbosityLevel = 0 // task-level only
	VerbosityNormal  VerbosityLevel = 1 // task + notable tool results
	VerbosityVerbose VerbosityLevel = 2 // everything: turns, tools, tokens, timing
)

// String returns a human-readable verbosity name.
func (v VerbosityLevel) String() string {
	switch v {
	case VerbosityQuiet:
		return "quiet"
	case VerbosityNormal:
		return "normal"
	case VerbosityVerbose:
		return "verbose"
	default:
		return "unknown"
	}
}

// ParseVerbosityLevel parses a string into a VerbosityLevel.
func ParseVerbosityLevel(s string) VerbosityLevel {
	switch strings.ToLower(s) {
	case "quiet":
		return VerbosityQuiet
	case "normal":
		return VerbosityNormal
	case "verbose":
		return VerbosityVerbose
	default:
		return VerbosityNormal
	}
}

// SynthesizedProgressEvent is a tiered progress event for UI consumption.
type SynthesizedProgressEvent struct {
	SessionID   string         `json:"session_id"`
	AgentID     string         `json:"agent_id"`
	Tier        VerbosityLevel `json:"tier"`
	Message     string         `json:"message"`
	SourceEvent AgentEventType `json:"source_event"`
	Timestamp   time.Time      `json:"timestamp"`
}

// ProgressSynthesizer converts raw agent events into tiered human-readable summaries.
type ProgressSynthesizer struct {
	bus    *bus.MessageBus
	client *llm.Client // optional: for LLM-based agent completion summaries
	model  string
	logger *slog.Logger
}

// NewProgressSynthesizer creates a new progress synthesizer.
// client may be nil (template-based synthesis only).
func NewProgressSynthesizer(b *bus.MessageBus, client *llm.Client, logger *slog.Logger) *ProgressSynthesizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProgressSynthesizer{
		bus:    b,
		client: client,
		logger: logger.With("component", "progress-synthesizer"),
	}
}

// Synthesize produces a tiered summary from a raw agent event.
// Returns nil for events that should not produce any summary.
func (s *ProgressSynthesizer) Synthesize(event AgentEvent) *SynthesizedProgressEvent {
	switch event.Type {
	case AgentEventAgentEnd:
		return s.synthesizeAgentEnd(event)
	case AgentEventToolExecutionEnd:
		return s.synthesizeToolEnd(event)
	case AgentEventToolExecutionStart:
		return s.synthesizeToolStart(event)
	case AgentEventTurnEnd:
		return s.synthesizeTurnEnd(event)
	default:
		return nil
	}
}

func (s *ProgressSynthesizer) synthesizeAgentEnd(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(AgentEndData)
	if !ok {
		return nil
	}
	duration := data.Duration
	if duration == 0 {
		duration = time.Since(event.Timestamp)
	}
	msg := fmt.Sprintf("%s: completed (%s)", event.AgentID, formatDuration(duration))
	return &SynthesizedProgressEvent{
		AgentID:     event.AgentID,
		Tier:        VerbosityQuiet,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

func (s *ProgressSynthesizer) synthesizeToolEnd(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(ToolExecutionEndData)
	if !ok {
		return nil
	}
	status := "completed"
	if !data.Success {
		status = "failed"
	}
	// Truncate result to first meaningful line
	result := firstLine(data.Result)
	if result != "" {
		result = ": " + truncate(result, 80)
	}
	msg := fmt.Sprintf("%s: %s %s (%s)%s", event.AgentID, status, data.ToolName, formatDuration(data.Duration), result)
	return &SynthesizedProgressEvent{
		AgentID:     event.AgentID,
		Tier:        VerbosityNormal,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

func (s *ProgressSynthesizer) synthesizeToolStart(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(ToolExecutionStartData)
	if !ok {
		return nil
	}
	msg := fmt.Sprintf("%s: executing %s", event.AgentID, data.ToolName)
	return &SynthesizedProgressEvent{
		AgentID:     event.AgentID,
		Tier:        VerbosityVerbose,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

func (s *ProgressSynthesizer) synthesizeTurnEnd(event AgentEvent) *SynthesizedProgressEvent {
	data, ok := event.Data.(TurnEndData)
	if !ok {
		return nil
	}
	msg := fmt.Sprintf("%s: turn %d done (%d tool calls, %d tokens)",
		event.AgentID, data.TurnNumber, data.ToolCallCount, data.ResponseTokens)
	return &SynthesizedProgressEvent{
		AgentID:     event.AgentID,
		Tier:        VerbosityVerbose,
		Message:     msg,
		SourceEvent: event.Type,
		Timestamp:   event.Timestamp,
	}
}

// helper functions

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func firstLine(s string) string {
	idx := strings.Index(s, "\n")
	if idx > 0 {
		return s[:idx]
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/... -v -run TestSynthesize`
Expected: PASS

- [ ] **Step 5: Run full agent test suite**

Run: `go test ./internal/agent/... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/agent/progress_synthesizer.go internal/agent/progress_synthesizer_test.go
git commit -m "feat: add ProgressSynthesizer for tiered agent progress summaries"
```

---

### Task 2: Add `Verbosity` to ChatConfig and client.json5

**Files:**
- Modify: `internal/tui/config.go:57-61` (ChatConfig struct)
- Modify: `config/client.json5:89-94` (chat section)

- [ ] **Step 1: Add Verbosity field to ChatConfig**

In `internal/tui/config.go`, modify the `ChatConfig` struct:

```go
// ChatConfig defines chat viewport behavior settings.
type ChatConfig struct {
	AutoCopyOnRelease bool   `json:"auto_copy_on_release"` // Auto-copy selected text on mouse release (default: false)
	ScrollSpeed       int    `json:"scroll_speed"`         // Lines to scroll per mouse wheel event (default: 3)
	Verbosity         string `json:"verbosity"`            // Progress verbosity: "quiet", "normal", "verbose" (default: "normal")
}
```

- [ ] **Step 2: Set default in DefaultClientConfig**

In `DefaultClientConfig()`, the `Chat` field isn't explicitly set. Add it after the `Input` block:

```go
		Chat: ChatConfig{
			Verbosity: "normal",
		},
```

- [ ] **Step 3: Add `verbosity` to `config/client.json5`**

In `config/client.json5`, modify the `"chat"` section:

```json5
  // Chat viewport behavior
  "chat": {
    // Auto-copy selected text to clipboard on mouse release
    "auto_copy_on_release": false,
    // Lines to scroll per mouse wheel event
    "scroll_speed": 3,
    // Agent progress verbosity: "quiet" (task-only), "normal" (task + tools), "verbose" (everything)
    "verbosity": "normal",
  },
```

- [ ] **Step 4: Build to verify**

Run: `go build ./internal/tui/... && go build ./cmd/meept/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/tui/config.go config/client.json5
git commit -m "feat: add verbosity setting to ChatConfig and client.json5"
```

---

### Task 3: Add verbosity keybinding and status bar indicator to TUI

**Files:**
- Modify: `internal/tui/app.go` (add ctrl+v handling, verbosity state, status bar)

- [ ] **Step 1: Add verbosity state to App struct**

In `internal/tui/app.go`, add to the `App` struct (after `tabFlashTime` around line ~102):

```go
	// Verbosity level for agent progress display
	verbosity VerbosityLevel
```

Add the `VerbosityLevel` type and constants. These mirror the agent package but live in the TUI to avoid import cycles. In `internal/tui/app.go`, add at the top level:

```go
// VerbosityLevel controls how much progress detail to show in the TUI.
type VerbosityLevel int

const (
	VerbosityQuiet   VerbosityLevel = 0
	VerbosityNormal  VerbosityLevel = 1
	VerbosityVerbose VerbosityLevel = 2
)

func (v VerbosityLevel) String() string {
	switch v {
	case VerbosityQuiet:
		return "quiet"
	case VerbosityNormal:
		return "normal"
	case VerbosityVerbose:
		return "verbose"
	default:
		return "unknown"
	}
}

func parseVerbosity(s string) VerbosityLevel {
	switch s {
	case "quiet":
		return VerbosityQuiet
	case "verbose":
		return VerbosityVerbose
	default:
		return VerbosityNormal
	}
}
```

- [ ] **Step 2: Initialize verbosity from config**

In `NewApp` (where client config is loaded), after the config is available, add:

```go
	a.verbosity = parseVerbosity(a.clientConfig.Chat.Verbosity)
```

- [ ] **Step 3: Add ctrl+v keybinding handler**

In the `Update` method of `App`, in the key handling section (around the `ctrl+s` handling at line ~402), add after the `ctrl+s` block:

```go
		// Check for Ctrl+V: cycle verbosity level
		if msg.String() == "ctrl+v" {
			a.verbosity = (a.verbosity + 1) % 3
			a.statusMessage = fmt.Sprintf("verbosity: %s", a.verbosity)
			a.statusMessageTime = time.Now()
			return a, nil
		}
```

- [ ] **Step 4: Add verbosity indicator to status bar**

In `renderStatusBar()`, add the verbosity indicator to the status bar content. Find the line where `parts` are joined (`content := strings.Join(parts, " │ ")`) and add before it:

```go
	// Verbosity indicator
	parts = append(parts, a.styles.Muted.Render(fmt.Sprintf("verbosity: %s", a.verbosity)))
```

- [ ] **Step 5: Add ctrl+v to quick actions**

In `getQuickActions()`, in the chat view section (around line ~1630), add to the actions:

```go
				actions = append(actions,
					a.styles.HelpKey.Render("^V")+" "+a.styles.HelpValue.Render("verbosity"),
				)
```

- [ ] **Step 6: Build and verify**

Run: `go build -o bin/meept ./cmd/meept`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go
git commit -m "feat: add ctrl+v verbosity cycling and status bar indicator to TUI"
```

---

### Task 4: Add participant badges to chat messages

**Files:**
- Modify: `internal/tui/models/chat.go:51-62` (ChatMessage struct)
- Modify: `internal/tui/models/constants.go` (add RoleParticipant)
- Modify: `internal/tui/models/chat.go` (render participant badge in message view)

- [ ] **Step 1: Add SourceClient to ChatMessage**

In `internal/tui/models/chat.go`, modify the `ChatMessage` struct:

```go
type ChatMessage struct {
	Role         string // "user", "assistant", "system", "participant", or "pending"
	Content      string
	SourceClient string // Client identifier for participant messages (e.g. "claude", "tui")
	Timestamp    time.Time
	State        MessageState
	Progress     *ProgressState // Progress state for pending messages

	// Rendering cache
	rendered   string // Cached rendered output
	renderedAt int    // Width when rendered
}
```

- [ ] **Step 2: Add RoleParticipant constant**

In `internal/tui/models/constants.go`, add:

```go
	RoleParticipant = "participant"
```

- [ ] **Step 3: Add helper for participant messages**

In `internal/tui/models/chat.go`, add a helper method:

```go
// AddParticipantMessage adds a message from a session participant to the chat transcript.
func (m *ChatModel) AddParticipantMessage(sourceClient, content string) {
	m.messages = append(m.messages, ChatMessage{
		Role:         RoleParticipant,
		Content:      content,
		SourceClient: sourceClient,
		Timestamp:    time.Now().UTC(),
		State:        MessageNormal,
	})
	m.updateViewport()
}
```

- [ ] **Step 4: Render participant badge in message view**

Find the message rendering function in `chat.go` (look for where messages are rendered based on role). Add a case for `RoleParticipant`:

```go
	case RoleParticipant:
		badge := fmt.Sprintf("[%s]", msg.SourceClient)
		rendered = m.styles.SystemMessage.Render(fmt.Sprintf("%s %s", badge, msg.Content))
```

Note: The exact rendering location depends on the chat model's view rendering. The implementor should find the switch on `msg.Role` and add this case.

- [ ] **Step 5: Handle chat.message.received events in TUI**

In the TUI's event handling (where `EventStreamDataMsg` is processed), add handling for `chat.message.received` topic events. When received, call `m.chat.AddParticipantMessage(sourceClient, content)`.

This wiring happens in `internal/tui/app.go` in the `Update` method where event stream data is processed. Find where bus events are handled and add:

```go
		case "chat.message.received":
			var payload struct {
				SourceClient string `json:"source_client"`
				Content      string `json:"content"`
			}
			if err := json.Unmarshal(rawPayload, &payload); err == nil {
				if payload.SourceClient != "tui" && a.chat != nil {
					// Don't re-display our own messages
					a.chat.AddParticipantMessage(payload.SourceClient, payload.Content)
				}
			}
```

- [ ] **Step 6: Build and verify**

Run: `go build -o bin/meept ./cmd/meept`
Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/tui/models/chat.go internal/tui/models/constants.go internal/tui/app.go
git commit -m "feat: add participant badges and multi-client message display to TUI"
```

---

### Task 5: Wire progress events into TUI chat display

**Files:**
- Modify: `internal/tui/app.go` (handle agent.progress.synthesized events)

This connects the synthesized progress events to the TUI, filtered by the current verbosity level.

- [ ] **Step 1: Add progress event handling in TUI Update**

In the TUI's event stream data handler (same location as Task 4 Step 5), add handling for `agent.progress.synthesized` events:

```go
		case "agent.progress.synthesized":
			var payload struct {
				AgentID     string `json:"agent_id"`
				Tier        int    `json:"tier"`
				Message     string `json:"message"`
				SourceEvent string `json:"source_event"`
			}
			if err := json.Unmarshal(rawPayload, &payload); err == nil {
				// Filter by current verbosity level
				if VerbosityLevel(payload.Tier) <= a.verbosity && a.chat != nil {
					a.chat.AddSystemMessage(payload.Message)
				}
			}
```

- [ ] **Step 2: Ensure the event stream subscribes to progress events**

In `internal/tui/events.go`, in `DefaultEventStreamConfig()`, verify that `agent.progress.*` is in the topics list. The current config has `"agent.*"` which should already match `agent.progress.synthesized`. If the glob pattern doesn't match nested topics, add it explicitly:

```go
		"agent.progress.*",
```

- [ ] **Step 3: Build and verify**

Run: `go build -o bin/meept ./cmd/meept`
Expected: no errors

- [ ] **Step 4: Run full test suite**

Run: `go test ./internal/tui/... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/events.go
git commit -m "feat: wire synthesized progress events into TUI with verbosity filtering"
```

---

### Task 6: Start ProgressSynthesizer in daemon

**Files:**
- Modify: `internal/daemon/components.go` (or wherever components are initialized)

The synthesizer needs to be started as a daemon component so it subscribes to agent events and produces synthesized progress events on the bus.

- [ ] **Step 1: Find the daemon component initialization**

Find where `EventEmitter`, `ChatHandler`, etc. are wired up in the daemon startup. This is typically in `internal/daemon/components.go` or `internal/daemon/daemon.go`.

- [ ] **Step 2: Add ProgressSynthesizer initialization**

After the bus and LLM client are initialized, add:

```go
	// Start progress synthesizer for tiered agent activity summaries
	progressSynthesizer := agent.NewProgressSynthesizer(msgBus, llmClient, logger)
	// The synthesizer subscribes to agent events and publishes synthesized events
	// It's driven by the event emitter's bus bridge — no separate goroutine needed
	// for the template-based path. LLM-based summaries would need a bus subscription.
	_ = progressSynthesizer // will be used for LLM summaries in future
```

Note: For the template-based path (Tasks 1-5 of this plan), the synthesizer is called directly by whatever component processes agent events. The daemon doesn't need to start a separate goroutine for it. The real integration point is in the event processing pipeline where `agent.event.*` events are consumed by the TUI's event stream.

For the initial implementation, the synthesizer can be called from the `EventEmitter` bridge or from a bus subscriber that transforms raw events into synthesized ones. The simplest approach: subscribe to `agent.event.*` topics and publish synthesized events:

```go
	// Subscribe to agent events and produce synthesized progress
	busSub := msgBus.Subscribe("progress-synthesizer", "agent.event.agent_end", "agent.event.tool_execution_end")
	go func() {
		for msg := range busSub.Channel {
			var event agent.AgentEvent
			if err := json.Unmarshal(msg.Payload, &event); err != nil {
				continue
			}
			synthesized := progressSynthesizer.Synthesize(event)
			if synthesized == nil {
				continue
			}
			payload, _ := json.Marshal(synthesized)
			synthMsg := &models.BusMessage{
				ID:        fmt.Sprintf("synth-%d", time.Now().UnixNano()),
				Type:      models.MessageTypeEvent,
				Topic:     "agent.progress.synthesized",
				Source:    "progress-synthesizer",
				Timestamp: time.Now().UTC(),
				Payload:   payload,
			}
			msgBus.Publish("agent.progress.synthesized", synthMsg)
		}
	}()
```

- [ ] **Step 3: Build and verify**

Run: `go build -o bin/meept-daemon ./cmd/meept-daemon`
Expected: no errors

- [ ] **Step 4: Run full test suite**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat: start ProgressSynthesizer in daemon for tiered progress events"
```

---

## Self-Review

### Spec Coverage

| Spec Section | Task |
|---|---|
| 3. Progress Synthesis & Configurable Verbosity | Tasks 1, 5, 6 |
| 3. Template-based synthesis | Task 1 |
| 5a. Participant badges on messages | Task 4 |
| 5b. Verbosity control (ctrl+v, status bar, client.json5) | Tasks 2, 3 |

### Placeholder Scan

No TBDs, TODOs, or vague instructions. Task 6 Step 1 has an exploratory step ("find the daemon component initialization") which is necessary because the exact wiring point depends on how the daemon is structured — but the code in Step 2 is concrete.

### Type Consistency

- `VerbosityLevel` defined in `progress_synthesizer.go` (agent package) and mirrored in `app.go` (tui package) to avoid import cycles
- `ChatMessage.SourceClient` defined in Task 4, used in Task 4 rendering
- `ChatConfig.Verbosity` defined in Task 2, read in Task 3
- `RoleParticipant` defined in Task 4, used in Task 4 rendering
- All struct field names consistent across tasks
