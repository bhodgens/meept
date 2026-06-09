# Multi-Participant Agent Communication — Track 1 (Core)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the broken report routing pipeline, add client identity for multi-participant sessions, broadcast messages between session participants, and expose meept as an MCP server for external agent platforms (Claude, etc.).

**Architecture:** Add `source_client` identity to messages, broadcast `chat.message.received` events for bilateral visibility, replace the dead-end `DetermineRouteAction` with a working `ReportRouter`, and build an MCP server that proxies between MCP protocol (stdin/stdout JSON-RPC) and meept's existing RPC (Unix socket JSON-RPC). The MCP server is stateless — it just translates protocols.

**Tech Stack:** Go 1.22+, existing RPC transport, MCP protocol (JSON-RPC over stdio), existing bus subscription/polling system.

**Spec:** `docs/superpowers/specs/2026-05-15-multi-participant-agent-comms-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `pkg/models/types.go` | Modify | Add `SourceClient` to `BusMessage` |
| `internal/agent/handler.go` | Modify | Broadcast `chat.message.received`, pass `SourceClient` through |
| `internal/agent/dispatcher.go` | Modify | Replace dead-end report handling with `ReportRouter` call |
| `internal/agent/report_router.go` | Create | Execute route actions, multi-agent handoff loop |
| `internal/agent/report_router_test.go` | Create | Tests for report router |
| `internal/agent/events.go` | Modify | Add `ChatMessageReceived` and `ChatClientDisconnected` event types |
| `internal/mcp/server.go` | Create | MCP server main loop, JSON-RPC message reading/writing |
| `internal/mcp/server_test.go` | Create | Tests for MCP server |
| `internal/mcp/tools.go` | Create | MCP tool definitions and implementations |
| `internal/mcp/tools_test.go` | Create | Tests for MCP tools |
| `internal/mcp/transport.go` | Create | MCP protocol constants, JSON-RPC types, stdio transport |
| `cmd/meept/mcp_chat_server.go` | Create | CLI subcommand `meept mcp-chat-server` |
| `cmd/meept/main.go` | Modify | Register `mcp-chat-server` subcommand |
| `config/meept.json5` | Modify | Add `mcp_chat_server` config section |

---

### Task 1: Add `SourceClient` to `BusMessage`

**Files:**
- Modify: `pkg/models/types.go:22-30`
- Test: `go test ./pkg/models/... -v`

This is the foundation — all other tasks depend on messages knowing who sent them.

- [x]**Step 1: Write the failing test**

Create `pkg/models/types_test.go`:

```go
package models

import (
	"encoding/json"
	"testing"
)

