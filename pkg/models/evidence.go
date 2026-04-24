// Package models provides shared data types for the Meept daemon.
package models

import (
	"time"
)

// EvidenceType represents the type of evidence.
type EvidenceType string

const (
	// EvidenceFileExists indicates a file exists at the given path.
	EvidenceFileExists EvidenceType = "file_exists"
	// EvidenceFileHash indicates a file's SHA256 hash.
	EvidenceFileHash EvidenceType = "file_hash"
	// EvidenceAPIResponse indicates an API response was received.
	EvidenceAPIResponse EvidenceType = "api_response"
	// EvidenceDatabaseRow indicates a database row was found/modified.
	EvidenceDatabaseRow EvidenceType = "db_row"
	// EvidenceProcessExit indicates a process exit code.
	EvidenceProcessExit EvidenceType = "process_exit"
	// EvidenceShellOutput indicates shell command output (hashed for compactness).
	EvidenceShellOutput EvidenceType = "shell_output"
)

// Evidence represents proof that a tool operation occurred successfully.
// Evidence is produced by tools and used to validate task completion.
type Evidence struct {
	// Type of evidence (file_exists, file_hash, process_exit, etc.)
	Type EvidenceType `json:"type"`
	// Subject is the path, URL, query, or command that was validated.
	Subject string `json:"subject"`
	// Value is the evidence value (hash, exit code, row JSON, output excerpt).
	Value string `json:"value"`
	// Timestamp when the evidence was collected.
	Timestamp time.Time `json:"timestamp"`
	// Source is the tool name that produced this evidence.
	Source string `json:"source"`
}

// NewEvidence creates a new Evidence with the current timestamp.
func NewEvidence(evidenceType EvidenceType, subject, value, source string) Evidence {
	return Evidence{
		Type:      evidenceType,
		Subject:   subject,
		Value:     value,
		Timestamp: time.Now().UTC(),
		Source:    source,
	}
}
