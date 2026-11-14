package plan

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps plan-file writes out of the operator home and out of the
// git repo. CreatePlan with an empty project path resolves under MeeptHome
// (see PlanManager.resolvePlanDir). Without this pin, package tests write
// docs/plans/*.md into ~/.meept or into the process CWD.
func TestMain(m *testing.M) {
	if os.Getenv("MEEPT_HOME") == "" {
		dir, err := os.MkdirTemp("", "meept-plan-testhome-")
		if err != nil {
			panic(err)
		}
		if err := os.Setenv("MEEPT_HOME", filepath.Join(dir, "home")); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
