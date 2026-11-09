package contact

import (
	"context"
	"strings"
)

// Resolve looks a contact up by whichever key the caller has (leaf 02
// T1): exact record id first, then normalized E.164 phone, then Signal
// uuid. ErrUnknownContact when none match. An empty key refuses without
// a store round-trip.
func (s *SQLiteStore) Resolve(ctx context.Context, key string) (Record, error) {
	key = strings.TrimSpace(key)
	if key == "" || s == nil || s.db == nil {
		return Record{}, ErrUnknownContact
	}
	if rec, err := s.Get(ctx, key); err == nil {
		return rec, nil
	}
	if rec, err := s.GetByPhone(ctx, NormalizeE164(key)); err == nil {
		return rec, nil
	}
	return s.GetBySignalUUID(ctx, key)
}
