package employee

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoalsSetGateRouteRegistered(t *testing.T) {
	t.Parallel()
	h := NewAgentAPIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/emp1/goals/g1/gate", strings.NewReader(`{"command":"go test ./..."}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("PUT .../gate is not registered")
	}
	// rpc is nil → 503 service unavailable, which still proves the route exists.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (nil rpc)", rec.Code)
	}
}
