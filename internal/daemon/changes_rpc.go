package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// registerChangesRPCHandlers registers the changes.* RPC methods that give
// the TUI (and other socket clients) the same change-review surface as the
// HTTP endpoints: list/accept/reject staged pending changes and list/revert
// journaled applied changes (containment leaf 07).
//
// The handlers live here (not internal/rpc) because internal/rpc cannot
// import internal/tools/builtin without an import cycle; internal/daemon
// already imports both. Accept reuses ResolveTool.AcceptChange — the exact
// shared accept path the HTTP handlers call — so RPC, HTTP, and tool-driven
// accepts can never diverge. Errors carry the builtin sentinel text so
// clients can detect drift ("file changed since staging") vs not-found.
//
// Registered AFTER the bus proxy so these override any proxied versions.
func registerChangesRPCHandlers(
	server *rpc.Server,
	registry *builtin.PendingChangesRegistry,
	resolveTool *builtin.ResolveTool,
	journal *builtin.Journal,
) {
	if server == nil || registry == nil {
		return
	}

	// changes.list — pending changes for one session (diff preview only,
	// never full file contents).
	server.RegisterHandler("changes.list", func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		if req.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}

		changes := registry.GetBySession(req.SessionID)
		items := make([]map[string]any, 0, len(changes))
		for _, c := range changes {
			item := map[string]any{
				"id":         c.ID,
				"file_path":  c.FilePath,
				"diff":       c.Diff,
				"created_at": c.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
			}
			if c.ExpiresAt != nil {
				item["expires_at"] = c.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000Z07:00")
			}
			items = append(items, item)
		}
		return map[string]any{"changes": items, rpc.RPCKeyCount: len(items)}, nil
	})

	// changes.accept — apply a staged change through the shared accept
	// path (fence re-validation, drift check, write, journal record).
	if resolveTool != nil {
		server.RegisterHandler("changes.accept", func(_ context.Context, params json.RawMessage) (any, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
			if req.ID == "" {
				return nil, fmt.Errorf("id is required")
			}
			if _, err := resolveTool.AcceptChange(req.ID); err != nil {
				// Propagate sentinel text (drift / not found) so clients
				// can distinguish conflict from failure.
				return nil, err
			}
			return map[string]any{"status": "applied"}, nil
		})
	}

	// changes.reject — discard a staged change; the file stays untouched.
	server.RegisterHandler("changes.reject", func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		if _, ok := registry.Get(req.ID); !ok {
			return nil, fmt.Errorf("%w: %s", builtin.ErrChangeNotFound, req.ID)
		}
		registry.Remove(req.ID)
		return map[string]any{"status": "rejected"}, nil
	})

	if journal == nil {
		return
	}

	// changes.journal — applied changes, newest first. Pre-image bytes stay
	// daemon-side; only the byte count travels.
	server.RegisterHandler("changes.journal", func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		entries, err := journal.List(req.SessionID, req.Limit)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			changeIDs := e.ChangeIDs
			if changeIDs == nil {
				changeIDs = []string{}
			}
			size, sizeErr := journal.PreImageSize(e.ID)
			if sizeErr != nil {
				size = 0
			}
			items = append(items, map[string]any{
				"id":             e.ID,
				"session_id":     e.SessionID,
				"file_path":      e.FilePath,
				"post_sha":       e.PostSHA,
				"applied_at":     e.AppliedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
				"change_ids":     changeIDs,
				"pre_image_size": size,
			})
		}
		return map[string]any{"entries": items, rpc.RPCKeyCount: len(items)}, nil
	})

	// changes.revert — restore a file to its pre-change content, guarded
	// by the journal's three-way checksum (drift -> error text carries
	// "changed since apply").
	server.RegisterHandler("changes.revert", func(_ context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		var fence builtin.FenceChecker
		if resolveTool != nil {
			fence = resolveTool.FenceChecker()
		}
		path, err := journal.Revert(req.ID, fence)
		if err != nil {
			// ErrEntryNotFound / drift text propagate verbatim to clients.
			return nil, err
		}
		return map[string]any{"restored_path": path}, nil
	})
}
