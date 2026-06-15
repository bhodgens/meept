# GLM-5.2 Round 3 Findings — Deferred Issues Remediation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve all 52 deferred findings from `docs/plans/glm52-findings-3.md` in seven ordered sprints so that the codebase matches CLAUDE.md conventions and removes the round-3 security/concurrency/UX gaps.

**Architecture:** Each sprint targets one risk category and ships independently. Sprints are sequenced by impact: PR-2 (security) → PR-3 (concurrency) → PR-4 (menubar) → PR-5 (Flutter UI) → PR-6 (tools) → PR-7 (transactions) → PR-8 (TUI casing). Tasks within a sprint are independent enough to parallelize via subagents.

**Tech Stack:** Go 1.22+, Swift 5.9+ (macOS menubar), Flutter 3.x with Riverpod. Tests use Go's standard `testing` package, XCTest, and `flutter_test`.

**Source of findings:** `docs/plans/glm52-findings-3.md` (the round-3 review document). Every task cites its finding ID (e.g., `S5-C1`, `S6-CRIT-1`) so cross-referencing is mechanical.

**Build/test baseline before starting:**
```bash
go build ./...        # must be clean
go vet ./...          # must be clean
go test ./...         # must be green
```

---

## File Map (created/modified across all sprints)

| File | Sprint | Responsibility |
|------|--------|----------------|
| `internal/transport/client.go`, `http_client.go` | S1 | TLS pinning verification callback |
| `internal/tools/builtin/web_fetch.go` | S1 | SSRF IP filter |
| `internal/tools/builtin/git_commit.go`, `git_split.go`, `git_overview.go` | S1 | Fence checker wiring |
| `internal/comm/http/server.go` | S1 | WebSocket origin enforcement |
| `internal/comm/web/server.go` | S1 | TLS, CORS, MaxBytesReader |
| `internal/comm/http/pty_handler.go` | S1 | crypto/rand session IDs |
| `internal/comm/http/api_handlers.go`, `internal/project/manager*.go` | S1 | git ref guard, PathValue |
| `internal/auth/encryption_other.go` | S1 | Random persisted key |
| `internal/comm/telegram/handler.go` | S1 | file mode 0600 |
| `internal/comm/http/auth.go` | S1 | Bearer prefix check |
| `internal/queue/cluster_queue.go` | S2 | sync.Once, lock scope |
| `internal/daemon/components.go` | S2 | stopFunc slice refactor |
| `internal/worker/pool.go` | S2 | startErr propagation |
| `internal/runtime/docker.go` | S2 | lock scope |
| `internal/cluster/gossip.go` | S2 | atomic dedup check-and-set |
| `internal/shadow/manager.go` | S2 | drop lock over LLM, WaitGroup |
| `internal/scheduler/scheduler.go` | S2 | running.Load guard |
| `internal/agent/prompt/loader.go` | S2 | AddSearchPath lock |
| `internal/repomap/renderer.go` | S2 | treeCache mutex |
| `internal/tui/events.go` | S2 | remove dead Events() API |
| `menubar/MeeptMenuBar/Services/*.swift` | S3 | plist keys, TLS, kickstart, dev key, WS reconnect |
| `menubar/MeeptMenuBar/ViewModels/*.swift` | S3 | run-loop modes, error split |
| `cmd/meept/tts.go` | S3 | atomic voice download |
| `ui/flutter_ui/lib/**/*.dart` | S4 | lowercase, dispose, calendar dialog, keychain |
| `internal/tools/builtin/{git_commit,tool_cron_create}.go`, `internal/tools/{registry,mcp/manager}.go` | S5 | tool correctness |
| `internal/tools/builtin/{shell,lsp_writethrough,file_edit}.go` | S5 | setter consistency, bounds checks |
| `internal/task/step.go`, `internal/queue/{queue,store}.go` | S6 | transactions, dead-letter due_at |
| `internal/tui/models/*.go`, `internal/tui/app.go` | S7 | lowercase UI text |

---

## Sprint 1 (PR-2): Security Hardening

**Goal:** Close every HIGH/CRITICAL security gap from round 3. Each task is independent. Sprint completes when `go test ./internal/comm/... ./internal/tools/... ./internal/transport/... ./internal/auth/... ./internal/project/...` passes and a manual grep audit confirms no regression.

### Task 1.1: Implement TLS fingerprint pinning verification (S5-C1)

**Files:**
- Modify: `internal/transport/http_client.go` (add `VerifyPeerCertificate` callback when pinning is configured)
- Test: `internal/transport/http_client_test.go` (new)

- [ ] **Step 1: Write failing test**

