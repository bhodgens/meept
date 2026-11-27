package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ptrBool is a small helper for the resolved *bool assertions.
func ptrBool(b bool) *bool { return &b }

// TestActivityFindingsResolvedParam pins the boundary parsing of the
// `resolved` query param (bughunt A1-15). Before the fix the handler did
// `resolvedParam == "true"`, so "1", "t", "true" all parsed and any other
// value (e.g. "yes") was silently treated as false. The fix uses
// strconv.ParseBool and returns 400 for unparseable values, matching how the
// other params reject malformed values.
func TestActivityFindingsResolvedParam(t *testing.T) {
	tests := []struct {
		name     string
		param    string
		wantCode int
		wantRes  *bool
	}{
		{"true", "true", http.StatusOK, ptrBool(true)},
		{"false", "false", http.StatusOK, ptrBool(false)},
		{"one", "1", http.StatusOK, ptrBool(true)},
		{"zero", "0", http.StatusOK, ptrBool(false)},
		{"yes-is-400", "yes", http.StatusBadRequest, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeRPCCall{Result: map[string]any{"findings": []any{}, "total": 0}}
			s := &Server{rpcCall: fake.Call}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/activity/findings?resolved="+tt.param, nil)
			w := httptest.NewRecorder()
			s.handleActivityFindings(w, req)

			if w.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d (body=%q)", w.Code, tt.wantCode, w.Body.String())
			}
			if tt.wantCode != http.StatusOK {
				// A 400 must not reach the RPC.
				if fake.Method != "" {
					t.Errorf("rpcCall was invoked with method=%s, want not invoked", fake.Method)
				}
				return
			}
			var params struct {
				Resolved *bool `json:"resolved,omitempty"`
			}
			if err := json.Unmarshal(fake.Params, &params); err != nil {
				t.Fatalf("params unmarshal: %v", err)
			}
			if params.Resolved == nil || *params.Resolved != *tt.wantRes {
				t.Errorf("resolved = %v, want %v", params.Resolved, tt.wantRes)
			}
		})
	}
}
