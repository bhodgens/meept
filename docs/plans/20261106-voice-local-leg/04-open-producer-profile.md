# Daemon open producer + profile resolver - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.
> RECONCILE FIRST: tierA leaf 03 (global keys emission at EventSetup) must
> be committed. This leaf WRAPS that emission in a per-call resolver and
> adds the open-frame send; it does not fork the keys wire.

## Meta

- **Parent:** master.md
- **Scope:** (a) Send the C1 `open` frame from the daemon right after the
  AudioPair exists (phone leg); (b) internal/voice/profile.go:
  ResolveProfile per CL4 precedence; (c) make tierA's keys emission call
  ResolveProfile instead of reading global config directly.
- **Dependencies:** leaf 01, tierA 03
- **Estimated Context:** 55K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voice/profile.go (new),
  internal/voice/profile_test.go (new), internal/voice/frame.go (alias if
  needed), internal/daemon/voice_wiring.go (send site + keys content call)

## Goal

Phone leg arms itself: pair -> open -> (child setup -> loop registers ->
keys built FROM ResolveProfile for that call's employee). The user gets
per-agent voice/leg selection on both legs through one resolver; the
defaults reproduce today's single-vendor config byte-exactly.

## Context

Key files:
- internal/daemon/voice_wiring.go - spliceRunner() creates the AudioPair
  (~"audio, err := voice.NewAudioPair") then calls RunSplice. Send open
  between those. rawFrameSender{srv: w.TextServer} already exists for raw
  lines (tierA used the same seam for keys - confirm and reuse).
- tierA leaf 03 emission point (routeFrame EventSetup, after loop
  registration): swap its config read for ResolveProfile(cfg,
  employeeID-of-this-call, "", "phone").
- Config from leaf 01: DefaultLeg, Local block, EmployeeLeg list.
- Employee for the call: inbound = cfg.DefaultEmployee path / registry
  instance; outbound = dialer's employeeID. Both reachable at emission.

## Interface Contracts (From Parent)

CL1 open shape (verbatim):
`{"call_id":"cc1","event":"open","from":"+15550001111","to":"+15552223333","direction":"outbound","codec":"L16","sample_rate":16000}`
(phone: local=false/absent, barge_in_rms absent).
CL4 resolver (verbatim):
```
type Profile struct { SttVendor, SttModel, TtsVendor, TtsModel, TtsVoice string }
func ResolveProfile(cfg config.VoiceConfig, employeeID, ttsVoiceOverride, leg string) Profile
```
Precedence: override > voice.employee_leg[employeeID] > per-leg config
(Local.* for "local", top-level for "phone") > ProviderConfig defaults.
Leg: arg > employee_leg.leg > DefaultLeg. BargeInRms comes straight from
voice.local.barge_in_rms (put it on the open frame for local calls).
Keys line maps Profile fields onto tierA C1 keys names verbatim; secrets
still via Broker only for cloud vendors; a missing cloud secret sends ""
(tierA contract). Local vendors send "" (tierA D2).

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | ResolveProfile precedence-table tests (override/employee/per-leg/default chains, empty-chain fallback = today) | profile_test.go | go test -p 2 ./internal/voice/ -run TestResolveProfile | no file | PASS |
| T2 | Test: spliceRunner sends byte-exact open before RunSplice completes (fake text sink captures) | voice_wiring_test.go | go test -p 2 ./internal/daemon/ -run TestVoiceOpenSend | no producer | PASS |
| T3 | Test: keys line content follows the CALL's employee (two employees, different employee_leg entries -> different tts_voice bytes) | voice_wiring_test.go | -run TestVoiceKeysPerEmployee | keys read global config | PASS |
| T4 | Implement open send + resolver call swap at tierA's site | voice_wiring.go, profile.go | T1-T3 | - | PASS |
| T5 | gofmt/vet + secrets audit script green (no new Broker.Value callers expected; add if any) | - | go vet ./internal/voice/... ./internal/daemon/...; python3 scripts/audit-secrets-value-callers.py | - | clean |

## Interface Contract (What This Leaf Exposes)

voice.Profile + voice.ResolveProfile (leaves 07,09 consume), the open-send
helper `w.sendOpen(ccid, direction, from, to, local, bargeInRms)`.

## Self-Verification Checklist

- [ ] tierA keys tests pass unmodified except content expectations
- [ ] Default config boot sends keys byte-identical to tierA's output
- [ ] Open send failure logs + continues (no call death storm)

## Review Checklist

- [ ] Byte-exact tierA keys field names (no renames!)
- [ ] No secret logging (log capture in T3)
- [ ] Local fields absent on phone frames (omitempty proof)
- [ ] ASCII only; no artifacts
