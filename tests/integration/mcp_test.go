package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/mcp"
)

// TestMCPManagerCreation tests basic MCP manager creation.
func TestMCPManagerCreation(t *testing.T) {
	manager := mcp.NewManager(nil) // Accepts nil logger
	if manager == nil {
		t.Fatal("NewManager should not return nil")
	}

	// Check initial state
	if count := manager.ServerCount(); count != 0 {
		t.Errorf("Expected 0 servers, got %d", count)
	}

	servers := manager.ListServers()
	if len(servers) != 0 {
		t.Errorf("Expected empty server list, got %v", servers)
	}
}

// TestMCPServerConfigValidation tests server configuration validation.
func TestMCPServerConfigValidation(t *testing.T) {
	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Test empty name
	err := manager.StartServer(ctx, mcp.ServerConfig{})
	if err == nil {
		t.Error("Expected error for empty server name")
	}

	// Test missing command and URL
	err = manager.StartServer(ctx, mcp.ServerConfig{Name: "test"})
	if err == nil {
		t.Error("Expected error when neither command nor URL is specified")
	}
}

// TestMCPToolDiscovery tests tool discovery from MCP servers.
// This test creates a mock MCP server using a simple echo script.
func TestMCPToolDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP tool discovery test in short mode")
	}

	// Create a temporary directory for our mock MCP server
	tempDir, err := os.MkdirTemp("", "mcp-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock MCP server script that implements the protocol
	scriptContent := `#!/bin/bash
# Mock MCP server that responds to initialize and tools/list

read line

# Check if it's initialize
if echo "$line" | grep -q '"method":"initialize"'; then
    echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"mock-server","version":"1.0.0"},"capabilities":{"tools":{}}}}'
    # Wait for initialized notification
    read line
    # Respond to tools/list
    read line
    if echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"echo","description":"Echoes input","inputSchema":{"type":"object","properties":{"text":{"type":"string","description":"Text to echo"}},"required":["text"]}}]}}'
    fi
fi
`
	scriptPath := filepath.Join(tempDir, "mock-mcp.sh")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to write mock server script: %v", err)
	}

	manager := mcp.NewManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start the mock server
	err = manager.StartServer(ctx, mcp.ServerConfig{
		Name:    "mock",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("Failed to start mock MCP server: %v", err)
	}
	defer manager.StopAll()

	// Verify server is listed
	servers := manager.ListServers()
	if len(servers) != 1 || servers[0] != "mock" {
		t.Errorf("Expected server 'mock' in list, got %v", servers)
	}

	// Verify server count
	if count := manager.ServerCount(); count != 1 {
		t.Errorf("Expected 1 server, got %d", count)
	}

	// Verify server is connected
	if !manager.IsServerConnected("mock") {
		t.Error("Expected mock server to be connected")
	}

	// Get client
	client := manager.GetClient("mock")
	if client == nil {
		t.Fatal("Expected to get client for mock server")
	}

	// Check tools were discovered
	allTools := manager.AllTools()
	if len(allTools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(allTools))
	}
	if len(allTools) > 0 && allTools[0].Name != "mock.echo" {
		t.Errorf("Expected tool name 'mock.echo', got '%s'", allTools[0].Name)
	}

	// Check LLM definitions
	llmDefs := manager.AllLLMDefinitions()
	if len(llmDefs) != 1 {
		t.Errorf("Expected 1 LLM definition, got %d", len(llmDefs))
	}
}

