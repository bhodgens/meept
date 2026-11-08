// Package contact is the per-person contact registry + fallback chain
// engine (contact-channels plan, contracts C1/C2, owner leaf 01).
//
// The registry stores people (display name, E.164 phone normalized at
// write, nullable Signal UUID, ordered channel policy) in SQLite
// (store.go, mirroring internal/voice/callstore.go). The chain engine
// (chain.go) walks a record's channel policy - or a per-run override -
// through registered ChannelAttempt adapters, stopping on
// connected/delivered and writing one shared chain execution id into
// the effects ledger + audit log for every attempt row.
//
// This package owns the SEAM and REGISTRY only: it never imports
// telnyx or signal packages; channel adapters (plan leaves 11/12)
// implement ChannelAttempt against these types.
package contact