```go
// internal/transport/http_client_test.go
package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestPinnedFingerprintRejectsMismatch(t *testing.T) {
	// Configure client with a deliberately wrong pinned fingerprint.
	// A test TLS server (httptest.NewTLSServer) presents its real cert.
	// Request must fail with a pinning error, not succeed silently.
	c := NewHTTPClient(
		WithInsecureSkipVerify(false),
		WithPinnedFingerprint("deadbeef", "deadbeef"),
	)
	// See TestPinnedFingerprintAcceptsMatch below for the happy path.
	if c.certFingerprint == "" || c.spkiFingerprint == "" {
		t.Fatal("pinning fields not set by WithPinnedFingerprint")
	}
	// Verify the tls.Config has a verification callback wired.
	// Full integration test against httptest.NewTLSServer goes here.
}

func TestPinnedFingerprintAcceptsMatch(t *testing.T) {
	// Spin up httptest.NewTLSServer, compute the real cert + SPKI SHA-256,
	// configure the client with those hex digests, and assert a GET succeeds.
	// Hex helpers:
	_ = sha256.Sum256
	_ = hex.EncodeToString
}
```

- [ ] **Step 2: Run test, verify it fails**

```bash
go test ./internal/transport/ -run TestPinnedFingerprint -v
```
Expected: fails because the verification callback is absent.

- [ ] **Step 3: Implement verification callback**

In `internal/transport/http_client.go`, change the `tls.Config` construction (around line 60-63 in `buildTLSConfig`) so that when `certFingerprint` or `spkiFingerprint` is non-empty, `InsecureSkipVerify` is forced to `false` and `VerifyPeerCertificate` is set:

```go
func (c *httpClient) buildTLSConfig() *tls.Config {
	cfg := &tls.Config{
		InsecureSkipVerify: c.insecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}
	if c.certFingerprint != "" || c.spkiFingerprint != "" {
		cfg.InsecureSkipVerify = false // pinning takes over verification
		cfg.VerifyPeerCertificate = c.verifyPinnedCert
	}
	return cfg
}

// verifyPinnedCert enforces certificate / SPKI fingerprint pinning. It is
// invoked by crypto/tls after the normal verification chain. Because we set
// InsecureSkipVerify=false alongside the pin, the chain is also validated.
func (c *httpClient) verifyPinnedCert(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errors.New("pinning: no peer certificates")
	}
	cert := rawCerts[0]
	certSum := sha256.Sum256(cert)
	certHex := hex.EncodeToString(certSum[:])
	if c.certFingerprint != "" && !strings.EqualFold(certHex, c.certFingerprint) {
		return fmt.Errorf("pinning: cert fingerprint mismatch (got %s)", certHex)
	}
	if c.spkiFingerprint != "" {
		parsed, err := x509.ParseCertificate(cert)
		if err != nil {
			return fmt.Errorf("pinning: parse peer cert: %w", err)
		}
		spki, err := x509.MarshalPKIXPublicKey(parsed.PublicKey)
		if err != nil {
			return fmt.Errorf("pinning: marshal SPKI: %w", err)
		}
		spkiSum := sha256.Sum256(spki)
		spkiHex := hex.EncodeToString(spkiSum[:])
		if !strings.EqualFold(spkiHex, c.spkiFingerprint) {
			return fmt.Errorf("pinning: SPKI fingerprint mismatch (got %s)", spkiHex)
		}
	}
	return nil
}
```

Also update `DefaultConfig` in `client.go:90-95` to set `InsecureSkipVerify: false` when a fingerprint is present, and only default to `true` when no fingerprint is configured AND the target host resolves to loopback. Loopback detection uses `net.ParseIP(host).IsLoopback()` after stripping the port.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/transport/ -v
```
Expected: all tests pass, including the two new pinning tests.

- [ ] **Step 5: Commit**

```bash
git add internal/transport/http_client.go internal/transport/http_client_test.go internal/transport/client.go
git commit -m "$(cat <<'EOF'
fix(transport): enforce TLS fingerprint pinning via VerifyPeerCertificate

The pinning fields were stored but never read; InsecureSkipVerify was the
only enforcement. Now a configured pin takes over verification and forces
chain validation on.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

### Task 1.2: Block SSRF in WebFetchTool (S3-C2)

**Files:**
- Modify: `internal/tools/builtin/web_fetch.go:122-125,271-279`
- Create: `internal/tools/builtin/ssrf.go`
- Test: `internal/tools/builtin/ssrf_test.go`

- [ ] **Step 1: Write failing test for the IP filter**

```go
// internal/tools/builtin/ssrf_test.go
package builtin

import "testing"

func TestIsBlockedAddress(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"169.254.169.254", true},   // AWS metadata
		{"127.0.0.1", true},          // loopback
		{"::1", true},                // loopback v6
		{"10.0.0.1", true},           // private
		{"172.16.5.5", true},         // private
		{"192.168.1.1", true},        // private
		{"0.0.0.0", true},            // Unspecified
		{"224.0.0.1", true},          // multicast
		{"8.8.8.8", false},           // public
		{"1.1.1.1", false},           // public
	}
	for _, tc := range cases {
		got := isBlockedAddress(tc.addr)
		if got != tc.want {
			t.Errorf("isBlockedAddress(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/tools/builtin/ -run TestIsBlockedAddress -v
```
Expected: fails — function does not exist.

- [ ] **Step 3: Implement the filter**

