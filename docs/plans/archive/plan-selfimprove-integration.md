# Plan: Self-Improve Integration

**Status:** Not Started
**Priority:** Low
**Estimated Effort:** 3-4 days

---

## Current State

The self-improvement system is **fully implemented** but **not integrated** into the daemon lifecycle:

| Component | File | Status |
|-----------|------|--------|
| Controller | `internal/selfimprove/controller.go` | Implemented (415 lines) |
| IssueDetector | `internal/selfimprove/detector.go` | Implemented |
| RootCauseAnalyzer | `internal/selfimprove/analyzer.go` | Implemented |
| PatchGenerator | `internal/selfimprove/generator.go` | Implemented |
| FixValidator | `internal/selfimprove/validator.go` | Implemented |
| ChangeApplier | `internal/selfimprove/applier.go` | Implemented |
| Models | `internal/selfimprove/models.go` | Implemented |
| Config | `internal/selfimprove/config.go` | Implemented |

### What Exists

1. **Full 5-Phase Cycle** (`controller.go`)
   - Phase 1: Detection - Find issues in codebase
   - Phase 2: Analysis - LLM-powered root cause analysis
   - Phase 3: Generation - Generate fix patches
   - Phase 4: Validation - Test fixes in sandbox
   - Phase 5: Application - Apply validated fixes

2. **Safety Features**
   - Circuit breaker pattern
   - Per-issue failure tracking
   - Human approval mode
   - Max iterations per cycle
   - Max fixes per cycle

3. **State Persistence**
   - Saves state to JSON
   - Tracks cycles, issues, analyses, fixes, validations

4. **CLI Commands** (exist but not connected)
   - `selfimprove detect`
   - `selfimprove full-cycle`
   - `selfimprove status`

### What's Missing

1. **No daemon integration** - Controller not initialized on startup
2. **No RPC handlers** - Can't invoke via RPC
3. **No scheduled runs** - No automatic cycle triggering
4. **No message bus integration** - Status not published
5. **No approval UI** - Human approval not implemented

---

## Implementation Plan

### Phase 1: Daemon Integration

**File:** `internal/daemon/components.go`

**Changes:**

1. Add self-improve controller to components:
```go
type Components struct {
    // ... existing fields
    selfImprove *selfimprove.Controller
}

func NewComponents(cfg *config.Config, ...) (*Components, error) {
    // ...

    // Initialize self-improve
    var selfImproveCtrl *selfimprove.Controller
    if cfg.SelfImprove.Enabled {
        selfImproveCtrl = selfimprove.NewController(
            selfimprove.Config{
                DataPath:             filepath.Join(cfg.DataDir, "selfimprove"),
                MaxIterationsPerCycle: cfg.SelfImprove.MaxIterations,
                MaxFixesPerCycle:      cfg.SelfImprove.MaxFixes,
                Safety: selfimprove.SafetyConfig{
                    RequireHumanApproval:    cfg.SelfImprove.RequireApproval,
                    MaxConsecutiveFailures:  3,
                    MaxFailuresPerIssue:     2,
                },
            },
            msgBus,
            llmClient,
            projectRoot,
            logger,
        )

        if err := selfImproveCtrl.Initialize(ctx); err != nil {
            logger.Warn("failed to initialize self-improve", "error", err)
        }
    }

    c.selfImprove = selfImproveCtrl
    // ...
}
```

2. Add shutdown:
```go
func (c *Components) Stop() error {
    if c.selfImprove != nil {
        c.selfImprove.Stop()
    }
    // ...
}
```

### Phase 2: RPC Handlers

**File:** `internal/rpc/proxy.go`

**Changes:**

