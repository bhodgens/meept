package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/pkg/models"
)

// Response payload key constants.
const (
	KeyStatus = "status"
	KeyCount  = "count"
	KeySaved  = "saved"
	KeyQueued = "queued"
)

// handleServiceError writes appropriate HTTP response based on service error type.
func (s *Server) handleServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrInvalidInput):
		s.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrUnauthorized):
		s.writeError(w, http.StatusUnauthorized, err.Error())
	default:
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

// handleChatStream handles GET /api/v1/chat/stream.
// It provides an SSE endpoint for real-time tool progress and agent events.
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Bus == nil {
		s.writeError(w, http.StatusServiceUnavailable, "bus service not available")
		return
	}

	sse, err := NewSSEWriter(w)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe to tool progress and agent events
	subID := fmt.Sprintf("sse-chat-%d", time.Now().UnixNano())
	sub, unsub := s.services.Bus.Subscribe(subID, "tool.execution.progress")
	if sub == nil {
		_ = sse.SendError("bus subscription failed")
		return
	}
	defer unsub()

	// Also subscribe to agent progress events
	agentSub, agentUnsub := s.services.Bus.Subscribe(subID+"-agent", "agent.progress")
	if agentSub != nil {
		defer agentUnsub()
	}

	// Subscribe to tool completion events
	completeSub, completeUnsub := s.services.Bus.Subscribe(subID+"-complete", "tool.execution.complete")
	if completeSub != nil {
		defer completeUnsub()
	}

	// Extract channels for select, guarding against nil subscriptions
	var agentCh, completeCh <-chan *models.BusMessage
	if agentSub != nil {
		agentCh = agentSub.Channel
	}
	if completeSub != nil {
		completeCh = completeSub.Channel
	}

	// Send initial connection event
	if err := sse.SendEvent("connected", map[string]string{KeyStatus: "ok"}); err != nil {
		return
	}

	// Heartbeat ticker
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Event loop: forward bus events as SSE, send heartbeats, detect disconnect
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return

		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			// Forward tool progress as SSE
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}
			if err := sse.SendEvent("tool_progress", payload); err != nil {
				return // Client disconnected
			}

		case msg, ok := <-agentCh:
			if !ok {
				return
			}
			// Forward agent progress as SSE
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}
			if err := sse.SendEvent("agent_progress", payload); err != nil {
				return
			}

		case msg, ok := <-completeCh:
			if !ok {
				return
			}
			// Forward tool completion as SSE
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}
			if err := sse.SendEvent("tool_complete", payload); err != nil {
				return
			}

		case <-heartbeat.C:
			if err := sse.SendComment(); err != nil {
				return // Client disconnected
			}
		}
	}
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
		KeyCount:    len(results),
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
		KeyCount:    len(results),
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
	_, _ = w.Write(data)
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
		KeyCount: len(jobs),
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
		KeyCount: len(tasks),
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "deleted"})
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
		KeyCount:    len(sessions),
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "deleted"})
}

// handleSessionMessages handles GET /api/v1/sessions/{id}/messages.
func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if _, err := strconv.Atoi(o); err == nil {
			offset, _ = strconv.Atoi(o)
		}
	}

	limit := 1000
	if l := r.URL.Query().Get("limit"); l != "" {
		if _, err := strconv.Atoi(l); err == nil {
			limit, _ = strconv.Atoi(l)
		}
	}

	messages, err := s.services.Session.GetMessages(r.Context(), services.GetMessagesRequest{
		ID:     id,
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"messages": messages,
		"total":    len(messages),
	})
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
		KeyCount:  len(skills),
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "triggered"})
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "cleared"})
}

// handleCacheInvalidate handles POST /api/v1/cache/invalidate.
func (s *Server) handleCacheInvalidate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Cache == nil {
		s.writeError(w, http.StatusServiceUnavailable, "cache service not available")
		return
	}

	var req services.InvalidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Cache.Invalidate(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "invalidated"})
}

