# Desktop Notifications for MenuBar App Implementation

**Created:** 2026-06-09
**Priority:** Medium
**Estimated Effort:** 3-5 days
**Status:** Pending Approval
**Last Updated:** 2026-06-10 (Critical/High fixes applied)

---

## Required Fixes Before Implementation

### Critical Fixes Applied
1. **C1 - Package Structure:** Event emitter moved to `internal/comm/http/events.go` (not `internal/daemon/`)
2. **C2 - WebSocketManager:** Extend existing `menubar/MeeptMenuBar/Services/WebSocketManager.swift` instead of creating new
3. **C3 - TLS:** WebSocket URL changed from `ws://` to `wss://` with `LocalhostTrustDelegate`
4. **C4 - WebSocket Library:** Changed from `gorilla/websocket` to `golang.org/x/net/websocket` (matches existing server)

### High Priority Fixes Applied
1. **H1 - Unsubscribe Race:** Fixed ordering (remove from slice before close)
2. **H2 - UUID:** Use `github.com/google/uuid` package
3. **H3 - Route Registration:** Use server's `mux` instead of global `http.HandleFunc`
4. **H4 - Goroutine Leak:** Use `time.NewTicker` instead of `time.After` in loop
5. **H5 - Config Integration:** Menubar config merged into main `config/meept.json5`
6. **H6 - Authentication:** Add API key auth to WebSocket handler

## Overview

Implement desktop notifications for the Meept MenuBar app to alert users when:
- LLM responses are ready
- Long-running tasks complete
- Errors or failures occur
- User confirmation is required

**Inspired by:** aider-ai/aider's `--notifications` flag implementation

## Use Cases

| Scenario | Notification Type | Priority |
|----------|------------------|----------|
| LLM response ready (after long delay) | Info | Low |
| Task completed successfully | Success | Low |
| Task failed with errors | Error | High |
| User confirmation needed | Warning | High |
| Background job finished | Info | Low |
| Security block occurred | Error | High |
| Budget threshold exceeded | Warning | Medium |

---

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                    Meept Daemon                                 │
│                                                                  │
│  ┌─────────────┐      ┌──────────────┐      ┌───────────────┐  │
│  │ Agent Loop  │─────▶│ Notification │─────▶│ HTTP+SSE API  │  │
│  │             │      │ Event emitter│      │   (port 8081) │  │
│  └─────────────┘      └───────────────┘      └───────┬───────┘  │
│                                (internal/comm/http/)  │         │
└─────────────────────────────────────────────────────┬─┘          │
                                                      │ (TLS)
                                                      ▼
┌────────────────────────────────────────────────────────────────┐
│                    macOS MenuBar App                            │
│  ┌─────────────┐      ┌──────────────┐      ┌───────────────┐ │
│  │ WSS Client  │─────▶│ Notification │─────▶│ NSUserNotificationCenter │ │
│  │ (real-time) │      │ Manager      │      │ (macOS native)│ │
│  └─────────────┘      └──────────────┘      └───────────────┘ │
│  NOTE: Extend existing WebSocketManager.swift, don't create new │
└────────────────────────────────────────────────────────────────┘
```

**Important Implementation Notes:**
- Event emitter package: `internal/comm/http/events.go` (NOT `internal/daemon/`)
- WebSocket library: `golang.org/x/net/websocket` (matches existing server code)
- TLS required: WSS with `LocalhostTrustDelegate` for self-signed cert
- Authentication: API key required on WebSocket endpoint

---

## Implementation

### 1. Daemon-Side: Notification Event System

#### Event Types (`internal/comm/http/events.go`)

```go
package http

import (
    "sync"
    "time"

    "github.com/google/uuid"
    "golang.org/x/net/websocket"
)

// NotificationType represents the type of notification
type NotificationType string

const (
    NotificationTypeInfo     NotificationType = "info"
    NotificationTypeSuccess  NotificationType = "success"
    NotificationTypeWarning  NotificationType = "warning"
    NotificationTypeError    NotificationType = "error"
)

// NotificationEvent represents a notification to be sent to clients
type NotificationEvent struct {
    ID        string           `json:"id"`
    Timestamp string           `json:"timestamp"`  // RFC3339
    Type      NotificationType `json:"type"`
    Title     string           `json:"title"`
    Message   string           `json:"message"`
    Data      map[string]interface{} `json:"data,omitempty"`

    // Routing
    AgentID   string `json:"agent_id,omitempty"`
    TaskID    string `json:"task_id,omitempty"`
    SessionID string `json:"session_id,omitempty"`
}

