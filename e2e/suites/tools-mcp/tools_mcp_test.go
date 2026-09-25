//go:build e2e

// Package toolsmcp covers the MCP client stack (config → spawn →
// initialize → exposure → dispatch) through a real scratch daemon talking
// to a tiny stub MCP server over stdio.
//
// The stub server is a ~60-line python3 script written into the sandbox via
// harness.WithPreBootHook; the daemon spawns it as a child (StdioTransport,
// internal/tools/mcp/transport/stdio.go), performs the MCP initialize +
// tools/list handshake, and routes calls through Manager.CallTool (rate
// limiter + per-server stats).
//
// Coverage map (manifest scenarios):
//
//	tools-mcp-01  stdio lifecycle: config→spawn→initialize→tool exposed — TestStdioMCPSpawnsInitializesAndExposesTool
//	tools-mcp-02  disabled server never starts; mcp.list reports it     — TestDisabledMCPServerNeverStarts
//	tools-mcp-03  calls route through the manager (rate limiter/stats)  — TestMCPCallRoutesThroughManagerStats
//
// Known limits, stated honestly:
//   - The DIRECT agent-turn call leg for scenario 01 (a scripted
//     "stubserver.echo" call reaching the stub) is not assertable today: the
//     per-agent Executor.checkPermission falls back to the dotted tool name
//     as the permission action and pkg/security.BuiltinRules has no rule for
//     dotted names, so the live path answers "Unknown action: stubserver.echo"
//     before Manager.CallTool runs (only the cua-driver./browser_ prefixes
//     have name-based rules). The real-call leg is covered end-to-end in
//     scenario 03 via the web_search MCP-first chain, which dispatches
//     through Manager.CallTool without the dotted-name permission lookup.
//   - The rate-limit DENY leg (a refused second call) stays unit-level
//     (internal/tools/mcp manager tests): websearch silently falls back to
//     the live DuckDuckGo scraper on any MCP error, which would break
//     hermeticity if asserted here.
package toolsmcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// stubMCPServer is a minimal MCP stdio server: initialize handshake,
// tools/list, and tools/call handlers for "echo" (plain echo) and "search"
// (SearXNG-JSON-shaped results the websearch MCP chain can parse).
const stubMCPServer = `#!/usr/bin/env python3
import sys, json

def reply(id_, result):
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": id_, "result": result}) + "\n")
    sys.stdout.flush()

TOOLS = [
    {"name": "echo", "description": "Echo back the input text.",
     "inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}, "required": ["text"]}},
    {"name": "search", "description": "Stub SearXNG search returning canned JSON results.",
     "inputSchema": {"type": "object", "properties": {"query": {"type": "string"}, "limit": {"type": "number"}}}},
]

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except ValueError:
        continue
    method = msg.get("method", "")
    id_ = msg.get("id")
    if id_ is None:
        continue  # notification (e.g. notifications/initialized)
    if method == "initialize":
        reply(id_, {"protocolVersion": "2024-11-05", "capabilities": {"tools": {}},
                    "serverInfo": {"name": "e2e-stub", "version": "0.0.1"}})
    elif method == "tools/list":
        reply(id_, {"tools": TOOLS})
    elif method == "tools/call":
        params = msg.get("params", {})
        args = params.get("arguments", {})
        if params.get("name") == "echo":
            reply(id_, {"content": [{"type": "text", "text": "stub-echo: " + str(args.get("text", ""))}]})
        elif params.get("name") == "search":
            payload = json.dumps({"query": str(args.get("query", "")), "results": [
                {"title": "Stub searxng hit for " + str(args.get("query", "")),
                 "url": "https://stub.example/hit", "snippet": "canned stub result"}]})
            reply(id_, {"content": [{"type": "text", "text": payload}]})
        else:
            reply(id_, {"content": [{"type": "text", "text": "unknown tool"}], "isError": True})
    else:
        reply(id_, {})
`

// startedMarker is written by startMarkerStubServer when the subprocess
// actually launches — proof a "disabled" server never spawned.
const startMarkerStubServer = `#!/usr/bin/env python3
import sys, json, os

marker = os.environ.get("MCP_START_MARKER", "")
if marker:
    open(marker, "w").write("started")

def reply(id_, result):
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": id_, "result": result}) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
    except ValueError:
        continue
    id_ = msg.get("id")
    if id_ is None:
        continue
    method = msg.get("method", "")
    if method == "initialize":
        reply(id_, {"protocolVersion": "2024-11-05", "capabilities": {"tools": {}},
                    "serverInfo": {"name": "e2e-stub", "version": "0.0.1"}})
    elif method == "tools/list":
        reply(id_, {"tools": [{"name": "ping", "description": "ping",
                               "inputSchema": {"type": "object", "properties": {}}}]})
    else:
        reply(id_, {})
`

// stubServerConfig is one mcp_servers.json5 entry for the stub server.
func stubServerConfig(name, scriptPath string, enabled bool) map[string]any {
	return map[string]any{
		"name":    name,
		"enabled": enabled,
		"command": []string{"python3", scriptPath},
	}
}