// handleCacheInspect handles GET /api/v1/cache/inspect.
func (s *Server) handleCacheInspect(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Cache == nil {
		s.writeError(w, http.StatusServiceUnavailable, "cache service not available")
		return
	}

	hash := r.URL.Query().Get("hash")
	if hash == "" {
		s.writeError(w, http.StatusBadRequest, "missing hash query parameter")
		return
	}

	results, err := s.services.Cache.Inspect(r.Context(), hash)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, results)
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
		KeyCount: len(jobs),
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

// handleSchedulerRemoveJob handles DELETE /api/v1/scheduler/jobs/{id}.
func (s *Server) handleSchedulerRemoveJob(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Scheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "scheduler service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	if err := s.services.Scheduler.RemoveJob(r.Context(), services.RemoveJobRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "removed"})
}

// handleSchedulerEnableJob handles POST /api/v1/scheduler/jobs/{id}/enable.
func (s *Server) handleSchedulerEnableJob(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Scheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "scheduler service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Scheduler.EnableJob(r.Context(), services.EnableJobRequest{
		ID:      id,
		Enabled: req.Enabled,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "updated"})
}

// handleSchedulerPauseJob handles POST /api/v1/scheduler/jobs/{id}/pause.
func (s *Server) handleSchedulerPauseJob(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Scheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "scheduler service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	if err := s.services.Scheduler.PauseJob(r.Context(), services.PauseJobRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "paused"})
}

// handleSchedulerResumeJob handles POST /api/v1/scheduler/jobs/{id}/resume.
func (s *Server) handleSchedulerResumeJob(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Scheduler == nil {
		s.writeError(w, http.StatusServiceUnavailable, "scheduler service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	if err := s.services.Scheduler.ResumeJob(r.Context(), services.ResumeJobRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "resumed"})
}


// ===== Model Endpoints =====

// handleModelsList handles GET /api/v1/models.
func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	models, err := s.services.Model.List(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
		"count":  len(models),
	})
}

// handleModelsProviders handles GET /api/v1/models/providers.
func (s *Server) handleModelsProviders(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	providers, err := s.services.Model.Providers(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
		"count":     len(providers),
	})
}

// handleModelsGetDefault handles GET /api/v1/models/default.
func (s *Server) handleModelsGetDefault(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	model, err := s.services.Model.GetDefault(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, model)
}