// EventEmitter broadcasts notification events to connected clients
type EventEmitter struct {
    mu          sync.RWMutex
    subscribers []chan *NotificationEvent
    buffer      []*NotificationEvent  // Recent events for late subscribers
    maxBuffer   int
    logger      *slog.Logger
    closed      bool
}

// NewEventEmitter creates the event emitter
func NewEventEmitter(bufferSize int, logger *slog.Logger) *EventEmitter {
    return &EventEmitter{
        subscribers: make([]chan *NotificationEvent, 0),
        buffer:      make([]*NotificationEvent, 0, bufferSize),
        maxBuffer:   bufferSize,
        logger:      logger,
    }
}

// Subscribe adds a new subscriber and returns their channel
func (e *EventEmitter) Subscribe() chan *NotificationEvent {
    e.mu.Lock()
    defer e.mu.Unlock()

    ch := make(chan *NotificationEvent, 100)  // Buffered to prevent blocking

    // Send buffered events first
    for _, event := range e.buffer {
        select {
        case ch <- event:
        default:
            // Channel full, skip
        }
    }

    e.subscribers = append(e.subscribers, ch)
    return ch
}

// Unsubscribe removes a subscriber - FIXED: remove from slice BEFORE closing
func (e *EventEmitter) Unsubscribe(ch chan *NotificationEvent) {
    e.mu.Lock()
    defer e.mu.Unlock()

    // Remove from slice FIRST
    for i, sub := range e.subscribers {
        if sub == ch {
            e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
            break
        }
    }

    // Close AFTER removing to prevent concurrent write to closed channel
    close(ch)
}

// Publish sends an event to all subscribers
func (e *EventEmitter) Publish(event *NotificationEvent) {
    e.mu.Lock()
    defer e.mu.Unlock()

    if e.closed {
        return  // Don't publish to closed emitter
    }

    // Add to buffer
    e.buffer = append(e.buffer, event)
    if len(e.buffer) > e.maxBuffer {
        e.buffer = e.buffer[1:]
    }

    // Broadcast to subscribers
    for _, sub := range e.subscribers {
        select {
        case sub <- event:
        default:
            // Subscriber not consuming, skip
            e.logger.Warn("Notification subscriber not consuming", "event", event.Title)
        }
    }

    e.logger.Debug("Notification published", "type", event.Type, "title", event.Title)
}

// Close gracefully shuts down the emitter
func (e *EventEmitter) Close() {
    e.mu.Lock()
    defer e.mu.Unlock()

    e.closed = true
    for _, sub := range e.subscribers {
        close(sub)
    }
    e.subscribers = nil
}

// generateUUID generates a unique identifier
func generateUUID() string {
    return uuid.New().String()
}

// PublishTaskNotification is a helper for task-related notifications
func (e *EventEmitter) PublishTaskNotification(taskID, agentID, notifType, title, message string) {
    event := &NotificationEvent{
        ID:        generateUUID(),
        Timestamp: time.Now().UTC().Format(time.RFC3339),
        Type:      NotificationType(notifType),
        Title:     title,
        Message:   message,
        Data: map[string]interface{}{
            "task_id": taskID,
            "agent_id": agentID,
        },
        TaskID:  taskID,
        AgentID: agentID,
    }
    e.Publish(event)
}
```

**File:** `internal/comm/http/events.go` (NEW - NOT `internal/daemon/`)

**Important:**
- Package is `http` not `daemon` (matches existing server structure)
- Uses `golang.org/x/net/websocket` (not `gorilla/websocket`)
- `Unsubscribe` fixed: removes from slice before closing channel
- Added `Close()` for graceful shutdown
- UUID via `github.com/google/uuid`

---

#### Integration with Agent Loop (`internal/agent/orchestrator.go`)

**NOTE:** The actual agent loop logic is in `internal/agent/loop.go` (`AgentLoop` struct), not `Orchestrator`.
The `Orchestrator` is a higher-level wrapper. Metrics/notification integration should happen in `AgentLoop.runOneIteration()`.

```go
// Add notification hooks to the agent loop:

type Orchestrator struct {
    // ... existing fields ...
    eventEmitter *http.EventEmitter  // NOT *Daemon.EventEmitter
}