1. Add self-improve RPC endpoints:
```go
func (p *Proxy) RegisterHandlers() {
    // ...
    p.Handle("selfimprove.detect", p.handleSelfImproveDetect)
    p.Handle("selfimprove.run", p.handleSelfImproveRun)
    p.Handle("selfimprove.status", p.handleSelfImproveStatus)
    p.Handle("selfimprove.approve", p.handleSelfImproveApprove)
    p.Handle("selfimprove.reject", p.handleSelfImproveReject)
}

func (p *Proxy) handleSelfImproveDetect(ctx context.Context, params json.RawMessage) (any, error) {
    if p.selfImprove == nil {
        return nil, fmt.Errorf("self-improve not enabled")
    }

    issues, err := p.selfImprove.Detect(ctx)
    if err != nil {
        return nil, err
    }

    return map[string]any{
        "issues": issues,
        "count":  len(issues),
    }, nil
}

func (p *Proxy) handleSelfImproveRun(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        Interactive bool `json:"interactive"`
    }
    json.Unmarshal(params, &req)

    cycle, err := p.selfImprove.RunFullCycle(ctx, req.Interactive)
    if err != nil {
        return nil, err
    }

    return cycle, nil
}

func (p *Proxy) handleSelfImproveStatus(ctx context.Context, params json.RawMessage) (any, error) {
    if p.selfImprove == nil {
        return nil, fmt.Errorf("self-improve not enabled")
    }

    return p.selfImprove.GetStatus(), nil
}

func (p *Proxy) handleSelfImproveApprove(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        FixID string `json:"fix_id"`
    }
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }

    // Call applier to approve pending fix
    return p.selfImprove.ApproveFix(ctx, req.FixID)
}
```

### Phase 3: Scheduled Cycles

**File:** `internal/selfimprove/scheduler.go` (new)

```go
package selfimprove

import (
    "context"
    "log/slog"
    "time"
)

// Scheduler runs self-improvement cycles on a schedule.
type Scheduler struct {
    controller *Controller
    interval   time.Duration
    logger     *slog.Logger
    stopCh     chan struct{}
}

// NewScheduler creates a new scheduler.
func NewScheduler(ctrl *Controller, interval time.Duration, logger *slog.Logger) *Scheduler {
    return &Scheduler{
        controller: ctrl,
        interval:   interval,
        logger:     logger,
        stopCh:     make(chan struct{}),
    }
}

// Start starts the scheduled cycles.
func (s *Scheduler) Start(ctx context.Context) {
    ticker := time.NewTicker(s.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-s.stopCh:
            return
        case <-ticker.C:
            s.logger.Info("starting scheduled self-improvement cycle")
            cycle, err := s.controller.RunFullCycle(ctx, false)
            if err != nil {
                s.logger.Error("scheduled cycle failed", "error", err)
                continue
            }
            s.logger.Info("scheduled cycle completed",
                "detected", cycle.IssuesDetected,
                "applied", cycle.FixesApplied)
        }
    }
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
    close(s.stopCh)
}
```

### Phase 4: Message Bus Integration

**File:** `internal/selfimprove/controller.go`

**Changes:**

1. Implement `publishStatus`:
```go
func (c *Controller) publishStatus(phase string, data any) {
    if c.bus == nil {
        return
    }

    msg := bus.Message{
        Topic: "selfimprove.status",
        Data: map[string]any{
            "phase":     phase,
            "data":      data,
            "timestamp": time.Now(),
        },
    }

    if err := c.bus.Publish(msg); err != nil {
        c.logger.Warn("failed to publish status", "error", err)
    }
}
```

2. Add progress callbacks:
```go
type ProgressCallback func(phase string, progress float64, message string)

func (c *Controller) SetProgressCallback(cb ProgressCallback) {
    c.progressCallback = cb
}
```

### Phase 5: Approval Workflow

**File:** `internal/selfimprove/applier.go`

**Changes:**

1. Add approval methods to ChangeApplier:
```go
func (a *ChangeApplier) ApproveFix(ctx context.Context, fixID string) (*AppliedFix, error) {
    pending := a.pendingApprovals[fixID]
    if pending == nil {
        return nil, fmt.Errorf("fix %s not pending approval", fixID)
    }

    // Apply the fix
    result, err := a.applyPatch(ctx, pending.fix.Patches)
    if err != nil {
        return nil, err
    }

    delete(a.pendingApprovals, fixID)

    applied := &AppliedFix{
        ID:         uuid.New().String(),
        FixID:      fixID,
        ApprovedBy: "human",
        AppliedAt:  time.Now(),
        Result:     result,
    }

    a.publishApplied(applied)
    return applied, nil
}

func (a *ChangeApplier) RejectFix(fixID string, reason string) error {
    pending := a.pendingApprovals[fixID]
    if pending == nil {
        return fmt.Errorf("fix %s not pending approval", fixID)
    }

    delete(a.pendingApprovals, fixID)

    a.logger.Info("fix rejected", "fix_id", fixID, "reason", reason)
    return nil
}
```

### Phase 6: TUI Integration

**File:** `internal/tui/selfimprove.go` (new)

