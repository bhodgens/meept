# Security + cross-boundary audit — wave 2026-11-06

- Scope: commit range `0a446a21..HEAD` — internal/security, internal/security/pii, pkg/security (incl. voice_call BuiltinRules row), internal/cluster, internal/backup, internal/session, internal/gatecapture (9e67ea34), turnjournal fix 98c85b11, 5131fc41
- HEAD: `6b42213b fix(security,agent): restore voice_call permission rows; make rewake eviction test deterministic`
- Captured: 2026-11-06T18:42:38Z
- Method: read-only; append each confirmed finding immediately.

## Findings

## F-1 [MEDIUM] gatecapture DB lands world-readable (0644) under a 0755 dir; command-gate records can carry secret-bearing argv
- `internal/gatecapture/store_sqlite.go:87-92` + `internal/gatecapture/path.go:18-21`
- Evidence:
  ```go
  //nolint:gosec // user config directory/file permissions
  if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil { ... }
  db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
  ```
  Default path is `$MEEPT_HOME/gatecapture.db` (`DefaultDBRelPath = "gatecapture.db"`). No chmod on the DB file, so sqlite creates it 0644 (umask-dependent, world-readable by default). Live proof on this host: every pre-existing `~/.meept/*.db` is `-rw-r--r--` (e.g. `audit.db`, `queue.db`), so the same default applies to gatecapture.db once a capture lands.
- Why it is a bug: the table stores `command` and `context_blob` — raw command text of gated shell actions (e.g. `curl -H "Authorization: Bearer sk-..."`, `psql 'postgres://user:pw@...'`). The serializeContext comment claims env/secret inputs are excluded, but the COMMAND TEXT itself is the secret carrier, and any local user/process can read the file. The sibling PII swap store in the same range uses `0o700` for its dir (`internal/security/pii/store.go:56`) — the repo already has the tighter convention; backup/ also uses 0700/0600 (`internal/backup/compress.go:25`, `manifest.go:48`). The `//nolint:gosec` acknowledges the deviation instead of fixing it.
- Fix shape: create the DB explicitly 0600 (OpenFile mode before sql.Open, or Chmod after first create; also chmod the WAL/shm siblings or use `file_mode=0600` DSN param of modernc/sqlite `_file_mode`), and drop dir to 0700 like pii/store. Remove the nolint.

## F-2 [LOW/INFO] DefaultOrchestratorConfig keeps EnableAuditLog=false while the gatecapture + decision-audit wave ships — audit capture is opt-in by default
- `internal/security/orchestrator.go:626-638` (relocated verbatim in this range; default unchanged: `EnableAuditLog: false, AuditDBPath: ""`)
- Evidence: the diff moved `DefaultOrchestratorConfig` to EOF byte-identically; `pkg/security/decision_audit_test.go` (+183 lines this range) proves a decision-audit surface exists that a default-constructed orchestrator does not feed.
- Why notable: per docs/agents/opt-in-defaults.md, default-off is a sanctioned product posture, so this is NOT a rule violation — but a security-decision audit chain that is off unless every call site passes an explicit config is an audit-chain gap in practice (an employee-gate denial on the default config writes nothing verifiable).
- Fix shape: either document the off-by-default posture next to decision_audit, or have the daemon wiring (not DefaultOrchestratorConfig) always pass EnableAuditLog=true; a daemon-config test pin would suffice.

## F-3 [MEDIUM] PII/secret restore fallback re-enables cross-session placeholder resolution the scoped lane was built to refuse
- `internal/agent/pii_restore_hook.go:94-97` (lane introduced/activated in 56570323; scoping in `internal/security/secrets.go` RestoreOutput)
- Evidence:
  ```go
  deobfuscated := h.obfuscator.RestoreOutput(restored, sessionID)
  if sessionID == "" || deobfuscated == restored {
      deobfuscated = h.obfuscator.Deobfuscate(restored)   // process-wide map, ignores sessionTags
  }
  ```
  `RestoreOutput` deliberately refuses placeholders tagged to ANOTHER session (secrets.go: "cross-session restore is an info leak, not a convenience"). But when it legitimately refuses — text contains only other sessions' tagged placeholders — `deobfuscated == restored` and the fallback fires `Deobfuscate`, which sweeps the whole process-wide `obfuscateMap` and resolves them. The stated intent of the fallback is only untagged pre-deploy placeholders, but its trigger is "no progress", which includes the exact leak case.