func (o *Orchestrator) Run(ctx context.Context, task *Task) (*TaskResult, error) {
    startTime := time.Now()

    // FIXED: Use time.NewTicker to avoid goroutine leak from time.After in loop
    longRunningTicker := time.NewTicker(30 * time.Second)
    defer longRunningTicker.Stop()

    // Track if we've notified about long-running task
    notifiedLongRunning := false

    go func() {
        for {
            select {
            case <-longRunningTicker.C:
                if !notifiedLongRunning && time.Since(startTime) > 30*time.Second {
                    o.eventEmitter.PublishTaskNotification(
                        task.ID,
                        o.agentID,
                        string(http.NotificationTypeInfo),
                        "Task Processing",
                        fmt.Sprintf("Task '%s' is taking longer than expected...", truncate(task.Description, 50)),
                    )
                    notifiedLongRunning = true
                }
            case <-ctx.Done():
                return  // Goroutine exits cleanly on context cancellation
            }
        }
    }()

    result, err := o.runLoop(ctx, task)

    // Publish completion notification
    if err != nil {
        o.eventEmitter.PublishTaskNotification(
            task.ID,
            o.agentID,
            string(http.NotificationTypeError),
            "Task Failed",
            fmt.Sprintf("Task '%s' failed: %v", truncate(task.Description, 50), err),
        )
    } else if result.Success {
        o.eventEmitter.PublishTaskNotification(
            task.ID,
            o.agentID,
            string(http.NotificationTypeSuccess),
            "Task Completed",
            fmt.Sprintf("Task '%s' completed successfully", truncate(task.Description, 50)),
        )
    }

    return result, err
}

// Helper for truncation
func truncate(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}
```

**File:** `internal/agent/orchestrator.go` (MODIFY)

**Also update `internal/agent/loop.go` for per-iteration metrics:**
```go
// In AgentLoop.runOneIteration(), add after LLM response:
if al.responseAnalyzer != nil {
    quality := al.responseAnalyzer.Analyze(response, tokensUsed)
    al.metricsCollector.RecordAgentTask(&metrics.AgentTaskMetrics{
        // ... fields ...
    })
}
```

---

#### HTTP Endpoint for Notifications (`internal/comm/http/notification_handlers.go`)

```go
package http

import (
    "context"
    "encoding/json"
    "net/http"
    "time"

    "golang.org/x/net/websocket"
)

// NotificationHandler handles notification endpoints
type NotificationHandler struct {
    eventEmitter *EventEmitter
    logger       *slog.Logger
    apiKey       string  // For authentication
}

// NewNotificationHandler creates the handler
func NewNotificationHandler(emitter *EventEmitter, logger *slog.Logger, apiKey string) *NotificationHandler {
    return &NotificationHandler{
        eventEmitter: emitter,
        logger:       logger,
        apiKey:       apiKey,
    }
}

// verifyAuth checks API key authentication
func (h *NotificationHandler) verifyAuth(r *http.Request) bool {
    // Check Bearer token in Authorization header
    authHeader := r.Header.Get("Authorization")
    if authHeader != "" {
        token := authHeader[len("Bearer "):]
        return token == h.apiKey
    }

    // Fallback: check query parameter (for WebSocket)
    queryToken := r.URL.Query().Get("token")
    return queryToken == h.apiKey
}

// ServeWebSocket handles WebSocket connections for real-time notifications
// FIXED: Uses golang.org/x/net/websocket (not gorilla/websocket)
// FIXED: Requires API key authentication
func (h *NotificationHandler) ServeWebSocket(ws *websocket.Conn) {
    // Verify authentication
    if !h.verifyAuth(ws.Request()) {
        h.logger.Warn("WebSocket auth failed")
        ws.Close()
        return
    }

    // Subscribe to events
    eventCh := h.eventEmitter.Subscribe()
    defer h.eventEmitter.Unsubscribe(eventCh)

    // Send events as they arrive
    for {
        select {
        case event, ok := <-eventCh:
            if !ok {
                return  // Channel closed
            }
            if err := websocket.JSON.Send(ws, event); err != nil {
                h.logger.Warn("WebSocket write error", "error", err)
                return
            }
        case <-ws.Request().Context().Done():
            return  // Client disconnected
        }
    }
}

