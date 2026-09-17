package llm

// Internal tests pinning audit finding F26: the reaper deletes a REPLACEMENT
// runtime's handles when working from a stale snapshot. The reap path re-reads
// the PID file immediately before removal and skips when its content differs
// from the snapshot pid; the same guard protects the .cmd record.
//
// The fixture below swaps the files between "detection" (what the snapshot
// says) and the RemoveRuntimeHandlesForPids call, exactly as a concurrent
// Start would.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveRuntimeHandlesForPids_SkipsReplacementRuntime swaps the PID file
// and spawn record for a FRESH runtime's after the snapshot was taken: the
// reaper must leave the replacement's handles in place.
func TestRemoveRuntimeHandlesForPids_SkipsReplacementRuntime(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "runtime.pid")

	// Snapshot state: the old runtime's pid, confirmed reaped.
	oldPID := deadSpawnPID(t)

	// Replacement state: a concurrent Start rewrote both files for a new
	// runtime behind the same path.
	newPID := deadSpawnPID(t) // dead keeps the test free of live processes; any pid ≠ oldPID works
	if newPID == oldPID {
		t.Skip("pid reuse made the two pids identical; cannot build the fixture")
	}

	// The snapshot the caller holds: the record naming oldPID.
	snapshotRecord := SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        []string{"mlx_lm", "server", "--port", "8081"},
		AutoStop:    true,
		PID:         oldPID,
	}

	// ...but the FILES on disk were swapped for the replacement before the
	// reaper's removal ran.
	current := SpawnRecord{
		EndpointKey: snapshotRecord.EndpointKey,
		PIDFile:     pidFile,
		Argv:        snapshotRecord.Argv,
		AutoStop:    true,
		PID:         newPID,
	}
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, newPID)), 0o600); err != nil {
		t.Fatalf("write replacement pid file: %v", err)
	}
	if err := WriteSpawnRecord(current); err != nil {
		t.Fatalf("write replacement record: %v", err)
	}

	RemoveRuntimeHandlesForPids(nil, []SpawnRecord{snapshotRecord}, []int{oldPID})

	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("the replacement runtime's PID file must survive (F26), stat err = %v", err)
	}
	rec, err := ReadSpawnRecord(pidFile)
	if err != nil {
		t.Fatalf("the replacement runtime's record must survive (F26): %v", err)
	}
	if rec.PID != newPID {
		t.Errorf("replacement record pid = %d, want %d (untouched)", rec.PID, newPID)
	}
}

// TestRemoveRuntimeHandlesForPids_StillRemovesStaleHandles makes sure the F26
// guard did not regress the F78 fix: when the files still name the snapshot
// pid (the normal reap case, no replacement), both handles are removed.
func TestRemoveRuntimeHandlesForPids_StillRemovesStaleHandles(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "runtime.pid")
	gonePID := deadSpawnPID(t)

	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf(`{"pid":%d,"token":"tok"}`, gonePID)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	rec := SpawnRecord{PIDFile: pidFile, Argv: []string{"sleep", "300"}, AutoStop: true, PID: gonePID}
	if err := WriteSpawnRecord(rec); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}

	RemoveRuntimeHandlesForPids(nil, []SpawnRecord{rec}, []int{gonePID})

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("the reaped runtime's PID file must still be removed, stat err = %v", err)
	}
	if _, err := ReadSpawnRecord(pidFile); err == nil {
		t.Error("the reaped runtime's durable record must still be removed")
	}
}

// TestRemoveRuntimeHandlesForPids_MissingPIDFileStillClearsRecord covers the
// edge where the PID file is already gone but the .cmd record survives: with
// no PID file to disagree, the record must still be cleared when its own pid
// matches the snapshot.
func TestRemoveRuntimeHandlesForPids_MissingPIDFileStillClearsRecord(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "runtime.pid")
	gonePID := deadSpawnPID(t)

	// Only the record exists.
	rec := SpawnRecord{PIDFile: pidFile, Argv: []string{"sleep", "300"}, AutoStop: true, PID: gonePID}
	if err := WriteSpawnRecord(rec); err != nil {
		t.Fatalf("write spawn record: %v", err)
	}

	RemoveRuntimeHandlesForPids(nil, []SpawnRecord{rec}, []int{gonePID})

	if _, err := ReadSpawnRecord(pidFile); err == nil {
		t.Error("the record must be cleared when the PID file is absent and its pid matches the snapshot")
	}
}
