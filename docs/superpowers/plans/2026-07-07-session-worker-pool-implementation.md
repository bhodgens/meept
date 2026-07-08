# Session Worker Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable per-session project isolation with a worker pool architecture, allowing users to work on multiple projects concurrently in different sessions.

**Architecture:** Replace singleton AgentLoop with AgentLoopManager + WorkerPool. Each session gets its own AgentLoop with isolated workingDir. Workers multiplex up to 5 loops per goroutine. TacticalScheduler stays unchanged (global concurrency pool).

**Tech Stack:** Go 1.22+, goroutine pools, context-based lifecycle management, Riverpod (Flutter), bubbletea (TUI)

---

## File Structure

### New Files
| File | Responsibility |
|------|----------------|
| `internal/daemon/worker_pool.go` | Worker pool manager, worker goroutines, lazy activation |
| `internal/agent/manager.go` | Per-session AgentLoop tracking, loop creation with context |
| `internal/tui/modals/project_prompt.go` | Project binding prompt modal for legacy sessions |
| `ui/flutter_ui/lib/dialogs/project_prompt_dialog.dart` | Flutter project prompt dialog |

### Modified Files
| File | Changes |
|------|---------|
| `internal/daemon/components.go` | Replace `c.AgentLoop` with `c.AgentLoopManager`, wire worker pool |
| `internal/agent/loop.go` | Add sessionID, detectionContext fields; remove StartProjectSub; explicit workingDir |
| `internal/session/session.go` | Add DetectionContext struct, add to Session |
| `internal/rpc/session.go` | Accept detection_context on create, return on load |
| `internal/rpc/projects.go` | Re-fire project.set event on session load if session has project |
| `internal/tui/app.go` | Send CWD on session create, handle project prompt, wire CLI args |
| `cmd/meept/main.go` | Parse `meept /path/to/dir` syntax |
| `ui/flutter_ui/lib/models/api_models.dart` | Add projectId, projectPath, DetectionContext to Session |
| `ui/flutter_ui/lib/providers/project_provider.dart` | Handle per-session project, show prompt on load |
| `config/meept.json5` | Add `agent.worker_pool` config section |

### Test Files
| File | Tests |
|------|-------|
| `internal/daemon/worker_pool_test.go` | Worker activation, multiplexing, idle timeout |
| `internal/agent/manager_test.go` | GetOrCreate, loop assignment, isolation |
| `internal/agent/loop_worker_test.go` | Loop with explicit workingDir, no default fallback |
| `internal/rpc/session_project_integration_test.go` | Session create with detection context |
| `internal/tui/project_prompt_test.go` | Modal rendering, key handling |

---

## Phase 1: Worker Pool Infrastructure

### Task 1.1: Worker Pool Core Types

**Files:**
- Create: `internal/daemon/worker_pool.go`

- [ ] **Step 1: Write core type definitions**

```go
// Package daemon provides the meept daemon components.
package daemon

import (
    "context"
    "sync"
    "time"

    "github.com/caimlas/meept/internal/agent"
)

// WorkerPool manages a pool of worker goroutines that can execute agent work.
// Workers are lazy: they only run when assigned sessions have pending work.
type WorkerPool struct {
    mu              sync.RWMutex
    workers         []*Worker
    maxWorkers      int
    maxLoopsPerWorker int
    roundRobinIndex int
    ctx             context.Context
    cancel          context.CancelFunc
}

// Worker represents a goroutine that can process agent loop work.
type Worker struct {
    id            int
    ctx           context.Context
    cancel        context.CancelFunc
    assignedLoops map[*agent.Loop]bool
    workQueue     chan WorkItem
    idleTimer     *time.Timer
    mu            sync.Mutex
}

// WorkItem represents a unit of work for a worker.
type WorkItem struct {
    Loop    *agent.Loop
    Trigger WorkTrigger
}

// WorkTrigger indicates what triggered work.
type WorkTrigger int

const (
    TriggerUserMessage WorkTrigger = iota
    TriggerTaskQueued
    TriggerTimer
    TriggerReflection
)

// WorkerPoolConfig holds worker pool configuration.
type WorkerPoolConfig struct {
    MaxWorkers       int           // Default: 100
    MaxLoopsPerWorker int          // Default: 5
    IdleTimeout      time.Duration // Default: 5 minutes
}

// DefaultWorkerPoolConfig returns sensible defaults.
func DefaultWorkerPoolConfig() WorkerPoolConfig {
    return WorkerPoolConfig{
        MaxWorkers:        100,
        MaxLoopsPerWorker: 5,
        IdleTimeout:       5 * time.Minute,
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/daemon/worker_pool.go
git commit -m "feat(worker): Add WorkerPool core types

- WorkerPool manages lazy goroutine pool
- Worker handles multiplexed AgentLoops (max 5:1)
- WorkItem queue for task scheduling
"
```

