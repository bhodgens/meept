package security

// fence.go — the FenceStage pipeline seam (agent-groups branch-01 leaf 01,
// docs/plans/20261030-agent-groups/01-permissions/01-fence-stage.md).
//
// C1 contract (verbatim): FenceStage interface {CheckPath(path, op string)
// error; CheckCommand(command, workDir string) error}. The interface lives
// in pkg/security and MUST NOT import internal/ — the daemon adapts its
// internal FenceChecker through NewFenceStageAdapter.

// FenceStage is the project-boundary check the permission pipeline consults
// for file and shell actions (after the path globs, before confirmation
// gating). Implementations must be safe for concurrent Check calls.
type FenceStage interface {
	CheckPath(path, op string) error
	CheckCommand(command, workDir string) error
}

// FenceChecker is the structural subset a fence adapter must supply. It
// mirrors the shape of the daemon's shared fence checker so that checker
// (and any test double with the same two methods) satisfies it without an
// import — pkg/security never imports internal/ (import-cycle direction).
type FenceChecker interface {
	CheckPath(path, op string) error
	CheckCommand(command, workDir string) error
}

// fenceStageAdapter routes FenceStage calls to an embedded FenceChecker.
type fenceStageAdapter struct {
	FenceChecker
}

// NewFenceStageAdapter adapts any FenceChecker-shaped value (the daemon's
// internal fence checker among them) to the FenceStage pipeline seam. A nil
// adapter input yields a nil FenceStage interface (callers treat that as
// "no fence").
func NewFenceStageAdapter(fc FenceChecker) FenceStage {
	if fc == nil {
		return nil
	}
	return fenceStageAdapter{FenceChecker: fc}
}
