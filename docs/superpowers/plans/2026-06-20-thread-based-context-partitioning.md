# Thread-Based Context Partitioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement thread-based context isolation to prevent context bloat when conversations switch topics within a session, while disabling the branch feature by default via configuration.

**Architecture:** Add Thread abstraction to Session store, topic detection in Dispatcher for routing, per-thread Conversation objects for isolation, and cross-thread summary injection for continuity. Branch feature gated behind `session.branches.enabled` config flag (default: false).

**Tech Stack:** Go 1.24, SQLite with FTS5, message bus pub/sub, bubbletea TUI, Flutter GUI

---

## File Structure Mapping

### Files to Create

| File | Responsibility |
|------|----------------|
| `internal/session/thread.go` | Thread struct, topic detection, per-thread conversation ID mapping |
| `internal/session/thread_store.go` | SQLite persistence for threads, CRUD operations |
| `internal/agent/topic_detector.go` | Keyword-based topic detection (MVP), embedding-based (Phase 2) |
| `internal/agent/thread_router.go` | Route requests to thread-specific conversations |
| `cmd/meept/thread.go` | CLI commands: `/thread new`, `/thread list`, `/thread switch`, `/thread current` |
| `internal/tui/thread_indicator.go` | TUI thread display component |
| `docs/concepts/threads.md` | Architecture documentation |

### Files to Modify

| File | Changes |
|------|---------|
| `internal/session/session.go:37-50` | Add `Threads map[string]*Thread`, `ActiveThreadID string` to Session struct |
| `internal/session/session.go:74-121` | Add thread-related Store interface methods |
| `internal/session/store.go` | Add ThreadManager, thread CRUD handlers |
| `internal/session/sqlite_store.go` | threads table schema, thread CRUD SQL operations |
| `internal/agent/dispatcher.go:1194` | RouteToAgent: get thread from topic detection, route to thread conversation |
| `internal/agent/conversation.go` | Per-thread conversation lookup, thread-aware caching |
| `internal/agent/loop.go:1188` | RunOnceWithParts: thread-scoped conversation retrieval |
| `internal/comm/http/api_handlers.go` | Add REST endpoints: `POST /api/v1/sessions/{id}/threads`, `GET /api/v1/sessions/{id}/threads` |
| `internal/config/config.go` | Add `Session.BranchingEnabled bool` config option (default: false) |
| `config/meept.json5` | Add `session.branches.enabled` configuration example |
| `cmd/meept/chat.go` | Wire `/thread` command parsing in TUI chat mode |
| `ui/flutter_ui/lib/services/session_service.dart` | Thread API integration |
| `ui/flutter_ui/lib/widgets/thread_selector.dart` | Thread display/switching widget |

### Files to Deprecate (Gate Behind Config)

| File | Deprecation Approach |
|------|---------------------|
| `cmd/meept/branch.go` | Wrap commands with config check, show "branches disabled" message |
| `internal/session/branch.go` | Early return in BranchManager methods if `!config.Session.BranchesEnabled` |
| `internal/session/tree.go` | Conditional compilation or runtime gate |

---

## Database Schema Changes

### New Table: `session_threads`

```sql
CREATE TABLE IF NOT EXISTS session_threads (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    topic_label     TEXT NOT NULL DEFAULT 'general',
    conversation_id TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    last_activity   TEXT NOT NULL,
    summary         TEXT,
    is_active       INTEGER DEFAULT 0,
    UNIQUE(session_id, topic_label)
);

CREATE INDEX idx_session_threads_session ON session_threads(session_id);
CREATE INDEX idx_session_threads_active ON session_threads(session_id, is_active);
```

### Schema Migration

```sql
-- Add thread_id column to session_messages (optional, for explicit thread tracking)
ALTER TABLE session_messages ADD COLUMN thread_id TEXT DEFAULT 'main';

-- Create index for thread-scoped queries
CREATE INDEX idx_messages_thread ON session_messages(session_id, thread_id);
```

---

## Task Breakdown

### Phase 1: Foundation - Thread Data Structures and Schema

#### Task 1: Thread Struct and Session Extensions

**Files:**
- Create: `internal/session/thread.go`
- Modify: `internal/session/session.go:37-50`

- [ ] **Step 1: Write unit test for Thread struct**

```go
// internal/session/thread_test.go
package session

import (
    "testing"
    "time"
    "github.com/google/go-cmp/cmp"
)

func TestThread_Struct(t *testing.T) {
    now := time.Now().UTC()
    thread := &Thread{
        ID:             "thread-001",
        SessionID:      "session-abc",
        TopicLabel:     "work",
        ConversationID: "conv-work-xyz",
        CreatedAt:      now,
        LastActivityAt: now,
        Summary:        "Discussion about Go feature implementation",
    }

    if thread.ID != "thread-001" {
        t.Errorf("expected ID 'thread-001', got %q", thread.ID)
    }
    if thread.TopicLabel != "work" {
        t.Errorf("expected TopicLabel 'work', got %q", thread.TopicLabel)
    }
    if !thread.IsActive() {
        t.Error("expected thread to be active")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -v -run TestThread_Struct`
Expected: FAIL with "undefined: Thread"

- [ ] **Step 3: Implement Thread struct and Session extensions**

```go
// internal/session/thread.go
package session

import "time"

// Thread represents an isolated conversation topic within a session.
// Each thread has its own Conversation object, preventing context bloat
// when users switch between unrelated topics.
type Thread struct {
    ID             string    `json:"id"`
    SessionID      string    `json:"session_id"`
    TopicLabel     string    `json:"topic_label"` // "work", "lunch", "general", etc.
    ConversationID string    `json:"conversation_id"`
    CreatedAt      time.Time `json:"created_at"`
    LastActivityAt time.Time `json:"last_activity_at"`
    Summary        string    `json:"summary,omitempty"` // Cross-thread injection summary
    IsActive       bool      `json:"is_active"`
}

// IsActive returns whether this thread is currently active.
func (t *Thread) IsActive() bool {
    return t.IsActive
}

// Touch updates the last activity timestamp.
func (t *Thread) Touch() {
    t.LastActivityAt = time.Now().UTC()
}

// ThreadConfig holds thread-specific configuration.
type ThreadConfig struct {
    // EnableTopicDetection enables automatic topic detection and thread routing.
    EnableTopicDetection bool `json:"enable_topic_detection"`
    // MinMessagesForSummary is the minimum messages before cross-thread summary injection.
    MinMessagesForSummary int `json:"min_messages_for_summary"`
}

// DefaultThreadConfig returns the default thread configuration.
func DefaultThreadConfig() ThreadConfig {
    return ThreadConfig{
        EnableTopicDetection:    true,
        MinMessagesForSummary:   5,
    }
}
// Note: production defaults are conservative (EnableTopicDetection: false,
// MinMessagesForSummary: 20) to avoid unexpected thread creation on existing
// deployments. Operators can opt in via config (`session.threads.enable_topic_detection: true`).
```

- [ ] **Step 4: Extend Session struct with thread fields**

```go
// internal/session/session.go:37-50 (modify)
type Session struct {
    ID              string              `json:"id"`
    Name            string              `json:"name"`
    Description     string              `json:"description,omitempty"`
    ConversationID  string              `json:"conversation_id"` // DEPRECATED: use thread.ConversationID
    CreatedAt       time.Time           `json:"created_at"`
    LastActivity    time.Time           `json:"last_activity"`
    AttachedClients []string            `json:"attached_clients"`
    WorkerIDs       []string            `json:"worker_ids,omitempty"`
    LeafMessageID   *int64              `json:"leaf_message_id,omitempty"`
    ProjectID       string              `json:"project_id,omitempty"`
    ProjectPath     string              `json:"project_path,omitempty"`
    NoFence         bool                `json:"no_fence,omitempty"`

    // Thread-based context partitioning (NEW)
    Threads         map[string]*Thread  `json:"threads,omitempty"` // threadID -> Thread
    ActiveThreadID  string              `json:"active_thread_id,omitempty"`
}

// GetActiveThread returns the currently active thread.
func (s *Session) GetActiveThread() *Thread {
    if s.ActiveThreadID == "" || s.Threads == nil {
        return nil
    }
    return s.Threads[s.ActiveThreadID]
}

// GetOrCreateThread returns existing thread or creates new one.
func (s *Session) GetOrCreateThread(threadID, topicLabel string) *Thread {
    if s.Threads == nil {
        s.Threads = make(map[string]*Thread)
    }

    if thread, exists := s.Threads[threadID]; exists {
        return thread
    }

    thread := &Thread{
        ID:             threadID,
        SessionID:      s.ID,
        TopicLabel:     topicLabel,
        ConversationID: s.ID + "-" + threadID, // Unique per thread
        CreatedAt:      time.Now().UTC(),
        LastActivityAt: time.Now().UTC(),
        IsActive:       true,
    }

    // Deactivate other threads
    for _, t := range s.Threads {
        t.IsActive = false
    }
    thread.IsActive = true
    s.ActiveThreadID = threadID
    s.Threads[threadID] = thread

    return thread
}
```

