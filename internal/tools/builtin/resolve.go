package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// ResolveTool allows accepting or rejecting pending file changes.
type ResolveTool struct {
	tools.ToolDefaults
	registry      *PendingChangesRegistry
	fenceChecker  FenceChecker
	defaultExpiry time.Duration
	// journal, when non-nil, receives a JournalEntry for every successful
	// accept so `meept changes revert <id>` can restore the file later.
	journal *Journal
}

// NewResolveTool creates a new resolve tool.
func NewResolveTool(registry *PendingChangesRegistry) *ResolveTool {
	return &ResolveTool{registry: registry}
}

// SetFenceChecker installs a path fence so the accept branch can re-validate
// staged file paths at write time (defense in depth — the path was validated
// when the pending change was registered, but configuration may have changed
// since then or future code paths may bypass registration-time checks).
// Follows the typed-nil interface guard pattern mandated by CLAUDE.md.
func (t *ResolveTool) SetFenceChecker(fc FenceChecker) {
	if fc != nil {
		t.fenceChecker = fc
	}
}

// SetJournal installs the change journal that records every accepted change
// (pre-image + applied checksum) for later revert. Follows the same typed-nil
// guard pattern as SetFenceChecker: passing nil is a no-op, keeping resolve
// usable in journal-less deployments (tests, fallback registries).
func (t *ResolveTool) SetJournal(j *Journal) {
	if j != nil {
		t.journal = j
	}
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

// verifyPreImage checks the staged change against the current on-disk state
// of change.FilePath. It returns (drifted=false, nil) when it is safe to apply
// the staged Modified content, and (drifted=true, nil) when applying would
// clobber changes made after staging. A non-nil error means verification
// itself failed (unreadable file) and the caller must not write.
//
// Decision table:
//
//	current file hash == PreImageSHA256      -> clean, safe to apply
//	current file hash == sha256(Modified)    -> already applied; idempotent no-op
//	PreImageSHA256 empty (legacy change)     -> cannot verify; proceed with warning
//	otherwise                                -> drift; refuse
func (t *ResolveTool) verifyPreImage(change *PendingChange) (bool, error) {
	data, err := os.ReadFile(change.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File vanished after staging. Applying recreates it with the
			// staged content — only meaningful if the pre-image was also
			// empty (a create). Otherwise treat as drift.
			if change.PreImageSHA256 == "" || change.PreImageSHA256 == sha256Hex("") {
				return false, nil
			}
			return true, fmt.Errorf("staged file no longer exists on disk")
		}
		return false, err
	}

	currentHash := sha256Hex(string(data))

	switch {
	case change.PreImageSHA256 == "":
		slog.Warn("ResolveTool: pending change has no pre-image hash (legacy); proceeding without drift check",
			"change_id", change.ID, "path", change.FilePath)
		return false, nil

	case currentHash == change.PreImageSHA256:
		// Pre-image intact — clean apply.
		return false, nil

	case currentHash == sha256Hex(change.Modified):
		// The staged content is already on disk (e.g. duplicate accept).
		// Idempotent success; skip the rewrite.
		slog.Info("ResolveTool: staged content already applied; skipping redundant write",
			"change_id", change.ID, "path", change.FilePath)
		return false, nil

	default:
		// Drift: the file changed between staging and accept.
		return true, nil
	}
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
	refusalNotes := make([]string, 0)
	for _, id := range finalIDs {
		change, ok := t.registry.Get(id)
		if !ok {
			result.Failed = append(result.Failed, id)
			continue
		}

		if action == "accept" {
			// Re-validate the staged file path against the fence at write time.
			// The path was checked at registration, but configuration may have
			// changed or future code paths may bypass that check.
			if t.fenceChecker != nil {
				if err := t.fenceChecker.CheckPath(change.FilePath, "write"); err != nil {
					result.Failed = append(result.Failed, id)
					continue
				}
			}
			// Pre-image integrity check: refuse to apply when the on-disk file
			// drifted after the change was staged. See verifyPreImage for the
			// decision table (clean / already-applied / legacy / drift).
			drifted, err := t.verifyPreImage(change)
			if err != nil {
				slog.Warn("ResolveTool: pre-image verification failed", "change_id", change.ID, "error", err)
				result.Failed = append(result.Failed, id)
				continue
			}
			if drifted {
				stagedShort := shortHash(change.PreImageSHA256)
				currentShort := ""
				if data, readErr := os.ReadFile(change.FilePath); readErr == nil {
					currentShort = shortHash(sha256Hex(string(data)))
				}
				refusal := fmt.Sprintf(
					"file changed since staging (staged pre-image %s… != current file %s…) — refusing to overwrite; re-stage against current content",
					stagedShort, currentShort,
				)
				slog.Warn("ResolveTool: "+refusal, "change_id", change.ID, "path", change.FilePath)
				refusalNotes = append(refusalNotes, fmt.Sprintf("%s: %s", id, refusal))
				result.Failed = append(result.Failed, id)
				continue
			}
			// Write the modified content to the file
			if err := os.WriteFile(change.FilePath, []byte(change.Modified), 0644); err != nil {
				result.Failed = append(result.Failed, id)
				continue
			}
			// Journal the applied change so it can be reverted later. The
			// pre-image is the Original the registry already holds; PostSHA
			// is computed from what was actually written (this also covers
			// legacy staged changes with empty PreImageSHA256). Journaling is
			// best-effort: a failed record must not fail an accepted write.
			if t.journal != nil {
				if err := t.journal.Record(&JournalEntry{
					SessionID: change.SessionID,
					FilePath:  change.FilePath,
					PreImage:  []byte(change.Original),
					PostSHA:   sha256Hex(change.Modified),
					ChangeIDs: []string{change.ID},
				}); err != nil {
					slog.Warn("ResolveTool: failed to journal accepted change",
						"change_id", change.ID, "path", change.FilePath, "error", err)
				}
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

	if len(refusalNotes) > 0 {
		result.Message += "; " + strings.Join(refusalNotes, "; ")
	}

	return result, nil
}

// SetDefaultExpiry sets a default expiration for pending changes.
// TODO: apply this duration automatically when new changes are registered.
func (t *ResolveTool) SetDefaultExpiry(duration time.Duration) {
	t.defaultExpiry = duration
}

// Ensure ResolveTool implements the Tool interface.
var _ tools.Tool = (*ResolveTool)(nil)