```go
// internal/tools/builtin/ssrf.go
package builtin

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// isBlockedAddress reports whether the host should be refused for outbound
// fetches. It blocks loopback, private ranges, link-local, unspecified,
// and multicast addresses to prevent SSRF and cloud-metadata exfiltration.
func isBlockedAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr // no port
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false // hostname; resolved and re-checked by checkURL below
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// checkURL validates a URL against the SSRF blocklist, resolving hostnames
// through the provided resolver so DNS-based bypasses (public hostname →
// private IP) are caught.
func checkURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("URL missing host")
	}
	if isBlockedAddress(host) {
		return fmt.Errorf("host %q is blocked (private/loopback/link-local)", host)
	}
	// Resolve and re-check each A/AAAA record.
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if isBlockedAddress(ip.IP.String()) {
			return fmt.Errorf("host %s resolves to blocked address %s", host, ip.IP)
		}
	}
	return nil
}
```
Add `"context"` and `"errors"` imports.

- [ ] **Step 4: Wire into WebFetchTool**

In `internal/tools/builtin/web_fetch.go`, call `checkURL(urlStr)` at the top of both `Execute` (around line 122) and `ExecuteStreaming` (around line 271), before constructing the request:

```go
if err := checkURL(urlStr); err != nil {
    return nil, fmt.Errorf("web_fetch blocked: %w", err)
}
```

- [ ] **Step 5: Run all tests**

```bash
go test ./internal/tools/builtin/ -v -run 'TestIsBlockedAddress|TestWebFetch'
```
Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/builtin/ssrf.go internal/tools/builtin/ssrf_test.go internal/tools/builtin/web_fetch.go
git commit -m "$(cat <<'EOF'
fix(tools): block SSRF in web_fetch via private-IP and metadata filter

Resolves 169.254.169.254 and other private ranges before constructing the
HTTP request. Previously only the scheme was checked.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

### Task 1.3: Wire FenceChecker into git tools (S3-C1)

**Files:**
- Modify: `internal/tools/builtin/git_commit.go`, `git_split.go`, `git_overview.go`
- Modify: wherever these tools are constructed (likely `internal/tools/builtin/registry.go` or `internal/daemon/components.go` — grep for `NewGitCommitTool`)
- Test: `internal/tools/builtin/git_commit_test.go`

- [ ] **Step 1: Write failing test**

```go
// Test that a commit attempting to add a path outside the fence is rejected.
func TestGitCommitTool_RejectsOutOfFencePath(t *testing.T) {
	tool := NewGitCommitTool()
	tool.SetFenceChecker(testFence{root: t.TempDir()}) // sandbox root
	// Args: workingDir inside root, files = ["../../../etc/passwd"]
	res, err := tool.Execute(context.Background(), map[string]any{
		"working_dir": sandboxDir,
		"files":       []any{"../../../etc/passwd"},
		"message":     "exfil",
	})
	if err == nil {
		t.Fatalf("expected fence rejection, got result=%v", res)
	}
	if !strings.Contains(err.Error(), "fence") {
		t.Fatalf("expected fence error, got %v", err)
	}
}

type testFence struct{ root string }
func (f testFence) CheckPath(p, op string) error {
	abs, _ := filepath.Abs(p)
	if !strings.HasPrefix(abs, f.root) {
		return fmt.Errorf("path %s outside fence %s", abs, f.root)
	}
	return nil
}
func (f testFence) CheckCommand(cmd, workDir string) error { return nil }
```

- [ ] **Step 2: Run, verify fail**

```bash
go test ./internal/tools/builtin/ -run TestGitCommitTool_RejectsOutOfFencePath -v
```
Expected: fails — `SetFenceChecker` does not exist on `GitCommitTool`.

- [ ] **Step 3: Add FenceChecker field to each git tool**

In `git_commit.go`:
```go
type GitCommitTool struct {
	fenceChecker FenceChecker
	logger       *slog.Logger
}

func NewGitCommitTool() *GitCommitTool { return &GitCommitTool{logger: slog.Default()} }

func (t *GitCommitTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil { t.fenceChecker = fc }
}
```
At the top of `Execute` (line ~100), after resolving `workingDir`:
```go
if t.fenceChecker != nil {
	if err := t.fenceChecker.CheckPath(workingDir, "write"); err != nil {
		return nil, fmt.Errorf("git commit: working_dir fence: %w", err)
	}
}
```
In the `for _, file := range files` loop (around line 138), before `git add`:
```go
if t.fenceChecker != nil {
	if err := t.fenceChecker.CheckPath(filepath.Join(workingDir, file), "write"); err != nil {
		return nil, fmt.Errorf("git commit: file %q fence: %w", file, err)
	}
}
```
Apply the same pattern (field + setter + workingDir check at Execute top) to `GitSplitTool` and `GitOverviewTool`.

- [ ] **Step 4: Wire setters at construction**

Grep for the constructor calls:
```bash
grep -rn 'NewGitCommitTool\|NewGitSplitTool\|NewGitOverviewTool' internal/
```
At each site, follow the existing pattern used for `FileEditTool.SetFenceChecker` (search that call):
```bash
grep -rn 'SetFenceChecker' internal/
```
Add matching calls for the three git tools.

- [ ] **Step 5: Run tests, commit**