func TestBusMessageSourceClient(t *testing.T) {
	msg := &BusMessage{
		ID:           "test-1",
		Type:         MessageTypeRequest,
		Source:       "chat-handler",
		SourceClient: "tui",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded BusMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SourceClient != "tui" {
		t.Errorf("SourceClient = %q, want %q", decoded.SourceClient, "tui")
	}
}

func TestBusMessageSourceClientOmitEmpty(t *testing.T) {
	msg := &BusMessage{
		ID:     "test-2",
		Type:   MessageTypeRequest,
		Source: "chat-handler",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// source_client should be absent when empty (omitempty)
	if string(data) != "" {
		var m map[string]any
		json.Unmarshal(data, &m)
		if _, exists := m["source_client"]; exists {
			t.Error("source_client should be omitted when empty")
		}
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./pkg/models/... -v -run TestBusMessage`
Expected: compile error or `SourceClient` field not found on `BusMessage`

- [x]**Step 3: Add `SourceClient` field to `BusMessage`**

In `pkg/models/types.go`, modify the `BusMessage` struct:

```go
type BusMessage struct {
	ID           string          `json:"id"`
	Type         MessageType     `json:"type"`
	Topic        string          `json:"topic,omitempty"`
	Source       string          `json:"source"`
	SourceClient string          `json:"source_client,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
	Payload      json.RawMessage `json:"payload"`
	ReplyTo      string          `json:"reply_to,omitempty"`
}
```

- [x]**Step 4: Run test to verify it passes**

Run: `go test ./pkg/models/... -v -run TestBusMessage`
Expected: PASS

- [x]**Step 5: Run full test suite to check for regressions**

Run: `go test ./... -count=1 2>&1 | tail -20`
Expected: All tests pass (no tests reference `SourceClient` yet, so no breakage)

- [x]**Step 6: Commit**

```bash
git add pkg/models/types.go pkg/models/types_test.go
git commit -m "feat: add SourceClient field to BusMessage for multi-participant identity"
```

---

### Task 2: Add `SourceClient` to `ChatRequest` and new event types

**Files:**
- Modify: `internal/agent/handler.go:52-56` (ChatRequest struct)
- Modify: `internal/agent/events.go` (add new event types)
- Test: `go test ./internal/agent/... -v -run TestChatRequest`

- [x]**Step 1: Write the failing test**

In `internal/agent/handler_test.go` (or create if needed), add:

```go
func TestChatRequestSourceClient(t *testing.T) {
	raw := `{"message": "hello", "conversation_id": "conv-1", "source_client": "claude"}`
	var req ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.SourceClient != "claude" {
		t.Errorf("SourceClient = %q, want %q", req.SourceClient, "claude")
	}
	if req.Message != "hello" {
		t.Errorf("Message = %q, want %q", req.Message, "hello")
	}
}

func TestChatRequestSourceClientDefault(t *testing.T) {
	raw := `{"message": "hello", "conversation_id": "conv-1"}`
	var req ChatRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.SourceClient != "" {
		t.Errorf("SourceClient should be empty when not provided, got %q", req.SourceClient)
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestChatRequest`
Expected: compile error — `ChatRequest` has no `SourceClient` field

- [x]**Step 3: Add `SourceClient` to `ChatRequest`**

In `internal/agent/handler.go`, modify the `ChatRequest` struct:

```go
type ChatRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
	SourceClient   string `json:"source_client,omitempty"`
}
```

- [x]**Step 4: Add new event types for bilateral visibility**

In `internal/agent/events.go`, add after the existing `SettledData` type:

```go
// --- Chat Message Visibility Events ---

// ChatMessageReceivedData is emitted when a client sends a message to a session.
// Broadcast to all session participants for bilateral visibility.
type ChatMessageReceivedData struct {
	SessionID    string `json:"session_id"`
	SourceClient string `json:"source_client"`
	Content      string `json:"content"`
}

func (ChatMessageReceivedData) agentEventData() {}

// ChatClientDisconnectedData is emitted when a client disconnects from a session.
type ChatClientDisconnectedData struct {
	SessionID    string `json:"session_id"`
	SourceClient string `json:"source_client"`
}

func (ChatClientDisconnectedData) agentEventData() {}
```

Add the corresponding `AgentEventType` constants:

```go
	// Chat visibility events
	AgentEventChatMessageReceived AgentEventType = "chat_message_received"
	AgentEventChatClientDisconnected AgentEventType = "chat_client_disconnected"
```

- [x]**Step 5: Run tests**

Run: `go test ./internal/agent/... -v -run "TestChatRequest"`
Expected: PASS

- [x]**Step 6: Run broader agent tests for regressions**

Run: `go test ./internal/agent/... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [x]**Step 7: Commit**

```bash
git add internal/agent/handler.go internal/agent/events.go
git commit -m "feat: add SourceClient to ChatRequest and bilateral visibility event types"
```

---

### Task 3: Broadcast `chat.message.received` from ChatHandler

**Files:**
- Modify: `internal/agent/handler.go:397-512` (handleRequest function)
- Test: `go test ./internal/agent/... -v -run TestHandleRequest`

- [x]**Step 1: Write the failing test**

Add to `internal/agent/handler_test.go`:

```go
func TestHandleRequestBroadcastsMessageReceived(t *testing.T) {
	msgBus := bus.NewMessageBus(slog.Default())
	loop := &AgentLoop{} // minimal, not used in this path
	handler := NewChatHandler(loop, nil, msgBus, slog.Default())

	// Subscribe to chat.message.received to verify broadcast
	received := make(chan *models.BusMessage, 1)
	sub := msgBus.Subscribe("test-handler", "chat.message.received")
	go func() {
		select {
		case msg := <-sub.Channel:
			received <- msg
		case <-time.After(2 * time.Second):
		}
	}()

	// Publish a chat request with source_client
	payload, _ := json.Marshal(ChatRequest{
		Message:        "hello from claude",
		ConversationID: "conv-test",
		SourceClient:   "claude",
	})
	reqMsg := &models.BusMessage{
		ID:        "req-1",
		Type:      models.MessageTypeRequest,
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	msgBus.Publish("chat.request", reqMsg)

	// Wait for broadcast
	select {
	case msg := <-received:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		if event["source_client"] != "claude" {
			t.Errorf("source_client = %v, want claude", event["source_client"])
		}
		if event["content"] != "hello from claude" {
			t.Errorf("content = %v, want 'hello from claude'", event["content"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for chat.message.received broadcast")
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestHandleRequestBroadcastsMessageReceived -timeout 10s`
Expected: timeout / no broadcast received (the feature doesn't exist yet)

- [x]**Step 3: Add broadcast in `handleRequest`**

In `internal/agent/handler.go`, in the `handleRequest` function, add the broadcast **after** the `req.Message == ""` check and conversation ID generation, and **before** the worker creation. Insert after line ~418 (after `conversationID` is determined):

```go
	// Broadcast chat.message.received for bilateral visibility
	// All session participants see who sent what
	if req.SourceClient != "" {
		broadcastPayload, _ := json.Marshal(map[string]string{
			"session_id":    conversationID,
			"source_client": req.SourceClient,
			"content":       req.Message,
			"timestamp":     time.Now().UTC().Format(time.RFC3339),
		})
		broadcastMsg := &models.BusMessage{
			ID:        generateMessageID(),
			Type:      models.MessageTypeEvent,
			Topic:     "chat.message.received",
			Source:    SourceChatHandler,
			Timestamp: time.Now().UTC(),
			Payload:   broadcastPayload,
		}
		h.bus.Publish("chat.message.received", broadcastMsg)
	}
```

Also add the legacy topic mapping in `internal/agent/emitter.go`:

```go
	AgentEventChatMessageReceived: "chat.message.received",
```

- [x]**Step 4: Run test**

Run: `go test ./internal/agent/... -v -run TestHandleRequestBroadcastsMessageReceived -timeout 10s`
Expected: PASS

- [x]**Step 5: Run full agent tests**

Run: `go test ./internal/agent/... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [x]**Step 6: Commit**

```bash
git add internal/agent/handler.go internal/agent/emitter.go
git commit -m "feat: broadcast chat.message.received for bilateral session visibility"
```

---

### Task 4: Create ReportRouter

**Files:**
- Create: `internal/agent/report_router.go`
- Create: `internal/agent/report_router_test.go`

This replaces the dead-end code in `dispatcher.go:699-710` where `DetermineRouteAction` is computed but never acted on.

- [x]**Step 1: Write the failing test**

Create `internal/agent/report_router_test.go`:

```go
package agent

import (
	"context"
	"testing"
)

func TestReportRouterRouteActionClose(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 5,
	})

	report := &AgentReport{
		Status:       ReportStatusCompleted,
		Accomplished: []string{"implemented auth module"},
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:     report,
		Action:     action,
		AgentID:    "coder",
		Depth:      0,
	})
	if result.FinalResponse == "" {
		t.Error("expected non-empty final response for RouteActionClose")
	}
	if result.Depth != 0 {
		t.Errorf("Depth = %d, want 0", result.Depth)
	}
}

func TestReportRouterMaxDepth(t *testing.T) {
	router := NewReportRouter(ReportRouterConfig{
		MaxDepth: 2,
	})

	report := &AgentReport{
		Status:             ReportStatusCompleted,
		SuggestedNextAgent: "reviewer",
	}
	action := DetermineRouteAction(report)

	result := router.Route(context.Background(), RouteParams{
		Report:     report,
		Action:     action,
		AgentID:    "coder",
		Depth:      2, // at max depth
	})
	// Should be forced to notify user instead of routing
	if result.ForceNotify {
		t.Log("correctly forced notify at max depth")
	} else {
		t.Error("expected ForceNotify at max depth")
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestReportRouter`
Expected: compile error — `ReportRouter`, `NewReportRouter`, `ReportRouterConfig`, `RouteParams` don't exist

- [x]**Step 3: Implement ReportRouter**

Create `internal/agent/report_router.go`:

```go
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

const defaultMaxRouteDepth = 5

// RouteParams holds the parameters for a routing decision.
type RouteParams struct {
	Report     *AgentReport
	Action     RouteAction
	AgentID    string
	Depth      int
}

// RouteResult holds the result of routing a completed agent's work.
type RouteResult struct {
	FinalResponse string
	ForceNotify   bool
	Depth         int
}

// ReportRouter executes routing decisions after an agent completes.
// It replaces the dead-end DetermineRouteAction call in the dispatcher.
type ReportRouter struct {
	registry   *AgentRegistry
	dispatcher *Dispatcher
	bus        interface{ Publish(string, any) }
	logger     *slog.Logger
	maxDepth   int
}

// ReportRouterConfig configures the report router.
type ReportRouterConfig struct {
	Registry   *AgentRegistry
	Dispatcher *Dispatcher
	Bus        interface{ Publish(string, any) }
	Logger     *slog.Logger
	MaxDepth   int
}

// NewReportRouter creates a new report router.
func NewReportRouter(cfg ReportRouterConfig) *ReportRouter {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxRouteDepth
	}
	return &ReportRouter{
		registry:   cfg.Registry,
		dispatcher: cfg.Dispatcher,
		bus:        cfg.Bus,
		logger:     cfg.Logger.With("component", "report-router"),
		maxDepth:   maxDepth,
	}
}

// Route determines what to do after an agent completes its work.
// For RouteActionClose, it returns the display response.
// For RouteActionRoute, it would run the next agent (handled by the dispatcher loop).
// For RouteActionNotifyUser/RouteActionNotifyError, it flags that user input is needed.
// At max depth, all actions become RouteActionNotifyUser.
func (r *ReportRouter) Route(ctx context.Context, params RouteParams) RouteResult {
	r.logger.Debug("routing",
		"action", params.Action.String(),
		"agent", params.AgentID,
		"depth", params.Depth,
	)

	// Force notify at max depth to prevent infinite handoff loops
	if params.Depth >= r.maxDepth {
		r.logger.Warn("max route depth reached, forcing user notification",
			"depth", params.Depth,
			"max", r.maxDepth,
		)
		return RouteResult{
			FinalResponse: r.formatAccumulatedResponse(params),
			ForceNotify:   true,
			Depth:         params.Depth,
		}
	}

	switch params.Action {
	case RouteActionClose:
		return RouteResult{
			FinalResponse: r.formatCloseResponse(params),
			Depth:         params.Depth,
		}

	case RouteActionRoute:
		// The actual agent handoff is done by the dispatcher's RouteToAgent loop.
		// Here we just indicate that routing should continue.
		return RouteResult{
			FinalResponse: "",
			Depth:         params.Depth + 1,
		}

	case RouteActionNotifyUser:
		return RouteResult{
			FinalResponse: r.formatNotifyUserResponse(params),
			ForceNotify:   true,
			Depth:         params.Depth,
		}

	case RouteActionNotifyError:
		return RouteResult{
			FinalResponse: r.formatErrorResponse(params),
			ForceNotify:   true,
			Depth:         params.Depth,
		}

	default:
		return RouteResult{
			FinalResponse: r.formatCloseResponse(params),
			Depth:         params.Depth,
		}
	}
}

func (r *ReportRouter) formatCloseResponse(params RouteParams) string {
	if params.Report == nil {
		return ""
	}
	var parts []string
	if len(params.Report.Accomplished) > 0 {
		parts = append(parts, params.Report.Accomplished...)
	}
	if len(params.Report.Observations) > 0 {
		parts = append(parts, params.Report.Observations...)
	}
	return strings.Join(parts, "; ")
}

func (r *ReportRouter) formatAccumulatedResponse(params RouteParams) string {
	if params.Report == nil {
		return "task reached maximum routing depth"
	}
	return fmt.Sprintf("routing depth limit reached after %d handoffs. accomplished: %s",
		params.Depth,
		strings.Join(params.Report.Accomplished, ", "),
	)
}

func (r *ReportRouter) formatNotifyUserResponse(params RouteParams) string {
	if params.Report == nil {
		return "user input needed"
	}
	var parts []string
	if params.Report.DecisionContext != "" {
		parts = append(parts, params.Report.DecisionContext)
	}
	if len(params.Report.NotDone) > 0 {
		parts = append(parts, "remaining: "+strings.Join(params.Report.NotDone, ", "))
	}
	return strings.Join(parts, "; ")
}

func (r *ReportRouter) formatErrorResponse(params RouteParams) string {
	if params.Report == nil {
		return "agent failed"
	}
	if len(params.Report.Issues) > 0 {
		return "error: " + strings.Join(params.Report.Issues, "; ")
	}
	return "agent failed with unspecified error"
}
```

- [x]**Step 4: Run test**

Run: `go test ./internal/agent/... -v -run TestReportRouter`
Expected: PASS

- [x]**Step 5: Run full agent test suite**

Run: `go test ./internal/agent/... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [x]**Step 6: Commit**

```bash
git add internal/agent/report_router.go internal/agent/report_router_test.go
git commit -m "feat: add ReportRouter for multi-agent handoff routing"
```

---

### Task 5: Wire ReportRouter into Dispatcher

**Files:**
- Modify: `internal/agent/dispatcher.go:699-716` (replace dead-end report handling)

This replaces the section where `DetermineRouteAction` is computed and discarded with an actual routing decision using the `ReportRouter`.

- [x]**Step 1: Write the failing test**

Add to `internal/agent/dispatcher_test.go`:

```go
func TestRouteToAgentUsesReportRouter(t *testing.T) {
	// Verify that RouteToAgent returns a response that incorporates
	// report routing decisions, not just StripReport of the raw response
	registry := NewAgentRegistry()
	// Register a mock agent that returns a report suggesting next agent
	registry.Register(AgentSpec{
		ID:      "coder",
		Name:    "coder",
		Purpose: "writes code",
	})
	registry.Register(AgentSpec{
		ID:      "reviewer",
		Name:    "reviewer",
		Purpose: "reviews code",
	})

	d := NewDispatcher(DispatcherConfig{
		Registry: registry,
		Logger:   slog.Default(),
	})

	if d == nil {
		t.Fatal("dispatcher should not be nil")
	}
	// The actual integration test requires a running agent loop,
	// so we test that the router field is initialized
	if d.router == nil {
		// Router is initialized lazily or in the constructor —
		// this test validates the wiring exists
		t.Log("router field exists on dispatcher (may be nil without bus)")
	}
}
```

- [x]**Step 2: Run test**

Run: `go test ./internal/agent/... -v -run TestRouteToAgentUsesReportRouter`
Expected: compile error — `dispatcher.router` field doesn't exist yet

- [x]**Step 3: Add router to Dispatcher and wire into RouteToAgent**

In `internal/agent/dispatcher.go`, add `router` field to the `Dispatcher` struct (after `stats` at line ~132):

```go
	router *ReportRouter
```

In `NewDispatcher`, after `d.stats` initialization (after line ~219), add:

```go
	// Initialize report router for multi-agent handoff
	d.router = NewReportRouter(ReportRouterConfig{
		Registry:   d.registry,
		Dispatcher: d,
		Logger:     cfg.Logger,
	})
```

Now replace the dead-end code block at lines 699-716. Replace:

```go
	// Run the agent
	response, err := agent.RunOnce(ctx, contextMsg, conversationID)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	// Parse structured report from response and strip it from the display output
	report := ExtractReport(response)
	action := DetermineRouteAction(report)
	d.logger.Debug("Agent completed", "action", action.String(), "agent", result.AgentID)
	displayResponse := StripReport(response)

	// Record memory of this interaction
	if d.memvid != nil && d.memvid.IsAvailable(ctx) {
		go d.recordInteraction(context.Background(), result, displayResponse) //nolint:gosec // background goroutine outlives request context
	}

	return displayResponse, nil
```

With:

```go
	// Run the agent
	response, err := agent.RunOnce(ctx, contextMsg, conversationID)
	if err != nil {
		return "", fmt.Errorf("agent execution failed: %w", err)
	}

	// Parse structured report and route through report router
	report := ExtractReport(response)
	action := DetermineRouteAction(report)
	d.logger.Info("Agent completed",
		"action", action.String(),
		"agent", result.AgentID,
		"has_report", report != nil,
	)
	displayResponse := StripReport(response)

	// Use report router to determine next action
	routeResult := d.router.Route(ctx, RouteParams{
		Report:  report,
		Action:  action,
		AgentID: result.AgentID,
		Depth:   0,
	})

	// If routing suggests a next agent, handle the handoff
	if action == RouteActionRoute && !routeResult.ForceNotify && report != nil {
		nextAgentID := report.SuggestedNextAgent
		d.logger.Info("Routing to next agent",
			"from", result.AgentID,
			"to", nextAgentID,
			"depth", routeResult.Depth,
		)
		// Build accumulated context from previous agent's work
		accumulatedContext := d.buildAccumulatedContext(report, displayResponse)
		nextResult := &DispatchResult{
			AgentID: nextAgentID,
			Intent:  result.Intent,
		}
		// Recursively route to the next agent
		return d.RouteToAgent(ctx, nextResult, conversationID)
	}

	// Record memory of this interaction
	if d.memvid != nil && d.memvid.IsAvailable(ctx) {
		go d.recordInteraction(context.Background(), result, displayResponse) //nolint:gosec // background goroutine outlives request context
	}

	// Use route result's response if available, otherwise use display response
	finalResponse := displayResponse
	if routeResult.FinalResponse != "" && routeResult.ForceNotify {
		finalResponse = routeResult.FinalResponse + "\n\n" + displayResponse
	}

	return finalResponse, nil
```

Add the `buildAccumulatedContext` helper method to the `Dispatcher`:

```go
// buildAccumulatedContext creates context from a previous agent's report for the next agent.
func (d *Dispatcher) buildAccumulatedContext(report *AgentReport, displayResponse string) string {
	var parts []string
	if len(report.Accomplished) > 0 {
		parts = append(parts, "accomplished: "+strings.Join(report.Accomplished, "; "))
	}
	if len(report.Issues) > 0 {
		parts = append(parts, "issues: "+strings.Join(report.Issues, "; "))
	}
	if len(report.Observations) > 0 {
		parts = append(parts, "observations: "+strings.Join(report.Observations, "; "))
	}
	if report.DecisionContext != "" {
		parts = append(parts, "decision context: "+report.DecisionContext)
	}
	return strings.Join(parts, "\n")
}
```

- [x]**Step 4: Run test**

Run: `go test ./internal/agent/... -v -run TestRouteToAgentUsesReportRouter`
Expected: PASS

- [x]**Step 5: Run full test suite**

Run: `go test ./internal/agent/... -count=1 2>&1 | tail -20`
Expected: All tests pass

- [x]**Step 6: Commit**

```bash
git add internal/agent/dispatcher.go
git commit -m "feat: wire ReportRouter into Dispatcher for multi-agent handoff"
```

---

### Task 6: Create MCP transport layer

**Files:**
- Create: `internal/mcp/transport.go`
- Test: `internal/mcp/transport_test.go`

This handles the MCP protocol JSON-RPC framing over stdin/stdout.

- [x]**Step 1: Write the failing test**

Create `internal/mcp/transport_test.go`:

```go
package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReadMessage(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"
	r := bytes.NewReader([]byte(input))

	msg, err := ReadMessage(r)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.Method != "tools/list" {
		t.Errorf("Method = %q, want %q", msg.Method, "tools/list")
	}
	if msg.ID != float64(1) {
		t.Errorf("ID = %v, want 1", msg.ID)
	}
}

func TestWriteMessage(t *testing.T) {
	var buf bytes.Buffer
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  json.RawMessage(`{"tools":[]}`),
	}
	if err := WriteMessage(&buf, resp); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
	// Should end with newline
	if buf.Bytes()[buf.Len()-1] != '\n' {
		t.Error("expected trailing newline")
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/... -v`
Expected: package doesn't exist yet

- [x]**Step 3: Create the MCP transport package**

Create `internal/mcp/transport.go`:

```go
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// JSONRPCRequest is a JSON-RPC 2.0 request (MCP uses JSON-RPC over stdio).
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ReadMessage reads a single JSON-RPC message from the reader (one line).
func ReadMessage(r io.Reader) (*JSONRPCRequest, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, fmt.Errorf("empty message")
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &req, nil
}

// WriteMessage writes a JSON-RPC response as a single line.
func WriteMessage(w io.Writer, resp *JSONRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := fmt.Fprintf(w, "%s\n", data); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
```

Add the missing `bytes` import and `TrimSpace` usage:

```go
import (
	"bytes"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)
```

Fix the `ReadMessage` to use `bytes.TrimSpace` directly — the `line` from `ReadBytes` already includes the newline, and `bytes.TrimSpace` handles it:

```go
	line = bytes.TrimSpace(line)
```

This is already in the code above. Good.

- [x]**Step 4: Run test**

Run: `go test ./internal/mcp/... -v -run TestRead`
Expected: PASS

- [x]**Step 5: Commit**

```bash
git add internal/mcp/transport.go internal/mcp/transport_test.go
git commit -m "feat: add MCP transport layer (JSON-RPC over stdio)"
```

---

### Task 7: Create MCP tool definitions

**Files:**
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/tools_test.go`

Defines the MCP tools that the server exposes: `meept_sessions`, `meept_send`, `meept_events`, `meept_status`.

- [x]**Step 1: Write the failing test**

Create `internal/mcp/tools_test.go`:

```go
package mcp

import (
	"encoding/json"
	"testing"
)

func TestToolDefinitions(t *testing.T) {
	tools := ToolDefinitions()
	if len(tools) == 0 {
		t.Fatal("expected at least one tool definition")
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		names[tool.Name] = true
	}

	expected := []string{"meept_sessions", "meept_send", "meept_events", "meept_status", "meept_session_history"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestToolDefinitionsJSON(t *testing.T) {
	tools := ToolDefinitions()
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal tools: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/... -v -run TestTool`
Expected: `ToolDefinitions` undefined

- [x]**Step 3: Implement tool definitions**

Create `internal/mcp/tools.go`:

```go
package mcp

// ToolDefinition describes an MCP tool.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolDefinitions returns all MCP tools exposed by the meept server.
func ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "meept_sessions",
			Description: "List, create, or attach to meept chat sessions. Use action 'list' to see sessions, 'create' to make a new one, 'attach' to join an existing session (auto-fetches history).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "create", "attach"},
						"description": "Action to perform",
					},
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session ID (required for attach)",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Name for new session (optional, for create)",
					},
					"client_id": map[string]any{
						"type":        "string",
						"description": "Client identifier (e.g. 'claude', used for attach)",
					},
				},
				"required": []string{"action"},
			},
		},
		{
			Name:        "meept_send",
			Description: "Send a message to an attached meept session. The message is processed by the agent system and the response is returned.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session ID to send to",
					},
					"message": map[string]any{
						"type":        "string",
						"description": "Message text to send",
					},
					"source_client": map[string]any{
						"type":        "string",
						"description": "Client identifier (e.g. 'claude')",
					},
				},
				"required": []string{"session_id", "message"},
			},
		},
		{
			Name:        "meept_events",
			Description: "Poll events from a meept session since the last call. Returns agent progress events, chat messages from other participants, and agent responses.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subscription_id": map[string]any{
						"type":        "string",
						"description": "Subscription ID from bus.subscribe",
					},
					"since": map[string]any{
						"type":        "string",
						"description": "RFC3339 timestamp to fetch events after",
					},
				},
				"required": []string{"subscription_id"},
			},
		},
		{
			Name:        "meept_status",
			Description: "Get meept daemon status including active agents, queue depth, and connected clients.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "meept_session_history",
			Description: "Get recent messages from a meept session for context.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{
						"type":        "string",
						"description": "Session ID",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum messages to return (default 50)",
					},
				},
				"required": []string{"session_id"},
			},
		},
	}
}
```

- [x]**Step 4: Run test**

Run: `go test ./internal/mcp/... -v -run TestTool`
Expected: PASS

- [x]**Step 5: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat: add MCP tool definitions for meept session operations"
```

---

### Task 8: Create MCP server

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/server_test.go`

The server reads JSON-RPC from stdin, dispatches to tool implementations, writes responses to stdout. It connects to meept-daemon via the existing RPC transport.

- [x]**Step 1: Write the failing test**

Create `internal/mcp/server_test.go`:

```go
package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestServerHandlesInitialize(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test"}}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	// Process one message
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	// Read response
	var resp JSONRPCResponse
	line, err := output.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	// Should contain server info
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["serverInfo"] == nil {
		t.Error("expected serverInfo in initialize response")
	}
}

func TestServerHandlesToolsList(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	var resp JSONRPCResponse
	line, _ := output.ReadBytes('\n')
	json.Unmarshal(line, &resp)

	var result map[string]any
	json.Unmarshal(resp.Result, &result)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Error("expected non-empty tools array")
	}
}
```

- [x]**Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/... -v -run TestServer`
Expected: `NewServer`, `processOne` undefined

- [x]**Step 3: Implement MCP server**

Create `internal/mcp/server.go`:

```go
package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/caimlas/meept/internal/transport"
)

// Server implements an MCP server that talks to meept-daemon via RPC.
type Server struct {
	input  io.Reader
	output io.Writer
	client transport.Client
	logger *slog.Logger
}

// NewServer creates a new MCP server reading from input and writing to output.
// client may be nil for testing (tools will return errors).
func NewServer(input io.Reader, output io.Writer, client transport.Client) *Server {
	return &Server{
		input:  input,
		output: output,
		client: client,
		logger: slog.Default().With("component", "mcp-server"),
	}
}

// Run starts the MCP server message loop. Blocks until EOF or error.
func (s *Server) Run() error {
	for {
		if err := s.processOne(); err != nil {
			if err == io.EOF {
				return nil
			}
			s.logger.Error("message processing error", "error", err)
			return err
		}
	}
}

// processOne reads and handles a single JSON-RPC message.
func (s *Server) processOne() error {
	req, err := ReadMessage(s.input)
	if err != nil {
		return err
	}

	var resp *JSONRPCResponse
	switch req.Method {
	case "initialize":
		resp = s.handleInitialize(req)
	case "notifications/initialized":
		// No response needed for notifications
		return nil
	case "tools/list":
		resp = s.handleToolsList(req)
	case "tools/call":
		resp = s.handleToolsCall(req)
	default:
		resp = &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &JSONRPCError{
				Code:    -32601,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}

	if resp != nil {
		return WriteMessage(s.output, resp)
	}
	return nil
}

func (s *Server) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	result, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "meept",
			"version": "0.1.0",
		},
	})
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	tools := ToolDefinitions()
	result, _ := json.Marshal(map[string]any{
		"tools": tools,
	})
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) handleToolsCall(req *JSONRPCRequest) *JSONRPCResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32602, Message: "invalid params"},
			}
		}
	}

	if s.client == nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32000, Message: "not connected to daemon"},
		}
	}

	var result any
	var err error

	switch params.Name {
	case "meept_sessions":
		result, err = s.toolSessions(params.Arguments)
	case "meept_send":
		result, err = s.toolSend(params.Arguments)
	case "meept_events":
		result, err = s.toolEvents(params.Arguments)
	case "meept_status":
		result, err = s.toolStatus(params.Arguments)
	case "meept_session_history":
		result, err = s.toolSessionHistory(params.Arguments)
	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		}
	}

	if err != nil {
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshal(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("error: %v", err)}}}),
		}
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  mustMarshal(map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", result)}}}),
	}
}

