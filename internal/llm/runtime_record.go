package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Durable spawn records.
//
// The orphan sweep (runtime_sweep.go) matches a leftover runtime against a
// command line. It used to learn that command line only from the CURRENT
// endpoint config, which made a leak unreapable the moment the config stopped
// validating: llm.ValidateAndNormalize STATS every model path, so an unmounted
// model volume or a renamed provider makes the endpoint fail validation, it is
// never registered, and the sweep — which only iterates registered endpoints —
// never sees it. The leftover then holds its endpoint port and its model in RAM
// forever, and the next boot cannot even spawn because the duplicate-spawn
// pre-check refuses the occupied port.
//
// A SpawnRecord closes that hole. It is written beside the runtime's PID file
// when the runtime is spawned and removed when it stops, so it records the
// expanded spawn command independently of whether the current config still
// validates. The sweep matches leftovers against records as well as configs,
// which makes detection config-independent.

// spawnRecordSuffix is appended to a PID file path to locate its record.
const spawnRecordSuffix = ".cmd"

// SpawnRecord is the durable record of one runtime spawn: the expanded spawn
// command, written beside the PID file, so the orphan sweep can still match a
// leftover after the config drifts (unmounted model volume, renamed provider)
// — exactly when a leak is otherwise unreapable.
type SpawnRecord struct {
	EndpointKey string   `json:"endpoint_key,omitempty"`
	PIDFile     string   `json:"pid_file"`
	Argv        []string `json:"argv"`
	AutoStop    bool     `json:"auto_stop"`
	PID         int      `json:"pid"`
}

// SpawnRecordPath returns the durable record path for a runtime PID file:
// the PID file path with the record suffix appended.
func SpawnRecordPath(pidFile string) string {
	return pidFile + spawnRecordSuffix
}

// WriteSpawnRecord atomically writes rec beside its PID file (temp file +
// rename, mode 0o600), creating the directory when needed. The write is atomic
// so a concurrent reader never observes a torn record, mirroring the PID-file
// writer in runtime_process.go.
func WriteSpawnRecord(rec SpawnRecord) error {
	if rec.PIDFile == "" {
		return fmt.Errorf("spawn record has no pid_file")
	}
	path := SpawnRecordPath(rec.PIDFile)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below has succeeded
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ReadSpawnRecord reads and parses the record written beside pidFile.
func ReadSpawnRecord(pidFile string) (SpawnRecord, error) {
	path := SpawnRecordPath(pidFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return SpawnRecord{}, err
	}
	var rec SpawnRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return SpawnRecord{}, fmt.Errorf("invalid spawn record %s: %w", path, err)
	}
	return rec, nil
}

// RemoveSpawnRecord deletes the record written beside pidFile. Best-effort: a
// missing file is not an error, and there is no logger in this package-level
// helper to report failures to.
func RemoveSpawnRecord(pidFile string) {
	if pidFile == "" {
		return
	}
	_ = os.Remove(SpawnRecordPath(pidFile))
}

// ScanSpawnRecords returns every parseable record in dir (glob *.cmd). An
// unreadable or invalid entry is skipped so one corrupt file cannot hide the
// rest of the records, and a missing dir yields no records and no error — a
// scan failure must never abort the sweep.
func ScanSpawnRecords(dir string) ([]SpawnRecord, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*"+spawnRecordSuffix))
	if err != nil {
		return nil, err
	}
	var recs []SpawnRecord
	for _, path := range matches {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var rec SpawnRecord
		if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
			continue
		}
		if rec.PIDFile == "" {
			// A record written without the field still identifies its PID
			// file by its own name; backfill so the sweep can use it.
			rec.PIDFile = strings.TrimSuffix(path, spawnRecordSuffix)
		}
		recs = append(recs, rec)
	}
	return recs, nil
}
