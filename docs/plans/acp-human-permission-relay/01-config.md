# Leaf 01 — [acp] human relay config (Contract A)

Parent: `master.md` Contract A. Branch: config. Single writer of config
surfaces in this tree.

## Tasks

1. RED tests (extend `internal/config/acp_config_test.go` only):
   - mode `"human"` valid; error-message case now lists four modes — update
     any existing assertion of the old verbatim message.
   - `relay_timeout <= 0` → error mentioning `relay_timeout` when mode ==
     "human" OR decision_escalate_to_human == true; accepted (ignored) when
     neither.
   - `DefaultConfig().ACP`: RelayTimeout == 120,
     DecisionEscalateToHuman == false, PermissionMode still "permissive".
2. Impl: ACPConfig gains `RelayTimeout int` + `DecisionEscalateToHuman bool`
   (json/toml tags per master); Validate() as above; DefaultConfig 120/false.
3. `config/meept.json5` [acp] comment: fourth mode + knobs
   (`relay_timeout: 120`, `decision_escalate_to_human: false` written —
   defaults, behavior unchanged).
4. docs/configuration acp knob table gains both + mode list.
5. `make docs-generate`; keep ONLY config.md delta (sibling WIP rule:
   revert foreign generated files or generate from clean HEAD worktree).

## Verify

`GOTOOLCHAIN=go1.26.5 go test -p 2 ./internal/config/ -count=1`;
`go build ./internal/config/`. Report RED + GREEN verbatim. Do NOT commit.

## Stop clause

If another [acp] field already named relay_timeout exists — report, stop.
