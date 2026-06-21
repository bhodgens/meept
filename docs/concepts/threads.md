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
    // Thread feature: enabled by default (context isolation)
    threads_enabled: true,
    min_messages_for_summary: 5,  // Cross-thread summary injection threshold

    // Branch feature: disabled by default (dead feature)
    // Enable only if you need git-like conversation forking
    branching: false,
    branch_summary_threshold: 5,
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
CREATE TABLE IF NOT EXISTS session_threads (
    id              TEXT PRIMARY KEY,
    session_id      TEXT REFERENCES sessions(id),
    topic_label     TEXT DEFAULT 'general',
    conversation_id TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    last_activity   TEXT NOT NULL,
    summary         TEXT,
    is_active       INTEGER DEFAULT 0
);

CREATE INDEX idx_session_threads_session ON session_threads(session_id);
CREATE INDEX idx_session_threads_active ON session_threads(session_id, is_active);
```

### Key Files

| File | Purpose |
|------|---------|
| `internal/session/thread.go` | Thread struct, ThreadRouter |
| `internal/session/thread_store.go` | Thread CRUD interface |
| `internal/session/thread_summary.go` | Cross-thread summary injection |
| `internal/session/thread_migration.go` | Schema migration |
| `internal/agent/topic_detector.go` | Keyword-based topic detection |
| `cmd/meept/thread.go` | CLI commands |
| `internal/tui/thread_indicator.go` | TUI thread display |

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

## API Reference

### Session Methods

```go
// GetActiveThread returns the currently active thread
func (s *Session) GetActiveThread() *Thread

// GetOrCreateThread returns existing thread or creates new one
func (s *Session) GetOrCreateThread(threadID, topicLabel string) *Thread
```

### ThreadRouter Methods

```go
// Detect identifies topic from user input
func (tr *ThreadRouter) Detect(input string) string

// GenerateThreadID creates unique thread ID
func (tr *ThreadRouter) GenerateThreadID(sessionID, topic string) string

// SetActiveThread/GetActiveThread track active thread per session
func (tr *ThreadRouter) SetActiveThread(sessionID, threadID string)
func (tr *ThreadRouter) GetActiveThread(sessionID string) (string, bool)
```

### ThreadStore Interface

```go
type ThreadStore interface {
    CreateThread(ctx context.Context, thread *Thread) error
    GetThread(ctx context.Context, threadID string) (*Thread, error)
    ListThreadsBySession(ctx context.Context, sessionID string) ([]*Thread, error)
    UpdateThread(ctx context.Context, thread *Thread) error
    DeleteThread(ctx context.Context, threadID string) error
    GetActiveThread(ctx context.Context, sessionID string) (*Thread, error)
    SetActiveThread(ctx context.Context, sessionID, threadID string) error
}
```

## Testing

```bash
# Unit tests
go test ./internal/session/... -v -run TestThread

# Build verification
go build ./internal/session/...
go build ./cmd/meept/...

# CLI testing
meept thread --help
```

## Future Enhancements

1. **Embedding-based topic detection** - More accurate than keyword matching
2. **Per-thread message persistence** - Store messages with thread_id
3. **Thread-aware conversation compaction** - Compact per-thread, not global
4. **Thread export/import** - Share individual conversation threads
5. **Thread merging** - Combine related threads

## References

- `~/.claude/skills/meept-subagent-context-architecture/SKILL.md` - Original architecture gap analysis
- `internal/agent/loop.go:1188` - AgentLoop conversation lookup
- `internal/agent/dispatcher.go:1194` - RouteToAgent implementation
- `docs/concepts/multi-agent.md` - Multi-agent system documentation
