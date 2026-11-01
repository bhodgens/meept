# PII Redaction and Reversible Pseudonymization -- Research Findings (R0)

> Leaf: docs/plans/20261031-agent-boundary-hardening/02-pii-proxy/R0-research.md
> Research only. No production code touched. ASCII only.
> Every non-obvious claim carries a citation: a URL or an in-repo file:line.
> Decision IDs D1..D6 and leakage-class IDs L1..L7 are the stable interface
> consumed by implementation leaves 01/02 via their dispatch addenda.

## Survey (T1) -- in-repo mechanisms the PII layer composes with

Read-only inventory. The planned PII layer REUSES these shapes; it does not
modify them.

1. **SecretObfuscator** -- `internal/security/secrets.go:41-51` (struct),
   `:188` Obfuscate, `:250` Deobfuscate, `:271` ObfuscateMessages,
   `:284` DeobfuscateMessages. Placeholders are 16-bit xxhash forms
   `#XXXX#` (`placeholder()`, secrets.go:181-185): only 65,536 possible
   placeholder strings, monotonically assigned from a counter, with a
   collision-retry loop at `:238-242`. Obfuscate is invoked on every
   context message via SecretObfuscationHook
   (`internal/agent/security_hooks.go:158-200`).
2. **Zero restore callers -- latent bug.** A grep for `Deobfuscate` across
   `internal/ pkg/ cmd/ scripts/` shows hits ONLY inside
   internal/security/secrets.go (its own implementation), its test file
   secrets_test.go, and no production caller. The model therefore sees
   `#XXXX#` placeholders but nothing ever restores the originals anywhere:
   a reply that quotes a placeholder stays obfuscated for the user, and the
   map grows unboundedly. The leaf brief calls this the latent bug; grep
   confirms it. Any PII restore path must NOT replicate this "obfuscate
   always, restore never" shape.
3. **MEEPT_SECRET broker twin** -- `internal/secrets/broker.go:25` defines
   `PlaceholderPrefix = "MEEPT_SECRET:"`. Broker values live in memory
   only; `resolve()` is unexported so no code outside package secrets can
   obtain a real value (broker.go:1-9 package comment, broker.go:50-57).
   `scripts/audit-secrets-value-callers.py` pins the in-process-only rule:
   `Broker.Value`/`resolve` callers must be in an explicit file allowlist
   (VALUE_CALL regex at script line 65; ALLOWED_VALUE_CALLERS file-level
   allowlist at line 42). Meept therefore already has TWO placeholder
   vocabularies: `#XXXX#` (short-lived, in-context) and `MEEPT_SECRET:<name>`
   (never-restored-by-design). The PII layer adds a THIRD vocabulary and
   must be distinguishable from both.
4. **ScanOutput** -- `internal/security/orchestrator.go:205-237`. Scans LLM
   output for credentials, optionally redacts (`o.config.RedactOutput`),
   returns (sanitized, ok, warnings). Called from
   `internal/agent/loop.go:3211-3220` in the reply-guard neighborhood,
   AFTER `refusalFallbackDisclosure` is appended (loop.go:3210). This is
   the natural host choke point for a PII restore pass: it already runs on
   every final reply and already carries a redact-and-replace contract.
5. **LoRA/trace capture** -- `internal/agent/loop.go:5360-5379`: tool output
   is wrapped (`WrapToolOutput`), tainted labels recorded, then
   `l.learningCapture.RecordResearch(ctx, conversationID, userQuery,
   toolName, output)` persists the RAW tool output for training data.
   Whatever redaction the egress path applies, this capture path records
   text that never passed ScanOutput -- a distinct leakage surface (L4).
6. **Audit chain** -- `internal/auditlog/redact.go`: `OutputSHA256` (line
   28) stores command output as SHA-256 only ("never raw command output",
   master.md C4); `SanitizePayload` (line 37) redacts secret-fragment KEYS
   (blocklist line 13-16), scrubs values via OutputMonitor
   DetectAndRedact, truncates strings over 4096 bytes. Audit rows are
   already PII-conservative; the PII layer must not weaken this and must
   not need restore capability in audit rows.
