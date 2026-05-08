package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/caimlas/meept/internal/services"
)

// handleServiceError writes appropriate HTTP response based on service error type.
func (s *Server) handleServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, services.ErrNotFound) {
		s.writeError(w, http.StatusNotFound, err.Error())
	} else if errors.Is(err, services.ErrInvalidInput) {
		s.writeError(w, http.StatusBadRequest, err.Error())
	} else if errors.Is(err, services.ErrUnauthorized) {
		s.writeError(w, http.StatusUnauthorized, err.Error())
	} else {
		s.logger.Debug("service error", "error", err)
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// ===== Chat Endpoints =====

// handleChat handles POST /api/v1/chat.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req services.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := s.services.Chat.Chat(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// ===== Memory Endpoints =====

// handleMemoryQuery handles POST /api/v1/memory/query.
func (s *Server) handleMemoryQuery(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Memory == nil {
		s.writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	var req services.MemoryQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	results, err := s.services.Memory.Query(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"memories": results,
		"count":    len(results),
	})
}

// handleMemoryRecent handles GET /api/v1/memory/recent.
func (s *Server) handleMemoryRecent(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Memory == nil {
		s.writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := strconv.Atoi(l); err == nil {
			limit, _ = strconv.Atoi(l)
		}
	}

	results, err := s.services.Memory.Recent(r.Context(), limit)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"memories": results,
		"count":    len(results),
	})
}

