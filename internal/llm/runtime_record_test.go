package llm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpawnRecord_RoundTrip(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "run", "mlx.pid")
	rec := SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     pidFile,
		Argv:        []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
		AutoStop:    true,
		PID:         4321,
	}
	if err := WriteSpawnRecord(rec); err != nil {
		t.Fatalf("WriteSpawnRecord: %v", err)
	}

	path := SpawnRecordPath(pidFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat record %s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("record mode = %o, want 0600", perm)
	}

	got, err := ReadSpawnRecord(pidFile)
	if err != nil {
		t.Fatalf("ReadSpawnRecord: %v", err)
	}
	if got.EndpointKey != rec.EndpointKey || got.PIDFile != rec.PIDFile ||
		got.PID != rec.PID || got.AutoStop != rec.AutoStop {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, rec)
	}
	if len(got.Argv) != len(rec.Argv) {
		t.Fatalf("argv length = %d, want %d", len(got.Argv), len(rec.Argv))
	}
	for i := range rec.Argv {
		if got.Argv[i] != rec.Argv[i] {
			t.Errorf("argv[%d] = %q, want %q", i, got.Argv[i], rec.Argv[i])
		}
	}

	RemoveSpawnRecord(pidFile)
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("RemoveSpawnRecord must delete the record, stat err = %v", statErr)
	}
}

func TestWriteSpawnRecord_RequiresPIDFile(t *testing.T) {
	if err := WriteSpawnRecord(SpawnRecord{Argv: []string{"mlx_lm"}}); err == nil {
		t.Fatal("expected an error for a record with no pid_file")
	}
}

// TestScanSpawnRecords_SkipsGarbageAndMissingDir proves one corrupt file cannot
// hide the rest of the records, and that a missing run dir is not a scan
// failure (the first boot on a fresh install has no run dir yet).
func TestScanSpawnRecords_SkipsGarbageAndMissingDir(t *testing.T) {
	dir := t.TempDir()

	valid := SpawnRecord{
		EndpointKey: "mlx:127.0.0.1:8081",
		PIDFile:     filepath.Join(dir, "mlx.pid"),
		Argv:        []string{"mlx_lm", "server", "--model", "/m/x", "--port", "8081"},
		AutoStop:    true,
		PID:         900,
	}
	if err := WriteSpawnRecord(valid); err != nil {
		t.Fatalf("WriteSpawnRecord: %v", err)
	}
	// A garbage .cmd entry must be skipped, not fail the scan.
	if err := os.WriteFile(filepath.Join(dir, "broken.pid.cmd"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write garbage record: %v", err)
	}
	// A non-record file must be ignored entirely.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	got, err := ScanSpawnRecords(dir)
	if err != nil {
		t.Fatalf("ScanSpawnRecords: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly the valid record, got %d: %+v", len(got), got)
	}
	if got[0].EndpointKey != valid.EndpointKey || got[0].PID != valid.PID {
		t.Errorf("scanned record = %+v, want %+v", got[0], valid)
	}

	missing, err := ScanSpawnRecords(filepath.Join(dir, "does-not-exist"))
	if err != nil {
		t.Fatalf("ScanSpawnRecords on a missing dir must not fail: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no records from a missing dir, got %+v", missing)
	}
}

// TestScanSpawnRecords_BackfillsPIDFile covers a record written without the
// pid_file field: its own name still identifies the PID file.
func TestScanSpawnRecords_BackfillsPIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mlx.pid.cmd")
	if err := os.WriteFile(path, []byte(`{"argv":["mlx_lm","server"],"auto_stop":true}`), 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}

	got, err := ScanSpawnRecords(dir)
	if err != nil {
		t.Fatalf("ScanSpawnRecords: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 record, got %d: %+v", len(got), got)
	}
	if want := filepath.Join(dir, "mlx.pid"); got[0].PIDFile != want {
		t.Errorf("PIDFile = %q, want %q (backfilled from the record name)", got[0].PIDFile, want)
	}
}
