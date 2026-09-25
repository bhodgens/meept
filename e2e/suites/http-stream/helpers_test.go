//go:build e2e

// Notifications WebSocket helper for the http-stream suite: the live-push
// leg of scenario http-stream-02.
package httpstream

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
)

// dialNotificationsWS opens /ws/notifications (no bearer needed: the
// sandbox runs with require_auth=false) and returns the live connection.
func dialNotificationsWS(t *testing.T, s *harness.Stack) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// HTTPBaseURL is https://host:port; the WS endpoint is wss:// on the
	// same TLS listener.
	wsURL := "wss" + strings.TrimPrefix(s.HTTPBaseURL(), "https") + "/ws/notifications"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			},
		},
	})
	if err != nil {
		t.Fatalf("http-stream: dial /ws/notifications: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "e2e done") })
	return conn
}
