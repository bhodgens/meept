package llm

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// aliasAuditTestConfig builds a ProvidersConfig covering every reason class:
//   - local: reachable via NoAuth on a loopback URL
//   - keyed: reachable via apiKey
//   - disabled: listed in DisabledProviders
//   - nokey: neither apiKey nor noAuth (the 2026-10-23 silent-skip shape)
//   - (ghost: referenced by an alias but absent from Providers)
func aliasAuditTestConfig() *ProvidersConfig {
	return &ProvidersConfig{
		DisabledProviders: []string{"disabled"},
		ModelAliases: map[string]ModelAliasEntry{
			"coder": {Models: []string{"local/llama", "keyed/gpt"}},
			"planner": {
				Models: []string{"disabled/m", "nokey/m", "ghost/m"},
			},
			"empty": {Models: []string{}},
		},
		Providers: map[string]ProviderConfig{
			"local": {Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:8080/v1", NoAuth: true}},
			"keyed": {Options: ProviderOptionsConfig{BaseURL: "https://api.example.test/v1", APIKey: "***"}},
			"disabled": {Options: ProviderOptionsConfig{
				BaseURL: "https://d.example.test/v1", APIKey: "***",
			}},
			"nokey": {Options: ProviderOptionsConfig{BaseURL: "https://n.example.test/v1"}},
		},
	}
}

func aliasFindingByMember(t *testing.T, findings []AliasFinding, alias, member string) AliasFinding {
	t.Helper()
	for _, f := range findings {
		if f.Alias == alias && f.Member == member {
			return f
		}
	}
	t.Fatalf("no finding for alias=%q member=%q in %+v", alias, member, findings)
	return AliasFinding{}
}

// TestAuditAliasReachability_StaticPredicate pins the static (registered=nil)
// predicate: one reason class per registration rule the daemon's
// buildProviderConfigs applies.
func TestAuditAliasReachability_StaticPredicate(t *testing.T) {
	findings := AuditAliasReachability(aliasAuditTestConfig(), nil)

	cases := []struct {
		alias, member string
		reachable     bool
		reason        string
	}{
		{"coder", "local/llama", true, AliasReasonOK},
		{"coder", "keyed/gpt", true, AliasReasonOK},
		{"planner", "disabled/m", false, AliasReasonDisabled},
		{"planner", "nokey/m", false, AliasReasonNoKeyNoAuth},
		{"planner", "ghost/m", false, AliasReasonUnknownProvider},
	}
	for _, c := range cases {
		f := aliasFindingByMember(t, findings, c.alias, c.member)
		if f.Reachable != c.reachable || f.Reason != c.reason {
			t.Errorf("%s/%s: got reachable=%v reason=%q, want reachable=%v reason=%q",
				c.alias, c.member, f.Reachable, f.Reason, c.reachable, c.reason)
		}
		wantProvider := strings.SplitN(c.member, "/", 2)[0]
		if f.ProviderID != wantProvider {
			t.Errorf("%s/%s: ProviderID = %q, want %q", c.alias, c.member, f.ProviderID, wantProvider)
		}
	}

	// Zero-member alias gets the sentinel finding.
	sentinel := aliasFindingByMember(t, findings, "empty", "")
	if sentinel.Reason != AliasReasonNoMembers || sentinel.Reachable {
		t.Errorf("empty alias sentinel = %+v, want reason %q unreachable", sentinel, AliasReasonNoMembers)
	}

	// nil config is safe.
	if got := AuditAliasReachability(nil, nil); got != nil {
		t.Errorf("AuditAliasReachability(nil) = %+v, want nil", got)
	}

	// The no-key fix hint names the concrete config change.
	if hint := AliasFixHint(AliasReasonNoKeyNoAuth); !strings.Contains(hint, "noAuth") || !strings.Contains(hint, "apiKey") {
		t.Errorf("no-key fix hint = %q, want it to name noAuth and apiKey", hint)
	}
}

// TestAuditAliasReachability_RegisteredOverride pins the override semantics:
// a non-nil registered map is the daemon's ACTUAL pool membership and flips
// verdicts per provider — an unknown-provider member (e.g. a dynamically
// OAuth-registered provider absent from the static config) becomes
// reachable, and a static-pass provider missing from the pool does not.
func TestAuditAliasReachability_RegisteredOverride(t *testing.T) {
	cfg := aliasAuditTestConfig()
	// "ghost" is in the real pool (OAuth-registered); "local" was skipped
	// at boot despite passing the static predicate.
	registered := map[string]bool{"ghost": true, "keyed": true}

	findings := AuditAliasReachability(cfg, registered)

	ghost := aliasFindingByMember(t, findings, "planner", "ghost/m")
	if !ghost.Reachable || ghost.Reason != AliasReasonOK {
		t.Errorf("registered override: ghost = %+v, want reachable/ok", ghost)
	}
	local := aliasFindingByMember(t, findings, "coder", "local/llama")
	if local.Reachable || local.Reason != AliasReasonNotRegistered {
		t.Errorf("registered override: local = %+v, want unreachable/not-registered", local)
	}
	// A provider failing BOTH the static predicate and the override keeps
	// the explanatory static reason.
	nokey := aliasFindingByMember(t, findings, "planner", "nokey/m")
	if nokey.Reachable || nokey.Reason != AliasReasonNoKeyNoAuth {
		t.Errorf("registered override: nokey = %+v, want unreachable/no-api-key-noauth", nokey)
	}
}

// captureLogs runs fn under a JSON slog handler and returns the decoded
// records (level + msg + attrs).
func captureLogs(t *testing.T, fn func(logger *slog.Logger)) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fn(logger)

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

// TestLogAliasAudit_EmitsInfoAndWarns pins the log contract: ONE Info per
// alias summarizing members/reachable, ONE Warn per unreachable member
// naming alias, member, provider, reason, and fix, and the extra Warn for
// aliases with no reachable members at all.
func TestLogAliasAudit_EmitsInfoAndWarns(t *testing.T) {
	findings := AuditAliasReachability(aliasAuditTestConfig(), nil)
	records := captureLogs(t, func(logger *slog.Logger) { LogAliasAudit(logger, findings) })

	infoByAlias := map[string]map[string]any{}
	var memberWarns, deadAliasWarns []map[string]any
	for _, rec := range records {
		level, _ := rec["level"].(string)
		msg, _ := rec["msg"].(string)
		switch {
		case level == "INFO" && msg == "alias reachability audit":
			alias, _ := rec["alias"].(string)
			infoByAlias[alias] = rec
		case level == "WARN" && msg == "alias member unreachable":
			memberWarns = append(memberWarns, rec)
		case level == "WARN" && strings.Contains(msg, "has no reachable members"):
			deadAliasWarns = append(deadAliasWarns, rec)
		}
	}

	// Per-alias INFO lines: coder (2/2), planner (0/3), empty (sentinel).
	if len(infoByAlias) != 3 {
		t.Fatalf("got %d alias INFO lines, want 3: %v", len(infoByAlias), infoByAlias)
	}
	coder := infoByAlias["coder"]
	if coder["members"].(float64) != 2 || coder["reachable"].(float64) != 2 {
		t.Errorf("coder INFO = members %v reachable %v, want 2/2", coder["members"], coder["reachable"])
	}
	planner := infoByAlias["planner"]
	if planner["members"].(float64) != 3 || planner["reachable"].(float64) != 0 {
		t.Errorf("planner INFO = members %v reachable %v, want 3/0", planner["members"], planner["reachable"])
	}

	// One WARN per unreachable member: planner's disabled/nokey/ghost.
	if len(memberWarns) != 3 {
		t.Fatalf("got %d member WARN lines, want 3", len(memberWarns))
	}
	for _, w := range memberWarns {
		for _, key := range []string{"alias", "member", "provider", "reason", "fix"} {
			if s, _ := w[key].(string); s == "" {
				t.Errorf("member WARN missing %s: %v", key, w)
			}
		}
		if w["alias"].(string) != "planner" {
			t.Errorf("member WARN alias = %v, want planner", w["alias"])
		}
	}

	// Dead-alias WARNs: planner (0 reachable) + empty (no members).
	if len(deadAliasWarns) != 2 {
		t.Fatalf("got %d 'no reachable members' WARN lines, want 2 (planner + empty)", len(deadAliasWarns))
	}

	// Nil logger must not panic (falls back to slog.Default).
	LogAliasAudit(nil, findings)
	// Nil/empty findings are safe.
	LogAliasAudit(slog.Default(), nil)
}
