package learning

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewDomainDatasets(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	datasets, err := NewDomainDatasets(tmp)
	if err != nil {
		t.Fatalf("NewDomainDatasets failed: %v", err)
	}
	if datasets == nil {
		t.Fatal("expected non-nil datasets")
	}

	if _, err := NewDomainDatasets(""); err == nil {
		t.Fatal("expected error for empty baseDir")
	}
}

func TestAppend(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	datasets, err := NewDomainDatasets(tmp)
	if err != nil {
		t.Fatalf("NewDomainDatasets failed: %v", err)
	}

	example := TrainingExample{
		Instruction: "how to parse json in go",
		Output:      "use encoding/json",
		Metadata: ExampleMetadata{
			Source:       "agent_research",
			Domain:       "code",
			QualityScore: 0.85,
		},
	}

	if err := datasets.Append("code", example); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Verify the file was created.
	filePath := filepath.Join(tmp, "code.jsonl")
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open dataset file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("expected at least one line")
	}

	var result TrainingExample
	if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Instruction != "how to parse json in go" {
		t.Errorf("expected instruction 'how to parse json in go', got %q", result.Instruction)
	}
	if result.Metadata.Domain != "code" {
		t.Errorf("expected domain 'code', got %q", result.Metadata.Domain)
	}
}

func TestAppendMultipleDomains(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	datasets, err := NewDomainDatasets(tmp)
	if err != nil {
		t.Fatalf("NewDomainDatasets failed: %v", err)
	}

	example1 := TrainingExample{Instruction: "code question", Output: "code answer"}
	example2 := TrainingExample{Instruction: "debugging question", Output: "debug answer"}

	if err := datasets.Append("code", example1); err != nil {
		t.Fatalf("Append code failed: %v", err)
	}
	if err := datasets.Append("debugging", example2); err != nil {
		t.Fatalf("Append debugging failed: %v", err)
	}

	// Verify separate files exist.
	if _, err := os.Stat(filepath.Join(tmp, "code.jsonl")); err != nil {
		t.Fatalf("code.jsonl missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "debugging.jsonl")); err != nil {
		t.Fatalf("debugging.jsonl missing: %v", err)
	}
}
