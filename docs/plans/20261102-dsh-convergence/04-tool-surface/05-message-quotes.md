# 05 Message-Quotes - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. The gate tests for this surface are the TUI
> headless/render assertions in 00T-tests.md (T4); the branch chose NOT to for
> a live-daemon e2e for this feature (presentational UI, master F12 marked
> E2E "n/a (UI)"). Do NOT delete, skip, weaken, or rewrite any committed gate
> test (report suspected-wrong gate tests to the orchestrator instead). Do NOT
> commit - the orchestrator handles all git operations after review. Do NOT
> use read_file on existing source - explore with search_files or terminal
> cat. After writing a file, do NOT read it back - write once and stop.
> Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** A presentational message-quote affordance: quote-select a chat
  message and insert a `> ` blockquote (with a `[msg <id>]` reference) into
  the input, so a reply visibly references the source message. Display already
  renders blockquotes; the gap is the select/insert gesture.
- **Dependencies:** none (independent of B1-B4)
- **Estimated Context:** 16K
- **Concurrency Group:** A
- **Audit references:** master C9 message-quotes; root AGENTS.md TUI/GUI parity rule

## Goal

Give users a way to reference a prior message when composing a reply: select a
chat message in the TUI and trigger quote-select, inserting a blockquote line
prefixed with a message-id reference into the input. This is a UI-parity
feature; the TUI is primary, the GUI is matched (root AGENTS.md TUI/GUI parity
rule) with an explicit documented deviation where a surface is deferred. No
server/engine work — the message renderer already supports blockquote display.

## Context

- `internal/tui/render/markdown.go` - the markdown renderer already detects
  blockquotes (`\n> `, detection at :97) and styles them. Display side already
  exists; the feature adds the input-side gesture + the reference prefix.
- `internal/tui/models/chat*.go` - TUI chat models: message selection and input
  state live here. The quote-select gesture plugs into the existing
  selection/input plumbing (keybinding → selected message id → input insert).
- `internal/tui/command_handler.go` and `internal/tui/palette.go` - where user
  gestures/keybindings route. Add the quote-select command/keybinding here.
- GUI: `ui/flutter_ui/` — parity target. Root rule: "TUI and Flutter GUI
  features must be kept at parity... Document surface-specific deviations
  explicitly with a justification." If the GUI surface is out of this leaf's
  testable scope, the deviation is documented, NOT silently omitted.

## Verified Anchors

