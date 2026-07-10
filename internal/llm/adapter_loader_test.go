package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAdapterRegistry_RoundTripsProvenance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adapters.json")

	want := AdapterRegistry{
		Version:     1,
		GeneratedAt: "2026-07-09T12:00:00Z",
		Adapters: []AdapterEntry{
			{
				ID:          "code-lfm2.5-8b-v1",
				Domain:      "code",
				Model:       "lfm2.5-8b",
				Path:        "~/.meept/adapters/code/lfm2.5-8b-v1",
				CreatedAt:   "2026-07-09T10:00:00Z",
				TrainingMD5: "abc123",
				Enabled:     true,
			},
		},
	}
	data, err := json.MarshalIndent(&want, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LoadAdapterRegistry(path)
	if err != nil {
		t.Fatalf("LoadAdapterRegistry: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
	if got.GeneratedAt != "2026-07-09T12:00:00Z" {
		t.Errorf("GeneratedAt = %q, want %q", got.GeneratedAt, "2026-07-09T12:00:00Z")
	}
	if len(got.Adapters) != 1 {
		t.Fatalf("len(Adapters) = %d, want 1", len(got.Adapters))
	}
	if got.Adapters[0].ID != "code-lfm2.5-8b-v1" {
		t.Errorf("Adapters[0].ID = %q, want %q", got.Adapters[0].ID, "code-lfm2.5-8b-v1")
	}
}

func TestLoadAdapterRegistry_MissingFileReturnsEmpty(t *testing.T) {
	got, err := LoadAdapterRegistry(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadAdapterRegistry: %v", err)
	}
	if got == nil {
		t.Fatal("registry is nil")
	}
	if got.Version != 0 {
		t.Errorf("Version = %d, want 0", got.Version)
	}
	if len(got.Adapters) != 0 {
		t.Errorf("len(Adapters) = %d, want 0", len(got.Adapters))
	}
}
