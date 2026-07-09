# Session Title Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix session title generation to produce concise LLM-summarized titles instead of echoing user messages, and add dynamic title updates as sessions progress.

**Architecture:** Three-layer fix: (1) Flutter display logic to prefer LLM-generated names, (2) improved fallback extractor for when LLM fails, (3) mid-session title refresh every N turns.

**Tech Stack:** Go (backend summarizer, RPC), Dart/Flutter (GUI display), RPC bus/call protocol

---

## Phase 1: Fix LLM Title Generation (Tasks 1-3)

### Task 1: Fix Flutter Session Normalizer

**Files:**
- Modify: `ui/flutter_ui/lib/models/api_models.dart:477-488`

- [ ] **Step 1: Update `_normaliseSessionJson` to prefer LLM-generated name**

Replace the current logic that picks description when it's longer:

```dart
static Map<String, dynamic> _normaliseSessionJson(Map<String, dynamic> json) {
  final name =
      json['name'] as String? ?? json['title'] as String? ?? 'Untitled';
  final description = json['description'] as String?;

  // Prefer name (LLM summary) unless it's generic, then fall back to description
  final displayTitle = (name != 'default' &&
                        name != 'Untitled' &&
                        name.isNotEmpty &&
                        name != 'chat')
      ? name  // Use LLM-generated name
      : (description ?? name);  // Fall back to description only if name is generic

  return {...json, 'name': displayTitle};
}
```

- [ ] **Step 2: Run Flutter analyzer**

```bash
cd ui/flutter_ui && flutter analyze
```

Expected: No errors

- [ ] **Step 3: Run session tests**

```bash
cd ui/flutter_ui && flutter test test/models/api_models_test.dart
```

Expected: PASS

---

### Task 2: Improve Backend Fallback Extractor

**Files:**
- Modify: `internal/session/summarizer.go:238-271`

- [ ] **Step 1: Update `extractSimpleResult` to produce better names**

Replace the function with improved logic:

```go
// extractSimpleResult extracts the first few significant words as a fallback.
func extractSimpleResult(text string) *SummarizeResult {
    words := strings.Fields(text)

    // Skip common filler words at the start
    fillers := map[string]bool{
        "what": true, "is": true, "are": true, "the": true, "a": true,
        "an": true, "how": true, "do": true, "does": true, "can": true,
        "could": true, "would": true, "should": true, "i": true, "my": true,
        "your": true, "we": true, "our": true, "they": true, "their": true,
    }

    // Name: Find first non-filler word and use 1-2 words
    name := "chat"
    for i, word := range words {
        cleanWord := strings.ToLower(strings.Trim(word, ".,!?;:"))
        if !fillers[cleanWord] && len(cleanWord) >= 3 {
            // Use this word and possibly the next
            if i+1 < len(words) && len(words[i+1]) >= 3 {
                name = strings.ToLower(cleanWord + " " + strings.Trim(words[i+1], ".,!?;:"))
            } else {
                name = cleanWord
            }
            break
        }
    }

    // If all words were fillers, use first word
    if name == "chat" && len(words) > 0 {
        name = strings.ToLower(strings.Trim(words[0], ".,!?;:"))
    }

    // Description: First 6-8 words (shorter = more title-like)
    maxDescWords := min(len(words), 8)
    desc := "new conversation"
    if maxDescWords > 0 {
        desc = strings.Join(words[:maxDescWords], " ")
        if len(words) > maxDescWords {
            desc += "..."
        }
        desc = strings.ToLower(desc)
    }

    return &SummarizeResult{
        Name:        name,
        Description: desc,
    }
}
```

- [ ] **Step 2: Run Go tests**

```bash
go test ./internal/session/... -v -run TestExtractSimpleResult
```

Expected: PASS

- [ ] **Step 3: Add unit tests for the improved extractor**

Create test cases:
- "what are your capabilities" → name="capabilities", description="what are your capabilities"
- "explore the code for the button" → name="explore code", description="explore the code for the button..."
- "how do I fix the null pointer" → name="fix null", description="how do I fix the null pointer..."

---

### Task 3: Add LLM Summarizer Debug Logging

**Files:**
- Modify: `internal/session/summarizer.go:45-133`
- Modify: `internal/session/session.go:1457-1559`

- [ ] **Step 1: Add detailed logging in `GenerateDescription`**

