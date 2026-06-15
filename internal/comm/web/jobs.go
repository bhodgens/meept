package web

import (
	"net/http"
)

// handleJobsCreate handles POST /api/v1/jobs.
func (s *Server) handleJobsCreate(w http.ResponseWriter, r *http.Request) {
	if s.jobScheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "job scheduling not configured")
		return
	}

	var cfg map[string]any
	if err := readJSON(w, r, &cfg); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	jobID, err := s.jobScheduler.CreateJob(r.Context(), cfg)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create job: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]string{
		"id":     jobID,
		"status": "created",
	})
}

// handleJobsGet handles GET /api/v1/jobs/{id}.
func (s *Server) handleJobsGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	if s.jobScheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "job scheduling not configured")
		return
	}

	job, err := s.jobScheduler.GetJob(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "job not found: "+id)
		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// handleJobsCancel handles DELETE /api/v1/jobs/{id}.
func (s *Server) handleJobsCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	if s.jobScheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "job scheduling not configured")
		return
	}

	if err := s.jobScheduler.CancelJob(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, "job not found: "+id)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
