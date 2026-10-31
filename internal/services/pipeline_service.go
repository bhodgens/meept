package services

import (

	"time"

)


// Pipeline represents a processing pipeline.
type Pipeline struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Steps       []PipelineStep    `json:"steps"`
	Metadata    map[string]string `json:"metadata"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// PipelineStep represents a single step in a pipeline.
type PipelineStep struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// PipelineService handles pipeline operations.
type PipelineService struct {
	pipelines map[string]*Pipeline
}

// NewPipelineService creates a pipeline service.
func NewPipelineService() *PipelineService {
	return &PipelineService{
		pipelines: make(map[string]*Pipeline),
	}
}

// StatusRequest contains status parameters.
type StatusRequest struct {
	PipelineID string `json:"pipeline_id"`
}

// PipelineStatusResponse contains pipeline status.
type PipelineStatusResponse struct {
	PipelineID string               `json:"pipeline_id"`
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	Steps      []PipelineStepStatus `json:"steps"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

// PipelineStepStatus contains step status information.
type PipelineStepStatus struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}


// PipelineListRequest contains list parameters.
type PipelineListRequest struct {
	Limit int `json:"limit,omitempty"`
}

// PipelineInfo contains pipeline summary information.
type PipelineInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}


// CreateRequest contains create parameters.
type CreatePipelineRequest struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Steps       []PipelineStep    `json:"steps,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}


// DeleteRequest contains delete parameters.
type DeletePipelineRequest struct {
	PipelineID string `json:"pipeline_id"`
}


// UpdateStatusRequest contains update status parameters.
type UpdateStatusRequest struct {
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"`
}