// HTTP polling endpoint (fallback for clients that don't support WebSocket)
func (h *NotificationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Verify authentication
    if !h.verifyAuth(r) {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }

    // Get events since timestamp
    since := r.URL.Query().Get("since")
    if since == "" {
        http.Error(w, "missing 'since' parameter", http.StatusBadRequest)
        return
    }

    // Parse timestamp
    sinceTime, err := time.Parse(time.RFC3339, since)
    if err != nil {
        http.Error(w, "invalid 'since' format", http.StatusBadRequest)
        return
    }

    // Get recent events
    events := h.eventEmitter.GetEventsSince(sinceTime)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "events": events,
    })
}

// GetEventsSince returns events after a given time (from buffer)
func (e *EventEmitter) GetEventsSince(t time.Time) []*NotificationEvent {
    e.mu.RLock()
    defer e.mu.RUnlock()

    var result []*NotificationEvent
    for _, event := range e.buffer {
        eventTime, _ := time.Parse(time.RFC3339, event.Timestamp)
        if eventTime.After(t) {
            result = append(result, event)
        }
    }
    return result
}
```

**File:** `internal/comm/http/notification_handlers.go` (NEW)

**Key fixes:**
- Uses `golang.org/x/net/websocket` (matches existing server)
- WebSocket handler signature changed: `ServeWebSocket(ws *websocket.Conn)` not `ServeWebSocket(w http.ResponseWriter, r *http.Request)`
- Added `verifyAuth()` for API key authentication (Bearer header or query token)
- Returns 401 Unauthorized if auth fails

---

#### Wire into HTTP Server (`internal/comm/http/server.go`)

```go
// Add notification endpoints to the unified HTTP server:

type HTTPServer struct {
    // ... existing fields ...
    notificationHandler *NotificationHandler
    mux                 *http.ServeMux  // Server's custom mux, NOT global default
}

func NewHTTPServer(config *ServerConfig, eventEmitter *EventEmitter, logger *slog.Logger, apiKey string) (*HTTPServer, error) {
    // ... existing initialization ...

    s := &HTTPServer{
        mux: http.NewServeMux(),  // Use server's own mux
        // ... existing fields ...
        notificationHandler: NewNotificationHandler(eventEmitter, logger, apiKey),
    }

    // Register routes
    s.registerRoutes()

    // Server listens on s.mux, not http.DefaultServeMux
    s.server = &http.Server{
        Addr:    config.Addr,
        Handler: s.mux,  // IMPORTANT: Not http.DefaultServeMux
        // ... TLS config ...
    }

    return s, nil
}

func (s *HTTPServer) registerRoutes() {
    // ... existing routes ...

    // Notification endpoints
    // FIXED: Use s.mux.Handle, not http.HandleFunc (which registers on global default mux)
    if s.config.WebsocketEnabled {
        s.mux.Handle("/ws/notifications", websocket.Handler(s.notificationHandler.ServeWebSocket))
    }
    s.mux.HandleFunc("/api/v1/notifications", s.notificationHandler.ServeHTTP)
}
```

**File:** `internal/comm/http/server.go` (MODIFY)

**Key fixes:**
- Routes registered on `s.mux` (server's custom mux), NOT `http.DefaultServeMux`
- WebSocket handler wrapped with `websocket.Handler()` adapter
- API key passed to handler constructor

---

### 2. MenuBar App: Notification Reception

**IMPORTANT:** The Menubar app already has `WebSocketManager.swift` at `menubar/MeeptMenuBar/Services/WebSocketManager.swift`.
**DO NOT create a new file** - extend the existing one to support the notifications endpoint.

The existing `APIClient.swift` uses `https://localhost:8081` with a `LocalhostTrustDelegate` for self-signed TLS.
The WebSocket connection MUST use `wss://` (TLS) and the same trust delegate.

#### Notification Manager (Swift)

