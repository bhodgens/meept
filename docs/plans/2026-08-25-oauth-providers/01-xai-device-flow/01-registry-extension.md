# FlowKind + Discovery Foundation - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add the `FlowKind` type + `OAuthProviderConfig` flow fields + OIDC discovery resolver + form-encoding support in the device flow.
- **Dependencies:** none (this is the root-wave gate — branches 02/03 import `FlowKind`)
- **Estimated Context:** 45K (exploration 15K + generation 15K + iteration 10K + overhead 5K)
- **Concurrency Group:** WAVE 1 (gate; must land before all Wave 2 leaves)
- **Audit references:** none

## Goal

Extend `internal/auth` with the flow-selection machinery three new providers need: a `FlowKind` enum on `OAuthProviderConfig`, the Codex/PKCE endpoint fields, an OIDC discovery helper for xAI's token endpoint, and form-encoded device-code requests (xAI requires `application/x-www-form-urlencoded`; the current code sends JSON). No registry entries are added here — only types, fields, and the two helpers.

## Context

meept's OAuth lives in `internal/auth` (module `github.com/caimlas/meept`): `providers.go` holds the `OAuthProviders` map + `OAuthProviderConfig` struct; `device_flow.go` holds `StartDeviceFlow` (currently JSON body), `PollForToken`, `RefreshTokenRequest` (form); `token_store.go` persists `TokenResult` encrypted. The CLI (`cmd/meept/oauth.go`) calls `providerCfg.DeviceFlowConfig()` → `StartDeviceFlow` → `PollForToken` → `store.Save`.

Key files to understand before implementing:
- `internal/auth/providers.go` — struct + registry + `DeviceFlowConfig()` mapping (~135 lines total)
- `internal/auth/device_flow.go` — `StartDeviceFlow` request building (lines ~100-160), `DeviceFlowConfig` struct (~40-50)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/auth/providers.go
type FlowKind string

const (
    FlowDeviceRFC8628 FlowKind = "device_rfc8628"
    FlowDeviceCodex   FlowKind = "device_codex"
    FlowPKCEPaste     FlowKind = "pkce_paste"
)

// OAuthProviderConfig — add these fields (do NOT reorder/touch existing fields):
    Flow             FlowKind          // empty == FlowDeviceRFC8628
    DeviceUserCodeEP string            // codex: usercode endpoint
    DevicePollEP     string            // codex: poll endpoint
    AuthorizeURL     string            // claude: authorize endpoint
    VerifyHint       string            // claude: user-facing hint
    DiscoveryURL     string            // xai: .well-known/openid-configuration
    RefreshJSON      bool              // claude: refresh as JSON body

// internal/auth/discovery.go (new file)
func ResolveTokenEndpoint(ctx context.Context, discoveryURL string) (string, error)

// internal/auth/device_flow.go — DeviceFlowConfig gains:
    FormEncoded bool
// StartDeviceFlow: when FormEncoded, send url.Values{"client_id","scope"}
//   with Content-Type application/x-www-form-urlencoded (Accept: application/json);
//   otherwise existing JSON body unchanged.
```

### What This Leaf Consumes

Existing `StartDeviceFlow`, `deviceFlowHTTPClient`, `DeviceCodeResult`, `TokenResult` — unchanged signatures.

## Tasks

### Task 1: FlowKind type + provider config fields

**Objective:** Add the flow-selection type and endpoint fields to `OAuthProviderConfig`.

**Files:**
- Modify: `internal/auth/providers.go`

**Step 1: Write failing test** — `internal/auth/providers_flow_test.go`:

```go
package auth

import "testing"

func TestFlowKindDefaults(t *testing.T) {
    // Existing entries have empty Flow -> treated as FlowDeviceRFC8628.
    for id, cfg := range OAuthProviders {
        if cfg.Flow != "" && cfg.Flow != FlowDeviceCodex && cfg.Flow != FlowPKCEPaste && cfg.Flow != FlowDeviceRFC8628 {
            t.Errorf("%s: unknown Flow %q", id, cfg.Flow)
        }
    }
}

