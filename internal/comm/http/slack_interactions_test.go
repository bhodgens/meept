package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubSlackHandler records ServeHTTP calls.
type stubSlackHandler struct {
	called int
}

func (s *stubSlackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.called++
	w.WriteHeader(http.StatusOK)
}

// TestSlackInteractionsRouteExemptFromBearerAuth pins the hitl-notifications
// leaf 01 registration contract: with the handler wired, POST
// /api/v1/slack/interactions answers WITHOUT a bearer API key (Slack cannot
// present one; the v0 request signature enforced inside the handler is the
// credential).
func TestSlackInteractionsRouteExemptFromBearerAuth(t *testing.T) {
	stub := &stubSlackHandler{}
	s := &Server{logger: slog.Default()}
	WithSlackInteractions(stub)(s)

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/slack/interactions", nil)
	// No Authorization header at all.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("route without bearer key: got %d, want 200", rec.Code)
	}
	if stub.called != 1 {
		t.Fatalf("handler called %d times, want 1", stub.called)
	}
}

// TestSlackInteractionsRouteAbsentWhenUnwired pins the default-off route
// invariant: with NO handler wired the route does not exist at all (404),
// never an always-on endpoint.
func TestSlackInteractionsRouteAbsentWhenUnwired(t *testing.T) {
	s := &Server{logger: slog.Default()} // no WithSlackInteractions
	mux := http.NewServeMux()
	s.setupRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/slack/interactions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unwired route: got %d, want 404", rec.Code)
	}
}