7. **Predictable-ID discipline** -- root AGENTS.md forbids
   `time.Now().UnixNano()`/`math/rand` for IDs even in fallbacks;
   `pkg/id.Generate()` with a zero-suffix catastrophic fallback is the
   house pattern, and `tools/analyzers/predid` detects violations. PII
   token generation MUST use crypto/rand or pkg/id, and the predid
   analyzer must stay clean.

## Research (T2, T3) -- external best practice, cited

### Presidio architecture (reference shape; pattern only, no import)

- Two-module split: an **analyzer** holding many pluggable **recognizers**
  (regex, deny-list, checksum, rule-based, NER-ML, context-word
  reinforcement) that produce `(entity_type, start, end, score)` spans; and
  an **anonymizer** holding named **operators** (replace, redact, hash,
  mask, encrypt, custom, keep) applied per entity type [1]
  (https://github.com/microsoft/presidio/blob/main/docs/text_anonymization.md).
- Reversibility is an operator property, not an engine property: the
  anonymizer has a separate **DeanonymizeEngine** whose only built-in
  operator is **decrypt** (AES, key-parameterized); deanonymization
  requires the operation to have been reversible [2]
  (https://github.com/microsoft/presidio/blob/53e9e520/docs/anonymizer/index.md).
  The key never travels with the text.
- Recognizer guidance: recognizers are the unit of extension; balance FP
  rate (usability cost) against FN rate (leakage cost); per-recognizer
  performance budget ~100ms/100 tokens; NER (spaCy/stanza/transformers)
  is recommended only where deny-lists and regexes are structurally
  insufficient [3]
  (https://presidio.dataprivacystack.org/analyzer/developing_recognizers/).
- Overlapping/conflicting spans are resolved by containment + score
  (merge same-type intersecting spans, drop contained lower-score ones)
  before operators run [4]
  (https://github.com/data-privacy-stack/presidio/blob/main/presidio_anonymizer/presidio_anonymizer/anonymizer_engine.py).

### OWASP LLM guidance (T2)

- OWASP LLM02:2025 Sensitive Information Disclosure names PII leakage,
  proprietary-algorithm exposure, and sensitive-business-data disclosure
  as the vulnerability classes; prompt-level restrictions "may not always
  be honored and could be bypassed via prompt injection" -- i.e.
  prompt-based "don't echo PII" instructions are NOT a control; sanitizing
  at the boundary is [5]
  (https://owasp.org/www-project-top-10-for-large-language-model-applications/assets/PDF/OWASP-Top-10-for-LLMs-v2025.pdf,
  LLM02 section).
- OWASP's "Advanced Techniques" for LLM02 explicitly lists
  **"Tokenization and Redaction: implement tokenization to preprocess and
  sanitize sensitive information. Techniques like pattern matching can
  detect and redact confidential content before processing"** [6]
  (https://github.com/OWASP/www-project-top-10-for-large-language-model-applications/blob/main/2_0_vulns/LLM02_SensitiveInformationDisclosure.md).
  This is exactly the planned shape: regex-first detection + token swap
  before the model, restore after.
- Training-data capture is a first-class leak channel in OWASP's model:
  "Implement data sanitization to prevent user data from entering the
  training model" [5]. The repo's learning capture (loop.go:5374-5378)
  is in scope.

### Reversible pseudonymization token shape (T3)

- Vaulted tokenization (token = random surrogate, mapping store holds the
  reversible link; no algorithmic relation between token and value) is the
  PCI-blessed shape and distinct from encryption: "A token has no
  algorithmic relationship to the PAN -- the vault is the only place that
  knows the mapping, and de-tokenization is gated by audited API calls"
  [7] (https://databunker.org/use-case/credit-card-tokenization/).
- UUID tokens are the canonical opaque surrogate: 128-bit random,
  structureless, safe to store anywhere [7] [8]
  (https://docs.databunker.org/pro/get-started/pii-vault).
  Deterministic variants trade privacy for joinability; non-deterministic
  per-occurrence tokens avoid linkability entirely [8] [9]
  (https://docs.skyflow.com/docs/tokenization).
- Encryption-based reversible pseudonymization (AES-SIV deterministic,
  FPE-FFX) is the alternative family; Google's own guidance recommends
  AES-SIV over FPE unless format/length preservation is a hard
  requirement [10]
  (https://docs.cloud.google.com/sensitive-data-protection/docs/pseudonymization).
  FPE on small domains is unsafe (enumerable tokens) [11]
  (https://nhimg.org/tokenisation-and-data-masking-guide).
- Key scoping: deterministic tokens must be scoped "within a defined
  trust boundary" with per-domain salts/keys, because an estate-wide
  deterministic key "makes every tokenised copy linkable" [11] [12]
  (https://www.protecto.ai/blog/overcoming-challenges-limitations-data-tokenization/).

### Placeholder-form k-anonymity / linkability tradeoffs (T3)

- Small output domains raise inference risk: "If the output domain is
  small or predictable, the risk of inference increases" [12]
  (https://www.gigantics.io/en/blog/data-masking-techniques-dbas). The
  existing `#XXXX#` form (65,536 slots, shared with secrets) sits at the
  extreme weak end and -- worse -- collides with the secret vocabulary.
- Consistent tokens enable frequency analysis and cross-dataset linkage;
  ICO-cited guidance: "consistent tokens can link people across
  databases... so scope each deterministic key or vault namespace to one
  purpose" [11].
- Collisions on a shared map are a correctness hazard, not just privacy:
  LiteLLM's Presidio guardrail assigned the same `<PERSON_1>` to
  different people across messages (last-write-wins on a placeholder-keyed
  map), restoring the WRONG original value to the end user, and models
  that saw two indices for one value learned to emit suffix-less tokens
  that silently failed to unmask [13]
  (https://github.com/BerriAI/litellm/issues/31959). Meept's `#XXXX#`
  16-bit space has exactly this failure mode under load: two different
  secrets/PII values can mint the same placeholder, and Deobfuscate then
  substitutes whichever original iterates first (secrets.go:255-257 --
  map-iteration order, effectively random). Lesson: the swap map must be
  keyed by `(entity_type, original_value)` and be one-to-one over a
  collision-free token space (128-bit), not keyed by placeholder string
  over a 16-bit hash.

### Restore-window scoping (T3)

- Vault practice favors explicit TTL on tokens, with expiry encoded so
  restore operations reject expired tokens, and background tidying purges
  expired mappings [14]
  (https://developer.hashicorp.com/vault/docs/secrets/transform/tokenization);
  Basis Theory exposes token TTL with purge-on-expiry [15]
  (https://developers.basistheory.com/docs/concepts/what-are-tokens);
  Skyflow "transient fields" purge vault data at TTL [8].
- Volatile vs persistent alias storage is an explicit, per-class choice:
  volatile aliases expire and become unrestorable; persistent ones live
  until deleted [16] (https://docs.verygoodsecurity.com/vault/tokens).
- Detokenization is an access decision: "Every detokenisation path is an
  access decision: restrict it to named services, return partial values
  where possible, and log each call" [11].
- Auth-token lifetime doctrine (shortest workable window, hard max TTL)
  [17] (https://developer.hashicorp.com/vault/docs/concepts/tokens)
  transfers directly: the restore window should match the lifetime of the
  artifact the tokens appear in, with a hard cap.

## Leakage Classes (T4) -- surfaces this repo actually has, with closure mechanisms

Each class names the surface, why it leaks, and the closure mechanism the
implementation leaves must provide. Implementation leaves cite L-IDs.

- **L1 -- LLM egress (final replies).** Surface:
  internal/agent/loop.go:3211-3220 (ScanOutput neighborhood). Raw PII that
  entered via user context can be echoed back to the user in degraded
  form, or to logs. Closure: PII swap on inbound context + restore pass
  co-located with ScanOutput on the outbound reply (D5), so replies carry
  restored originals for the user, never raw upstream-unseen values; log
  the redacted form.
- **L2 -- conversation history stored with placeholders.** Surface:
  session store rows / conv.AddToolResult + AddUserMessage paths
  (loop.go:5369). History persists beyond the turn; placeholders in
  history must remain restorable only while their window is open.
  Closure: restore window scoped to session lifetime (D4); expired
  mappings purged (never restored).
- **L3 -- WS/bus events and RPC payloads.** Surface: typed bus topics and
  WS relay (internal/comm; docs/generated/bus-topology.md lists every
  publisher). Chat replies and progress events fan out to clients carrying
  whatever the reply contains. Closure: bus payloads inherit the reply's
  post-restore text (restore happens BEFORE publish, inside the L1
  choke point), so no event carries a stale token; no new restore call
  sites in comm code.
- **L4 -- LoRA/trace learning capture.** Surface: loop.go:5360-5379
  RecordResearch persists raw tool output. Closure: capture the
  post-swap text (tokens, not originals) OR run the PII pass over the
  captured output before persist; per OWASP, training-data ingestion is a
  named leak channel [5]. Non-negotiable: never persist originals here.
- **L5 -- auditlog rows.** Surface: internal/auditlog/redact.go (SHA-256
  output channel, key blocklist, value scrubbing). Already conservative.
  Closure: keep one-way (no restore into audit rows); the PII layer only
  asserts that no audit payload contains live PII tokens or originals --
  tokens are secrets-adjacent material and key fragments must not admit
  them.
- **L6 -- logs.** Surface: slog calls across the daemon (e.g.
  orchestrator.go:219 warns on credential detection; loop.go:3214). Logs
  are written by the same process that holds the swap map, so log lines
  could carry raw PII or token-to-original pairs. Closure: log the token
  form and entity_type counts only, never originals and never full
  pre-swap text; follows the same discipline as broker.go (names/paths
  only, never values, broker.go:63).
- **L7 -- restore-path leakage (the latent-bug class).** Surface:
  SecretObfuscator.Deobfuscate has zero production callers (see Survey
  item 2) and map-iteration-order restore would substitute wrong values
  on placeholder collision (secrets.go:255-257). Closure for the PII
  layer: (a) exactly one restore call site, host-owned, behind an
  explicit interface (no stringly-typed global map lookups);
  (b) one-to-one token map keyed by (entity_type, original_value) [13];
  (c) expired-window tokens restore to a typed tombstone
  ("<pii:expired>") rather than silently passing through; (d) an audit
  script analogous to scripts/audit-secrets-value-callers.py pinning
  restore to its allowlisted call site.

## Design Decisions (T5) -- D1..D6, traceable to research

- **D1 -- Regex-first detector layering, NER as a plug-in seam.** Default
  detector set: regex recognizers for EMAIL_ADDRESS, PHONE_NUMBER (INTL
  format), IPv4, credit-card (with Luhn check), US/intl SSN-shaped IDs,
  IBAN-shaped IDs. Each recognizer returns Presidio-shaped spans
  (entity_type, start, end, score) with containment + score conflict
  resolution before swap [1] [4]. NER (persons, places, orgs) is NOT in
  the default set: regex/deny-list coverage is insufficient for names
  [3], so the seam is a Go interface
  (`Recognize(text) ([]Span, error)`) a future NER backend (local spaCy
  sidecar, RemoteRecognizer-style per [3]) can satisfy without touching
  call sites. In-repo precedent for the recognizer/split shape:
  SecretObfuscator already layers plain and regex entries
  (secrets.go:194-217). Rationale: OWASP names pattern-matching
  redaction as the control [6]; false positives from naive NER would
  corrupt user-visible replies (usability cost per [3]).
- **D2 -- Token format: prefixed UUIDv4, typed, distinguishable.** Token
  = `<pii:EMAIL_ADDRESS:8c1e8a2b-...>`-class form -- prefix
  `<pii:` + entity_type + `:` + crypto/rand UUIDv4 + `>`. Chosen over
  the Presidio default `<ENTITY_TYPE>` form and over the existing
  `#XXXX#` form because: (a) 128-bit random space eliminates the
  collision/wrong-restore class [13]; (b) the prefix is visually distinct
  from `#XXXX#` (secret obfuscator) and `MEEPT_SECRET:` (broker twin),
  so no parser confuses the three vocabularies (Survey item 3); (c) the
  entity_type inline enables restore-side validation and tombstones.
  Vaulted, not encrypted: no algorithmic relation between token and
  value [7] [10]; non-format-preserving is acceptable because no
  downstream consumer requires PII-shaped strings (unlike PANs into
  legacy banking schemas [7]).
- **D3 -- Swap store shape: session-scoped one-to-one map.**
  Key: (session_id, entity_type, original_value) -> token; reverse index
  token -> original. One-to-one per session [13]; deterministic WITHIN a
  session (same value -> same token, so the model sees a stable entity
  across turns and history [8] [9]) but never across sessions (linkage
  scope bounded per [11] [12]). In-memory with the same lifecycle as the
  session store; no persistence of originals beyond the restore window
  (mirrors broker.go: values in memory only, broker.go:50-57).
- **D4 -- Restore window: session lifetime with a hard TTL cap and
  purge-on-expiry.** Default: mappings live for the session's lifetime,
  capped by a configurable max TTL (Vault-style encoded expiry + reject
  expired + background tidy [14] [15] [16]). Not turn-scoped: history
  re-sent on subsequent turns still contains tokens (L2), so turn
  scoping would strand them; not daemon-lifetime-scoped: unbounded
  linkage + unbounded memory [11]. Restore after expiry yields the typed
  tombstone, never a silent pass-through (L7c).
- **D5 -- Secret-#XXXX# restore interplay: restore at the ScanOutput
  choke point, PII and secrets layers strictly separated.** The PII
  restore pass runs where ScanOutput already runs (loop.go:3211-3220),
  AFTER the reply guard and refusal-fallback disclosure, BEFORE any
  publish/log/persist of the reply (L1, L3). Ordering rule: secrets
  Deobfuscate and PII restore never compose in one text pass; each layer
  recognizes only its own token vocabulary (D2's distinct prefixes make
  this structural). Secrets `#XXXX#` placeholders are NOT restored by the
  new path -- fixing the zero-restore-caller latent bug is OUT OF SCOPE
  for PII leaves and stays flagged (Survey item 2); if the secret layer
  later grows a restore, it must follow the same one-callsite +
  audit-script discipline as L7d. This decision does NOT touch
  `Broker.Value`/`resolve` and adds no new allowlist entries, so
  scripts/audit-secrets-value-callers.py discipline is intact -- PII
  values are not broker secrets and never enter the broker.
- **D6 -- Learning capture redaction (L4 closure) is mandatory, not
  best-effort.** RecordResearch (loop.go:5374-5378) receives the same
  post-swap text the model saw, or an explicit PII pass runs over the
  capture before persist. OWASP names training-data ingestion as a leak
  channel [5]; a capture path that bypasses the egress choke point
  defeats the whole layer. Trace stores are evolver-only surfaces per
  docs/agents/opt-in-defaults.md; the capture-side redaction is enforced
  at the RecordResearch boundary regardless of opt-in state.

## Sources

[1] https://github.com/microsoft/presidio/blob/main/docs/text_anonymization.md
[2] https://github.com/microsoft/presidio/blob/53e9e520/docs/anonymizer/index.md
[3] https://presidio.dataprivacystack.org/analyzer/developing_recognizers/
[4] https://github.com/data-privacy-stack/presidio/blob/main/presidio_anonymizer/presidio_anonymizer/anonymizer_engine.py
[5] https://owasp.org/www-project-top-10-for-large-language-model-applications/assets/PDF/OWASP-Top-10-for-LLMs-v2025.pdf
[6] https://github.com/OWASP/www-project-top-10-for-large-language-model-applications/blob/main/2_0_vulns/LLM02_SensitiveInformationDisclosure.md
[7] https://databunker.org/use-case/credit-card-tokenization/
[8] https://docs.skyflow.com/docs/tokenization
[9] https://www.skyflow.com/post/overcoming-the-challenges-and-limitations-of-tokenization
[10] https://docs.cloud.google.com/sensitive-data-protection/docs/pseudonymization
[11] https://nhimg.org/tokenisation-and-data-masking-guide
[12] https://www.gigantics.io/en/blog/data-masking-techniques-dbas
[13] https://github.com/BerriAI/litellm/issues/31959
[14] https://developer.hashicorp.com/vault/docs/secrets/transform/tokenization
[15] https://developers.basistheory.com/docs/concepts/what-are-tokens
[16] https://docs.verygoodsecurity.com/vault/tokens
[17] https://developer.hashicorp.com/vault/docs/concepts/tokens

## Self-Verification

- [x] Findings file at exact path docs/research/pii-unmasking-findings.md, ASCII only
- [x] Every decision cites a source (URL or file:line); leakage classes map to real surfaces
- [x] No production code modified (git status shows only this findings file)
- [x] Secret-restore interplay decision (D5) does not touch Broker.Value/resolve
      and adds no audit-script allowlist entries
