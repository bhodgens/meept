package telnyx

// Gate tests for contract C1 (master.md, quoted verbatim):
//
//	C1 CallStatus: func (c *Client) GetCallStatus(ctx context.Context,
//	callControlID string) (CallStatus, error); type CallStatus struct {
//	Known, Alive bool }. 404 => Known=false, Alive=false, err=nil.
//	Empty id => error.
//
// Ported verbatim in shape from uptractor
// phone/core/internal/telnyx/callstatus_test.go (branch
// fix/coordinator-missed-hangup-watchdog); client constructor adapted to
// meept's NewClient(baseURL, apiKey). RED until leaf 01 (GetCallStatus +
// CallStatus exist).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// statusServer mounts h on an httptest server and returns a Client whose
// BaseURL points at it (same pattern the existing client_test.go uses).
func statusServer(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "test-key")
}

// TestCallStatus probes one live call through an httptest fake. Given: the
// carrier answers GET /calls/cc-1 with the table's status code and body.
// When: GetCallStatus. Then: the CallStatus struct is exactly `want` and
// error presence matches wantErr. The request must be a GET on
// /calls/cc-1 carrying bearer auth.
func TestCallStatus(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		body    string
		want    CallStatus
		wantErr bool
	}{
		// 200 alive: Known=true, Alive=true, nil error.
		{"alive", 200, `{"data":{"is_alive":true}}`, CallStatus{Known: true, Alive: true}, false},
		// 200 dead: Telnyx still knows the call but it is over.
		{"ended", 200, `{"data":{"is_alive":false}}`, CallStatus{Known: true, Alive: false}, false},
		// 404: Telnyx has forgotten the call — NOT an error, a verdict
		// (Known=false, Alive=false, err=nil). This distinction is the
		// whole point of the probe.
		{"forgotten by telnyx", 404, `{"errors":[{"code":"90018"}]}`, CallStatus{}, false},
		// Server error is not a verdict: the reaper treats an error as
		// "try again next sweep".
		{"server error is not a verdict", 500, `oops`, CallStatus{}, true},
		// Missing is_alive is not a verdict either: never guess.
		{"no is_alive is not a verdict", 200, `{"data":{}}`, CallStatus{}, true},
		// Garbage body is a decode error, not a verdict.
		{"garbage body", 200, `not json`, CallStatus{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := statusServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/calls/cc-1" {
					t.Errorf("request = %s %s, want GET /calls/cc-1", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("missing bearer auth")
				}
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			})
			got, err := c.GetCallStatus(context.Background(), "cc-1")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("status = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestCallStatusRejectsEmptyID pins the C1 clause: an empty
// call_control_id is an input error before any HTTP traffic.
func TestCallStatusRejectsEmptyID(t *testing.T) {
	c := NewClient("", "k")
	if _, err := c.GetCallStatus(context.Background(), ""); err == nil {
		t.Fatal("want an error for an empty call_control_id")
	}
}