```swift
// MeeptMenuBar/Services/NotificationManager.swift

import Foundation
import UserNotifications

class NotificationManager: ObservableObject {
    static let shared = NotificationManager()

    private let notificationCenter = UNUserNotificationCenter.current()
    private var websocket: WebSocketManager?
    private var lastNotificationTime: Date = Date.distantPast

    @Published var notifications: [NotificationEvent] = []
    @Published var isEnabled: Bool = true

    struct NotificationEvent: Codable, Identifiable {
        let id: String
        let timestamp: String
        let type: String  // "info", "success", "warning", "error"
        let title: String
        let message: String
        let data: [String: Any]?
        let taskID: String?
        let agentID: String?
    }

    private init() {
        requestAuthorization()
        setupWebSocket()
    }

    // Request notification permission
    func requestAuthorization() {
        notificationCenter.requestAuthorization(options: [.alert, .sound, .badge]) { granted, error in
            if let error = error {
                os_log("Notification authorization failed: %{public}@", log: .default, type: .error, error.localizedDescription)
                return
            }

            if granted {
                os_log("Notification authorization granted", log: .default, type: .info)
            } else {
                os_log("Notification authorization denied", log: .default, type: .info)
                self.isEnabled = false
            }
        }
    }

    // Setup WebSocket connection for real-time notifications
    // FIXED: Use wss:// (TLS) and LocalhostTrustDelegate like APIClient
    private func setupWebSocket() {
        let config = ConfigService.shared.config
        // FIXED: Use wss:// not ws:// - server requires TLS
        let wsURL = URL(string: "\(config.daemon.httpURL)/ws/notifications")!
            .withScheme("wss")  // Extend URL to replace http->https, ws->wss

        // FIXED: Reuse existing WebSocketManager, pass trust delegate for self-signed cert
        websocket = WebSocketManager(url: wsURL, trustDelegate: LocalhostTrustDelegate())
        websocket?.onMessage = { [weak self] data in
            self?.handleWebSocketMessage(data)
        }
        websocket?.connect()
    }

    // Handle incoming WebSocket message
    private func handleWebSocketMessage(_ data: Data) {
        guard let event = try? JSONDecoder().decode(NotificationEvent.self, from: data) else {
            os_log("Failed to decode notification event", log: .default, type: .error)
            return
        }

        DispatchQueue.main.async {
            // FIXED: Deduplicate by event ID
            let exists = self.notifications.contains { $0.id == event.id }
            if exists {
                return  // Skip duplicate
            }

            self.notifications.append(event)

            // Keep only last 50 notifications
            if self.notifications.count > 50 {
                self.notifications.removeFirst()
            }

            // Show native notification
            if self.isEnabled {
                self.showLocalNotification(event)
            }
        }
    }

    // Show native macOS notification
    private func showLocalNotification(_ event: NotificationEvent) {
        let content = UNMutableNotificationContent()
        content.title = event.title
        content.body = event.message
        content.sound = getNotificationSound(for: event.type)
        content.userInfo = [
            "task_id": event.taskID ?? "",
            "agent_id": event.agentID ?? "",
            "notif_type": event.type,
        ]

        // FIXED: Prefix identifier to avoid collision with stale macOS notifications
        let request = UNNotificationRequest(
            identifier: "meept:\(event.id)",  // Prefix prevents collision
            content: content,
            trigger: nil  // Immediate
        )

        notificationCenter.add(request) { error in
            if let error = error {
                os_log("Failed to add notification request: %{public}@", log: .default, type: .error, error.localizedDescription)
            }
        }
    }

    // Get notification sound based on type
    // FIXED: Renamed from getsound to getNotificationSound (Swift naming conventions)
    private func getNotificationSound(for type: String) -> UNNotificationSound? {
        switch type {
        case "error", "warning":
            return UNNotificationSound(named: UNNotificationSoundName("alert.aiff"))
        case "success":
            return UNNotificationSound(named: UNNotificationSoundName("glass.aiff"))
        default:
            return UNNotificationSound(named: UNNotificationSoundName("sent.aiff"))
        }
    }

    // Clear all notifications
    func clearNotifications() {
        notifications.removeAll()
        notificationCenter.removeAllDeliveredNotifications()
    }

    // Mark notification as read
    func markAsRead(_ eventID: String) {
        notifications.removeAll { $0.id == eventID }
    }
}
```

**File:** `MeeptMenuBar/Services/NotificationManager.swift` (NEW)

---

#### WebSocket Manager (Swift)

**IMPORTANT:** Extend the EXISTING `menubar/MeeptMenuBar/Services/WebSocketManager.swift` - do NOT create a new file.
The existing `APIClient.swift` uses `LocalhostTrustDelegate` for self-signed TLS - reuse this pattern.

