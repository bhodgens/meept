package validator

import (
	"errors"
	"strings"
	"testing"
)

func TestBuiltinRegistry_KnownNames(t *testing.T) {
	tests := []struct {
		name     string
		cfg      BuiltinConfig
		wantName string
		wantType string // concrete type marker via Name/behavior probing
	}{
		{name: "json_format", cfg: BuiltinConfig{}, wantName: "json_format", wantType: "*JSONFormatFilter"},
		{name: "language_en", cfg: BuiltinConfig{}, wantName: "language_en", wantType: "*LanguageFilter"},
		{name: "lint_go", cfg: BuiltinConfig{}, wantName: "lint_go", wantType: "*GoLintFilter"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filter, err := NewBuiltinFilter(tc.name, tc.cfg)
			if err != nil {
				t.Fatalf("NewBuiltinFilter(%q) error: %v", tc.name, err)
			}
			if filter == nil {
				t.Fatalf("NewBuiltinFilter(%q) returned nil filter with nil error", tc.name)
			}
			if filter.Name() != tc.wantName {
				t.Fatalf("Name() = %q, want %q", filter.Name(), tc.wantName)
			}
		})
	}
}

func TestBuiltinRegistry_TypeDispatch(t *testing.T) {
	// Verify the registry returns the RIGHT concrete type, not just a
	// non-nil filter.
	jsonF, err := NewBuiltinFilter("json_format", BuiltinConfig{})
	if err != nil {
		t.Fatalf("json_format: %v", err)
	}
	if _, ok := jsonF.(*JSONFormatFilter); !ok {
		t.Fatalf("json_format returned %T, want *JSONFormatFilter", jsonF)
	}

	langF, err := NewBuiltinFilter("language_en", BuiltinConfig{ExpectedLang: "en"})
	if err != nil {
		t.Fatalf("language_en: %v", err)
	}
	if _, ok := langF.(*LanguageFilter); !ok {
		t.Fatalf("language_en returned %T, want *LanguageFilter", langF)
	}
	if got := langF.Name(); got != "language_en" {
		t.Fatalf("language_en Name() = %q", got)
	}

	langDe, err := NewBuiltinFilter("language_en", BuiltinConfig{ExpectedLang: "de"})
	if err != nil {
		t.Fatalf("language_en(de): %v", err)
	}
	if got := langDe.Name(); got != "language_de" {
		t.Fatalf("language_en(de) Name() = %q, want language_de", got)
	}

	lintF, err := NewBuiltinFilter("lint_go", BuiltinConfig{GofmtBin: "gofmt", GoBin: "go"})
	if err != nil {
		t.Fatalf("lint_go: %v", err)
	}
	if _, ok := lintF.(*GoLintFilter); !ok {
		t.Fatalf("lint_go returned %T, want *GoLintFilter", lintF)
	}
}

func TestBuiltinRegistry_UnknownName(t *testing.T) {
	filter, err := NewBuiltinFilter("spellcheck", BuiltinConfig{})
	if err == nil {
		t.Fatal("unknown name returned nil error")
	}
	if filter != nil {
		t.Fatalf("unknown name returned non-nil filter %T", filter)
	}
	for _, known := range []string{"json_format", "language_en", "lint_go"} {
		if !strings.Contains(err.Error(), known) {
			t.Fatalf("error %q does not list known name %q", err.Error(), known)
		}
	}
	if !strings.Contains(err.Error(), "spellcheck") {
		t.Fatalf("error %q does not echo the unknown name", err.Error())
	}
	if !errors.Is(err, errUnknownBuiltin) {
		t.Fatalf("error %q is not errUnknownBuiltin", err.Error())
	}
}
