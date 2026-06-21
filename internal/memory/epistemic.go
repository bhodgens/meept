package memory

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ClaimStatus represents the lifecycle state of a claim.
type ClaimStatus string

const (
	// ClaimStatusConfirmed is a user-asserted claim with full trust (weight 1.0).
	ClaimStatusConfirmed ClaimStatus = "confirmed"
	// ClaimStatusAuto is a claim extracted by the ambient classifier with
	// configurable trust weight (default 0.5).
	ClaimStatusAuto ClaimStatus = "auto"
	// ClaimStatusPromoted is a former auto-claim that the user promoted to
	// full trust (weight 1.0).
	ClaimStatusPromoted ClaimStatus = "promoted"
	// ClaimStatusRejected is a claim the user rejected; excluded from queries.
	ClaimStatusRejected ClaimStatus = "rejected"
)

// DefaultAutoClaimTrustWeight is the default trust weight applied to
// ambient-extracted claims. Configurable via EpistemicConfig.AutoTrustWeight.
const DefaultAutoClaimTrustWeight = 0.5

// EffectiveAutoTrustWeight returns the configured auto-trust weight or the
// default when the configured value is zero, negative, or greater than 1.
func EffectiveAutoTrustWeight(configured float64) float64 {
	if configured <= 0 || configured > 1.0 {
		return DefaultAutoClaimTrustWeight
	}
	return configured
}

// TrustWeight returns the trust weight for a claim status.
//   - confirmed, promoted → 1.0
//   - auto                → configurable (default 0.5)
//   - rejected            → 0.0
func (s ClaimStatus) TrustWeight(autoWeight float64) float64 {
	switch s {
	case ClaimStatusConfirmed, ClaimStatusPromoted:
		return 1.0
	case ClaimStatusAuto:
		return EffectiveAutoTrustWeight(autoWeight)
	case ClaimStatusRejected:
		return 0.0
	}
	return 0.0
}

// IsRejected reports whether the status is ClaimStatusRejected.
func (s ClaimStatus) IsRejected() bool {
	return s == ClaimStatusRejected
}

// IsEligibleCanonical reports whether a claim with this status may serve as
// a canonical source. Only confirmed and promoted claims qualify.
func (s ClaimStatus) IsEligibleCanonical() bool {
	return s == ClaimStatusConfirmed || s == ClaimStatusPromoted
}

// EpistemicMemTypes lists all memory types treated as epistemic by the
// detection pipeline and helper methods.
var EpistemicMemTypes = []MemoryType{
	MemoryTypeClaim,
	MemoryTypeDecision,
	MemoryTypePrediction,
	MemoryTypeQuestion,
}

// IsEpistemicType reports whether a MemoryType is one of the epistemic
// types (claim, decision, prediction, question).
func IsEpistemicType(t MemoryType) bool {
	switch t {
	case MemoryTypeClaim, MemoryTypeDecision, MemoryTypePrediction, MemoryTypeQuestion:
		return true
	}
	return false
}

// Claim is a structured assertion of belief.
type Claim struct {
	Text       string      // the claim itself
	Premises   []string    // supporting claim IDs or text snippets
	Source     string      // URL, citation, or "user"
	Confidence float64     // 0.0-1.0, user-asserted
	Tags       []string    // controlled-vocabulary tags
	Status     ClaimStatus // lifecycle status
}

// Decision is a recorded call with expected outcome and review schedule.
type Decision struct {
	Call            string     // the decision made
	Alternatives    []string   // alternatives considered
	ExpectedOutcome string     // what the user expects to happen
	ReviewAt        *time.Time // when to revisit; nil = no auto-review
	Premises        []string   // claim IDs this decision rests on
	Status          string     // "open", "reviewed", "superseded"
}

// Prediction is a forecast with horizon and resolution tracking.
type Prediction struct {
	Forecast        string     // the prediction
	Horizon         time.Time  // when it should resolve
	RelatedDecision string     // decision ID (optional)
	Outcome         string     // filled in on resolution
	ResolvedAt      *time.Time // when the prediction was resolved
}

// Question is an open question the user is tracking.
type Question struct {
	Text           string   // the question
	RelatedClaims  []string // claim IDs that bear on this question
	Status         string   // "open", "answered"
	AnswerClaim    string   // claim ID that answers it (if answered)
}

// asString coerces a metadata value to a string, returning "" for non-strings.
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// stringTokenSet splits s on whitespace into a set of lowercase tokens.
func stringTokenSet(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, w := range strings.Fields(strings.ToLower(s)) {
		set[w] = struct{}{}
	}
	return set
}