// handleMemoryExport handles POST /api/v1/memory/export.
func (s *Server) handleMemoryExport(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Memory == nil {
		s.writeError(w, http.StatusServiceUnavailable, "memory service not available")
		return
	}

	var req struct {
		Format   string `json:"format"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Format == "" {
		req.Format = "json"
	}

	data, err := s.services.Memory.Export(r.Context(), req.Format, req.Category)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ===== Queue Endpoints =====

// handleQueueEnqueue handles POST /api/v1/queue/jobs.
func (s *Server) handleQueueEnqueue(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	var req services.EnqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	job, err := s.services.Queue.Enqueue(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, job)
}

// handleQueueList handles GET /api/v1/queue/jobs.
func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	state := r.URL.Query().Get("state")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := strconv.Atoi(l); err == nil {
			limit, _ = strconv.Atoi(l)
		}
	}

	jobs, err := s.services.Queue.ListByState(r.Context(), services.ListRequest{
		State: state,
		Limit: limit,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// handleQueueStats handles GET /api/v1/queue/stats.
func (s *Server) handleQueueStats(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	stats, err := s.services.Queue.Stats(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// ===== Task Endpoints =====

// handleTaskCreate handles POST /api/v1/tasks.
func (s *Server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	var req services.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	task, err := s.services.Task.Create(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, task)
}

// handleTaskList handles GET /api/v1/tasks.
func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := strconv.Atoi(l); err == nil {
			limit, _ = strconv.Atoi(l)
		}
	}

	tasks, err := s.services.Task.List(r.Context(), services.TaskListRequest{
		Limit: limit,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// handleTaskGet handles GET /api/v1/tasks/{id}.
func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	task, err := s.services.Task.Get(r.Context(), services.GetTaskRequest{ID: id})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, task)
}

// handleTaskUpdate handles PUT /api/v1/tasks/{id}.
func (s *Server) handleTaskUpdate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	var req services.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = id

	task, err := s.services.Task.Update(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, task)
}

// handleTaskDelete handles DELETE /api/v1/tasks/{id}.
func (s *Server) handleTaskDelete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	if err := s.services.Task.Delete(r.Context(), services.DeleteTaskRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ===== Session Endpoints =====

// handleSessionCreate handles POST /api/v1/sessions.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	var req services.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.services.Session.CreateSession(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, session)
}

// handleSessionList handles GET /api/v1/sessions.
func (s *Server) handleSessionList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := strconv.Atoi(l); err == nil {
			limit, _ = strconv.Atoi(l)
		}
	}

	sessions, err := s.services.Session.List(r.Context(), services.ListSessionsRequest{
		Limit: limit,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleSessionGet handles GET /api/v1/sessions/{id}.
func (s *Server) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	session, err := s.services.Session.GetSession(r.Context(), services.GetSessionRequest{ID: id})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleSessionDelete handles DELETE /api/v1/sessions/{id}.
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	if err := s.services.Session.DeleteSession(r.Context(), services.DeleteSessionRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ===== Worker Endpoints =====

// handleWorkerStats handles GET /api/v1/workers/stats.
func (s *Server) handleWorkerStats(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Worker == nil {
		s.writeError(w, http.StatusServiceUnavailable, "worker service not available")
		return
	}

	stats, err := s.services.Worker.Stats(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// ===== Skills Endpoints =====

// handleSkillsList handles GET /api/v1/skills.
func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Skills == nil {
		s.writeError(w, http.StatusServiceUnavailable, "skills service not available")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := strconv.Atoi(l); err == nil {
			limit, _ = strconv.Atoi(l)
		}
	}

	skills, err := s.services.Skills.List(r.Context(), services.SkillsListRequest{
		Limit: limit,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"skills": skills,
		"count":  len(skills),
	})
}

// handleSkillsGet handles GET /api/v1/skills/{slug}.
func (s *Server) handleSkillsGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Skills == nil {
		s.writeError(w, http.StatusServiceUnavailable, "skills service not available")
		return
	}

	slug := r.PathValue("slug")
	if slug == "" {
		s.writeError(w, http.StatusBadRequest, "skill slug is required")
		return
	}

	skill, err := s.services.Skills.Get(r.Context(), services.SkillsGetRequest{Slug: slug})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, skill)
}

// handleSkillsExecute handles POST /api/v1/skills/{slug}/execute.
func (s *Server) handleSkillsExecute(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Skills == nil {
		s.writeError(w, http.StatusServiceUnavailable, "skills service not available")
		return
	}

	slug := r.PathValue("slug")
	if slug == "" {
		s.writeError(w, http.StatusBadRequest, "skill slug is required")
		return
	}

	var req services.ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Slug = slug

	result, err := s.services.Skills.Execute(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// ===== Self-Improve Endpoints =====

// handleSelfImproveStatus handles GET /api/v1/selfimprove/status.
func (s *Server) handleSelfImproveStatus(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	status, err := s.services.SelfImprove.Status(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, status)
}

// handleSelfImproveTrigger handles POST /api/v1/selfimprove/trigger.
func (s *Server) handleSelfImproveTrigger(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	var req services.TriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.SelfImprove.Trigger(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "triggered"})
}

// ===== Cache Endpoints =====

// handleCacheStats handles GET /api/v1/cache/stats.
func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Cache == nil {
		s.writeError(w, http.StatusServiceUnavailable, "cache service not available")
		return
	}

	stats, err := s.services.Cache.Stats(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// handleCacheClear handles POST /api/v1/cache/clear.
func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Cache == nil {
		s.writeError(w, http.StatusServiceUnavailable, "cache service not available")
		return
	}

	var req services.ClearCacheRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Cache.Clear(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// ===== Security Endpoints =====

// handleSecurityCheck handles POST /api/v1/security/check.
func (s *Server) handleSecurityCheck(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Security == nil {
		s.writeError(w, http.StatusServiceUnavailable, "security service not available")
		return
	}

	var req services.CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := s.services.Security.Check(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// ===== Scheduler Endpoints =====

// handleSchedulerListJobs handles GET /api/v1/scheduler/jobs.
func (s *Server) handleSchedulerListJobs(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Scheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "scheduler service not available")
		return
	}

	jobs, err := s.services.Scheduler.ListJobs(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	})
}

// handleSchedulerAddJob handles POST /api/v1/scheduler/jobs.
func (s *Server) handleSchedulerAddJob(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Scheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "scheduler service not available")
		return
	}

	var req services.AddJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	job, err := s.services.Scheduler.AddJob(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, job)
}

// ===== Bus Endpoints =====

// handleBusPublish handles POST /api/v1/bus/publish.
func (s *Server) handleBusPublish(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Bus == nil {
		s.writeError(w, http.StatusServiceUnavailable, "bus service not available")
		return
	}

	var req services.PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Bus.Publish(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

// handleBusStats handles GET /api/v1/bus/stats.
func (s *Server) handleBusStats(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Bus == nil {
		s.writeError(w, http.StatusServiceUnavailable, "bus service not available")
		return
	}

	stats, err := s.services.Bus.Stats(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}
