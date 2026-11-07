# Daemon keys emission - Implementation Leaf (03)

> **For the implementing agent:** Implement ALL tasks below using TDD.
> The gate tests ALREADY EXIST and are committed - make them pass; do NOT
> delete, skip, weaken, or rewrite any gate test. Do NOT commit - the
> orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once
> and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** The daemon sends the extended C1 keys frame once per call,
  built from VoiceConfig + broker secrets, immediately after registering
  a call's talk loop.
- **Dependencies:** 01 (config fields), 02 (wire shape + factory cfg)
- **Estimated Context:** 45K
- **Concurrency Group:** B

## Goal

Close the producer half of the keys gap: the child has consumed keys
frames since leaf 08, but NOTHING in the daemon ever sends one (verified
2026-11-06: no `stt_key` producer exists outside internal/voicechild),
so real providers never construct in production and every call runs on
the stub STT/TTS. After this leaf, each live call receives its keys line.

## Context

- internal/daemon/voice_wiring.go - VoiceWiring.routeFrame
  (EventSetup branch ~line 389-430) registers the talk loop; this is the
  emission point. TextServer.Send serializes writes
  (internal/voice/socket.go:175-198) and is already a TalkLoop dep - the
  keys line goes through the same sender seam.
- internal/secrets - Broker.Value(name) resolves in-process only. New
  callers MUST be added to scripts/audit-secrets-value-callers.py in the
  same commit (root AGENTS.md rule; the script fails lint-ci otherwise).
- Secrets semantics per C4/D2: deepgram_api_key + openai_api_key are
  cloud secrets; qwen3 is keyless (send ""). A missing cloud secret
  sends "" - the child factory then fails THAT call with
  ErrMissingSTTKey/ErrMissingTTSKey (child + other calls stay up,
  providers_wiring.go:82-98 behavior). Never log key material or its
  length at info+; log presence booleans at debug at most.
- Emission failure (send error): log warn, do NOT tear the call down -
  the call continues on stub audio rather than dying (degrade, not fail;
  mirrors supervisor fail-open philosophy).

## Verified Anchors

- internal/daemon/voice_wiring.go:384-400 - routeFrame EventSetup branch
  (idempotency guard, then registry lookup, then NewTalkLoop)
- internal/daemon/voice_wiring.go:117 - Broker field on VoiceWiring
- internal/daemon/voice_wiring.go:146-165 - ResolveVoiceSecrets broker
  degrade pattern (loud WARN, voice-disabled) - follow its style for a
  missing broker at emission time
- internal/voice/socket.go:175-198 - TextServer.Send
- scripts/audit-secrets-value-callers.py - allowlist table to extend

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// VoiceWiring gains (unexported method):
func (w *VoiceWiring) sendKeysFrame(ccid string) {
    // Builds the C1 raw JSON line per contract C1/C4:
    //   stt_vendor = cfg.Voice.STTProvider   (empty = "deepgram" default)
    //   tts_vendor = cfg.Voice.TTSProvider   (empty = "openai"  default)
    //   stt_model/tts_model/tts_base_url/tts_voice from cfg.Voice fields
    //   stt_key = Broker.Value("deepgram_api_key") or "" on error/absent
    //   tts_key = Broker.Value("openai_api_key")  or "" on error/absent
    // Sends via TextServer.Send ONCE per setup; send error = warn log.
}
// Emission ordering (C4): AFTER talk loop registration in routeFrame's
// EventSetup branch; duplicate setups (F-6 guard) never re-send.
```

### What This Leaf Consumes

- C1 keys wire shape (leaf 02) - build the JSON with encoding/json from
  a struct with json tags matching C1 exactly.
- Config fields (leaf 01): VoiceConfig.STTProvider/.TTSProvider/.STTModel/
  .TTSModel/.TTSBaseURL/.TTSVoice.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | keysWire struct (daemon side) + sendKeysFrame builder honoring the C4 mapping, vendor defaults when config empty | Create: internal/daemon/voice_keys.go; Modify: internal/daemon/voice_wiring.go (routeFrame emission call) | `go test ./internal/daemon/ -run TestVoiceKeysEmission` | gate T2 RED (no emission) | gate T2 passes: one keys line per setup, fields per mapping, dup setup -> still one | Given wired VoiceWiring + fake broker + recording sender; When routeFrame(setup) x2; Then exactly one keys line |
| T2 | Broker lookups with allowlist rows | Modify: internal/daemon/voice_keys.go; Modify: scripts/audit-secrets-value-callers.py | `python3 scripts/audit-secrets-value-callers.py; echo rc=$?` | new Value callers not allowlisted -> audit fails | audit rc=0; missing broker/secret -> "" sent + debug log | Given broker with/without keys; When sendKeysFrame; Then key fields = resolved values or "" |
| T3 | Send failure degrade | covered in voice_keys test | `go test ./internal/daemon/ -run TestVoiceKeysSendFail` (new test, allowed: leaf-authored unit test for a new failure mode) | n/a (new behavior test) | send error -> warn logged, call continues (no teardown call) | Given TextServer.Send returning error; When sendKeysFrame; Then no panic, no hangup |
| T4 | No key material in logs | covered by T1 assertions | `go test ./internal/daemon/ -run TestVoiceKeys` (whole group) | n/a | keys never appear in any captured log output | Given emission with fake keys; When capture logs; Then secret values absent |

## Self-Verification Checklist

- [ ] Gate test F2 passes; no gate test modified
- [ ] Emission once per call, after loop registration, dup-setup safe
- [ ] Allowlist rows added in same change; audit script green
- [ ] No key/length at info+; degrade on send error
- [ ] gofmt + `go vet ./internal/daemon/` clean

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** internal/daemon/voice_keys.go (new), voice_wiring.go, scripts/audit-secrets-value-callers.py
2. **Verify command + output:** T1/T2 commands, last lines
3. **Deviations:** any seam adjustment

## Review Checklist (For Review Agent)

- [ ] Wire JSON matches C1 field-for-field
- [ ] Vendor defaults: empty config -> deepgram/openai (preserves today's intent)
- [ ] Emission ordering + idempotency per C4
- [ ] Audit allowlist rows present; no other rows touched
- [ ] No scope creep (no emission retry logic, no caching of secrets beyond the call)

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT gate emission on voice.supervisor_enabled or any other flag.
- Do NOT send keys for calls whose setup was refused (unknown ccid path
  returns before NewTalkLoop today - keep that ordering).
- If the recording-sender seam in the gate test needs a constructor tweak
  in voice_wiring.go, keep it additive and note it in Deviations.
