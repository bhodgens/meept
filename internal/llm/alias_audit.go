package llm

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// alias_audit.go: boot-time + doctor-time alias reachability audit.
//
// Background (verified live outage 2026-10-23): providers with no apiKey and
// no noAuth:true are silently skipped by the daemon's buildProviderConfigs
// (Debug-level log), so alias members pointing at them are unreachable; when
// the remaining pool then quota-blocked, every rotation returned a bare
// "no providers available" while a healthy local runtime sat idle. The audit
// makes every such silent drop visible at boot (daemon wiring passes its
// ACTUAL pool membership) and at doctor time (static config predicate).

// Alias reason classes returned by AuditAliasReachability.
const (
	// AliasReasonOK means the member's provider is reachable.
	AliasReasonOK = "ok"
	// AliasReasonUnknownProvider: no provider with this id exists in
	// cfg.Providers (typo, or a dynamic OAuth provider that never
	// registered).
	AliasReasonUnknownProvider = "unknown-provider"
	// AliasReasonDisabled: the provider is listed in disabled_providers.
	AliasReasonDisabled = "disabled"
	// AliasReasonNoKeyNoAuth: the provider has neither options.apiKey nor
	// options.noAuth:true, so buildProviderConfigs skips it — the fix is
	// to add noAuth (local runtimes) or an apiKey.
	AliasReasonNoKeyNoAuth = "no-api-key-noauth"
	// AliasReasonNotRegistered: the provider passes the static predicate
	// but was absent from the daemon's actual registered pool (only
	// possible on the registered-override path).
	AliasReasonNotRegistered = "not-registered"
	// AliasReasonNoMembers marks the sentinel finding emitted for an
	// alias with zero configured members (Member/ProviderID empty).
	AliasReasonNoMembers = "no-members"
)

// AliasFinding is one audit verdict for one alias member (or the
// zero-member sentinel for an alias with no members at all).
type AliasFinding struct {
	Alias      string // alias name in model_aliases
	Member     string // "provider/model" as configured (empty on the sentinel)
	ProviderID string // provider part of Member (empty on the sentinel)
	Reachable  bool
	Reason     string // one of the AliasReason* classes
}

// AliasFixHint returns the operator-facing fix for a reason class. It names
// the concrete config change, never a vague "check your config".
func AliasFixHint(reason string) string {
	switch reason {
	case AliasReasonOK:
		return ""
	case AliasReasonUnknownProvider:
		return "add the provider to models.json5 or fix the alias member spelling"
	case AliasReasonDisabled:
		return "remove the provider from disabled_providers"
	case AliasReasonNoKeyNoAuth:
		return "add noAuth or apiKey to the provider options"
	case AliasReasonNotRegistered:
		return "provider was not registered in the daemon pool at boot (see the buildProviderConfigs skip logs)"
	case AliasReasonNoMembers:
		return "add at least one provider/model member to the alias"
	default:
		return "inspect the alias member and its provider block"
	}
}