// seedScript writes a python script into the sandbox and returns its path.
func seedScript(t *testing.T, s *harness.Stack, name, body string) string {
	t.Helper()
	path := filepath.Join(s.Work, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// seedStubLaneAgent writes a custom user-tier roster agent that owns the
// "tooluse" lane and holds the stubserver.echo grant, so the filtered
// per-agent registry exposes the MCP tool to its executor turns. Runs AFTER
// the harness's bundled-roster seeding (pre-boot hooks run post-seedRoster);
// a fresh agent ID avoids shadowing questions with bundled definitions.
func seedStubLaneAgent(t *testing.T, s *harness.Stack) {
	t.Helper()
	dir := filepath.Join(s.MeeptHome, "agents", "aastub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir stub agent dir: %v", err)
	}
	body := `---
id: aastub
name: Stub Driver
role: executor
description: e2e-only agent holding the stubserver.echo MCP grant
intents: [tooluse]
enabled: true
can_delegate: false
additional_tools:
  - stubserver.echo
  - web_fetch
capabilities:
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# Stub Driver

You exist only inside the e2e sandbox. Follow the user's request briefly.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write stub agent AGENT.md: %v", err)
	}
}

// requestsDeclareTool reports whether ANY completion request the fake LLM
// received declared the given tool name in its tools array — the observable
// of per-agent tool exposure.
func requestsDeclareTool(f *harness.FakeLLM, tool string) bool {
	for _, body := range f.Requests() {
		raw, ok := body["tools"].([]any)
		if !ok {
			continue
		}
		for _, entry := range raw {
			fn, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n == tool {
				return true
			}
		}
	}
	return false
}

// toolResultsJoined collects every role=tool message the daemon sent back.
func toolResultsJoined(f *harness.FakeLLM) string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return strings.Join(out, "\n")
}

// mcpServerEntries calls the daemon's mcp.list RPC and returns
// name -> status entry (config + stats).
func mcpServerEntries(t *testing.T, s *harness.Stack) map[string]map[string]any {
	t.Helper()
	client := harness.DialRPC(t, s.SocketPath)
	result := client.CallResult("mcp.list", map[string]any{})
	servers, ok := result["servers"].([]any)
	if !ok {
		t.Fatalf("mcp.list result missing servers array: %v", result)
	}
	out := map[string]map[string]any{}
	for _, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cfg, _ := entry["config"].(map[string]any)
		name, _ := cfg["name"].(string)
		out[name] = entry
	}
	return out
}

// entryState extracts stats.state from one mcp.list entry.
func entryState(t *testing.T, entry map[string]any) string {
	t.Helper()
	stats, ok := entry["stats"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.list entry missing stats: %v", entry)
	}
	state, _ := stats["state"].(string)
	return state
}

// tools-mcp-01: config → spawn → initialize → tool exposed. The daemon boots
// with mcp.enabled + a stdio stub server; it must spawn the child, complete
// the MCP initialize + tools/list handshake (state active), and expose
// "stubserver.echo" to the stub-lane agent's executor turns.
func TestStdioMCPSpawnsInitializesAndExposesTool(t *testing.T) {
	var serverCfgPath string
	var scriptPath string

	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			scriptPath = seedScript(t, st, "mcp-stub-server.py", stubMCPServer)
			serverCfgPath = filepath.Join(st.MeeptHome, "mcp_servers.json5")
			servers := map[string]any{"servers": []any{stubServerConfig("stubserver", scriptPath, true)}}
			raw, err := json.Marshal(servers)
			if err != nil {
				return err
			}
			if err := os.WriteFile(serverCfgPath, raw, 0o600); err != nil {
				return err
			}
			seedStubLaneAgent(t, st) // must exist before the roster loads at boot
			return nil
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			cfg["mcp"] = map[string]any{
				"enabled":     true,
				"config_file": serverCfgPath, // computed by the pre-boot hook
			}
		}),
	)

	// Spawn + initialize: the configured server reached state active, which
	// requires a live child plus a completed initialize/tools/list handshake.
	entries := mcpServerEntries(t, s)
	entry, ok := entries["stubserver"]
	if !ok {
		t.Fatalf("mcp.list missing stubserver entry; got %v", entries)
	}
	if state := entryState(t, entry); state != "active" {
		t.Fatalf("stubserver state = %q, want active\ndaemon log tail:\n%s",
			state, s.Daemon.LogTail())
	}

	// Tool exposed: a stub-lane turn (classifier pinned to stublane) must
	// offer stubserver.echo to the model.
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "mcp-expose", s.ProjectDir)
	s.Fake.SetClassifierOutput(`{"intent":"tooluse","confidence":0.95,"reasoning":"pinned tooluse lane"}`)
	// Script a granted tool call so the turn performs a real executor
	// round-trip; the tools array of that request is the exposure evidence.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"https://example.com/probe"}`,
	})

	s.ChatTurn(t, sessionID,
		"Fetch https://example.com/probe and tell me what tools you have for echoing text",
		120*time.Second)

	if !requestsDeclareTool(s.Fake, "stubserver.echo") {
		toolReqs := 0
		var offered []string
		for _, body := range s.Fake.Requests() {
			if raw, ok := body["tools"].([]any); ok && len(raw) > 0 {
				toolReqs++
				if toolReqs == 1 {
					for _, entry := range raw {
						if fn, ok := entry.(map[string]any); ok {
							if f2, ok := fn["function"].(map[string]any); ok {
								fn = f2
							}
							if n, _ := fn["name"].(string); n != "" {
								offered = append(offered, n)
							}
						}
					}
				}
			}
		}
		t.Fatalf("no executor request declared stubserver.echo (tool-bearing requests: %d; first offered: %v)\ndaemon log tail:\n%s",
			toolReqs, offered, s.Daemon.LogTail())
	}

}

