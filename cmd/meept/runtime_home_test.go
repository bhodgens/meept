package main

import (
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// TestRuntimePIDPathMatchesSweepScanDirUnderMeeptHome pins audit finding F15
// end to end: with MEEPT_HOME set, the CLI-resolved pid file, its durable spawn
// record and the daemon/doctor run-dir scan must all land in ONE directory, so
// the record half of the orphan sweep actually fires in an isolated rig (before
// the fix the pid file/record went to the operator's real ~/.meept while the
// sweep scanned $MEEPT_HOME/run).
func TestRuntimePIDPathMatchesSweepScanDirUnderMeeptHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	// The shipped pid_file is a tilde path under the default meept home.
	pidFile := pidFileFromConfig(&llm.RuntimeLifecycleConfig{PIDFile: "~/.meept/run/llama-general.pid"})
	scanDir := config.MeeptPath("run")

	if got := filepath.Dir(pidFile); got != scanDir {
		t.Fatalf("pid dir = %q, want the scan dir %q (F15)", got, scanDir)
	}
	// The durable record is written beside the pid file, so its dir matches too.
	if got := filepath.Dir(llm.SpawnRecordPath(pidFile)); got != scanDir {
		t.Fatalf("record dir = %q, want the scan dir %q (F15)", got, scanDir)
	}
	// A record written at that pid path IS found by the run-dir scan.
	if err := llm.WriteSpawnRecord(llm.SpawnRecord{
		PIDFile:  pidFile,
		Argv:     []string{"llama-server", "-m", "/m/x.gguf"},
		AutoStop: true,
	}); err != nil {
		t.Fatalf("WriteSpawnRecord: %v", err)
	}
	recs, err := llm.ScanSpawnRecords(scanDir)
	if err != nil {
		t.Fatalf("ScanSpawnRecords: %v", err)
	}
	found := false
	for _, r := range recs {
		if r.PIDFile == pidFile {
			found = true
		}
	}
	if !found {
		t.Fatalf("the record at %s was not found by the scan of %s: %+v (F15)", pidFile, scanDir, recs)
	}
}
