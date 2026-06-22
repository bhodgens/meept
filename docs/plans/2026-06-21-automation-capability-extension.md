# Automation Capability Extension Plans

**Created:** 2026-06-21
**Author:** AI Agent
**Status:** ✅ COMPLETE - All 4 plans implemented & verified

## Executive Summary

This document contains four interconnected plans to extend Meept's automation capabilities to match or exceed Claude Code's functionality:

1. **Plan 1: Telegram Bot Abstraction** - Make Telegram a first-class bot type with bidirectional capabilities
2. **Plan 2: Lifecycle Hooks Extension** - Match Claude Code's hook capability (session lifecycle, HTTP, file watchers, async)
3. **Plan 3: Push-to-Session Capability** - Enable bots to communicate upward to users
4. **Plan 4: Session Designation & Promotion System** - Auto-designate sessions needing attention, surface across all UIs

**Key Insight:** Session designation (Plan 4) is the "glue" that connects all capabilities - it should be prioritized first.

---

## Current State Assessment

### Notification System (Existing)

| Component | Status | Location |
|-----------|--------|----------|
| EventEmitter | ✅ Implemented | `internal/comm/http/events.go` |
| WebSocket Handler | ✅ Implemented | `internal/comm/http/notification_handlers.go` |
| Menubar Integration | ✅ Implemented | `menubar/MeeptMenuBar/Services/NotificationManager.swift` |
| Notification Center UI | ✅ Implemented | `menubar/MeeptMenuBar/Views/NotificationCenterMenuView.swift` |
| Agent Loop Integration | ⚠️ Partial | `internal/agent/loop.go` |

### Notification Coverage Gaps

| Event Type | Currently Notified? | Priority | Plan |
|------------|---------------------|----------|------|
| Task started | ❌ No | Low | Plan 4.3 |
| Task completed | ❌ No | High | Plan 4.3 |
| Task failed | ✅ Yes (partial) | High | - |
| Task blocked (needs approval) | ❌ No | **Critical** | Plan 4.3 |
| Bot finished run | ❌ No | High | Plan 4.3 |
| Scheduler job completed | ❌ No | Medium | Plan 4.3 |
| Session waiting for human input | ❌ No | **Critical** | Plan 4.3 |
| Budget exhausted | ❌ No | High | Plan 1.3 |
| Security alert | ❌ No | High | Plan 2.2 |

---

## Plan 1: Telegram Bot Abstraction

**Goal:** Make Telegram a first-class bot type with bidirectional capabilities

### Phase 1: Bot Interface Definition

**Deliverables:**
- [ ] Define `Bot` interface with core methods
- [ ] Extract common fields from `BotDefinition` into base struct
- [ ] Ensure existing cron/webhook bots implement interface

```go
type Bot interface {
    ID() string
    Name() string
    Execute(ctx context.Context) error
}

type MessagingBot interface {
    Bot
    SendMessage(ctx context.Context, target string, content string) error
    CanInitiate() bool  // can bot send unsolicited messages?
}
```

