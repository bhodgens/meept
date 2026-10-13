package memory

// auditlog_adapter.go — daemon-side adapter implementing VerdictAuditor over
// any audit-chain writer. This file lives in package memory but imports no
// internal packages beyond auditlog-facing primitives the daemon already
// depends on; the daemon wires it at startup (see internal/daemon). Keeping
// the adapter here would make memory import auditlog, which the leaf forbids
// — so the adapter itself is defined in internal/daemon (verdict_audit.go)
// instead. This file carries only the shared doc anchor.

// VerdictAuditPayloadKeys are the canonical payload keys of a
// memory.claim.verdict audit entry (frozen contract, leaf T03-L1):
//
//	{action: "memory.claim.verdict", claim_id, verdict, reason_hash}
//
// reason_hash is the first 16 hex chars of sha256(reason); an empty reason
// hashes the empty string.
const VerdictAction = "memory.claim.verdict"
