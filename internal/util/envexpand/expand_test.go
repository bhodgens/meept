package envexpand_test

import (
	"os"
	"testing"

	"github.com/caimlas/meept/internal/util/envexpand"
)

func TestExpand(t *testing.T) {
	os.Setenv("TEST_FOO", "hello")
	os.Setenv("TEST_BAR", "world")
	defer func() {
		os.Unsetenv("TEST_FOO")
		os.Unsetenv("TEST_BAR")
	}()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "$VAR syntax",
			in:   "value is $TEST_FOO",
			want: "value is hello",
		},
		{
			name: "${VAR} syntax",
			in:   "value is ${TEST_BAR}",
			want: "value is world",
		},
		{
			name: "mixed syntax",
			in:   "$TEST_FOO and ${TEST_BAR}",
			want: "hello and world",
		},
		{
			name: "undefined variable (underscore is part of var name)",
			in:   "prefix_$MISSING_VAR_suffix",
			want: "prefix_",
		},
		{
			name: "no variables",
			in:   "plain text",
			want: "plain text",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := envexpand.Expand(tt.in)
			if got != tt.want {
				t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandWithPlaceholders(t *testing.T) {
	os.Setenv("TEST_FOO", "hello")
	defer func() { os.Unsetenv("TEST_FOO") }()

	testcases := []struct {
		name         string
		in           string
		placeholders envexpand.PlaceholderVars
		want         string
	}{
		{
			name:         "placeholder preserved",
			in:           "${MODEL_PATH}",
			placeholders: envexpand.PlaceholderVars{"MODEL_PATH": true},
			want:         "${MODEL_PATH}",
		},
		{
			name:         "non-placeholder expanded",
			in:           "$TEST_FOO and ${MODEL_PATH}",
			placeholders: envexpand.PlaceholderVars{"MODEL_PATH": true},
			want:         "hello and ${MODEL_PATH}",
		},
		{
			name:         "no placeholders map",
			in:           "$TEST_FOO and ${MODEL_PATH}",
			placeholders: nil,
			want:         "hello and ",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := envexpand.ExpandWithPlaceholders(tc.in, tc.placeholders)
			if got != tc.want {
				t.Errorf("ExpandWithPlaceholders(%q, ...) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
