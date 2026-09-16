package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

// stubBackend is a controllable SearchMCPBackend for the provider tests.
type stubBackend struct {
	server  string
	ok      bool
	result  *tools.ToolResult
	err     error
	gotName string
	gotArgs map[string]any
	calls   int
}

func (b *stubBackend) SearchServer() (string, bool) { return b.server, b.ok }

func (b *stubBackend) Call(ctx context.Context, fullName string, args map[string]any) (*tools.ToolResult, error) {
	b.calls++
	b.gotName = fullName
	b.gotArgs = args
	return b.result, b.err
}

func searxngJSON(t *testing.T, results string) string {
	t.Helper()
	return `{"query":"q","results":[` + results + `]}`
}

func TestMCPSearchProvider_Search(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		backend     *stubBackend
		wantErr     bool
		wantErrSub  string
		wantCount   int
		wantFirst   string
		wantErrNone bool
	}{
		{
			name:       "nil backend reports no backend",
			backend:    nil,
			wantErr:    true,
			wantErrSub: "no search mcp backend",
		},
		{
			name:       "no connected server reports no backend",
			backend:    &stubBackend{ok: false},
			wantErr:    true,
			wantErrSub: "no search mcp backend",
		},
		{
			name: "call error surfaces wrapped",
			backend: &stubBackend{
				server: "searxng.search", ok: true,
				err: errors.New("server exploded"),
			},
			wantErr:    true,
			wantErrSub: "search mcp call failed",
		},
		{
			name: "unparseable payload is an error",
			backend: &stubBackend{
				server: "searxng.search", ok: true,
				result: tools.NewSuccessResult("<html>not json</html>"),
			},
			wantErr:    true,
			wantErrSub: "unparseable",
		},
		{
			name: "searxng json parses url+snippet",
			backend: &stubBackend{
				server: "searxng.search", ok: true,
				result: tools.NewSuccessResult(searxngJSON(t,
					`{"title":"Example","url":"https://example.com","snippet":"An example"}`)),
			},
			wantCount: 1,
			wantFirst: "https://example.com",
		},
		{
			name: "link+content spellings accepted",
			backend: &stubBackend{
				server: "searxng.search", ok: true,
				result: tools.NewSuccessResult(searxngJSON(t,
					`{"title":"Alt","link":"https://alt.example","content":"alt content"}`)),
			},
			wantCount: 1,
			wantFirst: "https://alt.example",
		},
		{
			name: "entries without url or title skipped",
			backend: &stubBackend{
				server: "searxng.search", ok: true,
				result: tools.NewSuccessResult(searxngJSON(t,
					`{"title":"No URL","snippet":"x"},`+
						`{"url":"https://no-title.example","snippet":"y"},`+
						`{"title":"Good","url":"https://good.example","snippet":"z"}`)),
			},
			wantCount: 1,
			wantFirst: "https://good.example",
		},
		{
			name: "isError result is an error",
			backend: &stubBackend{
				server: "searxng.search", ok: true,
				result: tools.NewErrorResult("searxng down"),
			},
			wantErr:    true,
			wantErrSub: "searxng down",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewMCPSearchProvider(0)
			if tt.backend != nil {
				p.SetBackend(tt.backend)
			}
			res, err := p.Search(context.Background(), "q", 5)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", res)
				}
				if tt.wantErrSub != "" && !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Errorf("error %q missing substring %q", err.Error(), tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Count != tt.wantCount {
				t.Errorf("Count = %d, want %d", res.Count, tt.wantCount)
			}
			if tt.wantFirst != "" && (len(res.Results) == 0 || res.Results[0].URL != tt.wantFirst) {
				t.Errorf("first URL = %+v, want %q", res.Results, tt.wantFirst)
			}
		})
	}
}

func TestMCPSearchProvider_PassesQueryAndLimit(t *testing.T) {
	t.Parallel()
	backend := &stubBackend{
		server: "searxng.search", ok: true,
		result: tools.NewSuccessResult(searxngJSON(t, "")),
	}
	p := NewMCPSearchProvider(0)
	p.SetBackend(backend)
	if _, err := p.Search(context.Background(), "flaky tests", 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if backend.gotName != "searxng.search" {
		t.Errorf("called tool = %q", backend.gotName)
	}
	if backend.gotArgs["query"] != "flaky tests" {
		t.Errorf("query arg = %v", backend.gotArgs["query"])
	}
	if backend.gotArgs["limit"] != 7 {
		t.Errorf("limit arg = %v", backend.gotArgs["limit"])
	}
}

func TestMCPSearchProvider_NilSetterGuard(t *testing.T) {
	t.Parallel()
	p := NewMCPSearchProvider(0)
	p.SetBackend(nil) // must be a no-op, not a panic
	if _, err := p.Search(context.Background(), "q", 5); !errors.Is(err, ErrNoSearchBackend) {
		t.Errorf("err = %v, want ErrNoSearchBackend", err)
	}
}

func TestParseSearxngPayload_LimitAndTruncation(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString(`{"query":"q","results":[`)
	for i := 0; i < 5; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"title":"t","url":"https://u` + strings.Repeat("x", 1) + string(rune('a'+i)) + `.example","snippet":"s"}`)
	}
	sb.WriteString(`]}`)
	res, err := parseSearxngPayload(sb.String(), "q", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 3 {
		t.Errorf("Count = %d, want 3", res.Count)
	}
	if !res.Truncated {
		t.Error("Truncated = false, want true (5 results, limit 3)")
	}
}