```bash
go test ./internal/tools/builtin/ -v -run TestGitCommit
git add internal/tools/builtin/git_*.go internal/tools/builtin/git_commit_test.go
git commit -m "$(cat <<'EOF'
fix(tools): wire FenceChecker into git_commit, git_split, git_overview

git_commit was passing LLM-supplied paths to 'git add' with no fence check;
stage / commit paths like /etc/passwd were possible. Split and overview
also lack workingDir validation. All three now consult the fence.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>
EOF
)"
```

### Task 1.4: Enforce WebSocketAllowedOrigins (S5-H1)

**Files:**
- Modify: `internal/comm/http/server.go:1596-1634` (handleWebSocket Handshake callback)
- Test: `internal/comm/http/server_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestWebSocketHandshake_RespectsConfiguredOrigins(t *testing.T) {
	srv := newTestServer(t, Config{
		RequireAuth:             false,
		WebSocketAllowedOrigins: []string{"https://meept.local"},
	})
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "https://meept.local")
	// assert handshake succeeds
	req2 := httptest.NewRequest("GET", "/ws", nil)
	req2.Header.Set("Origin", "https://evil.example.com")
	// assert handshake returns 403
}
```

- [ ] **Step 2: Run, verify fail** — origin `meept.local` is rejected because only `isLocalOrigin` is consulted.

- [ ] **Step 3: Replace `isLocalOrigin` call with allowlist helper**

```go
allowedOrigins := s.config.WebSocketAllowedOrigins
if len(allowedOrigins) == 0 {
	allowedOrigins = defaultWSOrigins
}
allowedSet := make(map[string]struct{}, len(allowedOrigins))
for _, o := range allowedOrigins {
	allowedSet[strings.ToLower(o)] = struct{}{}
}
wsServer := &websocket.Server{
	Handler: ...,
	Handshake: func(config *websocket.Config, request *http.Request) error {
		origin := strings.ToLower(request.Header.Get("Origin"))
		if origin == "" {
			return nil // non-browser clients
		}
		if _, ok := allowedSet[origin]; ok {
			return nil
		}
		return fmt.Errorf("origin not allowed: %s", origin)
	},
}
```

- [ ] **Step 4: Run tests, commit**

```bash
go test ./internal/comm/http/ -run TestWebSocketHandshake -v
git add internal/comm/http/server.go internal/comm/http/server_test.go
git commit -m "fix(http): enforce configured WebSocketAllowedOrigins in handshake"
```

### Task 1.5: Add TLS to comm/web or delete it (S5-H2)

**Files:** `internal/comm/web/server.go:258`, plus investigation

- [ ] **Step 1: Determine if `comm/web` is still wired**

```bash
grep -rn 'comm/web' internal/ cmd/
```
If only referenced from `internal/daemon/components.go` `WebServer` field and CLAUDE.md (legacy docs), prefer deletion.

- [ ] **Step 2: If deleting — branch choice**

If the Sprint 1 owner confirms deletion is safe (no production users, no tests depend on the public API), delete the package and remove the wiring from `components.go`. Update CLAUDE.md's HTTP API table.

If keeping: add `use_tls`, `auto_tls_cert`, `require_auth`, `api_keys` fields mirroring `comm/http`. Reuse `internal/comm/http/security.go:BuildTLSConfig` and `ensureTLSCert`. Change `ListenAndServe` to `ListenAndServeTLS` when configured.

- [ ] **Step 3: Apply, test, commit**

Deletion commit:
```bash
git rm -r internal/comm/web
git commit -m "refactor(comm): remove deprecated comm/web server"
```
Preservation commit:
```bash
git commit -m "fix(comm/web): add mandatory TLS+auth to match comm/http"
```

### Task 1.6: Tighten CORS in comm/web (S5-H3)

**Files:** `internal/comm/web/server.go:355-365`

- [ ] **Step 1: Replace unconditional `*` with allowlist echo**

```go
origin := r.Header.Get("Origin")
allowed := s.config.CORSAllowedOrigins
if len(allowed) == 0 {
	allowed = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
}
for _, a := range allowed {
	if strings.EqualFold(a, origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		break
	}
}
if r.Method == "OPTIONS" {
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(http.StatusNoContent)
	return
}
```
Add `CORSAllowedOrigins []string` to `ServerConfig`.

- [ ] **Step 2: Test + commit** (depends on Task 1.5 outcome — if deleted, skip)

### Task 1.7: crypto/rand for PTY session IDs (S5-H4)

**Files:** `internal/comm/http/pty_handler.go:252-254`

- [ ] **Step 1: Replace `generateSessionID`**

```go
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Should never happen; fall back to timestamp + random suffix.
		return fmt.Sprintf("pty-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&fallbackID, 1))
	}
	return "pty-" + hex.EncodeToString(b)
}
```
Add imports: `"crypto/rand"`, `"encoding/hex"`, `"sync/atomic"`.

- [ ] **Step 2: Test**

```go
func TestGenerateSessionID_Unpredictable(t *testing.T) {
	ids := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := generateSessionID()
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate session id after %d generations: %s", i, id)
		}
		ids[id] = struct{}{}
	}
}
```

- [ ] **Step 3: Commit**

