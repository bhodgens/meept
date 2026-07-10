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

func TestPruneOldVersions(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	datasetsDir := filepath.Join(tmp, "datasets")
	versionsDir := filepath.Join(tmp, "versions")

	datasets, err := NewDomainDatasets(datasetsDir)
	if err != nil {
		t.Fatalf("NewDomainDatasets: %v", err)
	}
	if err := datasets.Append("code", TrainingExample{Instruction: "q", Output: "a"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, err := CreateSnapshot("code", datasetsDir, versionsDir); err != nil {
			t.Fatalf("CreateSnapshot %d: %v", i, err)
		}
	}

	pruned, err := PruneOldVersions("code", versionsDir, 3)
	if err != nil {
		t.Fatalf("PruneOldVersions: %v", err)
	}
	if pruned != 2 {
		t.Errorf("expected 2 pruned, got %d", pruned)
	}

	// v1 and v2 should be gone; v3-v5 remain.
	for _, gone := range []string{"code_v1.jsonl", "code_v2.jsonl"} {
		if _, err := os.Stat(filepath.Join(versionsDir, gone)); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, stat err=%v", gone, err)
		}
	}
	for _, kept := range []string{"code_v3.jsonl", "code_v4.jsonl", "code_v5.jsonl"} {
		if _, err := os.Stat(filepath.Join(versionsDir, kept)); err != nil {
			t.Errorf("expected %s kept: %v", kept, err)
		}
	}

	// versions.json should only have 3 entries for code.
	data, err := os.ReadFile(filepath.Join(versionsDir, "versions.json"))
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var versions []DatasetVersion
	if err := json.Unmarshal(data, &versions); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("expected 3 version metadata entries, got %d", len(versions))
	}

	// keep <= 0 is a no-op
	pruned, err = PruneOldVersions("code", versionsDir, 0)
	if err != nil {
		t.Fatalf("PruneOldVersions keep=0: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned with keep=0, got %d", pruned)
	}
}
