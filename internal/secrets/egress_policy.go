package secrets

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Egress decision actions.
const (
	EgressAllow = "allow"
	EgressAsk   = "ask"
	EgressDeny  = "deny"
)

// Egress modes for [security.egress].
const (
	EgressModeAllow = "allow" // passthrough unchanged (default)
	EgressModeDeny  = "deny"  // all proxied egress blocked
	EgressModeProxy = "proxy" // current behavior + rules consulted pre-injection
)

// DefaultAskTimeout bounds how long an "ask" decision may pend before it is
// resolved as deny. Keeps a non-responsive approver from wedging requests.
const DefaultAskTimeout = 30 * time.Second

// EgressDecisionMetricName is the canonical metric family counting egress
// policy verdicts, keyed by action ("allow"/"ask"/"deny").
const EgressDecisionMetricName = "egress.decision"

// PolicyRule is one ordered egress rule: Match is either a host suffix
// (e.g. ".example.com" or "example.com"; bare hosts are exact matches) or a
// CIDR (e.g. "10.0.0.0/8") checked against resolved destination IPs.
type PolicyRule struct {
	Match  string `json:"match"  toml:"match"`
	Action string `json:"action" toml:"action"`
}

// EgressApprover resolves an "ask" verdict. It returns true to allow, false
// to deny. Implementations must respect ctx cancellation.
type EgressApprover func(ctx context.Context, host string) bool

// EgressPolicy evaluates outbound destinations against ordered allow/ask/deny
// rules. Host rules match exact or dot-suffix; CIDR rules match any resolved
// IP. First matching rule wins; no match falls back to the mode's default
// action.
//
// DNS-rebinding defense: callers resolve the destination host themselves and
// pass the IPs to Decide; a host whose name-based match allows but whose
// resolved address falls in a denied CIDR is denied.
type EgressPolicy struct {
	mode         string
	scrubNoProxy bool
	rules        []compiledRule

	mu        sync.Mutex
	decisions map[string]int64

	lookupHost func(host string) ([]string, error) // injectable for tests
	approver   EgressApprover
	askTimeout time.Duration
}

type compiledRule struct {
	host   string     // lowercased, trailing dot stripped; empty if CIDR
	cidr   *net.IPNet // non-nil if CIDR
	action string
}

// NewEgressPolicy builds a policy. mode is "" (treated as allow), "allow",
// "deny", or "proxy". Malformed rules (unknown action, empty match, bad CIDR)
// return an error so misconfiguration fails at startup, not at request time.
func NewEgressPolicy(mode string, rules []PolicyRule, scrubNoProxy bool) (*EgressPolicy, error) {
	switch mode {
	case "", EgressModeAllow, EgressModeDeny, EgressModeProxy:
	default:
		return nil, fmt.Errorf("egress policy: invalid mode %q (want allow, deny, or proxy)", mode)
	}
	p := &EgressPolicy{
		mode:         mode,
		scrubNoProxy: scrubNoProxy,
		decisions:    make(map[string]int64),
		lookupHost:   net.LookupHost,
		askTimeout:   DefaultAskTimeout,
	}
	for i, r := range rules {
		switch r.Action {
		case EgressAllow, EgressAsk, EgressDeny:
		default:
			return nil, fmt.Errorf("egress policy: rule[%d] match %q: invalid action %q (want allow, ask, or deny)", i, r.Match, r.Action)
		}
		if strings.TrimSpace(r.Match) == "" {
			return nil, fmt.Errorf("egress policy: rule[%d]: empty match", i)
		}
		cr := compiledRule{action: r.Action}
		if _, _, err := net.ParseCIDR(r.Match); err == nil {
			// ParseCIDR accepts non-masked forms; canonicalize via netip so
			// "10.0.0.5/8" style entries fail loudly instead of silently
			// masking to the network address.
			prefix, perr := netip.ParsePrefix(r.Match)
			if perr != nil {
				return nil, fmt.Errorf("egress policy: rule[%d]: invalid CIDR %q: %w", i, r.Match, perr)
			}
			cr.cidr = &net.IPNet{
				IP:   prefix.Addr().AsSlice(),
				Mask: net.CIDRMask(prefix.Bits(), prefix.Addr().BitLen()),
			}
		} else if strings.ContainsAny(r.Match, "/:") && !strings.Contains(r.Match, ":") || strings.Contains(r.Match, "/") {
			return nil, fmt.Errorf("egress policy: rule[%d]: invalid CIDR %q: %w", i, r.Match, err)
		} else {
			h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(r.Match, ".")), "."))
			if h == "" {
				return nil, fmt.Errorf("egress policy: rule[%d]: empty match", i)
			}
			cr.host = h
		}
		p.rules = append(p.rules, cr)
	}
	return p, nil
}

// SetApprover registers the interactive approval hook used for "ask"
// verdicts. Nil means asks resolve as deny (non-interactive contexts).
func (p *EgressPolicy) SetApprover(a EgressApprover) { p.approver = a }

// ScrubNoProxy reports whether NO_PROXY-family env vars should be cleared for
// children when the proxy is active.
func (p *EgressPolicy) ScrubNoProxy() bool { return p.scrubNoProxy }

// Mode returns the configured mode ("" normalized to allow).
func (p *EgressPolicy) Mode() string {
	if p.mode == "" {
		return EgressModeAllow
	}
	return p.mode
}

