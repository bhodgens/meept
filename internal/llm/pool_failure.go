package llm

import (
	"strings"
	"time"
)

// Pool-scoped failure taxonomy (Contract C5, sovereign-router-parity leaf
// 06; frozen table: Sovereign Router src/pools.py:176-206).
//
// Every upstream failure classifies into one FAILURE CLASS at one FAILURE
// SCOPE, and the resolver's block memory keys on (scope, target). A 401
// blocks the whole ACCOUNT (sibling models on the same provider are dead
// too); a 400/413/422 is the CALLER's fault and blocks nothing.
//
// The body excerpt passed to ClassifyFailure is pattern-matched for signal
// phrases and then DISCARDED — it is never stored, never logged, never
// reaches a struct field (pools.py classify_failure docstring: "keeps only
// the resulting class/scope").

// FailureCategory is the CLASS dimension of the frozen taxonomy. (Named
// FailureCategory, not FailureClass: failure_policy.go already owns the
// int-enum FailureClass for the client retry policy; master C5's type name
// collided with it. The gate pins the Class* constant VALUES, not the type
// name.)
type FailureCategory string

const (
	ClassAccountDead  FailureCategory = "account_dead"  // 401 / 402 / 403
	ClassCongested    FailureCategory = "congested"     // 429
	ClassTransient    FailureCategory = "transient"     // 5xx, timeout, transport
	ClassRequestFault FailureCategory = "request_fault" // 400 / 413 / 422
	ClassEntryMissing FailureCategory = "entry_missing" // 404
)

// FailureScope is the SCOPE dimension: what the failure implicates.
type FailureScope string

const (
	ScopeModel   FailureScope = "model"   // this model only
	ScopePool    FailureScope = "pool"    // the shared upstream quota pool
	ScopeAccount FailureScope = "account" // every model on the credential
	ScopeRequest FailureScope = "request" // the caller's fault: blocks NOTHING
)

// Frozen cooldown bases (pools.py:222, adapted to meept's defaults per
// leaf 06): account death is a longer-lived fact than meept's 30s alias
// timeout default; pool exhaustion clears on a shorter horizon.
const (
	poolCooldownAccount      = 3600 * time.Second // 1h
	poolCooldownPool         = 900 * time.Second  // 15m
	poolCooldownModel        = 60 * time.Second
	poolCooldownTransient    = 30 * time.Second
	poolCooldownEntryMissing = 3600 * time.Second
	// poolCooldownCap: no block exceeds 4h (pools.py _MAX_COOLDOWN_S).
	poolCooldownCap = 4 * time.Hour
)

// FailureCooldown returns the frozen base cooldown for a class, clamped to
// the 4h cap (pools.py:222 + _MAX_COOLDOWN_S).
func FailureCooldown(cls FailureCategory) time.Duration {
	base := poolCooldownTransient
	switch cls {
	case ClassAccountDead:
		base = poolCooldownAccount
	case ClassCongested:
		base = poolCooldownPool
	case ClassEntryMissing:
		base = poolCooldownEntryMissing
	case ClassRequestFault:
		// A request fault arms nothing; the zero duration is a guard, not
		// a schedule (callers must not arm on request_fault at all).
		return 0
	default: // ClassTransient and anything unmapped
		base = poolCooldownTransient
	}
	return min(base, poolCooldownCap)
}

// PoolIDFor returns the pool a provider's models draw from. v1 (master Q4):
// ONE pool per provider, named "provider:<id>". The models.json5 `pool`
// field (ModelDef.Pool) is reserved so a future config can map two
// providers onto one shared pool without a migration; an empty declared
// pool keeps a provider pool-less (opt-in gate — ScopePool never fires).
func PoolIDFor(providerID string) string {
	return "provider:" + providerID
}

// openrouterInFlightBody reports the OpenRouter in-flight concurrency
// accounting shape (pools.py _openrouter_in_flight): a 402 whose body names
// limit_source with an in-flight marker is TRANSIENT concurrency accounting,
// never account death.
func openrouterInFlightBody(body string) bool {
	return strings.Contains(body, "limit_source") &&
		(strings.Contains(body, "in_flight") || strings.Contains(body, "in-flight"))
}

// PoolExhaustionSignal reports whether a 429 body implicates the whole
// POOL rather than one model. The two observed shapes (pools.py
// _pool_exhaustion_signal, :161-174): Cloudflare's daily-free-allocation
// error 4006, and OpenRouter's per-day free-pool limit phrasing. The 4006
// marker is matched provider-agnostic in meept's v1: pools are
// config-declared, not taxonomy-derived, so there is no provider→kind
// table to gate the marker on. Bare 429s stay model-scoped.
//
// bodyExcerpt is inspected and discarded — never stored, never logged.
func PoolExhaustionSignal(providerID string, bodyExcerpt string) bool {
	_ = providerID // reserved for the future provider→pool-kind mapping (Q4)
	if bodyExcerpt == "" {
		return false
	}
	if strings.Contains(bodyExcerpt, "4006") {
		return true
	}
	low := strings.ToLower(bodyExcerpt)
	for _, phrase := range []string{
		"free-models",
		"per-day",
		"per day",
		"daily limit",
		"daily free allocation",
	} {
		if strings.Contains(low, phrase) {
			return true
		}
	}
	return false
}

// ClassifyFailure is the frozen classification table (pools.py:176-206,
// verbatim; master Contract C5 — FROZEN 5-arg shape with poolID in the
// THIRD position). providerID/modelID identify the failed attempt, poolID
// the provider's declared pool ("" = no pool declared: the opt-in gate —
// a 429 pool-phrase still classifies congested but stays MODEL-scoped, so
// ScopePool never fires without configuration). bodyExcerpt is a BOUNDED
// body preview used only for signal-phrase matching and then discarded.
//
// Returns (class, scope, target): target is the block-memory key — modelID
// for model scope, poolID for pool scope, providerID for account scope,
// "-" for request scope (which blocks nothing).
func ClassifyFailure(providerID string, modelID string, poolID string, status int, bodyExcerpt string) (FailureCategory, FailureScope, string) {
	// Transport failure / no HTTP status: transient at model scope.
	if status == 0 {
		return ClassTransient, ScopeModel, modelID
	}
	switch {
	case status == 400 || status == 413 || status == 422:
		// The request's fault (shape/size/params): never counts against
		// the provider at any scope.
		return ClassRequestFault, ScopeRequest, "-"
	case status == 404:
		return ClassEntryMissing, ScopeModel, modelID
	case status == 401 || status == 403:
		return ClassAccountDead, ScopeAccount, providerID
	case status == 402:
		// OpenRouter in-flight budget 402: transient concurrency
		// accounting, subclassified BEFORE any account-scope reaction.
		if providerID == "openrouter" && openrouterInFlightBody(bodyExcerpt) {
			return ClassTransient, ScopeModel, modelID
		}
		return ClassAccountDead, ScopeAccount, providerID
	case status == 429:
		if poolID != "" && PoolExhaustionSignal(providerID, bodyExcerpt) {
			return ClassCongested, ScopePool, poolID
		}
		return ClassCongested, ScopeModel, modelID
	default:
		// 5xx and anything unmapped: transient, model scope.
		return ClassTransient, ScopeModel, modelID
	}
}