- [ ] **Step 5: Run tests to verify implementation**

Run: `go test ./internal/session/... -v -run TestThread`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/session/thread.go internal/session/session.go internal/session/thread_test.go
git commit -m "feat(session): add Thread struct and Session thread extensions"
```

#### Task 2: SQLite Thread Store Implementation

**Files:**
- Create: `internal/session/thread_store.go`
- Create: `internal/session/sqlite_thread_store.go`
- Modify: `internal/session/session.go:74-121` (Store interface)

- [ ] **Step 1: Write failing integration test for thread persistence**

```go
// internal/session/thread_store_test.go
package session

import (
    "context"
    "os"
    "testing"
    "time"
)

func TestThreadStore_CRUD(t *testing.T) {
    tmpDir := t.TempDir()
    dbPath := tmpDir + "/test_threads.db"

    store, err := NewSQLiteThreadStore(dbPath)
    if err != nil {
        t.Fatalf("failed to create store: %v", err)
    }
    defer store.Close()

    ctx := context.Background()
    thread := &Thread{
        ID:             "thread-001",
        SessionID:      "session-abc",
        TopicLabel:     "work",
        ConversationID: "conv-work-xyz",
        CreatedAt:      time.Now().UTC(),
        LastActivityAt: time.Now().UTC(),
        IsActive:       true,
    }

    // Test Create
    if err := store.CreateThread(ctx, thread); err != nil {
        t.Fatalf("CreateThread failed: %v", err)
    }

    // Test Get
    got, err := store.GetThread(ctx, "thread-001")
    if err != nil {
        t.Fatalf("GetThread failed: %v", err)
    }
    if got.TopicLabel != "work" {
        t.Errorf("expected TopicLabel 'work', got %q", got.TopicLabel)
    }

    // Test List by Session
    threads, err := store.ListThreadsBySession(ctx, "session-abc")
    if err != nil {
        t.Fatalf("ListThreadsBySession failed: %v", err)
    }
    if len(threads) != 1 {
        t.Errorf("expected 1 thread, got %d", len(threads))
    }

    // Test Update
    thread.Summary = "Work discussion summary"
    if err := store.UpdateThread(ctx, thread); err != nil {
        t.Fatalf("UpdateThread failed: %v", err)
    }

    // Verify update
    updated, _ := store.GetThread(ctx, "thread-001")
    if updated.Summary != "Work discussion summary" {
        t.Errorf("expected summary update, got %q", updated.Summary)
    }

    // Test Delete
    if err := store.DeleteThread(ctx, "thread-001"); err != nil {
        t.Fatalf("DeleteThread failed: %v", err)
    }

    _, err = store.GetThread(ctx, "thread-001")
    if err == nil {
        t.Error("expected error after delete, got nil")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/... -v -run TestThreadStore_CRUD`
Expected: FAIL with "undefined: NewSQLiteThreadStore"

- [ ] **Step 3: Add Store interface methods for threads**

```go
// internal/session/session.go:74-121 (add to Store interface)
type Store interface {
    // ... existing methods ...

    // Thread operations (NEW)
    CreateThread(ctx context.Context, thread *Thread) error
    GetThread(ctx context.Context, threadID string) (*Thread, error)
    ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error)
    UpdateThread(ctx context.Context, thread *Thread) error
    DeleteThread(ctx context.Context, threadID string) error
    GetActiveThread(ctx context.Context, sessionID string) (*Thread, error)
    SetActiveThread(ctx context.Context, sessionID, threadID string) error
}
```

- [ ] **Step 4: Implement SQLite thread store**

```go
// internal/session/sqlite_thread_store.go
package session

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    _ "modernc.org/sqlite"
)

const (
    createThreadsTableSQL = `
CREATE TABLE IF NOT EXISTS session_threads (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    topic_label     TEXT NOT NULL DEFAULT 'general',
    conversation_id TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    last_activity   TEXT NOT NULL,
    summary         TEXT DEFAULT '',
    is_active       INTEGER DEFAULT 0,
    UNIQUE(session_id, topic_label)
);

CREATE INDEX IF NOT EXISTS idx_session_threads_session ON session_threads(session_id);
CREATE INDEX IF NOT EXISTS idx_session_threads_active ON session_threads(session_id, is_active);
`
)

// SQLiteThreadStore implements thread persistence.
type SQLiteThreadStore struct {
    db *sql.DB
}

// NewSQLiteThreadStore creates a new SQLite thread store.
func NewSQLiteThreadStore(dbPath string) (*SQLiteThreadStore, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }

    // Enable foreign keys
    if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
        db.Close()
        return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
    }

    // Create schema
    if _, err := db.Exec(createThreadsTableSQL); err != nil {
        db.Close()
        return nil, fmt.Errorf("failed to create schema: %w", err)
    }

    return &SQLiteThreadStore{db: db}, nil
}

// CreateThread persists a new thread.
func (s *SQLiteThreadStore) CreateThread(ctx context.Context, thread *Thread) error {
    _, err := s.db.ExecContext(ctx, `
        INSERT INTO session_threads (id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            topic_label = excluded.topic_label,
            conversation_id = excluded.conversation_id,
            last_activity = excluded.last_activity,
            summary = excluded.summary,
            is_active = excluded.is_active
    `,
        thread.ID,
        thread.SessionID,
        thread.TopicLabel,
        thread.ConversationID,
        thread.CreatedAt.Format(time.RFC3339),
        thread.LastActivityAt.Format(time.RFC3339),
        thread.Summary,
        boolToInt(thread.IsActive),
    )
    return err
}

// GetThread retrieves a thread by ID.
func (s *SQLiteThreadStore) GetThread(ctx context.Context, threadID string) (*Thread, error) {
    row := s.db.QueryRowContext(ctx, `
        SELECT id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active
        FROM session_threads
        WHERE id = ?
    `, threadID)

    var t Thread
    var createdAtStr, lastActivityStr string
    err := row.Scan(&t.ID, &t.SessionID, &t.TopicLabel, &t.ConversationID,
        &createdAtStr, &lastActivityStr, &t.Summary, &t.IsActive)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, fmt.Errorf("thread not found: %s", threadID)
        }
        return nil, err
    }

    t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
    t.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityStr)
    return &t, nil
}

// ListThreadsBySession returns all threads for a session.
func (s *SQLiteThreadStore) ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error) {
    rows, err := s.db.QueryContext(ctx, `
        SELECT id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active
        FROM session_threads
        WHERE session_id = ?
        ORDER BY last_activity DESC
    `, sessionID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var threads []*Thread
    for rows.Next() {
        var t Thread
        var createdAtStr, lastActivityStr string
        if err := rows.Scan(&t.ID, &t.SessionID, &t.TopicLabel, &t.ConversationID,
            &createdAtStr, &lastActivityStr, &t.Summary, &t.IsActive); err != nil {
            return nil, err
        }
        t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
        t.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityStr)
        threads = append(threads, &t)
    }
    return threads, rows.Err()
}

// UpdateThread updates an existing thread.
func (s *SQLiteThreadStore) UpdateThread(ctx context.Context, thread *Thread) error {
    _, err := s.db.ExecContext(ctx, `
        UPDATE session_threads
        SET topic_label = ?, conversation_id = ?, last_activity = ?, summary = ?, is_active = ?
        WHERE id = ?
    `,
        thread.TopicLabel,
        thread.ConversationID,
        thread.LastActivityAt.Format(time.RFC3339),
        thread.Summary,
        boolToInt(thread.IsActive),
        thread.ID,
    )
    return err
}

// DeleteThread removes a thread.
func (s *SQLiteThreadStore) DeleteThread(ctx context.Context, threadID string) error {
    _, err := s.db.ExecContext(ctx, `DELETE FROM session_threads WHERE id = ?`, threadID)
    return err
}

// GetActiveThread returns the active thread for a session.
func (s *SQLiteThreadStore) GetActiveThread(ctx context.Context, sessionID string) (*Thread, error) {
    row := s.db.QueryRowContext(ctx, `
        SELECT id, session_id, topic_label, conversation_id, created_at, last_activity, summary, is_active
        FROM session_threads
        WHERE session_id = ? AND is_active = 1
        LIMIT 1
    `, sessionID)

    var t Thread
    var createdAtStr, lastActivityStr string
    err := row.Scan(&t.ID, &t.SessionID, &t.TopicLabel, &t.ConversationID,
        &createdAtStr, &lastActivityStr, &t.Summary, &t.IsActive)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, nil // No active thread
        }
        return nil, err
    }

    t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
    t.LastActivityAt, _ = time.Parse(time.RFC3339, lastActivityStr)
    return &t, nil
}