// handleModelsSetDefault handles POST /api/v1/models/default.
func (s *Server) handleModelsSetDefault(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Model.SetDefault(r.Context(), req.Provider, req.Model); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleModelsRemove handles DELETE /api/v1/models/{provider}/{model}.
func (s *Server) handleModelsRemove(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	provider := r.PathValue("provider")
	model := r.PathValue("model")

	if provider == "" || model == "" {
		s.writeError(w, http.StatusBadRequest, "provider and model are required")
		return
	}

	if err := s.services.Model.Remove(r.Context(), provider, model); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleModelsGetCredential handles GET /api/v1/models/credentials/{provider}.
func (s *Server) handleModelsGetCredential(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	provider := r.PathValue("provider")
	if provider == "" {
		s.writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	cred, err := s.services.Model.GetCredential(r.Context(), provider)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{
		"provider": provider,
		"credential": cred,
	})
}

// handleModelsSetCredential handles POST /api/v1/models/credentials/{provider}.
func (s *Server) handleModelsSetCredential(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	provider := r.PathValue("provider")
	if provider == "" {
		s.writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Model.SetCredential(r.Context(), provider, req.APIKey); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleModelsDeleteCredential handles DELETE /api/v1/models/credentials/{provider}.
func (s *Server) handleModelsDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Model == nil {
		s.writeError(w, http.StatusServiceUnavailable, "model service not available")
		return
	}

	provider := r.PathValue("provider")
	if provider == "" {
		s.writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	if err := s.services.Model.DeleteCredential(r.Context(), provider); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "published"})
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

// handleFirewallStats handles GET /api/v1/metrics/firewall.
func (s *Server) handleFirewallStats(w http.ResponseWriter, r *http.Request) {
	if s.FirewallStatsGetter == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	stats := s.FirewallStatsGetter()
	if stats == nil {
		stats = map[string]any{}
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// ===== Additional Queue Endpoints =====
// ===== Additional Queue Endpoints =====

// handleQueueGet handles GET /api/v1/queue/jobs/{id}.
func (s *Server) handleQueueGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	job, err := s.services.Queue.Get(r.Context(), services.GetRequest{JobID: id})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// handleQueueClaim handles POST /api/v1/queue/claim.
// Claims the next available job for a worker.
func (s *Server) handleQueueClaim(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	var req services.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	job, err := s.services.Queue.Claim(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, job)
}

// handleQueueComplete handles POST /api/v1/queue/jobs/{id}/complete.
func (s *Server) handleQueueComplete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	var req struct {
		Result any `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Queue.Complete(r.Context(), services.CompleteRequest{
		JobID:  id,
		Result: req.Result,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "completed"})
}

// handleQueueFail handles POST /api/v1/queue/jobs/{id}/fail.
func (s *Server) handleQueueFail(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	var req struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Queue.Fail(r.Context(), services.FailRequest{
		JobID: id,
		Error: req.Error,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "failed"})
}

// handleQueueRetry handles POST /api/v1/queue/jobs/{id}/retry.
func (s *Server) handleQueueRetry(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Queue == nil {
		s.writeError(w, http.StatusServiceUnavailable, "queue service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "job id is required")
		return
	}

	if err := s.services.Queue.Retry(r.Context(), services.RetryRequest{JobID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "retried"})
}

// ===== Additional Task Endpoints =====

// ===== Additional Task Endpoints =====

// handleTaskCancel handles POST /api/v1/tasks/{id}/cancel.
func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	if err := s.services.Task.Cancel(r.Context(), services.CancelTaskRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "cancelled"})
}

// handleTaskSteps handles GET /api/v1/tasks/{id}/steps.
func (s *Server) handleTaskSteps(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Task == nil {
		s.writeError(w, http.StatusServiceUnavailable, "task service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "task id is required")
		return
	}

	steps, err := s.services.Task.GetSteps(r.Context(), services.GetTaskStepsRequest{ID: id})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"steps": steps,
		KeyCount: len(steps),
	})
}

// ===== Additional Session Endpoints =====

// handleSessionAttach handles POST /api/v1/sessions/{id}/attach.
func (s *Server) handleSessionAttach(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.services.Session.Attach(r.Context(), services.AttachSessionRequest{
		ID:      id,
		AgentID: req.AgentID,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleSessionDetach handles POST /api/v1/sessions/{id}/detach.
func (s *Server) handleSessionDetach(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.services.Session.Detach(r.Context(), services.DetachSessionRequest{
		ID:      id,
		AgentID: req.AgentID,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleSessionResume handles POST /api/v1/sessions/{id}/resume.
// Restores a session into active memory.
func (s *Server) handleSessionResume(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	session, err := s.services.Session.ResumeSession(r.Context(), services.ResumeSessionRequest{
		ID: id,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleSessionBranch handles POST /api/v1/sessions/{id}/branch.
// Navigates to a branch point in the session tree.
func (s *Server) handleSessionBranch(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	var req struct {
		TargetMessageID int64 `json:"target_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, err := s.services.Session.BranchSession(r.Context(), services.BranchSessionRequest{
		ID:              id,
		TargetMessageID: req.TargetMessageID,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, session)
}

// handleSessionBranches handles GET /api/v1/sessions/{id}/branches.
// Lists all branches for a session.
func (s *Server) handleSessionBranches(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	branches, err := s.services.Session.ListBranches(r.Context(), services.ListBranchesRequest{
		ID: id,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"branches": branches,
		KeyCount:    len(branches),
	})
}

// handleSessionFork handles POST /api/v1/sessions/{id}/fork.
// Forks a session from a specific message.
func (s *Server) handleSessionFork(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	var req struct {
		FromMessageID int64  `json:"from_message_id"`
		Name          string `json:"name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	newSession, err := s.services.Session.ForkSession(r.Context(), services.ForkSessionRequest{
		SessionID:     id,
		FromMessageID: req.FromMessageID,
		Name:          req.Name,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, newSession)
}

// handleSessionTree handles GET /api/v1/sessions/{id}/tree.
// Returns the tree structure for a session.
func (s *Server) handleSessionTree(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	nodes, err := s.services.Session.GetTree(r.Context(), services.GetTreeRequest{
		ID: id,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"nodes": nodes,
		KeyCount: len(nodes),
	})
}

// handleSessionCompact handles POST /api/v1/sessions/{id}/compact.
// Triggers compaction on a session.
func (s *Server) handleSessionCompact(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Session == nil {
		s.writeError(w, http.StatusServiceUnavailable, "session service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "session id is required")
		return
	}

	result, err := s.services.Session.CompactSession(r.Context(), services.CompactSessionRequest{
		ID: id,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// ===== Additional Worker Endpoints =====

// handleWorkerAdd handles POST /api/v1/workers.
func (s *Server) handleWorkerAdd(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Worker == nil {
		s.writeError(w, http.StatusServiceUnavailable, "worker service not available")
		return
	}

	var req struct {
		ID           string   `json:"id"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	worker, err := s.services.Worker.Add(r.Context(), services.AddWorkerRequest{
		ID:           req.ID,
		Capabilities: req.Capabilities,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, worker)
}

// handleWorkerRemove handles DELETE /api/v1/workers/{id}.
func (s *Server) handleWorkerRemove(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Worker == nil {
		s.writeError(w, http.StatusServiceUnavailable, "worker service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "worker id is required")
		return
	}

	if err := s.services.Worker.Remove(r.Context(), services.RemoveWorkerRequest{ID: id}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "removed"})
}

// handleWorkerScale handles POST /api/v1/workers/scale.
func (s *Server) handleWorkerScale(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Worker == nil {
		s.writeError(w, http.StatusServiceUnavailable, "worker service not available")
		return
	}

	var req struct {
		DesiredCount int `json:"desired_count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Worker.Scale(r.Context(), services.ScaleWorkersRequest{
		DesiredCount: req.DesiredCount,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "scaled"})
}

// ===== Additional Self-Improve Endpoints =====

// handleSelfImproveAnalyze handles POST /api/v1/selfimprove/analyze.
func (s *Server) handleSelfImproveAnalyze(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	if err := s.services.SelfImprove.Analyze(r.Context()); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "analyzed"})
}

// handleSelfImproveGenerate handles POST /api/v1/selfimprove/generate.
func (s *Server) handleSelfImproveGenerate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	var req struct {
		ImprovementID string `json:"improvement_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.SelfImprove.Generate(r.Context(), services.GenerateImprovementRequest{
		ImprovementID: req.ImprovementID,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "generated"})
}

// handleSelfImproveValidate handles POST /api/v1/selfimprove/validate.
func (s *Server) handleSelfImproveValidate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	var req struct {
		ImprovementID string `json:"improvement_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := s.services.SelfImprove.Validate(r.Context(), services.ValidateImprovementRequest{
		ImprovementID: req.ImprovementID,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleSelfImproveApply handles POST /api/v1/selfimprove/apply.
func (s *Server) handleSelfImproveApply(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	var req struct {
		ImprovementID string `json:"improvement_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.SelfImprove.Apply(r.Context(), services.ApplyImprovementRequest{
		ImprovementID: req.ImprovementID,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "applied"})
}

// handleSelfImproveReject handles POST /api/v1/selfimprove/reject.
func (s *Server) handleSelfImproveReject(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.SelfImprove == nil {
		s.writeError(w, http.StatusServiceUnavailable, "self-improve service not available")
		return
	}

	var req struct {
		ImprovementID string `json:"improvement_id"`
		Reason        string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.SelfImprove.Reject(r.Context(), services.RejectImprovementRequest{
		ImprovementID: req.ImprovementID,
		Reason:        req.Reason,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "rejected"})
}

// ===== Chat Steering Endpoints =====

// handleChatSteer handles POST /api/v1/chat/steer.
func (s *Server) handleChatSteer(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req services.SteerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Chat.Steer(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeyQueued})
}

// handleChatSteerExplicit handles POST /api/v1/chat/steer-explicit.
// This is the ctrl+s equivalent -- forces steering regardless of intent classification.
func (s *Server) handleChatSteerExplicit(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req services.SteerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Chat.Steer(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{
		KeyStatus: KeyQueued,
		"mode":   "explicit",
	})
}

// handleChatFollowUp handles POST /api/v1/chat/followup.
func (s *Server) handleChatFollowUp(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req services.FollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Chat.FollowUp(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeyQueued})
}

// handleChatQueueStatus handles GET /api/v1/chat/queue/{id}.
func (s *Server) handleChatQueueStatus(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	// Only allow GET
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	conversationID := r.PathValue("id")
	if conversationID == "" {
		// Fallback for routers that don't support PathValue
		conversationID = strings.TrimPrefix(r.URL.Path, "/api/v1/chat/queue/")
	}
	if conversationID == "" {
		s.writeError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	status, err := s.services.Chat.GetQueueStatus(r.Context(), services.QueueStatusRequest{
		ConversationID: conversationID,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, status)
}

// ===== Queue Routing Endpoints =====

// handleQueueSteerRoute handles POST /api/v1/queue/steer.
// This is a convenience alias that routes steering messages through the standard queue API.
func (s *Server) handleQueueSteerRoute(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req services.SteerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Chat.Steer(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeyQueued})
}

// handleQueueFollowUpRoute handles POST /api/v1/queue/followup.
// This is a convenience alias that routes follow-up messages through the standard queue API.
func (s *Server) handleQueueFollowUpRoute(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req services.FollowUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Chat.FollowUp(r.Context(), req); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeyQueued})
}

// handleQueueStatusRoute handles GET /api/v1/queue/status/{id}.
// This is a convenience alias that returns the queue status for a conversation.
func (s *Server) handleQueueStatusRoute(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	conversationID := r.PathValue("id")
	if conversationID == "" {
		// Fallback for routers that don't support PathValue
		conversationID = strings.TrimPrefix(r.URL.Path, "/api/v1/queue/status/")
	}
	if conversationID == "" {
		s.writeError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}

	status, err := s.services.Chat.GetQueueStatus(r.Context(), services.QueueStatusRequest{
		ConversationID: conversationID,
	})
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, status)
}

