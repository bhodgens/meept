package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/runtime"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/internal/tui/modals"
)

// discardLogger keeps test output free of daemon log noise.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestMain silences the GLOBAL slog default so production code paths that
// log via slog.Warn/slog.Info directly (e.g. ResolveTool's drift refusal)
// do not pollute test output. Test-binary scope only.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Test 1: env isolation under real backend execution.
// ---------------------------------------------------------------------------

// TestEnvStrippedThroughBackendExecution proves the allowlist env policy
// strips daemon secrets on the way to a REAL child process (not just in
// BuildChildEnv unit isolation): printenv for the sentinel comes back empty
// and exits non-zero, while BuildChildEnv reports the name in `stripped`.
func TestEnvStrippedThroughBackendExecution(t *testing.T) {
	const sentinelName = "MEEPT_SENTINEL_SECRET"

	parentEnv := []string{
		"PATH=" + os.Getenv("PATH"), // base key; must still pass through
		"HOME=" + t.TempDir(),
		sentinelName + "=topsecret",
	}
	policy := runtime.EnvPolicyConfig{Mode: runtime.EnvModeAllowlist}

	be := runtime.NewLocalBackend(runtime.Config{EnvPolicy: policy}, parentEnv)
	t.Cleanup(func() { _ = be.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := be.Execute(ctx, runtime.Command{
		Cmd:     "printenv " + sentinelName,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute(printenv %s) failed: %v", sentinelName, err)
	}
	if got := strings.TrimSpace(res.Output); got != "" {
		t.Fatalf("child saw leaked secret: printenv output = %q, want empty", got)
	}
	// printenv exits 1 for an unset variable — the strongest signal the
	// name never reached the child at all.
	if res.ExitCode != 1 {
		t.Errorf("printenv exit code = %d, want 1 (variable unset in child)", res.ExitCode)
	}

	// Control: base-key PATH must still reach the child (allowlist is not
	// an empty-env bug).
	res, err = be.Execute(ctx, runtime.Command{Cmd: "printenv PATH", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute(printenv PATH) failed: %v", err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Output) == "" {
		t.Errorf("PATH did not reach child: exit=%d output=%q", res.ExitCode, res.Output)
	}

	// BuildChildEnv contract: the sentinel NAME is reported in stripped and
	// absent from the constructed env.
	env, stripped := runtime.BuildChildEnv(policy, parentEnv, nil)
	foundStripped := false
	for _, name := range stripped {
		if name == sentinelName {
			foundStripped = true
		}
	}
	if !foundStripped {
		t.Errorf("BuildChildEnv stripped = %v, want it to include %q", stripped, sentinelName)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, sentinelName+"=") {
			t.Errorf("BuildChildEnv env still contains %s entry %q", sentinelName, entry)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: secret placeholder round-trip through the egress proxy.
// ---------------------------------------------------------------------------

// TestSecretPlaceholderRoundTrip proves the broker+proxy pair end-to-end:
// a placeholder header/body routed toward an allowlisted host arrives at the
// upstream rewritten to Format(realValue); toward a non-allowlisted host it
// passes through UNTOUCHED and the leak-attempt counter increments. It also
// proves BuildChildEnv lets placeholder-form values bypass deny globs, so
// children can carry MEEPT_SECRET:<name> tokens to the proxy.
//
// DEVIATION NOTE: internal/secrets.Proxy.rewriteTransport (the downstream
// transport seam used by package-internal tests to retarget outbound
// requests at a local stub) is unexported, so a cross-package test cannot
// route a fake hostname like "example.test" to a local upstream. The
// round-trip source therefore additionally allowlists "127.0.0.1" so the
// real proxy forwarding path can deliver to the httptest upstream over
// loopback; the mismatch arm still uses the specified hosts ["example.test"].
func TestSecretPlaceholderRoundTrip(t *testing.T) {
	const (
		realToken  = "REALTOKEN-abc123"
		matchName  = "apitoken"
		mismatchNm = "exampletok"
	)

	t.Setenv("MEEPT_IT_TOKEN", realToken)

	broker, err := secrets.NewBroker(secrets.Config{
		matchName: {
			Kind: "env",
			Name: "MEEPT_IT_TOKEN",
			// "example.test" per the containment contract; "127.0.0.1"
			// added because the transport seam is package-internal (see
			// deviation note above) and the loopback upstream is the only
			// host a cross-package test can actually reach.
			Hosts:  []string{"example.test", "127.0.0.1"},
			Header: "Authorization",
			Format: "Bearer {}",
		},
		mismatchNm: {
			Kind:   "env",
			Name:   "MEEPT_IT_TOKEN",
			Hosts:  []string{"example.test"},
			Header: "Authorization",
			Format: "Bearer {}",
		},
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	// Upstream that echoes what it actually received: Authorization in a
	// response header, request body in the response body.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Saw-Authorization", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(upstream.Close)
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	proxy := secrets.NewProxy(broker, secrets.ProxyConfig{Enabled: true, Listen: "127.0.0.1:0"}, discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxyAddr, err := proxy.Start(ctx)
	if err != nil {
		t.Fatalf("proxy Start: %v", err)
	}
	t.Cleanup(proxy.Stop)

	proxyURL, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		t.Fatalf("parse proxy addr: %v", err)
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}

	// Arm 1 (header, matched host): placeholder replaced with Format(real).
	req, err := http.NewRequest(http.MethodGet, "http://"+upstreamHost+"/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", secrets.Placeholder(matchName))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("matched-host request through proxy failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("matched-host status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Saw-Authorization"); got != "Bearer "+realToken {
		t.Errorf("upstream Authorization = %q, want %q", got, "Bearer "+realToken)
	}
	if got := resp.Header.Get("X-Saw-Authorization"); strings.Contains(got, secrets.PlaceholderPrefix) {
		t.Errorf("placeholder leaked to upstream: %q", got)
	}

	// Arm 2 (body, matched host): placeholder in a scannable body replaced.
	bodyPayload := "token=" + secrets.Placeholder(matchName) + "\n"
	req, err = http.NewRequest(http.MethodPost, "http://"+upstreamHost+"/", strings.NewReader(bodyPayload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("body request through proxy failed: %v", err)
	}
	gotBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if want := "token=Bearer " + realToken + "\n"; string(gotBody) != want {
		t.Errorf("upstream body = %q, want %q", gotBody, want)
	}

	// Arm 3 (mismatched host): placeholder passes through untouched AND the
	// leak-attempt counter increments.
	before := proxy.LeakAttempts()
	req, err = http.NewRequest(http.MethodGet, "http://"+upstreamHost+"/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", secrets.Placeholder(mismatchNm))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("mismatched-host request through proxy failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got := resp.Header.Get("X-Saw-Authorization"); got != secrets.Placeholder(mismatchNm) {
		t.Errorf("mismatched host saw %q, want untouched placeholder %q", got, secrets.Placeholder(mismatchNm))
	}
	if got := proxy.LeakAttempts(); got != before+1 {
		t.Errorf("LeakAttempts() = %d, want %d (one mismatched request)", got, before+1)
	}

	// Arm 4 (child env): placeholder-form values bypass deny globs so the
	// child can carry the token to the proxy without the real value ever
	// entering its environment.
	policy := runtime.EnvPolicyConfig{
		Mode:      runtime.EnvModeAllowlist,
		DenyGlobs: []string{"*SECRET*", "*TOKEN*"},
	}
	env, stripped := runtime.BuildChildEnv(policy, nil, map[string]string{
		"API_TOKEN": secrets.Placeholder(matchName),
	})
	wantEntry := "API_TOKEN=" + secrets.Placeholder(matchName)
	found := false
	for _, entry := range env {
		if entry == wantEntry {
			found = true
		}
	}
	if !found {
		t.Errorf("BuildChildEnv env = %v, want it to include %q despite deny globs", env, wantEntry)
	}
	for _, name := range stripped {
		if name == "API_TOKEN" {
			t.Errorf("BuildChildEnv stripped API_TOKEN; placeholder values must never be stripped")
		}
	}
}

// ---------------------------------------------------------------------------
// Test 3: fail-closed sandbox refusal.
// ---------------------------------------------------------------------------

// TestSandboxRefusalFailsClosed proves RequireSandbox=true fails CLOSED when
// no qualifying backend is available: ResolveBackend returns
// ErrSandboxRequired rather than silently degrading to local exec, and
// Qualifies never credits "local" as confinement.
//
// On darwin/non-linux bwrap is provably unavailable (the resolver probe
// checks GOOS before PATH), so the refusal is deterministic. On a linux host
// where the bwrap binary IS installed the public API cannot force
// unavailability, so that specific arm skips with a reason instead of
// asserting non-deterministically.
func TestSandboxRefusalFailsClosed(t *testing.T) {
	if goruntime.GOOS == "linux" {
		if _, err := exec.LookPath("bwrap"); err == nil {
			t.Skip("bwrap binary available on linux; cannot force unavailability via public API")
		}
	}

	mgr, err := runtime.NewContainerManager(runtime.Config{}, discardLogger())
	if err != nil {
		t.Fatalf("NewContainerManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	be, err := runtime.ResolveBackend(mgr, runtime.ResolverConfig{
		Order:          runtime.SandboxOrderBwrap,
		RequireSandbox: true,
	}, discardLogger())
	if err == nil {
		_ = be.Close()
		t.Fatal("ResolveBackend returned a backend, want ErrSandboxRequired refusal")
	}
	if !errors.Is(err, runtime.ErrSandboxRequired) {
		t.Fatalf("ResolveBackend error = %v, want errors.Is(err, ErrSandboxRequired)", err)
	}
	// Contract wording: the refusal must name the missing sandbox, so
	// operator logs are unambiguous about WHY execution was refused.
	if !strings.Contains(err.Error(), "sandbox required") {
		t.Errorf("refusal message %q does not contain contract wording %q", err.Error(), "sandbox required")
	}

	if runtime.Qualifies("local") {
		t.Error("Qualifies(\"local\") = true, want false (local exec is not confinement)")
	}
	if !runtime.Qualifies("bwrap") || !runtime.Qualifies("docker") {
		t.Error("Qualifies must credit bwrap and docker as confinement backends")
	}
}

// ---------------------------------------------------------------------------
// Tests 4-5: stage -> accept -> journal -> revert chain, and surface parity.
// ---------------------------------------------------------------------------

// newTestJournal opens a journal DB inside t.TempDir and closes it on exit.
func newTestJournal(t *testing.T) *builtin.Journal {
	t.Helper()
	j, err := builtin.NewJournal(builtin.JournalConfig{
		DBPath: filepath.Join(t.TempDir(), "changes.db"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Errorf("journal Close: %v", err)
		}
	})
	return j
}

// writeFile is a test helper for deterministic on-disk state.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatalf("write %s: %v", path, err)
	}
}

// readFile is the counterpart of writeFile.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestStageAcceptJournalRevertChain proves the full containment chain:
// StageWrite -> external drift -> accept REFUSED with ErrChangeDrift ->
// restore -> accept succeeds and journals -> journal.List shows the entry ->
// Revert restores the original bytes -> a second Revert is an idempotent
// no-op. ResolveTool is wired with the journal via SetJournal so the shared
// accept path records the applied change.
func TestStageAcceptJournalRevertChain(t *testing.T) {
	const (
		sessionID = "sess-chain"
		original  = "line one\nline two\n"
		modified  = "line one\nline TWO (edited)\n"
		drifted   = "someone else touched this file\n"
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	writeFile(t, path, original)

	registry := builtin.NewPendingChangesRegistry()
	journal := newTestJournal(t)
	resolve := builtin.NewResolveTool(registry)
	resolve.SetJournal(journal)

	change, err := registry.StageWrite(sessionID, path, []byte(original), []byte(modified))
	if err != nil {
		t.Fatalf("StageWrite: %v", err)
	}

	// 1. External drift: the file changes after staging -> accept refuses.
	writeFile(t, path, drifted)
	gotPath, err := resolve.AcceptChange(change.ID)
	if !errors.Is(err, builtin.ErrChangeDrift) {
		t.Fatalf("accept after drift: err = %v, want errors.Is(err, ErrChangeDrift)", err)
	}
	if !strings.Contains(err.Error(), "file changed since staging") {
		t.Errorf("drift refusal message %q missing contract wording %q", err.Error(), "file changed since staging")
	}
	if gotPath != path {
		t.Errorf("drift refusal returned path %q, want %q", gotPath, path)
	}
	if got := readFile(t, path); got != drifted {
		t.Errorf("drifted file was overwritten: %q", got)
	}
	if _, ok := registry.Get(change.ID); !ok {
		t.Fatal("drifted change was removed from registry; it must stay staged for re-resolution")
	}

	// 2. Restore original bytes -> accept succeeds and applies the edit.
	writeFile(t, path, original)
	gotPath, err = resolve.AcceptChange(change.ID)
	if err != nil {
		t.Fatalf("accept after restore: %v", err)
	}
	if gotPath != path {
		t.Errorf("accept returned path %q, want %q", gotPath, path)
	}
	if got := readFile(t, path); got != modified {
		t.Errorf("file after accept = %q, want modified content %q", got, modified)
	}
	if _, ok := registry.Get(change.ID); ok {
		t.Error("accepted change still present in registry")
	}

	// 3. Journal gained the entry via the SetJournal wiring.
	entries, err := journal.List(sessionID, 10)
	if err != nil {
		t.Fatalf("journal.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal.List returned %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.FilePath != path {
		t.Errorf("journal entry FilePath = %q, want %q", entry.FilePath, path)
	}
	if entry.SessionID != sessionID {
		t.Errorf("journal entry SessionID = %q, want %q", entry.SessionID, sessionID)
	}
	if len(entry.ChangeIDs) != 1 || entry.ChangeIDs[0] != change.ID {
		t.Errorf("journal entry ChangeIDs = %v, want [%s]", entry.ChangeIDs, change.ID)
	}

	// 4. Revert restores the original bytes.
	revPath, err := journal.Revert(entry.ID, nil)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if revPath != path {
		t.Errorf("Revert returned path %q, want %q", revPath, path)
	}
	if got := readFile(t, path); got != original {
		t.Errorf("file after revert = %q, want original %q", got, original)
	}

	// 5. Second Revert is idempotent: file already at pre-image state.
	revPath, err = journal.Revert(entry.ID, nil)
	if err != nil {
		t.Fatalf("second Revert: %v (want idempotent success)", err)
	}
	if revPath != path {
		t.Errorf("second Revert returned path %q, want %q", revPath, path)
	}
	if got := readFile(t, path); got != original {
		t.Errorf("file after second revert = %q, want original unchanged", got)
	}
}

// registryBackedChangesAPI implements modals.PendingChangeAPI over the same
// registry/resolve tool the agent and HTTP surfaces use, standing in for the
// RPC-backed implementation in internal/tui. It proves the TUI-facing
// interface sees identical state without depending on transport internals.
type registryBackedChangesAPI struct {
	registry *builtin.PendingChangesRegistry
	resolve  *builtin.ResolveTool
}

func (a *registryBackedChangesAPI) ListPendingChanges(sessionID string) ([]modals.PendingChange, error) {
	changes := a.registry.GetBySession(sessionID)
	out := make([]modals.PendingChange, 0, len(changes))
	for _, c := range changes {
		item := modals.PendingChange{
			ID:        c.ID,
			FilePath:  c.FilePath,
			Diff:      c.Diff,
			CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if c.ExpiresAt != nil {
			item.ExpiresAt = c.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, item)
	}
	return out, nil
}

func (a *registryBackedChangesAPI) AcceptPendingChange(id string) error {
	_, err := a.resolve.AcceptChange(id)
	return err
}

func (a *registryBackedChangesAPI) RejectPendingChange(id string) error {
	if _, ok := a.registry.Get(id); !ok {
		return fmt.Errorf("pending change not found: %s", id)
	}
	a.registry.Remove(id)
	return nil
}

// TestSurfacesSeeSameState proves agent, HTTP, and TUI-facing surfaces see
// the SAME pending-change state and converge on accept:
//
//  1. a change staged via the registry is visible on GET
//     /api/v1/sessions/{sid}/pending-changes (live HTTP server) AND via a
//     modals.PendingChangeAPI implementation backed by the same registry;
//  2. accept (through the shared ResolveTool.AcceptChange path, which the
//     HTTP handler also calls) removes the change agent-side (registry.Get)
//     and adds a journal entry visible both via journal.List and GET
//     /api/v1/changes/journal;
//  3. drift surfaces as HTTP 409 per the pinned contract.
//
// Transport choice: a LIVE comm/http.Server is constructed cross-package via
// the exported NewServer + WithChangesAPI + Start surface (the package's own
// newChangesTestServer helper is unexported). Start binds 127.0.0.1:0 with a
// self-signed cert generated inside t.TempDir; readiness is established by
// bounded polling of /health (no fixed sleeps).
func TestSurfacesSeeSameState(t *testing.T) {
	const (
		sessionID = "sess-surfaces"
		original  = "alpha\n"
		modified  = "bravo\n"
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "surface.txt")
	writeFile(t, path, original)

	registry := builtin.NewPendingChangesRegistry()
	journal := newTestJournal(t)
	resolve := builtin.NewResolveTool(registry)
	resolve.SetJournal(journal)

	certFile := filepath.Join(dir, "tls-cert.pem")
	keyFile := filepath.Join(dir, "tls-key.pem")
	srv := commhttp.NewServer(commhttp.ServerConfig{
		Addr:            "127.0.0.1:0",
		RequireAuth:     false, // test-local parity check, not an auth test
		RESTEnabled:     true,
		TLSCertFile:     certFile,
		TLSKeyFile:      keyFile,
		FingerprintFile: "",
	}, nil, nil, nil, nil, discardLogger(),
		commhttp.WithChangesAPI(registry, resolve, journal))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(ctx) }()
	t.Cleanup(func() {
		_ = srv.Shutdown(context.Background())
		cancel()
		if err := <-startErr; err != nil {
			t.Errorf("server Start: %v", err)
		}
	})

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test server
		},
	}

	// Bounded readiness poll: wait for the bound addr, then for /health 200.
	// Ticker-driven (no bare sleeps); hard deadline keeps it deterministic.
	deadline := time.Now().Add(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	baseURL := ""
ready:
	for {
		if time.Now().After(deadline) {
			t.Fatal("HTTP server did not become ready within 10s")
		}
		addr := srv.Addr()
		if addr == "" || addr == "127.0.0.1:0" {
			<-ticker.C
			continue
		}
		baseURL = "https://" + addr
		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break ready
			}
		}
		<-ticker.C
	}

	doReq := func(t *testing.T, method, target string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, baseURL+target, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, target, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return resp.StatusCode, body
	}

	// --- stage one change visible to every surface ---
	change, err := registry.StageWrite(sessionID, path, []byte(original), []byte(modified))
	if err != nil {
		t.Fatalf("StageWrite: %v", err)
	}

	// HTTP surface sees it.
	type listItem struct {
		ID       string `json:"id"`
		FilePath string `json:"file_path"`
	}
	status, body := doReq(t, http.MethodGet, "/api/v1/sessions/"+sessionID+"/pending-changes")
	if status != http.StatusOK {
		t.Fatalf("GET pending-changes status = %d, body = %s", status, body)
	}
	var items []listItem
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode pending-changes list: %v (body %s)", err, body)
	}
	if len(items) != 1 || items[0].ID != change.ID || items[0].FilePath != path {
		t.Fatalf("GET pending-changes = %+v, want one item with ID %s path %s", items, change.ID, path)
	}

	// TUI-facing surface sees the same count via the modal's own command.
	tuiAPI := &registryBackedChangesAPI{registry: registry, resolve: resolve}
	modal := modals.NewPendingChangesModal(tuiAPI)
	msg := modal.FetchCount(sessionID)()
	countMsg, ok := msg.(modals.PendingChangesCountMsg)
	if !ok {
		t.Fatalf("FetchCount returned %T, want modals.PendingChangesCountMsg", msg)
	}
	if countMsg.Count != 1 {
		t.Errorf("TUI-facing count = %d, want 1 (same as HTTP surface)", countMsg.Count)
	}

	// --- accept via the shared accept path; all surfaces converge ---
	// (handleAcceptPendingChange delegates to the same AcceptChange; the
	// parity assertion below proves the registry/journal effects.)
	if _, err := resolve.AcceptChange(change.ID); err != nil {
		t.Fatalf("AcceptChange: %v", err)
	}

	if _, ok := registry.Get(change.ID); ok {
		t.Error("agent-side registry.Get still returns the accepted change")
	}

	status, body = doReq(t, http.MethodGet, "/api/v1/sessions/"+sessionID+"/pending-changes")
	if status != http.StatusOK {
		t.Fatalf("GET pending-changes after accept status = %d", status)
	}
	items = nil
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode pending-changes after accept: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("HTTP surface still lists %d changes after accept", len(items))
	}

	journalEntries, err := journal.List(sessionID, 10)
	if err != nil {
		t.Fatalf("journal.List: %v", err)
	}
	if len(journalEntries) != 1 {
		t.Fatalf("journal.List after accept = %d entries, want 1", len(journalEntries))
	}

	type journalItem struct {
		ID        string   `json:"id"`
		SessionID string   `json:"session_id"`
		FilePath  string   `json:"file_path"`
		ChangeIDs []string `json:"change_ids"`
	}
	status, body = doReq(t, http.MethodGet, "/api/v1/changes/journal?session="+sessionID+"&limit=10")
	if status != http.StatusOK {
		t.Fatalf("GET journal status = %d, body = %s", status, body)
	}
	var jItems []journalItem
	if err := json.Unmarshal(body, &jItems); err != nil {
		t.Fatalf("decode journal list: %v (body %s)", err, body)
	}
	if len(jItems) != 1 || jItems[0].ID != journalEntries[0].ID || jItems[0].FilePath != path {
		t.Errorf("GET journal = %+v, want one entry ID %s path %s", jItems, journalEntries[0].ID, path)
	}

	if got := readFile(t, path); got != modified {
		t.Errorf("file after accept = %q, want %q", got, modified)
	}

	// --- pinned contract: drift surfaces as HTTP 409 ---
	path2 := filepath.Join(dir, "drift.txt")
	writeFile(t, path2, original)
	change2, err := registry.StageWrite(sessionID, path2, []byte(original), []byte(modified))
	if err != nil {
		t.Fatalf("StageWrite (drift arm): %v", err)
	}
	writeFile(t, path2, "external mutation\n") // drift after staging

	status, body = doReq(t, http.MethodPost, "/api/v1/pending-changes/"+change2.ID+"/accept")
	if status != http.StatusConflict {
		t.Errorf("accept of drifted change: status = %d, want 409 (body %s)", status, body)
	}
	if _, ok := registry.Get(change2.ID); !ok {
		t.Error("drifted change removed from registry despite 409; it must stay staged")
	}
}