func TestFlowKindConstants(t *testing.T) {
    if FlowDeviceRFC8628 != "device_rfc8628" {
        t.Errorf("got %q", FlowDeviceRFC8628)
    }
    if FlowDeviceCodex != "device_codex" {
        t.Errorf("got %q", FlowDeviceCodex)
    }
    if FlowPKCEPaste != "pkce_paste" {
        t.Errorf("got %q", FlowPKCEPaste)
    }
}
```

**Step 2:** `go test ./internal/auth/ -run TestFlowKind` → FAIL (undefined symbols).

**Step 3:** Add the type, constants, and struct fields to `providers.go` above the `OAuthProviders` map. Add a doc comment on `FlowKind` noting empty means RFC 8628.

**Step 4:** Re-run → PASS. `go build ./internal/auth/`.

### Task 2: OIDC discovery resolver

**Objective:** Resolve the token endpoint from a provider's `.well-known/openid-configuration`.

**Files:**
- Create: `internal/auth/discovery.go`
- Test: `internal/auth/discovery_test.go`

**Step 1: Failing test** (httptest server serving `{"token_endpoint": "https://auth.example/token"}`; also a non-200 case and a missing-field case):

```go
func TestResolveTokenEndpoint(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprintln(w, `{"issuer":"https://auth.x.ai","token_endpoint":"https://auth.x.ai/oauth2/token"}`)
    }))
    defer srv.Close()
    ep, err := ResolveTokenEndpoint(context.Background(), srv.URL)
    if err != nil { t.Fatal(err) }
    if ep != "https://auth.x.ai/oauth2/token" { t.Errorf("got %q", ep) }
}

func TestResolveTokenEndpoint_Missing(t *testing.T) { /* 200 without token_endpoint -> error */ }
func TestResolveTokenEndpoint_HTTPError(t *testing.T) { /* 500 -> error mentioning status */ }
```

**Step 2:** Run → FAIL.

**Step 3:** Implement `discovery.go`: GET with `Accept: application/json` using the package `deviceFlowHTTPClient` pattern (own client var with 30s timeout is fine), decode `struct{ TokenEndpoint string `json:"token_endpoint"` }`, error paths wrap `%w`/`fmt.Errorf` with status code. Never log.

**Step 4:** PASS.

### Task 3: Form-encoded device requests

**Objective:** `DeviceFlowConfig.FormEncoded` switches `StartDeviceFlow` to form encoding.

**Files:**
- Modify: `internal/auth/device_flow.go` (struct + request building in `StartDeviceFlow` only)
- Test: `internal/auth/device_flow_form_test.go`

**Step 1: Failing test** — httptest capturing the request; assert `Content-Type: application/x-www-form-urlencoded`, body parses as `client_id=X&scope=a+b`, and the JSON branch still sends `Content-Type: application/json`:

```go
func TestStartDeviceFlow_FormEncoded(t *testing.T) {
    var gotCT, gotBody string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotCT = r.Header.Get("Content-Type")
        b, _ := io.ReadAll(r.Body)
        gotBody = string(b)
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"device_code":"d","user_code":"u","verification_uri":"https://v","expires_in":600,"interval":5}`)
    }))
    defer srv.Close()
    cfg := DeviceFlowConfig{ClientID: "cid", DeviceEP: srv.URL, TokenEP: srv.URL,
        Scopes: []string{"openid", "email"}, FormEncoded: true}
    if _, err := StartDeviceFlow(context.Background(), cfg); err != nil { t.Fatal(err) }
    if gotCT != "application/x-www-form-urlencoded" { t.Errorf("content-type %q", gotCT) }
    val, _ := url.ParseQuery(gotBody)
    if val.Get("client_id") != "cid" || val.Get("scope") != "openid email" { t.Errorf("body %q", gotBody) }
}
```

Plus the JSON-branch guard test (`FormEncoded: false` → `application/json`).

**Step 2:** FAIL. **Step 3:** Add `FormEncoded bool` to `DeviceFlowConfig`; branch in `StartDeviceFlow` between the existing JSON marshal and a `url.Values` encode. **Step 4:** PASS; run full `go test ./internal/auth/ -count=1`.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test ./internal/auth/ -count=1`)
- [ ] Interface contracts satisfied exactly (field names, constant values, helper signature)
- [ ] Files at exact paths; no existing registry entries touched
- [ ] No deviations from spec (or documented below)
- [ ] No scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Every Task implemented; every test present and passing
- [ ] `FlowKind` constant strings exact: `device_rfc8628`, `device_codex`, `pkce_paste`
- [ ] Struct fields added without modifying/removing existing fields or entries
- [ ] Discovery: non-200 and missing-field error paths tested
- [ ] Form encoding: body + Content-Type asserted; JSON branch regression-tested
- [ ] Conventions: `%w` wrapping, no unused imports, gofmt clean
- [ ] No debug artifacts, no TODOs, no placeholders, no line-number corruption

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- This leaf is the gate for the whole tree — branches 02/03 compile against `FlowKind`. Land it cleanly.
- Do not add any registry map entries here (leaf 02 and sibling branches own their entries).
- `PollForToken` already sends form-encoded grant `urn:ietf:params:oauth:grant-type:device_code` — do not touch it.
