package l0

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ErrStop is a sentinel for the lineFn to signal "stop scanning" without an
// error condition.
var errStop = fmt.Errorf("l0: stop")

// readLines streams one JSONL file, decoding each line into Record and
// invoking fn. Malformed lines are skipped (a torn final line from a crash
// must not break reads of the rest of the day).
func readLines(path string, fn func(Record) error) error {
	f, err := os.Open(path) // #nosec G304 -- path is caller-supplied data-dir path
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("l0: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // torn/corrupt line: skip, keep scanning
		}
		if err := fn(rec); err != nil {
			if err == errStop {
				return nil
			}
			return err
		}
	}
	return sc.Err()
}

// ReadSince returns records from path ordered as written, starting strictly
// after the line whose ID equals sinceID (empty sinceID reads from the
// start), capped at limit records (limit <= 0 means unlimited).
func ReadSince(path, sinceID string, limit int) ([]Record, error) {
	var out []Record
	seen := sinceID == ""
	err := readLines(path, func(rec Record) error {
		if !seen {
			if rec.ID == sinceID {
				seen = true
			}
			return nil
		}
		out = append(out, rec)
		if limit > 0 && len(out) >= limit {
			return errStop
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReadSession returns the given session's records from one day's file
// (date as YYYY-MM-DD), in file order. A missing file returns an empty
// slice, not an error.
func ReadSession(dir, date, sessionID string) ([]Record, error) {
	var out []Record
	err := readLines(datePath(dir, date), func(rec Record) error {
		if rec.SessionID == sessionID {
			out = append(out, rec)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
