package secrets

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Task 1: policy matching ------------------------------------------------

func TestEgressPolicy_HostSuffixMatch(t *testing.T) {
	p, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "example.com", Action: EgressDeny},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]string{
		"example.com":      EgressDeny,
		"api.example.com":  EgressDeny,
		"example.com:8080": EgressDeny,
		"EXAMPLE.COM":      EgressDeny,
		"notexample.com":   EgressAllow,
		"example.com.evil": EgressAllow,
	} {
		got, _ := p.Decide(host, nil)
		if got != want {
			t.Errorf("Decide(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestEgressPolicy_CIDRMatch(t *testing.T) {
	p, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "10.0.0.0/8", Action: EgressDeny},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Decide("anything.internal", []net.IP{net.ParseIP("10.1.2.3")}); got != EgressDeny {
		t.Errorf("CIDR private match = %q, want deny", got)
	}
	if got, _ := p.Decide("public.example", []net.IP{net.ParseIP("93.184.216.34")}); got != EgressAllow {
		t.Errorf("non-matching CIDR = %q, want allow", got)
	}
}

func TestEgressPolicy_IPv6CIDR(t *testing.T) {
	p, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "fc00::/7", Action: EgressDeny},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.Decide("v6host", []net.IP{net.ParseIP("fd12::1")}); got != EgressDeny {
		t.Errorf("IPv6 ULA = %q, want deny", got)
	}
	if got, _ := p.Decide("v6pub", []net.IP{net.ParseIP("2606:4700::1111")}); got != EgressAllow {
		t.Errorf("IPv6 public = %q, want allow", got)
	}
}

func TestEgressPolicy_PrecedenceByOrder(t *testing.T) {
	p, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "api.example.com", Action: EgressAllow},
		{Match: ".example.com", Action: EgressDeny},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, matched := p.Decide("api.example.com", nil); got != EgressAllow || matched != "api.example.com" {
		t.Errorf("first-match-wins: action=%q matched=%q, want allow/api.example.com", got, matched)
	}
	if got, _ := p.Decide("other.example.com", nil); got != EgressDeny {
		t.Errorf("second rule = %q, want deny", got)
	}
}

func TestEgressPolicy_NoMatchDefaults(t *testing.T) {
	rules := []PolicyRule{{Match: "example.com", Action: EgressDeny}}
	for _, tc := range []struct{ mode, want string }{
		{EgressModeProxy, EgressAllow},
		{EgressModeAllow, EgressAllow},
		{EgressModeDeny, EgressDeny},
		{"", EgressAllow},
	} {
		p, err := NewEgressPolicy(tc.mode, rules, true)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := p.Decide("unrelated.test", nil); got != tc.want {
			t.Errorf("mode %q no-match default = %q, want %q", tc.mode, got, tc.want)
		}
	}
	// mode=deny blocks even hosts that would otherwise be allowed by rules
	// order... actually rules still win first; verify a non-matching host is
	// denied and the matching one too.
	p, _ := NewEgressPolicy(EgressModeDeny, rules, true)
	if got, _ := p.Decide("unrelated.test", nil); got != EgressDeny {
		t.Errorf("mode=deny default = %q, want deny", got)
	}
}

func TestEgressPolicy_UnknownActionError(t *testing.T) {
	if _, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{{Match: "a.com", Action: "maybe"}}, true); err == nil {
		t.Error("unknown action: want error, got nil")
	}
	if _, err := NewEgressPolicy("bogus", nil, true); err == nil {
		t.Error("invalid mode: want error, got nil")
	}
	if _, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{{Match: "10.0.0.0/33", Action: EgressAllow}}, true); err == nil {
		t.Error("malformed CIDR: want error, got nil")
	}
	if _, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{{Match: "", Action: EgressAllow}}, true); err == nil {
		t.Error("empty match: want error, got nil")
	}
}

// --- Task 2: proxy integration ----------------------------------------------

func stubHost(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

// resolveFunc lets tests fake DNS for the rebinding defense path.
func (p *EgressPolicy) setLookup(f func(string) ([]string, error)) { p.lookupHost = f }

func TestProxyIntegration_DenyBlocks403(t *testing.T) {
	stub := startStub(t, echoAuthHandler)
	b := newTestBroker(t)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")
	policy, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "evil.example", Action: EgressDeny},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	// Fake resolution so CheckAndResolve doesn't hit real DNS.
	policy.setLookup(func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	})
	p := NewProxy(b, ProxyConfig{}, newDiscardLogger())
	p.rewriteTransport = &stubTransport{target: stubHost(stub)}
	p.policy = policy
	addr := startProxy(t, context.Background(), p)

	resp := do(t, p, "evil.example", "Authorization", "Bearer MEEPT_SECRET:"+testTokenName, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied host status = %d, want 403", resp.StatusCode)
	}
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(strings.ToLower(string(body[:n])), "blocked by egress policy") {
		t.Errorf("deny body %q missing 'blocked by egress policy'", body[:n])
	}
	if policy.Decisions(EgressDeny) == 0 {
		t.Error("egress.decision deny metric not recorded")
	}
	_ = addr
}

