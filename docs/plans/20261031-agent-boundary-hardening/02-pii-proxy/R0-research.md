# PII Redaction Best Practice Research - Research Leaf (R0)

> **For the implementing agent:** You are the researcher for this leaf.
> RESEARCH leaf: web_search + reading this repo's existing mechanisms,
> producing a findings document. NO production code. Do NOT commit.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Research PII redaction/unmasking best practice (Microsoft
  Presidio patterns, reversible pseudonymization with UUIDs,
  restore-window scoping, leakage classes) and distill design decisions
  for the branch's implementation leaves.
- **Dependencies:** none
- **Estimated Context:** 25K
- **Concurrency Group:** A
- **Audit references:** proposal 8 (master.md R4-R9)

## Goal

Produce `docs/research/pii-unmasking-findings.md` (new file) grounding
four design decisions: (a) regex-first detector layering + NER plug-in
seam, (b) reversible pseudonymization token shape and swap-store shape,
(c) restore-window scoping (session lifetime vs TTL vs turn), (d) the
leakage-class checklist the implementation must close.

## Context

Existing mechanisms to COMPOSE with (read, do not modify):
- internal/security/secrets.go:41-64, :182-300 - SecretObfuscator
  (#XXXX# placeholders, reversible map, zero restore callers - latent bug).
- internal/secrets/broker.go:25-28 - MEEPT_SECRET:<name> placeholder
  twin + scripts/audit-secrets-value-callers.py in-process-only rule.
- internal/security/orchestrator.go:205-237 - ScanOutput credential scan
  (the restore choke point's host).
- internal/agent/loop.go:3213 (ScanOutput call), loop.go:5353+ (LoRA/trace
  capture records tool output - a leakage surface).
- internal/auditlog/redact.go:13-16, :28 - output stored as SHA-256.

## Verified Anchors

- internal/security/secrets.go:188/:250/:271/:284 - Obfuscate/Deobfuscate/
  ObfuscateMessages/DeobfuscateMessages
- internal/security/secrets.go:182-187 - placeholder() 16-bit xxhash form
- internal/agent/security_hooks.go:160-200 - SecretObfuscationHook
- internal/agent/loop.go:3213 - ScanOutput call site (reply-guard
  neighborhood)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Survey in-repo mechanisms (read-only), map overlap with planned PII layer | findings doc "Survey" | grep confirms symbols | n/a | survey cites file:line | Given anchors; When read; Then survey names each + what the PII layer reuses |
| T2 | Web research: Microsoft Presidio architecture (recognizers, anonymizer, reversible operator patterns); OWASP LLM PII guidance | findings doc "Research" | file + citations | missing | >= 6 cited sources | Given research; When applied; Then recommendations carry sources |
| T3 | Web research: reversible pseudonymization with UUID tokens; k-anonymity tradeoffs of placeholder forms; restore-window scoping patterns (session vs TTL) | findings doc "Research" | section present | missing | token + window recommendation | Given findings; When applied; Then decision (b) and (c) are explicit |
| T4 | Leakage-class enumeration for this repo: user replies, conversation history, LoRA/trace capture (loop.go:5353+), auditlog rows, logs, bus events; checklist per class | findings doc "Leakage Classes" + "Design Decisions" | section present | missing | classes L1..Ln each with closure mechanism | Given the surfaces; When enumerated; Then implementation leaves can cite L-IDs |
| T5 | Design decisions D1..D5: regex set + defaults, NER seam interface, token format, restore scoping default, secret-#XXXX# restore interplay (incl. MEEPT_SECRET twin non-interference) | findings doc "Design Decisions" | section present | missing | decisions traceable to T2-T4 | Given findings; When applied; Then 01/02 leaves cite D-IDs |

ASCII markdown; sections Survey / Research / Leakage Classes / Design
Decisions (D1..Dn, L1..Ln). Every non-obvious claim cited (URL or file:line).

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `docs/research/pii-unmasking-findings.md` with D-decision IDs and
  L-leakage-class IDs consumed by leaves 01/02 via dispatch addenda.

### What This Leaf Consumes

- C-none (research leaf; consumes the anchors above).

## Self-Verification Checklist

- [ ] Findings file at exact path, ASCII only
- [ ] Every decision cites a source; leakage classes map to real surfaces
- [ ] No production code modified (`git status` shows only the findings file)
- [ ] Secret-restore interplay decision does NOT violate
      scripts/audit-secrets-value-callers.py discipline

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [findings file path]
2. **Verify command + output:** [grep proof anchors cited]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Findings present, complete, citations spot-checked
- [ ] Decisions actionable; leakage checklist complete vs T4 surfaces
- [ ] No production code touched

Output: APPROVED or specific gaps.

## Notes

- If research contradicts a branch contract (C1-B..C5-B), report it - the
  orchestrator amends contracts BEFORE implementation dispatches.
- Presidio's recognizer/anonymizer split is the reference shape for the
  NER seam; do not import Presidio (no Python in the daemon) - pattern
  only.
