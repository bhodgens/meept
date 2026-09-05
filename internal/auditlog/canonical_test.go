package auditlog

import (
	"bytes"
	"math"
	"testing"
	"time"
)

func TestCanonicalJSON_SortsKeys(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want string
	}{
		{"two keys", map[string]any{"b": 1, "a": "x"}, `{"a":"x","b":1}`},
		{"nested", map[string]any{"z": map[string]any{"y": 2, "x": 1}}, `{"z":{"x":1,"y":2}}`},
		{"array order preserved", map[string]any{"l": []any{3, 1, 2}}, `{"l":[3,1,2]}`},
		{"no html escape", map[string]any{"s": "<a>&</a>"}, `{"s":"<a>&</a>"}`},
		{"empty map", map[string]any{}, `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalJSON(tt.in)
			if err != nil {
				t.Fatalf("CanonicalJSON: %v", err)
			}
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestCanonicalJSON_Numbers(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"int", 42, `42`},
		{"int64 max", int64(9223372036854775807), `9223372036854775807`},
		{"uint64 seq", uint64(18446744073709551615), `18446744073709551615`},
		{"float integral", float64(3), `3`},
		{"float frac", 0.5, `0.5`},
		{"negative zero normalized", math.Copysign(0, -1), `0`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalJSON(tt.in)
			if err != nil {
				t.Fatalf("CanonicalJSON(%v): %v", tt.in, err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s want %s", got, tt.want)
			}
		})
	}
}

func TestCanonicalJSON_RejectsUnsupported(t *testing.T) {
	if _, err := CanonicalJSON(time.Now()); err == nil {
		t.Fatal("time.Time must be rejected")
	}
	if _, err := CanonicalJSON(math.Inf(1)); err == nil {
		t.Fatal("Inf must be rejected")
	}
	if _, err := CanonicalJSON(struct{ A int }{1}); err == nil {
		t.Fatal("struct must be rejected")
	}
}
