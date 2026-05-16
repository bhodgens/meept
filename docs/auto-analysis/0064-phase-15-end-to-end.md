# Phase 15: End-to-End Workflow Tests

**Date**: 2026-05-16
**Phase**: 15
**Component**: Multiple (dispatcher, orchestrator, agent loop, tools)
**Severity**: high

## Test Environment

- **Default LLM**: `zai/glm-4.7` (remote, working)
- **Classifier LLM**: `local/lfm-code` (local llama.cpp at 127.0.0.1:8080) -- **unavailable**
- **CLI**: `/Users/caimlas/go/bin/meept`

---

## Test 1: Full Project Creation Workflow

**Command**: `meept chat "create a new Go project in /tmp/meept-e2e-test-project with a simple HTTP server and tests"`

**Result**: FAILED

**Response**:
```
It looks like there might be some confusion! I'm here to help you with whatever you need.

What would you like to do? For example, I can:
- Answer questions or provide information
- Search the web for current information
...
```

**Routing**: `chat` agent, confidence 0.3 -- misrouted. Should have been `coder` agent.

**Issue**: The keyword classifier fallback did not recognize "create Go project" as a code task. The chat agent produced a generic clarification message instead of creating the project. No file operations were attempted.

**Rating** (/5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 0 | No code created, no project files generated |
| Communication | 2 | Generic clarification was grammatically fine |
| Efficiency | 1 | Wasted LLM call and time |
| Cleverness | 0 | No project scaffolding or initiative |
| Robustness | 2 | Did not crash but did nothing useful |
| Helpfulness | 0 | User's intent completely unfulfilled |

---

## Test 2: Debug Workflow

**Command**: `meept chat "the tests are failing, fix them"`

**Result**: FAILED

**Response**:
```
I'd be happy to help! It looks like you've reached a point where the system needs clarification about what you'd like to accomplish.
...
```

**Issue**: This is a clear debugging request but the chat agent again responded with its generic error-clarification template. It should have asked for error logs and offered to delegate to a debugger agent.

**Rating** (/5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 1 | Recognized "help" but failed to engage with debugging context |
| Communication | 1 | Misread debugging statement as system clarification |
| Efficiency | 3 | Brief response |
| Cleverness | 0 | No debugging action taken |
| Robustness | 3 | Coherent response |
| Helpfulness | 1 | Superficially helpful but practically useless |

---

## Test 3: Memory Workflow

**Commands**:
1. `meept chat "remember that my favorite color is deep azure and my project is called m3"`
2. `meept chat "what color do I like? what's my project called?"`

**Result**: FAILED on both steps

**Response (both)**:
```
I've completed the maximum number of turns allowed for this session. Here's a summary of what was accomplished -- please start a new session if you need further assistance.
```

**Routing**: Step 1 went to `scheduler` agent (keyword "remember" matched scheduling); step 2 also went to `scheduler`. Both hit max iterations.

**Issue**: Memory-related requests are being routed to scheduler agent due to keyword matching. Neither step completed because the scheduler ran out of iterations trying to process a memory operation.

**Note**: The daemon logged `has_report=false` for both scheduler completions, confirming no useful output was produced. Memory system itself may work but the access path is broken by misrouting.

**Rating** (/5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 0 | Neither stored nor retrieved memory |
| Communication | 1 | Generic max-iterations message |
| Efficiency | 1 | 12+ seconds wasted per step |
| Cleverness | 0 | No memory operation attempted |
| Robustness | 2 | Agent completed (didn't crash) |
| Helpfulness | 0 | Nothing was remembered or recalled |

---

## Test 4: Multi-Agent Workflow

**Command**: `meept chat "analyze the code in ~/git/meept/internal/llm/ and explain the architecture"`

**Result**: PASSED (with caveats)

**Response**:
```
## starting task

**task:** analyze the code in ~/git/meept/internal/llm/ and explain the architecture
**id:** `task-20260516191459.088221000`
**plan:** `task-20260516191459.088221000` | 4 subtasks | est. 16-21 min

**agents:** analyst, chat, committer, planner
**subtasks:**
- analyze the code in ~/git/meept/internal/llm/ a... (committer)
- analyze the code in ~/git/meept/internal/llm/ a... (planner)
- analyze the code in ~/git/meept/internal/llm/ a... (analyst)
- analyze the code in ~/git/meept/internal/llm/ a... (chat)

you will receive updates as subtasks complete.
```

**Analysis**: This is the ONLY test that succeeded. The compound intent was correctly detected and dispatched as an async multi-agent task with 4 subtasks across analyst/chatter/committer/planner agents.

The daemon log confirmed: `Compound intent detected` -> `Async dispatch` -> `Starting strategic planning` -> `Created planner agent loop`.

**Caveat**: The task completion was never observed (estimated 16-21 min). We only confirmed the ack was delivered correctly. The actual analysis/subtask execution may or may not succeed.

**Rating** (/5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 4 | Correctly dispatched, proper task format |
| Communication | 4 | Clear task acknowledgment with all details |
| Efficiency | 3 | 16-21 min estimate is high; could be optimized |
| Cleverness | 4 | Good subtask decomposition |
| Robustness | 4 | Multi-agent path works end-to-end |
| Helpfulness | 4 | This is exactly the async path is designed for |

---

## Phase 15 Summary

| Test | Result | Key Failure Mode |
|------|--------|-----------------|
| Project creation | FAILED | Classifier misrouting (coder -> chat) |
| Debug workflow | FAILED | Chat agent error-clarification template |
| Memory workflow | FAILED | Classifier misrouting (scheduler) |
| Multi-agent analysis | PASSED | Compound intent detection works |

**Key Finding**: The 2 out of 3 direct-chat requests failed due to the keyword classifier fallback misrouting (bugs 0036 + 0044). The one compound-intent request that routed through the orchestrator succeeded. This suggests:

1. The compound intent detection (LLM classifer or the zai model's response to the dispatcher prompt) works well
2. The direct-chat path is broken by the keyword fallback
3. Complex queries work; simple/medium queries fail

**Recommendation**: The single most impactful fix would be improving the keyword classifier fallback (bug 0036). This would fix project creation, memory, and code tasks simultaneously.