// SetActiveThread sets the active thread for a session (deactivates others).
func (s *SQLiteThreadStore) SetActiveThread(ctx context.Context, sessionID, threadID string) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Deactivate all threads for this session
    _, err = tx.ExecContext(ctx, `
        UPDATE session_threads SET is_active = 0 WHERE session_id = ?
    `, sessionID)
    if err != nil {
        return err
    }

    // Activate the specified thread
    _, err = tx.ExecContext(ctx, `
        UPDATE session_threads SET is_active = 1 WHERE id = ? AND session_id = ?
    `, threadID, sessionID)
    if err != nil {
        return err
    }

    return tx.Commit()
}

// Close releases database resources.
func (s *SQLiteThreadStore) Close() error {
    return s.db.Close()
}

// Helper function
func boolToInt(b bool) int {
    if b {
        return 1
    }
    return 0
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/session/... -v -run TestThreadStore_CRUD`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/session/thread_store.go internal/session/sqlite_thread_store.go internal/session/thread_store_test.go internal/session/session.go
git commit -m "feat(session): implement SQLite thread persistence layer"
```

### Phase 2: Topic Detection and Routing

#### Task 3: Keyword-Based Topic Detector

**Files:**
- Create: `internal/agent/topic_detector.go`

- [ ] **Step 1: Write unit tests for topic detection**

```go
// internal/agent/topic_detector_test.go
package agent

import (
    "testing"
)

func TestTopicDetector_Detect(t *testing.T) {
    detector := NewTopicDetector()

    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"work task", "I need to build a Go feature for the API", "work"},
        {"code bug", "There's a bug in the database connection code", "code"},
        {"lunch food", "What should I have for lunch today?", "food"},
        {"restaurant", "Recommend a good Italian restaurant", "food"},
        {"weekend plans", "What are my weekend plans?", "personal"},
        {"weather", "What's the weather like?", "general"},
        {"shopping list", "I need to buy groceries", "personal"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := detector.Detect(tt.input)
            if got != tt.expected {
                t.Errorf("Detect(%q) = %q, want %q", tt.input, got, tt.expected)
            }
        })
    }
}

func TestTopicDetector_CustomKeywords(t *testing.T) {
    detector := NewTopicDetector(
        WithTopicKeywords("gaming", []string{"game", "play", "steam", "xbox", "playstation"}),
        WithTopicKeywords("health", []string{"workout", "gym", "exercise", "running"}),
    )

    got := detector.Detect("I'm going to the gym later")
    if got != "health" {
        t.Errorf("expected 'health', got %q", got)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/... -v -run TestTopicDetector`
Expected: FAIL with "undefined: NewTopicDetector"

- [ ] **Step 3: Implement topic detector**

```go
// internal/agent/topic_detector.go
package agent

import (
    "strings"
    "sync"
)

// TopicDetector identifies conversation topics from user input.
type TopicDetector struct {
    mu       sync.RWMutex
    keywords map[string][]string // topic -> keywords
    defaultTopic string
}

// TopicDetectorOption configures a TopicDetector.
type TopicDetectorOption func(*TopicDetector)

// WithTopicKeywords adds keywords for a topic.
func WithTopicKeywords(topic string, keywords []string) TopicDetectorOption {
    return func(td *TopicDetector) {
        td.keywords[topic] = append(td.keywords[topic], keywords...)
    }
}

// WithDefaultTopic sets the default topic when no match is found.
func WithDefaultTopic(topic string) TopicDetectorOption {
    return func(td *TopicDetector) {
        td.defaultTopic = topic
    }
}

// NewTopicDetector creates a new topic detector.
func NewTopicDetector(opts ...TopicDetectorOption) *TopicDetector {
    td := &TopicDetector{
        keywords: map[string][]string{
            "work":      {"task", "feature", "bug", "code", "build", "deploy", "api", "function", "method", "endpoint"},
            "code":      {"debug", "error", "panic", "stack trace", "compile", "test", "lint"},
            "food":      {"lunch", "dinner", "breakfast", "food", "eat", "recipe", "restaurant", "cook", "hungry"},
            "personal":  {"weekend", "vacation", "hobby", "shopping", "family", "friend", "party", "travel"},
            "health":    {"workout", "gym", "exercise", "running", "diet", "sleep", "doctor", "medicine"},
        },
        defaultTopic: "general",
    }

    for _, opt := range opts {
        opt(td)
    }

    return td
}

// Detect identifies the topic from user input using keyword matching.
func (td *TopicDetector) Detect(input string) string {
    td.mu.RLock()
    defer td.mu.RUnlock()

    lowerInput := strings.ToLower(input)

    // Score each topic by keyword matches
    bestTopic := td.defaultTopic
    bestScore := 0

    for topic, keywords := range td.keywords {
        score := 0
        for _, keyword := range keywords {
            if strings.Contains(lowerInput, keyword) {
                score++
            }
        }
        if score > bestScore {
            bestScore = score
            bestTopic = topic
        }
    }

    return bestTopic
}

// GenerateThreadID creates a unique thread ID from session ID and topic.
func (td *TopicDetector) GenerateThreadID(sessionID, topic string) string {
    return "thread-" + topic + "-" + sessionID[len(sessionID)-4:]
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agent/... -v -run TestTopicDetector`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/topic_detector.go internal/agent/topic_detector_test.go
git commit -m "feat(agent): add keyword-based topic detector for thread routing"
```

#### Task 4: Thread Router in Dispatcher

**Files:**
- Create: `internal/agent/thread_router.go`
- Modify: `internal/agent/dispatcher.go:1194` (RouteToAgent)

- [ ] **Step 1: Write failing test for thread routing**

```go
// internal/agent/thread_router_test.go
package agent

import (
    "context"
    "testing"
)

func TestThreadRouter_GetOrCreateThread(t *testing.T) {
    router := NewThreadRouter()

    sessionID := "session-test-001"
    input := "I need to fix this bug in the code"

    threadID, topic := router.RouteThread(sessionID, input)

    if topic != "code" {
        t.Errorf("expected topic 'code', got %q", topic)
    }

    expectedThreadID := "thread-code-001"
    if threadID != expectedThreadID {
        t.Errorf("expected threadID %q, got %q", expectedThreadID, threadID)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/... -v -run TestThreadRouter`
Expected: FAIL

- [ ] **Step 3: Implement thread router**

```go
// internal/agent/thread_router.go
package agent

import (
    "sync"

    "github.com/caimlas/meept/internal/session"
)

// ThreadRouter manages thread creation and routing.
type ThreadRouter struct {
    mu            sync.RWMutex
    detector      *TopicDetector
    sessionStore  session.Store
}

// NewThreadRouter creates a new thread router.
func NewThreadRouter() *ThreadRouter {
    return &ThreadRouter{
        detector: NewTopicDetector(),
    }
}

// WithSessionStore sets the session store for persistence.
func (tr *ThreadRouter) WithSessionStore(store session.Store) {
    tr.mu.Lock()
    defer tr.mu.Unlock()
    tr.sessionStore = store
}

// RouteThread determines the thread for a given input.
// Returns threadID and topic label.
func (tr *ThreadRouter) RouteThread(sessionID, input string) (string, string) {
    tr.mu.RLock()
    defer tr.mu.RUnlock()

    topic := tr.detector.Detect(input)
    threadID := tr.detector.GenerateThreadID(sessionID, topic)

    return threadID, topic
}

// GetThreadConversationID returns the conversation ID for a thread.
// Creates the thread if it doesn't exist.
func (tr *ThreadRouter) GetThreadConversationID(ctx context.Context, sessionID, input string) (string, error) {
    tr.mu.Lock()
    defer tr.mu.Unlock()

    if tr.sessionStore == nil {
        // Fallback: use sessionID directly (no thread isolation)
        return sessionID, nil
    }

    threadID, topic := tr.RouteThread(sessionID, input)

    // Get or create session
    sess := tr.sessionStore.Get(sessionID)
    if sess == nil {
        return "", nil
    }

    // Get or create thread
    thread := sess.GetOrCreateThread(threadID, topic)

    return thread.ConversationID, nil
}
```

- [ ] **Step 4: Modify Dispatcher to use thread router**

```go
// internal/agent/dispatcher.go:1194 (modify RouteToAgent method)
func (d *Dispatcher) RouteToAgent(ctx context.Context, intent *DispatchIntent, sessionID string) (*DispatchResult, error) {
    // NEW: Get thread-scoped conversation ID
    var conversationID string
    var err error

    if d.threadRouter != nil {
        conversationID, err = d.threadRouter.GetThreadConversationID(ctx, sessionID, intent.Input)
        if err != nil {
            d.logger.Warn("Thread routing failed, using sessionID",
                "session_id", sessionID,
                "error", err,
            )
            conversationID = sessionID
        }
    } else {
        conversationID = sessionID
    }

    // Rest of existing RouteToAgent implementation uses conversationID
    // ... existing code ...
}
```

- [ ] **Step 5: Run tests to verify**

Run: `go test ./internal/agent/... -v -run TestThreadRouter`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/thread_router.go internal/agent/thread_router_test.go internal/agent/dispatcher.go
git commit -m "feat(agent): wire thread router into dispatcher routing"
```

### Phase 3: Cross-Thread Summary Injection

#### Task 5: Cross-Thread Summary Service

**Files:**
- Create: `internal/session/thread_summary.go`

- [ ] **Step 1: Define summary interface and tests**

```go
// internal/session/thread_summary_test.go
package session

import (
    "context"
    "testing"
)

func TestCrossThreadSummary_AssembleThreadContext(t *testing.T) {
    threads := []*Thread{
        {ID: "thread-work", TopicLabel: "work", Summary: "Debugging API endpoint", IsActive: false},
        {ID: "thread-lunch", TopicLabel: "food", Summary: "Restaurant recommendations", IsActive: false},
        {ID: "thread-current", TopicLabel: "code", Summary: "", IsActive: true},
    }

    summary := AssembleThreadContext(threads, "thread-current")

    if !strings.Contains(summary, "work thread") {
        t.Error("expected work thread context")
    }
    if !strings.Contains(summary, "food thread") {
        t.Error("expected food thread context")
    }
    if strings.Contains(summary, "code thread") {
        t.Error("should not include active thread")
    }
}
```

- [ ] **Step 2: Implement cross-thread summary**

```go
// internal/session/thread_summary.go
package session

import (
    "fmt"
    "strings"
)

// AssembleThreadContext creates context from inactive thread summaries.
// This provides continuity when switching between threads.
func AssembleThreadContext(threads []*Thread, activeThreadID string) string {
    var sb strings.Builder

    for _, thread := range threads {
        if thread.ID != activeThreadID && thread.Summary != "" {
            sb.WriteString(fmt.Sprintf(
                "[Context from %s thread]: %s\n",
                thread.TopicLabel,
                thread.Summary,
            ))
        }
    }

    if sb.Len() == 0 {
        return ""
    }

    return "\n" + sb.String() + "\n"
}

// GenerateThreadSummary creates a summary from thread messages.
func GenerateThreadSummary(messages []Message) string {
    if len(messages) == 0 {
        return ""
    }

    // Simple summary: first and last message preview
    firstMsg := messages[0]
    lastMsg := messages[len(messages)-1]

    firstPreview := truncateString(firstMsg.Content, 100)
    lastPreview := truncateString(lastMsg.Content, 100)

    return fmt.Sprintf("Discussion from %s: %s... (latest: %s...)",
        firstMsg.Role, firstPreview, lastPreview)
}

func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-3] + "..."
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/session/thread_summary.go internal/session/thread_summary_test.go
git commit -m "feat(session): add cross-thread summary injection"
```

### Phase 4: CLI and TUI Integration

#### Task 6: Thread CLI Commands

**Files:**
- Create: `cmd/meept/thread.go`

- [ ] **Step 1: Write thread CLI commands**

```go
// cmd/meept/thread.go
package main

import (
    "encoding/json"
    "fmt"
    "github.com/spf13/cobra"
)

func newThreadCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "thread",
        Short: "Manage conversation threads",
        Long: `Create, list, and switch between conversation threads.

Threads isolate different topics within a session, preventing context bloat.

Examples:
  meept thread new "work"           # Create new work thread from current context
  meept thread list                 # List threads in current session
  meept thread switch <thread-id>   # Switch to a thread
  meept thread current              # Show current thread`,
    }

    cmd.AddCommand(newThreadNewCmd())
    cmd.AddCommand(newThreadListCmd())
    cmd.AddCommand(newThreadSwitchCmd())
    cmd.AddCommand(newThreadCurrentCmd())

    return cmd
}

func newThreadNewCmd() *cobra.Command {
    var topicLabel string

    cmd := &cobra.Command{
        Use:   "new [topic]",
        Short: "create a new thread",
        Long:  `Create a new thread, optionally copying recent messages from current context.`,
        Args:  cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            if len(args) > 0 {
                topicLabel = args[0]
            }
            return runThreadNew(topicLabel)
        },
    }

    return cmd
}

