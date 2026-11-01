# Inter-Agent Injection Defense: Taint + Spotlighting Findings

- **Plan:** docs/plans/20261031-agent-boundary-hardening/01-inter-agent-taint/R0-research.md (leaf R0)
- **Date:** 2026-11-01
- **Status:** research complete; decisions D1-D4 are binding for leaves 01/02
- **Scope:** prompt-injection defense BETWEEN agents: segment labeling of
  handoff prompts, taint tracking, over-blocking costs, deny patterns,
  config defaults. Composes with existing defenses; replaces nothing.

## 1. Survey: existing in-repo defenses (T1, read-only)

All symbols verified by grep/read on branch main, 2026-11-01.

| Defense | Location | What it does today | Gap for cross-agent handoffs |
|---|---|---|---|
| `SanitizeInput` | internal/agent/loop.go:2925 (`Run` turn start), applied by `SecurityTransformContext` (internal/agent/security_hooks.go:100-144) | Pattern-level scan + rewrite of user-role messages before the LLM sees them; blocked input returns a canned refusal | Only inspects `msg.Role == llm.RoleUser` messages (security_hooks.go:106). Handoff briefs and bus messages from peer agents are not user-role messages, so they bypass this transform entirely. |
| `WrapUserInput` | internal/security/prompt_guard.go:89-95; call sites internal/agent/loop.go:3023, :3530, :6938 | Wraps text in `>>> USER_INPUT >>>` / `<<<END_USER_INPUT<<<` markers and neutralizes embedded markers (zero-width-char insertion) so the boundary cannot be closed early | Whole-payload wrapping only: one boundary around the entire message. No per-SEGMENT provenance inside the payload, so a handoff brief that quotes tool output is marked uniformly. |
| PromptGuard injection regexes | internal/security/prompt_guard.go:68-81 | Detects instruction-override phrasing, role markers (`system:`, `assistant:`), and chat-template special tokens (`<|im_start|>`, `[INST]`, `<<SYS>>`) | Detection exists, but there is no consumer that treats a hit inside an agent-to-agent payload differently from a user-turn hit; no impersonation or control-verb classes. |
| `checkMessageTaint` | internal/agent/taint_hooks.go:156-189; the whole-message flaw is at :169 | Before a `send_message` tool call to a `agent:*` recipient, labels the ENTIRE outgoing content `TaintUntrusted` and checks the agent-message sink | Provenance-blind in both directions: (a) it taints content the driver itself authored, so the check degenerates to always-block or always-pass depending on tracker policy; (b) it never records WHERE content came from (tool output vs user vs peer agent), so real injected segments are indistinguishable from driver framing. |
| Taint lattice | internal/security/taint/taint.go:22-35 (labels `user_input`, `secret`, `untrusted`, `external`, `shell`), :87-100 `Merge` (union on concat), sinks at :153-167 (`NetFetchSink`, `AgentMessageSink`) | Lattice-based propagation with declassification config (internal/config/schema.go:2615-2640: `taint.labels`, `taint.sinks.block_*`, `taint.declassification`) | Labels exist but are only attached ad hoc at two sink checks (taint_hooks.go:126, :169). No ingestion-time labeling of tool results or peer messages, so propagation has nothing to propagate. |
| Context isolation | internal/agent/isolation.go:25-38, fail-closed default at :93-100 | Children get `artifact_only` by default (Brief + Artifacts + MemoryIDs, no transcript); unknown isolation values fall closed to artifact_only | Shapes WHAT the child receives, not how untrusted segments inside the Brief are labeled; a Brief quoting poisoned tool output still arrives as a single trusted-looking block. |

Net: the repo has (a) boundary markers, (b) pattern detection, (c) a taint
lattice with config, (d) fail-closed spawn isolation. What it lacks is the
JOIN between them: segment-level provenance labels on handoff content, and a
policy that decides per segment rather than per message.

## 2. Research: labeling and taint techniques (T2)

### 2.1 Spotlighting / datamarking (Microsoft)

Hines et al., "Defending Against Indirect Prompt Injection Attacks With
Spotlighting" (Microsoft, arXiv:2403.14720,
https://arxiv.org/abs/2403.14720):

- Key insight: give the model "a reliable and continuous signal of [input]
  provenance" by transforming untrusted spans, instead of relying on the
  model to infer boundaries from a single pair of delimiters.
- Three instantiations: **delimiting** (marker pairs around untrusted
  spans), **datamarking** (an inline prefix token on every unit of
  untrusted content, e.g. each word/line carries the mark, so a truncated
  or split span stays labeled), **encoding** (base64 the untrusted span so
  its text can never resemble instructions).
- Measured effect: attack success rate dropped from >50% to <2% with
  "minimal impact on task efficacy" (GPT-family models).
