"""Prompt builders for the teacher-mix sweep.

Prompt order is FROZEN as part of the experiment record:
lanes FIRST, then the verbatim message, then the output instruction.
See docs/plans/teacher-gate-mixture/master.md (Interface Contracts) and
leaf 02 for the lane table contract.
"""
from __future__ import annotations

import json

# 13 lanes, exact spellings. Order matters only for readability; the
# contract is the SET + SPELLINGS.
LANES: list[tuple[str, str]] = [
    ("coding", "writing, modifying, or generating source code: implement functions, create APIs, add features, refactor, write tests or migrations"),
    ("debugging", "diagnosing or fixing broken behavior: crashes, errors, failing tests, leaks, hangs, exceptions, investigate why something fails"),
    ("analysis", "research, compare, evaluate options, best practices, study a topic, explain tradeoffs, summarize findings"),
    ("search", "find external information: search the web or docs, look up references, locate libraries or articles"),
    ("chat", "small talk, greetings, thanks, acknowledgments, or brief social replies with no task content"),
    ("platform", "questions about the assistant itself: capabilities, available tools, agents, or how to use the system"),
    ("git", "version control operations: commit, push, pull request, branch, merge, rebase, tag, diff, history"),
    ("scheduling", "reminders, timers, calendar events, meetings, recurring tasks, time-based actions"),
    ("planning", "design an approach or architecture, roadmap, migration strategy, break work into steps before building"),
    ("review", "critique or assess provided work: code review, check an implementation for issues, quality feedback"),
    ("reporting", "summarize what happened: progress reports, digests of activity, status overviews"),
    ("recall", "remember or retrieve past conversation context: what we discussed, prior decisions, earlier work"),
    ("quickplan", "the user asks to plan AND execute autonomously now: run subagents, implement an approved plan, review-and-fix end to end without further check-ins"),
]

LANE_NAMES = [name for name, _ in LANES]

_OUTPUT_CONTRACT = (
    'Reply with ONLY a single-line JSON object: '
    '{"intent": "<lane>", "confidence": <0.0-1.0>, "reason": "<=15 words"}'
)

_ARBITRATION_RULE = (
    "The two classifiers disagreed on intent. Decide which intent is correct "
    "from the message text alone. You may pick either classifier's intent or "
    "a third intent from the list."
)


def _lane_block() -> str:
    lines = ["Classify the MESSAGE below into exactly one lane from this list:"]
    for name, desc in LANES:
        lines.append(f"- {name}: {desc}")
    return "\n".join(lines)


def worker_prompt(message: str) -> str:
    """Build the worker classification prompt (frozen order)."""
    return (
        f"{_lane_block()}\n\n"
        f"MESSAGE:\n{message}\n\n"
        f"{_OUTPUT_CONTRACT}"
    )


def judge_prompt(message: str, a: dict, b: dict) -> str:
    """Build the judge arbitration prompt (frozen order)."""
    verdicts = (
        "CLASSIFIER A VERDICT: " + json.dumps(a, ensure_ascii=True) + "\n"
        "CLASSIFIER B VERDICT: " + json.dumps(b, ensure_ascii=True)
    )
    return (
        f"{_lane_block()}\n\n"
        f"MESSAGE:\n{message}\n\n"
        f"{verdicts}\n\n"
        f"{_ARBITRATION_RULE}\n"
        f"{_OUTPUT_CONTRACT}"
    )