// handleChatWithAgent handles POST /api/v1/chat/with-agent.
// Routes a steering message to a specific agent (e.g., coder, debugger, planner).
func (s *Server) handleChatWithAgent(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Chat == nil {
		s.writeError(w, http.StatusServiceUnavailable, "chat service not available")
		return
	}

	var req struct {
		Message        string `json:"message"`
		ConversationID string `json:"conversation_id"`
		Source         string `json:"source,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.services.Chat.Steer(r.Context(), services.SteerRequest{
		Message:        req.Message,
		ConversationID: req.ConversationID,
		Source:         req.Source,
	}); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: KeyQueued})
}

// ===== Calendar Endpoints =====

// handleCalendarList handles GET /api/v1/calendar/events.
func (s *Server) handleCalendarList(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	timeMin := r.URL.Query().Get("time_min")
	timeMax := r.URL.Query().Get("time_max")
	maxResults := 50
	if mr := r.URL.Query().Get("max_results"); mr != "" {
		if n, err := strconv.Atoi(mr); err == nil && n > 0 {
			maxResults = n
		}
	}

	var tMin, tMax time.Time
	if timeMin != "" {
		var err error
		tMin, err = time.Parse(time.RFC3339, timeMin)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid time_min format")
			return
		}
	}
	if timeMax != "" {
		var err error
		tMax, err = time.Parse(time.RFC3339, timeMax)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid time_max format")
			return
		}
	}

	req := services.ListEventsRequest{
		TimeMin:    tMin,
		TimeMax:    tMax,
		MaxResults: maxResults,
	}

	resp, err := s.services.Calendar.ListEvents(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleCalendarGet handles GET /api/v1/calendar/events/{id}.
func (s *Server) handleCalendarGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	eventID := r.PathValue("id")
	if eventID == "" {
		s.writeError(w, http.StatusBadRequest, "event id required")
		return
	}

	event, err := s.services.Calendar.GetEvent(r.Context(), eventID)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, event)
}

// handleCalendarCreate handles POST /api/v1/calendar/events.
func (s *Server) handleCalendarCreate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	var req services.CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	event, err := s.services.Calendar.CreateEvent(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, event)
}

// handleCalendarUpdate handles PUT /api/v1/calendar/events/{id}.
func (s *Server) handleCalendarUpdate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	eventID := r.PathValue("id")
	if eventID == "" {
		s.writeError(w, http.StatusBadRequest, "event id required")
		return
	}

	var req services.UpdateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ID = eventID

	event, err := s.services.Calendar.UpdateEvent(r.Context(), req)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, event)
}

// handleCalendarDelete handles DELETE /api/v1/calendar/events/{id}.
func (s *Server) handleCalendarDelete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	eventID := r.PathValue("id")
	if eventID == "" {
		s.writeError(w, http.StatusBadRequest, "event id required")
		return
	}

	if err := s.services.Calendar.DeleteEvent(r.Context(), eventID); err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleCalendarToday handles GET /api/v1/calendar/today.
func (s *Server) handleCalendarToday(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	resp, err := s.services.Calendar.GetToday(r.Context())
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleCalendarUpcoming handles GET /api/v1/calendar/upcoming.
func (s *Server) handleCalendarUpcoming(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	duration := 24 * time.Hour
	if d := r.URL.Query().Get("duration"); d != "" {
		if parsed, err := time.ParseDuration(d); err == nil {
			duration = parsed
		}
	}

	maxResults := 10
	if mr := r.URL.Query().Get("max_results"); mr != "" {
		if n, err := strconv.Atoi(mr); err == nil && n > 0 {
			maxResults = n
		}
	}

	resp, err := s.services.Calendar.GetUpcoming(r.Context(), duration, maxResults)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleCalendarQuickAdd handles POST /api/v1/calendar/quickadd.
func (s *Server) handleCalendarQuickAdd(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Calendar == nil {
		s.writeError(w, http.StatusServiceUnavailable, "calendar service not available")
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Text == "" {
		s.writeError(w, http.StatusBadRequest, "text required")
		return
	}

	event, err := s.services.Calendar.QuickAdd(r.Context(), req.Text)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, event)
}

// ===== Terminal Endpoints =====

// handleTerminalHistory handles GET /api/v1/terminal/history.
func (s *Server) handleTerminalHistory(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Terminal == nil {
		s.writeError(w, http.StatusServiceUnavailable, "terminal service not available")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	history := s.services.Terminal.GetHistory(limit)

	s.writeJSON(w, http.StatusOK, map[string]any{
		"history": history,
		"count":   len(history),
	})
}

// handleTerminalExec handles POST /api/v1/terminal/exec.
func (s *Server) handleTerminalExec(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Terminal == nil {
		s.writeError(w, http.StatusServiceUnavailable, "terminal service not available")
		return
	}

	var req struct {
		Command    string `json:"command"`
		WorkingDir string `json:"working_dir,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Command) == "" {
		s.writeError(w, http.StatusBadRequest, "command required")
		return
	}

	result, err := s.services.Terminal.ExecuteCommand(r.Context(), req.Command, req.WorkingDir)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, result)
}

// handleTerminalSessions handles GET /api/v1/terminal/sessions.
func (s *Server) handleTerminalSessions(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Terminal == nil {
		s.writeError(w, http.StatusServiceUnavailable, "terminal service not available")
		return
	}

	sessions := s.services.Terminal.ListSessions()

	s.writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleTerminalClear handles POST /api/v1/terminal/clear.
func (s *Server) handleTerminalClear(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Terminal == nil {
		s.writeError(w, http.StatusServiceUnavailable, "terminal service not available")
		return
	}

	s.services.Terminal.ClearHistory()

	s.writeJSON(w, http.StatusOK, map[string]string{KeyStatus: "cleared"})
}
