package auditlog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// Record is one entry in the hash-chained audit log (master.md C1).
type Record struct {
	Seq        uint64         `json:"seq"`
	PrevHash   string         `json:"prev_hash"`
	RecordHash string         `json:"record_hash"` // excluded from hash input
	Type       string         `json:"type"`
	EmployeeID string         `json:"employee_id"`
	Payload    map[string]any `json:"payload"`
	At         time.Time      `json:"at"`
}

// HashRecord returns lowercase-hex SHA-256 of CanonicalJSON of rec with
// RecordHash omitted and At normalized to RFC3339Nano UTC. Hash-input key
// set: at, employee_id, payload, prev_hash, seq, type.
func HashRecord(rec Record) (string, error) {
	at := rec.At
	if at.IsZero() {
		return "", fmt.Errorf("hash record: zero At")
	}
	input := map[string]any{
		"seq":         rec.Seq,
		"prev_hash":   rec.PrevHash,
		"type":        rec.Type,
		"employee_id": rec.EmployeeID,
		"payload":     rec.Payload,
		"at":          at.UTC().Format(time.RFC3339Nano),
	}
	b, err := CanonicalJSON(input)
	if err != nil {
		return "", fmt.Errorf("hash record: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyResult reports the outcome of a full-chain walk (master.md C1).
type VerifyResult struct {
	OK       bool
	Records  uint64
	Head     string
	BrokenAt uint64
	Reason   string
}

// VerifyChain walks the audit_log_chain table in seq order and verifies each
// record's hash and its prev-hash link. The first failure short-circuits.
func VerifyChain(ctx context.Context, db *sql.DB) (VerifyResult, error) {
	rows, err := db.QueryContext(ctx, `
        SELECT seq, prev_hash, record_hash, type, employee_id, payload, at
        FROM audit_log_chain ORDER BY seq ASC`)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify chain: %w", err)
	}
	defer rows.Close()

	var res VerifyResult
	var wantPrev string
	var prevSeq uint64
	first := true
	for rows.Next() {
		var (
			rec     Record
			payload string
			at      string
		)
		if err := rows.Scan(&rec.Seq, &rec.PrevHash, &rec.RecordHash,
			&rec.Type, &rec.EmployeeID, &payload, &at); err != nil {
			return VerifyResult{}, fmt.Errorf("verify chain scan: %w", err)
		}
		if err := decodePayload(payload, &rec.Payload); err != nil {
			return VerifyResult{}, err
		}
		if err := rec.At.UnmarshalJSON([]byte(`"` + at + `"`)); err != nil {
			return VerifyResult{}, fmt.Errorf("verify chain at %d: %w", rec.Seq, err)
		}
		res.Records++
		if first {
			if rec.Seq != 1 || rec.PrevHash != "" {
				res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
					"genesis must be seq 1 with empty prev_hash"
				return res, nil
			}
			first = false
		} else {
			if rec.Seq != prevSeq+1 {
				res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
					fmt.Sprintf("seq gap: got %d want %d", rec.Seq, prevSeq+1)
				return res, nil
			}
			if rec.PrevHash != wantPrev {
				res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
					fmt.Sprintf("prev_hash mismatch: got %q want %q", rec.PrevHash, wantPrev)
				return res, nil
			}
		}
		h, err := HashRecord(rec)
		if err != nil {
			res.OK, res.BrokenAt, res.Reason = false, rec.Seq, err.Error()
			return res, nil
		}
		if h != rec.RecordHash {
			res.OK, res.BrokenAt, res.Reason = false, rec.Seq,
				fmt.Sprintf("record_hash mismatch: got %q computed %q", rec.RecordHash, h)
			return res, nil
		}
		prevSeq = rec.Seq
		wantPrev = rec.RecordHash
		res.Head = rec.RecordHash
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, fmt.Errorf("verify chain rows: %w", err)
	}
	res.OK = true
	return res, nil
}
