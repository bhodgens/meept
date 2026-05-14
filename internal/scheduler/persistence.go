package scheduler

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store provides persistent storage for job configurations.
type Store struct {
	mu       sync.RWMutex
	filePath string
	jobs     map[string]JobConfig
}

// StoreData represents the persisted job data structure.
type StoreData struct {
	Version   int         `json:"version"`
	UpdatedAt time.Time   `json:"updated_at"`
	Jobs      []JobConfig `json:"jobs"`
}

const (
	currentStoreVersion = 1
	defaultFileName     = "jobs.json"
)

// NewStore creates a new Store with the given data directory.
// If dataDir is empty, it defaults to ~/.meept/
func NewStore(dataDir string) (*Store, error) {
	if dataDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dataDir = filepath.Join(homeDir, ".meept")
	}

	// Expand ~ in path
	if dataDir != "" && dataDir[0] == '~' {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to expand home directory: %w", err)
		}
		dataDir = filepath.Join(homeDir, dataDir[1:])
	}

	// Ensure directory exists
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	filePath := filepath.Join(dataDir, defaultFileName)

	store := &Store{
		filePath: filePath,
		jobs:     make(map[string]JobConfig),
	}

	return store, nil
}

// Load reads job configurations from the persistent store.
func (s *Store) Load() ([]JobConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if file exists
	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		// No jobs file yet, return empty list
		return nil, nil
	}

	file, err := os.Open(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open jobs file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read jobs file: %w", err)
	}

	// Handle empty file
	if len(data) == 0 {
		return nil, nil
	}

	var storeData StoreData
	if err := json.Unmarshal(data, &storeData); err != nil {
		return nil, fmt.Errorf("failed to parse jobs file: %w", err)
	}

	// Update internal map
	s.jobs = make(map[string]JobConfig, len(storeData.Jobs))
	for _, job := range storeData.Jobs {
		s.jobs[job.ID] = job
	}

	return storeData.Jobs, nil
}

// Save persists job configurations to the store.
func (s *Store) Save(jobs []JobConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveUnlocked(jobs)
}

// saveUnlocked saves jobs without acquiring the lock (caller must hold lock).
func (s *Store) saveUnlocked(jobs []JobConfig) error {
	storeData := StoreData{
		Version:   currentStoreVersion,
		UpdatedAt: time.Now().UTC(),
		Jobs:      jobs,
	}

	data, err := json.MarshalIndent(storeData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal jobs: %w", err)
	}

	// Write to temp file first (atomic write)
	tempFile := s.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Rename temp file to final path (atomic on POSIX)
	if err := os.Rename(tempFile, s.filePath); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Update internal map
	s.jobs = make(map[string]JobConfig, len(jobs))
	for _, job := range jobs {
		s.jobs[job.ID] = job
	}

	return nil
}

// Get retrieves a job configuration by ID.
func (s *Store) Get(id string) (JobConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[id]
	return job, ok
}

// Add adds or updates a job configuration and persists it.
func (s *Store) Add(job JobConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs[job.ID] = job
	return s.persistUnlocked()
}

// Remove deletes a job configuration and persists the change.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[id]; !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	delete(s.jobs, id)
	return s.persistUnlocked()
}

// Update updates a job configuration and persists it.
func (s *Store) Update(job JobConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.ID]; !ok {
		return fmt.Errorf("job not found: %s", job.ID)
	}

	s.jobs[job.ID] = job
	return s.persistUnlocked()
}

// List returns all job configurations.
func (s *Store) List() []JobConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]JobConfig, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// Count returns the number of stored jobs.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.jobs)
}

// persistUnlocked persists the current jobs to disk (caller must hold lock).
func (s *Store) persistUnlocked() error {
	jobs := make([]JobConfig, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return s.saveUnlocked(jobs)
}

// UpdateLastRun updates the last run time for a job.
func (s *Store) UpdateLastRun(id string, runTime time.Time, runErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	job.LastRunAt = &runTime
	job.RunCount++
	if runErr != nil {
		job.LastError = runErr.Error()
	} else {
		job.LastError = ""
	}

	s.jobs[id] = job
	return s.persistUnlocked()
}

// SetEnabled enables or disables a job.
func (s *Store) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}

	job.Enabled = enabled
	s.jobs[id] = job
	return s.persistUnlocked()
}

// FilePath returns the path to the jobs file.
func (s *Store) FilePath() string {
	return s.filePath
}

// Clear removes all jobs from the store.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = make(map[string]JobConfig)
	return s.persistUnlocked()
}

// Export exports all jobs to JSON.
func (s *Store) Export() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]JobConfig, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	storeData := StoreData{
		Version:   currentStoreVersion,
		UpdatedAt: time.Now().UTC(),
		Jobs:      jobs,
	}

	return json.MarshalIndent(storeData, "", "  ")
}

// Import imports jobs from JSON, optionally replacing existing jobs.
func (s *Store) Import(data []byte, replace bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var storeData StoreData
	if err := json.Unmarshal(data, &storeData); err != nil {
		return fmt.Errorf("failed to parse import data: %w", err)
	}

	if replace {
		s.jobs = make(map[string]JobConfig)
	}

	for _, job := range storeData.Jobs {
		s.jobs[job.ID] = job
	}

	return s.persistUnlocked()
}
