// Package employee — drift.go implements the DriftScore calculation formula (E5).
//
// DriftScore = sum(finding_i.weight * time_decay_i) / max_score
//
// Where:
//   - critical findings have weight 1.0
//   - warning findings have weight 0.3
//   - info findings have weight 0.1
//   - time_decay = exp(-days_since_detected / half_life)
//   - half_life defaults to 7 days (configurable)
//   - max_score = sum(all_weights) (i.e., if all findings were critical and
//     detected today, the score would be 1.0)
//
// The result is clamped to [0.0, 1.0]. An empty findings list yields 0.0.
package employee

import (
	"math"
	"time"
)

// DriftHalfLife is the default half-life for time decay in the drift score
// formula. Findings older than this have their weight halved.
const DriftHalfLife = 7 * 24 * time.Hour // 7 days

// Severity weights for the drift score formula.
var severityWeights = map[AuditSeverity]float64{
	SeverityCritical: 1.0,
	SeverityWarning:  0.3,
	SeverityInfo:     0.1,
}

// CalculateDriftScore computes the drift score for a set of findings using
// the formula:
//
//	DriftScore = sum(finding_i.weight * time_decay_i) / max_score
//
// where:
//   - weight is determined by severity (critical=1.0, warning=0.3, info=0.1)
//   - time_decay = exp(-days_since_detected / half_life_days)
//   - max_score = sum of all finding weights (without time decay)
//
// The score is clamped to [0.0, 1.0]. An empty findings list yields 0.0.
// Findings with a resolved_at timestamp are excluded (resolved findings
// don't contribute to ongoing drift).
//
// The halfLife parameter controls how quickly findings' impact decays. A
// half-life of 7 days means a finding detected 7 days ago contributes half
// its weight. Pass DriftHalfLife for the default.
func CalculateDriftScore(findings []AuditFinding, now time.Time, halfLife time.Duration) float64 {
	if len(findings) == 0 {
		return 0.0
	}
	if halfLife <= 0 {
		halfLife = DriftHalfLife
	}

	halfLifeDays := halfLife.Hours() / 24
	if halfLifeDays <= 0 {
		halfLifeDays = 7
	}

	var weightedSum float64
	var maxScore float64

	for _, f := range findings {
		// Skip resolved findings — they don't contribute to ongoing drift.
		if f.ResolvedAt != nil {
			continue
		}
		weight, ok := severityWeights[f.Severity]
		if !ok {
			weight = 0.1 // unknown severity treated as info
		}

		// Time decay: exponential decay based on days since detection.
		daysSince := now.Sub(f.DetectedAt).Hours() / 24
		if daysSince < 0 {
			daysSince = 0 // future-dated findings treated as just detected
		}
		timeDecay := math.Exp(-daysSince / halfLifeDays)

		weightedSum += weight * timeDecay
		maxScore += weight
	}

	if maxScore == 0 {
		return 0.0
	}

	score := weightedSum / maxScore
	// Clamp to [0.0, 1.0].
	if score < 0 {
		return 0.0
	}
	if score > 1 {
		return 1.0
	}
	return score
}
