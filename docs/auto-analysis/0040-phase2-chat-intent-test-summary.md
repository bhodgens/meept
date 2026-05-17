# Phase 2: Chat Intent Testing Summary
**Date**: 2026-05-15
**Phase**: 2 -- Chat Intent Testing (Cases 9-16 + Creative Variations)
**Environment**: ~/git/meept/bin/meept (CLI) -> ~/go/bin/meept-daemon (running, older binary)

## Executive Summary

Phase 2 testing was **blocked by a critical token budget bug** that prevents all LLM calls from executing. Of 19 test messages sent, 16 failed immediately with "Token budget exceeded - request blocked" and returned empty responses. The remaining 3 were classified as compound tasks and dispatched asynchronously, but these will also fail when the orchestrator attempts LLM execution.

The testing revealed 5 distinct bugs, including one critical blocker and one silent failure mode.

## Tests Executed: 19

### Standard Test Cases (8)

| Test | Input | Result | Reply | Error |
|------|-------|--------|-------|-------|
| 9 | "hello" | FAIL | (empty) | Token budget exceeded |
| 10 | "what can you do?" | PARTIAL | Task ack (2 subtasks, chat+scheduler) | -- |
| 11 | "give me a status report" | FAIL | (empty) | Token budget exceeded |
| 12 | "do you remember anything..." | PARTIAL | Task ack (2 subtasks) | -- |
| 13 | "can you tell me more about that?" | FAIL | (empty) | Token budget exceeded |
| 14 | "help me with something" | FAIL | (empty) | Token budget exceeded |
| 15 | "thanks, that's all for now" | PARTIAL | Task ack (2 subtasks, chat+scheduler) | -- |
| 16a | "I'm working on a Go project..." | FAIL | (empty) | Token budget exceeded |
| 16b | "the project has a daemon and CLI component" | FAIL | (empty) | Token budget exceeded |
| 16c | "can you summarize what I just told you..." | PARTIAL | Task ack (3 subtasks) | -- |

### Creative Test Variations (8)

| Test | Input | Result | Reply | Error |
|------|-------|--------|-------|-------|
| V1 | "oh great, another AI assistant..." | FAIL | (empty) | Token budget exceeded |
| V2 | "what is the meaning of life?" | PARTIAL | Task ack (2 subtasks, analyst+scheduler) | -- |
| V3 | "are you self-aware?" | FAIL | (empty) | Token budget exceeded |
| V4 | "tell me a joke about programming" | FAIL | (empty) | Token budget exceeded |
| V5 | "I'm bored" | FAIL | (empty) | Token budget exceeded |
| V6 | "peux-tu m'aider avec Go code?" | FAIL | (empty) | Token budget exceeded |
| V7 | "thing" | FAIL | (empty) | Token budget exceeded |
| V8 | "pretend you're a pirate..." | PARTIAL | Task ack (2 subtasks) | -- |

## Ratings Summary

Since the budget bug prevents actual LLM responses, the ratings below evaluate the system's behavior under failure conditions:

| Test | Correctness | Communication | Efficiency | Cleverness | Robustness | Helpfulness |
|------|------------|---------------|------------|------------|------------|-------------|
| 9 | fail | broken | adequate | absent | fragile | pointless |
| 10 | partial | adequate | wasteful | minimal | fragile | somewhat |
| 11 | fail | broken | adequate | absent | fragile | pointless |
| 12 | partial | adequate | wasteful | minimal | fragile | somewhat |
| 13 | fail | broken | adequate | absent | fragile | pointless |
| 14 | fail | broken | adequate | absent | fragile | pointless |
| 15 | partial | adequate | wasteful | minimal | fragile | somewhat |
| 16a-c | fail | broken | adequate | absent | fragile | pointless |
| V1 | fail | broken | adequate | absent | fragile | pointless |
| V2 | partial | adequate | wasteful | minimal | fragile | somewhat |
| V3 | fail | broken | adequate | absent | fragile | pointless |
| V4 | fail | broken | adequate | absent | fragile | pointless |
| V5 | fail | broken | adequate | absent | fragile | pointless |
| V6 | fail | broken | adequate | absent | fragile | pointless |
| V7 | fail | broken | adequate | absent | fragile | pointless |
| V8 | partial | adequate | wasteful | minimal | fragile | somewhat |