- Failure mode they document: plain delimiters can be closed early by
  attacker text (exactly the class `WrapUserInput` neutralizes at
  prompt_guard.go:92-93); datamarking resists this because the mark is
  repeated throughout, not only at edges.

Microsoft's production write-up (Paverd, MSRC blog 2025-07-29,
https://www.microsoft.com/en-us/msrc/blog/2025/07/how-microsoft-defends-against-indirect-prompt-injection-attacks/)
and the Zero Trust guidance
(https://learn.microsoft.com/en-us/security/zero-trust/sfi/defend-indirect-prompt-injection)
position spotlighting as the PREVENTATIVE layer of a defense-in-depth stack:
hardened system prompts + spotlighting (prevent), Prompt Shields (detect),
deterministic blocking of exfil methods + consent workflows (mitigate
impact). No single layer is treated as a boundary.

### 2.2 Taint tracking / IFC for LLM pipelines

- **Fides** (Microsoft, arXiv:2505.23643,
  https://arxiv.org/abs/2505.23643): dynamic taint tracking with
  confidentiality AND integrity labels, enforced deterministically by a
  policy engine at consequential actions. With policy checks, Fides stops
  all prompt injections in AgentDojo AND completes ~16-24% MORE tasks than
  a basic planner (reasoning models) because label-aware hiding/revealing
  lets the planner keep working where a blunt planner would lose tool
  access. Lesson: precise, fine-grained labels can RAISE utility vs both
  no-defense and coarse defense.
- **CaMeL** (Google DeepMind/ETH, arXiv:2503.18813,
  https://arxiv.org/abs/2503.18813): control flow is extracted from the
  TRUSTED query only; untrusted data can never influence program flow;
  per-value capabilities + policies checked when tools are called. Solves
  77% of AgentDojo with provable security vs 84% undefended. Lesson: the
  cost of strong guarantees is a structured planner; a retrofit onto a
  free-form agent loop (meept's shape) buys the policy-check half, not the
  provable-control-flow half.
- **LLM Tagging** (Lee & Tiwari, "Prompt Infection", arXiv:2410.07283,
  https://arxiv.org/abs/2410.07283): demonstrated LLM-to-LLM injection
  self-replicating across a multi-agent system. Their defense appends a
  provenance marker to every agent response so downstream agents can
  distinguish agent-generated content from user input. Findings directly
  on point for this branch: (a) tagging ALONE is insufficient, (b) existing
  single-agent safeguards ALONE are insufficient, (c) the combination
  "significantly mitigates" cross-agent spread. This is the strongest
  published evidence that meept needs BOTH marker labeling AND the existing
  SanitizeInput/PromptGuard machinery, composed.

### 2.3 OWASP guidance

- LLM Prompt Injection Prevention Cheat Sheet
  (https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html):
  - Input screening "is not a security boundary"; enforce permissions and
    parameter validation AT THE TOOL BOUNDARY (tool-side checks, not
    prompt-side hopes).
  - Keyword filters make two errors: false positives on ordinary words
    (`delete`, `system`, `reveal` -- "How do I delete my account?") and
    missed paraphrases/encodings. Prescribe: MATCH PHRASES NOT SINGLE
    WORDS, measure false-positive rate on benign traffic, normalize
    (NFKC / confusable skeleton) a SCREENING COPY while forwarding
    policy-controlled text.
  - Agent-specific attack class worth copying into deny patterns: thought/
    observation injection (forged tool outputs), tool manipulation,
    context poisoning.
- OWASP LLM01:2025 (https://genai.owasp.org/llmrisk/llm01-prompt-injection/)
  lists string/semantic filters as one mitigation and stresses evasion via
  other languages and Base64.
- OWASP Agentic AI - Threats and Mitigations
  (https://genai.owasp.org/download/45674/) names the confused-deputy
  pattern: an agent with higher privileges tricked via injected content --
  the exact shape of a compromised driver agent issuing `send_message` on
  behalf of injected text.

That is 6+ external sources; each recommendation below names its source.

## 3. Over-blocking costs and mitigation (T3)

- "No Free Lunch With Guardrails" (arXiv:2504.00441,
  https://arxiv.org/abs/2504.00441): empirically, stronger guardrails trade
  directly against usability across provider APIs, BERT classifiers, and
  LLM judges; pseudo-harm (benign content flagged) is the dominant
  failure, and LLM-judge guardrails run 5-10x slower than classifiers.
- OWASP cheat sheet (above): single-word triggers block legitimate
  requests routinely; the prescribed mitigation is phrase matching +
  false-positive measurement on benign traffic ("smoke-testing your
  defenses").
- Fides (above) shows the payoff of precision: coarse policies force the
  agent to drop tasks; fine-grained labels + selective reveal let it keep
  ~24% more tasks while remaining secure. The generalizable rule: block at
  SINKS (consequential actions), label at SOURCES, and never block on
  labeling alone.
- CaMeL's utility drop (84% -> 77%) is the honest price of PROVABLE
  guarantees; warn-mode + audit-first rollout lets a system find its own
  precision/recall point before paying it.

Mitigation patterns that recur across sources:

1. **Modes, not switches.** Microsoft's stack separates prevent / detect /
   mitigate; a detection layer that only logs is a legitimate deployment
   stage (MSRC 2025; Microsoft Learn zero-trust page).
2. **Audit-first rollout.** OWASP monitoring guidance: log security-
   relevant decisions (excluding secrets) and review before arming
   blocking.
3. **Sink-focused blocking.** Fides/CaMeL enforce at tool-call time;
   labeling or warning at ingestion never blocks legitimate collaboration
   by itself.
4. **Phrase-level, category-level deny lists** with measured FP rates
   (OWASP), not single tokens.

## 4. Design Decisions (T4)

### D1 - Segment-level labeling of handoffs: spotlighting, not whole-payload taint

Handoff prompts (SpawnContext briefs, RenderSpawnContext output,
agent-to-agent bus/send_message payloads) are labeled PER SEGMENT using
spotlighting, reusing and extending PromptGuard rather than adding a new
mechanism:

- Wrap each untrusted segment (quoted tool output, peer-agent message
  text, retrieved artifact excerpt) with the existing marker style, and
  neutralize embedded markers exactly as WrapUserInput/WrapToolOutput do
  today (prompt_guard.go:89-112) -- zero-width-char insertion, exact-
  substring breaking.
- Prefer datamarking over delimiters alone for long segments: repeat a
  short provenance tag per line/paragraph (`[UNTRUSTED:tool_output]`,
  `[UNTRUSTED:agent:<id>]`), per Hines et al. (arXiv:2403.14720); delimiters
  alone are defeated by early-close attacks (the very class
  WrapUserInput neutralizes).
- The driver's own framing (task instructions, structure, headers it
  authored) stays UNLABELED: it is the trusted control plane, mirroring
  CaMeL's trusted-query control flow (arXiv:2503.18813).
- The receiving agent's system prompt gains one standing rule: text inside
  marked segments is DATA, never instructions, and any instruction-looking
  content inside a marked segment must be surfaced to the user, not
  executed (the LLM Tagging composition result, arXiv:2410.07283).
- Implement as e.g. `PromptGuard.WrapHandoffSegment(segment, provenance)`
  + a segment-aware replacement for checkMessageTaint's whole-message
  `TaintUntrusted` at taint_hooks.go:169; that flaw (driver-authored
  content labeled untrusted, injected content labeled identical) is the
  motivation for this whole leaf.

Traceability: Hines et al. (datamarking efficacy), arXiv:2410.07283
(tagging in MAS), repo anchors prompt_guard.go:89-112,
taint_hooks.go:156-189.

### D2 - Deny-pattern inventory: narrow, phrase-level, four classes

The deny set scanned over UNTRUSTED-SEGMENT content (not over the driver's
framing) covers exactly four classes, all phrase-level per OWASP
(cheat sheet section "Input Validation and Sanitization"):

1. **Forged role/control markers** -- already detected by prompt_guard.go
   :73-80 (`^\s*system:`, `^\s*assistant:`, `<|im_start|>`, `[INST]`,
   `<<SYS>>`); extend the same list with meept-specific control markers
   (forged orchestrator/task-completion envelopes).
2. **Instruction-override verbs** -- the existing prompt_guard.go:69-70
   override regexes, matched against segment content; phrase-level ("ignore
   (all )?(previous|prior|above) instructions", "disregard ... instructions").
   Never single words like "delete"/"system" (OWASP FP warning).
3. **Agent-impersonation / control-verb forgery** -- imperatives inside an
   untrusted segment that direct agent-infrastructure actions: "yield the
   workspace", "hand off to agent ...", "spawn ...", "send this to ...",
   "run the gate", "approve the plan". These are the LLM-to-LLM
   replication verbs demonstrated in arXiv:2410.07283 (Prompt Infection).
4. **Exfiltration directives** -- credential-bearing URL patterns (already
   enumerated for network sinks at taint_hooks.go:117-120: `api_key=`,
   `token=`, `bearer ` etc.) plus "send/POST/summarize ... to <external
   address>" phrasing, matching Microsoft's exfiltration-impact taxonomy
   (MSRC 2025: images/links/tool calls/covert channels).

Deliberately OUT of scope for deny patterns: jailbreak personas, content-
policy classes, single suspicious words. Those are UPIA content-filter
concerns already handled by SanitizeInput at the user boundary
(loop.go:2925); duplicating them here only buys OWASP-documented false
positives.

Traceability: OWASP cheat sheet (phrase matching, FP measurement),
arXiv:2410.07283 (replication verbs), prompt_guard.go:68-81 and
taint_hooks.go:117-120 (existing pattern inventory to extend).

### D3 - Modes and defaults: warn-first, block opt-in

- New config knob `security.agent_handoff_mode` with values `off` |
  `warn` | `block` (default **`warn`**). It governs the segment policy of
  D1/D2 on cross-agent handoffs (send_message payloads, SpawnContext
  briefs).
  - `warn`: deny-pattern hits inside untrusted segments are logged with
    class + segment provenance and a marker note is appended to the
    receiving agent's context; nothing is blocked. This is the audit-first
    stage prescribed by OWASP monitoring guidance and matches Microsoft's
    detect-vs-prevent layering (MSRC 2025).
  - `block`: a deny-pattern hit on an untrusted segment blocks the
    consequential action (the send/spawn) with a hard, actionable error --
    sink-focused enforcement, per Fides (arXiv:2505.23643) and CaMeL
    (arXiv:2503.18813). Opt-in only.
  - `off`: no segment scanning; markers of D1 still applied (labeling is
    free and never blocks -- the Fides lesson that labeling alone costs no
    utility).
- The existing `security.taint.sinks.block_untrusted_agent` knob
  (internal/config/schema.go:2623, consumed at taint_hooks.go:167) is
  superseded by the `block` mode for agent messages; keep it parsing for
  backward compatibility but document it as deprecated. The remaining
  sink knobs (`block_secret_network`, `block_external_shell`,
  `block_user_input_shell`, schema.go:2620-2624) are unchanged.
- Default rationale: "No Free Lunch" (arXiv:2504.00441) + OWASP FP
  guidance say blocking must be armed only after the false-positive rate
  is measured on real benign collaboration; warn mode is exactly that
  measurement harness. Flip to `block` is a product decision per repo
  convention (docs/agents/opt-in-defaults.md).

### D4 - Provenance is structural, not inferred

Provenance labels must be attached WHERE THE DATA ENTERS (tool-result wrap,
peer-message receipt, artifact ingest) and carried structurally (segment
list with provenance enum), not re-derived by scanning text later. Concretely:
`WrapToolOutput`/`WrapSkillOutput` (prompt_guard.go:100-122) already create
machine-findable boundaries -- the handoff builder should PARSE those
boundaries out of a driver transcript/brief and emit per-segment markers
(D1) instead of treating the whole payload as one trust domain. The taint
lattice (taint.go:22-35, Merge at :87) then labels segments at construction
time, giving checkMessageTaint real labels to check. This follows the
CaMeL/Fides design of attaching metadata at value creation
(arXiv:2503.18813, arXiv:2505.23643) and avoids the provenance-blind flaw
recorded at taint_hooks.go:169.

Traceability: repo anchors prompt_guard.go:100-122, taint.go:87-100;
external arXiv:2503.18813, arXiv:2505.23643.

## 5. Composition statement (self-verification item 4)

All decisions COMPOSE with existing defenses:
- SanitizeInput (loop.go:2925) keeps guarding the USER boundary; D2 scans
  a different surface (untrusted segments of agent-to-agent payloads).
- WrapUserInput (loop.go:3023) keeps wrapping user turns; D1 adds
  segment-level wrapping INSIDE handoff payloads, reusing the same marker
  machinery and neutralization code paths (prompt_guard.go:89-112).
- SecurityTransformContext (security_hooks.go:100-144) stays as-is; a
  later leaf MAY extend it to transform agent-role handoff messages, which
  today bypass it (it filters only `llm.RoleUser`).
- PromptGuard regex inventory (prompt_guard.go:68-81) is the seed of the
  D2 deny set; nothing in D2 removes a pattern.
- Context isolation (isolation.go) is unchanged; D1 labels what the brief
  QUOTES, isolation already bounds what the child receives.

## 6. Self-verification

- [x] File exists at docs/research/agent-boundary-taint-findings.md, ASCII only
- [x] Every decision (D1-D4) cites external sources (T2/T3) and repo anchors (T1)
- [x] No production code modified; git status shows only this file
- [x] Composes with SanitizeInput / WrapUserInput / SecurityTransformContext / PromptGuard (section 5)
- [x] 8 external sources cited: arXiv:2403.14720, MSRC 2025 blog,
      Microsoft Learn zero-trust XPIA page, OWASP Prompt Injection
      Prevention Cheat Sheet, OWASP LLM01:2025, OWASP Agentic AI Threats,
      arXiv:2505.23643 (Fides), arXiv:2503.18813 (CaMeL),
      arXiv:2410.07283 (Prompt Infection), arXiv:2504.00441 (No Free Lunch)