// TestMCPToolExecution tests tool execution through MCP.
func TestMCPToolExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP tool execution test in short mode")
	}

	// Create a temporary directory for our mock MCP server
	tempDir, err := os.MkdirTemp("", "mcp-test-exec")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock MCP server script that handles tool calls
	scriptContent := `#!/bin/bash
# Mock MCP server that handles tool execution

while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"exec-server","version":"1.0.0"},"capabilities":{"tools":{}}}}'
    elif echo "$line" | grep -q '"method":"notifications/initialized"'; then
        : # Notification, no response
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"greet","description":"Greets a person","inputSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}]}}'
    elif echo "$line" | grep -q '"method":"tools/call"'; then
        # Extract the name argument (simple parsing)
        name=$(echo "$line" | sed 's/.*"name":"\([^"]*\)".*/\1/')
        echo '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"Hello, '"$name"'!"}],"isError":false}}'
    fi
done
`
	scriptPath := filepath.Join(tempDir, "exec-mcp.sh")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to write mock server script: %v", err)
	}

	manager := mcp.NewManager(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start the mock server
	err = manager.StartServer(ctx, mcp.ServerConfig{
		Name:    "exec",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("Failed to start mock MCP server: %v", err)
	}
	defer manager.StopAll()

	// Call the tool
	result, err := manager.CallTool(ctx, "exec.greet", map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if !result.Success {
		t.Errorf("Expected successful result, got error: %s", result.Error)
	}

	// Check result contains expected text
	if resultStr, ok := result.Result.(string); ok {
		if resultStr != "Hello, World!" {
			t.Errorf("Expected 'Hello, World!', got '%s'", resultStr)
		}
	}
}

// TestMCPGracefulShutdown tests graceful shutdown of MCP servers.
func TestMCPGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP graceful shutdown test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mcp-shutdown")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a simple server that stays running
	scriptContent := `#!/bin/bash
while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"shutdown-test","version":"1.0.0"},"capabilities":{}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
    fi
done
`
	scriptPath := filepath.Join(tempDir, "shutdown-mcp.sh")
	//nolint:gosec // test directory/file
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to write mock server script: %v", err)
	}

	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Start server
	err = manager.StartServer(ctx, mcp.ServerConfig{
		Name:    "shutdown",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Verify running
	if !manager.IsServerConnected("shutdown") {
		t.Error("Server should be connected")
	}

	// Stop single server
	err = manager.StopServer("shutdown")
	if err != nil {
		t.Errorf("StopServer failed: %v", err)
	}

	// Verify stopped
	if manager.IsServerConnected("shutdown") {
		t.Error("Server should be disconnected after stop")
	}

	if count := manager.ServerCount(); count != 0 {
		t.Errorf("Expected 0 servers after stop, got %d", count)
	}
}

// TestMCPStopAll tests stopping all MCP servers at once.
func TestMCPStopAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP stop all test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mcp-stopall")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	scriptContent := `#!/bin/bash
while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"test","version":"1.0.0"},"capabilities":{}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
    fi
done
`
	script1 := filepath.Join(tempDir, "server1.sh")
	script2 := filepath.Join(tempDir, "server2.sh")
	os.WriteFile(script1, []byte(scriptContent), 0755)
	os.WriteFile(script2, []byte(scriptContent), 0755)

	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Start multiple servers
	manager.StartServer(ctx, mcp.ServerConfig{Name: "server1", Command: []string{"bash", script1}, Type: "stdio"})
	manager.StartServer(ctx, mcp.ServerConfig{Name: "server2", Command: []string{"bash", script2}, Type: "stdio"})

	if count := manager.ServerCount(); count != 2 {
		t.Errorf("Expected 2 servers, got %d", count)
	}

	// Stop all
	manager.StopAll()

	if count := manager.ServerCount(); count != 0 {
		t.Errorf("Expected 0 servers after StopAll, got %d", count)
	}
}

// TestMCPReload tests hot-reloading MCP configuration.
func TestMCPReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP reload test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mcp-reload")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create server scripts
	scriptContent := `#!/bin/bash
while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"reload-test","version":"1.0.0"},"capabilities":{}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
    fi
done
`
	scriptA := filepath.Join(tempDir, "serverA.sh")
	scriptB := filepath.Join(tempDir, "serverB.sh")
	scriptC := filepath.Join(tempDir, "serverC.sh")
	os.WriteFile(scriptA, []byte(scriptContent), 0755)
	os.WriteFile(scriptB, []byte(scriptContent), 0755)
	os.WriteFile(scriptC, []byte(scriptContent), 0755)

	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Initial config: servers A and B
	initialConfigs := []mcp.ServerConfig{
		{Name: "serverA", Command: []string{"bash", scriptA}, Type: "stdio"},
		{Name: "serverB", Command: []string{"bash", scriptB}, Type: "stdio"},
	}

	// Start initial servers
	for _, cfg := range initialConfigs {
		if err := manager.StartServer(ctx, cfg); err != nil {
			t.Fatalf("Failed to start initial server %s: %v", cfg.Name, err)
		}
	}

	if count := manager.ServerCount(); count != 2 {
		t.Fatalf("Expected 2 initial servers, got %d", count)
	}

	// New config: servers B and C (A removed, C added)
	newConfigs := []mcp.ServerConfig{
		{Name: "serverB", Command: []string{"bash", scriptB}, Type: "stdio"},
		{Name: "serverC", Command: []string{"bash", scriptC}, Type: "stdio"},
	}

	// Reload
	if err := manager.Reload(ctx, newConfigs); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify server count
	if count := manager.ServerCount(); count != 2 {
		t.Errorf("Expected 2 servers after reload, got %d", count)
	}

	// Verify serverA is gone
	if manager.IsServerConnected("serverA") {
		t.Error("serverA should not be connected after reload")
	}

	// Verify serverB is still there
	if !manager.IsServerConnected("serverB") {
		t.Error("serverB should still be connected after reload")
	}

	// Verify serverC is now there
	if !manager.IsServerConnected("serverC") {
		t.Error("serverC should be connected after reload")
	}

	// Cleanup
	manager.StopAll()
}

// TestMCPToolWrapper tests the MCPTool wrapper.
func TestMCPToolWrapper(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP tool wrapper test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mcp-wrapper")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	scriptContent := `#!/bin/bash
while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"wrapper-test","version":"1.0.0"},"capabilities":{"tools":{}}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"test_tool","description":"A test tool","inputSchema":{"type":"object","properties":{"input":{"type":"string"}}}}]}}'
    elif echo "$line" | grep -q '"method":"tools/call"'; then
        echo '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"tool executed"}],"isError":false}}'
    fi
