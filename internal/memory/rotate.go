package memory

import (
	"os"
)

// maxCalibrationLogBytes is the size cap shared by the calibration JSONL
// loggers (rejected_candidates.jsonl, claim_verdicts.jsonl,
// raw_responses.jsonl). When a log exceeds this at append time it rotates to
// <name>.jsonl.1 — exactly one prior generation, newest data kept. 10 MiB is
// deliberately hardcoded (no config surface): these are best-effort
// calibration datasets, not audit records.
const maxCalibrationLogBytes int64 = 10 << 20 // 10 MiB

// MaxCalibrationLogBytes exposes the rotation cap for tests and callers that
// need to size a file past it.
const MaxCalibrationLogBytes = maxCalibrationLogBytes

// RotateIfNeeded rotates path to path+".1" when the file currently exceeds
// maxBytes. The previous .1 generation is overwritten, so at most two
// generations of the log exist (current + .1). Rename on the same filesystem
// is atomic, so a crash mid-rotation leaves either the old file or the
// rotated one, never a partial copy. A missing file is not an error (nothing
// to rotate).
func RotateIfNeeded(path string, maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !fi.Mode().IsRegular() || fi.Size() <= maxBytes {
		return nil
	}
	return os.Rename(path, path+".1")
}
