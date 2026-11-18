package sms

import (
	"context"
	"testing"
)

// A whole-second DLR stored first, then a same-second FRACTIONAL DLR, must
// apply. RFC3339Nano trims trailing zeros, so the raw strings compare
// "...05.5Z" < "...05Z" even though the fractional stamp is NEWER.
func TestUpsertState_FractionalStampAfterWholeSecondApplies(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	id := "dlr-fractional"
	dst := phone("0009")

	state := func(stamp, status, eventID string) State {
		return State{
			ProviderMessageID: id,
			Dst:               dst,
			Status:            status,
			UpdatedAt:         stamp,
			EventID:           eventID,
		}
	}

	if applied, err := s.UpsertState(ctx, state("2026-11-17T12:00:05Z", "delivered", "evt-whole")); err != nil || !applied {
		t.Fatalf("first upsert: applied=%v err=%v", applied, err)
	}
	// Newer by instant, older as a raw string.
	applied, err := s.UpsertState(ctx, state("2026-11-17T12:00:05.5Z", "failed", "evt-fraction"))
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if !applied {
		t.Fatal("newer fractional DLR was discarded as older by string compare")
	}

	var status, eventID string
	if err := s.db.QueryRowContext(ctx,
		"SELECT status, event_id FROM sms_message_state WHERE provider_message_id = ?", id,
	).Scan(&status, &eventID); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if status != "failed" || eventID != "evt-fraction" {
		t.Fatalf("state = (%q, %q), want (failed, evt-fraction)", status, eventID)
	}

	// A genuinely older fractional stamp is still rejected.
	applied, err = s.UpsertState(ctx, state("2026-11-17T12:00:05.12Z", "queued", "evt-older"))
	if err != nil || applied {
		t.Fatalf("older fractional upsert: applied=%v err=%v (want false)", applied, err)
	}
}