Add log lines to capture:
- LLM client availability check
- Request payload sent
- Raw response received
- JSON parse result/failure
- Final name/description values

```go
s.logger.Info("Summarizer.GenerateDescription called",
    "first_message_len", len(req.FirstMessage),
    "project_name", req.ProjectName,
    "has_llm_client", s.llmClient != nil,
    "llm_client_type", fmt.Sprintf("%T", s.llmClient),
)
```

- [ ] **Step 2: Log JSON parsing failures with content**

```go
if err := json.Unmarshal([]byte(content), &result); err != nil {
    s.logger.Error("Failed to parse JSON response, using fallback",
        "error", err,
        "content", content,
        "content_bytes", []byte(content),
    )
    return extractSimpleResult(req.FirstMessage), nil
}
```

- [ ] **Step 3: Add handler-level logging**

In `handleGenerateDescription`:
- Log whether summarizer is nil
- Log LLM success vs fallback path taken
- Log final name/description saved

---

## Phase 2: Dynamic Title Updates (Tasks 4-6)

### Task 4: Add Periodic Title Refresh Trigger

**Files:**
- Create: `internal/session/refresher.go`
- Modify: `internal/session/session.go` (add new RPC handler)

- [ ] **Step 1: Create `SessionRefresher` type**

```go
package session

import (
    "context"
    "fmt"
    "log/slog"

    "github.com/caimlas/meept/internal/llm"
)

// SessionRefresher periodically refreshes session titles based on
// conversation content.
type SessionRefresher struct {
    llmClient *llm.Client
    logger    *slog.Logger
}

// NewSessionRefresher creates a new refresher.
func NewSessionRefresher(llmClient *llm.Client, logger *slog.Logger) *SessionRefresher {
    if logger == nil {
        logger = slog.Default()
    }
    return &SessionRefresher{
        llmClient: llmClient,
        logger:    logger,
    }
}

// RefreshRequest contains parameters for refreshing a session title.
type RefreshRequest struct {
    SessionID   string   `json:"session_id"`
    Topic       string   `json:"topic,omitempty"`      // Current dominant topic
    TurnCount   int      `json:"turn_count"`           // Number of exchanges
    Keywords    []string `json:"keywords,omitempty"`   // Extracted keywords
}

// RefreshResult contains the updated title.
type RefreshResult struct {
    Name        string `json:"name"`
    Description string `json:"description"`
}

// Refresh generates an updated title based on session progress.
func (r *SessionRefresher) Refresh(ctx context.Context, req RefreshRequest) (*RefreshResult, error) {
    r.logger.Info("SessionRefresher.Refresh called",
        "session_id", req.SessionID,
        "turn_count", req.TurnCount,
        "topic", req.Topic,
    )

    if r.llmClient == nil {
        r.logger.Warn("No LLM client, generating simple title")
        return &RefreshResult{
            Name:        fmt.Sprintf("session-%d", req.TurnCount),
            Description: fmt.Sprintf("%d turns", req.TurnCount),
        }, nil
    }

    systemPrompt := `You are updating a session title based on conversation progress.
Generate a JSON object with:
1. "name": A single lowercase word capturing the dominant topic
2. "description": A brief 3-6 word description in "category: detail" format

Categories: coding, research, task, personal, creative, system

Output ONLY valid JSON. Example:
{"name": "debugging", "description": "coding: fixed auth bug"}

All lowercase. No punctuation.`

    userPrompt := fmt.Sprintf(
        "Session has %d turns. Topic: %s. Keywords: %v\nProvide updated title.",
        req.TurnCount, req.Topic, req.Keywords,
    )

    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    resp, err := r.llmClient.Chat(ctx, []llm.ChatMessage{
        {Role: llm.RoleSystem, Content: systemPrompt},
        {Role: llm.RoleUser, Content: userPrompt},
    }, llm.WithMaxTokens(80), llm.WithTemperature(0.3))

    if err != nil {
        r.logger.Warn("LLM refresh failed, using fallback", "error", err)
        return &RefreshResult{
            Name:        fmt.Sprintf("session-%d", req.TurnCount),
            Description: fmt.Sprintf("%d turns, topic: %s", req.TurnCount, req.Topic),
        }, nil
    }

    var result RefreshResult
    if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
        r.logger.Warn("JSON parse failed, using fallback", "error", err)
        return &RefreshResult{
            Name:        fmt.Sprintf("session-%d", req.TurnCount),
            Description: fmt.Sprintf("%d turns", req.TurnCount),
        }, nil
    }

    return &result, nil
}
```

