package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	if store == nil {
		t.Fatal("store is nil")
	}

	expectedPath := filepath.Join(tmpDir, "jobs.json")
	if store.FilePath() != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, store.FilePath())
	}
}

func TestStoreEmptyLoad(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Load from non-existent file
	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestStoreSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Create test jobs
	jobs := []JobConfig{
		{
			ID:       "job-1",
			Name:     "Test Job 1",
			Type:     JobTypeReminder,
			Schedule: "@hourly",
			Enabled:  true,
			ReminderConfig: &ReminderJobConfig{
				Message: "Reminder 1",
			},
		},
		{
			ID:       "job-2",
			Name:     "Test Job 2",
			Type:     JobTypeShell,
			Schedule: "@daily",
			Enabled:  true,
			ShellConfig: &ShellJobConfig{
				Command: "echo",
				Args:    []string{"hello"},
			},
		},
	}

	// Save
	if err := store.Save(jobs); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Check file exists
	if _, err := os.Stat(store.FilePath()); os.IsNotExist(err) {
		t.Fatal("jobs file was not created")
	}

	// Load
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(loaded))
	}

	// Verify contents
	jobMap := make(map[string]JobConfig)
	for _, j := range loaded {
		jobMap[j.ID] = j
	}

	if j, ok := jobMap["job-1"]; !ok {
		t.Error("job-1 not found")
	} else {
		if j.Name != "Test Job 1" {
			t.Errorf("expected name 'Test Job 1', got %q", j.Name)
		}
		if j.ReminderConfig == nil || j.ReminderConfig.Message != "Reminder 1" {
			t.Error("reminder config not preserved")
		}
	}

	if j, ok := jobMap["job-2"]; !ok {
		t.Error("job-2 not found")
	} else {
		if j.ShellConfig == nil || j.ShellConfig.Command != "echo" {
			t.Error("shell config not preserved")
		}
	}
}

