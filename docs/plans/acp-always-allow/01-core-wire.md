# Leaf 01 — core + wire (Contract A)

Parent: `master.md` Contract A. Branch: acp+bus+rpc+http+cli+e2e.

Tasks: per master Contract A, TDD-first. Key checks:
1. First VERIFY fakeagent permission params shape (does it already send
   `options`? resolveAllowOptionID existence implies some legs expect
   parseable options — read session_test.go + acp-wire tests to see what
   raw params the harness sends today; keep every existing expectation
   green).
2. Options cap 16; event payload additive; decision "always" threaded:
   Relay.Resolve -> relayDecision{allow:true, always:true} -> reply echo
   resolveAlwaysOptionID fallback-chain (always->allow_once->default).
3. RPC/HTTP/CLI validators + docs comments; reason stays "answered".
4. Tests in existing files only; e2e: new scenario human-mode resolve
   "always" -> assert fakeagent received selected + the allow_always
   optionId (fakeagent may need to echo the received reply for assertion —
   extend it minimally, backward-compatible).
Verify: acp/bus/rpc/comm http/daemon sweeps; e2e acp-wire x2;
go vet; gofmt. Do NOT commit.
