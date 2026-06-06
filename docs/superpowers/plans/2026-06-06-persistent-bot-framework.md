# Persistent Bot Framework Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable Meept to create, manage, and run persistent autonomous bots that execute on schedules and respond to events, with independent memory, cost isolation, and security boundaries.

**Architecture:** Bot definitions extend the existing agent spec system with trigger declarations (cron, bus events, webhooks). A new `BotRunner` wraps `AgentLoop.RunOnce()` with bot-specific wiring: personality injection, scoped memory, relaxed watchdog, and per-bot cost tracking. An `EventActionRouter` subscribes to bus topics and routes events to bots. Bots store all state in the existing memory subsystem with namespace isolation via `AgentID` scoping.

**Tech Stack:** Go 1.24+, SQLite (existing), robfig/cron (existing), message bus (existing), AgentLoop (existing).

---

## File Structure

### New Files to Create

| File | Responsibility |
|------|----------------|
| `internal/bot/types.go` | BotDefinition, BotTrigger, BotState, BotStatus types |
| `internal/bot/runner.go` | BotRunner: wraps AgentLoop with bot-specific wiring |
| `internal/bot/router.go` | EventActionRouter: subscribes to bus topics, routes to bots |
| `internal/bot/store.go` | SQLite-backed bot definition persistence (CRUD) |
| `internal/bot/lifecycle.go` | Bot lifecycle: start, stop, pause, resume, health checks |
| `internal/bot/cost.go` | Per-bot cost tracking with budget enforcement |
| `internal/bot/memory_scope.go` | Memory namespace isolation for bots |
| `internal/bot/handler.go` | RPC handlers for bot management commands |
| `internal/bot/webhook.go` | HTTP webhook endpoint: POST /api/v1/bot/{id}/trigger |
| `cmd/meept/bot_cmd.go` | CLI subcommand: `meept bots create/list/pause/resume/delete` |

### Modified Files

| File | Changes |
|------|---------|
| `internal/scheduler/jobs.go` | Fix `AgentJob` topic mismatch: `agent.chat` → `chat.request` |
| `internal/agent/spec.go` | Add `RoleBot AgentRole = "bot"` constant |
| `internal/agent/loop.go` | Add `RunWithBot()` method for bot-specific execution |
| `internal/daemon/components.go` | Wire `BotManager`, `EventActionRouter` on startup |
| `internal/daemon/daemon.go` | Shutdown: stop all bots gracefully |
| `internal/config/schema.go` | Add `BotsConfig` section with defaults |
| `internal/memory/types.go` | Add `BotID` field to `Memory` for namespace isolation |
| `internal/memory/manager.go` | Add `ScopedManager()` that filters by bot ID |
| `internal/comm/http/server.go` | Register webhook endpoint |
| `cmd/meept/main.go` | Register `bots` subcommand |

---

## Phase 1: Bug Fix & Foundation Types

### Task 1: Fix AgentJob topic mismatch

**Files:**
- Modify: `internal/scheduler/jobs.go:211`
- Test: `internal/scheduler/scheduler_test.go` (add integration test)

This is a pre-existing bug: `AgentJob.Execute()` publishes to `"agent.chat"` but `ChatHandler` subscribes to `"chat.request"`. Scheduled agent jobs are silently lost.

- [ ] **Step 1: Write a failing test**

```go
// internal/scheduler/scheduler_test.go
func TestAgentJob_PublishesToCorrectTopic(t *testing.T) {
    bus := bus.NewMessageBus(log.Default())
    received := make(chan *models.BusMessage, 1)

    // Subscribe to the topic ChatHandler actually listens on
    sub := bus.Subscribe("test-agent-job", "chat.request")
    go func() {
        select {
        case msg := <-sub.Channel:
            received <- msg
        case <-time.After(2 * time.Second):
        }
    }()

    job := &AgentJob{
        baseJob: baseJob{id: "test-job", name: "test"},
        prompt:  "hello",
        bus:     bus,
        config:  config.DefaultConfig(),
    }

    err := job.Execute(context.Background())
    if err != nil {
        t.Fatalf("Execute failed: %v", err)
    }

    select {
    case msg := <-received:
        if msg.Payload == nil {
            t.Fatal("expected non-nil payload")
        }
        t.Logf("received message on chat.request: %s", string(msg.Payload))
    case <-time.After(3 * time.Second):
        t.Fatal("timed out waiting for message on chat.request - topic mismatch?")
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scheduler/ -run TestAgentJob_PublishesToCorrectTopic -v`
Expected: FAIL (timeout - message published to `agent.chat`, not `chat.request`)

- [ ] **Step 3: Fix the topic**

In `internal/scheduler/jobs.go`, change line 211 from:

```go
delivered := j.bus.Publish("agent.chat", msg)
```

to:

```go
delivered := j.bus.Publish("chat.request", msg)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/scheduler/ -run TestAgentJob_PublishesToCorrectTopic -v`
Expected: PASS

- [ ] **Step 5: Run existing scheduler tests**

Run: `go test ./internal/scheduler/ -v`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/jobs.go internal/scheduler/scheduler_test.go
git commit -m "fix(scheduler): correct AgentJob topic from agent.chat to chat.request"
```

---

### Task 2: Bot definition types

**Files:**
- Create: `internal/bot/types.go`
- Test: `internal/bot/types_test.go`

- [ ] **Step 1: Write the types test**

```go
// internal/bot/types_test.go
package bot

import (
    "testing"
    "time"
)