// Decisions reports the egress.decision counter for an action.
func (p *EgressPolicy) Decisions(action string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.decisions[action]
}

func (p *EgressPolicy) record(action string) {
	p.mu.Lock()
	p.decisions[action]++
	p.mu.Unlock()
}

// Decide evaluates host (with optional :port) against the ordered rules using
// ips as the resolved addresses for CIDR rules. Returns the action and the
// matched rule pattern ("" when the mode default applied). The returned
// action for an unresolved "ask" is already resolved via the approver.
func (p *EgressPolicy) Decide(host string, ips []net.IP) (action, matched string) {
	action, matched = p.evaluate(host, ips)
	if action == EgressAsk {
		action = p.resolveAsk(host)
	}
	p.record(action)
	return action, matched
}

func (p *EgressPolicy) evaluate(host string, ips []net.IP) (string, string) {
	h := normalizeHost(host)
	action := ""
	matched := ""
	for _, cr := range p.rules {
		if cr.cidr != nil {
			for _, ip := range ips {
				if cr.cidr.Contains(ip) {
					action, matched = cr.action, cr.cidr.String()
					break
				}
			}
			if action != "" {
				break
			}
			continue
		}
		if h != "" && (h == cr.host || strings.HasSuffix(h, "."+cr.host)) {
			action, matched = cr.action, cr.host
			break
		}
	}
	// DNS-rebinding defense: even when a host rule allowed the request,
	// re-check every resolved IP against deny-CIDR rules; a denied CIDR wins
	// so public-looking hosts resolving into private space are rejected.
	if action != EgressDeny {
		for _, cr := range p.rules {
			if cr.cidr == nil || cr.action != EgressDeny {
				continue
			}
			for _, ip := range ips {
				if cr.cidr.Contains(ip) {
					return EgressDeny, cr.cidr.String()
				}
			}
		}
	}
	if action != "" {
		return action, matched
	}
	// No rule matched: mode default. proxy mode permits (rules consulted
	// pre-injection); deny blocks everything; allow passes through.
	switch p.Mode() {
	case EgressModeDeny:
		return EgressDeny, ""
	default:
		return EgressAllow, ""
	}
}

// resolveAsk consults the registered approver under the ask timeout. No
// approver, timeout, or false all resolve to deny. Never blocks indefinitely.
func (p *EgressPolicy) resolveAsk(host string) string {
	if p.approver == nil {
		return EgressDeny
	}
	timeout := p.askTimeout
	if timeout <= 0 {
		timeout = DefaultAskTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- p.approver(ctx, normalizeHost(host)) }()
	select {
	case ok := <-done:
		if ok {
			return EgressAllow
		}
		return EgressDeny
	case <-ctx.Done():
		return EgressDeny
	}
}

// CheckAndResolve performs the full request-time check: normalizes host,
// resolves DNS (unless ips were supplied), and decides. It exists so the
// proxy does the resolved-IP double-check (DNS rebinding defense) in one call.
func (p *EgressPolicy) CheckAndResolve(dest string) (action, matched string, ips []net.IP, err error) {
	h := normalizeHost(dest)
	if h == "" {
		return EgressDeny, "", nil, fmt.Errorf("egress policy: empty destination host")
	}
	addrs, lerr := p.lookupHost(h)
	if lerr != nil {
		return "", "", nil, fmt.Errorf("egress policy: resolve %q: %w", h, lerr)
	}
	ips = make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil {
			ips = append(ips, ip)
		}
	}
	action, matched = p.Decide(dest, ips)
	return action, matched, ips, nil
}

// normalizeHost strips port and trailing dot, lowercases. Empty stays empty.
func normalizeHost(raw string) string {
	if raw == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(raw); err == nil {
		raw = h
	} else if strings.Count(raw, ":") > 1 {
		// Bare IPv6 literal without port.
		if ip := net.ParseIP(raw); ip != nil {
			return strings.ToLower(ip.String())
		}
	}
	return strings.ToLower(strings.TrimSuffix(raw, "."))
}

// ApplyEgressEnv post-passes a child environment: when the egress proxy is
// active and scrubbing is enabled, NO_PROXY/no_proxy are removed (so nothing
// bypasses the proxy) and HTTP_PROXY/HTTPS_PROXY point children at the proxy.
func ApplyEgressEnv(env []string, proxyAddr string, scrub bool) []string {
	if proxyAddr == "" || !scrub {
		return env
	}
	out := make([]string, 0, len(env)+4)
	set := map[string]string{
		"HTTP_PROXY":  "http://" + proxyAddr,
		"http_proxy":  "http://" + proxyAddr,
		"HTTPS_PROXY": "http://" + proxyAddr,
		"https_proxy": "http://" + proxyAddr,
	}
	for _, kv := range env {
		key, _ := splitEnvKey(kv)
		if strings.EqualFold(key, "NO_PROXY") || strings.EqualFold(key, "no_proxy") {
			continue // scrubbed so traffic actually flows through the proxy
		}
		if _, overridden := set[key]; !overridden {
			out = append(out, kv)
		}
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}

// splitEnvKey splits "KEY=rest" into key and value-presence.
func splitEnvKey(kv string) (key string, hasValue bool) {
	i := strings.IndexByte(kv, '=')
	if i < 0 {
		return kv, false
	}
	return kv[:i], true
}