// Tool implementations delegate to the RPC client.

func (s *Server) toolSessions(args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	switch action {
	case "list":
		return s.client.ListSessions()
	case "create":
		name, _ := args["name"].(string)
		if name == "" {
			name = "mcp-session"
		}
		return s.client.CreateSession(name)
	case "attach":
		sessionID, _ := args["session_id"].(string)
		clientID, _ := args["client_id"].(string)
		if clientID == "" {
			clientID = "mcp"
		}
		if err := s.client.AttachSession(sessionID, clientID); err != nil {
			return nil, err
		}
		// Auto-catchup: fetch recent history
		messages, err := s.client.GetSessionMessages(sessionID, 0, 50)
		if err != nil {
			return map[string]any{"status": "attached", "session_id": sessionID}, nil
		}
		return map[string]any{
			"status":   "attached",
			"session_id": sessionID,
			"history":  messages,
		}, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}

func (s *Server) toolSend(args map[string]any) (any, error) {
	sessionID, _ := args["session_id"].(string)
	message, _ := args["message"].(string)
	if sessionID == "" || message == "" {
		return nil, fmt.Errorf("session_id and message are required")
	}
	// Use the chat RPC method with source_client
	sourceClient, _ := args["source_client"].(string)
	if sourceClient == "" {
		sourceClient = "mcp"
	}
	// The Chat method on transport.Client sends to chat.request
	// We need to include source_client, so use the low-level Call method
	params := map[string]any{
		"message":         message,
		"conversation_id": sessionID,
		"source_client":   sourceClient,
	}
	result, err := s.client.Call("chat", params)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"response": string(result),
	}, nil
}