- Why it is a bug: any cross-session content transfer (agent handoff, delegate result, shared memory replay, cluster-dispatched task output) that carries another session's `#PLACEHOLDER#` into session B's output restores B's user the A-secret, defeating leaf 07 T2. The `sessionID == ""` half of the condition is likewise a full process-wide restore for a sessionless caller.
- Fix shape: make the fallback resolve ONLY untagged placeholders — e.g. add `DeobfuscateUntagged` that skips `ph` present in `s.sessionTags` — or drop the fallback entirely and expire untagged entries instead. Pin with a two-session test asserting a session-A placeholder in session-B text never restores.

## Audited-clean

- **voice_call BuiltinRules row (6b42213b / e0ab3974, pkg/security/permissions.go:90)** — RiskMedium + RequiresConfirmation:false matches the send_message precedent exactly (line 48); no gate bypassed: unknown actions deny by default, and the real outbound policy (voice.enabled, target allowlist, one-live-call-per-employee, effects reservation, audit row) lives in voice.Dialer.Dial per internal/voice/AGENTS.md C8. RequiresConfirmation is only consumed via the require_confirmation_high gate; a MEDIUM row never silences a HIGH gate for other actions. Verdict: correct as documented.
- **9e67ea34 (gatecapture gosec G202)** — LIMIT/OFFSET now bound as args, filter fragments are compile-time constants joined with strings.Join; all user values go through `?` params. Fix is genuine.
- **98c85b11 (turnjournal path fix)** — test-only; queue.db read switched to the daemon's data dir; matches components.go fallback.
- **5131fc41 (scope_ctx.go)** — context-key identity carrier; unexported key type, zero-value fail-open documented as legacy scope-blind behavior; MultiUser bit makes empty UserID fail closed at the consumer (memory.go WithScopeFilter). No traversal/auth issue in the landed file itself.
- **internal/security/secrets.go session scoping (56570323)** — the scoped lane itself is correct: RestoreOutput refuses when sessionID=="", scans only session-tagged placeholders, placeholder entropy raised 16→48 bits (fixes wrong-secret restore on collision), locks held properly (no I/O under lock beyond maps). The leak is in the CALLER fallback (F-3), not this file.
- **internal/security/agent_boundary.go** — deny-pattern scanner fails CLOSED (matches => blocked=true in block mode; monitor-mode is the documented warn-first posture); nil receiver returns not-blocked, which is the disabled-feature posture; log sample is clipped to 80 chars single-line.
- **internal/security/pii** — store dir 0700, sqlite WAL; restore logs counts + session_id only, never values; ResolveForRestore snapshots under RLock then replaces after unlock (no I/O under mutex); longest-token-first prevents prefix-shadowing wrong restore.
- **pkg/security/fence.go / groups_backstop.go diff sweep** — no new fail-open return found in the permission-check paths touched in range; dangerousCommands/financialPatterns rejections unchanged.
- **internal/cluster** — DispatchJob.signingBytes covers every execution-affecting field except Signature itself (mirror of ClusterEvent shape); executor_bridge.go verifies ed25519 with `dispatchSignatures: true` fail-closed default and rejects empty signatures; wireguard_sync.go removal is a surface reduction. Peer_liveness additions are test-only.
- **internal/backup** — 0700 dirs / 0600 manifest files, no path built from untrusted input in the changed hunks.
- **internal/session** — changed hunks are SQL/query plumbing (SaveMessagesWithToolCalls, alternate-identity capture per AGENTS.md); one added slog.Debug logs a rollback error only.
- **internal/comm/http voice + slack auth exemptions (in-scope boundary check)** — the bearer-exempt branch (server.go:2011) dispatches ONLY through HandlesPath exact-match to the two signed routes; both handlers fail closed (503) without the public key, verify signature BEFORE parse/upgrade, and the plain listener refuses non-loopback binds at startup (ensureLoopbackAddr, unparseable = refused). Slack route is registered only when wired and signature + allowed_user_ids enforced inside the handler. No auth bypass found on these paths.
- Pairing-code logging: no changed line in range logs a pairing code or dev key at Info.
- `~/.meept` spot-check: gatecapture.db not yet present locally (finding F-1 is from code, file-mode claim grounded on the sibling DBs all being 0644).

Status: FINAL (call budget reached at ~25). 3 findings (F-1 MEDIUM, F-2 LOW/INFO, F-3 MEDIUM). No commits made; only this file touched.

## Wave closeout (parent, 2026-11-06)
- F-1 CLOSED: openGateDB now 0700 dir + 0600 DB (pre-create + tighten on
  open, WAL best-effort); pinned by internal/gatecapture/store_sqlite_test.go
  (TestOpenGateDBFileModes, passes; old modes would fail both checks).
- F-2 OPEN as INFO (default flip = product decision, deferred to user).
- F-3 OPEN (parent triaging next).