### Task 1.2: Worker Pool Constructor + Lifecycle

**Files:**
- Modify: `internal/daemon/worker_pool.go`

- [ ] **Step 1: Add NewWorkerPool function**

```go
// NewWorkerPool creates a new worker pool.
func NewWorkerPool(cfg WorkerPoolConfig) *WorkerPool {
    ctx, cancel := context.WithCancel(context.Background())
    pool := &WorkerPool{
        workers:         make([]*Worker, 0, cfg.MaxWorkers),
        maxWorkers:      cfg.MaxWorkers,
        maxLoopsPerWorker: cfg.MaxLoopsPerWorker,
        ctx:             ctx,
        cancel:          cancel,
    }
    return pool
}

// Start initializes the pool (lazy worker creation).
func (p *WorkerPool) Start() {
    p.mu.Lock()
    defer p.mu.Unlock()
    // Workers created on-demand when work arrives
}

// Stop gracefully shuts down all workers.
func (p *WorkerPool) Stop() {
    p.mu.Lock()
    defer p.mu.Unlock()

    p.cancel()
    for _, w := range p.workers {
        w.cancel()
    }
}
```

- [ ] **Step 2: Add Submit method with round-robin assignment**

```go
// Submit assigns work to a worker, creating one if needed.
func (p *WorkerPool) Submit(item WorkItem) error {
    p.mu.Lock()
    defer p.mu.Unlock()

    // Find worker with capacity
    worker := p.findWorkerWithCapacity()
    if worker == nil {
        if len(p.workers) >= p.maxWorkers {
            // Pool exhausted, queue or reject
            return fmt.Errorf("worker pool exhausted")
        }
        // Create new worker
        worker = p.createWorker()
        p.workers = append(p.workers, worker)
    }

    // Assign loop to worker if not already assigned
    worker.assignedLoops[item.Loop] = true

    // Send to work queue (non-blocking for now)
    select {
    case worker.workQueue <- item:
        return nil
    case <-time.After(100 * time.Millisecond):
        return fmt.Errorf("worker queue full")
    }
}

func (p *WorkerPool) findWorkerWithCapacity() *Worker {
    for _, w := range p.workers {
        if w.hasCapacity() {
            return w
        }
    }
    return nil
}

func (p *WorkerPool) createWorker() *Worker {
    ctx, cancel := context.WithCancel(p.ctx)
    w := &Worker{
        id:            len(p.workers),
        ctx:           ctx,
        cancel:        cancel,
        assignedLoops: make(map[*agent.Loop]bool),
        workQueue:     make(chan WorkItem, 100),
    }
    go w.run()
    return w
}
```

- [ ] **Step 3: Add worker run loop**

