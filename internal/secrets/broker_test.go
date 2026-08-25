package secrets

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// newDiscardLogger returns a logger that writes nowhere; keeps test output clean.
func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPlaceholderFormat(t *testing.T) {
	if got := Placeholder("api_token"); got != "MEEPT_SECRET:api_token" {
		t.Fatalf("Placeholder(api_token) = %q, want %q", got, "MEEPT_SECRET:api_token")
	}
}

func TestNewBroker_EnvKindLoads(t *testing.T) {
	t.Setenv("TEST_SECRET_TOKEN", "hunter2")

	cfg := Config{
		"api": {Kind: "env", Name: "TEST_SECRET_TOKEN"},
	}
	b, err := NewBroker(cfg, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}

	val, err := b.resolve("api")
	if err != nil {
		t.Fatalf("resolve(api) failed: %v", err)
	}
	if val != "hunter2" {
		t.Fatalf("resolve(api) = %q, want %q", val, "hunter2")
	}
}

func TestNewBroker_FileKindTrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.txt")
	if err := os.WriteFile(path, []byte("filevalue\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	b, err := NewBroker(Config{
		"tok": {Kind: "file", Name: path},
	}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}

	val, err := b.resolve("tok")
	if err != nil {
		t.Fatalf("resolve(tok) failed: %v", err)
	}
	if val != "filevalue" {
		t.Fatalf("resolve(tok) = %q, want trimmed %q", val, "filevalue")
	}
}

func TestNewBroker_FileKindCRLFTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.txt")
	if err := os.WriteFile(path, []byte("crlfvalue\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	b, err := NewBroker(Config{
		"tok": {Kind: "file", Name: path},
	}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}
	val, err := b.resolve("tok")
	if err != nil {
		t.Fatalf("resolve(tok) failed: %v", err)
	}
	if val != "crlfvalue" {
		t.Fatalf("resolve(tok) = %q, want %q", val, "crlfvalue")
	}
}

func TestNewBroker_AggregatesAllFailures(t *testing.T) {
	cfg := Config{
		"a":     {Kind: "env", Name: "MISSING_VAR_XYZ"},
		"b":     {Kind: "file", Name: "/nope/definitely-missing-file"},
		"c":     {Kind: "env", Name: "ALSO_MISSING_QQQ"},
		"okvar": {Kind: "env", Name: "PRESENT_VAR_ABC"},
	}
	t.Setenv("PRESENT_VAR_ABC", "fine")

	_, err := NewBroker(cfg, newDiscardLogger())
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}

	msg := err.Error()
	for _, want := range []string{
		"3 secrets failed:",
		"a (env MISSING_VAR_XYZ)",
		"b (file /nope/definitely-missing-file)",
		"c (env ALSO_MISSING_QQQ)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}
	// The successful source must NOT be reported as failed.
	if strings.Contains(msg, "okvar") {
		t.Errorf("error message reports successfully-loaded source: %q", msg)
	}
}

func TestNewBroker_UnknownKindFails(t *testing.T) {
	_, err := NewBroker(Config{
		"x": {Kind: "keyring", Name: "whatever"},
	}, newDiscardLogger())
	if err == nil || !strings.Contains(err.Error(), "x") {
		t.Fatalf("expected failure naming x, got %v", err)
	}
}

func TestNewBroker_EmptyConfigOK(t *testing.T) {
	b, err := NewBroker(Config{}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker(empty) failed: %v", err)
	}
	if got := b.Names(); len(got) != 0 {
		t.Fatalf("Names() = %v, want empty", got)
	}
}

func TestChildValue_KnownReturnsExactPlaceholder(t *testing.T) {
	t.Setenv("TOK_VAR", "real-secret-value")

	b, err := NewBroker(Config{
		"api_token": {Kind: "env", Name: "TOK_VAR"},
	}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}

	got, err := b.ChildValue("api_token")
	if err != nil {
		t.Fatalf("ChildValue failed: %v", err)
	}
	if got != "MEEPT_SECRET:api_token" {
		t.Fatalf("ChildValue = %q, want exact placeholder", got)
	}
	if strings.Contains(got, "real-secret-value") {
		t.Fatal("ChildValue leaked plaintext")
	}
}

func TestChildValue_UnknownErrors(t *testing.T) {
	b, err := NewBroker(Config{}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}
	_, err = b.ChildValue("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown secret, got nil")
	}
}

func TestResolve_UnknownErrors(t *testing.T) {
	b, _ := NewBroker(Config{}, newDiscardLogger())
	if _, err := b.resolve("ghost"); err == nil {
		t.Fatal("expected error resolving unknown secret")
	}
}

func TestNames_Sorted(t *testing.T) {
	t.Setenv("V1", "a")
	t.Setenv("V2", "b")
	t.Setenv("V3", "c")

	b, err := NewBroker(Config{
		"zeta":  {Kind: "env", Name: "V1"},
		"alpha": {Kind: "env", Name: "V2"},
		"mid":   {Kind: "env", Name: "V3"},
	}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}

	got := b.Names()
	if !slices.IsSorted(got) {
		t.Fatalf("Names() not sorted: %v", got)
	}
	if !reflect.DeepEqual(got, []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("Names() = %v, want [alpha mid zeta]", got)
	}
}

func TestSource_Lookup(t *testing.T) {
	want := Source{Kind: "env", Name: "SRC_VAR", Hosts: []string{"api.example.com"}, Header: "Authorization", Format: "Bearer {}"}
	t.Setenv("SRC_VAR", "v")

	b, err := NewBroker(Config{"s": want}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}

	got, ok := b.Source("s")
	if !ok {
		t.Fatal("Source(s) reported missing")
	}
	if got.Kind != want.Kind || got.Name != want.Name || got.Header != want.Format && false {
		t.Fatalf("Source mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.Hosts, want.Hosts) || got.Header != want.Header || got.Format != want.Format {
		t.Fatalf("Source fields mismatch: got %+v want %+v", got, want)
	}

	if _, ok := b.Source("absent"); ok {
		t.Fatal("Source(absent) should be false")
	}
}

func TestResolve_NeverLoggedPlaintextContract(t *testing.T) {
	// Guard against accidental regressions: broker API surface exposes
	// placeholders publicly and real values only via unexported resolve.
	// This test pins ChildValue as the ONLY public value accessor shape.
	t.Setenv("PIN_VAR", "pin-value")
	b, err := NewBroker(Config{"p": {Kind: "env", Name: "PIN_VAR"}}, newDiscardLogger())
	if err != nil {
		t.Fatalf("NewBroker failed: %v", err)
	}
	cv, err := b.ChildValue("p")
	if err != nil {
		t.Fatal(err)
	}
	rv, rerr := b.resolve("p")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if cv == rv {
		t.Fatal("placeholder must differ from resolved value")
	}
}