// tools-mcp-02: a disabled server never spawns and is reported as disabled.
func TestDisabledMCPServerNeverStarts(t *testing.T) {
	var markerPath string
	var serverCfgPath string
	var scriptPath string

	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			markerPath = filepath.Join(st.Work, "stub-never-started.marker")
			scriptPath = seedScript(t, st, "mcp-marker-server.py", startMarkerStubServer)
			// The stub reads its marker path from the environment.
			serverCfgPath = filepath.Join(st.MeeptHome, "mcp_servers.json5")
			entry := stubServerConfig("disabledserver", scriptPath, false)
			entry["env"] = map[string]string{"MCP_START_MARKER": markerPath}
			raw, err := json.Marshal(map[string]any{"servers": []any{entry}})
			if err != nil {
				return err
			}
			return os.WriteFile(serverCfgPath, raw, 0o600)
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			cfg["mcp"] = map[string]any{
				"enabled":     true,
				"config_file": serverCfgPath,
			}
		}),
	)

	// Reported as disabled via the RPC status surface.
	entries := mcpServerEntries(t, s)
	entry, ok := entries["disabledserver"]
	if !ok {
		t.Fatalf("mcp.list missing disabledserver entry; got %v", entries)
	}
	if state := entryState(t, entry); state != "disabled" {
		t.Fatalf("disabledserver state = %q, want disabled", state)
	}

	// Never spawned: the start marker must not exist.
	if _, err := os.Stat(markerPath); err == nil {
		t.Fatal("disabled MCP server spawned its subprocess (start marker exists)")
	}
}

// tools-mcp-03: a real MCP tool call routed through Manager.CallTool. The
// websearch tool's MCP-first chain dispatches "searxng.search" through the
// manager (rate limiter + stats + sanitizer all apply), so a chat-lane turn
// with a scripted web_search reaches the stub server and its canned SearXNG
// results land in the tool envelope the model sees.
func TestMCPCallRoutesThroughManagerStats(t *testing.T) {
	var serverCfgPath string
	var scriptPath string

	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			scriptPath = seedScript(t, st, "mcp-stub-server.py", stubMCPServer)
			serverCfgPath = filepath.Join(st.MeeptHome, "mcp_servers.json5")
			entry := stubServerConfig("searxng", scriptPath, true)
			// Tiny limiter: every call passes through getRateLimiter; the
			// deny leg is unit-level (see package comment).
			entry["rate_limit_rps"] = 10.0
			entry["rate_limit_burst"] = 20
			raw, err := json.Marshal(map[string]any{"servers": []any{entry}})
			if err != nil {
				return err
			}
			return os.WriteFile(serverCfgPath, raw, 0o600)
		}),
		harness.WithConfigHook(func(cfg map[string]any) {
			cfg["mcp"] = map[string]any{
				"enabled":     true,
				"config_file": serverCfgPath,
			}
		}),
	)

	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "mcp-search", s.ProjectDir)
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_search",
		Arguments: `{"query":"e2e stub query","limit":5}`,
	})

	s.ChatTurn(t, sessionID,
		"Look up what the stub search returns for the e2e stub query and summarize it for me",
		120*time.Second)

	res := toolResultsJoined(s.Fake)
	if !strings.Contains(res, "Stub searxng hit for e2e stub query") {
		t.Fatalf("web_search tool result missing the stub MCP search hit; results:\n%s\ndaemon log tail:\n%s",
			res, s.Daemon.LogTail())
	}

	// Per-server stats recorded the dispatched call.
	entries := mcpServerEntries(t, s)
	entry, ok := entries["searxng"]
	if !ok {
		t.Fatalf("mcp.list missing searxng entry; got %v", entries)
	}
	stats, _ := entry["stats"].(map[string]any)
	if state := entryState(t, entry); state != "active" {
		t.Fatalf("searxng state = %q, want active", state)
	}
	requests, _ := stats["requests"].(float64)
	if requests < 1 {
		t.Fatalf("searxng stats.requests = %v, want >= 1 after the routed call", stats["requests"])
	}
	if _, ok := stats["last_request_at"]; !ok {
		t.Fatalf("searxng stats.last_request_at not set: %v", stats)
	}
}
