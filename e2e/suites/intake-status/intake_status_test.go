//go:build e2e

// Suite intake-status: hermetic live gate for the intake.status parity
// surface (issue #109 TUI/GUI). A real meept-daemon boots against a fake
// GitHub (httptest, copied request-shape from the github-intake suite),
// [intake] enabled, HTTP auth required with one scratch Bearer key — the
// same auth pattern the http-auth suite pins. The suite asserts:
//
//	intake-status-01  GET /api/v1/intake/status over the HTTP bridge with
//	                  the Bearer key returns 200 and the frozen cross-
//	                  surface JSON shape the TUI (internal/tui/
//	                  intake_view.go) and the Flutter GUI
//	                  (ui/flutter_ui/lib/features/intake) consume:
//	                  enabled, watch_repo, poll_interval, required_labels,
//	                  done_label, default_employee, gate_command,
//	                  project_number, poller_scheduled, note. With
//	                  [intake] enabled + valid, the poll job is scheduled
//	                  (poller_scheduled true). No Authorization -> 401.
//	intake-status-02  the disabled default: no [intake] block -> the
//	                  bridge still answers 200 with the disabled envelope
//	                  (enabled false, poller_scheduled false, a note) —
//	                  never 404/500/"method not found". Same posture the
//	                  nil-handler registration test pins at unit level.
//
// The HTTP route is the GUI's transport (the Flutter client only speaks
// HTTP through SdkApiClient), so the assertions drive the bridge rather
// than the Unix socket — the socket-side method is already pinned by the
// github-intake suite's pollers and internal/rpc unit tests.
//
// Run via `make e2e-fast-area AREA=intake-status`.
package intakestatus

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

const scratchKey = "e2e-intake-status-scratch-key"

// frozenFields lists the intake.status contract (rpc.IntakeStatusView).
// A rename on any surface (daemon/TUI/GUI) fails these pins on purpose.
var frozenFields = []string{
	"enabled",
	"watch_repo",
	"poll_interval",
	"required_labels",
	"done_label",
	"default_employee",
	"gate_command",
	"project_number",
	"poller_scheduled",
	"note",
}

// startAuthed boots the sandbox with HTTP auth required and one valid key.
func startAuthed(t *testing.T, extra map[string]any) *harness.Stack {
	t.Helper()
	overlay := map[string]any{
		"transport.http.require_auth": true,
		"transport.http.api_keys":     []string{scratchKey},
	}
	for k, v := range extra {
		overlay[k] = v
	}
	return harness.Start(t, harness.WithConfigOverlay(overlay))
}

// get hits path with the given Authorization value ("" omits the header).
func get(t *testing.T, s *harness.Stack, path, authHeader string) (int, map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.HTTPBaseURL()+path, nil)
	if err != nil {
		t.Fatalf("intake-status: build request: %v", err)
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("intake-status: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		fields = nil
	}
	return resp.StatusCode, fields
}

// writeTokenFile stages a fake [github] token file for the enabled rig.
func writeTokenFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "github_token")
	if err := os.WriteFile(p, []byte("ghp_intake_status_test_token"), 0o600); err != nil {
		t.Fatalf("write github token file: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// intake-status-01 (M): enabled [intake] -> the bridge answers the full
// frozen shape with the poll job scheduled; missing auth -> 401.
// ---------------------------------------------------------------------------

func TestIntakeStatusBridgeEnabledShape(t *testing.T) {
	// An httptest GitHub keeps the enabled rig hermetic (no live github.com
	// dial even if the 2s poll fires while we assert).
	fake := newFakeGitHub(t)
	s := startAuthed(t, map[string]any{
		"github.enabled":          true,
		"github.base_url":         fake.URL(),
		"github.token_secret":     writeTokenFile(t),
		"intake.enabled":          true,
		"intake.default_employee": "intake-status-emp",
		"intake.poll_interval":    "2s",
		"intake.watch_repo":       "octo/forge",
		"intake.required_labels":  []any{"factory"},
		"intake.done_label":       "factory:done",
		"intake.gate_command":     "go test ./...",
	})

	code, fields := get(t, s, "/api/v1/intake/status", "Bearer "+scratchKey)
	if code != http.StatusOK {
		t.Fatalf("intake-status-01: status = %d, want 200 (%v)", code, fields)
	}
	if fields == nil {
		t.Fatal("intake-status-01: body is not a JSON object")
	}
	for _, f := range frozenFields {
		if _, ok := fields[f]; !ok {
			t.Errorf("intake-status-01: frozen field %q missing from %v", f, fields)
		}
	}
	if got, _ := fields["enabled"].(bool); !got {
		t.Errorf("intake-status-01: enabled = %v, want true", fields["enabled"])
	}
	if got, _ := fields["watch_repo"].(string); got != "octo/forge" {
		t.Errorf("intake-status-01: watch_repo = %q, want %q", got, "octo/forge")
	}
	if got, _ := fields["default_employee"].(string); got != "intake-status-emp" {
		t.Errorf("intake-status-01: default_employee = %q, want %q", got, "intake-status-emp")
	}
	if got, _ := fields["poll_interval"].(string); got != "2s" {
		t.Errorf("intake-status-01: poll_interval = %q, want %q", got, "2s")
	}
	if got, _ := fields["gate_command"].(string); got != "go test ./..." {
		t.Errorf("intake-status-01: gate_command = %q, want %q", got, "go test ./...")
	}
	labels, _ := fields["required_labels"].([]any)
	if len(labels) != 1 || labels[0] != "factory" {
		t.Errorf("intake-status-01: required_labels = %v, want [factory]", fields["required_labels"])
	}
	// The daemon scheduled "intake_poller" during boot, so the probe the
	// handler was wired with (Components.Scheduler.GetJob) reports true.
	// This is the GUI-side signal the TUI prints as "poll job: scheduled".
	if got, _ := fields["poller_scheduled"].(bool); !got {
		t.Errorf("intake-status-01: poller_scheduled = %v, want true for an enabled+valid [intake]", fields["poller_scheduled"])
	}

	// Auth middleware applies to the new route like every other protected
	// endpoint: missing header -> 401 (never an unauthenticated snapshot).
	code, fields = get(t, s, "/api/v1/intake/status", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("intake-status-01: missing-auth status = %d, want 401 (%v)", code, fields)
	}
}

// ---------------------------------------------------------------------------
// intake-status-02 (S): disabled default -> 200 with the disabled envelope,
// never "method not found".
// ---------------------------------------------------------------------------

func TestIntakeStatusBridgeDisabledEnvelope(t *testing.T) {
	s := startAuthed(t, nil)

	code, fields := get(t, s, "/api/v1/intake/status", "Bearer "+scratchKey)
	if code != http.StatusOK {
		t.Fatalf("intake-status-02: status = %d, want 200 disabled envelope (%v)", code, fields)
	}
	if fields == nil {
		t.Fatal("intake-status-02: body is not a JSON object")
	}
	if got, _ := fields["enabled"].(bool); got {
		t.Errorf("intake-status-02: enabled = %v, want false (no [intake] block)", fields["enabled"])
	}
	if got, _ := fields["poller_scheduled"].(bool); got {
		t.Errorf("intake-status-02: poller_scheduled = %v, want false", fields["poller_scheduled"])
	}
	note, _ := fields["note"].(string)
	if note == "" {
		t.Errorf("intake-status-02: note empty, want the disabled-state explanation")
	}
}
