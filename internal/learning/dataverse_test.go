package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateSnapshot(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	datasetsDir := filepath.Join(tmp, "datasets")
	versionsDir := filepath.Join(tmp, "versions")

	// Create a dataset with a few examples.
	datasets, err := NewDomainDatasets(datasetsDir)
	if err != nil {
		t.Fatalf("NewDomainDatasets failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		example := TrainingExample{
			Instruction: "test question",
			Output:      "test answer",
		}
		if err := datasets.Append("code", example); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Create a snapshot.
	version, err := CreateSnapshot("code", datasetsDir, versionsDir)
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	if version == nil {
		t.Fatal("expected non-nil version")
	}
	if version.Version != 1 {
		t.Errorf("expected version 1, got %d", version.Version)
	}
	if version.Domain != "code" {
		t.Errorf("expected domain 'code', got %q", version.Domain)
	}
	if version.MD5 == "" {
		t.Error("expected non-empty MD5")
	}
	if version.ExampleCount != 3 {
		t.Errorf("expected 3 examples, got %d", version.ExampleCount)
	}

	// Verify the snapshot file exists.
	snapshotPath := filepath.Join(versionsDir, "code_v1.jsonl")
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("snapshot file not created: %v", err)
	}

	// Verify versions.json exists and has 1 entry.
	versionsJSONPath := filepath.Join(versionsDir, "versions.json")
	data, err := os.ReadFile(versionsJSONPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var versions []DatasetVersion
	if err := json.Unmarshal(data, &versions); err != nil {
		t.Fatalf("unmarshal versions.json: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("expected 1 version, got %d", len(versions))
	}

	// Create another snapshot; version should be 2.
	version2, err := CreateSnapshot("code", datasetsDir, versionsDir)
	if err != nil {
		t.Fatalf("second CreateSnapshot failed: %v", err)
	}
	if version2.Version != 2 {
		t.Errorf("expected version 2, got %d", version2.Version)
	}

	// Verify versions.json now has 2 entries.
	data2, err := os.ReadFile(versionsJSONPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	if err := json.Unmarshal(data2, &versions); err != nil {
		t.Fatalf("unmarshal versions.json: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}