// AuditAliasReachability checks every model_aliases member against the
// registration rules the daemon's buildProviderConfigs applies: the provider
// exists in cfg.Providers, is not in cfg.DisabledProviders, and carries
// options.apiKey or options.noAuth:true.
//
// registered semantics: when non-nil it OVERRIDES the static predicate per
// provider — the daemon passes its ACTUAL pool membership (provider ids from
// buildProviderConfigs plus dynamically OAuth-registered ids), so a provider
// the static config cannot see (OAuth) still counts as reachable, and a
// provider the static predicate would pass but the pool skipped does not.
// A registered-override miss keeps the static reason when one explains the
// drop, else AliasReasonNotRegistered. When registered is nil the pure
// static predicate decides (the doctor path, which runs offline).
//
// Findings are ordered by alias name (sorted) and then by member order
// within the alias. An alias with zero members yields ONE sentinel finding
// (Reason AliasReasonNoMembers, empty Member/ProviderID) so loggers and
// doctor can still surface it.
func AuditAliasReachability(cfg *ProvidersConfig, registered map[string]bool) []AliasFinding {
	if cfg == nil {
		return nil
	}

	aliases := make([]string, 0, len(cfg.ModelAliases))
	for name := range cfg.ModelAliases {
		aliases = append(aliases, name)
	}
	sort.Strings(aliases)

	disabled := make(map[string]bool, len(cfg.DisabledProviders))
	for _, p := range cfg.DisabledProviders {
		disabled[p] = true
	}

	var findings []AliasFinding
	for _, alias := range aliases {
		entry := cfg.ModelAliases[alias]
		if len(entry.Models) == 0 {
			findings = append(findings, AliasFinding{
				Alias:  alias,
				Reason: AliasReasonNoMembers,
			})
			continue
		}
		for _, member := range entry.Models {
			providerID := member
			if i := strings.Index(member, "/"); i >= 0 {
				providerID = member[:i]
			}
			f := AliasFinding{
				Alias:      alias,
				Member:     member,
				ProviderID: providerID,
			}
			staticReason := staticAliasReachabilityReason(cfg, disabled, providerID)
			if registered != nil {
				if registered[providerID] {
					f.Reachable = true
					f.Reason = AliasReasonOK
				} else {
					// Keep the explanatory static reason when the
					// static predicate also fails; otherwise the
					// pool simply never registered it.
					if staticReason == AliasReasonOK {
						staticReason = AliasReasonNotRegistered
					}
					f.Reason = staticReason
				}
			} else {
				f.Reachable = staticReason == AliasReasonOK
				f.Reason = staticReason
			}
			findings = append(findings, f)
		}
	}
	return findings
}

// staticAliasReachabilityReason mirrors the buildProviderConfigs
// registration rules against the pure config (no pool state).
func staticAliasReachabilityReason(cfg *ProvidersConfig, disabled map[string]bool, providerID string) string {
	provider, ok := cfg.Providers[providerID]
	if !ok {
		return AliasReasonUnknownProvider
	}
	if disabled[providerID] {
		return AliasReasonDisabled
	}
	if provider.Options.APIKey == "" && !provider.Options.NoAuth {
		return AliasReasonNoKeyNoAuth
	}
	return AliasReasonOK
}

// LogAliasAudit reports audit findings: ONE Info line per alias summarizing
// (alias, members, reachable), ONE Warn per unreachable member naming the
// alias, member, provider, reason, and the concrete fix, and an extra Warn
// for any alias with zero reachable members ("alias X has no reachable
// members; turns resolving it will fail"). The zero-member sentinel finding
// is not counted as a member. A nil logger falls back to slog.Default.
func LogAliasAudit(logger *slog.Logger, findings []AliasFinding) {
	if logger == nil {
		logger = slog.Default()
	}

	// Group per alias, preserving the (sorted) first-appearance order.
	type aliasGroup struct {
		members   int
		reachable int
		bad       []AliasFinding
		noMembers bool
	}
	order := make([]string, 0)
	groups := make(map[string]*aliasGroup)
	for _, f := range findings {
		g, ok := groups[f.Alias]
		if !ok {
			g = &aliasGroup{}
			groups[f.Alias] = g
			order = append(order, f.Alias)
		}
		if f.Reason == AliasReasonNoMembers {
			g.noMembers = true
			continue
		}
		g.members++
		if f.Reachable {
			g.reachable++
		} else {
			g.bad = append(g.bad, f)
		}
	}

	for _, alias := range order {
		g := groups[alias]
		logger.Info("alias reachability audit",
			"alias", alias,
			"members", g.members,
			"reachable", g.reachable,
		)
		for _, f := range g.bad {
			logger.Warn("alias member unreachable",
				"alias", f.Alias,
				"member", f.Member,
				"provider", f.ProviderID,
				"reason", f.Reason,
				"fix", AliasFixHint(f.Reason),
			)
		}
		if g.noMembers || (g.members > 0 && g.reachable == 0) {
			logger.Warn(fmt.Sprintf("alias %s has no reachable members; turns resolving it will fail", alias),
				"alias", alias,
				"members", g.members,
				"reachable", g.reachable,
			)
		}
	}
}
