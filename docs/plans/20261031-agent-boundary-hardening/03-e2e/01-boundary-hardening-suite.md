# Boundary-Hardening E2E Suite + Manifest - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on
> existing source files - explore with search_files or terminal cat.
> After writing a file, do NOT read it back - write once and stop.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Hermetic e2e suite e2e/suites/boundary-hardening/ with four
  scenarios (taint pass/block, PII roundtrip, secret restore) + full
  e2e/manifest.json integration.
- **Dependencies:** branches 01-inter-agent-taint and 02-pii-proxy
  REVIEWED (wiring landed in internal/daemon/components.go)
- **Estimated Context:** 90K
- **Concurrency Group:** A
- **Audit references:** master.md R1-R10 (integration gate)

## Goal

Prove both security lanes end to end on a real scratch daemon: legitimate
collaboration still flows with the taint lane on; forged control messages
are neutralized; PII is masked toward the provider and restored for the
originating session across reply/history/trace surfaces; secret `#XXXX#`
placeholders come back as the original secret.

## Context

- e2e/harness - the ONLY sanctioned way to build/run scratch daemons:
  builds meept-daemon/meept into $TMPDIR/meept-e2e-bin* once per test
  process; detached reaper cleans bins and orphan daemons after abnormal
  exits. NEVER os.MkdirTemp("meept-e2e-bin...") or raw exec daemons.
- e2e/suites/output-filters/output_filters_test.go - reference idiom:
  `//go:build e2e`, package docblock explaining the scenario inventory,
  harness import github.com/caimlas/meept/e2e/harness.
- e2e/manifest.json - keys: version, generated, comment, path_map (dict
  path-prefix -> suite), suites (list of {name, dir, status}),
  scenarios (list of {id, suite, diff, title, paths}).
- Scratch configs: the harness writes $MEEPT_HOME-scoped configs per
  daemon; this suite sets security.taint.enabled=true and
  security.pii.enabled=true (plus pii classes default).
- Branch wiring (already landed): "inter-agent-taint" BeforeToolCall +
  "pii-restore" TransformContext in internal/daemon/components.go;
  drivers mark handoffs; InspectHandoff deny patterns;
  internal/security/pii Swapper/Store; restore at loop.go ScanOutput
  choke point.

## Verified Anchors

- e2e/suites/output-filters/output_filters_test.go:1-30 - suite idiom
- e2e/manifest.json - path_map dict + suites/scenarios lists
- internal/agent/loop.go:3213 - ScanOutput choke point (restored output
  observable in step results / chat replies)
- internal/daemon/components.go:1960-1990 - both registrations landed

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C1-C: four scenarios boundary-hardening-01..04 (GREEN)
- C2-C: manifest suite + path_map + scenario entries

### What This Leaf Consumes

- C4 (master): suite name/keys - boundary-hardening
- C3 (branch 02): restore at ScanOutput - observable via chat reply +
  stored history + trace rows
