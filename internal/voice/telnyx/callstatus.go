package telnyx

// Ported from uptractor phone/core/internal/telnyx/callstatus.go (branch
// fix/coordinator-missed-hangup-watchdog), adapted to meept's Client shape
// (postCall's bearer/snippet conventions). Contract C1.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// CallStatus is what Telnyx reports about one call. Known is false when
// Telnyx no longer has the call at all (404): that call is over.
type CallStatus struct {
	Known bool
	Alive bool
}

// GetCallStatus asks Telnyx whether a call is still alive:
// GET /calls/{call_control_id}, reading data.is_alive. It is the
// authority the call reaper uses when a hangup webhook never arrives.
// A 404 means Telnyx has forgotten the call (it ended long ago) and is
// reported as Known=false, Alive=false rather than as an error. Any
// other non-2xx, a body without is_alive, or a decode failure is an
// error (never a verdict): callers retry on the next sweep.
func (c *Client) GetCallStatus(ctx context.Context, callControlID string) (CallStatus, error) {
	if callControlID == "" {
		return CallStatus{}, errors.New("telnyx call status: empty call_control_id")
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(base, "/")+"/calls/"+url.PathEscape(callControlID), nil)
	if err != nil {
		return CallStatus{}, fmt.Errorf("telnyx call status: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	hc := c.httpClient
	if hc == nil {
		hc = defaultHTTPClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return CallStatus{}, fmt.Errorf("telnyx call status: %w", err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return CallStatus{}, fmt.Errorf("telnyx call status: read response: %w", readErr)
	}
	if resp.StatusCode == http.StatusNotFound {
		// Telnyx has forgotten the call: a verdict, not an error.
		return CallStatus{Known: false, Alive: false}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Surface a body snippet (never headers, so the bearer token
		// cannot leak) as in postCall.
		return CallStatus{}, fmt.Errorf("telnyx call status: status %d: %s", resp.StatusCode, snippet(raw))
	}
	var out struct {
		Data struct {
			IsAlive *bool `json:"is_alive"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return CallStatus{}, fmt.Errorf("telnyx call status: decode: %w", err)
	}
	if out.Data.IsAlive == nil {
		// No verdict: do not guess. Callers treat an error as "try again".
		return CallStatus{}, errors.New("telnyx call status: response has no is_alive")
	}
	return CallStatus{Known: true, Alive: *out.Data.IsAlive}, nil
}

// isCallEventType reports whether t is in the "call." family.
func isCallEventType(t string) bool { return strings.HasPrefix(t, "call.") }
