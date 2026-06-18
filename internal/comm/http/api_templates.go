// Package http provides HTTP handlers for the meept daemon.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/caimlas/meept/internal/sharedclient"
)

// TemplatesService handles HTTP requests for template operations.
type TemplatesService struct{}

// TemplatesRequest represents a template invocation request.
type TemplatesRequest struct {
	Name      string   `json:"name"`
	Arguments []string `json:"arguments"`
}

// TemplatesResponse represents a template invocation response.
type TemplatesResponse struct {
	Content string `json:"content"`
}

// HandleInvoke handles POST /api/v1/templates/invoke
func (s *TemplatesService) HandleInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TemplatesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Get the template
	cmd, ok := sharedclient.GetCustomCommand(req.Name)
	if !ok {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	// Render with arguments
	rendered := sharedclient.RenderTemplate(cmd.Template, req.Arguments)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TemplatesResponse{Content: rendered})
}

// HandleList handles GET /api/v1/templates/list
func (s *TemplatesService) HandleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all custom commands
	commands := sharedclient.DiscoverCustomCommands()

	// Convert to list format
	type CommandSummary struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}

	summaries := make([]CommandSummary, 0, len(commands))
	for _, cmd := range commands {
		summaries = append(summaries, CommandSummary{
			Name:        cmd.Name,
			Description: cmd.Description,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]CommandSummary{"templates": summaries})
}
