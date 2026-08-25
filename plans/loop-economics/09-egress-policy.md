# Egress Policy (Host/CIDR allow-ask-deny) - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Extend containment-tree secrets proxy into full egress policy: host/CIDR rules, resolved-IP double-check, NO_PROXY scrubbing.
- **Deps:** plans/containment-and-computer-use leaf 04 MERGED (proxy exists) | **Context:** 55K | **Group:** D
- **Cross-tree:** reads internal/secrets/proxy.go from sibling tree.

## Goal

duckagent semantics: every outbound request from proxied children evaluated against ordered rules — host-suffix or CIDR match -> allow|ask|deny. Deny = 403 at proxy; ask = hold pending approval via existing confirmation channel (or deny-after-timeout in non-interactive contexts); resolved IPs re-checked so DNS games fail; children's NO_PROXY-family env cleared when policy active so nothing bypasses.

## Context

Proxy from containment tree intercepts child traffic already. This leaf adds a rule engine consulted BEFORE secret injection, plus env scrubbing in EnvPolicy integration point, plus ask-flow wiring.

Key files: internal/secrets/proxy.go (extend), internal/security (rule types if better housed there — decide by existing import direction; keep package deps acyclic), config schema [security.egress], internal/runtime/envpolicy.go (scrub hook).

## Interface Contracts (From Parent)

```go
// internal/security/egress.go:
type EgressRule struct {
    Match  string `json:"match"`  // host suffix ".example.com" | CIDR "10.0.0.0/8" | exact host
    Action string `json:"action"` // "allow"|"ask"|"deny"
}
type EgressConfig struct {
    Mode         string       `json:"mode"`           // "allow"(default)|"deny"| via-proxy implied when proxy enabled
    Rules        []EgressRule `json:"rules"`
    ScrubNoProxy bool         `json:"scrub_no_proxy"` // default true
}
func NewEgressPolicy(cfg EgressConfig) (*EgressPolicy, error)
func (p *EgressPolicy) Decide(host string, ips []net.IP) (action string, matched string)
// First-match-wins in declared order; no match -> Mode default action.
```

Proxy integration: before secret-injection pass, resolve r.Host -> Decide; deny -> 403 text "blocked by egress policy"; ask -> if interactive approver registered, pend w/ 30s timeout default then deny; record metrics egress.decision{action}.
EnvPolicy integration: when ScrubNoProxy && proxy active -> BuildChildEnv strips NO_PROXY/no_proxy and sets HTTP(S)_PROXY to proxy addr (coordination contract with containment tree leaf 01 — implement as post-pass func ApplyEgressEnv(env []string) []string here).

## Tasks
1. Failing tests policy: suffix vs exact vs CIDR matching precedence-by-order; IPv6; no-match default; malformed CIDR error at construction.
2. Failing proxy-integration tests: denied host 403; allowed passes through incl. secret injection still applied after allow; ask flow with fake approver approves/denies/timeouts.
3. Failing env test: scrub removes NO_PROXY variants; proxy vars set.
4. Config plumbing + docs section (extend containment docs page for secrets/proxy).

## Self-Verification Checklist
- [ ] -race green internal/security internal/secrets
- [ ] Default mode=allow => zero behavior change unless rules configured
- [ ] Ask path never blocks daemon goroutine (async pending)

## Review Checklist
- [ ] Resolution cached briefly (60s) to avoid per-request DNS storm
- [ ] Metrics namespaced consistently
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: this is deliberately the LAST networking piece — depends on merged sibling tree; do not stub proxy internals here.
