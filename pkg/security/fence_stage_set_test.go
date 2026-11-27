package security

// fence_stage_set_test.go — bughunt H4 pin: FenceStageSet distinguishes a
// checker carrying a fence stage from a glob-only base checker. The
// voice/employee lane arm consults it to skip double-arming.

import "testing"

func TestFenceStageSet_NilSafeAndTracksSetFence(t *testing.T) {
	var nilChecker *PermissionChecker
	if nilChecker.FenceStageSet() {
		t.Fatal("nil checker must report no fence stage without panicking")
	}

	base := NewPermissionChecker(Config{})
	if base.FenceStageSet() {
		t.Fatal("fresh base checker must carry no fence stage")
	}

	base.SetFence(fakeFenceStageForSet{})
	if !base.FenceStageSet() {
		t.Fatal("SetFence must make FenceStageSet report true")
	}

	// Typed-nil guarded: SetFence(typedNil) clears the stage.
	var typedNil *typedNilFenceStage
	base.SetFence(typedNil)
	if base.FenceStageSet() {
		t.Fatal("typed-nil fence must clear the stage (SetFence guard)")
	}
}

type fakeFenceStageForSet struct{}

func (fakeFenceStageForSet) CheckPath(path, op string) error            { return nil }
func (fakeFenceStageForSet) CheckCommand(command, workDir string) error { return nil }

type typedNilFenceStage struct {
	fakeFenceStageForSet
}