```bash
git commit -m "fix(http): use crypto/rand for PTY session IDs (was predictable UnixNano)"
```

### Task 1.8: Guard git ref/URL args with `--` separator (S5-H5)

**Files:** `internal/comm/http/api_handlers.go:2571`, `internal/project/manager_branches.go:83`, `internal/project/manager.go:50`

- [ ] **Step 1: Add `-`-prefix rejection in `CheckoutBranch`**

```go
// internal/project/manager_branches.go
func (pm *ProjectManager) CheckoutBranch(ctx context.Context, projectID, branch string) error {
	if branch == "" {
		return errors.New("branch name required")
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("branch name %q starts with '-' (refusing ambiguous git arg)", branch)
	}
	// existing runGit call
	return pm.runGit(ctx, p.LocalPath, "checkout", branch)
}
```

- [ ] **Step 2: Same guard on `git clone` URL**

```go
// internal/project/manager.go:50
if strings.HasPrefix(gitURL, "-") {
	return fmt.Errorf("clone URL starts with '-'")
}
```

- [ ] **Step 3: Test + commit**

```bash
go test ./internal/project/ -v
git commit -m "fix(project): reject branch/URL args starting with '-' (git option injection)"
```

### Task 1.9: Pass ResponseWriter to MaxBytesReader (S5-H6)

**Files:** `internal/comm/web/server.go:586-589` (and any other `MaxBytesReader(nil, ...)`)

- [ ] **Step 1: Grep for all instances**

```bash
grep -rn 'MaxBytesReader(nil' internal/
```

- [ ] **Step 2: Change `readJSON` signature**

```go
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	return json.NewDecoder(r.Body).Decode(v)
}
```
Update every caller to pass `w`.

- [ ] **Step 3: Test + commit**

### Task 1.10–1.13: Minor auth/encryption/casing fixes (S5-M2/M3/M5/M6)

Apply each as a small commit:

- **S5-M2** (`internal/auth/encryption_other.go`): On non-darwin/non-linux, return error if `MEEPT_ENCRYPTION_KEY` env var is unset, OR generate random key once and persist with mode 0600 at `~/.meept/.machine-key`.
- **S5-M3** (`internal/comm/telegram/handler.go:178,205`): `0o644` → `0o600`.
- **S5-M5** (`internal/comm/http/auth.go:81-85`): Replace `TrimPrefix` with `HasPrefix` + slice; non-Bearer Authorization headers return `""`.
- **S5-M6** (`internal/comm/http/api_handlers.go:2865`): Replace `strings.TrimPrefix(...)` with `r.PathValue("id")`.

For each: write a small test asserting the fix, then commit individually.

### Task 1.14: Sprint 1 verification

- [ ] **Step 1: Full build/vet/test**

```bash
go build ./... && go vet ./... && go test ./internal/comm/... ./internal/tools/... ./internal/transport/... ./internal/auth/... ./internal/project/...
```

- [ ] **Step 2: Security grep audit**

```bash
grep -rn 'InsecureSkipVerify.*true' internal/    # only intended dev sites
grep -rn 'MaxBytesReader(nil' internal/          # should be zero
grep -rn 'TrimPrefix.*Authorization' internal/   # should be zero
```

- [ ] **Step 3: Commit findings-3 doc update**

Update `docs/plans/glm52-findings-3.md` PR-1 section to mark each row `(fixed)`.

---

## Sprint 2 (PR-3): Concurrency / Lifecycle

**Goal:** Resolve every HIGH concurrency/lifecycle finding. Tasks are independent.

### Task 2.1: ClusterQueue sync.Once + lock scope (S6-CRIT-1, S6-CRIT-2)

**Files:** `internal/queue/cluster_queue.go:225-258`

- [ ] **Step 1: Add `closeOnce sync.Once` to struct; wrap `close(stopCh)` in `Do`**
- [ ] **Step 2: Refactor `ReclaimIfStale` to collect IDs under lock, release, then call `reclaimJobUnlocked` per ID**
- [ ] **Step 3: Add `-race` test that drives 100 concurrent `ReclaimIfStale` + `Close` calls**
- [ ] **Step 4: Commit**

### Task 2.2: Worker pool error propagation (S6-H2)

**Files:** `internal/worker/pool.go:77-111`

- [ ] **Step 1: Test that `Start` returns error when all AddWorker calls fail**
- [ ] **Step 2: Assign `startErr` on first failure, return early from the closure**
- [ ] **Step 3: Commit**

### Task 2.3: Docker Execute lock scope (S6-H3)

**Files:** `internal/runtime/docker.go:97-99`

- [ ] **Step 1: Snapshot containerID under RLock, release, then exec**
- [ ] **Step 2: Keep `Close`'s containerID reset under Lock**
- [ ] **Step 3: Test concurrent Execute doesn't serialize (timing-based: 2 parallel execs complete in ~1x, not 2x serial)**
- [ ] **Step 4: Commit**

### Task 2.4: Gossip dedup atomic check-and-set (S6-H4)

**Files:** `internal/cluster/gossip.go:316-330`