// outcomeOverlapScore returns the Jaccard token overlap between two outcome
// strings. Returns 1.0 for identical non-empty strings, 0.0 for disjoint
// sets or empty inputs.
func outcomeOverlapScore(expected, actual string) float64 {
	if expected == "" || actual == "" {
		return 0.0
	}
	a := stringTokenSet(expected)
	b := stringTokenSet(actual)
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	var intersection int
	for tok := range a {
		if _, ok := b[tok]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0.0
	}
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}

// StoreClaim writes a claim as a typed memory and returns its ID.
func (m *Manager) StoreClaim(ctx context.Context, c Claim) (string, error) {
	if c.Status == "" {
		c.Status = ClaimStatusConfirmed
	}
	meta := map[string]any{
		"status":     string(c.Status),
		"confidence": c.Confidence,
	}
	if c.Source != "" {
		meta["source"] = c.Source
	}
	if len(c.Premises) > 0 {
		meta["premises"] = c.Premises
	}
	if len(c.Tags) > 0 {
		meta["tags"] = c.Tags
	}
	return m.Store(ctx, Memory{
		Type:     MemoryTypeClaim,
		Category: "claim",
		Content:  c.Text,
		Metadata: meta,
	})
}

// StoreDecision writes a decision as a typed memory and returns its ID.
func (m *Manager) StoreDecision(ctx context.Context, d Decision) (string, error) {
	if d.Status == "" {
		d.Status = "open"
	}
	meta := map[string]any{
		"status":          d.Status,
		"expected_outcome": d.ExpectedOutcome,
	}
	if len(d.Alternatives) > 0 {
		meta["alternatives"] = d.Alternatives
	}
	if d.ReviewAt != nil {
		meta["review_at"] = d.ReviewAt.Format(time.RFC3339)
	}
	if len(d.Premises) > 0 {
		meta["premises"] = d.Premises
	}
	return m.Store(ctx, Memory{
		Type:     MemoryTypeDecision,
		Category: "decision",
		Content:  d.Call,
		Metadata: meta,
	})
}

// StorePrediction writes a prediction as a typed memory and returns its ID.
func (m *Manager) StorePrediction(ctx context.Context, p Prediction) (string, error) {
	meta := map[string]any{
		"horizon": p.Horizon.Format(time.RFC3339),
		"status":  "open",
	}
	if p.RelatedDecision != "" {
		meta["related_decision"] = p.RelatedDecision
	}
	return m.Store(ctx, Memory{
		Type:     MemoryTypePrediction,
		Category: "prediction",
		Content:  p.Forecast,
		Metadata: meta,
	})
}

// StoreQuestion writes an open question as a typed memory and returns its ID.
func (m *Manager) StoreQuestion(ctx context.Context, q Question) (string, error) {
	if q.Status == "" {
		q.Status = "open"
	}
	meta := map[string]any{
		"status": q.Status,
	}
	if len(q.RelatedClaims) > 0 {
		meta["related_claims"] = q.RelatedClaims
	}
	if q.AnswerClaim != "" {
		meta["answer_claim"] = q.AnswerClaim
	}
	return m.Store(ctx, Memory{
		Type:     MemoryTypeQuestion,
		Category: "question",
		Content:  q.Text,
		Metadata: meta,
	})
}

// updateMetadataField loads a memory, sets a metadata key, and writes a new
// version via StoreVersioned.
func (m *Manager) updateMetadataField(ctx context.Context, id, key string, value any) error {
	mem, err := m.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("load memory %s: %w", id, err)
	}
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]any)
	}
	mem.Metadata[key] = value
	if _, err := m.StoreVersioned(ctx, *mem, StoreOptions{CreateVersion: true, ParentID: id}); err != nil {
		return fmt.Errorf("store versioned: %w", err)
	}
	return nil
}

// PromoteClaim transitions an auto claim to promoted status.
func (m *Manager) PromoteClaim(ctx context.Context, claimID string) error {
	mem, err := m.GetByID(ctx, claimID)
	if err != nil {
		return fmt.Errorf("load claim %s: %w", claimID, err)
	}
	if mem.Type != MemoryTypeClaim {
		return fmt.Errorf("memory %s is type %s, not claim", claimID, mem.Type)
	}
	if asString(mem.Metadata["status"]) != string(ClaimStatusAuto) {
		return fmt.Errorf("claim %s is not auto (status=%q)", claimID, mem.Metadata["status"])
	}
	return m.updateMetadataField(ctx, claimID, "status", string(ClaimStatusPromoted))
}

// RejectClaim transitions a claim to rejected status.
func (m *Manager) RejectClaim(ctx context.Context, claimID string) error {
	mem, err := m.GetByID(ctx, claimID)
	if err != nil {
		return fmt.Errorf("load claim %s: %w", claimID, err)
	}
	if mem.Type != MemoryTypeClaim {
		return fmt.Errorf("memory %s is type %s, not claim", claimID, mem.Type)
	}
	return m.updateMetadataField(ctx, claimID, "status", string(ClaimStatusRejected))
}

