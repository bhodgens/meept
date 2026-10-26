//go:build e2e

package httpauth

// http-auth-04: the terminal/exec guard-block contract (security audit
// accepted finding). A command refused by a security guard BEFORE
// execution (blocked-command list, fence, permission table, scanner) must
// answer 403 — an authorization outcome — not 500. The PTY endpoint
// already answers fence rejections with 403; REST must not diverge. The
// benign control proves the endpoint itself works, so the 403 is specific
// to the guard, not a broken route.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// postExec POSTs a JSON body to /api/v1/terminal/exec and returns the
// status plus the decoded body (loose map: CommandHistory carries numbers).
func postExec(t *testing.T, s *harness.Stack, body string) (int, map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.HTTPBaseURL()+"/api/v1/terminal/exec", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("http-auth-04: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testKey)
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("http-auth-04: post: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	return resp.StatusCode, fields
}

func TestTerminalExecGuardBlockedIsForbiddenNotInternal(t *testing.T) {
	s := start(t)

	// Guard-blocked: `rm` is on the always-denied list, so the risk
	// classifier refuses it before any exec. Pre-fix this surfaced as 500
	// "internal server error" through handleServiceError's default arm.
	code, fields := postExec(t, s, `{"command":"rm -rf /nonexistent-e2e-guard-path"}`)
	if code != http.StatusForbidden {
		t.Fatalf("http-auth-04: guard-blocked status = %d, want 403 (body: %v)", code, fields)
	}
	msg, _ := fields["error"].(string)
	if !strings.Contains(msg, "blocked") {
		t.Fatalf("http-auth-04: guard-blocked error = %q, want it to name the block", msg)
	}

	// Benign control: the same endpoint runs an allowed command normally,
	// proving the 403 above is the guard, not a broken route.
	code, fields = postExec(t, s, `{"command":"echo e2e-guard-ok"}`)
	if code != http.StatusOK {
		t.Fatalf("http-auth-04: benign status = %d, want 200 (body: %v)", code, fields)
	}
	// The history entry reports success (CommandHistory omits empty
	// output; the marker assertion would pin unrelated behavior).
	if ok, _ := fields["success"].(bool); !ok {
		t.Fatalf("http-auth-04: benign entry not successful: %v", fields)
	}
	if code, _ := fields["exit_code"].(float64); code != 0 {
		t.Fatalf("http-auth-04: benign exit_code = %v, want 0", fields["exit_code"])
	}
}