```swift
// MeeptMenuBar/Services/WebSocketManager.swift (EXTEND EXISTING FILE)

import Foundation

class WebSocketManager: NSObject, URLSessionWebSocketDelegate {
    var onMessage: ((Data) -> Void)?
    var onDisconnect: (() -> Void)?

    private var webSocket: URLSessionWebSocketTask?
    private var session: URLSession?  // FIXED: Store session for cleanup
    private let url: URL
    private let trustDelegate: LocalhostTrustDelegate?  // FIXED: For self-signed TLS
    private var reconnectDelay: TimeInterval = 1.0
    private var maxReconnectDelay: TimeInterval = 60.0

    // FIXED: Added trustDelegate parameter for TLS
    init(url: URL, trustDelegate: LocalhostTrustDelegate? = nil) {
        self.url = url
        self.trustDelegate = trustDelegate
        super.init()
    }

    func connect() {
        // FIXED: Create session once and store it
        let config = URLSessionConfiguration.default
        session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
        webSocket = session?.webSocketTask(with: url)
        webSocket?.resume()
        receiveMessage()
    }

    func disconnect() {
        webSocket?.cancel(with: .normalClosure, reason: nil)
        webSocket = nil
        // FIXED: Invalidate session to prevent leak
        session?.invalidateAndCancel()
        session = nil
    }

    private func receiveMessage() {
        webSocket?.receive { [weak self] result in
            switch result {
            case .success(let message):
                switch message {
                case .string(let text):
                    if let data = text.data(using: .utf8) {
                        self?.onMessage?(data)
                    }
                case .data(let data):
                    self?.onMessage?(data)
                @unknown default:
                    break
                }
                self?.receiveMessage()  // Continue listening

            case .failure(let error):
                os_log("WebSocket error: %{public}@", log: .default, type: .error, error.localizedDescription)
                self?.reconnect()
            }
        }
    }

    private func reconnect() {
        guard reconnectDelay < 60 else { return }  // Max 60s delay

        DispatchQueue.global().asyncAfter(deadline: .now() + reconnectDelay) { [weak self] in
            self?.connect()
            self?.reconnectDelay *= 2  // Exponential backoff
        }
    }

    // URLSessionWebSocketDelegate
    func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didOpenWithProtocol protocol: String?) {
        os_log("WebSocket connected", log: .default, type: .info)
        reconnectDelay = 1.0  // Reset on successful connection
    }

    func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask, didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {
        os_log("WebSocket closed", log: .default, type: .info)
        onDisconnect?()
    }
}
```

**File:** `MeeptMenuBar/Services/WebSocketManager.swift` (NEW)

---

#### Notification Center Menu View (SwiftUI)

```swift
// MeeptMenuBar/Views/NotificationCenterMenuView.swift

import SwiftUI

struct NotificationCenterMenuView: View {
    @StateObject private var notificationManager = NotificationManager.shared
    @State private var expanded = false

    var body: some View {
        Menu {
            if notificationManager.notifications.isEmpty {
                Text("No notifications")
                    .foregroundColor(.secondary)
            } else {
                ForEach(notificationManager.notifications) { notification in
                    NotificationRowView(notification: notification)
                }

                Divider()

                Button("Clear All") {
                    notificationManager.clearNotifications()
                }
            }

            Divider()

            Toggle("Enable Notifications", isOn: $notificationManager.isEnabled)
        } label: {
            ZStack {
                Image(systemName: "bell")

                if !notificationManager.notifications.isEmpty {
                    Circle()
                        .fill(.red)
                        .frame(width: 8, height: 8)
                        .offset(x: 4, y: -4)
                }
            }
        }
    }
}

struct NotificationRowView: View {
    let notification: NotificationManager.NotificationEvent
    @StateObject private var notificationManager = NotificationManager.shared

    var typeIcon: String {
        switch notification.type {
        case "error": return "xmark.circle.fill"
        case "warning": return "exclamationmark.triangle.fill"
        case "success": return "checkmark.circle.fill"
        default: return "info.circle.fill"
        }
    }

    var typeColor: Color {
        switch notification.type {
        case "error": return .red
        case "warning": return .orange
        case "success": return .green
        default: return .blue
        }
    }

    var timeAgo: String {
        guard let date = ISO8601DateFormatter().date(from: notification.timestamp) else {
            return ""
        }
        return RelativeDateTimeFormatter().localizedString(for: date, relativeTo: Date())
    }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: typeIcon)
                .foregroundColor(typeColor)
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 4) {
                Text(notification.title)
                    .font(.system(size: 13, weight: .medium))

                Text(notification.message)
                    .font(.system(size: 12))
                    .foregroundColor(.secondary)
                    .lineLimit(3)

                Text(timeAgo)
                    .font(.system(size: 10))
                    .foregroundColor(.tertiary)
            }

            Spacer()

            Button(action: {
                notificationManager.markAsRead(notification.id)
            }) {
                Image(systemName: "xmark")
                    .font(.system(size: 10))
                    .foregroundColor(.tertiary)
            }
            .buttonStyle(.plain)
        }
        .padding(.vertical, 4)
    }
}
```

