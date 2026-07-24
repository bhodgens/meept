# Tree 04: Compaction — User Message Preservation

## Goal

Add an "All User Messages" verbatim preservation section to the context compaction prompt. Prevents user-intent drift across compactions by ensuring the agent remembers exactly what the user asked for, even after the conversation is summarized.

## Architecture Overview

Single change to the compaction prompt in `internal/llm/context_compactor.go`. The existing 9-section structured prompt gets a 10th section that lists all user messages verbatim (non-tool-result messages). This section is exempt from summarization — user words are preserved exactly.

This is a single-leaf tree — all work fits one agent (~35K context).

## Interface Contracts

### Compaction Prompt Section (added)
```
## All User Messages
Verbatim list of every user message (excluding tool results). These are preserved
exactly to prevent intent drift. Do NOT summarize or paraphrase — copy them word-for-word.
```

### Compaction Output Format
The compaction summary gains a new section between "Errors Encountered" and "Next Steps":
```
## All User Messages
- [timestamp or turn N]: "exact user message text"
- [timestamp or turn N]: "exact user message text"
```

## Child Index

| # | Leaf | Est. Context | Dependencies | Files Touched |
|---|------|-------------|--------------|---------------|
| 01 | compaction-user-messages | 35K | none | ~3 files |

## Dispatch Protocol

1. Dispatch leaf 01.
2. Review in-session. Commit after review.

## Review Checklist

- [ ] Compaction prompt includes "All User Messages" section
- [ ] Section instructs verbatim preservation (no summarization)
- [ ] User messages extracted correctly (role=user, not tool results)
- [ ] Section appears in compaction output between Errors and Next Steps
- [ ] Existing 9 sections unchanged
- [ ] No debug artifacts, no TODOs, no placeholder values

## Coding Conventions

See SHARED-CONVENTIONS.md §2-§3.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| 01-compaction-user-messages | COMPLETE | Verbatim user msg preservation in both prompts |

## Integration Test Plan

1. `go build ./internal/llm/...`
2. `go test ./internal/llm/... -race -run TestCompact`
3. Verify compaction prompt contains "All User Messages"
4. Verify compaction output preserves user messages verbatim
5. Verify tool results are NOT included in the user messages section

---

# Leaf 04-01: User Message Preservation in Compaction

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 04-compaction-user-messages/orchestrator.md
**Scope:** Add "All User Messages" section to the compaction prompt and extraction logic.
**Dependencies:** None
**Estimated Context:** ~35K

## Interface Contract

This leaf exposes:
- Updated compaction prompt with 10th section
- `extractUserMessages(messages []Message) []string` helper
- Updated compaction output formatting

## Tasks

### Task 1: Add user message extraction helper

**File:** `internal/llm/context_compactor.go`

Read the existing compaction code. Add a helper that extracts all user messages (role=user, excluding tool results) from the message history:

```go
// extractUserMessages returns all user-authored messages (not tool results)
// from the conversation history. These are preserved verbatim in compaction
// to prevent user-intent drift.
func extractUserMessages(messages []Message) []string {
    var userMsgs []string
    for _, msg := range messages {
        if msg.Role != "user" {
            continue
        }
        // Skip tool result messages (they have role=user but contain tool_result content)
        if isToolResultMessage(msg) {
            continue
        }
        content := extractTextContent(msg)
        if content != "" {
            userMsgs = append(userMsgs, content)
        }
    }
    return userMsgs
}
```

The exact implementation depends on the message type structure. Read the existing code to determine how to distinguish user messages from tool results (likely by checking content block types or a metadata field).

### Task 2: Update compaction prompt

**File:** `internal/llm/context_compactor.go`

Find the structured compaction prompt (the 9-section template). Add a 10th section after "Errors Encountered" and before "Next Steps":

```
## All User Messages
List ALL user messages that are not tool results. These are critical for understanding
the user's feedback and changing intent. Preserve them VERBATIM — do not summarize,
paraphrase, or omit any. Copy the exact words the user used.

Format:
- [turn N]: "exact user message text"
```

### Task 3: Inject user messages into compaction context

**File:** `internal/llm/context_compactor.go`

In the compaction execution (where the LLM is called to produce the summary), inject the extracted user messages as additional context so the summarizer has them available:

```go
// Before calling the LLM for compaction:
userMsgs := extractUserMessages(messagesToCompact)
if len(userMsgs) > 0 {
    var b strings.Builder
    b.WriteString("\n\n## User Messages (preserve verbatim in summary)\n")
    for i, msg := range userMsgs {
        b.WriteString(fmt.Sprintf("- [turn %d]: %q\n", i+1, msg))
    }
    compactionInput += b.String()
}
```

### Task 4: Update compaction output formatting

**File:** `internal/llm/context_compactor.go` or the formatting function

If the compaction output is post-processed (e.g., `formatCompactSummary` equivalent), ensure the "All User Messages" section is preserved in the formatted output and not stripped.

### Task 5: Update handoff prompt too

**File:** `internal/llm/context_compactor.go`

The handoff compaction prompt (11-section version for agent-to-agent handoff) should also include user message preservation. Add the same section to the handoff prompt template.

### Task 6: Tests

**File:** `internal/llm/context_compactor_test.go` (extend existing)

- `TestExtractUserMessages` — extracts user messages, skips tool results
- `TestExtractUserMessagesEmpty` — returns empty for no user messages
- `TestCompactionPromptHasUserSection` — prompt contains "All User Messages"
- `TestCompactionPreservesUserMessages` — user messages appear in compaction output
- `TestToolResultsExcluded` — tool result messages not in user messages section

## Self-Verification Checklist

- [ ] `go build ./internal/llm/...` compiles
- [ ] `go test ./internal/llm/... -race -run TestCompact|TestExtractUser` passes
- [ ] Compaction prompt has 10 sections (was 9)
- [ ] User messages extracted correctly (role=user, not tool results)
- [ ] Verbatim preservation instruction in prompt
- [ ] Handoff prompt also updated
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] User messages are truly verbatim (no summarization instruction)
- [ ] Tool results correctly excluded (check content block types)
- [ ] Section ordering: after Errors, before Next Steps
- [ ] Handoff prompt includes same section
- [ ] Existing 9 sections unchanged
- [ ] No debug artifacts, no TODOs, no placeholder values