func TestStoreAdd(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	job := JobConfig{
		ID:       "add-test",
		Name:     "Add Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	}

	if err := store.Add(job); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check count
	if store.Count() != 1 {
		t.Errorf("expected count 1, got %d", store.Count())
	}

	// Get job
	retrieved, ok := store.Get("add-test")
	if !ok {
		t.Fatal("job not found after Add")
	}

	if retrieved.Name != "Add Test" {
		t.Errorf("expected name 'Add Test', got %q", retrieved.Name)
	}
}

func TestStoreRemove(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add jobs
	store.Add(JobConfig{
		ID:       "remove-1",
		Name:     "Remove 1",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})
	store.Add(JobConfig{
		ID:       "remove-2",
		Name:     "Remove 2",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	if store.Count() != 2 {
		t.Fatalf("expected count 2, got %d", store.Count())
	}

	// Remove one
	if err := store.Remove("remove-1"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if store.Count() != 1 {
		t.Errorf("expected count 1 after remove, got %d", store.Count())
	}

	// Try to remove non-existent
	if err := store.Remove("non-existent"); err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestStoreUpdate(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add job
	job := JobConfig{
		ID:       "update-test",
		Name:     "Original Name",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Original",
		},
	}
	store.Add(job)

	// Update
	job.Name = "Updated Name"
	job.ReminderConfig.Message = "Updated"
	if err := store.Update(job); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update
	retrieved, _ := store.Get("update-test")
	if retrieved.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", retrieved.Name)
	}
	if retrieved.ReminderConfig.Message != "Updated" {
		t.Errorf("expected message 'Updated', got %q", retrieved.ReminderConfig.Message)
	}

	// Try to update non-existent
	job.ID = "non-existent"
	if err := store.Update(job); err == nil {
		t.Error("expected error for non-existent job")
	}
}

func TestStoreUpdateLastRun(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add job
	store.Add(JobConfig{
		ID:       "lastrun-test",
		Name:     "Last Run Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	// Update last run
	runTime := time.Now()
	if err := store.UpdateLastRun("lastrun-test", runTime, nil); err != nil {
		t.Fatalf("UpdateLastRun failed: %v", err)
	}

	// Verify
	job, _ := store.Get("lastrun-test")
	if job.LastRunAt == nil {
		t.Fatal("LastRunAt not set")
	}
	if job.RunCount != 1 {
		t.Errorf("expected RunCount 1, got %d", job.RunCount)
	}
	if job.LastError != "" {
		t.Errorf("expected no error, got %q", job.LastError)
	}

	// Update with error
	err = store.UpdateLastRun("lastrun-test", runTime, os.ErrNotExist)
	if err != nil {
		t.Fatalf("UpdateLastRun with error failed: %v", err)
	}

	job, _ = store.Get("lastrun-test")
	if job.RunCount != 2 {
		t.Errorf("expected RunCount 2, got %d", job.RunCount)
	}
	if job.LastError == "" {
		t.Error("expected LastError to be set")
	}
}

func TestStoreSetEnabled(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add enabled job
	store.Add(JobConfig{
		ID:       "enabled-test",
		Name:     "Enabled Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		Enabled:  true,
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	// Disable
	if err := store.SetEnabled("enabled-test", false); err != nil {
		t.Fatalf("SetEnabled(false) failed: %v", err)
	}

	job, _ := store.Get("enabled-test")
	if job.Enabled {
		t.Error("expected job to be disabled")
	}

	// Re-enable
	if err := store.SetEnabled("enabled-test", true); err != nil {
		t.Fatalf("SetEnabled(true) failed: %v", err)
	}

	job, _ = store.Get("enabled-test")
	if !job.Enabled {
		t.Error("expected job to be enabled")
	}
}

func TestStoreList(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Empty list
	if len(store.List()) != 0 {
		t.Error("expected empty list")
	}

	// Add jobs
	for i := 0; i < 5; i++ {
		store.Add(JobConfig{
			ID:       string(rune('a' + i)),
			Name:     "Job",
			Type:     JobTypeReminder,
			Schedule: "@hourly",
			ReminderConfig: &ReminderJobConfig{
				Message: "Test",
			},
		})
	}

	jobs := store.List()
	if len(jobs) != 5 {
		t.Errorf("expected 5 jobs, got %d", len(jobs))
	}
}

func TestStoreClear(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add jobs
	for i := 0; i < 3; i++ {
		store.Add(JobConfig{
			ID:       string(rune('a' + i)),
			Name:     "Job",
			Type:     JobTypeReminder,
			Schedule: "@hourly",
			ReminderConfig: &ReminderJobConfig{
				Message: "Test",
			},
		})
	}

	if store.Count() != 3 {
		t.Fatalf("expected 3 jobs, got %d", store.Count())
	}

	// Clear
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if store.Count() != 0 {
		t.Errorf("expected 0 jobs after clear, got %d", store.Count())
	}
}

func TestStoreExportImport(t *testing.T) {
	tmpDir := t.TempDir()

	store1, err := NewStore(filepath.Join(tmpDir, "store1"))
	if err != nil {
		t.Fatalf("NewStore (1) failed: %v", err)
	}

	// Add jobs
	store1.Add(JobConfig{
		ID:       "export-1",
		Name:     "Export 1",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Test 1",
		},
	})
	store1.Add(JobConfig{
		ID:       "export-2",
		Name:     "Export 2",
		Type:     JobTypeShell,
		Schedule: "@daily",
		ShellConfig: &ShellJobConfig{
			Command: "echo",
		},
	})

	// Export
	data, err := store1.Export()
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("exported data is empty")
	}

	// Create new store and import
	store2, err := NewStore(filepath.Join(tmpDir, "store2"))
	if err != nil {
		t.Fatalf("NewStore (2) failed: %v", err)
	}

	if err := store2.Import(data, false); err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if store2.Count() != 2 {
		t.Errorf("expected 2 jobs after import, got %d", store2.Count())
	}

	// Verify imported jobs
	job, ok := store2.Get("export-1")
	if !ok {
		t.Fatal("export-1 not found after import")
	}
	if job.Name != "Export 1" {
		t.Errorf("expected name 'Export 1', got %q", job.Name)
	}
}

func TestStoreImportReplace(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add existing job
	store.Add(JobConfig{
		ID:       "existing",
		Name:     "Existing",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Existing",
		},
	})

	// Import with replace=false (merge)
	importData := `{
		"version": 1,
		"jobs": [
			{
				"id": "imported",
				"name": "Imported",
				"type": "reminder",
				"schedule": "@hourly",
				"reminder_config": {"message": "Imported"}
			}
		]
	}`

	if err := store.Import([]byte(importData), false); err != nil {
		t.Fatalf("Import (merge) failed: %v", err)
	}

	if store.Count() != 2 {
		t.Errorf("expected 2 jobs after merge, got %d", store.Count())
	}

	// Import with replace=true
	if err := store.Import([]byte(importData), true); err != nil {
		t.Fatalf("Import (replace) failed: %v", err)
	}

	if store.Count() != 1 {
		t.Errorf("expected 1 job after replace, got %d", store.Count())
	}

	if _, ok := store.Get("existing"); ok {
		t.Error("existing job should have been replaced")
	}
}

func TestStoreAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Add a job
	store.Add(JobConfig{
		ID:       "atomic-test",
		Name:     "Atomic Test",
		Type:     JobTypeReminder,
		Schedule: "@hourly",
		ReminderConfig: &ReminderJobConfig{
			Message: "Test",
		},
	})

	// Check no temp file exists
	tempPath := store.FilePath() + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful save")
	}

	// Check main file exists
	if _, err := os.Stat(store.FilePath()); os.IsNotExist(err) {
		t.Error("jobs file should exist after save")
	}
}

func TestStoreConcurrency(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			jobID := string(rune('a' + n))
			store.Add(JobConfig{
				ID:       jobID,
				Name:     "Concurrent",
				Type:     JobTypeReminder,
				Schedule: "@hourly",
				ReminderConfig: &ReminderJobConfig{
					Message: "Test",
				},
			})
			store.Get(jobID)
			store.List()
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final state
	if store.Count() != 10 {
		t.Errorf("expected 10 jobs, got %d", store.Count())
	}
}