func TestProxyIntegration_AllowStillInjectsSecrets(t *testing.T) {
	b := newTestBroker(t)
	setSource(b, []string{"github.com"}, "Authorization", "Bearer {}")
	policy, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "github.com", Action: EgressAllow},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	policy.setLookup(func(host string) ([]string, error) {
		return []string{"140.82.121.4"}, nil // github public IP
	})
	stub := startStub(t, echoAuthHandler)
	tr := &stubTransport{target: stubHost(stub)}
	p := NewProxy(b, ProxyConfig{}, newDiscardLogger())
	p.rewriteTransport = tr
	p.policy = policy
	startProxy(t, context.Background(), p)

	resp := do(t, p, "github.com", "Authorization", Placeholder(testTokenName), "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allowed host status = %d, want 200", resp.StatusCode)
	}
	if got := tr.lastHeader("Authorization"); got != "Bearer "+testTokenVal {
		t.Errorf("injected auth = %q, want real value after allow", got)
	}
}

func TestProxyIntegration_RebindingToPrivateIPRejected(t *testing.T) {
	b := newTestBroker(t)
	setSource(b, []string{"corp.example"}, "Authorization", "Bearer {}")
	// Public host allowed by name, but resolves into a denied private CIDR:
	// the resolved-IP double-check must reject it.
	policy, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
		{Match: "corp.example", Action: EgressAllow},
		{Match: "10.0.0.0/8", Action: EgressDeny},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	policy.setLookup(func(host string) ([]string, error) {
		return []string{"10.9.9.9"}, nil // rebinding target: private
	})
	stub := startStub(t, echoAuthHandler)
	p := NewProxy(b, ProxyConfig{}, newDiscardLogger())
	p.rewriteTransport = &stubTransport{target: stubHost(stub)}
	p.policy = policy
	startProxy(t, context.Background(), p)

	resp := do(t, p, "corp.example", "Authorization", "Bearer MEEPT_SECRET:"+testTokenName, "", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("rebound host status = %d, want 403", resp.StatusCode)
	}
}

func TestProxyIntegration_AskFlowApproveDenyTimeout(t *testing.T) {
	cases := []struct {
		name     string
		approve  bool
		useAppr  bool
		wantCode int
	}{
		{"approved", true, true, http.StatusOK},
		{"denied", false, true, http.StatusForbidden},
		{"no approver denies", false, false, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBroker(t)
			setSource(b, []string{"ask.example"}, "Authorization", "Bearer {}")
			policy, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
				{Match: "ask.example", Action: EgressAsk},
			}, true)
			if err != nil {
				t.Fatal(err)
			}
			policy.askTimeout = 50 * time.Millisecond
			policy.setLookup(func(string) ([]string, error) { return []string{"93.184.216.34"}, nil })
			var calls atomic.Int32
			if tc.useAppr {
				policy.SetApprover(func(ctx context.Context, host string) bool {
					calls.Add(1)
					return tc.approve
				})
			}
			stub := startStub(t, echoAuthHandler)
			p := NewProxy(b, ProxyConfig{}, newDiscardLogger())
			p.rewriteTransport = &stubTransport{target: stubHost(stub)}
			p.policy = policy
			startProxy(t, context.Background(), p)

			resp := do(t, p, "ask.example", "Authorization", "Bearer MEEPT_SECRET:"+testTokenName, "", "")
			resp.Body.Close()
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("ask(%s) status = %d, want %d", tc.name, resp.StatusCode, tc.wantCode)
			}
			if tc.useAppr && calls.Load() != 1 {
				t.Errorf("approver called %d times, want 1", calls.Load())
			}
		})
	}
	t.Run("timeout resolves deny without blocking forever", func(t *testing.T) {
		policy, err := NewEgressPolicy(EgressModeProxy, []PolicyRule{
			{Match: "slow.example", Action: EgressAsk},
		}, true)
		if err != nil {
			t.Fatal(err)
		}
		policy.askTimeout = 30 * time.Millisecond
		policy.setLookup(func(string) ([]string, error) { return []string{"93.184.216.34"}, nil })
		block := make(chan struct{})
		policy.SetApprover(func(ctx context.Context, host string) bool {
			<-block
			return true
		})
		t.Cleanup(func() { close(block) })

		action, _ := policy.Decide("slow.example", nil)
		if action != EgressDeny {
			t.Errorf("timed-out ask = %q, want deny", action)
		}
	})
}

// --- Task 3: env scrubbing ---------------------------------------------------

func TestApplyEgressEnv_ScrubsNoProxyAndSetsProxyVars(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"NO_PROXY=internal.corp",
		"no_proxy=localhost,.local",
		"http_proxy=http://old:3128",
	}
	got := ApplyEgressEnv(env, "127.0.0.1:9999", true)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "NO_PROXY") || strings.Contains(joined, "no_proxy") {
		t.Errorf("NO_PROXY survived scrubbing: %s", joined)
	}
	for _, want := range []string{
		"HTTP_PROXY=http://127.0.0.1:9999",
		"http_proxy=http://127.0.0.1:9999",
		"HTTPS_PROXY=http://127.0.0.1:9999",
		"https_proxy=http://127.0.0.1:9999",
		"PATH=/usr/bin",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("env missing %q:\n%s", want, joined)
		}
	}
}

func TestApplyEgressEnv_NoScrubNoChange(t *testing.T) {
	env := []string{"NO_PROXY=x", "PATH=/bin"}
	got := ApplyEgressEnv(env, "127.0.0.1:9999", false)
	if len(got) != len(env) {
		t.Errorf("scrub=false changed env: %v -> %v", env, got)
	}
	got = ApplyEgressEnv(env, "", true)
	if len(got) != len(env) {
		t.Errorf("proxyAddr=\"\" changed env: %v -> %v", env, got)
	}
}