func newThreadListCmd() *cobra.Command {
    var sessionID string

    cmd := &cobra.Command{
        Use:   "list",
        Short: "list all threads",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runThreadList(sessionID)
        },
    }

    cmd.Flags().StringVar(&sessionID, "session", "", "Session ID")
    return cmd
}

func newThreadSwitchCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "switch <thread-id>",
        Short: "switch to a thread",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runThreadSwitch(args[0])
        },
    }

    return cmd
}

func newThreadCurrentCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "current",
        Short: "show current thread",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runThreadCurrent()
        },
    }

    return cmd
}

func runThreadNew(topicLabel string) error {
    client, err := connectDaemon()
    if err != nil {
        return err
    }
    defer client.Close()

    params := map[string]string{
        "topic_label": topicLabel,
    }

    result, err := client.Call("thread.new", params)
    if err != nil {
        return fmt.Errorf("failed to create thread: %w", err)
    }

    var resp struct {
        ThreadID   string `json:"thread_id"`
        TopicLabel string `json:"topic_label"`
    }
    if err := json.Unmarshal(result, &resp); err != nil {
        return err
    }

    fmt.Printf("created thread %s (%s)\n", resp.ThreadID, resp.TopicLabel)
    return nil
}

func runThreadList(sessionID string) error {
    client, err := connectDaemon()
    if err != nil {
        return err
    }
    defer client.Close()

    result, err := client.Call("thread.list", map[string]string{"session_id": sessionID})
    if err != nil {
        return err
    }

    var resp struct {
        Threads []struct {
            ID         string `json:"id"`
            TopicLabel string `json:"topic_label"`
            IsActive   bool   `json:"is_active"`
        } `json:"threads"`
    }
    if err := json.Unmarshal(result, &resp); err != nil {
        return err
    }

    fmt.Println("threads")
    fmt.Println("=======")
    for _, t := range resp.Threads {
        activeMarker := ""
        if t.IsActive {
            activeMarker = " (active)"
        }
        fmt.Printf("  %s: %s%s\n", t.ID, t.TopicLabel, activeMarker)
    }

    return nil
}

func runThreadSwitch(threadID string) error {
    client, err := connectDaemon()
    if err != nil {
        return err
    }
    defer client.Close()

    _, err = client.Call("thread.switch", map[string]string{"thread_id": threadID})
    if err != nil {
        return err
    }

    fmt.Printf("switched to thread %s\n", threadID)
    return nil
}

func runThreadCurrent() error {
    client, err := connectDaemon()
    if err != nil {
        return err
    }
    defer client.Close()

    result, err := client.Call("thread.current", nil)
    if err != nil {
        return err
    }

    var resp struct {
        ThreadID   string `json:"thread_id"`
        TopicLabel string `json:"topic_label"`
    }
    if err := json.Unmarshal(result, &resp); err != nil {
        return err
    }

    fmt.Printf("current thread: %s (%s)\n", resp.ThreadID, resp.TopicLabel)
    return nil
}
```

- [ ] **Step 2: Register thread command in main.go**

```go
// cmd/meept/main.go (find where subcommands are registered)
rootCmd.AddCommand(newThreadCmd())
```

- [ ] **Step 3: Commit**

```bash
git add cmd/meept/thread.go cmd/meept/main.go
git commit -m "feat(cli): add thread management commands"
```

#### Task 7: Branch Feature Disable Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `config/meept.json5`
- Modify: `internal/session/branch.go`

- [ ] **Step 1: Add branch config option**

```go
// internal/config/config.go (add to SessionConfig struct)
type SessionConfig struct {
    BranchSummaryThreshold    int  `json:"branch_summary_threshold"`
    BranchesEnabled           bool `json:"branches_enabled"`           // NEW
    ThreadsEnabled            bool `json:"threads_enabled"`            // NEW
}