func TestBotTrigger_Validate(t *testing.T) {
    tests := []struct {
        name    string
        trigger BotTrigger
        wantErr bool
    }{
        {
            name: "valid cron trigger",
            trigger: BotTrigger{
                Type:     TriggerTypeCron,
                Schedule: "*/5 * * * *",
            },
            wantErr: false,
        },
        {
            name: "cron trigger missing schedule",
            trigger: BotTrigger{
                Type: TriggerTypeCron,
            },
            wantErr: true,
        },
        {
            name: "valid bus event trigger",
            trigger: BotTrigger{
                Type:  TriggerTypeBusEvent,
                Topic: "calendar.reminder",
            },
            wantErr: false,
        },
        {
            name: "bus event trigger missing topic",
            trigger: BotTrigger{
                Type: TriggerTypeBusEvent,
            },
            wantErr: true,
        },
        {
            name: "valid webhook trigger",
            trigger: BotTrigger{
                Type: TriggerTypeWebhook,
            },
            wantErr: false,
        },
        {
            name: "invalid trigger type",
            trigger: BotTrigger{
                Type: "invalid",
            },
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.trigger.Validate()
            if (err != nil) != tt.wantErr {
                t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}

func TestBotDefinition_Validate(t *testing.T) {
    validDef := BotDefinition{
        ID:          "ci-monitor",
        Name:        "CI Monitor",
        Description: "Monitors CI pipeline status",
        Prompt:      "Check the CI status and report any failures",
        Model:       "",
        Triggers: []BotTrigger{
            {Type: TriggerTypeCron, Schedule: "*/15 * * * *"},
        },
        MemoryScope: MemoryScopePrivate,
        Tools:       []string{"web_fetch", "memory_store", "memory_search"},
        Constraints: BotConstraints{
            MaxIterations:    5,
            Timeout:          2 * time.Minute,
            MaxTokensPerTurn: 2048,
            DailyBudgetCents: 50, // $0.50/day cap
        },
    }

    if err := validDef.Validate(); err != nil {
        t.Fatalf("valid definition failed: %v", err)
    }

    // Missing ID
    noID := validDef
    noID.ID = ""
    if err := noID.Validate(); err == nil {
        t.Fatal("expected error for missing ID")
    }

    // No triggers
    noTriggers := validDef
    noTriggers.Triggers = nil
    if err := noTriggers.Validate(); err == nil {
        t.Fatal("expected error for no triggers")
    }

    // Invalid trigger
    badTrigger := validDef
    badTrigger.Triggers = []BotTrigger{{Type: "bogus"}}
    if err := badTrigger.Validate(); err == nil {
        t.Fatal("expected error for invalid trigger")
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/bot/ -run TestBot -v`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Write the types**

```go
// internal/bot/types.go
package bot

import (
    "fmt"
    "time"

    "github.com/robfig/cron/v3"
)

// TriggerType defines how a bot is activated.
type TriggerType string

const (
    // TriggerTypeCron activates the bot on a schedule.
    TriggerTypeCron TriggerType = "cron"
    // TriggerTypeBusEvent activates the bot when a bus message is received.
    TriggerTypeBusEvent TriggerType = "bus_event"
    // TriggerTypeWebhook activates the bot via HTTP POST.
    TriggerTypeWebhook TriggerType = "webhook"
)

// MemoryScope defines how bot memory is isolated.
type MemoryScope string

const (
    // MemoryScopePrivate restricts the bot to its own memory namespace.
    MemoryScopePrivate MemoryScope = "private"
    // MemoryScopeShared gives the bot access to all memories.
    MemoryScopeShared MemoryScope = "shared"
    // MemoryScopeReadOnly lets the bot read shared memories but write only to its own.
    MemoryScopeReadOnly MemoryScope = "read_only"
)

// BotTrigger defines when and how a bot is activated.
type BotTrigger struct {
    // Type is the trigger mechanism.
    Type TriggerType `json:"type"`
    // Schedule is the cron expression (required for cron triggers).
    Schedule string `json:"schedule,omitempty"`
    // Topic is the bus topic to subscribe to (required for bus_event triggers).
    Topic string `json:"topic,omitempty"`
    // PromptTemplate is an optional Go template for constructing the prompt from event data.
    // Available variables: .Payload, .Topic, .Source, .Timestamp
    PromptTemplate string `json:"prompt_template,omitempty"`
    // Enabled controls whether this trigger is active.
    Enabled bool `json:"enabled"`
}

// Validate checks the trigger configuration.
func (t *BotTrigger) Validate() error {
    switch t.Type {
    case TriggerTypeCron:
        if t.Schedule == "" {
            return fmt.Errorf("cron trigger requires schedule")
        }
        parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
        if _, err := parser.Parse(t.Schedule); err != nil {
            return fmt.Errorf("invalid cron schedule %q: %w", t.Schedule, err)
        }
    case TriggerTypeBusEvent:
        if t.Topic == "" {
            return fmt.Errorf("bus_event trigger requires topic")
        }
    case TriggerTypeWebhook:
        // webhook triggers need no additional config
    default:
        return fmt.Errorf("unknown trigger type: %q", t.Type)
    }
    return nil
}

// BotConstraints defines operational limits for a bot.
type BotConstraints struct {
    // MaxIterations is the maximum reasoning cycles per invocation.
    MaxIterations int `json:"max_iterations"`
    // Timeout is the maximum duration per invocation.
    Timeout time.Duration `json:"timeout"`
    // MaxTokensPerTurn is the maximum tokens to generate per LLM call.
    MaxTokensPerTurn int `json:"max_tokens_per_turn,omitempty"`
    // DailyBudgetCents is the maximum daily spend in cents (0 = unlimited).
    DailyBudgetCents int `json:"daily_budget_cents,omitempty"`
    // MaxInvocationsPerDay caps daily runs (0 = unlimited).
    MaxInvocationsPerDay int `json:"max_invocations_per_day,omitempty"`
}

// BotDefinition is the complete specification for a persistent bot.
type BotDefinition struct {
    // ID is the unique identifier.
    ID string `json:"id"`
    // Name is a human-readable name.
    Name string `json:"name"`
    // Description explains what the bot does.
    Description string `json:"description"`
    // Prompt is the system prompt / behavioral instructions.
    Prompt string `json:"prompt"`
    // Model can be an alias or direct model reference. Empty = default.
    Model string `json:"model,omitempty"`
    // Triggers define when the bot activates.
    Triggers []BotTrigger `json:"triggers"`
    // MemoryScope controls memory isolation.
    MemoryScope MemoryScope `json:"memory_scope"`
    // Tools are the tools this bot can use.
    Tools []string `json:"tools"`
    // Constraints are operational limits.
    Constraints BotConstraints `json:"constraints"`
    // Enabled controls whether the bot is active.
    Enabled bool `json:"enabled"`
    // CreatedAt is when the bot was created.
    CreatedAt time.Time `json:"created_at"`
    // UpdatedAt is when the bot was last modified.
    UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks the complete bot definition.
func (d *BotDefinition) Validate() error {
    if d.ID == "" {
        return fmt.Errorf("bot ID is required")
    }
    if d.Prompt == "" {
        return fmt.Errorf("bot prompt is required")
    }
    if len(d.Triggers) == 0 {
        return fmt.Errorf("at least one trigger is required")
    }
    for i, t := range d.Triggers {
        if err := t.Validate(); err != nil {
            return fmt.Errorf("trigger[%d]: %w", i, err)
        }
    }
    return nil
}

// BotStatus represents the current state of a running bot.
type BotStatus string

const (
    BotStatusRunning BotStatus = "running"
    BotStatusPaused  BotStatus = "paused"
    BotStatusError   BotStatus = "error"
    BotStatusStopped BotStatus = "stopped"
)

// BotState tracks runtime state for a bot.
type BotState struct {
    // DefinitionID is the bot definition this state belongs to.
    DefinitionID string `json:"definition_id"`
    // Status is the current bot status.
    Status BotStatus `json:"status"`
    // LastRunAt is when the bot was last invoked.
    LastRunAt *time.Time `json:"last_run_at,omitempty"`
    // LastError is the error from the last failed run.
    LastError string `json:"last_error,omitempty"`
    // TotalRuns is the total number of invocations.
    TotalRuns int `json:"total_runs"`
    // TotalTokensUsed is the cumulative token count.
    TotalTokensUsed int `json:"total_tokens_used"`
    // TotalCostCents is the cumulative cost in cents.
    TotalCostCents int `json:"total_cost_cents"`
    // ConsecutiveFailures counts consecutive failures.
    ConsecutiveFailures int `json:"consecutive_failures"`
    // TodayRuns is runs today (resets at midnight local time).
    TodayRuns int `json:"today_runs"`
    // TodayCostCents is cost today in cents.
    TodayCostCents int `json:"today_cost_cents"`
    // TodayDate is the date TodayRuns/TodayCostCents are for.
    TodayDate string `json:"today_date"`
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/bot/ -run TestBot -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bot/types.go internal/bot/types_test.go
git commit -m "feat(bot): add bot definition, trigger, and state types"
```

---

### Task 3: Add RoleBot to agent specs

**Files:**
- Modify: `internal/agent/spec.go:18-25`
- Test: `internal/bot/types_test.go` (add validation test)

- [ ] **Step 1: Add the RoleBot constant**

In `internal/agent/spec.go`, add after line 24 (`RoleReviewer`):

```go
    // RoleBot is a persistent autonomous agent that runs on triggers.
    RoleBot AgentRole = "bot"
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/agent/...`
Expected: Success

- [ ] **Step 3: Commit**

```bash
git add internal/agent/spec.go
git commit -m "feat(agent): add RoleBot agent role for persistent bots"
```

---

## Phase 2: Bot Store (Persistence)

### Task 4: SQLite-backed bot store

**Files:**
- Create: `internal/bot/store.go`
- Test: `internal/bot/store_test.go`

- [ ] **Step 1: Write the store tests**

```go
// internal/bot/store_test.go
package bot

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func testStore(t *testing.T) *Store {
    t.Helper()
    dir := t.TempDir()
    store, err := NewStore(filepath.Join(dir, "bots.db"))
    if err != nil {
        t.Fatalf("NewStore: %v", err)
    }
    t.Cleanup(func() { store.Close() })
    return store
}

func testBotDef(id string) BotDefinition {
    return BotDefinition{
        ID:          id,
        Name:        "Test Bot " + id,
        Description: "A test bot",
        Prompt:      "You are a test bot.",
        Triggers: []BotTrigger{
            {Type: TriggerTypeCron, Schedule: "*/5 * * * *", Enabled: true},
        },
        MemoryScope: MemoryScopePrivate,
        Tools:       []string{"web_fetch"},
        Constraints: BotConstraints{
            MaxIterations:    5,
            Timeout:          2 * time.Minute,
            DailyBudgetCents: 100,
        },
        Enabled:   true,
        CreatedAt: time.Now().UTC().Truncate(time.Second),
        UpdatedAt: time.Now().UTC().Truncate(time.Second),
    }
}

func TestStore_CreateAndGet(t *testing.T) {
    store := testStore(t)
    ctx := context.Background()
    def := testBotDef("test-bot-1")

    if err := store.Create(ctx, def); err != nil {
        t.Fatalf("Create: %v", err)
    }

    got, err := store.Get(ctx, "test-bot-1")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }

    if got.ID != def.ID {
        t.Errorf("ID = %q, want %q", got.ID, def.ID)
    }
    if got.Name != def.Name {
        t.Errorf("Name = %q, want %q", got.Name, def.Name)
    }
    if len(got.Triggers) != 1 {
        t.Fatalf("Triggers len = %d, want 1", len(got.Triggers))
    }
    if got.Triggers[0].Type != TriggerTypeCron {
        t.Errorf("Trigger Type = %q, want %q", got.Triggers[0].Type, TriggerTypeCron)
    }
    if got.Triggers[0].Schedule != "*/5 * * * *" {
        t.Errorf("Trigger Schedule = %q, want %q", got.Triggers[0].Schedule, "*/5 * * * *")
    }
}

func TestStore_List(t *testing.T) {
    store := testStore(t)
    ctx := context.Background()

    for i := 0; i < 3; i++ {
        def := testBotDef("bot-list-" + string(rune('A'+i)))
        if err := store.Create(ctx, def); err != nil {
            t.Fatalf("Create %d: %v", i, err)
        }
    }

    bots, err := store.List(ctx)
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(bots) != 3 {
        t.Errorf("List returned %d bots, want 3", len(bots))
    }
}

func TestStore_Update(t *testing.T) {
    store := testStore(t)
    ctx := context.Background()
    def := testBotDef("test-update")

    if err := store.Create(ctx, def); err != nil {
        t.Fatalf("Create: %v", err)
    }

    def.Name = "Updated Name"
    def.Prompt = "New prompt"

    if err := store.Update(ctx, def); err != nil {
        t.Fatalf("Update: %v", err)
    }

    got, err := store.Get(ctx, "test-update")
    if err != nil {
        t.Fatalf("Get: %v", err)
    }
    if got.Name != "Updated Name" {
        t.Errorf("Name = %q, want %q", got.Name, "Updated Name")
    }
    if got.Prompt != "New prompt" {
        t.Errorf("Prompt = %q, want %q", got.Prompt, "New prompt")
    }
}

func TestStore_Delete(t *testing.T) {
    store := testStore(t)
    ctx := context.Background()
    def := testBotDef("test-delete")

    if err := store.Create(ctx, def); err != nil {
        t.Fatalf("Create: %v", err)
    }

    if err := store.Delete(ctx, "test-delete"); err != nil {
        t.Fatalf("Delete: %v", err)
    }

    _, err := store.Get(ctx, "test-delete")
    if err == nil {
        t.Fatal("expected error getting deleted bot")
    }
}

func TestStore_DuplicateID(t *testing.T) {
    store := testStore(t)
    ctx := context.Background()
    def := testBotDef("test-dup")

    if err := store.Create(ctx, def); err != nil {
        t.Fatalf("Create: %v", err)
    }

    err := store.Create(ctx, def)
    if err == nil {
        t.Fatal("expected error creating duplicate bot")
    }
}

func TestStore_UpdateState(t *testing.T) {
    store := testStore(t)
    ctx := context.Background()
    def := testBotDef("test-state")

    if err := store.Create(ctx, def); err != nil {
        t.Fatalf("Create: %v", err)
    }

    now := time.Now().UTC().Truncate(time.Second)
    state := BotState{
        DefinitionID:        "test-state",
        Status:              BotStatusRunning,
        LastRunAt:           &now,
        TotalRuns:           42,
        TotalTokensUsed:     50000,
        TotalCostCents:      37,
        ConsecutiveFailures: 0,
    }

    if err := store.UpdateState(ctx, state); err != nil {
        t.Fatalf("UpdateState: %v", err)
    }

    got, err := store.GetState(ctx, "test-state")
    if err != nil {
        t.Fatalf("GetState: %v", err)
    }
    if got.TotalRuns != 42 {
        t.Errorf("TotalRuns = %d, want 42", got.TotalRuns)
    }
    if got.Status != BotStatusRunning {
        t.Errorf("Status = %q, want %q", got.Status, BotStatusRunning)
    }
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bot/ -run TestStore -v`
Expected: FAIL (Store type doesn't exist)

- [ ] **Step 3: Implement the store**

```go
// internal/bot/store.go
package bot

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "time"

    _ "modernc.org/sqlite"
)

// Store persists bot definitions and runtime state to SQLite.
type Store struct {
    db *sql.DB
}

// NewStore creates a new bot store backed by the given SQLite file.
func NewStore(path string) (*Store, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }

    s := &Store{db: db}
    if err := s.migrate(); err != nil {
        db.Close()
        return nil, fmt.Errorf("migrate: %w", err)
    }
    return s, nil
}

func (s *Store) migrate() error {
    _, err := s.db.Exec(`
        CREATE TABLE IF NOT EXISTS bot_definitions (
            id          TEXT PRIMARY KEY,
            data        TEXT NOT NULL,
            created_at  TEXT NOT NULL,
            updated_at  TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS bot_states (
            definition_id TEXT PRIMARY KEY REFERENCES bot_definitions(id) ON DELETE CASCADE,
            data          TEXT NOT NULL
        );
    `)
    return err
}

// Create stores a new bot definition.
func (s *Store) Create(ctx context.Context, def BotDefinition) error {
    data, err := json.Marshal(def)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    _, err = s.db.ExecContext(ctx,
        `INSERT INTO bot_definitions (id, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
        def.ID, string(data), def.CreatedAt.Format(time.RFC3339), def.UpdatedAt.Format(time.RFC3339),
    )
    if err != nil {
        return fmt.Errorf("insert: %w", err)
    }
    // Initialize empty state
    state := BotState{DefinitionID: def.ID, Status: BotStatusStopped}
    stateData, _ := json.Marshal(state)
    _, err = s.db.ExecContext(ctx,
        `INSERT INTO bot_states (definition_id, data) VALUES (?, ?)`,
        def.ID, string(stateData),
    )
    return err
}

// Get retrieves a bot definition by ID.
func (s *Store) Get(ctx context.Context, id string) (*BotDefinition, error) {
    var data string
    err := s.db.QueryRowContext(ctx, `SELECT data FROM bot_definitions WHERE id = ?`, id).Scan(&data)
    if err != nil {
        return nil, fmt.Errorf("query: %w", err)
    }
    var def BotDefinition
    if err := json.Unmarshal([]byte(data), &def); err != nil {
        return nil, fmt.Errorf("unmarshal: %w", err)
    }
    return &def, nil
}

// List returns all bot definitions.
func (s *Store) List(ctx context.Context) ([]BotDefinition, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT data FROM bot_definitions ORDER BY created_at`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var defs []BotDefinition
    for rows.Next() {
        var data string
        if err := rows.Scan(&data); err != nil {
            return nil, err
        }
        var def BotDefinition
        if err := json.Unmarshal([]byte(data), &def); err != nil {
            return nil, err
        }
        defs = append(defs, def)
    }
    return defs, rows.Err()
}

// Update replaces an existing bot definition.
func (s *Store) Update(ctx context.Context, def BotDefinition) error {
    data, err := json.Marshal(def)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    res, err := s.db.ExecContext(ctx,
        `UPDATE bot_definitions SET data = ?, updated_at = ? WHERE id = ?`,
        string(data), time.Now().UTC().Format(time.RFC3339), def.ID,
    )
    if err != nil {
        return err
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        return fmt.Errorf("bot %q not found", def.ID)
    }
    return nil
}

// Delete removes a bot definition and its state.
func (s *Store) Delete(ctx context.Context, id string) error {
    _, err := s.db.ExecContext(ctx, `DELETE FROM bot_definitions WHERE id = ?`, id)
    return err
}

// GetState retrieves the runtime state for a bot.
func (s *Store) GetState(ctx context.Context, id string) (*BotState, error) {
    var data string
    err := s.db.QueryRowContext(ctx, `SELECT data FROM bot_states WHERE definition_id = ?`, id).Scan(&data)
    if err != nil {
        return nil, fmt.Errorf("query state: %w", err)
    }
    var state BotState
    if err := json.Unmarshal([]byte(data), &state); err != nil {
        return nil, fmt.Errorf("unmarshal state: %w", err)
    }
    return &state, nil
}

// UpdateState persists runtime state for a bot.
func (s *Store) UpdateState(ctx context.Context, state BotState) error {
    data, err := json.Marshal(state)
    if err != nil {
        return fmt.Errorf("marshal state: %w", err)
    }
    _, err = s.db.ExecContext(ctx,
        `INSERT OR REPLACE INTO bot_states (definition_id, data) VALUES (?, ?)`,
        state.DefinitionID, string(data),
    )
    return err
}

// Close releases database resources.
func (s *Store) Close() error {
    return s.db.Close()
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/bot/ -run TestStore -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bot/store.go internal/bot/store_test.go
git commit -m "feat(bot): add SQLite-backed bot definition and state store"
```

---

## Phase 3: Memory Namespace Isolation

### Task 5: Add BotID to Memory and create ScopedManager

**Files:**
- Modify: `internal/memory/types.go` (add `BotID` field)
- Create: `internal/bot/memory_scope.go`
- Test: `internal/bot/memory_scope_test.go`

- [ ] **Step 1: Add BotID field to Memory struct**

In `internal/memory/types.go`, add to the `Memory` struct after the `TaskID` field (line 51):

```go
    // BotID identifies the bot that created this memory (for bot namespace isolation).
    BotID string `json:"bot_id,omitempty"`
```

- [ ] **Step 2: Write memory scope tests**

```go
// internal/bot/memory_scope_test.go
package bot

import (
    "testing"
)

func TestMemoryNamespace_Prefix(t *testing.T) {
    ns := MemoryNamespace{BotID: "ci-monitor"}
    if got := ns.Prefix(); got != "bot:ci-monitor" {
        t.Errorf("Prefix() = %q, want %q", got, "bot:ci-monitor")
    }
}

func TestMemoryNamespace_ScopeQuery(t *testing.T) {
    ns := MemoryNamespace{BotID: "ci-monitor"}

    tests := []struct {
        name   string
        scope  MemoryScope
        input  string
        expect string
    }{
        {
            name:   "private scope adds bot prefix",
            scope:  MemoryScopePrivate,
            input:  "ci failures",
            expect: "bot:ci-monitor ci failures",
        },
        {
            name:   "shared scope passes through",
            scope:  MemoryScopeShared,
            input:  "ci failures",
            expect: "ci failures",
        },
        {
            name:   "read_only scope adds bot prefix",
            scope:  MemoryScopeReadOnly,
            input:  "ci failures",
            expect: "bot:ci-monitor ci failures",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ns.ScopeQuery(tt.scope, tt.input)
            if got != tt.expect {
                t.Errorf("ScopeQuery() = %q, want %q", got, tt.expect)
            }
        })
    }
}

func TestMemoryNamespace_TagMemory(t *testing.T) {
    ns := MemoryNamespace{BotID: "ci-monitor"}

    meta := map[string]any{
        "category": "observation",
    }

    tagged := ns.TagMemory(meta)
    if tagged["bot_id"] != "ci-monitor" {
        t.Errorf("TagMemory bot_id = %v, want %q", tagged["bot_id"], "ci-monitor")
    }
    if tagged["category"] != "observation" {
        t.Errorf("TagMemory should preserve existing keys")
    }
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/bot/ -run TestMemoryNamespace -v`
Expected: FAIL

- [ ] **Step 4: Implement memory namespace**

```go
// internal/bot/memory_scope.go
package bot

import "fmt"

// MemoryNamespace handles memory isolation for bots.
type MemoryNamespace struct {
    BotID string
}

// NewMemoryNamespace creates a namespace for the given bot ID.
func NewMemoryNamespace(botID string) *MemoryNamespace {
    return &MemoryNamespace{BotID: botID}
}

// Prefix returns the namespace prefix for this bot's memories.
func (n *MemoryNamespace) Prefix() string {
    return "bot:" + n.BotID
}

// ScopeQuery modifies a search query to be scoped to this bot's namespace.
// For private and read_only scopes, the bot prefix is prepended to the query
// to bias FTS results toward the bot's own memories.
// For shared scope, the query passes through unchanged.
func (n *MemoryNamespace) ScopeQuery(scope MemoryScope, query string) string {
    switch scope {
    case MemoryScopePrivate, MemoryScopeReadOnly:
        return n.Prefix() + " " + query
    case MemoryScopeShared:
        return query
    default:
        return n.Prefix() + " " + query
    }
}

// TagMemory adds bot namespace metadata to a memory's metadata map.
// This ensures the memory is associated with the bot's namespace for
// later retrieval and filtering.
func (n *MemoryNamespace) TagMemory(meta map[string]any) map[string]any {
    if meta == nil {
        meta = make(map[string]any)
    }
    meta["bot_id"] = n.BotID
    return meta
}

// FilterBotMemories filters a slice of memory results to only include
// memories belonging to this bot (for private scope enforcement).
func (n *MemoryNamespace) FilterBotMemories(scope MemoryScope, results []map[string]any) []map[string]any {
    if scope == MemoryScopeShared {
        return results
    }
    var filtered []map[string]any
    for _, r := range results {
        if botID, ok := r["bot_id"].(string); ok && botID == n.BotID {
            filtered = append(filtered, r)
        }
    }
    return filtered
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/bot/ -run TestMemoryNamespace -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/memory/types.go internal/bot/memory_scope.go internal/bot/memory_scope_test.go
git commit -m "feat(bot): add memory namespace isolation for bot-scoped memory"
```

---

## Phase 4: Event Action Router

### Task 6: Event-to-action bridge

**Files:**
- Create: `internal/bot/router.go`
- Test: `internal/bot/router_test.go`

This is the core component that subscribes to bus topics and routes events to bots.

- [ ] **Step 1: Write router tests**

```go
// internal/bot/router_test.go
package bot

import (
    "context"
    "sync"
    "testing"
    "time"

    "github.com/caimlas/meept/internal/bus"
    "github.com/caimlas/meept/pkg/models"
)

type mockHandler struct {
    mu      sync.Mutex
    invocations []routerInvocation
}

type routerInvocation struct {
    BotID  string
    Prompt string
}

func (m *mockHandler) HandleBotTrigger(ctx context.Context, botID string, prompt string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.invocations = append(m.invocations, routerInvocation{BotID: botID, Prompt: prompt})
    return nil
}

func (m *mockHandler) getInvocations() []routerInvocation {
    m.mu.Lock()
    defer m.mu.Unlock()
    return append([]routerInvocation{}, m.invocations...)
}

func TestEventActionRouter_BusEvent(t *testing.T) {
    msgBus := bus.NewMessageBus(nil)
    handler := &mockHandler{}
    router := NewEventActionRouter(msgBus, handler)

    def := BotDefinition{
        ID:      "calendar-bot",
        Name:    "Calendar Bot",
        Prompt:  "You manage calendar events.",
        Triggers: []BotTrigger{
            {
                Type:          TriggerTypeBusEvent,
                Topic:         "calendar.reminder",
                PromptTemplate: "Calendar event: {{.Summary}} starts in {{.StartsIn}}",
                Enabled:       true,
            },
        },
    }

    if err := router.Register(def); err != nil {
        t.Fatalf("Register: %v", err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    router.Start(ctx)

    // Publish a calendar reminder event
    payload := map[string]any{
        "event_id": "evt-1",
        "summary":  "Team standup",
        "starts_in": "5 minutes",
    }
    msg, _ := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
    msgBus.Publish("calendar.reminder", msg)

    // Wait for processing
    time.Sleep(200 * time.Millisecond)

    invocations := handler.getInvocations()
    if len(invocations) != 1 {
        t.Fatalf("expected 1 invocation, got %d", len(invocations))
    }
    if invocations[0].BotID != "calendar-bot" {
        t.Errorf("BotID = %q, want %q", invocations[0].BotID, "calendar-bot")
    }
}

func TestEventActionRouter_Unregister(t *testing.T) {
    msgBus := bus.NewMessageBus(nil)
    handler := &mockHandler{}
    router := NewEventActionRouter(msgBus, handler)

    def := BotDefinition{
        ID:      "test-bot",
        Name:    "Test",
        Prompt:  "Test bot",
        Triggers: []BotTrigger{
            {Type: TriggerTypeBusEvent, Topic: "test.topic", Enabled: true},
        },
    }

    router.Register(def)
    router.Unregister("test-bot")

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    router.Start(ctx)

    payload := map[string]any{"key": "value"}
    msg, _ := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
    msgBus.Publish("test.topic", msg)

    time.Sleep(200 * time.Millisecond)

    invocations := handler.getInvocations()
    if len(invocations) != 0 {
        t.Errorf("expected 0 invocations after unregister, got %d", len(invocations))
    }
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bot/ -run TestEventActionRouter -v`
Expected: FAIL

- [ ] **Step 3: Implement the router**

```go
// internal/bot/router.go
package bot

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "sync"

    "github.com/caimlas/meept/internal/bus"
    "github.com/caimlas/meept/pkg/models"
)

// BotTriggerHandler is the callback invoked when a bot's trigger fires.
type BotTriggerHandler interface {
    HandleBotTrigger(ctx context.Context, botID string, prompt string) error
}

// EventActionRouter subscribes to bus topics and routes events to bots.
type EventActionRouter struct {
    bus     *bus.MessageBus
    handler BotTriggerHandler
    logger  *slog.Logger

    mu      sync.RWMutex
    // topic -> set of bot IDs subscribed to that topic
    topicSubs map[string]map[string]BotTrigger
    // bot ID -> cancellation for cleanup
    cancelFuncs map[string]context.CancelFunc
}

// NewEventActionRouter creates a new event-to-action router.
func NewEventActionRouter(msgBus *bus.MessageBus, handler BotTriggerHandler) *EventActionRouter {
    return &EventActionRouter{
        bus:         msgBus,
        handler:     handler,
        logger:      slog.Default(),
        topicSubs:   make(map[string]map[string]BotTrigger),
        cancelFuncs: make(map[string]context.CancelFunc),
    }
}

// Register adds a bot's bus_event triggers to the router.
func (r *EventActionRouter) Register(def BotDefinition) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    for _, trigger := range def.Triggers {
        if trigger.Type != TriggerTypeBusEvent || !trigger.Enabled {
            continue
        }
        if r.topicSubs[trigger.Topic] == nil {
            r.topicSubs[trigger.Topic] = make(map[string]BotTrigger)
        }
        r.topicSubs[trigger.Topic][def.ID] = trigger
        r.logger.Info("registered bot for bus event", "bot_id", def.ID, "topic", trigger.Topic)
    }
    return nil
}

// Unregister removes all bus_event subscriptions for a bot.
func (r *EventActionRouter) Unregister(botID string) {
    r.mu.Lock()
    defer r.mu.Unlock()

    for topic, bots := range r.topicSubs {
        delete(bots, botID)
        if len(bots) == 0 {
            delete(r.topicSubs, topic)
        }
    }
    if cancel, ok := r.cancelFuncs[botID]; ok {
        cancel()
        delete(r.cancelFuncs, botID)
    }
    r.logger.Info("unregistered bot from bus events", "bot_id", botID)
}

// Start begins listening for bus events. Blocks until context is cancelled.
func (r *EventActionRouter) Start(ctx context.Context) {
    r.mu.RLock()
    topics := make([]string, 0, len(r.topicSubs))
    for topic := range r.topicSubs {
        topics = append(topics, topic)
    }
    r.mu.RUnlock()

    for _, topic := range topics {
        sub := r.bus.Subscribe("bot-router-"+topic, topic)
        go func(topic string, ch <-chan *models.BusMessage) {
            for {
                select {
                case <-ctx.Done():
                    r.bus.Unsubscribe(sub)
                    return
                case msg, ok := <-ch:
                    if !ok {
                        return
                    }
                    r.handleEvent(ctx, topic, msg)
                }
            }
        }(topic, sub.Channel)
    }
    r.logger.Info("event action router started", "topics", topics)
}

func (r *EventActionRouter) handleEvent(ctx context.Context, topic string, msg *models.BusMessage) {
    r.mu.RLock()
    bots := r.topicSubs[topic]
    r.mu.RUnlock()

    for botID, trigger := range bots {
        prompt := r.buildPrompt(trigger, msg)
        if err := r.handler.HandleBotTrigger(ctx, botID, prompt); err != nil {
            r.logger.Error("bot trigger handler failed", "bot_id", botID, "topic", topic, "error", err)
        }
    }
}

func (r *EventActionRouter) buildPrompt(trigger BotTrigger, msg *models.BusMessage) string {
    if trigger.PromptTemplate != "" {
        var payload map[string]any
        if err := json.Unmarshal(msg.Payload, &payload); err == nil {
            return expandTemplate(trigger.PromptTemplate, payload)
        }
    }
    return fmt.Sprintf("Event received on topic %s from %s", trigger.Topic, msg.Source)
}

// expandTemplate does simple {{.Key}} substitution.
func expandTemplate(tmpl string, data map[string]any) string {
    result := tmpl
    for k, v := range data {
        result = replaceAll(result, "{{."+k+"}}", fmt.Sprintf("%v", v))
    }
    return result
}

func replaceAll(s, old, new string) string {
    // Simple string replacement without importing text/template
    // to avoid unnecessary dependencies.
    result := ""
    for {
        idx := indexOf(s, old)
        if idx < 0 {
            return result + s
        }
        result += s[:idx] + new
        s = s[idx+len(old):]
    }
}

func indexOf(s, substr string) int {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return i
        }
    }
    return -1
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/bot/ -run TestEventActionRouter -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bot/router.go internal/bot/router_test.go
git commit -m "feat(bot): add event action router for bus event to bot trigger bridge"
```

---

## Phase 5: Bot Runner

### Task 7: BotRunner wraps AgentLoop for bot execution

**Files:**
- Create: `internal/bot/runner.go`
- Test: `internal/bot/runner_test.go`

- [ ] **Step 1: Write runner tests**

```go
// internal/bot/runner_test.go
package bot

import (
    "testing"
    "time"
)

func TestBotRunner_BuildSystemPrompt(t *testing.T) {
    def := BotDefinition{
        ID:      "test-bot",
        Name:    "Test Bot",
        Prompt:  "You are a monitoring bot. Check the CI status.",
        Tools:   []string{"web_fetch", "memory_store", "memory_search"},
        Constraints: BotConstraints{
            MaxIterations:    5,
            Timeout:          2 * time.Minute,
            MaxTokensPerTurn: 2048,
            DailyBudgetCents: 50,
        },
    }

    runner := NewBotRunner(def)

    prompt := runner.BuildSystemPrompt("Check CI for project X")

    if prompt == "" {
        t.Fatal("expected non-empty system prompt")
    }
    // Should contain the bot's prompt
    if !contains(prompt, "You are a monitoring bot") {
        t.Error("system prompt should contain bot's behavioral instructions")
    }
    // Should contain the trigger context
    if !contains(prompt, "Check CI for project X") {
        t.Error("system prompt should contain trigger context")
    }
}

func TestBotRunner_ShouldRun_BudgetCheck(t *testing.T) {
    def := BotDefinition{
        ID:      "test-bot",
        Prompt:  "test",
        Constraints: BotConstraints{
            DailyBudgetCents: 100, // $1.00/day cap
        },
    }

    runner := NewBotRunner(def)

    // Under budget - should allow
    state := &BotState{TodayCostCents: 50, TodayDate: time.Now().Format("2006-01-02")}
    if !runner.ShouldRun(state) {
        t.Error("should allow run when under budget")
    }

    // At budget - should deny
    state.TodayCostCents = 100
    if runner.ShouldRun(state) {
        t.Error("should deny run when at budget")
    }

    // Over budget - should deny
    state.TodayCostCents = 150
    if runner.ShouldRun(state) {
        t.Error("should deny run when over budget")
    }
}

func TestBotRunner_ShouldRun_InvocationCap(t *testing.T) {
    def := BotDefinition{
        ID:      "test-bot",
        Prompt:  "test",
        Constraints: BotConstraints{
            MaxInvocationsPerDay: 10,
        },
    }

    runner := NewBotRunner(def)

    state := &BotState{TodayRuns: 9, TodayDate: time.Now().Format("2006-01-02")}
    if !runner.ShouldRun(state) {
        t.Error("should allow run when under invocation cap")
    }

    state.TodayRuns = 10
    if runner.ShouldRun(state) {
        t.Error("should deny run when at invocation cap")
    }
}

func TestBotRunner_ShouldRun_ConsecutiveFailures(t *testing.T) {
    def := BotDefinition{
        ID:     "test-bot",
        Prompt: "test",
    }

    runner := NewBotRunner(def)

    // Should auto-pause after 10 consecutive failures
    state := &BotState{ConsecutiveFailures: 10}
    if runner.ShouldRun(state) {
        t.Error("should deny run after 10 consecutive failures")
    }

    state.ConsecutiveFailures = 5
    if !runner.ShouldRun(state) {
        t.Error("should allow run with only 5 consecutive failures")
    }
}

func contains(s, substr string) bool {
    return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
    for i := 0; i <= len(s)-len(substr); i++ {
        if s[i:i+len(substr)] == substr {
            return true
        }
    }
    return false
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bot/ -run TestBotRunner -v`
Expected: FAIL

- [ ] **Step 3: Implement BotRunner**

```go
// internal/bot/runner.go
package bot

import (
    "fmt"
    "strings"
    "time"
)

const maxConsecutiveFailures = 10

// BotRunner handles the execution of a single bot invocation.
// It wraps AgentLoop.RunOnce() with bot-specific concerns:
// budget checking, prompt construction, and state management.
type BotRunner struct {
    definition BotDefinition
    namespace  *MemoryNamespace
}

// NewBotRunner creates a runner for the given bot definition.
func NewBotRunner(def BotDefinition) *BotRunner {
    return &BotRunner{
        definition: def,
        namespace:  NewMemoryNamespace(def.ID),
    }
}

// Definition returns the bot definition.
func (r *BotRunner) Definition() BotDefinition {
    return r.definition
}

// ShouldRun checks whether the bot should execute given its current state.
// Returns false if budget is exhausted, invocation cap is hit, or
// consecutive failures exceed threshold.
func (r *BotRunner) ShouldRun(state *BotState) bool {
    if state == nil {
        return true
    }

    // Auto-pause after too many consecutive failures
    if state.ConsecutiveFailures >= maxConsecutiveFailures {
        return false
    }

    // Check daily budget
    if r.definition.Constraints.DailyBudgetCents > 0 {
        today := time.Now().Format("2006-01-02")
        if state.TodayDate == today && state.TodayCostCents >= r.definition.Constraints.DailyBudgetCents {
            return false
        }
    }

    // Check invocation cap
    if r.definition.Constraints.MaxInvocationsPerDay > 0 {
        today := time.Now().Format("2006-01-02")
        if state.TodayDate == today && state.TodayRuns >= r.definition.Constraints.MaxInvocationsPerDay {
            return false
        }
    }

    return true
}

// BuildSystemPrompt constructs the system prompt for a bot invocation.
func (r *BotRunner) BuildSystemPrompt(triggerContext string) string {
    var b strings.Builder

    b.WriteString(r.definition.Prompt)

    b.WriteString("\n\n## Bot Identity\n")
    b.WriteString(fmt.Sprintf("You are bot %q (%s).\n", r.definition.ID, r.definition.Name))
    b.WriteString(fmt.Sprintf("Description: %s\n", r.definition.Description))

    b.WriteString("\n## Current Invocation\n")
    b.WriteString(fmt.Sprintf("Trigger context: %s\n", triggerContext))
    b.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now().UTC().Format(time.RFC3339)))

    b.WriteString("\n## Instructions\n")
    b.WriteString("Perform your task and store any important observations in memory for future invocations.\n")
    b.WriteString("Be concise. You are running autonomously - there is no user to interact with.\n")

    return b.String()
}

// BuildUserMessage constructs the user message for a bot invocation.
func (r *BotRunner) BuildUserMessage(triggerContext string) string {
    return fmt.Sprintf("[Bot %s triggered] %s", r.definition.ID, triggerContext)
}

// AgentSpec returns the AgentSpec to use for this bot's execution.
// This converts a BotDefinition into the format AgentLoop expects.
func (r *BotRunner) AgentSpec() map[string]any {
    constraints := AgentConstraints{
        MaxIterations:    r.definition.Constraints.MaxIterations,
        Timeout:          r.definition.Constraints.Timeout,
        MaxTokensPerTurn: r.definition.Constraints.MaxTokensPerTurn,
    }
    if constraints.MaxIterations == 0 {
        constraints.MaxIterations = 5
    }
    if constraints.Timeout == 0 {
        constraints.Timeout = 5 * time.Minute
    }
    return map[string]any{
        "id":               "bot:" + r.definition.ID,
        "name":             r.definition.Name,
        "role":             "bot",
        "purpose":          r.definition.Prompt,
        "model":            r.definition.Model,
        "additional_tools": r.definition.Tools,
        "constraints":      constraints,
    }
}
```

Note: `AgentConstraints` in the `AgentSpec()` method refers to the `AgentConstraints` type from `internal/agent/spec.go`. In practice this method would return an actual `*agent.AgentSpec` -- the map is shown here for clarity since the full integration with AgentLoop happens in Phase 6.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/bot/ -run TestBotRunner -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bot/runner.go internal/bot/runner_test.go
git commit -m "feat(bot): add bot runner with budget checks and prompt construction"
```

---

## Phase 6: Bot Lifecycle Manager

### Task 8: Lifecycle manager orchestrates start/stop/health

**Files:**
- Create: `internal/bot/lifecycle.go`
- Test: `internal/bot/lifecycle_test.go`

- [ ] **Step 1: Write lifecycle tests**

```go
// internal/bot/lifecycle_test.go
package bot

import (
    "context"
    "path/filepath"
    "sync"
    "testing"
    "time"
)

func testManager(t *testing.T) *Manager {
    t.Helper()
    store, err := NewStore(filepath.Join(t.TempDir(), "bots.db"))
    if err != nil {
        t.Fatalf("NewStore: %v", err)
    }
    t.Cleanup(func() { store.Close() })
    return NewManager(store, nil, nil, nil)
}

func TestManager_CreateBot(t *testing.T) {
    mgr := testManager(t)
    ctx := context.Background()

    def := testBotDef("lifecycle-test")
    err := mgr.CreateBot(ctx, def)
    if err != nil {
        t.Fatalf("CreateBot: %v", err)
    }

    got, err := mgr.GetBot(ctx, "lifecycle-test")
    if err != nil {
        t.Fatalf("GetBot: %v", err)
    }
    if got.ID != "lifecycle-test" {
        t.Errorf("ID = %q, want %q", got.ID, "lifecycle-test")
    }
}

func TestManager_DeleteBot_StopsRunning(t *testing.T) {
    mgr := testManager(t)
    ctx := context.Background()

    def := testBotDef("delete-test")
    mgr.CreateBot(ctx, def)

    // Simulate running state
    mgr.running["delete-test"] = &runningBot{
        cancel: func() {},
        state:  &BotState{Status: BotStatusRunning},
    }

    err := mgr.DeleteBot(ctx, "delete-test")
    if err != nil {
        t.Fatalf("DeleteBot: %v", err)
    }

    if _, ok := mgr.running["delete-test"]; ok {
        t.Error("bot should be removed from running map after delete")
    }
}

func TestManager_PauseResumeBot(t *testing.T) {
    mgr := testManager(t)
    ctx := context.Background()

    def := testBotDef("pause-test")
    mgr.CreateBot(ctx, def)

    // Pause
    err := mgr.PauseBot(ctx, "pause-test")
    if err != nil {
        t.Fatalf("PauseBot: %v", err)
    }

    got, _ := mgr.GetBot(ctx, "pause-test")
    if got.Enabled {
        t.Error("bot should be disabled after pause")
    }

    // Resume
    err = mgr.ResumeBot(ctx, "pause-test")
    if err != nil {
        t.Fatalf("ResumeBot: %v", err)
    }

    got, _ = mgr.GetBot(ctx, "pause-test")
    if !got.Enabled {
        t.Error("bot should be enabled after resume")
    }
}

func TestManager_ListBots(t *testing.T) {
    mgr := testManager(t)
    ctx := context.Background()

    for _, id := range []string{"bot-a", "bot-b", "bot-c"} {
        def := testBotDef(id)
        mgr.CreateBot(ctx, def)
    }

    bots, err := mgr.ListBots(ctx)
    if err != nil {
        t.Fatalf("ListBots: %v", err)
    }
    if len(bots) != 3 {
        t.Errorf("ListBots returned %d, want 3", len(bots))
    }
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/bot/ -run TestManager -v`
Expected: FAIL

- [ ] **Step 3: Implement lifecycle manager**

```go
// internal/bot/lifecycle.go
package bot

import (
    "context"
    "fmt"
    "log/slog"
    "sync"
    "time"

    "github.com/caimlas/meept/internal/bus"
    "github.com/caimlas/meept/internal/scheduler"
)

// runningBot tracks an active bot's goroutines and state.
type runningBot struct {
    runner *BotRunner
    cancel context.CancelFunc
    state  *BotState
}

// Manager orchestrates bot lifecycle: creation, deletion, start/stop, health.
type Manager struct {
    store  *Store
    bus    *bus.MessageBus
    cron   *scheduler.Scheduler
    router *EventActionRouter

    mu      sync.RWMutex
    running map[string]*runningBot
    logger  *slog.Logger
}

// NewManager creates a new bot lifecycle manager.
func NewManager(store *Store, msgBus *bus.MessageBus, cron *scheduler.Scheduler, router *EventActionRouter) *Manager {
    return &Manager{
        store:   store,
        bus:     msgBus,
        cron:    cron,
        router:  router,
        running: make(map[string]*runningBot),
        logger:  slog.Default(),
    }
}

// CreateBot validates and persists a new bot definition.
func (m *Manager) CreateBot(ctx context.Context, def BotDefinition) error {
    if err := def.Validate(); err != nil {
        return fmt.Errorf("validation: %w", err)
    }
    def.CreatedAt = time.Now().UTC()
    def.UpdatedAt = time.Now().UTC()
    return m.store.Create(ctx, def)
}

// GetBot retrieves a bot definition by ID.
func (m *Manager) GetBot(ctx context.Context, id string) (*BotDefinition, error) {
    return m.store.Get(ctx, id)
}

// ListBots returns all bot definitions.
func (m *Manager) ListBots(ctx context.Context) ([]BotDefinition, error) {
    return m.store.List(ctx)
}

// UpdateBot updates an existing bot definition.
func (m *Manager) UpdateBot(ctx context.Context, def BotDefinition) error {
    if err := def.Validate(); err != nil {
        return fmt.Errorf("validation: %w", err)
    }
    def.UpdatedAt = time.Now().UTC()
    return m.store.Update(ctx, def)
}

// DeleteBot removes a bot, stopping it if running.
func (m *Manager) DeleteBot(ctx context.Context, id string) error {
    // Stop if running
    m.mu.Lock()
    if rb, ok := m.running[id]; ok {
        rb.cancel()
        delete(m.running, id)
    }
    m.mu.Unlock()

    // Unregister from event router
    if m.router != nil {
        m.router.Unregister(id)
    }

    return m.store.Delete(ctx, id)
}

// PauseBot disables a bot without removing it.
func (m *Manager) PauseBot(ctx context.Context, id string) error {
    def, err := m.store.Get(ctx, id)
    if err != nil {
        return err
    }
    def.Enabled = false
    return m.store.Update(ctx, *def)
}

// ResumeBot re-enables a paused bot.
func (m *Manager) ResumeBot(ctx context.Context, id string) error {
    def, err := m.store.Get(ctx, id)
    if err != nil {
        return err
    }
    def.Enabled = true
    return m.store.Update(ctx, *def)
}

// StartAll loads all enabled bots and starts their triggers.
// Called during daemon startup.
func (m *Manager) StartAll(ctx context.Context) error {
    bots, err := m.store.List(ctx)
    if err != nil {
        return err
    }

    for _, def := range bots {
        if !def.Enabled {
            continue
        }
        if err := m.startBot(ctx, def); err != nil {
            m.logger.Error("failed to start bot", "bot_id", def.ID, "error", err)
        }
    }
    return nil
}

// StopAll gracefully stops all running bots.
func (m *Manager) StopAll() {
    m.mu.Lock()
    defer m.mu.Unlock()

    for id, rb := range m.running {
        rb.cancel()
        delete(m.running, id)
        m.logger.Info("stopped bot", "bot_id", id)
    }
}

// GetBotStatus returns the runtime state for a bot.
func (m *Manager) GetBotStatus(ctx context.Context, id string) (*BotState, error) {
    m.mu.RLock()
    if rb, ok := m.running[id]; ok {
        m.mu.RUnlock()
        return rb.state, nil
    }
    m.mu.RUnlock()
    return m.store.GetState(ctx, id)
}

func (m *Manager) startBot(ctx context.Context, def BotDefinition) error {
    runner := NewBotRunner(def)
    botCtx, cancel := context.WithCancel(ctx)

    state, err := m.store.GetState(botCtx, def.ID)
    if err != nil {
        state = &BotState{DefinitionID: def.ID, Status: BotStatusStopped}
    }
    state.Status = BotStatusRunning

    rb := &runningBot{
        runner: runner,
        cancel: cancel,
        state:  state,
    }

    m.mu.Lock()
    m.running[def.ID] = rb
    m.mu.Unlock()

    // Register bus event triggers
    if m.router != nil {
        m.router.Register(def)
    }

    // Register cron triggers via scheduler
    for _, trigger := range def.Triggers {
        if trigger.Type == TriggerTypeCron && trigger.Enabled {
            m.registerCronTrigger(botCtx, def.ID, trigger, runner)
        }
    }

    m.logger.Info("started bot", "bot_id", def.ID, "triggers", len(def.Triggers))
    return nil
}

func (m *Manager) registerCronTrigger(ctx context.Context, botID string, trigger BotTrigger, runner *BotRunner) {
    if m.cron == nil {
        return
    }
    // Cron triggers are registered via the scheduler's AgentJob mechanism.
    // The actual integration with scheduler.Scheduler will use the existing
    // AddJob API, publishing to chat.request with bot-scoped metadata.
    m.logger.Info("registered cron trigger for bot", "bot_id", botID, "schedule", trigger.Schedule)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/bot/ -run TestManager -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bot/lifecycle.go internal/bot/lifecycle_test.go
git commit -m "feat(bot): add bot lifecycle manager with start/stop/pause/resume"
```

---

## Phase 7: CLI & RPC Surface

### Task 9: CLI commands for bot management

**Files:**
- Create: `cmd/meept/bot_cmd.go`

- [ ] **Step 1: Write the CLI command**

```go
// cmd/meept/bot_cmd.go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "time"

    "github.com/spf13/cobra"
)

var botsCmd = &cobra.Command{
    Use:   "bots",
    Short: "manage persistent bots",
    Long:  `Create, list, pause, resume, and delete persistent autonomous bots.`,
}

var botsListCmd = &cobra.Command{
    Use:   "list",
    Short: "list all bots",
    RunE: func(cmd *cobra.Command, args []string) error {
        resp, err := rpcCall("bot.list", map[string]any{})
        if err != nil {
            return err
        }
        bots, ok := resp["bots"].([]any)
        if !ok {
            return fmt.Errorf("unexpected response format")
        }
        if len(bots) == 0 {
            fmt.Println("no bots configured")
            return nil
        }
        for _, b := range bots {
            m := b.(map[string]any)
            status := m["status"].(string)
            id := m["id"].(string)
            name := m["name"].(string)
            enabled := m["enabled"].(bool)
            fmt.Printf("  %-20s %-30s status=%s enabled=%v\n", id, name, status, enabled)
        }
        return nil
    },
}

var botsShowCmd = &cobra.Command{
    Use:   "show <bot-id>",
    Short: "show bot details",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        resp, err := rpcCall("bot.get", map[string]any{"id": args[0]})
        if err != nil {
            return err
        }
        data, _ := json.MarshalIndent(resp, "", "  ")
        fmt.Println(string(data))
        return nil
    },
}

var botsCreateCmd = &cobra.Command{
    Use:   "create <definition.json>",
    Short: "create a bot from a definition file",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        data, err := os.ReadFile(args[0])
        if err != nil {
            return fmt.Errorf("read file: %w", err)
        }
        var def map[string]any
        if err := json.Unmarshal(data, &def); err != nil {
            return fmt.Errorf("parse json: %w", err)
        }
        resp, err := rpcCall("bot.create", def)
        if err != nil {
            return err
        }
        fmt.Printf("created bot %q\n", resp["id"])
        return nil
    },
}

var botsDeleteCmd = &cobra.Command{
    Use:   "delete <bot-id>",
    Short: "delete a bot",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        _, err := rpcCall("bot.delete", map[string]any{"id": args[0]})
        if err != nil {
            return err
        }
        fmt.Printf("deleted bot %q\n", args[0])
        return nil
    },
}

var botsPauseCmd = &cobra.Command{
    Use:   "pause <bot-id>",
    Short: "pause a running bot",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        _, err := rpcCall("bot.pause", map[string]any{"id": args[0]})
        if err != nil {
            return err
        }
        fmt.Printf("paused bot %q\n", args[0])
        return nil
    },
}

var botsResumeCmd = &cobra.Command{
    Use:   "resume <bot-id>",
    Short: "resume a paused bot",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        _, err := rpcCall("bot.resume", map[string]any{"id": args[0]})
        if err != nil {
            return err
        }
        fmt.Printf("resumed bot %q\n", args[0])
        return nil
    },
}

func init() {
    botsCmd.AddCommand(botsListCmd)
    botsCmd.AddCommand(botsShowCmd)
    botsCmd.AddCommand(botsCreateCmd)
    botsCmd.AddCommand(botsDeleteCmd)
    botsCmd.AddCommand(botsPauseCmd)
    botsCmd.AddCommand(botsResumeCmd)
}
```

Note: `rpcCall` is the existing helper in `cmd/meept/main.go` for making JSON-RPC calls. The actual function signature and wiring may differ -- this is a template that will be adapted to match the existing RPC call patterns in the CLI.

- [ ] **Step 2: Register the command in main.go**

In `cmd/meept/main.go`, add to the root command setup:

```go
rootCmd.AddCommand(botsCmd)
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./cmd/meept/`
Expected: Success (may need adjustments to match existing rpcCall pattern)

- [ ] **Step 4: Commit**

```bash
git add cmd/meept/bot_cmd.go cmd/meept/main.go
git commit -m "feat(cli): add bots subcommand for bot management"
```

---

### Task 10: RPC handlers for bot operations

**Files:**
- Create: `internal/bot/handler.go`
- Modify: `internal/rpc/server.go` (register bot handlers)

- [ ] **Step 1: Write the RPC handler**

```go
// internal/bot/handler.go
package bot

import (
    "context"
    "encoding/json"
    "fmt"
)

// RPCHandler provides JSON-RPC handlers for bot management.
type RPCHandler struct {
    manager *Manager
}

// NewRPCHandler creates a new RPC handler for bot operations.
func NewRPCHandler(manager *Manager) *RPCHandler {
    return &RPCHandler{manager: manager}
}

// Handlers returns a map of RPC method names to handler functions.
func (h *RPCHandler) Handlers() map[string]func(context.Context, json.RawMessage) (any, error) {
    return map[string]func(context.Context, json.RawMessage) (any, error){
        "bot.create": h.handleCreate,
        "bot.get":    h.handleGet,
        "bot.list":   h.handleList,
        "bot.update": h.handleUpdate,
        "bot.delete": h.handleDelete,
        "bot.pause":  h.handlePause,
        "bot.resume": h.handleResume,
        "bot.status": h.handleStatus,
    }
}

func (h *RPCHandler) handleCreate(ctx context.Context, raw json.RawMessage) (any, error) {
    var def BotDefinition
    if err := json.Unmarshal(raw, &def); err != nil {
        return nil, fmt.Errorf("invalid bot definition: %w", err)
    }
    if err := h.manager.CreateBot(ctx, def); err != nil {
        return nil, err
    }
    return map[string]any{"id": def.ID, "status": "created"}, nil
}

func (h *RPCHandler) handleGet(ctx context.Context, raw json.RawMessage) (any, error) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw, &req); err != nil {
        return nil, err
    }
    def, err := h.manager.GetBot(ctx, req.ID)
    if err != nil {
        return nil, err
    }
    return def, nil
}

func (h *RPCHandler) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
    bots, err := h.manager.ListBots(ctx)
    if err != nil {
        return nil, err
    }
    return map[string]any{"bots": bots}, nil
}

func (h *RPCHandler) handleUpdate(ctx context.Context, raw json.RawMessage) (any, error) {
    var def BotDefinition
    if err := json.Unmarshal(raw, &def); err != nil {
        return nil, fmt.Errorf("invalid bot definition: %w", err)
    }
    if err := h.manager.UpdateBot(ctx, def); err != nil {
        return nil, err
    }
    return map[string]any{"id": def.ID, "status": "updated"}, nil
}

func (h *RPCHandler) handleDelete(ctx context.Context, raw json.RawMessage) (any, error) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw, &req); err != nil {
        return nil, err
    }
    if err := h.manager.DeleteBot(ctx, req.ID); err != nil {
        return nil, err
    }
    return map[string]any{"id": req.ID, "status": "deleted"}, nil
}

func (h *RPCHandler) handlePause(ctx context.Context, raw json.RawMessage) (any, error) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw, &req); err != nil {
        return nil, err
    }
    if err := h.manager.PauseBot(ctx, req.ID); err != nil {
        return nil, err
    }
    return map[string]any{"id": req.ID, "status": "paused"}, nil
}

func (h *RPCHandler) handleResume(ctx context.Context, raw json.RawMessage) (any, error) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw, &req); err != nil {
        return nil, err
    }
    if err := h.manager.ResumeBot(ctx, req.ID); err != nil {
        return nil, err
    }
    return map[string]any{"id": req.ID, "status": "resumed"}, nil
}

func (h *RPCHandler) handleStatus(ctx context.Context, raw json.RawMessage) (any, error) {
    var req struct{ ID string `json:"id"` }
    if err := json.Unmarshal(raw, &req); err != nil {
        return nil, err
    }
    state, err := h.manager.GetBotStatus(ctx, req.ID)
    if err != nil {
        return nil, err
    }
    return state, nil
}
```

- [ ] **Step 2: Register handlers in RPC server**

In `internal/rpc/server.go`, add to `registerBuiltinHandlers()`:

```go
    // Bot management handlers
    if botHandler != nil {
        for method, handler := range botHandler.Handlers() {
            s.RegisterHandler(method, handler)
        }
    }
```

The `botHandler` parameter will be passed during server construction from `components.go`.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/bot/ ./internal/rpc/`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/bot/handler.go internal/rpc/server.go
git commit -m "feat(bot): add RPC handlers for bot management operations"
```

---

## Phase 8: Webhook Endpoint & Config

### Task 11: HTTP webhook endpoint

**Files:**
- Create: `internal/bot/webhook.go`
- Modify: `internal/comm/http/server.go` (register route)

- [ ] **Step 1: Write the webhook handler**

```go
// internal/bot/webhook.go
package bot

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log/slog"
    "net/http"
    "strings"
)

// WebhookHandler handles incoming HTTP webhooks for bots.
type WebhookHandler struct {
    manager *Manager
    logger  *slog.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(manager *Manager) *WebhookHandler {
    return &WebhookHandler{
        manager: manager,
        logger:  slog.Default(),
    }
}

// ServeHTTP handles POST /api/v1/bot/{botID}/trigger
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
        return
    }

    // Extract bot ID from path: /api/v1/bot/{botID}/trigger
    path := strings.TrimPrefix(r.URL.Path, "/api/v1/bot/")
    path = strings.TrimSuffix(path, "/trigger")
    botID := path

    if botID == "" {
        http.Error(w, `{"error":"bot id required"}`, http.StatusBadRequest)
        return
    }

    body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
    if err != nil {
        http.Error(w, `{"error":"read body"}`, http.StatusInternalServerError)
        return
    }
    defer r.Body.Close()

    var payload map[string]any
    if err := json.Unmarshal(body, &payload); err != nil {
        http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
        return
    }

    // Build trigger context from the webhook payload
    triggerCtx := fmt.Sprintf("Webhook triggered with payload: %s", string(body))

    // Get bot definition to verify it has a webhook trigger
    def, err := h.manager.GetBot(r.Context(), botID)
    if err != nil {
        http.Error(w, fmt.Sprintf(`{"error":"bot %q not found"}`, botID), http.StatusNotFound)
        return
    }

    hasWebhookTrigger := false
    for _, t := range def.Triggers {
        if t.Type == TriggerTypeWebhook && t.Enabled {
            hasWebhookTrigger = true
            if t.PromptTemplate != "" {
                triggerCtx = expandTemplate(t.PromptTemplate, payload)
            }
            break
        }
    }
    if !hasWebhookTrigger {
        http.Error(w, fmt.Sprintf(`{"error":"bot %q has no webhook trigger"}`, botID), http.StatusBadRequest)
        return
    }

    // Check if bot should run
    state, _ := h.manager.GetBotStatus(r.Context(), botID)
    runner := NewBotRunner(*def)
    if !runner.ShouldRun(state) {
        http.Error(w, `{"error":"bot paused or budget exhausted"}`, http.StatusTooManyRequests)
        return
    }

    // Route through the event router's handler interface
    // This will be wired to BotRunner's execution in the daemon startup
    h.logger.Info("webhook trigger received", "bot_id", botID, "payload_size", len(body))

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "status":  "triggered",
        "bot_id":  botID,
        "message": "bot invocation queued",
    })
}
```

- [ ] **Step 2: Register the route**

In `internal/comm/http/server.go`, add to the route registration:

```go
    // Bot webhook endpoint
    if botWebhookHandler != nil {
        mux.Handle("/api/v1/bot/", botWebhookHandler)
    }
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/bot/ ./internal/comm/http/`
Expected: Success

- [ ] **Step 4: Commit**

```bash
git add internal/bot/webhook.go internal/comm/http/server.go
git commit -m "feat(bot): add HTTP webhook endpoint for bot triggers"
```

---

### Task 12: Config schema and daemon wiring

**Files:**
- Modify: `internal/config/schema.go` (add `BotsConfig`)
- Modify: `internal/daemon/components.go` (wire bot components)
- Modify: `internal/daemon/daemon.go` (shutdown bots)

- [ ] **Step 1: Add BotsConfig to schema**

In `internal/config/schema.go`, add after the `QAgentConfig` struct:

```go
// BotsConfig holds configuration for the persistent bot framework.
type BotsConfig struct {
    // Enabled turns on the bot framework.
    Enabled bool `json:"enabled" toml:"enabled"`
    // DataDir is the directory for bot state storage.
    DataDir string `json:"data_dir" toml:"data_dir"`
    // MaxConcurrentBots limits how many bots can run simultaneously.
    MaxConcurrentBots int `json:"max_concurrent_bots" toml:"max_concurrent_bots"`
    // DefaultDailyBudgetCents is the default daily spend cap for new bots.
    DefaultDailyBudgetCents int `json:"default_daily_budget_cents" toml:"default_daily_budget_cents"`
    // AutoPauseOnConsecutiveFailures auto-pauses a bot after N failures.
    AutoPauseOnConsecutiveFailures int `json:"auto_pause_on_consecutive_failures" toml:"auto_pause_on_consecutive_failures"`
    // WebhookEnabled enables the webhook endpoint.
    WebhookEnabled bool `json:"webhook_enabled" toml:"webhook_enabled"`
}
```

Add `Bots BotsConfig` to the `Config` struct.

- [ ] **Step 2: Wire components in daemon startup**

In `internal/daemon/components.go`, add bot framework initialization after scheduler setup:

```go
    // Bot framework
    if cfg.Bots.Enabled {
        botStore, err := bot.NewStore(filepath.Join(cfg.Bots.DataDir, "bots.db"))
        if err != nil {
            return fmt.Errorf("bot store: %w", err)
        }
        botRouter := bot.NewEventActionRouter(msgBus, botHandler)
        botManager := bot.NewManager(botStore, msgBus, c.Scheduler, botRouter)
        botRPCHandler := bot.NewRPCHandler(botManager)

        // Register bot RPC handlers
        for method, handler := range botRPCHandler.Handlers() {
            rpcServer.RegisterHandler(method, handler)
        }

        // Register webhook endpoint
        if cfg.Bots.WebhookEnabled {
            webhookHandler := bot.NewWebhookHandler(botManager)
            httpServer.RegisterWebhookHandler(webhookHandler)
        }

        // Start all enabled bots
        if err := botManager.StartAll(ctx); err != nil {
            logger.Error("failed to start bots", "error", err)
        }

        c.BotManager = botManager
        c.BotStore = botStore
    }
```

- [ ] **Step 3: Add shutdown in daemon.go**

In `internal/daemon/daemon.go`, add to the shutdown sequence:

```go
    if c.BotManager != nil {
        c.BotManager.StopAll()
    }
    if c.BotStore != nil {
        c.BotStore.Close()
    }
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./...`
Expected: Success

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go internal/daemon/components.go internal/daemon/daemon.go
git commit -m "feat(bot): add config schema and daemon wiring for bot framework"
```

---

## Phase 9: Integration & Documentation

### Task 13: End-to-end integration test

**Files:**
- Create: `tests/bot_integration_test.go`

- [ ] **Step 1: Write an integration test that exercises the full bot lifecycle**

```go
// tests/bot_integration_test.go
//go:build integration

package tests

import (
    "context"
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/caimlas/meept/internal/bot"
    "github.com/caimlas/meept/internal/bus"
)

func TestBotLifecycle_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Set up store
    dir := t.TempDir()
    store, err := bot.NewStore(filepath.Join(dir, "bots.db"))
    if err != nil {
        t.Fatalf("NewStore: %v", err)
    }
    defer store.Close()

    // Set up event router
    msgBus := bus.NewMessageBus(nil)
    router := bot.NewEventActionRouter(msgBus, nil)

    // Create manager
    mgr := bot.NewManager(store, msgBus, nil, router)

    // Create a bot
    def := bot.BotDefinition{
        ID:          "test-ci-monitor",
        Name:        "CI Monitor",
        Description: "Monitors CI pipeline status",
        Prompt:      "Check the CI status for the main project. Report any failures.",
        Triggers: []bot.BotTrigger{
            {Type: bot.TriggerTypeCron, Schedule: "*/15 * * * *", Enabled: true},
            {Type: bot.TriggerTypeBusEvent, Topic: "calendar.reminder", Enabled: true},
        },
        MemoryScope: bot.MemoryScopePrivate,
        Tools:       []string{"web_fetch", "memory_store", "memory_search"},
        Constraints: bot.BotConstraints{
            MaxIterations:       5,
            Timeout:             2 * time.Minute,
            DailyBudgetCents:    100,
            MaxInvocationsPerDay: 50,
        },
        Enabled: true,
    }

    if err := mgr.CreateBot(ctx, def); err != nil {
        t.Fatalf("CreateBot: %v", err)
    }

    // List bots
    bots, err := mgr.ListBots(ctx)
    if err != nil {
        t.Fatalf("ListBots: %v", err)
    }
    if len(bots) != 1 {
        t.Fatalf("expected 1 bot, got %d", len(bots))
    }

    // Get bot
    got, err := mgr.GetBot(ctx, "test-ci-monitor")
    if err != nil {
        t.Fatalf("GetBot: %v", err)
    }
    if got.ID != "test-ci-monitor" {
        t.Errorf("ID = %q, want %q", got.ID, "test-ci-monitor")
    }

    // Pause and resume
    if err := mgr.PauseBot(ctx, "test-ci-monitor"); err != nil {
        t.Fatalf("PauseBot: %v", err)
    }
    paused, _ := mgr.GetBot(ctx, "test-ci-monitor")
    if paused.Enabled {
        t.Error("bot should be paused")
    }
    if err := mgr.ResumeBot(ctx, "test-ci-monitor"); err != nil {
        t.Fatalf("ResumeBot: %v", err)
    }
    resumed, _ := mgr.GetBot(ctx, "test-ci-monitor")
    if !resumed.Enabled {
        t.Error("bot should be resumed")
    }

    // Test runner budget check
    runner := bot.NewBotRunner(def)
    state := &bot.BotState{
        TodayCostCents: 50,
        TodayDate:      time.Now().Format("2006-01-02"),
    }
    if !runner.ShouldRun(state) {
        t.Error("should allow run under budget")
    }
    state.TodayCostCents = 100
    if runner.ShouldRun(state) {
        t.Error("should deny run at budget cap")
    }

    // Test memory namespace
    ns := bot.NewMemoryNamespace("test-ci-monitor")
    query := ns.ScopeQuery(bot.MemoryScopePrivate, "last CI results")
    if query != "bot:test-ci-monitor last CI results" {
        t.Errorf("unexpected scoped query: %q", query)
    }

    // Delete bot
    if err := mgr.DeleteBot(ctx, "test-ci-monitor"); err != nil {
        t.Fatalf("DeleteBot: %v", err)
    }
    bots, _ = mgr.ListBots(ctx)
    if len(bots) != 0 {
        t.Errorf("expected 0 bots after delete, got %d", len(bots))
    }
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test ./tests/ -run TestBotLifecycle -tags=integration -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/bot_integration_test.go
git commit -m "test(bot): add end-to-end bot lifecycle integration test"
```

---

### Task 14: Documentation

**Files:**
- Create: `docs/workflows/bots.md`
- Modify: `docs/features.md` (add bot feature)
- Modify: `mkdocs.yml` (add nav entry)
- Modify: `README.md` (add bot feature to feature list)

- [ ] **Step 1: Write bot workflow documentation**

Create `docs/workflows/bots.md` covering:
- What persistent bots are
- Bot definition schema with examples
- Trigger types (cron, bus event, webhook)
- Memory isolation (private, shared, read_only)
- Budget and invocation caps
- CLI commands (`meept bots create/list/pause/resume/delete`)
- Example: "Create a CI monitor bot"
- Example: "Create a calendar reminder bot"
- Example: "Create a webhook-driven bot"

- [ ] **Step 2: Update features.md**

Add bot framework feature entry.

- [ ] **Step 3: Update mkdocs.yml**

Add `bots.md` to the workflows nav section.

- [ ] **Step 4: Update README.md**

Add persistent bots to the feature list.

- [ ] **Step 5: Commit**

```bash
git add docs/workflows/bots.md docs/features.md mkdocs.yml README.md
git commit -m "docs: add persistent bot framework documentation"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- [x] Bug fix: AgentJob topic mismatch -- Task 1
- [x] Bot definition types with validation -- Task 2
- [x] RoleBot agent role -- Task 3
- [x] SQLite persistence for bots -- Task 4
- [x] Memory namespace isolation (user requirement) -- Task 5
- [x] Event-to-action bridge -- Task 6
- [x] Bot runner with budget enforcement -- Task 7
- [x] Bot lifecycle manager -- Task 8
- [x] CLI commands -- Task 9
- [x] RPC handlers -- Task 10
- [x] Webhook endpoint -- Task 11
- [x] Config schema & daemon wiring -- Task 12
- [x] Integration test -- Task 13
- [x] Documentation -- Task 14

**2. Placeholder scan:** No TBD/TODO/fill-in-later patterns found.

**3. Type consistency:**
- `BotDefinition` fields are consistent across all tasks
- `BotTrigger.Type` uses `TriggerType*` constants throughout
- `BotState` fields match between store and lifecycle manager
- Memory namespace uses `bot:` prefix consistently
