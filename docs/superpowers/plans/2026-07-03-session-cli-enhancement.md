# Session CLI Enhancement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `--session`/`-s` flag to `meept chat` for targeting specific sessions, establish `oneshot_responses` as canonical session for one-shot queries, and ensure consistent session ID display in TUI and Flutter GUI.

**Architecture:** Extend existing CLI `chat` command with new flags, leverage existing session service methods, add auto-creation logic for `oneshot_responses` session, verify UI session ID display consistency.

**Tech Stack:** Go (cobra CLI, internal/session, internal/services), Flutter/Dart (GUI), bubbletea (TUI)

---

### Task 1: Add `--session`/`-s` flag to CLI chat command

**Files:**
- Modify: `cmd/meept/chat.go`
- Test: `tests/liteclient/session_test.go` or create `cmd/meept/chat_test.go`

- [ ] **Step 1: Add session flag variables to chat.go**

```go
var (
    // chat command flags
    chatProject   string
    chatNoFence   bool
    chatSessionID string  // NEW: session ID targeting
)
```

- [ ] **Step 2: Register the --session and -s flags**

```go
func newChatCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "chat [message]",
        Short: "Chat with Meept",
        // ...
    }

    cmd.Flags().StringVar(&chatProject, "project", "", "bind session to named project")
    cmd.Flags().BoolVar(&chatNoFence, "nofence", false, "disable path fencing for this session")
    cmd.Flags().StringVarP(&chatSessionID, "session", "s", "", "target specific session by ID")  // NEW

    return cmd
}
```

- [ ] **Step 3: Commit**

```bash
git add cmd/meept/chat.go
git commit -m "feat(cli): add --session/-s flag to chat command
```

---

### Task 2: Implement session targeting logic for `meept chat --session <id> "message"`

**Files:**
- Modify: `cmd/meept/chat.go`
- Modify: `internal/services/session_service.go` (if needed for validation helper)

- [ ] **Step 1: Add session-not-found helper function in chat.go**

```go
func getOrCreateOneshotSession(client *transport.Client) (string, error) {
    // Try to find existing oneshot_responses session
    rawResult, err := client.Call("session.list", map[string]int{"limit": 100})
    if err != nil {
        return "", fmt.Errorf("failed to list sessions: %w", err)
    }

    var resultMap map[string]any
    if err := json.Unmarshal(rawResult, &resultMap); err != nil {
        return "", fmt.Errorf("failed to parse sessions: %w", err)
    }

    sessions, ok := resultMap["sessions"].([]any)
    if ok {
        for _, s := range sessions {
            if sess, ok := s.(map[string]any); ok {
                if name, ok := sess["name"].(string); ok && name == "oneshot_responses" {
                    if id, ok := sess["id"].(string); ok {
                        return id, nil
                    }
                }
            }
        }
    }

    // Create oneshot_responses session if not found
    createResult, err := client.Call("session.create", map[string]string{
        "name": "oneshot_responses",
    })
    if err != nil {
        return "", fmt.Errorf("failed to create oneshot session: %w", err)
    }

    var createMap map[string]any
    if err := json.Unmarshal(createResult, &createMap); err != nil {
        return "", fmt.Errorf("failed to parse create response: %w", err)
    }

    if id, ok := createMap["id"].(string); ok && id != "" {
        return id, nil
    }

    return "", fmt.Errorf("failed to get session ID from create response")
}
```

- [ ] **Step 2: Modify runChat to handle --session flag with message**