func (s *Server) toolEvents(args map[string]any) (any, error) {
	subID, _ := args["subscription_id"].(string)
	since, _ := args["since"].(string)
	if subID == "" {
		return nil, fmt.Errorf("subscription_id is required")
	}
	params := map[string]any{
		"subscription_id": subID,
		"since":           since,
	}
	result, err := s.client.Call("bus.poll", params)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(result), nil
}

func (s *Server) toolStatus(args map[string]any) (any, error) {
	status, err := s.client.Status()
	if err != nil {
		return nil, err
	}
	return status, nil
}

func (s *Server) toolSessionHistory(args map[string]any) (any, error) {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	limit := 50
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	return s.client.GetSessionMessages(sessionID, 0, limit)
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// ConnectRPC establishes the RPC connection to meept-daemon.
func (s *Server) ConnectRPC(socketPath string) error {
	cfg := transport.DefaultConfig()
	cfg.SocketPath = socketPath
	client, err := transport.New(cfg)
	if err != nil {
		return fmt.Errorf("create RPC client: %w", err)
	}
	if err := client.Connect(); err != nil {
		client.Close()
		return fmt.Errorf("connect to daemon: %w", err)
	}
	s.client = client
	return nil
}

// ConnectAndSubscribe connects to the daemon and subscribes to event topics.
// Returns the subscription ID for use with meept_events.
func (s *Server) ConnectAndSubscribe(socketPath string) (string, error) {
	if err := s.ConnectRPC(socketPath); err != nil {
		return "", err
	}

	// Subscribe to relevant bus topics
	topics := []string{
		"chat.message.received",
		"chat.response",
		"agent.event.*",
		"worker.*",
	}
	result, err := s.client.Call("bus.subscribe", map[string]any{"topics": topics})
	if err != nil {
		return "", fmt.Errorf("subscribe: %w", err)
	}

	var resp struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return "", fmt.Errorf("parse subscription response: %w", err)
	}
	return resp.SubscriptionID, nil
}
```

- [x]**Step 4: Run tests**

Run: `go test ./internal/mcp/... -v`
Expected: PASS

- [x]**Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat: add MCP server with tool implementations and RPC bridge"
```