**Security Review:**
- Interface boundaries must be clear (Telegram can't be cast to CronBot)
- Type-safe casting only

### Phase 2: Telegram Channel Adapter

**Deliverables:**
- [ ] Create `TelegramChannel` type implementing `MessagingBot`
- [ ] Refactor `AgentHandler` to use new interface
- [ ] Support bidirectional messaging

```go
type TelegramChannel struct {
    botClient  *telegram.Bot
    config     TelegramChannelConfig
    sessions   SessionManager
}

func (t *TelegramChannel) Execute(ctx context.Context) error {
    // Start polling loop
    // Route messages through agent
    // Handle bot-initiated messages
}
```

### Phase 3: Notification Bot (New Type)

**Deliverables:**
- [ ] Create `NotificationBot` that pushes to channels
- [ ] Templates for outbound messages
- [ ] Rate limiting per channel

```go
type NotificationBotConfig struct {
    TargetChannels []string  // "telegram:12345", "cli:session-abc"
    Templates      map[string]string
    MaxPerHour     int
}
```

### Phase 4: Security Hardening

**Deliverables:**
- [ ] Channel allowlists
- [ ] Outbound message templates enforced
- [ ] Rate limiting
- [ ] Audit logging for all bot→user messages

---

## Plan 2: Lifecycle Hooks Extension

**Goal:** Match Claude Code's hook capability

### Phase 1: Session Lifecycle Hooks

**Deliverables:**
- [ ] Add `SessionStartHook`, `SessionEndHook` interfaces
- [ ] Wire hooks into agent loop
- [ ] Hook execution at session boundaries

```go
// internal/agent/hooks.go - ADD:
type SessionStartHook interface {
    OnSessionStart(ctx context.Context, state SessionState) ContextTransform
}

type SessionEndHook interface {
    OnSessionEnd(ctx context.Context, state SessionState, result SessionResult) error
}
```

**Wiring Points:**
- `agent/loop.go:RunOnce()` - call `SessionStartHook` at beginning
- `agent/loop.go:Close()` - call `SessionEndHook` before cleanup

### Phase 2: HTTP Hooks

**Deliverables:**
- [ ] `HTTPHook` type with URL, headers, timeout
- [ ] JSON payload construction
- [ ] Response handling (2xx = success, non-2xx = failure)
- [ ] Allowlist for URLs

```go
type HTTPHookConfig struct {
    URL          string            `json:"url"`
    Method       string            `json:"method"`  // POST, PUT
    Headers      map[string]string `json:"headers"`
    AllowedEnvVars []string        `json:"allowed_env_vars"`
    Timeout      time.Duration     `json:"timeout"`
    RetryCount   int               `json:"retry_count"`
}
```

**Security:**
- `allowHttpHookUrls` config setting (regexp list)
- No internal network access by default

### Phase 3: File Watcher Hooks

**Deliverables:**
- [ ] File watcher using `fsnotify`
- [ ] Pattern matching (`*.go`, `**/test_*.py`)
- [ ] Debouncing (don't fire on every keystroke)

```go
type FileWatcherHook struct {
    Pattern   string  // glob pattern
    Callback  HookHandler
    Debounce  time.Duration
    Ignore    []string  // .git/, node_modules/
    watcher   *fsnotify.Watcher
}
```

**Resource Management:**
- Limit number of watched files
- Auto-unwatch after session ends

### Phase 4: Async Hooks

**Deliverables:**
- [ ] `async: true` flag in hook config
- [ ] `asyncRewake: true` option
- [ ] Background goroutine management

```go
type AsyncHookConfig struct {
    Async      bool `json:"async"`
    Rewake     bool `json:"async_rewake"`  // wake agent on failure
    Timeout    time.Duration
}
```

---

## Plan 3: Push-to-Session Capability

**Goal:** Enable bots to communicate upward to users

### Phase 1: Session Activity Tracker

**Deliverables:**
- [ ] Track last activity timestamp per session
- [ ] Define "active session" (activity within N minutes)
- [ ] Expose API for querying active sessions

```go
// internal/session/activity_tracker.go
type ActivityTracker struct {
    mu sync.RWMutex
    activity map[SessionID]*ActivityState
}

func (t *ActivityTracker) RecordActivity(sessionID string, clientID string)
func (t *ActivityTracker) GetActiveSessions(window time.Duration) []SessionID
func (t *ActivityTracker) HasRecentActivity(sessionID string, window time.Duration) bool
```

### Phase 2: Push Service API

**Deliverables:**
- [ ] `PushService` interface
- [ ] Message types (notification, alert, summary)
- [ ] Delivery tracking

```go
// internal/services/push_service.go
type PushService struct {
    sessionMgr *SessionManager
    bus        *bus.MessageBus
    logger     *slog.Logger
}

func (s *PushService) Push(ctx context.Context, req *PushRequest) (*PushResult, error)
type PushRequest struct {
    SessionIDs []string
    Source     string
    Type       PushType
    Content    string
    Priority   string
}
```

### Phase 3: Channel Routing

**Deliverables:**
- [ ] Route push to appropriate channel (Telegram, CLI, TUI)
- [ ] Format adaptation per channel
- [ ] Undelivered message queue

```go
// Each channel implements:
type PushChannel interface {
    CanReceive(sessionID string) bool
    Push(ctx context.Context, sessionID string, msg *PushMessage) error
}

// Registry:
type ChannelRegistry struct {
    channels map[ChannelType]PushChannel  // "telegram", "cli", "tui", "http"
}
```

### Phase 4: Bot Integration

**Deliverables:**
- [ ] Expose push API to bots
- [ ] Expose push API to scheduler jobs
- [ ] Example: CI monitor pushes build status

```go
// Bot can now call:
err := botCtx.PushNotification(ctx, &PushRequest{
    Source:  "bot:ci-monitor",
    Type:    PushTypeAlert,
    Content: "Build #1234 failed: tests red",
})
```

### Phase 5: User Controls

**Deliverables:**
- [ ] Per-user notification preferences
- [ ] Rate limiting (max N pushes per hour)
- [ ] "Do not disturb" mode
- [ ] Push history/query API

---

## Plan 4: Session Designation & Promotion System

**Goal:** Auto-designate sessions needing attention, surface visually across all UIs, send system notifications

### Phase 1: Data Model & Designation API

**Deliverables:**
- [ ] Add `SessionDesignation` struct to `internal/session/session.go`
- [ ] Add `designation` field to Session
- [ ] Create `SetDesignation()` and `ClearDesignation()` methods
- [ ] Add designation persistency (auto-save with session)

```go
// internal/session/session.go - ADD:
type DesignationStatus string

const (
    DesignationNone              DesignationStatus = "none"
    DesignationWaitingHuman      DesignationStatus = "waiting_human"
    DesignationHumanResponded    DesignationStatus = "human_responded"
    DesignationBotThinking       DesignationStatus = "bot_thinking"
    DesignationRequiresApproval  DesignationStatus = "requires_approval"
)

type SessionDesignation struct {
    Status     DesignationStatus `json:"status"`
    Reason     string            `json:"reason"`
    CreatedAt  time.Time         `json:"created_at"`
    UpdatedAt  time.Time         `json:"updated_at"`
    AcknowledgedAt *time.Time    `json:"acknowledged_at,omitempty"`
    Priority   string            `json:"priority"`  // low, normal, high, urgent
}

// Add to Session struct:
type Session struct {
    // ... existing fields ...
    Designation *SessionDesignation `json:"designation,omitempty"`
}
```

### Phase 2: Automatic Designation in Agent Loop

**Deliverables:**
- [ ] Hook into agent loop to set designation automatically
- [ ] Triggers: tool blocked, clarification needed, user responded
- [ ] Clear designation after turn completes

| Trigger | Designation Status | Reason |
|---------|-------------------|--------|
| Tool permission denied | `requires_approval` | "Blocked: shell command requires approval" |
| Agent asks clarifying question | `waiting_human` | "Awaiting: user clarification on X" |
| User sends message (while bot waiting) | `human_responded` | "User responded" |
| Bot starts processing | `bot_thinking` | "Processing..." |
| Bot completes turn | `none` | (clear designation) |

**Files to modify:**
- `internal/agent/loop.go`
- `internal/agent/orchestrator.go`

### Phase 3: Session Promotion (TUI/GUI) + Notification Gap Closures ⭐

**Deliverables:**
- [ ] TUI: Sort by designation priority
- [ ] TUI: Visual indicator (badge/color)
- [ ] GUI: Same sorting + visual indicator
- [ ] Menubar: Badge for urgent sessions
- [ ] **Wire task completion notifications**
- [ ] **Wire "needs approval" notifications**
- [ ] **Wire bot completion notifications**

**TUI Sorting Logic:**
```go
// internal/tui/models/sessions.go - MODIFY sort logic:
func (m *SessionsModel) sortSessions() {
    sort.Slice(m.sessions, func(i, j int) bool {
        // Designated sessions first (sorted by priority)
        iDesig := m.sessions[i].Designation
        jDesig := m.sessions[j].Designation

        if iDesig != nil && jDesig != nil {
            return priorityOrder(iDesig.Priority) < priorityOrder(jDesig.Priority)
        }
        if iDesig != nil {
            return true  // i has designation, put first
        }
        if jDesig != nil {
            return false  // j has designation, put first
        }

        // Fall back to last activity
        return m.sessions[i].LastActivity.After(m.sessions[j].LastActivity)
    })
}
```

**Notification Gap Closures - Wiring Points:**

```go
// internal/agent/loop.go - ADD at task completion (~line 2700):
if l.notificationPublisher != nil {
    l.notificationPublisher.PublishTaskNotification(
        t.ID, l.agentID, "success",
        "Task Completed", t.Description[:50]+"...",
    )
}

// internal/agent/loop.go - ADD at tool permission blocking:
if toolBlocked && requiresUserApproval {
    l.notificationPublisher.PublishTaskNotification(
        taskID, l.agentID, "warning",
        "Action Requires Approval",
        "Shell command blocked: "+command,
    )
}

// internal/agent/loop.go - ADD when bot completes:
if l.notificationPublisher != nil {
    l.notificationPublisher.PublishTaskNotification(
        sessionID, l.agentID, "success",
        "Bot Run Completed", "All tasks finished successfully",
    )
}
```

**Files to modify:**
- `internal/tui/models/sessions.go`
- `internal/tui/app.go`
- `ui/flutter_ui/` (Flutter session list widget)
- `menubar/Views/SessionListView.swift`
- `internal/agent/loop.go` (notification wiring)

### Phase 4: Notification Dispatch Enhancements

**Deliverables:**
- [ ] Add new notification types for session states
- [ ] Menubar integration (native macOS notifications)
- [ ] Rate limiting (max N notifications per minute)
- [ ] Config: notification preferences per type

```go
// internal/comm/http/notification_handlers.go - ADD:
const (
    NotificationTypeSessionWaiting   NotificationType = "session_waiting"
    NotificationTypeSessionCompleted NotificationType = "session_completed"
    NotificationTypeBotFinished      NotificationType = "bot_finished"
    NotificationTypeRequiresApproval NotificationType = "requires_approval"
)
```

**Files to create:**
- `internal/daemon/notifications.go` (consolidated notification service)

**Files to modify:**
- `internal/comm/http/notification_handlers.go`
- `menubar/MeeptMenuBar/Services/NotificationManager.swift` (extend event types)

### Phase 5: HTTP/RPC API

**Deliverables:**
- [ ] HTTP endpoints (get, acknowledge, list with filter)
- [ ] RPC handlers for TUI/menubar
- [ ] WebSocket/SSE for real-time updates

```go
// internal/comm/http/api_handlers.go - ADD:

// GET /api/v1/sessions/:id/designation
func (s *Server) GetSessionDesignation(w http.ResponseWriter, r *http.Request) { ... }

// POST /api/v1/sessions/:id/acknowledge
func (s *Server) AcknowledgeSession(w http.ResponseWriter, r *http.Request) { ... }

// GET /api/v1/sessions?designation=waiting_human
func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request) { ... }
```

**Files to modify:**
- `internal/comm/http/api_handlers.go`
- `internal/rpc/session_handlers.go`
- `internal/comm/http/events.go` (for SSE)

### Phase 6: User Controls

**Deliverables:**
- [ ] Notification preferences (per type, per channel)
- [ ] "Do not disturb" mode
- [ ] Designation history (audit log)
- [ ] CLI: `meept sessions --needs-attention`

**Files to create/modify:**
- `internal/config/schema.go` (add notification settings)
- `cmd/meept/sessions.go` (CLI command)

---

## Combined Architecture

```
┌──────────────────────────────────────────────────────────────────────────┐
│                         Bot Framework                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │  CronBot    │  │ WebhookBot  │  │  ChatBot    │  │ NotificationBot │ │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
         │                    │                    │
         └────────────────────┼────────────────────┘
                              │
                              ↓
         ┌─────────────────────────────────────────────┐
         │           Session Designation               │
         │  - waiting_human / requires_approval        │
         │  - Auto-set by agent loop                  │
         └─────────────────────────────────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         │                    │                    │
         ↓                    ↓                    ↓
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   TUI (sort)    │  │  GUI (sort)     │  │  Menubar        │
│  Move to top    │  │  Move to top    │  │  Notification   │
└─────────────────┘  └─────────────────┘  └─────────────────┘
         │                    │                    │
         └────────────────────┼────────────────────┘
                              │
                              ↓
         ┌─────────────────────────────────────────────┐
         │           Lifecycle Hooks                   │
         │  - SessionStart/End                         │
         │  - HTTP hooks (external integrations)       │
         │  - FileChanged (auto-commit, auto-test)     │
         └─────────────────────────────────────────────┘
```

---

## Implementation Priority

Based on architectural dependencies and user value:

| Priority | Plan | Phase | Rationale |
|----------|------|-------|-----------|
| **P0** | Plan 4 | Phase 1-2 | Session designation is foundational |
| **P0** | Plan 4 | Phase 3 | **Notification gap closures** - immediate user value |
| **P1** | Plan 3 | Phase 1-2 | Push API needed for notifications |
| **P1** | Plan 4 | Phase 4-5 | Menubar notifications, API |
| **P1** | Plan 2 | Phase 1 | Session lifecycle hooks (enable rest) |
| **P2** | Plan 1 | Phase 1-3 | Notification Bot uses push API |
| **P2** | Plan 2 | Phase 2 | HTTP hooks |
| **P3** | Plan 2 | Phase 3-4 | File watchers, async hooks |

---

## Notification Gap Summary

**Current State:** Menubar notifications only fire for:
- Long-running task warnings (>30s)
- Task failures (partial)

**Missing (to be added in Plan 4, Phase 3-4):**
- ✅ Task completions (success)
- ✅ Approval requests (blocked tool)
- ✅ Bot completions
- ✅ Session waiting for human input
- ✅ Budget exhausted alerts
- ✅ Scheduler job completions

These will be wired to the existing `EventEmitter` infrastructure, using new `SessionDesignation` states to trigger appropriate notifications.

---

## Next Steps

1. **Start with Plan 4, Phase 1** - Session designation data model
2. **Immediately follow with Plan 4, Phase 3** - Wire notification gaps for quick wins
3. **Then Plan 3** - Push-to-session API for broader routing
4. **Finally Plans 1-2** - Extended capabilities once foundation is solid
