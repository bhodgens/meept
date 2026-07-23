# DISPATCH INSTRUCTION

**Parent**: `20260723-config-extraction/master.md`  
**Scope**: Extract hardcoded URLs, timeouts, and limits into config  
**Dependencies**: None  
**Estimated Context**: ~60K  
**Files to touch**: Config schema files, `internal/transport/client.go`, `internal/shadow/config.go`, `internal/session/store_sqlite.go`, `internal/session/summarizer.go`, potentially TLS config

## Goal

Replace hardcoded operational parameters with configurable values from the config schema.

## Tasks

### Task 1: Identify all hardcoded values

Search for these patterns:
```bash
grep -rn 'localhost:8081' --include='*.go' .
grep -rn 'localhost:11434' --include='*.go' .
grep -rn '5000' --include='*.go' internal/session/
grep -rn '8000' --include='*.go' internal/session/
grep -rn 'TLS 1.2\|tls.VersionTLS12' --include='*.go' .
```

Document each location and current value.

### Task 2: Read config schema

Find the main config struct definition (likely in `internal/config/` or similar). Understand the structure and how defaults are set.

### Task 3: Add config fields

Add fields to the appropriate config section:

```go
type ServerConfig struct {
    HTTPBaseURL string `json:"http_base_url" default:"https://localhost:8081"`
    // ... other fields
}

type LLMConfig struct {
    OllamaEndpoint string `json:"ollama_endpoint" default:"http://localhost:11434"`
    // ... other fields
}

type SessionConfig struct {
    SQLiteBusyTimeoutMs int `json:"sqlite_busy_timeout_ms" default:5000`
    PromptTruncationLimit int `json:"prompt_truncation_limit" default:8000`
    // ... other fields
}

type SecurityConfig struct {
    TLSMinVersion string `json:"tls_min_version" default:"1.2"` // "1.2", "1.3"
    // ... other fields
}
```

### Task 4: Update code to use config

For each hardcoded value:
1. Read the file
2. Replace literal with config reference:
   ```go
   // Before:
   url := "https://localhost:8081"
   
   // After:
   url := cfg.Server.HTTPBaseURL
   ```
3. Ensure config is passed to the function/struct

### Task 5: Handle TLS version parsing

For TLS min version, add a helper to parse string to `uint16`:

```go
func parseTLSVersion(s string) (uint16, error) {
    switch s {
    case "1.2":
        return tls.VersionTLS12, nil
    case "1.3":
        return tls.VersionTLS13, nil
    default:
        return 0, fmt.Errorf("unsupported TLS version: %s", s)
    }
}
```

### Task 6: Update defaults

Ensure default config values match the previous hardcoded values so existing deployments aren't affected.

### Task 7: Verify

Run:
```bash
go build ./...
```

Ensure compilation succeeds. Test that config loading works correctly.

## Self-Verification Checklist

- [ ] All hardcoded values identified (use grep)
- [ ] Config schema updated with new fields
- [ ] Defaults match previous hardcoded values
- [ ] All code locations updated to use config
- [ ] TLS version parsing implemented
- [ ] Config is properly threaded to usage sites
- [ ] `go build ./...` succeeds
- [ ] No debug artifacts

## Do NOT commit

Write code, run tests, report results. The orchestrator handles all git operations.