// ListAutoClaims returns claims with status=auto, optionally filtered by
// created_after for incremental review prompts.
func (m *Manager) ListAutoClaims(ctx context.Context, createdAfter time.Time, limit int) ([]MemoryResult, error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return nil, errors.New("memory manager not initialized")
	}
	if limit <= 0 {
		limit = 20
	}
	// Use a broad search and filter; the FTS5 index handles free-text but
	// metadata filtering happens post-query.
	results, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypeClaim,
		Limit: limit * 4,
	})
	if err != nil {
		return nil, fmt.Errorf("search auto claims: %w", err)
	}
	var out []MemoryResult
	for _, r := range results {
		if r.Memory.Type != MemoryTypeClaim {
			continue
		}
		if asString(r.Memory.Metadata["status"]) != string(ClaimStatusAuto) {
			continue
		}
		if !createdAfter.IsZero() && r.Memory.CreatedAt.Before(createdAfter) {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ListPendingReviews returns decisions whose ReviewAt is before the given
// time, and predictions whose Horizon is before the given time.
func (m *Manager) ListPendingReviews(ctx context.Context, before time.Time) (decisions, predictions []MemoryResult, err error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return nil, nil, errors.New("memory manager not initialized")
	}

	decResults, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypeDecision,
		Limit: 50,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("search decisions: %w", err)
	}
	for _, r := range decResults {
		if r.Memory.Type != MemoryTypeDecision {
			continue
		}
		if asString(r.Memory.Metadata["status"]) != "open" {
			continue
		}
		reviewAtStr := asString(r.Memory.Metadata["review_at"])
		if reviewAtStr == "" {
			continue
		}
		reviewAt, parseErr := time.Parse(time.RFC3339, reviewAtStr)
		if parseErr != nil {
			continue
		}
		if reviewAt.Before(before) {
			decisions = append(decisions, r)
		}
	}

	predResults, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypePrediction,
		Limit: 50,
	})
	if err != nil {
		return decisions, nil, fmt.Errorf("search predictions: %w", err)
	}
	for _, r := range predResults {
		if r.Memory.Type != MemoryTypePrediction {
			continue
		}
		horizonStr := asString(r.Memory.Metadata["horizon"])
		if horizonStr == "" {
			continue
		}
		horizon, parseErr := time.Parse(time.RFC3339, horizonStr)
		if parseErr != nil {
			continue
		}
		if horizon.Before(before) {
			predictions = append(predictions, r)
		}
	}
	return decisions, predictions, nil
}

// FindCanonicalFor returns the canonical claim for a topic. Walks
// canonical_for metadata first, falls back to the first eligible
// (confirmed/promoted) claim matching the topic. Never returns auto or
// rejected claims.
func (m *Manager) FindCanonicalFor(ctx context.Context, topic string) (*Memory, error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return nil, errors.New("memory manager not initialized")
	}

	results, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypeClaim,
		Query: topic,
		Limit: 20,
	})
	if err != nil {
		return nil, fmt.Errorf("search canonical: %w", err)
	}

	// First pass: explicit canonical_for match.
	for _, r := range results {
		if r.Memory.Type != MemoryTypeClaim {
			continue
		}
		if asString(r.Memory.Metadata["canonical_for"]) != topic {
			continue
		}
		status := ClaimStatus(asString(r.Memory.Metadata["status"]))
		if status.IsEligibleCanonical() {
			mem := r.Memory
			return &mem, nil
		}
	}
	// Second pass: any eligible claim matching the topic.
	for _, r := range results {
		if r.Memory.Type != MemoryTypeClaim {
			continue
		}
		status := ClaimStatus(asString(r.Memory.Metadata["status"]))
		if status.IsEligibleCanonical() {
			mem := r.Memory
			return &mem, nil
		}
	}
	return nil, ErrNotFound
}

