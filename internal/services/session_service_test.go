package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

// newTestSessionService constructs a SessionService backed by an in-memory
// session store for unit testing.
func newTestSessionService(t *testing.T) *SessionService {
	t.Helper()
	store := session.NewMemoryStore(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewSessionService(store)
}

func TestSessionServiceArchiveSession(t *testing.T) {
	svc := newTestSessionService(t)

	sess, err := svc.CreateSession(context.Background(), CreateSessionRequest{Name: "to-archive"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: sess.ID, Archived: true}); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}

	got, err := svc.GetSession(context.Background(), GetSessionRequest{ID: sess.ID})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !got.Archived {
		t.Fatalf("expected Archived=true, got false")
	}

	// Unarchive round-trip
	if err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: sess.ID, Archived: false}); err != nil {
		t.Fatalf("ArchiveSession unarchive: %v", err)
	}
	got, _ = svc.GetSession(context.Background(), GetSessionRequest{ID: sess.ID})
	if got.Archived {
		t.Fatalf("expected Archived=false after unarchive, got true")
	}
}

func TestSessionServiceArchiveSession_NotFound(t *testing.T) {
	svc := newTestSessionService(t)

	err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: "nonexistent", Archived: true})
	if err == nil {
		t.Fatalf("expected error for nonexistent session, got nil")
	}
	// Verify it maps to ErrNotFound for HTTP 404 handling.
	if !isServiceError(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound mapping, got: %v", err)
	}
}

func TestSessionServiceArchiveSession_InvalidInput(t *testing.T) {
	svc := newTestSessionService(t)

	err := svc.ArchiveSession(context.Background(), ArchiveSessionRequest{ID: "", Archived: true})
	if err == nil {
		t.Fatalf("expected error for empty ID, got nil")
	}
	if !isServiceError(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput mapping, got: %v", err)
	}
}

// isServiceError reports whether err is a *ServiceError wrapping target via
// errors.Is. This avoids duplicating error-text checks across tests.
func isServiceError(err, target error) bool {
	se, ok := err.(*ServiceError)
	if !ok {
		return false
	}
	return errors.Is(se, target)
}