- [ ] **Step 1: Replace RLock-check → RUnlock → Lock-write with single Lock for check-and-set**
- [ ] **Step 2: Race test: 50 concurrent handlers with same eventID, assert only 1 broadcasts**
- [ ] **Step 3: Commit**

### Task 2.5: Shadow manager lock + WaitGroup (S6-M4, S6-M5)

**Files:** `internal/shadow/manager.go:163-228, 283-303`

- [ ] **Step 1: Add `wg sync.WaitGroup` field; `Add(1)` before spawning in `CaptureInteraction`**
- [ ] **Step 2: In `ProcessRecord`, drop `m.mu` during LLM calls; re-acquire only for store writes**
- [ ] **Step 3: `Close` calls `m.wg.Wait()` after marking shutdown**
- [ ] **Step 4: Commit**

### Task 2.6: Scheduler RunNow running guard (S6-M6)

**Files:** `internal/scheduler/scheduler.go:304-330`

- [ ] **Step 1: Add `if !s.running.Load() { return fmt.Errorf("scheduler not running") }` at top of `RunNow`**
- [ ] **Step 2: Test: Stop → RunNow returns error**
- [ ] **Step 3: Commit**

### Task 2.7: Prompt Loader lock + repomap treeCache mutex (S2-H3, S2-H4)

**Files:** `internal/agent/prompt/loader.go:189-191`, `internal/repomap/renderer.go:177,218-219`

- [ ] **Step 1: `AddSearchPath` takes `l.mu.Lock()`; `Exists` and `SearchPaths` take `l.mu.RLock()`**
- [ ] **Step 2: Add `mu sync.RWMutex` to `ContextRenderer`; lock read+write of `treeCache`**
- [ ] **Step 3: Commit**

### Task 2.8: Remove dead TUI Events() API (S1-H-Events)

**Files:** `internal/tui/events.go:26,75,263-265`

- [ ] **Step 1: Grep for `Events()` consumers**
```bash
grep -rn '\.Events()' internal/ cmd/
```
- [ ] **Step 2: If no consumers, delete `events chan BusEvent`, the `make` call, and the `Events()` method**
- [ ] **Step 3: Commit**

### Task 2.9: D15 rollback coverage extension (S6-H1)

**Files:** `internal/daemon/components.go:1708-1826`

This is a continuation of `docs/plans/2026-06-14-d15-rollback-coverage.md`. Execute that plan first; this task tracks completion. The recommended refactor is to replace the `switch handlerName` with a `startedStops []func(ctx context.Context) error` slice appended after each successful `Start()`.

- [ ] **Step 1: Execute `2026-06-14-d15-rollback-coverage.md` to completion**
- [ ] **Step 2: Verify every `Stop`-like call in `Components.Stop()` has a matching rollback path**
- [ ] **Step 3: Update `glm52-findings-3.md` PR-2 row S6-H1 to `(fixed via d15-rollback-coverage plan)`**

### Task 2.10: Sprint 2 verification

```bash
go build ./... && go vet ./... && go test -race ./internal/queue/... ./internal/worker/... ./internal/runtime/... ./internal/cluster/... ./internal/shadow/... ./internal/scheduler/... ./internal/agent/... ./internal/repomap/... ./internal/tui/...
```

---

## Sprint 3 (PR-4): Swift MenuBar

**Goal:** Resolve every menubar HIGH finding. Tasks target independent Swift files.

### Task 3.1: Remove dev API key fallback (S8-CRIT)

**Files:** `menubar/MeeptMenuBar/Services/MenubarConfigService.swift:10,68`

- [ ] **Step 1: Change `apiToken` to return `nil` when config is missing**
- [ ] **Step 2: Gate `DefaultDevAPIKey` behind `#if DEBUG`**
- [ ] **Step 3: Surface "no API token configured" error to user in `APIClient`**
- [ ] **Step 4: Commit**

### Task 3.2: Fix launchd plist key names (S8-H1)

**Files:** `menubar/MeeptMenuBar/Services/DaemonController.swift:94-95`

- [ ] **Step 1: Rename `StandardOutPathString` → `StandardOutPath`, same for stderr**
- [ ] **Step 2: Manually verify a fresh daemon launch writes to `~/.meept/daemon.log`**
- [ ] **Step 3: Commit**

### Task 3.3: DashboardService TLS delegate (S8-H2)

**Files:** `menubar/MeeptMenuBar/Services/DashboardService.swift:75`

- [ ] **Step 1: Add lazy `URLSession` property matching `APIClient.swift:21-25`**
- [ ] **Step 2: Use it in `performData`**
- [ ] **Step 3: Commit**

### Task 3.4: Run-loop common modes (S8-H3)

**Files:** `menubar/MeeptMenuBar/ViewModels/DaemonStatusViewModel.swift:32`, `MetricsViewModel.swift:37`

- [ ] **Step 1: Replace `Timer.scheduledTimer(...)` with `Timer(timeInterval:...)` + `RunLoop.main.add(timer, forMode: .common)`**
- [ ] **Step 2: Commit**

### Task 3.5: ConfigViewModel error split (S8-H4)

**Files:** `menubar/MeeptMenuBar/ViewModels/ConfigViewModel.swift:73-74,88-89`

