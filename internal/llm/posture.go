package llm

// Data posture (Contract C4, sovereign-router-parity leaf 05, R3; source
// reference: Sovereign Router src/sensitivity.py + the registry `lane`
// plus spillway_sensitive_excluded flag).
//
// The compliance problem: meept had no way to say "this model trains on
// submitted data, so never send it this prompt." Google's free tier trains
// on data, several aggregators expose no auditable policy, and an operator
// could not express that as a routing rule. This file adds the vocabulary,
// the parse, and the FIRST filter of the C1 selection chain (selection.go):
// a sensitive turn never reaches a non-cleared model, even when a later
// filter would skip the same candidate (order is load-bearing — master C1:
// the routing log must name the BINDING constraint).
//
// IMPORT-CYCLE FALLBACK (drift-audit binding): the leaf doc's preferred
// shape imports *security.SecretScanner directly, but internal/llm CANNOT
// import internal/security — the cycle is real and verified:
//
//	internal/security -> internal/preferences -> internal/tools -> internal/llm
//
// (go list -deps ./internal/security contains internal/llm; the leaf doc
// prescribes exactly this fallback: declare a narrow SecretScanner
// interface here, satisfied STRUCTURALLY by *security.SecretScanner, and
// wire the concrete scanner from the daemon — which already imports both
// packages. The tools-side scanner instance in components.go is a separate
// instance for the file/shell tools and is untouched.)

import (
	"log/slog"
	"strings"
)

// SecretScanner is the narrow structural seam over
// internal/security.SecretScanner (see the import-cycle note above). The
// concrete *security.SecretScanner satisfies it without any adapter: the
// only member the posture gate needs is Scan. Methods return RULE NAMES —
// never the matched text — which is what makes the T9 log discipline
// structurally easy to keep.
type SecretScanner interface {
	// Scan returns the names of all rules that match the given text.
	Scan(text string) []string
}

// ParsePosture maps a config string to a Posture. Empty, unknown, and
// absent all map to PosturePrivate — the backward-compatible default
// (master Q3): a config with no data_posture key anywhere routes
// byte-identically to pre-leaf, which is the tree's flag-off control gate.
// Matching is exact (lowercase wire vocabulary, frozen in shadow.go).
func ParsePosture(s string) Posture {
	switch Posture(s) {
	case PosturePrivate, PosturePublicOnly, PostureNonSensitive:
		return Posture(s)
	default:
		return PosturePrivate
	}
}

// AllowsSensitive reports whether a SENSITIVE turn may route to a model
// carrying this posture: true ONLY for PosturePrivate (no-training,
// cleared). PosturePublicOnly (provider may train) and PostureNonSensitive
// (aggregator, posture unclear) bar sensitive turns — Sovereign Router's
// conservative posture stands (docs/ROUTING-PRINCIPLES.md rule 7): a flag
// also covers every route whose data policy is not auditable, so UNCLEAR
// is treated as BARRED, never as cleared.
func (p Posture) AllowsSensitive() bool {
	return p == PosturePrivate
}