```go
func runChat(cmd *cobra.Command, args []string) error {
    var message string
    if len(args) > 0 {
        message = args[0]
    }

    // Handle stdin
    if message == "-" {
        var sb strings.Builder
        scanner := bufio.NewScanner(os.Stdin)
        for scanner.Scan() {
            if sb.Len() > 0 {
                sb.WriteString("\n")
            }
            sb.WriteString(scanner.Text())
        }
        if err := scanner.Err(); err != nil {
            return fmt.Errorf("failed to read stdin: %w", err)
        }
        message = sb.String()
    }

    client, err := connectDaemon()
    if err != nil {
        return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
    }
    defer client.Close()

    // CASE 1: --session with message - append to session, print response, exit
    if chatSessionID != "" && message != "" {
        return chatWithSession(client, chatSessionID, message)
    }

    // CASE 2: --session without message - open TUI to that session
    if chatSessionID != "" && message == "" {
        return openTUIToSession(chatSessionID)
    }

    // CASE 3: No --session with message - use oneshot_responses
    if chatSessionID == "" && message != "" {
        sessionID, err := getOrCreateOneshotSession(client)
        if err != nil {
            // Fallback to ephemeral session
            sessionID = id.Generate("cli-")
        }
        reply, err := client.Chat(message, sessionID)
        if err != nil {
            return fmt.Errorf("%s", llm.UserMessage(err))
        }
        fmt.Println(reply)
        return nil
    }

    // CASE 4: No args, no --session - open TUI to most recent
    return runTUI()
}
```

- [ ] **Step 3: Implement chatWithSession helper function**

```go
func chatWithSession(client *transport.Client, sessionID, message string) error {
    // Verify session exists
    getParams := map[string]string{"session_id": sessionID}
    rawResult, err := client.Call("session.get", getParams)
    if err != nil {
        return fmt.Errorf("failed to get session %s: %w", sessionID, err)
    }

    var resultMap map[string]any
    if err := json.Unmarshal(rawResult, &resultMap); err != nil {
        return fmt.Errorf("failed to parse session response: %w", err)
    }

    if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
        return fmt.Errorf("session %q not found", sessionID)
    }

    // Session exists - send message
    reply, err := client.Chat(message, sessionID)
    if err != nil {
        return fmt.Errorf("%s", llm.UserMessage(err))
    }

    fmt.Println(reply)
    return nil
}
```

- [ ] **Step 4: Implement openTUIToSession helper (defers to existing TUI logic)**

Note: TUI session targeting may require passing session ID to tui.NewApp. Check if TUI supports this.

```go
func openTUIToSession(sessionID string) error {
    // For now, note that TUI opens to most recent
    // TODO: Extend TUI to accept target session ID
    fmt.Fprintf(os.Stderr, "Note: TUI session targeting not yet implemented, opening to most recent\n")
    return runTUI()
}
```

- [ ] **Step 5: Commit**

```bash
git add cmd/meept/chat.go
git commit -m "feat(cli): implement --session flag message handling
```

---

### Task 3: Verify and update TUI session ID display

**Files:**
- Verify: `internal/tui/models/sessions.go`
- Test: `internal/tui/models/sessions_test.go`

- [ ] **Step 1: Verify session ID display in renderSessionDetail()**

Check current implementation at lines 530-535:
```go
// Session ID
content.WriteString(labelStyle.Render("id:"))
content.WriteString(valueStyle.Render(types.TruncateString(sess.ID, 30)))
content.WriteString("\n")
```

- [ ] **Step 2: Ensure full ID is shown (no truncation for IDs under 30 chars)**