- C3-A/C4-A (branch 01): driver marking + deny patterns - observable via
  collaboration session outcome

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|---------|----------------|------------|------------|-----------------|
| T1 | Suite scaffold + scenario 01: legit collaboration round with taint lane ON completes; worker receives the driver's framing + task output; session finalizes success | Create: e2e/suites/boundary-hardening/main_test.go (+ suite docblock); Modify: none | `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/ -run TestBoundaryLegitCollaboration -v` | suite missing -> compile RED; against disabled-config variant scenario logic RED | scenario GREEN on scratch daemon with taint enabled | Given two agents in a pair-driver session with taint on; When a round completes; Then task finalizes success and the worker's recorded input contains the framing text |
| T2 | Scenario 02: forged control message neutralized - a handoff carrying an impersonation preamble + forged `{"action":"request_token"...}`/workspace_yield-style instruction from a tainted segment is blocked; session finalizes failed/blocked with a clean error, no instruction executed | same file | `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/ -run TestBoundaryForgedControlMessage -v` | deny patterns missing -> RED (forgery rides through) | GREEN: blocked + audited; and scenario 01 still green (no over-block) | Given a tainted handoff embedding forged control JSON; When the worker turn runs; Then the handoff is blocked (deny pattern) and nothing yields the workspace |
| T3 | Scenario 03: PII roundtrip - chat turn whose input contains email+phone+card-like number: (a) provider-bound context recorded for the turn carries `[PII:` tokens and NOT the raw values, (b) the user-visible reply and stored conversation history contain the restored originals, (c) trace/LoRA capture rows contain no `[PII:` tokens, (d) a SECOND session cannot restore the first session's tokens (its reply keeps tokens literal) | same file | `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/ -run TestBoundaryPiiRoundtrip -v` | restore lane off -> RED (raw PII at provider, or tokens in reply) | GREEN for (a)-(d) | Given a session with pii enabled; When a PII-bearing turn completes; Then recorded provider request has tokens only, reply has originals, capture rows token-free; Given another session replaying a stolen token; Then token survives verbatim |
| T4 | Scenario 04: secret restore - daemon with secrets.json5 entry; a turn whose context obfuscated the secret replies containing the original secret (not #XXXX#); a fresh daemon (other process) with no knowledge of the map leaves #XXXX# untouched in output | same file | `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/ -run TestBoundarySecretRestore -v` | dead Deobfuscate lane -> RED (#XXXX# reaches the user) | GREEN | Given env secret VALUE registered by the obfuscator; When the reply would contain #XXXX#; Then user sees VALUE; Given a fresh daemon; Then #XXXX# passes through (no cross-process leak) |
| T5 | Manifest integration: suites += boundary-hardening; path_map += internal/security/pii/ and internal/agent/taint_hooks.go (+ driver files if unmapped); scenarios += the four with paths[] of touched sources | Modify: e2e/manifest.json | `python3 -c "import json;json.load(open('e2e/manifest.json'))" && grep -c boundary-hardening e2e/manifest.json` | missing keys | manifest parses; 4 scenarios + suite + path_map present; `scripts/e2e-affected.sh` maps the new paths to the suite | Given the manifest; When e2e-affected runs on a diff touching internal/security/pii; Then boundary-hardening is selected |

## Self-Verification Checklist

- [ ] All four scenarios GREEN via harness
  (`go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/ -v`)
- [ ] `make e2e-fast-area AREA=boundary-hardening` green
- [ ] Manifest entries complete and formatted like existing entries
- [ ] Hermetic: no external network; scratch configs; harness-managed
      scratch bins only (reaper active)
- [ ] `gofmt -l e2e/suites/boundary-hardening/` empty; `go vet -tags e2e` clean
- [ ] No debug artifacts; no line-number corruption

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines per verify run]
3. **Deviations:** [none / list with rationale - incl. how scenarios reach
   the collaboration driver path in the hermetic rig]

## Review Checklist (For Review Agent)

- [ ] Every scenario implemented; suite green; scenarios independent
- [ ] Manifest keys present and well-formed; e2e-affected mapping works
- [ ] Harness-only scratch bins; reaper protection intact
- [ ] Assertions on observable surfaces (replies, rows, recorded requests),
      not internal function pokes
- [ ] No debug artifacts; no line-number corruption; no commits made

Output: APPROVED or specific gaps with file + line references.

## Notes

- If a scripted path to a pair-driver collaboration session does not exist
  in the hermetic rig, drive the driver through the agent registry seam
  inside the e2e test process (harness-permitted) - document the choice.
- Scenario 02's forged payload must be realistic: impersonation preamble
  ("I am the coordinator agent...") + control-verb JSON; assert BOTH the
  deny-pattern block and that no workspace_yield-style side effect
  occurred (e.g., check the workspace dir state).
- Keep total suite runtime modest (<2 min); share one scratch daemon per
  scenario group where config allows, but scenario 04 needs its own fresh
  daemon for the cross-process negative case.