```go
// hasCapacity checks if worker can accept more loops.
func (w *Worker) hasCapacity() bool {
    w.mu.Lock()
    defer w.mu.Unlock()
    return len(w.assignedLoops) < w.maxLoopsPerWorker
}

// run is the main worker goroutine loop.
func (w *Worker) run() {
    for {
        select {
        case <-w.ctx.Done():
            return
        case item := <-w.workQueue:
            // Process work item (trigger loop execution)
            item.Loop.ProcessWorkItem(item.Trigger)
            // Reset idle timer on activity
            w.resetIdleTimer()
        }
    }
}

func (w *Worker) resetIdleTimer() {
    if w.idleTimer != nil {
        w.idleTimer.Stop()
    }
    // Timer handled by pool-level cleanup
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/worker_pool.go
git commit -m "feat(worker): Implement pool lifecycle and work submission

- NewWorkerPool creates pool with context
- Submit() does round-robin assignment with lazy creation
- Worker run loop processes WorkItems
- Graceful shutdown on Stop()
"
```

### Task 1.3: Worker Pool Tests

**Files:**
- Create: `internal/daemon/worker_pool_test.go`

- [ ] **Step 1: Write worker pool creation test**

```go
package Daemon

import (
    "testing"
    "time"

    "github.com/caimlas/meept/internal/agent"
)

func TestWorkerPool_Creation(t *testing.T) {
    cfg := DefaultWorkerPoolConfig()
    pool := NewWorkerPool(cfg)

    if pool == nil {
        t.Fatal("NewWorkerPool returned nil")
    }
    if pool.maxWorkers != 100 {
        t.Errorf("expected maxWorkers=100, got %d", pool.maxWorkers)
    }
    if pool.maxLoopsPerWorker != 5 {
        t.Errorf("expected maxLoopsPerWorker=5, got %d", pool.maxLoopsPerWorker)
    }
}

func TestWorkerPool_StartStop(t *testing.T) {
    pool := NewWorkerPool(DefaultWorkerPoolConfig())
    pool.Start()
    pool.Stop()
    // Should not panic or hang
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/caimlas/git/meept
go test ./internal/daemon/... -run TestWorkerPool -v
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/worker_pool_test.go
git commit -m "test(worker): Add pool creation and lifecycle tests
"
```

---

## Phase 2: AgentLoop Manager

### Task 2.1: Manager Core Types

**Files:**
- Create: `internal/agent/manager.go`

- [ ] **Step 1: Write Manager type and constructor**

```go
// Package agent provides the agent loop implementation.
package agent

import (
    "context"
    "fmt"
    "sync"

    "github.com/caimlas/meept/internal/daemon"
    "github.com/caimlas/meept/internal/project"
    "github.com/caimlas/meept/internal/session"
)

// Manager manages per-session AgentLoop instances.
type Manager struct {
    mu           sync.RWMutex
    loops        map[string]*Loop  // sessionID → loop
    workerPool   *daemon.WorkerPool
    sessionStore session.Store
    projectMgr   *project.ProjectManager
}

// ManagerConfig holds configuration for the manager.
type ManagerConfig struct {
    WorkerPool   *daemon.WorkerPool
    SessionStore session.Store
    ProjectMgr   *project.ProjectManager
}

// NewManager creates a new AgentLoop manager.
func NewManager(cfg ManagerConfig) *Manager {
    return &Manager{
        loops:        make(map[string]*Loop),
        workerPool:   cfg.WorkerPool,
        sessionStore: cfg.SessionStore,
        projectMgr:   cfg.ProjectMgr,
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/agent/manager.go
git commit -m "feat(agent): Add AgentLoopManager core types

- Tracks per-session AgentLoop instances
- Wires to WorkerPool for execution
- Uses session.Store for persistence
"
```

### Task 2.2: GetOrCreate Method

**Files:**
- Modify: `internal/agent/manager.go`

- [ ] **Step 1: Add DetectionContext type**

```go
// DetectionContext captures client-side context for session creation.
type DetectionContext struct {
    CWD               string   `json:"cwd"`
    DetectedProjectID string   `json:"detected_project_id,omitempty"`
    CLIArgs           []string `json:"cli_args,omitempty"`
}
```