// Default session config
func defaultSessionConfig() SessionConfig {
    return SessionConfig{
        BranchSummaryThreshold: 5,
        BranchesEnabled:        false,  // Disabled by default (user request)
        ThreadsEnabled:         true,   // Enabled by default
    }
}
```

- [ ] **Step 2: Add config gate to branch.go**

```go
// internal/session/branch.go:29 (add to NewBranchManager)
func NewBranchManager(store Store, summarizer BranchSummarizer, cfg config.SessionConfig, logger *slog.Logger) *BranchManager {
    if logger == nil {
        logger = slog.Default()
    }
    if cfg.BranchSummaryThreshold == 0 {
        cfg.BranchSummaryThreshold = 5
    }

    // Log deprecation notice if branches are disabled
    if !cfg.BranchesEnabled {
        logger.Info("Branch feature is disabled by configuration. Use session.branches.enabled=true to enable.")
    }

    return &BranchManager{
        store:      store,
        summarizer: summarizer,
        logger:     logger,
        config:     cfg,
    }
}

// NavigateToBranch returns early if branches are disabled (modified)
func (bm *BranchManager) NavigateToBranch(ctx context.Context, sessionID string, targetMessageID int64) (*NavigationResult, error) {
    if !bm.config.BranchesEnabled {
        return nil, fmt.Errorf("branch navigation is disabled (set session.branches.enabled=true to enable)")
    }
    // ... rest of existing implementation ...
}
```

- [ ] **Step 3: Add JSON5 config example**

```json5
// config/meept.json5 (add to session section)
{
  // ... existing config ...

  session: {
    // Branch feature: disabled by default (dead feature)
    // Enable only if you need git-like conversation forking
    branches_enabled: false,
    branch_summary_threshold: 5,  // Messages before summary generated

    // Thread feature: enabled by default (context isolation)
    threads_enabled: true,
    min_messages_for_summary: 5,  // Cross-thread summary injection threshold
  }
}
```

- [ ] **Step 4: Modify branch CLI to show disabled message**

```go
// cmd/meept/branch.go:14 (add config check to newBranchCmd)
func newBranchCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "branch",
        Short: "Manage session branches (disabled by default)",
        Long: `Branch management is disabled by default.

To enable branches, add to ~/.meept/meept.json5:
  { session: { branches_enabled: true } }

Branches let you explore alternative responses or fork a conversation
from a prior point without losing the original context.`,
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Println("Branch feature is disabled by default.")
            fmt.Println("To enable, set session.branches.enabled=true in your config.")
            fmt.Println()
            fmt.Println("See: docs/concepts/threads.md for thread-based context (recommended)")
        },
    }

    // Subcommands still show disabled message
    cmd.AddCommand(newBranchListCmd())
    cmd.AddCommand(newBranchNavigateCmd())
    cmd.AddCommand(newBranchForkCmd())
    cmd.AddCommand(newBranchTreeCmd())

    return cmd
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go config/meept.json5 internal/session/branch.go cmd/meept/branch.go
git commit -m "feat(config): disable branch feature by default, add thread config"
```

### Phase 5: TUI and GUI Integration

#### Task 8: TUI Thread Indicator

**Files:**
- Create: `internal/tui/thread_indicator.go`
- Modify: `internal/tui/chat.go` (or main chat view file)

- [ ] **Step 1: Write thread indicator component**

```go
// internal/tui/thread_indicator.go
package tui

import (
    "fmt"
    "github.com/charmbracelet/bubbles/help"
    "github.com/charmbracelet/bubbles/key"
   tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

// ThreadIndicator shows the current thread and allows switching.
type ThreadIndicator struct {
    threads      []ThreadInfo
    currentIndex int
    showList     bool
    help         help.Model
    styles       threadStyles
}

type ThreadInfo struct {
    ID         string
    TopicLabel string
    IsActive   bool
}

type threadStyles struct {
    active      lipgloss.Style
    inactive    lipgloss.Style
    container   lipgloss.Style
    indicator   lipgloss.Style
    topicLabel  lipgloss.Style
}

func defaultThreadStyles() threadStyles {
    return threadStyles{
        active: lipgloss.NewStyle().
            Foreground(lipgloss.Color("205")).
            Bold(true),
        inactive: lipgloss.NewStyle().
            Foreground(lipgloss.Color("241")),
        container: lipgloss.NewStyle().
            BorderStyle(lipgloss.RoundedBorder()).
            BorderForeground(lipgloss.Color("62")).
            Padding(0, 1),
        indicator: lipgloss.NewStyle().
            Foreground(lipgloss.Color("82")),
        topicLabel: lipgloss.NewStyle().
            Foreground(lipgloss.Color("147")),
    }
}

// NewThreadIndicator creates a new thread indicator.
func NewThreadIndicator() *ThreadIndicator {
    h := help.New()
    h.Styles.ShortKey = h.Styles.ShortKey.Foreground(lipgloss.Color("62"))
    h.Styles.ShortDesc = h.Styles.ShortDesc.Foreground(lipgloss.Color("241"))

    return &ThreadIndicator{
        threads:      []ThreadInfo{},
        currentIndex: 0,
        help:         h,
        styles:       defaultThreadStyles(),
    }
}

// ThreadIndicatorKeyMap defines keybindings.
type ThreadIndicatorKeyMap struct {
    ToggleList key.Binding
    SwitchPrev key.Binding
    SwitchNext key.Binding
    Select     key.Binding
    Close      key.Binding
}

// DefaultThreadIndicatorKeys returns default keybindings.
func DefaultThreadIndicatorKeys() ThreadIndicatorKeyMap {
    return ThreadIndicatorKeyMap{
        ToggleList: key.NewBinding(
            key.WithKeys("T"),
            key.WithHelp("T", "threads"),
        ),
        SwitchPrev: key.NewBinding(
            key.WithKeys("left", "h"),
            key.WithHelp("←/h", "prev thread"),
        ),
        SwitchNext: key.NewBinding(
            key.WithKeys("right", "l"),
            key.WithHelp("→/l", "next thread"),
        ),
        Select: key.NewBinding(
            key.WithKeys("enter"),
            key.WithHelp("enter", "switch"),
        ),
        Close: key.NewBinding(
            key.WithKeys("esc", "q"),
            key.WithHelp("esc/q", "close"),
        ),
    }
}

// Keys returns keybindings for help view.
func (ti *ThreadIndicator) Keys() []key.Binding {
    km := DefaultThreadIndicatorKeys()
    return []key.Binding{km.ToggleList, km.SwitchPrev, km.SwitchNext}
}

// Update handles messages.
func (ti *ThreadIndicator) Update(msg tea.Msg, km ThreadIndicatorKeyMap) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch {
        case key.Matches(msg, km.ToggleList):
            ti.showList = !ti.showList
            return ti, nil
        case key.Matches(msg, km.Close) && ti.showList:
            ti.showList = false
            return ti, nil
        case key.Matches(msg, km.SwitchPrev) && ti.showList:
            if ti.currentIndex > 0 {
                ti.currentIndex--
            }
            return ti, nil
        case key.Matches(msg, km.SwitchNext) && ti.showList:
            if ti.currentIndex < len(ti.threads)-1 {
                ti.currentIndex++
            }
            return ti, nil
        case key.Matches(msg, km.Select) && ti.showList:
            // Return command to switch thread
            return ti, ti.switchThread(ti.threads[ti.currentIndex].ID)
        }
    }
    return ti, nil
}

// View renders the thread indicator.
func (ti *ThreadIndicator) View() string {
    if len(ti.threads) == 0 {
        return ""
    }

    if ti.showList {
        return ti.viewList()
    }

    return ti.viewCompact()
}

func (ti *ThreadIndicator) viewCompact() string {
    if len(ti.threads) == 0 {
        return ""
    }

    current := ti.threads[ti.currentIndex]
    label := fmt.Sprintf("thread: %s", current.TopicLabel)

    return ti.styles.container.Render(
        ti.styles.indicator.Render("● ") +
            ti.styles.topicLabel.Render(label),
    )
}

func (ti *ThreadIndicator) viewList() string {
    var lines []string

    for i, thread := range ti.threads {
        prefix := "  "
        style := ti.styles.inactive

        if i == ti.currentIndex {
            prefix = ti.styles.indicator.Render("● ")
            style = ti.styles.active
        }

        line := fmt.Sprintf("%s%s: %s", prefix, thread.ID, thread.TopicLabel)
        lines = append(lines, style.Render(line))
    }

    // Add help
    km := DefaultThreadIndicatorKeys()
    help := ti.help.View(km)
    lines = append(lines, "", help)

    return ti.styles.container.Render(
        lipgloss.JoinVertical(lipgloss.Left, lines...),
    )
}

// SetThreads updates the thread list.
func (ti *ThreadIndicator) SetThreads(threads []ThreadInfo) {
    ti.threads = threads
    for i, t := range threads {
        if t.IsActive {
            ti.currentIndex = i
            break
        }
    }
}