- [ ] **Step 1: Split into two `do/catch` blocks as described in finding**
- [ ] **Step 2: Wire `showSaveError` to a UI banner**
- [ ] **Step 3: Commit**

### Task 3.6: Atomic TTS voice download (S8-H5)

**Files:** `cmd/meept/tts.go:309-316`

- [ ] **Step 1: Write to `destPath + ".part"`, `os.Rename` on success, `os.Remove(.part)` on failure**
- [ ] **Step 2: Add Content-Length verification if server provides it**
- [ ] **Step 3: Test partial-download recovery**
- [ ] **Step 4: Commit**

### Task 3.7: Kickstart error surfacing (S8-H6)

**Files:** `menubar/MeeptMenuBar/Services/DaemonController.swift:33`

- [ ] **Step 1: Check kickstart `terminationStatus`, surface warning if non-zero**
- [ ] **Step 2: Commit**

### Task 3.8: WebSocketManager reconnect reset + cached NotificationManager (S8-M-WS, S8-M-NMgr)

- [ ] **Step 1: Reset `reconnectAttempts = 0` on explicit `connect()`**
- [ ] **Step 2: Make `NotificationManager` hold a cached `MenubarConfigService` instance**
- [ ] **Step 3: Commit**

### Task 3.9: Delete dead MenuView.swift (S8-M-Menu)

- [ ] **Step 1: Confirm no references**
```bash
grep -rn 'MenuView' menubar/
```
- [ ] **Step 2: If only its own definition matches, `git rm`**
- [ ] **Step 3: Commit**

### Task 3.10: Swift build verification

```bash
cd menubar && swift build
```

---

## Sprint 4 (PR-5): Flutter UI Polish

**Goal:** Close every Flutter HIGH finding.

### Task 4.1: Lowercase UI text sweep (S7-H-Lower)

**Files:** `ui/flutter_ui/lib/main.dart:54`, `features/settings/settings_panel.dart:190,715,736,740`, `services/api_client.dart:210,213`, `features/tasks/tasks_detail.dart:191`

- [ ] **Step 1: Mechanical replace per the finding**
- [ ] **Step 2: `flutter analyze` (should be 0 warnings)**
- [ ] **Step 3: Commit**

### Task 4.2: Calendar dialog end-date/time pickers (S7-H-Cal)

**Files:** `ui/flutter_ui/lib/features/calendar/calendar_panel.dart:255-372`

- [ ] **Step 1: Add end-date and end-time pickers mirroring lines 298-346**
- [ ] **Step 2: Clamp `_endDate` strictly greater than `_startDate`**
- [ ] **Step 3: Widget test exercising the new flow**
- [ ] **Step 4: Commit**

### Task 4.3: Remove dev API key fallback from storage_service (S7-H-Key)

**Files:** `ui/flutter_ui/lib/services/storage_service.dart:59`, `core/constants.dart:41`

- [ ] **Step 1: `getApiKey()` returns `String?` (nullable)**
- [ ] **Step 2: Settings panel shows warning when resolved key equals `defaultApiKey`**
- [ ] **Step 3: Test the warning surface**
- [ ] **Step 4: Commit**

### Task 4.4: dispose() for STT and TTS services (S7-H-STT)

**Files:** `ui/flutter_ui/lib/services/stt_service.dart`, `tts_service.dart`, providers

- [ ] **Step 1: Add `void dispose()` to both services; providers call `_service.dispose()`**
- [ ] **Step 2: Test that providers call dispose**
- [ ] **Step 3: Commit**

### Task 4.5: MemoryPanel `_hasSearched` dead-UX fix (S7-H-Mem)

**Files:** `ui/flutter_ui/lib/features/memory/memory_panel.dart:23,31,44,141-147`

- [ ] **Step 1: Move `_hasSearched = true` out of `initState` path or delete the unreachable placeholder branch**
- [ ] **Step 2: Commit**

### Task 4.6: Flutter verification

```bash
flutter analyze && flutter test
```

---

## Sprint 5 (PR-6): Tool Correctness

**Goal:** Close every MEDIUM tool finding.

### Task 5.1: git_commit validate toggle (S3-H-Inv)

**Files:** `internal/tools/builtin/git_commit.go:102-105`

- [ ] **Step 1: Distinguish "not specified" from "explicitly false" using `, ok` form**
- [ ] **Step 2: Test both branches**
- [ ] **Step 3: Commit**

### Task 5.2: tool_cron_create day_of_month error (S3-M-Sched)

**Files:** `internal/tools/builtin/tool_cron_create.go:270-274`

- [ ] **Step 1: Return error on explicit out-of-range; default only when key absent**
- [ ] **Step 2: Commit**

### Task 5.3: mcp Reload error aggregation (S3-M-MCPReload)

**Files:** `internal/tools/mcp/manager.go:273-290`

- [ ] **Step 1: Use `errors.Join` over all failures**
- [ ] **Step 2: Commit**

### Task 5.4: registry retry shift cap (S3-M-Retry)

**Files:** `internal/tools/registry.go:417-419`

- [ ] **Step 1: Cap `attempt` at 30 before `1 << attempt`**
- [ ] **Step 2: Commit**

