package employee

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestS6_AgentPlanEndpointsRemoved verifies that the deprecated agent-specific
// plan approval/rejection endpoints are no longer registered. The consolidated
// endpoints at /api/v1/plans/{pid}/approve and /api/v1/plans/{pid}/reject
// should be used instead.
func TestS6_AgentPlanEndpointsRemoved(t *testing.T) {
	t.Parallel()

	h := NewAgentAPIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "approve",
			method: "POST",
			path:   "/api/v1/agents/emp1/goals/g1/plans/p1/approve",
		},
		{
			name:   "reject",
			method: "POST",
			path:   "/api/v1/agents/emp1/goals/g1/plans/p1/reject",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Unregistered routes fall through to the default mux handler,
			// which returns 404.
			if rr.Code != http.StatusNotFound {
				t.Errorf("deprecated endpoint %s %s: want 404, got %d",
					tc.method, tc.path, rr.Code)
			}
		})
	}
}
