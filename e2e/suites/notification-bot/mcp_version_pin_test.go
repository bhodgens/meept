//go:build e2e

// mcp-version-pin-01 (issue #166): an unpinned npx/uvx command is
// refused before any process spawn; a pinned command passes validation.
package notificationbot

import (
 "strings"
 "testing"

 "github.com/caimlas/meept/internal/tools/mcp"
)

func TestUnpinnedMCPCommandRefusedBeforeSpawn(t *testing.T) {
 err := mcp.ValidateVersionPin("bad", []string{"npx", "-y", "@modelcontextprotocol/server-memory"})
 if err == nil { t.Fatal("unpinned command must be refused") }
 if !strings.Contains(err.Error(), "SECURITY: MCP server 'bad'") { t.Fatalf("error must name the server: %q", err.Error()) }
 if !strings.Contains(err.Error(), "Pin an exact version") { t.Fatalf("error must name the fix: %q", err.Error()) }
 for _, c := range [][]string{
  {"npx","-y","@modelcontextprotocol/server-memory@2.2.1"},
  {"uvx","mcp-server-fetch==2026.11.18"},
  {"cua-driver","mcp"},
 } {
  if e := mcp.ValidateVersionPin("ok", c); e != nil { t.Errorf("pinned command %v refused: %v", c, e) }
 }
}
