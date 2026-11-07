# TUI voice session (part 2, optional) - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit — the orchestrator handles git. Explore with search_files or
> terminal cat, never read_file. Do NOT read back files you just wrote.
> GATE: do not dispatch until the user answers master.md OQ1 (TUI-without-
> GUI-parity deviation approved, or cut this leaf).

## Meta

- **Parent:** master.md
- **Scope:** A `/voice` command in the TUI that starts, monitors, and stops
  a local voice session for the current employee, reusing the RPC control
  plane and the client audio shim from leaf 08.
- **Dependencies:** leaf 08 (RPC + voice.RawMicCapture/SpeakerSink)
- **Estimated Context:** 70K
- **Concurrency Group:** F (optional, part 2)
- **Files this leaf owns:** internal/tui (command registration, a
  voice-session model/component file), internal/tui client config keys if
  a default employee label is needed

## Goal

From the TUI: `/voice eve` starts a local session (status line shows live
call + leg), `/voice stop` ends it, `/voice list` shows sessions. Audio
routing uses leaf 08's shim - no new audio code. The Flutter GUI does not
get parity in this leaf; that deviation is documented per the parity rule
(see master.md OQ1) with a follow-up issue opened by leaf 12's docs pass.

## Tasks

| Task ID | Objective | Files | Verify | RED reason | PASS |
|---|---|---|---|---|---|
| T1 | Command registration + help text (lowercase UI rule) | internal/tui/commands.go, handlers | go build ./internal/tui/ && the repo's tui command-table test if one exists | no command | PASS |
| T2 | Session state: RPC open on start, status line rendering from voice.local.status poll or push, stop via RPC end, teardown on app quit | internal/tui/models/voice.go (new), app.go hook | go test -p 2 ./internal/tui/ -run TestVoiceModel (fake RPC) | no model | PASS |
| T3 | Audio shim reuse smoke: sink/capture start-stop lifecycle embedded in the model behind a fake | voice.go | same | - | PASS |
| T4 | gofmt/vet; deviation note recorded (file comment + docs pointer) | - | go vet ./internal/tui/... | - | clean |

## Self-Verification Checklist

- [ ] No dart/Flutter files touched (deviation is deliberate, not accidental)
- [ ] Keybinding does not collide (grep keymap registry)

## Review Checklist

- [ ] Parity deviation documented in docs/workflows/voice.md (leaf 12)
- [ ] No audio code duplicated from leaf 08