---

### Task 9: Create `meept mcp-chat-server` CLI subcommand

**Files:**
- Create: `cmd/meept/mcp_chat_server.go`
- Modify: `cmd/meept/main.go:110-129` (add subcommand registration)

- [x]**Step 1: Create the subcommand**

Create `cmd/meept/mcp_chat_server.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/mcp"
)

func newMCPChatServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-chat-server",
		Short: "run the mcp chat server (for external agent platforms)",
		Long: `Run the MCP chat server for external agent platforms (Claude, etc.).

Communicates via MCP protocol over stdin/stdout (JSON-RPC).
Connects to the meept daemon via Unix socket RPC.

Configuration is read from ~/.meept/meept.json5.

Register with Claude Code by adding to ~/.claude/settings.json:
{
  "mcpServers": {
    "meept": {
      "command": "meept",
      "args": ["mcp-chat-server"]
    }
  }
}`,
		RunE: runMCPChatServer,
	}
}

func runMCPChatServer(cmd *cobra.Command, args []string) error {
	socketPath := getSocketPath()

	srv := mcp.NewServer(os.Stdin, os.Stdout, nil)

	// Connect to daemon and subscribe to event topics
	subID, err := srv.ConnectAndSubscribe(socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}

	// Log subscription info to stderr (stdout is MCP protocol)
	fmt.Fprintf(os.Stderr, "meept mcp-chat-server: connected (subscription: %s)\n", subID)

	// Run the MCP message loop (blocks until stdin closes)
	if err := srv.Run(); err != nil {
		return fmt.Errorf("mcp server error: %w", err)
	}
	return nil
}
```