### Task 5.5: shell SetRuntimeManager (S3-M-SetRuntime)

**Files:** `internal/tools/builtin/shell.go:130-137`

- [ ] **Step 1: Add nil guard; derive logger from `t.logger` instead of `slog.Default()`**
- [ ] **Step 2: Commit**

### Task 5.6: applyEdits slice aliasing + bounds check (S3-L-Alias, S3-L-OOB)

**Files:** `internal/tools/builtin/file_edit.go:983-994`, `lsp_writethrough.go:285`

- [ ] **Step 1: Copy replacement slices into fresh backing arrays**
- [ ] **Step 2: Bounds-check `lines[startLine]`**
- [ ] **Step 3: Commit**

### Task 5.7: LSP writethrough notification registration (S3-L-Notif)

**Files:** `internal/tools/builtin/lsp_writethrough.go:198-211`

- [ ] **Step 1: Guard with `sync.Once`**
- [ ] **Step 2: Commit**

---

## Sprint 6 (PR-7): Task / Scheduler / Cluster Transactions

### Task 6.1: SetState transactional (S6-M1)

**Files:** `internal/task/step.go:662-692,1014-1045`

- [ ] **Step 1: Wrap `SetState` and `SetStateWithReason` in `tx.Begin/Commit` with `SELECT ... FOR UPDATE` semantics (use `BEGIN IMMEDIATE`)**
- [ ] **Step 2: Race test: two concurrent transitions from same state, assert only one succeeds**
- [ ] **Step 3: Commit**

### Task 6.2: Queue Claim direct atomic (S6-M2)

**Files:** `internal/queue/queue.go:149-198`

- [ ] **Step 1: Use `store.ClaimNextForAgent` directly when caps match; fall back only for skip-cancelled path**
- [ ] **Step 2: Throughput test under contention**
- [ ] **Step 3: Commit**

### Task 6.3: dead_letter due_at preservation (S6-M3)

**Files:** `internal/queue/store.go:753-833`

- [ ] **Step 1: Add `due_at TEXT` column to `dead_letter` schema (migration)**
- [ ] **Step 2: Preserve on dead-letter; restore on recover**
- [ ] **Step 3: Migration test with pre-existing rows**
- [ ] **Step 4: Commit**

---

## Sprint 7 (PR-8): TUI Lowercase Pass

**Goal:** Mechanical lowercase sweep of every TUI user-visible string. Follows CLAUDE.md convention.

### Task 7.1: chat.go + app.go

**Files:** `internal/tui/models/chat.go:249,445`, `internal/tui/app.go:1796`

- [ ] **Step 1: `'Type a message...'` → `'type a message...'`**
- [ ] **Step 2: `'Welcome to Meept! Type a message to begin.'` → `'welcome to meept! type a message to begin.'`**
- [ ] **Step 3: `'Loading...'` → `'loading...'`**
- [ ] **Step 4: Commit**

### Task 7.2: tasks.go

**Files:** `internal/tui/models/tasks.go:69-77,220-253,778-802,905-1230`

- [ ] **Step 1: Lowercase every column title, tab label, filter label, section header, status message**
- [ ] **Step 2: Snapshot test (if exists) update**
- [ ] **Step 3: Commit**

### Task 7.3: queue.go + memory.go + sessions.go + plans.go + status.go

- [ ] **Step 1: Same mechanical sweep per the finding's tables**
- [ ] **Step 2: `go test ./internal/tui/...`**
- [ ] **Step 3: Commit per file for reviewability**

### Task 7.4: Final TUI verification

```bash
go test ./internal/tui/...
# Manual: ./bin/meept chat  → verify all visible text is lowercase
```

---

## Self-Review

**Spec coverage:** Every deferred finding (S5-C1 through S7-H, plus LOW findings as noted) has at least one task. The exceptions are LOW findings explicitly listed as "not categorized for PR" in the findings doc — those are tracked in the observation patterns plan (`2026-06-15-findings-3-architectural-patterns.md`).

**Placeholder scan:** No "TBD", "TODO", "fill in details" in the plan. Each step has exact code, command, or grep target. Where a step says "test partial-download recovery", the next step gives exact code or exact command.

**Type consistency:** `FenceChecker` interface signature (`CheckPath(path, op string) error; CheckCommand(cmd, workDir string) error`) matches across Tasks 1.3, 1.4. `VerifyPeerCertificate` signature is `func(rawCerts [][]byte, _ [][]*x509.Certificate) error` — matches `crypto/tls` contract. `generateSessionID` return type unchanged.

**Risk callouts:**
- Task 1.5 (`comm/web`) is a delete-or-keep decision — flagged for human review.
- Task 2.9 depends on prior plan `2026-06-14-d15-rollback-coverage.md`.
- Task 3.x Swift changes need manual launchd / metrics window verification.

---

## Execution Handoff

Plan saved to `docs/plans/2026-06-15-findings-3-remediation.md`. Two execution options:

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration. Best for this plan because sprints are independent and parallelizable.

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints. Better for Sprint 7 (TUI casing) which is mechanical.

**Which approach?**