// PosturePromptText renders the outbound prompt text the C4 posture gate
// scans: the concatenation of the request's SYSTEM and USER message text,
// in message order (leaf review checklist: only user/system text is
// scanned). Tool results are deliberately EXCLUDED — they were already
// checked at the tool boundary (internal/tools/builtin filesystem/shell
// wire the secret scanner over tool input/output); re-scanning them here
// would double-count and slow every tool turn. Assistant text is excluded
// for the same boundary reason. Empty when there is nothing to scan.
func PosturePromptText(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	for i := range messages {
		m := &messages[i]
		if m.Role != RoleUser && m.Role != RoleSystem {
			continue
		}
		if m.Content != "" {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// postureFilter is the R3 hard compliance filter — position 1 in the C1
// chain (selection.go; the registration order is FROZEN and load-bearing:
// posture must be evaluated even when the quality floor would skip the
// same candidate, so the routing log names the binding constraint).
//
// scanner carries the C4 detector; nil (flag-off default, before daemon
// wiring) leaves the filter fail-open inert — see postureSensitive.
type postureFilter struct {
	scanner SecretScanner
}

func (postureFilter) Name() string { return "posture" }

// Filter applies the frozen C4 semantics:
//
//	sensitive turn + private model      -> ALLOW
//	sensitive turn + public_only        -> SKIP ("sensitive_data_posture")
//	sensitive turn + non_sensitive      -> SKIP ("sensitive_data_posture")
//	non-sensitive turn                  -> ALLOW for every posture
//	nil scanner / empty prompt          -> treat as NOT sensitive (fail open)
//
// The fail-open rule is deliberate and must survive review: a broken or
// unwired detector must not become a denial of service. It mirrors the
// repo posture that a warmup/classifier failure never hard-fails a turn
// (internal/agent/AGENTS.md, classifier boot readiness). Scanner.Scan
// cannot error (it returns rule names), so the nil scanner is the only
// fault channel.
func (f postureFilter) Filter(c *Candidate, fc *FilterCtx) Verdict {
	if c == nil || c.Model == nil || fc == nil {
		return VerdictAllow
	}
	// Sensitivity determination — lazy, once per request. A caller may
	// pre-set fc.Sensitive (the live resolve path scans BEFORE the chain
	// runs so rotation can bypass non-cleared candidates wholesale);
	// otherwise the filter scans fc.PromptText itself. The write-back is
	// the ONE sanctioned mutation of FilterCtx (leaf contract: "set
	// fc.Sensitive"): later candidates on the same request reuse the
	// verdict instead of re-running the regex battery.
	if !fc.Sensitive && fc.PromptText != "" && f.scanner != nil {
		if rules := f.scanner.Scan(fc.PromptText); len(rules) > 0 {
			fc.Sensitive = true
			// Log discipline (T9 / Sovereign Router: "the match itself
			// is never logged"): rule NAMES and the match count only.
			// The matched text never enters a log line or a DB row.
			slog.Debug("posture: sensitive data detected in outbound prompt",
				"matches", len(rules), "rules", rules)
		}
	}
	if !fc.Sensitive {
		return VerdictAllow
	}
	if c.Model.DataPosture.AllowsSensitive() {
		return VerdictAllow
	}
	return VerdictSkip
}

// Reason is the skip reason recorded when the posture filter rejects a
// candidate. EXACT string — a cross-surface contract (routing logs and the
// e2e gate assert it).
func (postureFilter) Reason() string { return "sensitive_data_posture" }

// --------------------------------------------------------------------------
// Resolver-side seams (declared here because this leaf owns them; the
// state lives on Resolver).
// --------------------------------------------------------------------------

// SetPostureScanner wires the C4 secret scanner (daemon wiring; the narrow
// structural seam — see the SecretScanner doc). Nil is a no-op (typed-nil
// setter rule): the gate stays fail-open inert until a real scanner lands.
// Called once at daemon construction, before any request can resolve.
func (r *Resolver) SetPostureScanner(s SecretScanner) {
	if s == nil {
		return
	}
	r.mu.Lock()
	r.postureScanner = s
	r.mu.Unlock()
}

// postureSensitive determines whether promptText carries secret-shaped
// content (C4). Fail-open: no scanner wired (feature off) or empty text
// means NOT sensitive — the opt-in default keeps every existing install
// byte-identical (flag-off control gate), and a broken detector must never
// become a denial of service. The rule names are safe to log; the matched
// text never leaves the scanner.
//
// LOCKING: callers hold r.mu (the write lock on the alias resolve path),
// so this reads the field DIRECTLY — taking r.mu.RLock here would
// self-deadlock (Go's RWMutex is not reentrant; the selflock-analyzer
// class). The scanner field is written once at daemon construction
// (SetPostureScanner takes the write lock), so every call site that runs
// after wiring observes the non-nil scanner without a second lock.
func (r *Resolver) postureSensitive(promptText string) (bool, []string) {
	if promptText == "" {
		return false, nil
	}
	if r.postureScanner == nil {
		return false, nil
	}
	rules := r.postureScanner.Scan(promptText)
	if len(rules) == 0 {
		return false, nil
	}
	return true, rules
}

// postureBlocks reports whether the position-1 posture filter skips this
// candidate for the request's FilterCtx. Callers hold r.mu (both call
// sites are inside ResolveForAliasPosture's critical section). A nil or
// non-sensitive fc never blocks.
func (r *Resolver) postureBlocks(fc *FilterCtx, mc *ModelConfig) bool {
	if fc == nil || !fc.Sensitive || mc == nil {
		return false
	}
	pf := postureFilter{scanner: r.postureScanner}
	return pf.Filter(&Candidate{Model: mc}, fc) == VerdictSkip
}

// posturePass returns the index of the FIRST candidate at/after startIdx
// (wrapping) that the posture filter allows, or -1 when every candidate is
// skipped. It runs ONLY the position-1 filter — not the whole chain — so
// later filters' semantics (quality floor, admission estimate) can never
// silently change this compliance rotation as those dimensions gain live
// inputs. r.mu held by the caller.
func (r *Resolver) posturePass(alias *AliasEntry, aliasName string, fc *FilterCtx, startIdx int) int {
	n := len(alias.Models)
	if n == 0 {
		return -1
	}
	pf := postureFilter{scanner: r.postureScanner}
	for i := range n {
		idx := (startIdx + i) % n
		cand := alias.Models[idx]
		if pf.Filter(&Candidate{Model: cand, Alias: aliasName}, fc) == VerdictAllow {
			return idx
		}
	}
	return -1
}

// ResolvePostureContext is the resolve-time prompt seam (leaf 05). The
// daemon installs one closure at construction; the resolver reads it on
// the alias path to obtain the outbound prompt text for the position-1
// posture gate. Nil (test resolvers, unit suites) = no text = the gate
// fail-opens. Text is per-request scope, never persisted by the resolver,
// and NEVER logged (only scanner rule names/counts may be).
type ResolvePostureContext func(sessionID string) string

// SetResolvePostureContext installs the prompt seam. Nil is a no-op
// (typed-nil setter rule). Called once from daemon wiring.
func (r *Resolver) SetResolvePostureContext(fn ResolvePostureContext) {
	if fn == nil {
		return
	}
	r.mu.Lock()
	r.resolvePostureCtx = fn
	r.mu.Unlock()
}

// posturePromptForResolve reads the installed prompt seam. No seam, nil
// closure, or empty text yields "" — the gate treats the turn as
// non-sensitive (fail-open, the flag-off posture). LOCKING: this runs
// inside ResolveForAlias with r.mu HELD (the resolvePostureCtx field is
// read directly), so the closure itself must NOT take the Resolver lock —
// it takes only foreign locks (the agent loop's l.mu for its snapshot
// field; l.mu never takes r.mu, so no lock-order cycle). The daemon's
// closure satisfies this.
func (r *Resolver) posturePromptForResolve(sessionID string) string {
	if r.resolvePostureCtx == nil {
		return ""
	}
	return r.resolvePostureCtx(sessionID)
}