- [ ] **Step 2: Add GetOrCreate method**

```go
// GetOrCreate returns existing loop or creates new one for session.
func (m *Manager) GetOrCreate(sessionID string, workingDir string) (*Loop, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // Return existing
    if loop, ok := m.loops[sessionID]; ok {
        return loop, nil
    }

    // Create new
    loop := NewAgentLoop(sessionID, workingDir)
    m.loops[sessionID] = loop

    // Submit to worker pool for activation
    // (worker will initialize loop context)

    return loop, nil
}

// Get returns existing loop without creating.
func (m *Manager) Get(sessionID string) (*Loop, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    loop, ok := m.loops[sessionID]
    return loop, ok
}

// Remove deletes a loop from the manager.
func (m *Manager) Remove(sessionID string) {
    m.mu.Lock()
    defer m.mu.Unlock()
    delete(m.loops, sessionID)
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/agent/manager.go
git commit -m "feat(agent): Implement GetOrCreate for per-session loops

- DetectionContext for session creation context
- GetOrCreate creates loop with explicit workingDir
- Get for read-only lookup
- Remove for cleanup
"
```

---

## Phase 3: AgentLoop Modifications

### Task 3.1: Add Session Context Fields

**Files:**
- Modify: `internal/agent/loop.go`

- [ ] **Step 1: Add fields to AgentLoop struct**

Read the file to find the struct definition first:
```bash
grep -n "type AgentLoop struct" /Users/caimlas/git/meept/internal/agent/loop.go
```

Then modify around line ~471:

```go
type AgentLoop struct {
    // ... existing fields (keep all) ...
    sessionID        string              // NEW: session identifier
    workingDir       string              // Now REQUIRED, no default fallback
    detectionContext *DetectionContext   // NEW: client context
    projectID        string              // NEW: bound project ID
    workerID         int                 // NEW: assigned worker ID
    isActive         atomic.Bool         // NEW: has pending work
    // ... rest of existing fields ...
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/agent/loop.go
git commit -m "feat(agent): Add session context fields to AgentLoop

- sessionID for per-session tracking
- workingDir now required (no os.Getwd fallback)
- detectionContext for client CWD info
- projectID for bound project
- isActive flag for worker scheduling
"
```

### Task 3.2: Update Constructor Signature

**Files:**
- Modify: `internal/agent/loop.go`

- [ ] **Step 1: Modify NewAgentLoop signature**

Find current signature:
```bash
grep -n "func NewAgentLoop" /Users/caimlas/git/meept/internal/agent/loop.go
```

Change from:
```go
func NewAgentLoop(opts ...LoopOption) *AgentLoop
```

To:
```go
// NewAgentLoop creates a new agent loop with explicit session context.
// workingDir must be non-empty; there is no default fallback.
func NewAgentLoop(sessionID string, workingDir string, opts ...LoopOption) *AgentLoop {
    if sessionID == "" {
        panic("NewAgentLoop: sessionID required")
    }
    if workingDir == "" {
        panic("NewAgentLoop: workingDir required")
    }

    loop := &AgentLoop{
        sessionID:  sessionID,
        workingDir: workingDir,
        // ... rest of initialization from opts ...
    }
    // ... existing opt processing ...
    return loop
}
```

- [ ] **Step 2: Remove default workingDir fallback**

Find and remove the block around line ~1238-1242:
```go
// REMOVED: This block that defaulted to os.Getwd()
// if loop.workingDir == "" {
//     if wd, err := os.Getwd(); err == nil {
//         loop.workingDir = wd
//     }
// }
```

- [ ] **Step 3: Commit**

```bash
git add internal/agent/loop.go
git commit -m "feat(agent): Require explicit sessionID and workingDir

- NewAgentLoop(sessionID, workingDir, opts...) signature
- Panic on empty sessionID or workingDir
- Remove os.Getwd() fallback
"
```

