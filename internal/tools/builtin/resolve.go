package builtin

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ResolveTool allows accepting or rejecting pending file changes.
type ResolveTool struct {
	registry *PendingChangesRegistry
}

// NewResolveTool creates a new resolve tool.
func NewResolveTool(registry *PendingChangesRegistry) *ResolveTool {
	return &ResolveTool{registry: registry}
}

func (t *ResolveTool) Name() string { return "resolve" }

func (t *ResolveTool) Category() string { return "filesystem" }

func (t *ResolveTool) Description() string {
	return "Accept or reject pending file changes created by file_edit or other destructive operations. " +
		"Use this to review proposed changes before they are applied. Supports batch operations by " +
		"specifying multiple change IDs. Pending changes expire after the session ends or after a " +
		"configurable timeout."
}

func (t *ResolveTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"change_ids": {
				Type:        schemaTypeArray,
				Description: "List of pending change IDs to resolve. Use 'all' to resolve all pending changes in the session.",
				Items: &llm.ParameterProperty{
					Type:        schemaTypeString,
					Description: "A pending change ID or 'all'",
				},
			},
			"action": {
				Type:        schemaTypeString,
				Description: "Action to take: 'accept' to apply changes, 'reject' to discard them.",
				Enum:        []string{"accept", "reject"},
			},
			"session_id": {
				Type:        schemaTypeString,
				Description: "Session ID for filtering pending changes (used with 'all').",
			},
		},
		Required: []string{"change_ids", "action"},
	}
}

// ResolveResult represents the result of a resolve operation.
type ResolveResult struct {
	Accepted []string `json:"accepted,omitempty"` // Change IDs that were accepted
	Rejected []string `json:"rejected,omitempty"` // Change IDs that were rejected
	Failed   []string `json:"failed,omitempty"`   // Change IDs that failed to resolve
	Message  string   `json:"message"`
}

func (t *ResolveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	changeIDsRaw, _ := args["change_ids"].([]any)
	if len(changeIDsRaw) == 0 {
		return nil, fmt.Errorf("change_ids is required")
	}

	action, _ := args["action"].(string)
	if action != "accept" && action != "reject" {
		return nil, fmt.Errorf("action must be 'accept' or 'reject'")
	}

	sessionID, _ := args["session_id"].(string)

	var changeIDs []string
	for _, idRaw := range changeIDsRaw {
		idStr, ok := idRaw.(string)
		if !ok {
			return nil, fmt.Errorf("change_ids must be strings")
		}
		changeIDs = append(changeIDs, idStr)
	}

	result := ResolveResult{
		Accepted: make([]string, 0),
		Rejected: make([]string, 0),
		Failed:   make([]string, 0),
	}

	// Expand 'all' to all pending changes in the session
	var finalIDs []string
	for _, id := range changeIDs {
		if id == "all" {
			if sessionID == "" {
				result.Failed = append(result.Failed, "all")
				continue
			}
			changes := t.registry.GetBySession(sessionID)
			for _, c := range changes {
				finalIDs = append(finalIDs, c.ID)
			}
		} else {
			finalIDs = append(finalIDs, id)
		}
	}

	// Process each change
	for _, id := range finalIDs {
		change, ok := t.registry.Get(id)
		if !ok {
			result.Failed = append(result.Failed, id)
			continue
		}

		if action == "accept" {
			// Write the modified content to the file
			if err := os.WriteFile(change.FilePath, []byte(change.Modified), 0644); err != nil {
				result.Failed = append(result.Failed, id)
				continue
			}
			result.Accepted = append(result.Accepted, id)
		} else {
			// Reject: just remove from registry (original file is unchanged)
			result.Rejected = append(result.Rejected, id)
		}

		// Remove the change from the registry
		t.registry.Remove(id)
	}

	// Build message
	if len(result.Accepted) > 0 && len(result.Rejected) > 0 {
		result.Message = fmt.Sprintf("Accepted %d changes, rejected %d changes", len(result.Accepted), len(result.Rejected))
	} else if len(result.Accepted) > 0 {
		result.Message = fmt.Sprintf("Accepted %d changes", len(result.Accepted))
	} else if len(result.Rejected) > 0 {
		result.Message = fmt.Sprintf("Rejected %d changes", len(result.Rejected))
	} else {
		result.Message = "No changes resolved"
	}

	if len(result.Failed) > 0 {
		result.Message += fmt.Sprintf(", %d failed (not found)", len(result.Failed))
	}

	return result, nil
}

// SetDefaultExpiry sets a default expiration for pending changes.
func (t *ResolveTool) SetDefaultExpiry(duration time.Duration) {
	_ = duration // Future: store as default for registry
}

// Ensure ResolveTool implements the Tool interface.
var _ tools.Tool = (*ResolveTool)(nil)