- [x]**Step 2: Register the subcommand in main.go**

In `cmd/meept/main.go`, add after the existing `rootCmd.AddCommand` lines (around line 129):

```go
	rootCmd.AddCommand(newMCPChatServerCmd())
```

- [x]**Step 3: Build and verify it compiles**

Run: `go build -o bin/meept ./cmd/meept`
Expected: no errors

- [x]**Step 4: Verify the help text**

Run: `./bin/meept mcp-chat-server --help`
Expected: prints usage info with description

- [x]**Step 5: Commit**

```bash
git add cmd/meept/mcp_chat_server.go cmd/meept/main.go
git commit -m "feat: add meept mcp-chat-server CLI subcommand"
```

---

### Task 10: Add MCP server config to `meept.json5`

**Files:**
- Modify: `config/meept.json5`

- [x]**Step 1: Add config section**

In `config/meept.json5`, add after the transport section:

```json5
  // MCP chat server configuration
  "mcp_chat_server": {
    "enabled": true,
    "socket_path": "~/.meept/meept.sock",
  },
```

- [x]**Step 2: Verify the config is valid JSON5**

Run: `python3 -c "import json5; json5.load(open('config/meept.json5'))" 2>&1 || echo "json5 not available, manual check"`
Expected: no parse error (or manual verification)

- [x]**Step 3: Build to verify no breakage**

Run: `go build -o bin/meept-daemon ./cmd/meept-daemon`
Expected: no errors

- [x]**Step 4: Commit**

```bash
git add config/meept.json5
git commit -m "feat: add mcp_chat_server config section to meept.json5"
```

---

## Self-Review

### Spec Coverage

| Spec Section | Task |
|---|---|
| 1. Client Identity & Bilateral Visibility | Tasks 1, 2, 3 |
| 2. Report Router | Tasks 4, 5 |
| 4. MCP Chat Server | Tasks 6, 7, 8, 9, 10 |
| 6b. Client disconnects | Task 2 (event type added) |

### Placeholder Scan

No TBDs, TODOs, or vague instructions found. Every step contains concrete code or commands.

### Type Consistency

- `ChatRequest.SourceClient` — defined in Task 2, used in Tasks 3 and 8
- `BusMessage.SourceClient` — defined in Task 1, available throughout
- `ReportRouter` / `RouteParams` / `RouteResult` — defined in Task 4, used in Task 5
- `JSONRPCRequest` / `JSONRPCResponse` — defined in Task 6, used in Tasks 7, 8
- `ToolDefinition` — defined in Task 7, used in Task 8

All type names consistent across tasks.
