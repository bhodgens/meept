package contact

// Shared accessors for the chain report shape pinned by chain_test.go.
// Same-package test helpers (no import): the concrete ChainReport /
// AttemptRecord types do not exist yet — the leaf-01/02 implementation
// lands them with these field names per contract C2 (per-attempt
// {channel, outcome, err, duration_ms}, one chain id per run).

// reportAttempts returns the ChainReport's per-attempt rows in run order.
func reportAttempts(r ChainReport) []AttemptRecord {
	return r.Attempts
}

// chainID returns the run's single chain execution id (ONE per run; every
// attempt row and every effects/audit row of the run carries it).
func chainID(r ChainReport) string {
	return r.ChainID
}
