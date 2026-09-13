package rpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// TestIsParameterError_Structured verifies that isParameterError classifies
// errors using structured detection (errors.Is/As) rather than substring
// heuristics that false-positive on common words.
//
// Pre-migration, isParameterError took a string and matched substrings like
// "expected", "type", "parse", etc. This test verifies the new behavior.
//
// Note: services.ErrInvalidInput is tested in internal/errcls/classify_test.go.
// This test cannot import services due to an import cycle
// (services -> scheduler -> rpc), so we focus on the *llm.APIError 400 path
// and the false-positive regressions.
func TestIsParameterError_Structured(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// True cases — structured parameter errors
		{"api 400", &llm.APIError{StatusCode: 400, Detail: "bad request"}, true},
		{"wrapped api 400", fmt.Errorf("handler: %w", &llm.APIError{StatusCode: 400}), true},

		// False-positive cases that the old substring heuristic incorrectly
		// classified as parameter errors. These MUST return false now.
		{"plain 'expected' string (old false positive)", errors.New("expected 1 result, got 0"), false},
		{"plain 'type' string (old false positive)", errors.New("type mismatch in data"), false},
		{"plain 'parse' string (old false positive)", errors.New("parse phase completed"), false},
		{"plain 'missing' string (old false positive)", errors.New("missing file on disk"), false},
		{"plain 'invalid' string (old false positive)", errors.New("invalid state reached"), false},
		{"plain 'required' string (old false positive)", errors.New("required field absent"), false},
		{"plain 'unmarshal' string (old false positive)", errors.New("unmarshal step skipped"), false},
		{"plain 'argument' string (old false positive)", errors.New("argument list too long"), false},
		{"plain 'param' string (old false positive)", errors.New("param export complete"), false},

		// Negative cases
		{"api 500", &llm.APIError{StatusCode: 500}, false},
		{"api 404", &llm.APIError{StatusCode: 404}, false},
		{"nil", nil, false},
		{"generic internal error", errors.New("internal failure"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isParameterError(tt.err); got != tt.want {
				t.Errorf("isParameterError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestStatusHandler_TokenLimits verifies the daemon status payload carries the
// configured hourly/daily token limits. Clients must be able to print usage
// against the LIMIT instead of inferring a total from used+remaining, which
// reads as 0 remaining (and therefore a bogus 100%) whenever the limit is
// disabled.
func TestStatusHandler_TokenLimits(t *testing.T) {
	tests := []struct {
		name       string
		getter     func() (int, int)
		wantHourly int
		wantDaily  int
	}{
		{
			name:       "getter unwired reports zero limits",
			getter:     nil,
			wantHourly: 0,
			wantDaily:  0,
		},
		{
			name:       "limit disabled reports zero limits",
			getter:     func() (int, int) { return 0, 0 },
			wantHourly: 0,
			wantDaily:  0,
		},
		{
			name:       "configured limits are reported",
			getter:     func() (int, int) { return 50000, 500000 },
			wantHourly: 50000,
			wantDaily:  500000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(&Config{SocketPath: ""}, bus.New(nil, nil), slog.Default())
			srv.BudgetStatusGetter = func() (int, int, int, int, int, int, float64, float64, float64, float64, float64, float64, int, int) {
				// hourly used with 0 remaining: the disabled-limit shape that
				// used to render as 100%.
				return 12345, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
			}
			srv.TokenLimitGetter = tt.getter
			srv.registerBuiltinHandlers()

			srv.mu.RLock()
			handler := srv.handlers["status"]
			srv.mu.RUnlock()
			if handler == nil {
				t.Fatal("status handler not registered")
			}

			raw, err := handler(context.Background(), nil)
			if err != nil {
				t.Fatalf("status handler: %v", err)
			}
			result, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("status handler returned %T, want map[string]any", raw)
			}

			hourly, ok := result["hourly_token_limit"].(int)
			if !ok {
				t.Fatalf("hourly_token_limit missing or not an int: %#v", result["hourly_token_limit"])
			}
			if hourly != tt.wantHourly {
				t.Errorf("hourly_token_limit = %d, want %d", hourly, tt.wantHourly)
			}

			daily, ok := result["daily_token_limit"].(int)
			if !ok {
				t.Fatalf("daily_token_limit missing or not an int: %#v", result["daily_token_limit"])
			}
			if daily != tt.wantDaily {
				t.Errorf("daily_token_limit = %d, want %d", daily, tt.wantDaily)
			}

			if used, ok := result["tokens_used"].(int); !ok || used != 12345 {
				t.Errorf("tokens_used = %#v, want 12345", result["tokens_used"])
			}
		})
	}
}
