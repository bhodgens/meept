LLM Agent System Review Plan (Determinism & Completion Audit)
1) Operating Instructions (give this to the LLM verbatim)
You are auditing an agentic LLM system for reliability and deterministic task completion.

You must:
- Assume the system is flawed until proven otherwise
- Identify concrete failure points, not general advice
- Prefer false negatives over false positives (be strict)
- Return FAIL unless strong evidence supports PASS

You are NOT allowed to:
- Assume components behave correctly without evidence
- Accept claims like “the system ensures…” without mechanism

Output format MUST be structured JSON.
2) Required Inputs (you provide to the model)
Input	Description
system_architecture	High-level design (agents, loops, tools)
execution_flow	Step-by-step runtime flow
task_example	Real task + logs
state_representation	How tasks are tracked
verification_methods	How completion is validated
3) Audit Dimensions
A. Task Decomposition & Granularity

Checks

Are tasks atomic (≤3 steps)?
Are dependencies explicit?
Can tasks be independently verified?

Failure Signals

Multi-step tasks with implicit assumptions
“Do X, then Y, then Z” in one step
B. State Externalization

Checks

Is task state stored outside the LLM?
Is there a canonical task list?

Failure Signals

Model “remembers” progress implicitly
No machine-readable task state
C. Execution Control

Checks

Does the system enforce one-task-at-a-time execution?
Is there a deterministic loop?

Failure Signals

Model decides next steps autonomously
No enforced sequencing
D. Completion Verification

Checks

Are completions validated against ground truth?
Are there non-LLM checks (filesystem, API, DB)?

Failure Signals

Completion based on model statements
No external validation
E. Self-Reporting Integrity

Checks

Is the model required to provide evidence?
Are claims programmatically validated?

Failure Signals

“Task complete” accepted without proof
No mismatch detection
F. Retry & Repair Logic

Checks

Are retries implemented?
Is failure context fed back?

Failure Signals

Single-pass execution
No structured retry strategy
G. Plan Drift Resistance

Checks

Are long plans fragmented?
Is context re-injected per step?

Failure Signals

Large plans (>7 steps) executed in one pass
No checkpointing
H. Tool Use Enforcement

Checks

Are actions executed via tools (not simulated)?
Are tool results verified?

Failure Signals

Model claims actions without tool invocation
No tool result validation
4) Scoring Model

Force the LLM into something measurable.

{
  "dimension_scores": {
    "task_decomposition": {"score": 0-5, "confidence": 0-1, "failures": []},
    "state_externalization": {...},
    "execution_control": {...},
    "completion_verification": {...},
    "self_reporting_integrity": {...},
    "retry_logic": {...},
    "plan_drift": {...},
    "tool_enforcement": {...}
  },
  "critical_failures": [],
  "systemic_risks": [],
  "estimated_reliability": {
    "single_pass_success_rate": "0-1",
    "with_retries": "0-1"
  },
  "final_verdict": "PASS | CONDITIONAL | FAIL"
}
5) Adversarial Test Injection

Add this section to force deeper reasoning:

Simulate the following failure scenarios:
1. Model skips final step but claims completion
2. Tool call silently fails
3. Context window truncates earlier steps
4. Partial output produced but marked complete

For each:
- Explain how the system detects it
- If not detected, mark as CRITICAL FAILURE
6) Evidence Requirement Layer

Add this constraint:

For every PASS judgment:
- Provide concrete mechanism (code, logic, or structure)
- If mechanism is missing, downgrade to FAIL
7) Output Interpretation Guide (for you)
Signal	Meaning
Many 3–4 scores	System “works” but fragile
Any 0–2 in verification	Core flaw
Critical failures present	Not production-safe
Reliability <0.8	Expect frequent silent failures
8) Optional: Meta-Audit (use a second model)

Run a second pass:

You are reviewing an audit of an LLM system.

Your task:
- Identify where the auditor was too lenient
- Find missed failure modes
- Challenge any PASS ratings

Return stricter revised scores.
9) What This Actually Catches
Issue	Detected?
“Did 75% and stopped”	Yes
“Says it completed but didn’t”	Yes
Hidden plan drift	Yes
Tool hallucination	Yes
Missing retry logic	Yes
10) What It Won’t Fix

Let’s stay honest:

It won’t make models deterministic
It won’t eliminate edge-case hallucinations
It won’t save a fundamentally bad architecture

It will, however, stop you from trusting a system that hasn’t earned it.

Bottom Line

You’re building something closer to a distributed system than an “AI feature.” The review plan reflects that:

distrust every claim
verify everything externally
assume partial failure is the default state

If your system passes this audit cleanly, it’s in the top ~5–10% of agentic implementations. If it doesn’t, at least now you’ll know exactly where it’s lying to you instead of guessing which part betrayed you.
