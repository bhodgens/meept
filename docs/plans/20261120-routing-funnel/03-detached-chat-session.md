# Detached chat session - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Make the 00T session test pass. Do not weaken it.
> Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Merge detached employee and bot-trigger output into one standing chat session per employee.
- **Dependencies:** 00T-test-gate.md, 01 REVIEWED, 02 REVIEWED, contract C4
- **Estimated Context:** 70K
- **Concurrency Group:** B

## Goal

A goal round, a bus trigger, a cron trigger, and a webhook trigger write the assistant reply into the employee chat session. They do not mint a throwaway conversation id.

## Tasks

1. Add `EmployeeChatSessionID(employeeID string) string` returning `"emp-sess-" + employeeID`.

2. Before Execute, ensure the session store has a row for that id. Use the existing session create path. Do not invent a second store.

3. Change the executor so RunOnce receives that id, not `id.Generate("bot-exec")`. Confirm the chat message writer used by a normal chat turn also runs for this id. If RunOnce only updates an in-memory conversation, call the same persist function the chat handler uses.

4. SetSpeakContext for a goal round uses EmployeeChatSessionID, not the raw employee id.

5. `speak_push_bridge` push sets SessionIDs to that one session. Remove the nil broadcast for this path.

6. ExecutionResultEvent.SessionID for these rounds is the standing session id.

7. Make `TestEmployeeTurnUsesStandingSession` pass.

Do not merge the voice spoken guard into this path. A live voice call keeps its own session.

## Interface Contract

Exposes C4.

## Self-Verification Checklist

- [ ] No production employee turn uses the bot-exec prefix
- [ ] Notify push names one session id
- [ ] Voice guard files were not edited
- [ ] Do NOT commit

## Review Checklist

- [ ] Session id format matches C4
- [ ] Chat persist path is the existing writer, not a new table
- [ ] Do NOT commit