Create a TUI panel for approval workflow:

```go
package tui

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

type SelfImprovePanel struct {
    pendingFixes []PendingFix
    selectedIdx  int
    width, height int
}

type PendingFix struct {
    ID          string
    Description string
    File        string
    Diff        string
}

func NewSelfImprovePanel() *SelfImprovePanel {
    return &SelfImprovePanel{}
}

func (p *SelfImprovePanel) Update(msg tea.Msg) tea.Cmd {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "a": // Approve
            return p.approveCurrent()
        case "r": // Reject
            return p.rejectCurrent()
        case "j", "down":
            p.selectedIdx = min(p.selectedIdx+1, len(p.pendingFixes)-1)
        case "k", "up":
            p.selectedIdx = max(p.selectedIdx-1, 0)
        }
    }
    return nil
}

func (p *SelfImprovePanel) View() string {
    // Render list of pending fixes with diff preview
    // ...
}
```

### Phase 7: CLI Updates

**File:** `cmd/meept/selfimprove.go`

**Changes:**

1. Wire CLI commands to RPC:
```go
var selfimproveCmd = &cobra.Command{
    Use:   "selfimprove",
    Short: "Self-improvement system",
}

var selfimproveDetectCmd = &cobra.Command{
    Use:   "detect",
    Short: "Detect issues in codebase",
    RunE: func(cmd *cobra.Command, args []string) error {
        client, err := rpc.NewClient(socketPath)
        if err != nil {
            return err
        }
        defer client.Close()

        result, err := client.Call("selfimprove.detect", nil)
        if err != nil {
            return err
        }

        // Display issues
        fmt.Printf("Detected %d issues\n", result["count"])
        return nil
    },
}

var selfimproveRunCmd = &cobra.Command{
    Use:   "run",
    Short: "Run full improvement cycle",
    RunE: func(cmd *cobra.Command, args []string) error {
        interactive, _ := cmd.Flags().GetBool("interactive")

        client, err := rpc.NewClient(socketPath)
        if err != nil {
            return err
        }
        defer client.Close()

        result, err := client.Call("selfimprove.run", map[string]any{
            "interactive": interactive,
        })
        if err != nil {
            return err
        }

        // Display cycle results
        return nil
    },
}
```

---

## Configuration

**File:** `internal/config/schema.go`

Add self-improve config:

```go
type SelfImproveConfig struct {
    Enabled         bool   `toml:"enabled"`
    DataPath        string `toml:"data_path"`
    MaxIterations   int    `toml:"max_iterations"`
    MaxFixes        int    `toml:"max_fixes"`
    RequireApproval bool   `toml:"require_approval"`
    ScheduleInterval string `toml:"schedule_interval"` // e.g., "24h"
}
```

**File:** `~/.meept/meept.toml`

```toml
[selfimprove]
enabled = false
data_path = "~/.meept/selfimprove"
max_iterations = 10
max_fixes = 5
require_approval = true
schedule_interval = "24h"
```

---

## Testing Plan

### Unit Tests

1. **Controller tests** - Cycle execution, circuit breaker
2. **Scheduler tests** - Scheduled runs
3. **Approval tests** - Approve/reject workflow

### Integration Tests

1. Test full cycle with test codebase
2. Test RPC endpoints
3. Test scheduled runs
4. Test approval workflow

### Manual Testing

1. Run `./bin/meept selfimprove detect`
2. Run `./bin/meept selfimprove run --interactive`
3. Approve/reject fixes in TUI
4. Verify fixes are applied correctly

---

## Files to Modify

| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Initialize controller |
| `internal/rpc/proxy.go` | Add RPC handlers |
| `internal/selfimprove/controller.go` | Implement publishStatus |
| `internal/selfimprove/applier.go` | Add approve/reject |
| `internal/config/schema.go` | Add config |
| `cmd/meept/selfimprove.go` | Wire to RPC |

## Files to Create

| File | Purpose |
|------|---------|
| `internal/selfimprove/scheduler.go` | Scheduled cycles |
| `internal/tui/selfimprove.go` | Approval UI panel |
| `tests/integration/selfimprove_test.go` | Integration tests |

---

## Success Criteria

1. Self-improve controller starts with daemon
2. Detection phase runs via CLI/RPC
3. Full cycle can be triggered
4. Human approval workflow works
5. Scheduled cycles run (when enabled)
6. Tests pass