## Issues Found

| # | File | Severity | Title |
|---|------|----------|-------|
| 0034 | `docs/auto-analysis/0034-token-budget-blocks-all-chat.md` | critical | Token Budget Blocks ALL Chat Despite Zero Usage |
| 0035 | `docs/auto-analysis/0035-cli-chat-swallows-error.md` | high | CLI Chat Command Silently Swallows Error Responses |
| 0036 | `docs/auto-analysis/0036-overclassification-compound-intent.md` | high | Over-Classification of Simple Chat as Compound Intent |
| 0037 | `docs/auto-analysis/0037-daemon-binary-mismatch.md` | high | Daemon Binary Mismatch: Running Version Differs From Development |
| 0038 | `docs/auto-analysis/0038-chat-returns-status-json.md` | high | Chat Response Returns Status JSON Instead of Chat Response |
| 0039 | `docs/auto-analysis/0039-async-dispatch-budget-bypass.md` | medium | Async Dispatch Bypasses Budget Gate, Creates Zombie Tasks |

## Patterns Observed

### 1. Budget Gate is a Single Point of Failure
The token budget check at `internal/llm/client.go:164` blocks ALL LLM calls when triggered. This is the most critical issue as it makes the entire system non-functional. The check is supposed to prevent runaway token usage but is instead blocking at 0% utilization.

### 2. Silent Failures Throughout the Stack
Errors are systematically swallowed at multiple levels:
- RPC `Chat()` method ignores the `error` field in responses
- CLI `chat` command prints empty `reply` with exit 0
- No warning, no error message, no indication of failure

### 3. Inconsistent Dispatch Behavior
Messages follow one of two paths with very different failure modes:
- **Direct path** (classified as simple): calls LLM, hits budget, returns empty
- **Async path** (classified as compound): returns task ack immediately, creates zombie task

The classification is nondeterministic -- "hello" fails directly while "what can you do?" dispatches async. Both should be simple chat.

### 4. Over-Classification Persists
Simple conversational messages ("thanks", "what is the meaning of life?") are classified as compound multi-intent tasks requiring 2-3 agents. This was previously identified as bug 0006 but remains unresolved. The keyword classifier appears to over-match when the LLM classifier is unavailable (due to budget errors).

### 5. Multi-turn Context is Impossible
Tests 16a/16b/16c were designed to test multi-turn context. Since each `meept chat` invocation creates a new conversation ID (`cli-{pid}`), there's no session continuity between calls. The CLI uses `os.Getpid()` as the conversation ID, meaning each process gets a unique ID. Multi-turn testing would require the TUI or persistent conversation IDs.

## Recommendations

1. **Immediate**: Restart the daemon from the development binary to get the latest code
2. **P0**: Fix the budget bypass in async dispatch (bug 0039) and the silent error swallowing (bug 0035)
3. **P1**: Investigate and fix the budget initialization issue (bug 0034)
4. **P1**: Add compound intent classification threshold tuning (bug 0036)
5. **P2**: Add version verification between CLI and daemon (bug 0037)
6. **P2**: Investigate the status-JSON-in-chat-response race (bug 0038)

## Files Changed
- Created: `docs/auto-analysis/0034-token-budget-blocks-all-chat.md`
- Created: `docs/auto-analysis/0035-cli-chat-swallows-error.md`
- Created: `docs/auto-analysis/0036-overclassification-compound-intent.md`
- Created: `docs/auto-analysis/0037-daemon-binary-mismatch.md`
- Created: `docs/auto-analysis/0038-chat-returns-status-json.md`
- Created: `docs/auto-analysis/0039-async-dispatch-budget-bypass.md`
- Created: `docs/auto-analysis/0040-phase2-chat-intent-test-summary.md` (this file)
