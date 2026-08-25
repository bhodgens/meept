package main

import (
	"strings"
	"testing"
)

func TestReadPastedCode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trims surrounding whitespace", input: "  abc123  \n", want: "abc123"},
		{name: "plain code", input: "xyz-456\n", want: "xyz-456"},
		{name: "empty input errors", input: "", wantErr: true},
		{name: "whitespace-only input errors", input: "   \n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readPastedCode(strings.NewReader(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("readPastedCode(%q) = %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("readPastedCode(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("readPastedCode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