done
`
	scriptPath := filepath.Join(tempDir, "wrapper-mcp.sh")
	os.WriteFile(scriptPath, []byte(scriptContent), 0755)

	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Start server
	err = manager.StartServer(ctx, mcp.ServerConfig{
		Name:    "wrapper",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer manager.StopAll()

	// Get LLM definitions and create MCPTool wrapper
	defs := manager.AllLLMDefinitions()
	if len(defs) == 0 {
		t.Fatal("Expected at least one tool definition")
	}

	mcpTool := mcp.NewMCPTool(defs[0], manager, "wrapper")

	// Test Tool interface methods
	if name := mcpTool.Name(); name != "wrapper.test_tool" {
		t.Errorf("Expected name 'wrapper.test_tool', got '%s'", name)
	}

	if desc := mcpTool.Description(); desc != "A test tool" {
		t.Errorf("Expected description 'A test tool', got '%s'", desc)
	}

	if server := mcpTool.Server(); server != "wrapper" {
		t.Errorf("Expected server 'wrapper', got '%s'", server)
	}

	// Test execution
	result, err := mcpTool.Execute(ctx, map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result != "tool executed" {
		t.Errorf("Expected 'tool executed', got '%v'", result)
	}
}

// TestMCPInvalidToolCall tests error handling for invalid tool calls.
func TestMCPInvalidToolCall(t *testing.T) {
	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Call tool with invalid format
	_, err := manager.CallTool(ctx, "no-dot-in-name", map[string]any{})
	if err == nil {
		t.Error("Expected error for invalid tool name format")
	}

	// Call tool for non-existent server
	_, err = manager.CallTool(ctx, "nonexistent.tool", map[string]any{})
	if err == nil {
		t.Error("Expected error for non-existent server")
	}
}

// TestMCPDuplicateServerStart tests starting a server with duplicate name.
func TestMCPDuplicateServerStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping duplicate server test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mcp-dup")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	scriptContent := `#!/bin/bash
while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"dup-test","version":"1.0.0"},"capabilities":{}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
    fi
done
`
	scriptPath := filepath.Join(tempDir, "dup.sh")
	os.WriteFile(scriptPath, []byte(scriptContent), 0755)

	manager := mcp.NewManager(nil)
	ctx := context.Background()

	// Start first server
	err = manager.StartServer(ctx, mcp.ServerConfig{
		Name:    "duplicate",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("Failed to start first server: %v", err)
	}
	defer manager.StopAll()

	// Try to start duplicate
	err = manager.StartServer(ctx, mcp.ServerConfig{
		Name:    "duplicate",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err == nil {
		t.Error("Expected error when starting duplicate server")
	}
}

// TestMCPToolRegistration tests registering MCP tools with a tool registry.
func TestMCPToolRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping MCP tool registration test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mcp-reg")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	scriptContent := `#!/bin/bash
while read line; do
    if echo "$line" | grep -q '"method":"initialize"'; then
        echo '{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"reg-test","version":"1.0.0"},"capabilities":{"tools":{}}}}'
    elif echo "$line" | grep -q '"method":"tools/list"'; then
        echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"tool1","description":"Tool 1","inputSchema":{"type":"object"}},{"name":"tool2","description":"Tool 2","inputSchema":{"type":"object"}}]}}'
    elif echo "$line" | grep -q '"method":"tools/call"'; then
        echo '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"executed"}],"isError":false}}'
    fi
done
`
	scriptPath := filepath.Join(tempDir, "reg.sh")
	os.WriteFile(scriptPath, []byte(scriptContent), 0755)

	mcpManager := mcp.NewManager(nil)
	ctx := context.Background()

	err = mcpManager.StartServer(ctx, mcp.ServerConfig{
		Name:    "reg",
		Command: []string{"bash", scriptPath},
		Type:    "stdio",
	})
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer mcpManager.StopAll()

	// Create tool registry and register MCP tools
	registry := tools.NewRegistry(nil)

	defs := mcpManager.AllLLMDefinitions()
	for _, def := range defs {
		serverName := ""
		for i, c := range def.Function.Name {
			if c == '.' {
				serverName = def.Function.Name[:i]
				break
			}
		}
		tool := mcp.NewMCPTool(def, mcpManager, serverName)
		registry.Register(tool)
	}

	// Verify tools are registered
	if count := registry.Count(); count != 2 {
		t.Errorf("Expected 2 tools registered, got %d", count)
	}

	// Verify tool names
	tool1 := registry.Get("reg.tool1")
	if tool1 == nil {
		t.Error("Expected reg.tool1 to be registered")
	}

	tool2 := registry.Get("reg.tool2")
	if tool2 == nil {
		t.Error("Expected reg.tool2 to be registered")
	}

	// Execute through registry
	result, err := registry.Execute(ctx, "reg.tool1", map[string]any{})
	if err != nil {
		t.Fatalf("Execute through registry failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected successful execution, got error: %s", result.Error)
	}
}