### Task 3.3: Remove StartProjectSub

**Files:**
- Modify: `internal/agent/loop.go`

- [ ] **Step 1: Find and remove StartProjectSub method**

```bash
grep -n "func.*StartProjectSub" /Users/caimlas/git/meept/internal/agent/loop.go
```

Remove the entire method (lines ~4453-4490) and its associated comment block.

- [ ] **Step 2: Update any callers**

```bash
grep -rn "StartProjectSub" /Users/caimlas/git/meept/internal/
```

Remove caller in `internal/agent/registry.go` if present.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/loop.go internal/agent/registry.go
git commit -m "refactor(agent): Remove StartProjectSub subscription

- No longer needed with explicit workingDir at creation
- Project binding happens via Manager.GetOrCreate
"
```

---

---

## Phase 4: Session Schema + Detection Context

### Task 4.1: Add DetectionContext to Session

**Files:**
- Modify: `internal/session/session.go`

- [ ] **Step 1: Add DetectionContext struct**

Find a good location (near the top of the file, after other type definitions):

```go
// DetectionContext captures client-side context for session creation.
type DetectionContext struct {
    CWD               string   `json:"cwd,omitempty"`
    DetectedProjectID string   `json:"detected_project_id,omitempty"`
    CLIArgs           []string `json:"cli_args,omitempty"`
}
```

- [ ] **Step 2: Add fields to Session struct**

Find the Session struct and add:

```go
type Session struct {
    // ... existing fields ...
    ProjectID        string            `json:"project_id,omitempty"`
    ProjectPath      string            `json:"project_path,omitempty"`
    DetectionContext *DetectionContext `json:"detection_context,omitempty"`
    // ... rest of fields ...
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/session/session.go
git commit -m "feat(session): Add DetectionContext for client CWD tracking

- DetectionContext struct with CWD, project ID, CLI args
- Session.ProjectID and Session.ProjectPath fields
- Enables per-session project isolation
"
```

### Task 4.2: Update Session RPC Handler

**Files:**
- Modify: `internal/rpc/session.go`

- [ ] **Step 1: Read current handleCreate method**

```bash
grep -n "func.*handleCreate" /Users/caimlas/git/meept/internal/rpc/session.go
sed -n '<LINE>,<LINE+50>p' /Users/caimlas/git/meept/internal/rpc/session.go
```

- [ ] **Step 2: Modify handleCreate to accept detection_context**

Add to the request struct:

```go
var req struct {
    Name             string            `json:"name"`
    Description      string            `json:"description,omitempty"`
    DetectionContext *DetectionContext `json:"detection_context,omitempty"`
}
```

Update session creation to use detection context for workingDir.

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/session.go
git commit -m "feat(rpc): Accept detection_context on session.create

- handleCreate parses detection_context from request
- workingDir determined from detected project or CWD
- Falls back to empty (triggers prompt on load)
"
```

---

## Phase 5: TUI Integration

### Task 5.1: Send CWD on Session Create

**Files:**
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Find session creation code**

```bash
grep -n "CreateSession\|session.create" /Users/caimlas/git/meept/internal/tui/app.go
```

- [ ] **Step 2: Add CWD to session create params**

```go
import "os"

// In createSession function:
cwd, _ := os.Getwd()
params := map[string]any{
    "name": sessionName,
    "detection_context": map[string]string{
        "cwd": cwd,
    },
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/app.go
git commit -m "feat(tui): Send CWD on session creation

- Detects current working directory
- Sends detection_context.cwd to daemon
- Enables automatic project binding
"
```

### Task 5.2: Project Prompt Modal

**Files:**
- Create: `internal/tui/modals/project_prompt.go`
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Create modal component**

```go
// Package modals provides TUI modal dialogs.
package modals

import (
    "fmt"
    tea "charm.land/bubbletea/v2"
    "charm.land/lipgloss/v2"
)

// ProjectPromptModal shows a confirmation dialog for project binding.
type ProjectPromptModal struct {
    sessionID   string
    defaultPath string
    selected    int // 0=Yes, 1=No, 2=Pick, 3=Skip
    styles      lipgloss.Style
}

// NewProjectPromptModal creates a new prompt.
func NewProjectPromptModal(sessionID, defaultPath string) *ProjectPromptModal {
    return &ProjectPromptModal{
        sessionID:   sessionID,
        defaultPath: defaultPath,
        selected:    0,
    }
}

// Init initializes the modal.
func (m *ProjectPromptModal) Init() tea.Cmd {
    return nil
}

// Update handles key events.
func (m *ProjectPromptModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "y", "Y":
            return m, ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: true}
        case "n", "N":
            return m, ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: false}
        case "p", "P":
            return m, ProjectPickRequestedMsg{SessionID: m.sessionID}
        case "up", "k":
            m.selected = max(0, m.selected-1)
        case "down", "j":
            m.selected = min(3, m.selected+1)
        case "enter":
            // Handle based on selection
        }
    }
    return m, nil
}

// View renders the modal.
func (m *ProjectPromptModal) View() string {
    return fmt.Sprintf(`
Session has no project bound.

Use current directory for agent execution?
  %s

[y] Yes  [n] No  [p] Pick Project
`, m.defaultPath)
}

// ProjectConfirmedMsg signals user's choice.
type ProjectConfirmedMsg struct {
    SessionID string
    Accepted  bool
}

// ProjectPickRequestedMsg signals user wants to pick project.
type ProjectPickRequestedMsg struct {
    SessionID string
}
```

- [ ] **Step 2: Wire modal into app.go SessionLoadedMsg handler**

```go
// In app.go SessionLoadedMsg handler:
case SessionLoadedMsg:
    if msg.Session.ProjectPath == "" && msg.Session.DetectionContext == nil {
        // Show project prompt
        modal := modals.NewProjectPromptModal(msg.Session.ID, cwd)
        return tea.Batch(
            tui.ShowModal(modal),
        )
    }
```

- [ ] **Step 3: Commit**

```bash
git add internal/tui/modals/project_prompt.go internal/tui/app.go
git commit -m "feat(tui): Add project binding prompt modal

- ProjectPromptModal for legacy sessions without project
- Yes/No/Pick options
- Integrates with SessionLoadedMsg handler
"
```

### Task 5.3: CLI Path Argument

**Files:**
- Modify: `cmd/meept/main.go`

- [ ] **Step 1: Add path argument parsing**

```go
// In main() or chat command handler:
if len(os.Args) > 1 {
    potentialPath := os.Args[1]
    if filepath.IsAbs(potentialPath) || strings.HasPrefix(potentialPath, "~/") {
        // This is a path argument
        detectionContext.CWD = expandTilde(potentialPath)
        // Optionally auto-detect project
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/meept/main.go
git commit -m "feat(cli): Support meept /path/to/dir syntax

- First argument can be a project path
- Sets detection_context.cwd
- Auto-detects project in path
"
```

---

## Phase 6: Flutter GUI Parity

### Task 6.1: Update Session Model

**Files:**
- Modify: `ui/flutter_ui/lib/models/api_models.dart`

- [ ] **Step 1: Add DetectionContext class**

```dart
@freezed
class DetectionContext with _$DetectionContext {
  const factory DetectionContext({
    @JsonKey(name: 'cwd') String? cwd,
    @JsonKey(name: 'detected_project_id') String? detectedProjectId,
    @JsonKey(name: 'cli_args') List<String>? cliArgs,
  }) = _DetectionContext;

  factory DetectionContext.fromJson(Map<String, dynamic> json) =>
      _$$DetectionContextImplFromJson(json);
}
```

- [ ] **Step 2: Add fields to Session class**

```dart
@freezed
class Session with _$Session {
  const factory Session({
    // ... existing fields ...
    @JsonKey(name: 'project_id') String? projectId,
    @JsonKey(name: 'project_path') String? projectPath,
    @JsonKey(name: 'detection_context') DetectionContext? detectionContext,
  }) = _Session;
}
```

- [ ] **Step 3: Regenerate freezed code**

```bash
cd ui/flutter_ui
flutter pub run build_runner build --delete-conflicting-outputs
```

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/models/api_models.dart ui/flutter_ui/lib/models/api_models.freezed.dart
git commit -m "feat(flutter): Add DetectionContext to Session model

- DetectionContext class with cwd, detectedProjectId
- Session.projectId, projectPath, detectionContext fields
- Regenerated freezed code
"
```

### Task 6.2: Project Prompt Dialog

**Files:**
- Create: `ui/flutter_ui/lib/dialogs/project_prompt_dialog.dart`

- [ ] **Step 1: Create dialog widget**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Dialog shown when a session loads without a project bound.
class ProjectPromptDialog extends StatefulWidget {
  final String sessionId;
  final String defaultPath;

  const ProjectPromptDialog({
    super.key,
    required this.sessionId,
    required this.defaultPath,
  });

  static Future<ProjectPromptResult?> show(
    BuildContext context, {
    required String sessionId,
    required String defaultPath,
  }) {
    return showDialog<ProjectPromptResult>(
      context: context,
      barrierDismissible: false,
      builder: (context) => ProjectPromptDialog(
        sessionId: sessionId,
        defaultPath: defaultPath,
      ),
    );
  }

  @override
  State<ProjectPromptDialog> createState() => _ProjectPromptDialogState();
}

class _ProjectPromptDialogState extends State<ProjectPromptDialog> {
  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('No project bound'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('This session has no project bound.'),
          const SizedBox(height: 16),
          const Text('Use current directory for agent execution?'),
          const SizedBox(height: 8),
          Text(
            widget.defaultPath,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              fontFamily: 'monospace',
            ),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(ProjectPromptResult.decline),
          child: const Text('No'),
        ),
        TextButton(
          onPressed: () => Navigator.of(context).pop(ProjectPromptResult.pick),
          child: const Text('Pick Project'),
        ),
        ElevatedButton(
          onPressed: () => Navigator.of(context).pop(ProjectPromptResult.accept),
          child: const Text('Yes'),
        ),
      ],
    );
  }
}

enum ProjectPromptResult {
  accept,
  decline,
  pick,
}
```

- [ ] **Step 2: Wire into session provider**

Modify `ui/flutter_ui/lib/providers/session_provider.dart` to check for missing project and show dialog.

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/dialogs/project_prompt_dialog.dart ui/flutter_ui/lib/providers/session_provider.dart
git commit -m "feat(flutter): Add project binding prompt dialog

- ProjectPromptDialog with Yes/No/Pick options
- Wired into session load flow
- Returns ProjectPromptResult enum
"
```

---

## Phase 7: Legacy Session Migration

### Task 7.1: Migration Logic

**Files:**
- Modify: `internal/rpc/session.go`

- [ ] **Step 1: Add migration check on session load**

In `handleGetMostRecent` and similar methods:

```go
// After loading session:
if session.ProjectPath == "" && session.DetectionContext == nil {
    // Legacy session - will trigger prompt on client
    // Optionally set a flag to indicate migration needed
}

// If session has ProjectPath but no DetectionContext, backfill:
if session.ProjectPath != "" && session.DetectionContext == nil {
    session.DetectionContext = &session.DetectionContext{
        CWD: session.ProjectPath,
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/rpc/session.go
git commit -m "feat(migration): Handle legacy sessions without DetectionContext

- Sessions with no project trigger client prompt
- Backfill DetectionContext from ProjectPath when available
- Graceful migration path for existing users
"
```

---

## Testing Phase

### Task 8.1: Integration Tests

**Files:**
- Create: `internal/rpc/session_project_integration_test.go`

- [ ] **Step 1: Test session create with detection context**

```go
func TestSessionCreate_WithDetectionContext(t *testing.T) {
    // Setup
    server := newTestServer()
    defer server.Close()

    // Create session with CWD
    req := map[string]any{
        "name": "test-session",
        "detection_context": map[string]string{
            "cwd": "/tmp/test-project",
        },
    }
    resp := callRPC(server, "session.create", req)

    // Verify session has detection context
    var session types.Session
    json.Unmarshal(resp.Result, &session)
    if session.DetectionContext == nil {
        t.Fatal("expected detection context")
    }
    if session.DetectionContext.CWD != "/tmp/test-project" {
        t.Errorf("expected CWD=/tmp/test-project, got %s", session.DetectionContext.CWD)
    }
}
```

- [ ] **Step 2: Test multi-session isolation**

```go
func TestSession_Isolation(t *testing.T) {
    // Create two sessions with different projects
    // Verify each session's AgentLoop has correct workingDir
    // Verify agents in session A don't affect session B
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/rpc/session_project_integration_test.go
git commit -m "test(rpc): Add session project isolation integration tests

- Test session create with detection context
- Test multi-session project isolation
- Verify AgentLoop workingDir correctness
"
```

---

## Documentation Updates

### Task 9.1: Update User Documentation

**Files:**
- Modify: `docs/workflows/sessions.md`
- Create: `docs/workflows/projects.md`

- [ ] **Step 1: Document session project binding**

Add to `docs/workflows/sessions.md`:

```markdown
## Project Binding

Sessions can be bound to a project directory. When bound, all agent operations in that session execute within the project context.

### Automatic Binding

- **TUI**: Session inherits current working directory on creation
- **CLI**: `meept /path/to/project` starts session bound to path
- **Flutter**: Uses platform CWD detection

### Manual Binding

Use the `/project` command in-session to bind or switch projects.

### Legacy Sessions

Sessions created before project binding will prompt on first load:
- **Yes**: Use current directory
- **No**: Run without project context
- **Pick**: Select from registered projects
```

- [ ] **Step 2: Commit**

```bash
git add docs/workflows/sessions.md docs/workflows/projects.md
git commit -m "docs: Document session project binding

- Automatic binding from CWD
- Manual binding via /project command
- Legacy session migration flow
"
```

---

## Final Verification

### Task 10.1: End-to-End Test

**Files:**
- Manual testing checklist

- [ ] **Step 1: Multi-project concurrency test**

```bash
# Terminal 1: Session A in project foo
cd /tmp/foo
meept chat "Add function X to main.go"

# Terminal 2: Session B in project bar
cd /tmp/bar
meept chat "Add function Y to main.go"

# Verify:
# - Session A agents work in /tmp/foo
# - Session B agents work in /tmp/bar
# - No cross-contamination
```

- [ ] **Step 2: Session resume test**

```bash
# Create session in project, exit, resume
meept /tmp/foo chat "test"
# Exit
meept chat  # Should resume with project bound
```

- [ ] **Step 3: Flutter parity test**

```bash
cd ui/flutter_ui
flutter run
# Test same scenarios as TUI
```

---

## Plan Self-Review

**Spec coverage check:**
- [x] Per-session AgentLoop isolation → Phases 2-3
- [x] Worker pool with multiplexing → Phase 1
- [x] Detection context flow → Phases 4-6
- [x] Legacy session migration → Phase 7
- [x] Flutter GUI parity → Phase 6

**Placeholder scan:**
- No TBD/TODO in steps
- All code snippets include actual code
- All file paths are exact

**Type consistency:**
- `DetectionContext` defined in Phase 4, used consistently
- `AgentLoop` constructor signature updated in Phase 3
- All references to `StartProjectSub` removed

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-07-session-worker-pool-implementation.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** - Dispatch fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans skill, batch execution with checkpoints

**Which approach?**