// MarkSuperseded flips is_current=0 on oldID, writes a superseded edge from
// oldID to newID, and redirects incoming evidence_for/evidence_against edges
// from oldID to newID. Returns the count of redirected edges and an audit ID.
//
// auto claims cannot supersede confirmed/promoted claims.
func (m *Manager) MarkSuperseded(ctx context.Context, oldID, newID string) (redirectedEdges int, auditID string, err error) {
	m.mu.RLock()
	initialized := m.initialized
	graph := m.graph
	m.mu.RUnlock()
	if !initialized {
		return 0, "", errors.New("memory manager not initialized")
	}

	oldMem, err := m.GetByID(ctx, oldID)
	if err != nil {
		return 0, "", fmt.Errorf("load old memory %s: %w", oldID, err)
	}
	newMem, err := m.GetByID(ctx, newID)
	if err != nil {
		return 0, "", fmt.Errorf("load new memory %s: %w", newID, err)
	}

	// Enforce auto-cannot-supersede-confirmed/promoted.
	oldStatus := ClaimStatus(asString(oldMem.Metadata["status"]))
	newStatus := ClaimStatus(asString(newMem.Metadata["status"]))
	if newStatus == ClaimStatusAuto && oldStatus.IsEligibleCanonical() {
		return 0, "", fmt.Errorf("auto claim %s cannot supersede %s claim %s",
			newID, oldStatus, oldID)
	}

	// Mark old version non-current.
	if err := m.markVersionNonCurrent(ctx, oldID); err != nil {
		return 0, "", fmt.Errorf("mark old non-current: %w", err)
	}

	// Write superseded edge.
	auditID = generateAuditID()
	if graph != nil {
		if err := graph.AddEdge(ctx, MemoryEdge{
			SourceID:  oldID,
			TargetID:  newID,
			EdgeType:  EdgeTypeSuperseded,
			Weight:    1.0,
			Confidence: 1.0,
			Metadata: map[string]any{
				"audit_id": auditID,
				"at":       time.Now().Format(time.RFC3339),
			},
		}); err != nil {
			return 0, auditID, fmt.Errorf("write superseded edge: %w", err)
		}

		// Redirect incoming evidence_for / evidence_against edges.
		_, inEdges, err := graph.GetEdgesForMemory(ctx, oldID)
		if err != nil {
			return 0, auditID, fmt.Errorf("get edges for redirect: %w", err)
		}
		for _, e := range inEdges {
			if e.EdgeType != EdgeTypeEvidenceFor && e.EdgeType != EdgeTypeEvidenceAgainst {
				continue
			}
			if err := graph.AddEdge(ctx, MemoryEdge{
				SourceID:   e.SourceID,
				TargetID:   newID,
				EdgeType:   e.EdgeType,
				Weight:     e.Weight,
				Confidence: e.Confidence,
				Metadata: map[string]any{
					"audit_id":     auditID,
					"redirected_from": oldID,
					"at":           time.Now().Format(time.RFC3339),
				},
			}); err != nil {
				return redirectedEdges, auditID, fmt.Errorf("redirect edge: %w", err)
			}
			redirectedEdges++
		}
	}
	return redirectedEdges, auditID, nil
}

// MarkResolved closes a prediction with the given outcome.
func (m *Manager) MarkResolved(ctx context.Context, predictionID, outcome string) (string, error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return "", errors.New("memory manager not initialized")
	}
	mem, err := m.GetByID(ctx, predictionID)
	if err != nil {
		return "", fmt.Errorf("load prediction %s: %w", predictionID, err)
	}
	if mem.Type != MemoryTypePrediction {
		return "", fmt.Errorf("memory %s is type %s, not prediction", predictionID, mem.Type)
	}
	auditID := generateAuditID()
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]any)
	}
	mem.Metadata["outcome"] = outcome
	mem.Metadata["status"] = "resolved"
	mem.Metadata["resolved_at"] = time.Now().Format(time.RFC3339)
	mem.Metadata["audit_id"] = auditID
	if _, err := m.StoreVersioned(ctx, *mem, StoreOptions{CreateVersion: true, ParentID: predictionID}); err != nil {
		return "", fmt.Errorf("store versioned: %w", err)
	}
	return auditID, nil
}

// RecordReview closes a decision with the actual outcome and scores the
// expected-vs-actual overlap. Returns the score and an audit ID.
func (m *Manager) RecordReview(ctx context.Context, decisionID, actualOutcome string) (float64, string, error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return 0, "", errors.New("memory manager not initialized")
	}
	mem, err := m.GetByID(ctx, decisionID)
	if err != nil {
		return 0, "", fmt.Errorf("load decision %s: %w", decisionID, err)
	}
	if mem.Type != MemoryTypeDecision {
		return 0, "", fmt.Errorf("memory %s is type %s, not decision", decisionID, mem.Type)
	}
	expected := asString(mem.Metadata["expected_outcome"])
	score := outcomeOverlapScore(expected, actualOutcome)
	auditID := generateAuditID()
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]any)
	}
	mem.Metadata["actual_outcome"] = actualOutcome
	mem.Metadata["review_score"] = score
	mem.Metadata["status"] = "reviewed"
	mem.Metadata["reviewed_at"] = time.Now().Format(time.RFC3339)
	mem.Metadata["audit_id"] = auditID
	if _, err := m.StoreVersioned(ctx, *mem, StoreOptions{CreateVersion: true, ParentID: decisionID}); err != nil {
		return 0, "", fmt.Errorf("store versioned: %w", err)
	}
	return score, auditID, nil
}

// generateAuditID returns a short hex identifier for epistemic operations.
func generateAuditID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("aud-%x", buf)
}

// Suppress unused-import warnings until later tasks wire these into use.
var _ = context.Background
