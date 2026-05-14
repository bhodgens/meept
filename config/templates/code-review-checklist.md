---
name: code-review-checklist
description: "systematic code review checklist"
scope: turn
---

Review the following code against this checklist. For each item, mark [pass], [fail], or [n/a] and add a comment if relevant.

**Correctness:**
- [ ] Logic matches intent (no off-by-one errors, correct conditions)
- [ ] Edge cases handled (empty input, nil/null, overflow)
- [ ] Error paths are correct (errors propagated, not swallowed)

**Security:**
- [ ] No injection vectors (SQL, command, template)
- [ ] Input validation present where needed
- [ ] No hardcoded secrets or credentials

**Performance:**
- [ ] No unnecessary allocations in hot paths
- [ ] No N+1 queries or redundant loops
- [ ] Appropriate data structures used

**Readability:**
- [ ] Names are clear and consistent
- [ ] Complex logic has comments explaining "why", not "what"
- [ ] Function length is reasonable (under 50 lines)

**Testing:**
- [ ] Happy path covered
- [ ] Error paths covered
- [ ] Edge cases tested

**Go-specific (if applicable):**
- [ ] Errors wrapped with context: `fmt.Errorf("...: %w", err)`
- [ ] Resources closed with `defer` (response bodies, files, etc.)
- [ ] Context propagation on all I/O functions
- [ ] No goroutine leaks (wait groups, channels, or context cancel)
- [ ] Interfaces satisfied implicitly (no unnecessary type assertions)

Code to review:

$@