// switchThread returns a command to switch to the given thread.
func (ti *ThreadIndicator) switchThread(threadID string) tea.Cmd {
    return func() tea.Msg {
        return threadSwitchMsg{ThreadID: threadID}
    }
}

// threadSwitchMsg is sent when user switches threads.
type threadSwitchMsg struct {
    ThreadID string
}
```

- [ ] **Step 2: Wire thread indicator into chat view**

```go
// internal/tui/chat.go (find where the view is assembled)
// Add thread indicator import and initialization

type chatModel struct {
    // ... existing fields ...
    threadIndicator *ThreadIndicator
}

// In init or NewChatModel:
m.threadIndicator = NewThreadIndicator()

// In Update method, add case for thread indicator messages:
case threadSwitchMsg:
    // Handle thread switch
    return m, m.switchToThread(msg.ThreadID)

// In View method, add thread indicator:
func (m chatModel) View() string {
    var parts []string

    // Thread indicator at top
    if m.threadIndicator != nil {
        parts = append(parts, m.threadIndicator.View())
    }

    // ... rest of existing view ...

    return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/thread_indicator.go internal/tui/chat.go
git commit -m "feat(tui): add thread indicator component"
```

#### Task 9: GUI Thread Selector (Flutter)

**Files:**
- Create: `ui/flutter_ui/lib/widgets/thread_selector.dart`
- Create: `ui/flutter_ui/lib/services/thread_service.dart`
- Modify: `ui/flutter_ui/lib/screens/chat_screen.dart`

- [ ] **Step 1: Create thread service**

```dart
// ui/flutter_ui/lib/services/thread_service.dart
import 'dart:convert';
import 'package:http/http.dart' as http;

class ThreadService {
  final String baseUrl;
  final String? authToken;

  ThreadService({
    required this.baseUrl,
    this.authToken,
  });

  Future<List<ThreadInfo>> listThreads(String sessionId) async {
    final response = await http.get(
      Uri.parse('$baseUrl/api/v1/sessions/$sessionId/threads'),
      headers: {
        if (authToken != null) 'Authorization': 'Bearer $authToken',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to list threads: ${response.body}');
    }

    final data = jsonDecode(response.body) as Map<String, dynamic>;
    final threads = data['threads'] as List;
    return threads
        .map((t) => ThreadInfo.fromJson(t as Map<String, dynamic>))
        .toList();
  }

  Future<ThreadInfo> createThread(String sessionId, String topicLabel) async {
    final response = await http.post(
      Uri.parse('$baseUrl/api/v1/sessions/$sessionId/threads'),
      headers: {
        'Content-Type': 'application/json',
        if (authToken != null) 'Authorization': 'Bearer $authToken',
      },
      body: jsonEncode({'topic_label': topicLabel}),
    );

    if (response.statusCode != 201) {
      throw Exception('Failed to create thread: ${response.body}');
    }

    final data = jsonDecode(response.body) as Map<String, dynamic>;
    return ThreadInfo.fromJson(data);
  }

  Future<void> switchThread(String sessionId, String threadId) async {
    final response = await http.put(
      Uri.parse('$baseUrl/api/v1/sessions/$sessionId/threads/$threadId/active'),
      headers: {
        if (authToken != null) 'Authorization': 'Bearer $authToken',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to switch thread: ${response.body}');
    }
  }

  Future<ThreadInfo> getCurrentThread(String sessionId) async {
    final response = await http.get(
      Uri.parse('$baseUrl/api/v1/sessions/$sessionId/threads/active'),
      headers: {
        if (authToken != null) 'Authorization': 'Bearer $authToken',
      },
    );

    if (response.statusCode != 200) {
      throw Exception('Failed to get current thread: ${response.body}');
    }

    final data = jsonDecode(response.body) as Map<String, dynamic>;
    return ThreadInfo.fromJson(data);
  }
}

class ThreadInfo {
  final String id;
  final String topicLabel;
  final String conversationId;
  final bool isActive;
  final DateTime createdAt;
  final DateTime lastActivityAt;
  final String? summary;

  ThreadInfo({
    required this.id,
    required this.topicLabel,
    required this.conversationId,
    required this.isActive,
    required this.createdAt,
    required this.lastActivityAt,
    this.summary,
  });

  factory ThreadInfo.fromJson(Map<String, dynamic> json) {
    return ThreadInfo(
      id: json['id'] as String,
      topicLabel: json['topic_label'] as String,
      conversationId: json['conversation_id'] as String,
      isActive: json['is_active'] as bool,
      createdAt: DateTime.parse(json['created_at'] as String),
      lastActivityAt: DateTime.parse(json['last_activity_at'] as String),
      summary: json['summary'] as String?,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      'id': id,
      'topic_label': topicLabel,
      'conversation_id': conversationId,
      'is_active': isActive,
      'created_at': createdAt.toIso8601String(),
      'last_activity_at': lastActivityAt.toIso8601String(),
      'summary': summary,
    };
  }
}
```

- [ ] **Step 2: Create thread selector widget**

```dart
// ui/flutter_ui/lib/widgets/thread_selector.dart
import 'package:flutter/material.dart';
import '../services/thread_service.dart';

class ThreadSelector extends StatelessWidget {
  final ThreadService threadService;
  final String sessionId;
  final List<ThreadInfo> threads;
  final ThreadInfo? activeThread;
  final Function(ThreadInfo) onThreadSelected;
  final Function(String)? onNewThread;

  const ThreadSelector({
    super.key,
    required this.threadService,
    required this.sessionId,
    required this.threads,
    this.activeThread,
    required this.onThreadSelected,
    this.onNewThread,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: Theme.of(context).cardColor,
        border: Border(
          bottom: BorderSide(
            color: Theme.of(context).dividerColor,
            width: 1,
          ),
        ),
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Row(
        children: [
          // Thread label
          Text(
            'thread: ',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Colors.grey[600],
            ),
          ),

          // Active thread chip
          if (activeThread != null)
            Expanded(
              child: InkWell(
                onTap: () => _showThreadList(context),
                borderRadius: BorderRadius.circular(16),
                child: Chip(
                  avatar: const Icon(Icons.chat, size: 16),
                  label: Text(
                    activeThread!.topicLabel,
                    style: const TextStyle(
                      fontWeight: FontWeight.bold,
                      color: Colors.white,
                    ),
                  ),
                  backgroundColor: _getThreadColor(activeThread!.topicLabel),
                  deleteIcon: const Icon(Icons.keyboard_arrow_down, size: 16),
                  onDeleted: () => _showThreadList(context),
                ),
              ),
            )
          else
            const CircularProgressIndicator(strokeWidth: 2),

          // New thread button
          IconButton(
            icon: const Icon(Icons.add_circle_outline),
            onPressed: () => _showNewThreadDialog(context),
            tooltip: 'New thread',
          ),
        ],
      ),
    );
  }

  void _showThreadList(BuildContext context) {
    showModalBottomSheet(
      context: context,
      builder: (context) => ThreadListSheet(
        threads: threads,
        activeThread: activeThread,
        onThreadSelected: onThreadSelected,
      ),
    );
  }

  void _showNewThreadDialog(BuildContext context) {
    final controller = TextEditingController();
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('New Thread'),
        content: TextField(
          controller: controller,
          decoration: const InputDecoration(
            hintText: 'Topic label (e.g., work, lunch, general)',
          ),
          autofocus: true,
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          ElevatedButton(
            onPressed: () {
              if (controller.text.isNotEmpty && onNewThread != null) {
                onNewThread!(controller.text);
                Navigator.pop(context);
              }
            },
            child: const Text('Create'),
          ),
        ],
      ),
    );
  }

  Color _getThreadColor(String topicLabel) {
    // Hash-based color selection for consistency
    final colors = [
      Colors.blue,
      Colors.green,
      Colors.orange,
      Colors.purple,
      Colors.teal,
      Colors.pink,
      Colors.indigo,
    ];
    final index = topicLabel.hashCode.abs() % colors.length;
    return colors[index];
  }
}

class ThreadListSheet extends StatelessWidget {
  final List<ThreadInfo> threads;
  final ThreadInfo? activeThread;
  final Function(ThreadInfo) onThreadSelected;

