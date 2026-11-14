# Leaf 05 — Lane Pools + Config Surface

> **DISPATCH INSTRUCTION:** Implement this leaf end-to-end with TDD. Work in
> the isolation worktree (path given in your dispatch context). Do NOT run
> `git commit` or `git add` — the orchestrator commits after review. Do NOT commit. Run the
> exact verify commands at the bottom; report results + file list.

## Meta

- **Parent:** orchestrator.md (phase1-clips)
- **Depends on:** 00T gate tests committed (TESTS_VALIDATED)
- **Estimated context:** ~50K
- **Parallel group:** Wave A (with 01; file-disjoint)
- **Requirements:** R2, R5 (config half), C5 contract

## Goal

Make filler-class text pools lane-aware and enumerable, per root Contract.

## Scope

Make filler-class text pools lane-aware and enumerable, per root Contract
C5. Today: `voice.filler` list (schema.go:5486), `voice.greeting` SINGLE
STRING (schema.go:5485), `voice.silence_prompt` string (schema.go:5601),
`voice.fragment_clarify` string. Target: five lanes, each an enumerable
line set, so leaf 02 can pre-render all of them.

Facts to respect (verified, do not re-derive):
- Filler already round-robins over the whole list (`talkloop.go:549-553`
  via `fillerIdx`); what is missing is enumeration and lane identity, not
  multi-line support.
- Filler and greeting are GLOBAL-only today: `voice_wiring.go:1008-1009`
  passes `w.cfg.Filler` / `w.cfg.GreetingLine`, and `ResolveAcoustics`
  (`internal/voice/profile.go:127-170`) carries no such fields. Per-employee
  filler text pools are OUT of scope for this tree; state that in the report
  rather than half-wiring it.

## Tasks

1. `internal/config/schema.go` — add list fields per C5:
   `GreetingLines []string`, `SilencePromptLines []string`,
   `FragmentClarifyLines []string`, `FarewellLines []string`. Keep the
   existing scalars untouched for back-compat.
   - `FarewellLines` schema default MUST be EMPTY (nil). A non-empty default
     would make every existing deployment start speaking a goodbye, which
     violates the repository's pinned "zero-config = byte-identical legacy"
     rule (internal/voice/AGENTS.md). Ship the three lines
     (`["Goodbye!","Bye!","Talk soon."]`) in `config/meept.json5` instead,
     so fresh installs get the feature and existing configs opt in.
   - `GreetingLines` empty ⇒ fall back to today's rule (greeting scalar, then
     the first filler line).
   Validation mirrors the existing per-employee tri-state rules (negative/
   nil handling like schema.go:5746's pattern). Per-employee overrides: add
   the `_lines` keys to the `employee_leg` row struct
   (schema.go ~5818-5839 region) with the same tri-state semantics.
2. `config/meept.json5` — document + ship the new keys next to the
   existing filler block (config/meept.json5:1358-1381 area), same comment
   style (lowercase inline comments).
3. `internal/voice/lanes.go` (new file, package voice):
   ```go
   type Lane string
   const (LaneGreeting Lane = "greeting"; LaneFiller = "filler"; LaneSilence = "silence";
          LaneFragment = "fragment"; LaneFarewell = "farewell")
   type LaneLine struct { Lane Lane; Text string }
   // VoiceLanes resolves (row > global > default, the existing tri-state
   // precedence) and returns the DEDUPLICATED set of every line any
   // filler-class lane can speak this session. Greeting empty -> first
   // filler line (today's rule preserved).
   func VoiceLanes(cfg *config.Config, employeeID string) []LaneLine
   ```
   Plus `PickLine(lanes []LaneLine, lane Lane, cursor *int) string` —
   round-robin within a lane (same semantics as today's fillerIdx).
4. `internal/configui/sections_voice.go` — surface the new fields in the
   voice config section (list fields: the comma-joined editor pattern the
   existing `Filler` field uses — copy it).

## Interface Contract (exposes to siblings)

`voice.LaneLine{Lane,Text}` / `voice.VoiceLanes(cfg, employeeID)` / `voice.PickLine` —
consumed by leaves 02 (render set) and 04 (farewell lane), and by
talkloop wiring.

## Self-Verification Checklist

- [ ] `go build ./internal/config/... ./internal/voice/...`
- [ ] `go test -p 2 ./internal/config/... ./internal/voice/... -count=1` green
- [ ] Precedence table tests: row list > row scalar > global list > global
      scalar > default (write the table test; it is the C5 contract).
- [ ] Empty-list default for the two `_lines` keys ⇒ scalar keeps binding
      (byte-identical existing behavior when new keys unset).
- [ ] `FarewellLines` defaults EMPTY in `DefaultConfig`: a config built
      without the template speaks no farewell (test asserts the lane is
      empty), while `config/meept.json5` ships the three lines.
- [ ] `gofmt -l` empty.

## Review Checklist (for the orchestrator)

- [ ] No changes to internal/voice/talkloop.go in this leaf (wave-C
      territory).
- [ ] config-bootstrap drift check passes (`make config-bootstrap`
      semantics: new keys land in template AND schema).
- [ ] TUI/GUI parity honored per AGENTS.md: configui fields added; note in
      report if the Flutter GUI voice settings screen needs a matching
      follow-up (it reads the same configui section if wired that way —
      verify, then state).
