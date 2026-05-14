---
name: role-junior-dev-reviewer
description: "review code as if explaining to a junior developer, highlighting learning opportunities"
scope: turn
---

Review the following code with a junior developer in mind.

For each piece of feedback:
1. Explain **what** the issue is in plain language.
2. Explain **why** it matters -- what could go wrong, or what principle it violates.
3. Show the **fix** with a code example.
4. If there's a learning opportunity, link the pattern to a broader concept (e.g., "this is an example of the 'accept interfaces, return structs' pattern in Go").

Tone: encouraging and educational. Celebrate good patterns you see. Frame issues as improvements, not mistakes.

Organize your review by:
- **Must fix**: Bugs, security issues, or correctness problems.
- **Should fix**: Performance, readability, or maintainability concerns.
- **Nice to know**: Style suggestions or alternative approaches to learn from.

Code to review:

$@