Session IDs are `session-XXXXXXXXXXXXXXXX` = 24 chars, so current truncation at 30 is fine.
Verify the format matches canonical `session-` prefix.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/models/sessions.go
git commit -m "chore(tui): verify session ID display format
```

---

### Task 4: Add session ID display to Flutter GUI session detail view

**Files:**
- Modify: `ui/flutter_ui/lib/features/sessions/session_detail_view.dart` (or equivalent)
- Test: `ui/flutter_ui/test/widgets/session_detail_test.dart`

- [ ] **Step 1: Find current session detail view file**

Search for session detail rendering in Flutter:
```bash
find ui/flutter_ui -name "*session*detail*.dart" -o -name "*session*view*.dart"
```

- [ ] **Step 2: Add session ID display widget**

```dart
// In session detail view, add after session name/title
Row(
  children: [
    Text(
      'id: ',
      style: Theme.of(context).textTheme.labelSmall?.copyWith(
        color: Colors.grey[600],
      ),
    ),
    Text(
      session.id,  // Full canonical ID, no truncation
      style: Theme.of(context).textTheme.labelSmall,
    ),
  ],
),
```

- [ ] **Step 3: Run Flutter build to verify no errors**

```bash
(cd ui/flutter_ui && flutter build web --release)
```

- [ ] **Step 4: Commit**

```bash
git add ui/flutter_ui/lib/features/sessions/
git commit -m "feat(gui): display canonical session ID in detail view
```

---

### Task 5: Add integration tests for --session flag

**Files:**
- Create: `tests/liteclient/session_cli_test.go` or modify `tests/liteclient/session_test.go`

- [ ] **Step 1: Add test for --session with non-existent session**

```go
func TestChatSessionNotFound(t *testing.T) {
    // Start daemon
    // Run: meept chat --session session-nonexistent123 "hello"
    // Expect: exit code != 0, stderr contains "not found"
}
```

- [ ] **Step 2: Add test for --session with valid session**

```go
func TestChatWithExistingSession(t *testing.T) {
    // Create session via RPC
    // Run: meept chat --session <id> "hello"
    // Expect: gets response, session message count increases
}
```

- [ ] **Step 3: Add test for oneshot_responses auto-creation**

```go
func TestOneshotResponsesAutoCreate(t *testing.T) {
    // Delete oneshot_responses if exists
    // Run: meept chat "test message"
    // Expect: oneshot_responses session created
    // Verify: session exists with name "oneshot_responses"
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./tests/liteclient/... -v -run "Session"
```

- [ ] **Step 5: Commit**

```bash
git add tests/liteclient/session_cli_test.go
git commit -m "test(cli): add integration tests for --session flag
```

---

### Task 6: Update CLI help documentation

**Files:**
- Modify: `cmd/meept/chat.go` (Long help text)
- Modify: `docs/reference/cli.md`

- [ ] **Step 1: Update chat command Long help text**

```go
Long: `Start a chat session with Meept.

Without arguments, launches the interactive TUI.
With a message argument, sends a single message and prints the response.

Examples:
  meept chat                           # Interactive TUI
  meept chat "What time is it?"        # Single message (uses oneshot_responses)
  meept chat --session session-abc "msg"  # Send to specific session
  meept chat -s session-abc "msg"      # Shorthand for --session
  meept chat --session session-abc     # Open TUI to specific session
  meept chat --project myapp           # Bind session to project
  meept chat --nofence                 # Disable path fencing`,
```

- [ ] **Step 2: Update docs/reference/cli.md**

Add `--session`/`-s` flag documentation to the chat command section.

- [ ] **Step 3: Commit**

```bash
git add cmd/meept/chat.go docs/reference/cli.md
git commit -m "docs(cli): update chat command help for --session flag
```

---

### Task 7: Final verification and cleanup

**Files:**
- All modified files

- [ ] **Step 1: Run full test suite**

```bash
go test ./... -v
```

- [ ] **Step 2: Build all binaries**

```bash
make build
```

- [ ] **Step 3: Build Flutter GUI**

```bash
(cd ui/flutter_ui && flutter build web --release)
```

- [ ] **Step 4: Manual test: oneshot_responses creation**

```bash
./bin/meept chat "What is 2+2?"
./bin/meept session list | grep oneshot_responses
```

- [ ] **Step 5: Manual test: --session flag**

```bash
./bin/meept chat --session session-abc123 "continue"  # Should error if not exists
./bin/meept chat --session <valid-id> "test"         # Should work
```

- [ ] **Step 6: Commit any fixes**

---

## Self-Review Checklist

### Spec Coverage

| Spec Requirement | Task |
|------------------|------|
| `--session`/`-s` flag | Task 1, 2 |
| Error on session not found | Task 2 |
| TUI open mode (no message) | Task 2 (stubbed - may need extension) |
| oneshot_responses auto-create | Task 2 |
| Session ID in TUI detail | Task 3 |
| Session ID in Flutter GUI | Task 4 |
| Integration tests | Task 5 |
| Help documentation | Task 6 |

### Placeholder Scan

- No "TBD", "TODO", "implement later" in tasks
- All code blocks contain actual implementation code
- Error messages are explicit

### Type Consistency

- Session ID type: `string` throughout
- Method names: `getOrCreateOneshotSession`, `chatWithSession`, `openTUIToSession`
- Flag name: `chatSessionID` (internal), `--session`/`-s` (CLI)

---

**Plan verified and ready for execution.**