- [ ] **Step 2: Add RPC handler in `session.go`**

Register handler: `"session.refresh_title": h.handleSessionRefreshTitle`

```go
func (h *Handler) handleSessionRefreshTitle(msg *models.BusMessage) (any, error) {
    var params RefreshRequest
    if err := json.Unmarshal(msg.Payload, &params); err != nil {
        return nil, err
    }

    if params.SessionID == "" {
        return nil, fmt.Errorf("session_id required")
    }

    result, err := h.refresher.Refresh(msg.Context(), params)
    if err != nil {
        return nil, err
    }

    // Save to session store
    if err := h.store.UpdateName(params.SessionID, result.Name); err != nil {
        h.logger.Warn("Failed to update name", "error", err)
    }
    if err := h.store.UpdateDescription(params.SessionID, result.Description); err != nil {
        h.logger.Warn("Failed to update description", "error", err)
    }

    return result, nil
}
```

---

### Task 5: Wire Up Title Refresh in Agent Loop

**Files:**
- Modify: `internal/agent/loop.go`
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Add refresh trigger configuration**

Add to agent loop config:
```go
type AgentLoopConfig struct {
    // ... existing fields
    SessionTitleRefreshEvery int // Default: 5 turns
}
```

- [ ] **Step 2: Add turn counter and refresh logic**

In agent loop, track turns and call refresh:

```go
func (l *AgentLoop) maybeRefreshTitle(ctx context.Context) {
    l.turnCount++
    if l.turnCount % 5 == 0 && l.turnCount > 0 {
        // Get dominant topic from session tracker
        topic := l.sessionTracker.GetDominantIntent(l.sessionID)
        keywords := l.extractKeywordsFromRecentMessages(5)

        _, err := l.sessionHandler.RefreshTitle(ctx, session.RefreshRequest{
            SessionID: l.sessionID,
            Topic:     topic,
            TurnCount: l.turnCount,
            Keywords:  keywords,
        })
        if err != nil {
            l.logger.Warn("Title refresh failed", "error", err)
        } else {
            l.logger.Info("Session title refreshed", "turn", l.turnCount)
        }
    }
}
```

- [ ] **Step 3: Call refresh after each agent response**

Add call to `maybeRefreshTitle` after `handleAgentResponse`

---

### Task 6: Flutter Integration for Title Updates

**Files:**
- Modify: `ui/flutter_ui/lib/features/chat/chat_view.dart`
- Modify: `ui/flutter_ui/lib/services/websocket_service.dart`

- [ ] **Step 1: Listen for session.title.updated events**

Add event handler in WebSocket service:

```dart
void _handleSessionTitleUpdated(Map<String, dynamic> payload) {
  final sessionId = payload['session_id'] as String;
  final name = payload['name'] as String;
  final description = payload['description'] as String;

  // Update active session
  final active = _activeSessionController.value;
  if (active != null && active.id == sessionId) {
    _activeSessionController.value = active.copyWith(
      title: name.isNotEmpty ? name : description,
      description: description,
    );
  }

  // Update session list
  final sessions = _sessionController.value.sessions;
  final updated = sessions.map((s) {
    if (s.id == sessionId) {
      return s.copyWith(title: name.isNotEmpty ? name : description);
    }
    return s;
  }).toList();
  _sessionController.value = SessionState(sessions: updated);
}
```

- [ ] **Step 2: Dispatch refresh from client side (optional manual trigger)**

Add method:

```dart
Future<void> refreshSessionTitle() async {
  final session = _activeSessionController.value;
  if (session == null) return;

  await _post('/api/v1/bus/call', body: {
    'method': 'session.refresh_title',
    'params': {
      'session_id': session.id,
    },
  });
}
```

---

## Testing

### Task 7: End-to-End Testing

- [ ] **Step 1: Test LLM title generation**

Send message "what are your capabilities" and verify title is "capabilities" not the full message

- [ ] **Step 2: Test fallback when LLM unavailable**

Verify improved extractor produces better names

- [ ] **Step 3: Test mid-session refresh**

Have 5+ turn conversation and verify title updates

- [ ] **Step 4: Test Flutter display**

Verify status bar and sessions list show correct title
