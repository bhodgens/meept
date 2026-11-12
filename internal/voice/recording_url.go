package voice

// Recording URL storage (leaf-06 gate-compile prerequisite, declared by
// leaf 04 per the 00T ordering note). The recording.saved webhook stores
// its URL on the call record in an additive recording_url column; leaf 06
// owns the config keys, the answer-path start, and the webhook route —
// this is only the two read/write primitives the gate pins.

import (
	"context"
	"fmt"
)

// SetRecordingURL stores the carrier's recording URL on the call row
// (additive recording_url TEXT column, guarded migration). Unknown call
// ids are a no-op (idempotent webhook posture).
func (s *CallStore) SetRecordingURL(ctx context.Context, callID, url string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE voice_calls SET recording_url = ? WHERE call_id = ?`, url, callID,
	); err != nil {
		return fmt.Errorf("voice callstore: set recording url for %s: %w", callID, err)
	}
	return nil
}

// RecordingURL returns the stored recording URL for callID, empty when
// the call is unknown or no recording was saved.
func (s *CallStore) RecordingURL(ctx context.Context, callID string) string {
	var url string
	if err := s.db.QueryRowContext(ctx,
		`SELECT recording_url FROM voice_calls WHERE call_id = ?`, callID,
	).Scan(&url); err != nil {
		return ""
	}
	return url
}