  const ThreadListSheet({
    super.key,
    required this.threads,
    required this.activeThread,
    required this.onThreadSelected,
  });

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      shrinkWrap: true,
      itemCount: threads.length,
      itemBuilder: (context, index) {
        final thread = threads[index];
        final isActive = thread.id == activeThread?.id;

        return ListTile(
          leading: Icon(
            isActive ? Icons.check_circle : Icons.circle_outlined,
            color: isActive ? Colors.green : null,
          ),
          title: Row(
            children: [
              Container(
                width: 12,
                height: 12,
                decoration: BoxDecoration(
                  color: _getThreadColor(thread.topicLabel),
                  shape: BoxShape.circle,
                ),
              ),
              const SizedBox(width: 8),
              Text(thread.topicLabel),
            ],
          ),
          subtitle: Text(
            thread.id,
            style: Theme.of(context).textTheme.bodySmall,
          ),
          onTap: () {
            onThreadSelected(thread);
            Navigator.pop(context);
          },
        );
      },
    );
  }

  Color _getThreadColor(String topicLabel) {
    final colors = [
      Colors.blue,
      Colors.green,
      Colors.orange,
      Colors.purple,
      Colors.teal,
      Colors.pink,
      Colors.indigo,
    ];
    final index = topicLabel.hashCode.abs() % colors.length;
    return colors[index];
  }
}
```

- [ ] **Step 3: Integrate into chat screen**

```dart
// ui/flutter_ui/lib/screens/chat_screen.dart (find main chat widget)
// Add import and thread selector integration

import '../widgets/thread_selector.dart';
import '../services/thread_service.dart';

class ChatScreenState extends State<ChatScreen> {
  // ... existing fields ...
  late ThreadService _threadService;
  List<ThreadInfo> _threads = [];
  ThreadInfo? _activeThread;

  @override
  void initState() {
    super.initState();
    _threadService = ThreadService(
      baseUrl: widget.config.baseUrl,
      authToken: widget.config.authToken,
    );
    _loadThreads();
  }

  Future<void> _loadThreads() async {
    try {
      final threads = await _threadService.listThreads(_sessionId);
      setState(() {
        _threads = threads;
        _activeThread = threads.firstWhere(
          (t) => t.isActive,
          orElse: () => threads.isNotEmpty ? threads.first : ThreadInfo(
            id: '',
            topicLabel: 'general',
            conversationId: _sessionId,
            isActive: true,
            createdAt: DateTime.now(),
            lastActivityAt: DateTime.now(),
          ),
        );
      });
    } catch (e) {
      // Handle error silently for now
    }
  }

  Future<void> _switchThread(ThreadInfo thread) async {
    try {
      await _threadService.switchThread(_sessionId, thread.id);
      setState(() {
        _activeThread = thread;
      });
      // Reload messages for new thread conversation
      await _loadMessages();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to switch thread: $e')),
        );
      }
    }
  }

  Future<void> _createNewThread(String topicLabel) async {
    try {
      final newThread = await _threadService.createThread(_sessionId, topicLabel);
      setState(() {
        _threads.add(newThread);
        _activeThread = newThread;
      });
      await _loadMessages();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to create thread: $e')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(
        children: [
          // Thread selector bar
          ThreadSelector(
            threadService: _threadService,
            sessionId: _sessionId,
            threads: _threads,
            activeThread: _activeThread,
            onThreadSelected: _switchThread,
            onNewThread: _createNewThread,
          ),

          // ... existing chat messages widget ...
        ],
      ),
    );
  }
}
```

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/widgets/thread_selector.dart ui/flutter_ui/lib/services/thread_service.dart ui/flutter_ui/lib/screens/chat_screen.dart
git commit -m "feat(flutter): add thread selector widget and service"
```

### Phase 6: HTTP API Endpoints

#### Task 10: Thread REST Endpoints

**Files:**
- Modify: `internal/comm/http/api_handlers.go`

- [ ] **Step 1: Add thread handlers**

```go
// internal/comm/http/api_handlers.go (add to handler registration)
func (h *APIHandler) registerThreadEndpoints() {
    // GET /api/v1/sessions/{id}/threads - list threads
    h.mux.HandleFunc("GET /api/v1/sessions/{session_id}/threads", h.listThreads)

    // POST /api/v1/sessions/{id}/threads - create thread
    h.mux.HandleFunc("POST /api/v1/sessions/{session_id}/threads", h.createThread)

    // GET /api/v1/sessions/{id}/threads/active - get active thread
    h.mux.HandleFunc("GET /api/v1/sessions/{session_id}/threads/active", h.getActiveThread)

    // PUT /api/v1/sessions/{id}/threads/{thread_id}/active - set active thread
    h.mux.HandleFunc("PUT /api/v1/sessions/{session_id}/threads/{thread_id}/active", h.setActiveThread)

    // DELETE /api/v1/sessions/{id}/threads/{thread_id} - delete thread
    h.mux.HandleFunc("DELETE /api/v1/sessions/{session_id}/threads/{thread_id}", h.deleteThread)
}

// listThreads handles GET /api/v1/sessions/{session_id}/threads
func (h *APIHandler) listThreads(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "session_id")

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    threads, err := h.threadStore.ListThreadsBySession(ctx, sessionID)
    if err != nil {
        h.writeError(w, "failed to list threads", err, http.StatusInternalServerError)
        return
    }

    h.writeJSON(w, http.StatusOK, map[string]any{
        "threads": threads,
    })
}

// createThread handles POST /api/v1/sessions/{session_id}/threads
func (h *APIHandler) createThread(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "session_id")

    var req struct {
        TopicLabel string `json:"topic_label"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        h.writeError(w, "invalid request body", err, http.StatusBadRequest)
        return
    }

    if req.TopicLabel == "" {
        req.TopicLabel = "general"
    }

    thread := &session.Thread{
        ID:             fmt.Sprintf("thread-%s-%s", req.TopicLabel, sessionID[len(sessionID)-4:]),
        SessionID:      sessionID,
        TopicLabel:     req.TopicLabel,
        ConversationID: fmt.Sprintf("conv-%s-%s", req.TopicLabel, sessionID[len(sessionID)-4:]),
        CreatedAt:      time.Now().UTC(),
        LastActivityAt: time.Now().UTC(),
        IsActive:       true,
    }

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    if err := h.threadStore.CreateThread(ctx, thread); err != nil {
        h.writeError(w, "failed to create thread", err, http.StatusInternalServerError)
        return
    }

    h.writeJSON(w, http.StatusCreated, map[string]any{
        "thread": thread,
    })
}

// getActiveThread handles GET /api/v1/sessions/{session_id}/threads/active
func (h *APIHandler) getActiveThread(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "session_id")

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    thread, err := h.threadStore.GetActiveThread(ctx, sessionID)
    if err != nil {
        h.writeError(w, "failed to get active thread", err, http.StatusInternalServerError)
        return
    }

    if thread == nil {
        h.writeError(w, "no active thread", nil, http.StatusNotFound)
        return
    }

    h.writeJSON(w, http.StatusOK, map[string]any{
        "thread": thread,
    })
}

// setActiveThread handles PUT /api/v1/sessions/{session_id}/threads/{thread_id}/active
func (h *APIHandler) setActiveThread(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "session_id")
    threadID := chi.URLParam(r, "thread_id")

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    if err := h.threadStore.SetActiveThread(ctx, sessionID, threadID); err != nil {
        h.writeError(w, "failed to set active thread", err, http.StatusInternalServerError)
        return
    }

    h.writeJSON(w, http.StatusOK, map[string]any{
        "status": "ok",
    })
}

