// Package callerid is the neutral caller-identity seam between the voice
// answer path (which STAMPS who is calling onto every turn context) and
// the enforcement path (which READS the stamp to gate a caller's actions)
// — plan 20261110-caller-authz, frozen contract.
//
// The seam exists so neither side imports the other: internal/voice owns
// telnyx/signal plumbing, internal/employee owns constitution
// enforcement, and a step-1c gate that read voice types directly would
// couple policy to transport. Info is the whole vocabulary — a resolved
// identity (Number/Name/Known) plus the approval bit (Approved) — and
// With/From are the only transport. The package imports stdlib only;
// nothing here may expand that surface.
//
// Presence vs. absence is load-bearing: a non-voice turn never passes
// through With, so From reports ok=false and the step-1c gate is inert
// ("no caller in ctx -> 1c inert"). A zero Info with ok=true is the
// voice-side stamp of an unresolvable caller — legal, distinct, and
// still gated (unknown number = unapproved, decision O2).
package callerid

import "context"

// Info is one resolved inbound caller. Number is the E.164 leg address —
// storage/routing only, NEVER rendered into prompts (display-surface
// redaction is issue #126, a separate tree). Name is the resolved contact
// display name (” when the caller is unknown). Approved is the voice-
// action approval bit from the contact registry (contacts.db is the
// system of record, decision O2; unknown number = false). Known reports
// whether the number resolved to a contact record at all.
type Info struct {
	Number   string // E.164; storage/routing only, never rendered to prompts
	Name     string // resolved contact display name ('' if unknown)
	Approved bool
	Known    bool
}

// ctxKey is the private context key for the stamp. A private struct type
// makes collisions with any other package's context values impossible by
// construction; nothing outside this package can read or overwrite the
// stamp.
type ctxKey struct{}

// With returns a copy of ctx carrying the caller identity. The stamp is
// per-context (never package state), so concurrent voice calls on shared
// parents cannot bleed identities across goroutines. A zero Info is a
// legal stamp — use it for "this is a voice turn, caller unresolved".
func With(ctx context.Context, info Info) context.Context {
	return context.WithValue(ctx, ctxKey{}, info)
}

// From reads the caller stamp. ok=false means the context never passed
// through With: a non-voice turn, and every consumer must treat the
// absence as "no caller gating applies" rather than a zero Info.
func From(ctx context.Context) (Info, bool) {
	if ctx == nil {
		return Info{}, false
	}
	info, ok := ctx.Value(ctxKey{}).(Info)
	return info, ok
}
