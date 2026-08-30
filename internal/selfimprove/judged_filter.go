package selfimprove

// OnlyJudged gates learned patterns on trajectory judgment outcome: only
// patterns whose source trajectory is judged AND passed are returned.
// Unjudged != passed (master contract C7): a pattern distilled from an
// unjudged or failed trajectory must not be promoted to the evolver.
//
// Pattern → judgment linkage is pattern.Metadata["trajectory_id"] (string);
// judgments is keyed by trajectory ID. A pattern with no trajectory_id in
// metadata, an empty one, a non-string one, or one absent from the judgment
// map is dropped. Judged-but-failed patterns are also dropped: this is the
// strict promotion gate; failure lessons need an explicit caller ask.
//
// DEVIATION NOTE (mandated signature impossible): the plan asks for
//
//	OnlyJudged(patterns []LearnedPattern, judgments map[string]eval.TrajectoryJudgment) []LearnedPattern
//
// but internal/eval already depends on internal/agent which depends on
// internal/selfimprove (eval → agent → selfimprove), so selfimprove cannot
// import eval without an import cycle. The judgment parameter is therefore
// generalized over any type reporting its pass state; eval.TrajectoryJudgment
// satisfies the constraint via JudgedPassed(), so the mandated call shape
// works unchanged:
//
//	selfimprove.OnlyJudged(patterns, judgments) // map[string]eval.TrajectoryJudgment
//
// Returns an empty (non-nil) slice when nothing qualifies.
func OnlyJudged[J interface{ JudgedPassed() bool }](patterns []LearnedPattern, judgments map[string]J) []LearnedPattern {
	out := make([]LearnedPattern, 0, len(patterns))
	for _, p := range patterns {
		raw, ok := p.Metadata["trajectory_id"]
		if !ok {
			continue
		}
		id, ok := raw.(string)
		if !ok || id == "" {
			continue
		}
		j, ok := judgments[id]
		if !ok || !j.JudgedPassed() {
			continue
		}
		out = append(out, p)
	}
	return out
}
