package mcp

import "testing"

func TestValidateVersionPin(t *testing.T) {
	cases := []struct {
		name    string
		command []string
		wantErr bool
	}{
		{"pinned scoped npx", []string{"npx", "-y", "@modelcontextprotocol/server-memory@2.2.1"}, false},
		{"pinned plain npx", []string{"npx", "-y", "firecrawl-mcp@3.41.0"}, false},
		{"unpinned scoped npx", []string{"npx", "-y", "@modelcontextprotocol/server-memory"}, true},
		{"unpinned plain npx", []string{"npx", "-y", "mcp-server-docker"}, true},
		{"pinned uvx", []string{"uvx", "mcp-server-fetch==2026.11.18"}, false},
		{"unpinned uvx", []string{"uvx", "mcp-server-fetch"}, true},
		{"native binary", []string{"cua-driver", "mcp"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateVersionPin("test", tc.command)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateVersionPin(%v) error = %v, wantErr %v", tc.command, err, tc.wantErr)
			}
			if err != nil && (err.Server != "test" || !contains(err.Error(), "Pin an exact version")) {
				t.Fatalf("error must name server and exact fix, got %q", err.Error())
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
