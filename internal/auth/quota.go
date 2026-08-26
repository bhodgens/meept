package auth

import "context"

// OPEN QUESTION (do not resolve): quota sizing basis undecided —
// (a) percentage of machine-level config budget per user,
// (b) absolute per-user token budget, (c) per-node vs pooled-cluster.
// Implement a no-op Evaluator until decided.

// QuotaEvaluator decides whether an identity may spend the given cost
// (tokens, requests, or another unit chosen once sizing is resolved).
type QuotaEvaluator interface {
	Allow(ctx context.Context, id *Identity, cost int) (bool, error)
}

// NoopQuota is the placeholder QuotaEvaluator: it allows everything and
// never errors. Wire a real evaluator only after the open question above
// is resolved.
type NoopQuota struct{}

// Allow reports that every request is within quota.
func (NoopQuota) Allow(_ context.Context, _ *Identity, _ int) (bool, error) {
	return true, nil
}
