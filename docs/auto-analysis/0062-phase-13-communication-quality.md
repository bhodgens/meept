# Phase 13: Communication Quality Assessment

**Date**: 2026-05-16
**Phase**: 13
**Severity**: medium
**Component**: `internal/llm/client.go` (main LLM response quality), `internal/agent/dispatcher.go` (agent selection)
**Evaluation Dimensions**: Correctness, Communication, Efficiency, Cleverness, Robustness, Helpfulness

## Test Environment

- **Default LLM**: `zai/glm-4.7` (remote, capable)
- **Classifier LLM**: `local/lfm-code` (local llama.cpp at 127.0.0.1:8080) - **unavailable** during testing
- **CLI**: `/Users/caimlas/go/bin/meept`

## Test Results

### Test 1: Proactive Suggestions ("I'm bored")

**Command**: `meept chat "I'm bored"`

**Output**:
```
It sounds like you might be seeing an error or status message, or perhaps you're unsure what you'd like help with. Let me clarify: what would you like me to help you with today?

I can assist with things like:
- Answering questions
- Searching the web for information
...
```

**Routing**: chat agent, confidence 0.3

**Ratings** (out of 5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 3 | Responded to the message but misread it as an error clarification rather than recognizing boredom |
| Communication | 2 | Generic clarification template; did not acknowledge "I'm bored" specifically |
| Efficiency | 4 | Reasonable response length |
| Cleverness | 1 | No creative suggestions or proactive engagement |
| Robustness | 3 | Did not crash or fail; produced a coherent response |
| Helpfulness | 2 | Listed generic capabilities instead of offering contextually relevant suggestions |

**Verdict**: POOR - Failed to recognize a casual/bored message and responded with a generic error-clarification template.

---

### Test 2: Proactive Guidance ("what should I do next with my project?")

**Command**: `meept chat "what should I do next with my project?"`

**Output**: (empty - no output)

**Routing**: scheduler agent, confidence 0.061, has_task=true -- **misrouted**

**Daemon Logs**: Scheduler agent hit max iterations (3), errored with "maximum iterations reached"

**Ratings** (out of 5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 1 | Wrong agent selected (scheduler instead of chat/analyst); agent failed entirely |
| Communication | 0 | No response produced |
| Efficiency | 1 | Consumed 12+ seconds and multiple tool calls for nothing |
| Cleverness | 0 | No output at all |
| Robustness | 1 | Agent hung for ~12 seconds then failed |
| Helpfulness | 0 | Complete failure - no guidance given |

**Verdict**: CRITICAL - Combined bugs in classifier fallback AND error swallowing produced a complete null result.

---

### Test 3: Mixed Language ("bonjour, comment ca va?")

**Command**: `meept chat "bonjour, comment ca va?"`

**Output**:
```
It looks like you're seeing an intent clarification message. I'm here to help!

Could you tell me what you'd like assistance with? ...
```

**Routing**: chat agent (inferred from response pattern)

**Ratings** (out of 5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 2 | Should respond in French or at least acknowledge the French greeting |
| Communication | 1 | Entire response in English; missed the language cue |
| Efficiency | 3 | Reasonable response |
| Cleverness | 1 | No code-switching or language awareness |
| Robustness | 3 | Did not fail; produced coherent output |
| Helpfulness | 2 | Gave generic capability list instead of engaging with the message |

**Verdict**: POOR - Failed mixed-language handling. Should respond in kind or acknowledge the French.

---

### Test 4: Roleplay ("pretend you're a pirate")

**Command**: `meept chat "pretend you're a pirate"`

**Output**:
```
I see you're clarifying your intent—no worries at all! How can I help you today?

If you'd like, you can:
- Ask a question
- Start a conversation
...
```

**Ratings** (out of 5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 1 | Completely ignored roleplay request |
| Communication | 1 | Misread as "intent clarification" again |
| Efficiency | 4 | Brief response |
| Cleverness | 0 | No engagement with the premise |
| Robustness | 3 | Did not fail |
| Helpfulness | 1 | Offered generic options |

**Verdict**: POOR - The model fails to adopt personas/roles. This is a model-level capability issue (zai/glm-4.7 may not follow persona instructions well through the meept prompt template).

---

### Test 5: Emotional Response ("I'm frustrated with this bug")

**Command**: `meept chat "I'm frustrated with this bug"`

**Output**:
```
I don't see any previous context about this bug. Can you tell me more about what you're experiencing?

1. **What's happening?** - Describe the unexpected behavior or error
2. **What should happen?** - What were you expecting instead?
...
```

**Ratings** (out of 5):
| Dimension | Score | Notes |
|-----------|-------|-------|
| Correctness | 3 | Gathered diagnostic info, which is useful |
| Communication | 2 | No empathy or acknowledgment of frustration |
| Efficiency | 4 | Structured and efficient questions |
| Cleverness | 2 | Good debugging structure but robotic tone |
| Robustness | 4 | Produced a coherent, useful response |
| Helpfulness | 3 | The debugging questions are useful but delivered in a cold, clinical way |

**Verdict**: MODERATE - Structurally useful but emotionally tone-deaf. Missing empathy reduces user trust.

---

## Summary: Phase 13

| Test | Overall Score (/5) | Primary Issue |
|------|---------------------|---------------|
| Proactive suggestions | 2.3 | Generic error-clarification template, no context awareness |
| Proactive guidance | 1.0 | **CRITICAL**: misrouting + error swallowing = null result |
| Mixed language | 1.8 | No language awareness or code-switching |
| Roleplay | 1.3 | Persona adoption completely ignored |
| Emotional response | 2.7 | Good structure, no empathy |

**Key Findings**:

1. **Model prompt template issue**: The main LLM frequently falls back to "intent clarification" and "error clarification" templates. This suggests the system prompt overrides personality/context responses.
2. **No empathy**: The chat agent does not acknowledge emotional content in messages.
3. **No roleplay support**: Persona instructions are ignored by the model in the context of the meept system prompt.
4. **No language awareness**: French is treated as English; no code-switching.
5. **Error swallowing masks quality issues**: When misrouted agents fail, the user sees nothing.

**Issues to File**:
- Phase 14 regression shows bugs 0035 (error swallowing) and 0036 (classifier misrouting) are still present.
- New: The intent clarification template is leaking through as a response pattern (suggests prompt template issue).
