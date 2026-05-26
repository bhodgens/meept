# Terminal Panel Implementation Gap

## Overview

The Terminal Panel Flutter UI implementation requires backend HTTP API support that does not currently exist.

## Current State

### Backend (Go)
- `ShellExecuteTool` exists in `internal/tools/builtin/shell.go`
- Supports sync and streaming execution via `Execute()` and `ExecuteStreaming()`
- No HTTP endpoint exposed for shell command history or terminal session management
- No WebSocket endpoint for real-time terminal output streaming

### Frontend (Flutter)
- Terminal panel stub exists in `chat_tab.dart` with status "coming soon"
- No `terminal_panel.dart` file created

## Required Backend Implementation

### HTTP Endpoints Needed

```go
// GET /api/v1/terminal/history - Get shell command history
func (s *Server) handleTerminalHistory(w http.ResponseWriter, r *http.Request) {
    // Return recent shell commands with output
}

// POST /api/v1/terminal/exec - Execute shell command (optional, security concerns)
func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
    // Execute command and return result
    // Requires authentication and security scanning
}

// WebSocket /api/v1/terminal/stream - Real-time terminal output streaming
func (s *Server) handleTerminalStream(w http.ResponseWriter, r *http.Request) {
    // Upgrade to WebSocket and stream shell output
}

// GET /api/v1/terminal/sessions - List active terminal sessions
func (s *Server) handleTerminalSessions(w http.ResponseWriter, r *http.Request) {
    // List PTY sessions with metadata
}
```

### Service Layer Needed

```go
// internal/services/terminal_service.go
type TerminalService struct {
    bus    *bus.MessageBus
    logger *slog.Logger
}

type CommandHistory struct {
    ID        string    `json:"id"`
    Command   string    `json:"command"`
    Output    string    `json:"output"`
    ExitCode  int       `json:"exit_code"`
    Timestamp time.Time `json:"timestamp"`
    WorkingDir string   `json:"working_dir"`
}

func (svc *TerminalService) GetHistory(limit int) ([]CommandHistory, error)
func (svc *TerminalService) ExecuteCommand(ctx context.Context, cmd, workDir string) (CommandHistory, error)
func (svc *TerminalService) SubscribeToSession(sessionID string) (<-chan string, error)
```

### Configuration

```json5
// config/meept.json5
{
  terminal: {
    enabled: true,
    max_history: 100,
    session_timeout: 30 * time.Minute,
  }
}
```

## Frontend Implementation (Pending Backend)

Once backend endpoints exist, create `terminal_panel.dart`:

```dart
class TerminalPanel extends ConsumerStatefulWidget {
  const TerminalPanel({super.key});

  @override
  ConsumerState<TerminalPanel> createState() => _TerminalPanelState();
}

class _TerminalPanelState extends ConsumerState<TerminalPanel> {
  List<CommandEntry> _history = [];
  bool _isLoading = false;
  final _commandController = TextEditingController();
  WebSocketService? _wsService;

  Future<void> _loadHistory() async {
    final client = ref.read(apiClientProvider);
    final data = await client.get<List>('/terminal/history');
    setState(() {
      _history = data.map((e) => CommandEntry.fromJson(e)).toList();
    });
  }

  Widget _buildCommandInput() {
    return TextField(
      controller: _commandController,
      decoration: const InputDecoration(
        hintText: 'enter command...',
        border: OutlineInputBorder(),
      ),
      onSubmitted: (cmd) => _executeCommand(cmd),
    );
  }
}
```

## Security Considerations

1. **Authentication required** - Terminal access must require valid session token
2. **Command scanning** - All commands must pass through Tirith security scanner
3. **Rate limiting** - Prevent abuse with execution rate limits
4. **Audit logging** - Log all commands for security review
5. **Working directory restrictions** - Sandbox to project directory
6. **Blocked commands** - Respect existing `blockedCommands` map from shell.go

## Alternative Approaches

### Option A: Read-Only History (Recommended for MVP)
- Only expose command history via HTTP API
- No new command execution from UI
- Minimal security surface area

### Option B: Full Interactive Terminal
- WebSocket-based PTY session
- Real-time streaming output
- Higher security requirements
- More complex backend (PTY management)

### Option C: Memory-Based (Quick Win)
- Query memory for shell command references
- Similar to files panel approach
- No backend changes needed
- Limited to commands seen in conversations

## Recommendation

Implement Option A (read-only history) first:
1. Add `GET /api/v1/terminal/history` endpoint
2. Wire to shell command audit log
3. Create Flutter panel with history list
4. Mark status as "beta" initially
5. Consider Option B only after security review

## Related Files

- Backend: `internal/tools/builtin/shell.go`
- Backend: `internal/comm/http/api_handlers.go`
- Backend: `internal/services/` (create `terminal_service.go`)
- Frontend: `ui/flutter_ui/lib/features/terminal/` (create `terminal_panel.dart`)
- Frontend: `ui/flutter_ui/lib/features/chat/chat_tab.dart` (update status)