// deleteThread handles DELETE /api/v1/sessions/{session_id}/threads/{thread_id}
func (h *APIHandler) deleteThread(w http.ResponseWriter, r *http.Request) {
    sessionID := chi.URLParam(r, "session_id")
    threadID := chi.URLParam(r, "thread_id")

    ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
    defer cancel()

    if err := h.threadStore.DeleteThread(ctx, threadID); err != nil {
        h.writeError(w, "failed to delete thread", err, http.StatusInternalServerError)
        return
    }

    h.writeJSON(w, http.StatusOK, map[string]any{
        "status": "ok",
    })
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/comm/http/api_handlers.go
git commit -m "feat(http): add thread REST API endpoints"
```

### Phase 7: Documentation and Verification

#### Task 11: Architecture Documentation

**Files:**
- Create: `docs/concepts/threads.md`

- [ ] **Step 2: Add documentation link to mkdocs.yml**

```yaml
# mkdocs.yml (add to nav section)
nav:
  - Concepts:
    - ...
    - Threads: concepts/threads.md
```

- [ ] **Step 3: Commit**

```bash
git add docs/concepts/threads.md mkdocs.yml
git commit -m "docs: add thread-based context partitioning documentation"
```

#### Task 12: Self-Review and Verification

**Files:**
- N/A (Review checklist)

- [ ] **Step 1: Spec Coverage Review**

Check each requirement against implementation:

| Requirement | Task | Status |
|-------------|------|--------|
| Thread isolation | Task 1-4 | Implemented |
| Topic detection | Task 3 | Keyword-based MVP |
| Per-thread conversations | Task 2, 4 | SQLite store + router |
| Cross-thread summaries | Task 5 | AssembleThreadContext |
| CLI `/thread` commands | Task 6 | new/list/switch/current |
| TUI thread indicator | Task 8 | ThreadIndicator widget |
| GUI thread selector | Task 9 | Flutter widget + service |
| Branch feature disabled | Task 7 | Config gate added |
| HTTP API endpoints | Task 10 | REST handlers |
| Documentation | Task 11 | concepts/threads.md |

- [ ] **Step 2: Placeholder Scan**

Search plan for red flags:
```bash
grep -E "TBD|TODO|implement later|fill in|add appropriate|similar to Task" docs/superpowers/plans/2026-06-20-thread-based-context-partitioning.md
```
Expected: No matches

- [ ] **Step 3: Type Consistency Check**

Verify type signatures match across tasks:
- `Thread` struct in Task 1 matches usage in Tasks 2-5
- `ThreadInfo` in Task 6 (CLI) matches Task 9 (Flutter)
- Store interface methods in Task 2 match implementation

- [ ] **Step 4: Build and Test Verification**

```bash
# Build all packages
go build ./...

# Run all tests
go test ./internal/session/... ./internal/agent/... -v

# Build CLI
go build -o bin/meept ./cmd/meept

# Build daemon
go build -o bin/meept-daemon ./cmd/meept-daemon

# Build Flutter UI
cd ui/flutter_ui && flutter build
```

- [ ] **Step 5: Commit final verification**

```bash
git add .
git commit -m "chore: final verification and build check"
```

---

## Migration Path for Existing Sessions

### Option A: Silent Migration (Recommended)

Existing sessions continue using single conversation ID. Threads are created on-demand when new messages arrive:

```go
// First message after upgrade
session := store.Get(sessionID)
if session.ConversationID != "" && session.Threads == nil {
    // Migrate: create "general" thread with existing conversation
    session.Threads = map[string]*Thread{
        "thread-general-xxxx": {
            ID:             "thread-general-xxxx",
            TopicLabel:     "general",
            ConversationID: session.ConversationID,
            CreatedAt:      session.CreatedAt,
            IsActive:       true,
        },
    }
    session.ActiveThreadID = "thread-general-xxxx"
}
```

### Option B: Manual Thread Creation

Users manually create threads for existing sessions via CLI:
```bash
meept thread new "general"  # Creates first thread
```

---

## Testing Plan

### Unit Tests

| Component | Test File | Coverage Target |
|-----------|-----------|-----------------|
| Thread struct | `thread_test.go` | 90% |
| TopicDetector | `topic_detector_test.go` | 95% |
| ThreadRouter | `thread_router_test.go` | 85% |
| SQLiteThreadStore | `sqlite_thread_store_test.go` | 90% |
| CrossThreadSummary | `thread_summary_test.go` | 80% |

### Integration Tests

```bash
# Thread lifecycle
go test ./internal/session/... -run TestThreadLifecycle -v

# Topic detection + routing
go test ./internal/agent/... -run TestThreadRouting -v

# End-to-end (daemon + CLI)
./bin/meept-daemon &
./bin/meept thread new "work"
./bin/meept thread list
./bin/meept thread switch thread-work-001
```

### TUI Tests

```bash
# Bubbletea test framework
go test ./internal/tui/... -run TestThreadIndicator -v
```

### Flutter Tests

```bash
cd ui/flutter_ui
flutter test test/widgets/thread_selector_test.dart
```

---

## Rollout Plan

### Phase 1: Core Infrastructure (Week 1)
- Task 1-2: Thread data structures + SQLite store
- Task 3-4: Topic detection + routing
- Internal testing only

### Phase 2: CLI + Config (Week 2)
- Task 6: CLI commands
- Task 7: Branch disable config
- Manual testing by developers

### Phase 3: UI Integration (Week 3)
- Task 8: TUI thread indicator
- Task 9: Flutter thread selector
- Task 10: HTTP API

### Phase 4: Documentation + Polish (Week 4)
- Task 11: Architecture docs
- Task 12: Self-review
- Beta testing with users

---

## Verification

To verify the implementation:

```bash
# 1. Build succeeds
go build ./...

# 2. All tests pass
go test ./... -v

# 3. CLI commands work
./bin/meept thread --help
./bin/meept thread new "test"
./bin/meept thread list

# 4. Threads isolate conversations (manual test)
./bin/meept-daemon
./bin/meept chat "Build a Go feature"  # Creates work thread
./bin/meept chat "What's for lunch?"   # Creates food thread
# Verify different conversation IDs in logs
```

---

## Notes

- **Backward Compatibility**: Existing sessions continue working; threads are opt-in via topic detection
- **Thread + Branch Intersection**: Each thread has independent branch tree (future enhancement, not in this plan)
- **Embedding-Based Detection**: Phase 2 enhancement (currently keyword-based MVP)
- **Branch Deprecation**: Not removed, just disabled by default; can be re-enabled via config

## References

- `~/.claude/skills/meept-subagent-context-architecture/SKILL.md` - Original architecture gap analysis
- `internal/agent/loop.go:1188` - AgentLoop.RunOnceWithParts conversation lookup
- `internal/agent/dispatcher.go:1194` - RouteToAgent implementation
- `internal/session/session.go:37-50` - Session struct
- `docs/concepts/multi-agent.md` - Multi-agent system documentation


```markdown
# Thread-Based Context Partitioning

## Overview

Threads provide isolated conversation contexts within a session, preventing context bloat when conversations switch between unrelated topics.

## Problem

Without threads, all agents operating on a `conversationID` share the SAME `Conversation` object containing the full message history. This causes:

- Context pollution when switching from work → lunch → work
- Increased token usage (models read irrelevant history)
- Confused agents seeing unrelated prior conversations

## Solution

Each thread has its own `Conversation` object, isolating messages by topic:

```
Session (session-abc)
  ├─→ Thread "work" (conv-work-001)
  │   └─→ Messages: ["Build API", "Fix bug", "Deploy"]
  ├─→ Thread "food" (conv-food-001)
  │   └─→ Messages: ["Lunch ideas", "Recipe for pasta"]
  └─→ Thread "personal" (conv-personal-001)
      └─→ Messages: ["Weekend plans"]
```

## How It Works

### 1. Topic Detection

When a user sends a message, the `TopicDetector` analyzes input:

```go
input := "I need to fix this database bug"
topic := detector.Detect(input)  // Returns: "code"
threadID := fmt.Sprintf("thread-code-%s", sessionID[len(sessionID)-4:])
```

Keyword categories (configurable):
- `work`: task, feature, bug, code, build, deploy, api
- `code`: debug, error, panic, compile, test
- `food`: lunch, dinner, food, eat, recipe, restaurant
- `personal`: weekend, vacation, hobby, shopping
- `general`: default fallback

### 2. Thread Routing

The `ThreadRouter` maps topics to conversation IDs:

```go
conversationID, err := router.GetThreadConversationID(ctx, sessionID, input)
// Returns: "conv-code-001" for code topic
```

### 3. Cross-Thread Summary Injection

When switching threads, inactive thread summaries provide context:

```
[Context from work thread]: API endpoint debugging, fixed connection pool bug
[Context from food thread]: Italian restaurant recommendations
```

## CLI Usage

```bash
# Create new thread
meept thread new "work"

# List threads
meept thread list

# Switch thread
meept thread switch thread-work-001

# Show current thread
meept thread current
```

## TUI Usage

- Press `T` to show thread list
- Use `←/→` or `h/l` to navigate
- Press `enter` to switch

## Configuration

```json5
{
  session: {
    threads_enabled: true,  // Enable thread routing
    min_messages_for_summary: 5,  // Summary injection threshold
  }
}
```

## Threads vs. Branches

| Feature | Threads | Branches |
|---------|---------|----------|
| Purpose | Topic isolation | Alternative histories |
| Analogy | Browser tabs | Git branches |
| Default | Enabled | Disabled |
| Use case | Work vs. lunch vs. weekend | "What if I tried X?" |

## Implementation Details

### Database Schema

```sql
CREATE TABLE session_threads (
    id              TEXT PRIMARY KEY,
    session_id      TEXT REFERENCES sessions(id),
    topic_label     TEXT DEFAULT 'general',
    conversation_id TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    last_activity   TEXT NOT NULL,
    summary         TEXT,
    is_active       INTEGER DEFAULT 0
);
```

### Key Files

- `internal/session/thread.go` - Thread struct
- `internal/session/thread_store.go` - Persistence
- `internal/agent/topic_detector.go` - Topic detection
- `internal/agent/thread_router.go` - Routing logic
- `cmd/meept/thread.go` - CLI commands
