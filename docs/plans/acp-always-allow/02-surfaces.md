# Leaf 02 — surfaces + docs (Contract B)

Parent: `master.md` Contract B. Depends leaf 01 (read committed API).
Branch: tui + flutter + docs.

Tasks per Contract B:
- TUI: `a` key -> resolve "always", conditional footer when head offers
  allow_always (options key from event); extend modal_flows_test cases.
- GUI: third "always" button iff options has allow_always-kind entry
  (case-insensitive kind=="allow_always"); provider passes decision;
  widget tests in existing dialog test family; dart format first.
- Docs: workflows/acp.md (surfaces table + relay-decision text +
  semantics paragraph: meept echoes agent's own optionId, persists
  nothing); rpc.md/http-api.md/cli.md decision values add "always";
  features.md ACP row clause; parent
  docs/plans/acp-human-permission-relay/master.md non-goal note gains a
  SUPERSEDED pointer line to this tree (append-only edit).
Verify: tui sweep; flutter dialog+provider tests; dart format check;
docs freshness untouched (handwritten pages). Do NOT commit.
