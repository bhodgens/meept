# Egress Secret-Injection Proxy - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Do NOT commit.
> Do NOT use read_file on existing source — search_files/terminal cat only.

## Meta

- **Parent:** ../master.md
- **Scope:** Loopback HTTP proxy that rewrites MEEPT_SECRET:<name> placeholders into real credential headers for allowlisted hosts only.
- **Dependencies:** 03-secret-broker.md (Broker.resolve, Source.Hosts/Header/Format)
- **Estimated Context:** 80K
- **Concurrency Group:** C
- **Audit references:** parity-audit gap #3 (duckagent reverse-proxy pattern, Apache-2.0 — architecture reference only, clean-room Go implementation)

## Goal

Children holding placeholders need a path where those placeholders become real credentials at the network boundary. A loopback-only reverse proxy scans outgoing requests for placeholder strings in headers and body; when the target host matches the secret's declared Hosts list, it injects Header: Format(value). Requests to non-matching hosts pass through UNMODIFIED and increment a leak-attempt counter (metric name secrets.leak_attempt). Proxy binds 127.0.0.1 with ephemeral port by default; address exposed via CLI/status for shell-profile wiring.

## Context

Go stdlib net/http/httputil.ReverseProxy suffices; no new deps. Metrics: meept has internal/metrics time-series store — follow its existing recording pattern (find via search_files "metrics.Record" usage).

Key files:
- internal/secrets/broker.go from leaf 03 (resolve unexported, same package)
- internal/metrics - recording convention
- internal/daemon/components.go - startup wiring point

## Interface Contracts (From Parent)

### Exposes

```go
// File: internal/secrets/proxy.go
package secrets

type ProxyConfig struct {
    Enabled bool   `json:"enabled" toml:"enabled"`
    Listen  string `json:"listen"  toml:"listen"` // default "127.0.0.1:0"
}

func NewProxy(b *Broker, cfg ProxyConfig, logger *slog.Logger) *Proxy

// Start binds and serves in background goroutines; ctx cancel shuts down.
func (p *Proxy) Start(ctx context.Context) (addr string, err error)

// Addr returns bound address after Start (for env/profile injection).
func (p *Proxy) Addr() string
```

Injection semantics (testable behaviors):
1. Request header VALUE containing PlaceholderPrefix+name AND host matches Source.Hosts suffix -> replace ENTIRE header value with Format-applied real value under Source.Header if different header named? NO — simpler rule: placeholder appearing in ANY request header value is replaced by Format(value) IN PLACE within that same header. Host mismatch -> untouched + leak_attempt metric.
2. Body scan limited to first 1MiB text/* or JSON content types; replacement in place.
3. Chunked bodies rejected 400 (duckagent lesson).
4. Placeholder for unknown secret name -> left untouched + metric.
5. Real values NEVER logged.

Config plumbing: [secrets.proxy] section on SecretsConfig; daemon wires when enabled; docs paragraph.

### Consumes

- Broker.resolve(name), Source fields (leaf 03)
- metrics store convention

## Tasks

### Task 1: Host matching + placeholder scanning helpers

**Files:** Create internal/secrets/proxy.go helpers + proxy_test.go
Failing tests table-driven: hostMatches("api.openai.com", ["openai.com","api.test"]) true; false cases incl. port/scheme handling via net/url parse of r.Host; scanHeader finds all placeholder occurrences w/ names. Standard cycle.

### Task 2: ReverseProxy modifier

**Files:** same files.
Failing test with httptest stub upstream: request carrying Authorization: MEEPT_SECRET:gh to stub at host matching hosts=["github.com"] receives Authorization: Bearer ghp_real (Format "Bearer {}"); mismatched-host request receives original untouched; leak_attempt recorded twice for two mismatches; unknown-secret placeholder untouched; body substitution on application/json under 1MiB works; chunked -> 400.
Standard cycle. Use Director/Rewrite func per Go version idiom present in codebase's go.mod (check go directive; go1.22+ supports Rewrite).

### Task 3: Config + daemon wiring + status exposure

**Files:** schema.go [secrets] proxy subsection; components.go start-on-boot when enabled storing addr; expose via existing runtime status RPC/CLI pattern (meept status extension) so users can wire http_proxy-ish profile snippets.
Failing component test asserting proxy addr present in components when enabled.
Docs: extend secrets doc page from leaf 03 with proxy setup + curl example through proxy.

## Self-Verification Checklist

- [ ] All Task-2 behaviors proven by httptest tests
- [ ] No value ever formatted into logs (grep logger args)
- [ ] Bind enforced loopback unless listen explicitly overridden (warn if non-loopback)
- [ ] -race green

**DO NOT COMMIT.**
**Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Injection ONLY toward declared hosts; mismatch = passthrough + metric
- [ ] Chunked rejection; 1MiB cap honored
- [ ] Graceful shutdown on ctx cancel (no leaked listeners)
- [ ] Conventions: wrapped errors, no panics in handlers

Output: APPROVED or gaps.

## Notes

- This is the newest subsystem — keep scope tight. SOCKS/TLS interception explicitly OUT of scope.
- If leaf 03 lands concurrently, coordinate on package layout only; interfaces above are frozen.