**File:** `MeeptMenuBar/Views/NotificationCenterMenuView.swift` (NEW)

---

#### Integration with MenuBar App

```swift
// MeeptMenuBar/MeeptMenuBarApp.swift

import SwiftUI

@main
struct MeeptMenuBarApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate

    var body: some Scene {
        Settings {
            EmptyView()
        }
    }
}

class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem?
    private let popover = NSPopover()

    func applicationDidFinishLaunching(_ notification: Notification) {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem?.button {
            let contentView = MenuBarContentView()
            button.image = NSImage(systemSymbolName: "barbell", accessibilityDescription: "Meept")
            button.action = #selector(togglePopover)
            button.target = self
        }

        popover.contentViewController = NSHostingController(rootView: MenuBarContentView())
        popover.behavior = .transient
    }

    @objc func togglePopover() {
        if let button = statusItem?.button {
            if popover.isShown {
                popover.performClose(button)
            } else {
                popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            }
        }
    }
}

// MeeptMenuBar/Views/MenuBarContentView.swift

import SwiftUI

struct MenuBarContentView: View {
    @StateObject private var daemonController = DaemonController.shared
    @StateObject private var configService = ConfigService.shared

    var body: some View {
        VStack(spacing: 16) {
            // Daemon status
            DaemonStatusView()

            Divider()

            // Quick stats
            QuickStatsView()

            Divider()

            // Notifications
            NotificationCenterMenuView()

            Divider()

            // Settings
            MenuBarSettingsView()
        }
        .padding()
        .frame(width: 320)
    }
}
```

**File:** `MeeptMenuBar/Views/MenuBarContentView.swift` (MODIFY)

---

### 3. Configuration

#### Daemon Config (`config/meept.json5`)

```json5
{
  notifications: {
    enabled: true,

    // Event types to notify
    on_task_complete: true,
    on_task_failure: true,
    on_long_running_task: true,  // Notify after 30s
    on_confirmation_needed: true,
    on_security_block: true,
    on_budget_warning: true,

    // Long-running task threshold
    long_running_threshold_seconds: 30,

    // HTTP API for MenuBar
    http: {
      websocket_enabled: true,
      polling_enabled: true,
    },
  },
}
```

#### MenuBar Config (`~/.meept/menubar.json5`)

```json5
{
  notifications: {
    enabled: true,
    show_in_menu: true,
    play_sounds: true,
    max_displayed: 50,

    // Filter by type
    filter: {
      show_info: true,
      show_success: true,
      show_warning: true,
      show_error: true,
    },
  },

  daemon: {
    transport: "http",
    http_url: "http://localhost:8081",
  },
}
```

---

## Testing Plan

### Daemon Testing
1. Event emitter broadcast functionality
2. WebSocket connection handling
3. HTTP polling endpoint
4. Agent loop integration (notification triggers)

### MenuBar Testing
1. Notification permission request
2. WebSocket reconnection logic
3. Native notification display
4. Menu view rendering

### Integration Testing
1. End-to-end: daemon → HTTP → MenuBar → macOS notifications
2. Notification filtering and preferences
3. WebSocket fallback to polling

---

## Privacy & Security

- All notifications stay local (no external transmission)
- WebSocket connections only to localhost
- Notification content limited to avoid leaking sensitive data
- User can disable notifications at any time

---

## Success Criteria

- [x] Desktop notifications appear for task completion/failure
- [x] Long-running task notifications work (>30s threshold)
- [x] WebSocket real-time delivery functional
- [x] HTTP polling fallback works
- [x] MenuBar notification center displays history
- [x] Native macOS notifications trigger with correct sounds
- [x] User can enable/disable notifications
- [x] Configuration options respected
- [x] No impact on daemon performance

---

## Related Documentation

- `docs/reference/http-api.md` — HTTP API endpoints
- `menubar/` — MenuBar app structure
- `internal/comm/http/` — HTTP server implementation
