package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
)

func newArchiveRPCTestServer(t *testing.T) (*rpc.Server, *services.SessionService) {
	t.Helper()
	store := session.NewMemoryStore(slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := services.NewSessionService(store)
	// rpc.New dereferences cfg (cfg.Shutdown), so pass a non-nil zero Config.
	srv := rpc.New(&rpc.Config{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	registerSessionRPCHandlers(srv, svc)
	return srv, svc
}

func TestRPC_SessionsArchive(t *testing.T) {
	srv, svc := newArchiveRPCTestServer(t)

	created, err := svc.CreateSession(context.Background(), services.CreateSessionRequest{Name: "rpc-archive-test"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	params, _ := json.Marshal(map[string]any{"id": created.ID, "archived": true})
	resp, err := srv.CallMethod(context.Background(), "sessions.archive", params)
	if err != nil {
		t.Fatalf("sessions.archive: %v", err)
	}
	respMap, ok := resp.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", resp)
	}
	if got, _ := respMap["status"].(string); got != "archived" {
		t.Fatalf("expected status=archived, got %v", respMap["status"])
	}

	// Verify via the service that the flag persisted.
	got, err := svc.GetSession(context.Background(), services.GetSessionRequest{ID: created.ID})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !got.Archived {
		t.Fatalf("expected Archived=true, got false")
	}
}

func TestRPC_SessionsArchive_MissingID(t *testing.T) {
	srv, _ := newArchiveRPCTestServer(t)

	params, _ := json.Marshal(map[string]any{"archived": true})
	_, err := srv.CallMethod(context.Background(), "sessions.archive", params)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestRPC_SessionsArchive_NotFound(t *testing.T) {
	srv, _ := newArchiveRPCTestServer(t)

	params, _ := json.Marshal(map[string]any{"id": "nonexistent", "archived": true})
	_, err := srv.CallMethod(context.Background(), "sessions.archive", params)
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
}
