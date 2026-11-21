# Executor prompt and tokens - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Make the 00T prefix and token tests pass.
> Do not weaken them. Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Pass SynthesizedPrompt into the per-turn clone. Return real token counts.
- **Dependencies:** 00T-test-gate.md, contracts C2 and C3
- **Estimated Context:** 60K
- **Concurrency Group:** A

## Goal

The constitution text the loop builds must reach the model. The result event must carry the provider token count.

## Tasks

1. Add `SetTurnSystemPrefix(prefix string)` on `AgentLoop`. It applies only to the next RunOnce on that receiver, then clears. Do not write the prefix onto the shared template loop's permanent system prompt.

2. In `employeeBoundAgentLoopExecutor.ExecuteBot` and `agentLoopBotExecutorAdapter.ExecuteBot`, call SetTurnSystemPrefix with the systemPrompt argument before RunOnce. Do not log-and-drop it.

3. Add `LastTurnTokens() int` on `AgentLoop`. Set it from the provider usage on the turn that just finished. Return 0 only when usage was absent. Do not change the RunOnce signature.

4. ExecuteBot returns LastTurnTokens() as tokensUsed. GoalLoop already forwards that int onto ExecutionResultEvent.TokensUsed. Do not hardcode 0.

5. Make `TestExecuteBotPassesSystemPrefix` and `TestExecuteBotReturnsLastTurnTokens` pass.

File ownership: `internal/agent/loop.go` seams and `internal/daemon/employee_service_adapter.go` only. Do not edit goal_loop.go. That file belongs to leaf 01.

## Interface Contract

Exposes C2 and C3.

## Self-Verification Checklist

- [ ] Shared template loop system prompt is unchanged after a turn
- [ ] A turn with usage 17 stores 17 on the result path
- [ ] RunOnce still returns (string, error)
- [ ] Do NOT commit

## Review Checklist

- [ ] Prefix is cleared after one turn
- [ ] No voice spoken-guard edit
- [ ] Do NOT commit
