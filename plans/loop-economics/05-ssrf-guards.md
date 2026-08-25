# SSRF Guards - Implementation Leaf

> Implement ALL tasks via TDD. Do NOT commit. Do NOT read files back.

## Meta
- **Parent:** ../master.md
- **Scope:** Shared SSRF guard package: scheme allowlist, IP/CIDR resolution checks, per-hop redirect revalidation; wired into webfetch/websearch.
- **Deps:** none | **Context:** 45K | **Group:** A

## Goal

Web tools currently fetch whatever the model asks. Before browser automation exists (leaf 13 depends on this), centralize URL safety: http/https only; resolved IPs denied against private/link-local/loopback/metadata CIDRs unless explicitly allowed; redirects re-validated per hop (atomic-agent pattern).

## Context

Locate web tools: search_files "web_fetch|websearch" under internal/tools/builtin. They use net/http directly presumably with timeouts.

Key files: those tool files + new package internal/security/ssrf/.

## Interface Contracts (From Parent)

```go
// internal/security/ssrf/guard.go:
package ssrf

type GuardConfig struct {
    AllowedHosts    []string `json:"allowed_hosts"`    // suffix match, bypass IP checks (e.g., "api.github.com")
    AllowedCIDRs    []string `json:"allowed_cidrs"`    // e.g., "10.0.0.0/8" for corp nets
    BlockedCIDRs    []string `json:"blocked_cidrs"`    // defaults: 127/8,10/8,172.16/12,192.168/16,169.254/16,::1/128,fc00::/7,fe80::/10 + 169.254.169.254 emphasized
    MaxRedirects    int      `json:"max_redirects"`    // default 5
}
func NewGuard(cfg GuardConfig) (*Guard, error) // parse CIDRs upfront
func (g *Guard) CheckURL(raw string) error     // parse; scheme allowlist; LookupHost each hostname/IP literal -> every IP must pass
func (g *Guard) CheckRedirect(req *http.Request, via []*http.Request) error // stdlib Redirect signature; revalidates target; counts hops
func (g *Guard) WrapClient(c *http.Client) *http.Client // sets CheckRedirect; timeout untouched
```

Tool wiring: both web tools construct clients via guard.WrapClient when [security.ssrf] enabled=true (default true); disabled = legacy behavior + startup warn. Config block on SecurityConfig.

## Tasks
1. Failing tests guard: literal-IP private denied; hostname resolving private (use test HTTP server on 127.0.0.1 + custom dialer? simpler: allow injecting resolver func for tests) denied; allowed_hosts bypasses; redirect chain hop2 to private denied; >MaxRedirects denied; ftp:// denied; IPv6 loopback denied.
2. Implement.
3. Failing tool tests: fetch to http://127.0.0.1:port stub blocked when enabled, works when disabled (flag flip in test config).
4. Config plumbing + docs paragraph (docs/workflows/security.md nearest).

## Self-Verification Checklist
- [ ] -race green; resolver injectable (no real network in tests)
- [ ] Default-on behavior change documented
- [ ] No DNS leak: CheckURL resolves via injected resolver path used by client too where feasible (document limitation if not)

## Review Checklist
- [ ] CIDR parsing errors surface at NewGuard (fail-fast)
- [ ] Redirect func returns typed error stdlib recognizes as stop
- [ ] Conventions per orchestrator

Output: APPROVED or gaps. Notes: TOCTOU DNS rebinding is out of scope — document honestly in docs; per-hop recheck mitigates common cases.