- internal/tui/render/markdown.go:97 - blockquote detection (display side exists)
- internal/tui/models/chat*.go - message selection + input state (gesture home)
- internal/tui/command_handler.go / palette.go - keybinding/command routing
- ui/flutter_ui/ - GUI parity target

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/tui/models/messagequote.go (NEW) - pure model logic, testable headless:
//  func QuoteReference(msgID string, text string) string
//      // returns "> [msg <id>] <first line>\n>  <remaining lines...>" (blockquote)
//  func QuoteLabel(msgID string) string   // "[msg <id>] "
// TUI: a command/keybinding (e.g. palette "quote message" / a keyboard short)
//  that takes the currently selected message id + text and inserts
//  QuoteReference(...) at the cursor in the input.
// GUI: matching affordance and rendering, or an explicit documented deviation.
```

### What This Leaf Consumes

- B-B4 play no part here; the renderer's existing blockquote support
  (markdown.go:97) is the only dependency. Consumers: branch Review Checklist
  (parity), 00T gate T4.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|--------------|
| T1 | QuoteReference + QuoteLabel pure model | Create internal/tui/models/messagequote.go; Test internal/tui/messagequote_test.go (gate 00T T4) | `go test -p 2 ./internal/tui -run Quote -v` | FAIL: no QuoteReference/Label (gate test references it) | PASS: multi-line text becomes a blockquote with a `[msg <id>]` prefix; empty text yields `> [msg <id>] `; id is sanitized (no newlines) | Given msgID "msg-12" and text "first\nsecond"; When QuoteReference runs; Then "\n> [msg-12] first\n> second" is returned (leading newline separates from prior text) |
| T2 | Insert gesture in the TUI | Modify internal/tui/models/chat*.go (input insert) + a keybinding/command in command_handler.go or palette.go | `go test -p 2 ./internal/tui -run 'Quote|Palette' -v` | FAIL: no gesture wired | PASS: selecting a message and invoking the quote command inserts QuoteReference at the input cursor and keeps the previous input | Given a selected chat message; When the user triggers "quote message"; Then the input gains the blockquote + reference without clobbering existing text |
| T3 | Renderer parity pin | (verify only) no file change expected unless renderer lacks blockquote output | run an existing render test / inspect markdown.go:97 | (n/a - already renders) | PASS: rendered output shows the inserted blockquote as a quoted block (not as raw literal text) | Given the blockquote inserted into input; When the message renders; Then it displays as a quoted block (already supported) |
| T4 | GUI parity or documented deviation | ui/flutter_ui/ (if in scope) OR a documented deviation note here | manual / flutter unit if implemented | (n/a if deferred) | PASS: GUI mirrors the affordance, or the deviation below is explicit with justification | Given the TUI ships quote-select; When the GUI surface is checked; Then it is either implemented or the parity deviation is documented with a reason |

**Worked example - T1:**

T1: QuoteReference pure model.
1. Test:
```go
func TestQuoteReference_Basic(t *testing.T) {
    got := QuoteReference("msg-12", "first\nsecond")
    want := "\n> [msg-12] first\n> second"
    if got != want { t.Fatalf("got %q want %q", got, want) }
}
```
2. Implementation:
```go
func QuoteReference(msgID, text string) string {
    label := QuoteLabel(msgID)          // "[msg-12] "
    lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
    if len(lines) == 0 { lines = []string{""} }
    lines[0] = label + lines[0]
    var b strings.Builder
    b.WriteString("\n")
    for _, l := range lines { fmt.Fprintf(&b, "> %s\n", l) }
    return strings.TrimSuffix(b.String(), "\n")
}
func QuoteLabel(msgID string) string { return "[" + sanitizeID(msgID) + "] " }
// sanitizeID strips newlines/control chars; empty -> "<unknown>"
```
3. Verify: `go test -p 2 ./internal/tui -run Quote -v` -> PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and gate tests passing (00T T4 headless/render)
- [ ] Interface shape (QuoteReference/QuoteLabel) satisfied exactly
- [ ] TUI gesture wired (keybinding/command) + insert keeps prior input
- [ ] GUI parity done OR the permitted deviation documented here with justification (root AGENTS.md parity rule)
- [ ] E2E "n/a (UI)" justification recorded in the Completion Report
- [ ] No gate test deleted/weakened; gate-test diff empty
- [ ] No scope creep - no engine/server changes, no backend message model changes

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [the verify commands in Tasks; last 20 lines]
3. **Deviations:** [none / the GUI parity deviation with justification]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented (or the GUI deviation explicitly documented)
- [ ] Presentational scope held — no backend changes
- [ ] Reference id sanitized (no message-id/newline injection into a rendered block)
- [ ] Conventions followed; no bugs; no debug artifacts or line-number corruption
- [ ] Parity rule respected or deviation justified (root AGENTS.md)

Output: APPROVED or specific gaps with file + line references.

## Notes

- This is the one feature in the branch the master froze with `E2E: n/a (UI)`
  (master Test Gate row F12). The honest substitution is the TUI headless +
  markdown-renderer unit tests in the gate. Keep it presentational; do not
  reach into the message persistence layer — a reply that "quotes" is still an
  ordinary user message whose text happens to start with a `> ` block. Any
  future richer feature (inline anchored quote chips, message-id hyperlinks)
  is a separate change.
- Lower test coverage is EXPECTED here vs the other leaves; the gate matrix
  already records it. Do not inflate scope to "catch up" on coverage.
