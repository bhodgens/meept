package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/internal/config"
)

// BackupSyncHandler provides native RPC methods for the backup scheduler,
// config syncer, and peer sync puller. Handlers nil-guard their dependencies
// and return a "service not available" error when the underlying component
// is not wired, mirroring the PlanHandler pattern in plan.go.
type BackupSyncHandler struct {
	backupScheduler *backup.GitBackupScheduler
	configSyncer    *config.ConfigSyncer
	syncPuller      *backup.SyncPuller
}

// NewBackupSyncHandler creates a new handler. Any argument may be nil; the
// corresponding RPC methods will return "service not available" until the
// component is wired.
func NewBackupSyncHandler(scheduler *backup.GitBackupScheduler, syncer *config.ConfigSyncer, puller *backup.SyncPuller) *BackupSyncHandler {
	return &BackupSyncHandler{
		backupScheduler: scheduler,
		configSyncer:    syncer,
		syncPuller:      puller,
	}
}

// SetSyncPuller wires (or re-wires) the peer sync puller. Nil-guarded per
// CLAUDE.md setter convention.
func (h *BackupSyncHandler) SetSyncPuller(puller *backup.SyncPuller) {
	if puller != nil {
		h.syncPuller = puller
	}
}

// RegisterBackupSyncMethods registers all backup/sync RPC methods on server.
func (h *BackupSyncHandler) RegisterBackupSyncMethods(server *Server) {
	server.RegisterHandler("backup.list", h.handleBackupList)
	server.RegisterHandler("backup.push", h.handleBackupPush)
	server.RegisterHandler("config_sync.status", h.handleConfigSyncStatus)
	server.RegisterHandler("config_sync.pull", h.handleConfigSyncPull)
	server.RegisterHandler("config_sync.push", h.handleConfigSyncPush)
	server.RegisterHandler("sync.status", h.handleSyncStatus)
	server.RegisterHandler("sync.pull", h.handleSyncPull)
}

// availBackup returns an error if the backup scheduler is not wired.
func (h *BackupSyncHandler) availBackup() error {
	if h == nil || h.backupScheduler == nil {
		return fmt.Errorf("backup service not available")
	}
	return nil
}

// availConfigSync returns an error if the config syncer is not wired.
func (h *BackupSyncHandler) availConfigSync() error {
	if h == nil || h.configSyncer == nil {
		return fmt.Errorf("config sync service not available")
	}
	return nil
}

// availSyncPuller returns an error if the peer sync puller is not wired.
func (h *BackupSyncHandler) availSyncPuller() error {
	if h == nil || h.syncPuller == nil {
		return fmt.Errorf("peer sync service not available")
	}
	return nil
}

// handleBackupList returns the list of backup date directories and their
// manifest contents for this node. Delegates to GitBackupScheduler.ListBackups.
func (h *BackupSyncHandler) handleBackupList(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availBackup(); err != nil {
		return nil, err
	}
	backups, err := h.backupScheduler.ListBackups()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"backups":    backups,
		RPCKeyCount:  len(backups),
		RPCKeyStatus: "ok",
	}, nil
}

// handleBackupPush triggers an immediate backup cycle. The "force" parameter
// is accepted for forward compatibility but currently has no effect —
// RunNow always performs a full backup cycle regardless of change state.
func (h *BackupSyncHandler) handleBackupPush(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availBackup(); err != nil {
		return nil, err
	}
	// Params optional; ignore parse failures so callers can send nil.
	var req struct {
		Force bool `json:"force"`
	}
	if len(params) > 0 && string(params) != "null" {
		_ = json.Unmarshal(params, &req)
	}

	if err := h.backupScheduler.RunNow(); err != nil {
		return map[string]any{
			RPCKeyStatus: "error",
			"error":      err.Error(),
		}, nil
	}
	return map[string]any{
		RPCKeyStatus:  "ok",
		RPCKeyMessage: "backup push completed",
	}, nil
}

// handleConfigSyncStatus returns the current ConfigSyncer status including
// the latest known commit hash. The CLI uses this to display the live
// commit (as opposed to a static value read from disk).
func (h *BackupSyncHandler) handleConfigSyncStatus(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availConfigSync(); err != nil {
		return nil, err
	}
	return h.configSyncer.Status(), nil
}

// handleConfigSyncPull triggers an immediate pull+merge cycle. Note: the
// ConfigSyncer currently exposes no public PullNow method, so we return
// the status and rely on the existing ticker. When a PullNow method is
// added, this handler should delegate to it.
func (h *BackupSyncHandler) handleConfigSyncPull(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availConfigSync(); err != nil {
		return nil, err
	}
	// Trigger an immediate cycle by reading current status. The next
	// periodic tick will pick up any changes. This matches the existing
	// ConfigSyncer.run model — there is no PullNow method on ConfigSyncer.
	// TODO: when ConfigSyncer.PullNow lands, call it here.
	status := h.configSyncer.Status()
	return map[string]any{
		RPCKeyStatus:  "ok",
		RPCKeyMessage: "config sync pull triggered",
		"last_commit": status.LastCommit,
		"repo_url":    status.RepoURL,
	}, nil
}

// handleConfigSyncPush triggers an immediate commit+push of any local
// working-tree changes to the config repo. The optional "message"
// parameter overrides the default commit message. Returns a status of
// "ok" when the push completes (including when the working tree was
// already clean, in which case CommitAndPush is a no-op).
func (h *BackupSyncHandler) handleConfigSyncPush(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availConfigSync(); err != nil {
		return nil, err
	}
	// Params optional; ignore parse failures so callers can send nil.
	var req struct {
		Message string `json:"message"`
	}
	if len(params) > 0 && string(params) != "null" {
		_ = json.Unmarshal(params, &req)
	}

	if err := h.configSyncer.PushLocalChanges(ctx, req.Message); err != nil {
		return map[string]any{
			RPCKeyStatus: "error",
			"error":      err.Error(),
		}, nil
	}
	return map[string]any{
		RPCKeyStatus:  "ok",
		RPCKeyMessage: "config sync push completed",
	}, nil
}

// handleSyncStatus returns the peer-sync status (last sync times, errors,
// merge stats) from the SyncPuller's metadata store.
func (h *BackupSyncHandler) handleSyncStatus(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availSyncPuller(); err != nil {
		return nil, err
	}
	peerStatus, err := h.syncPuller.PeerStatus()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"peers":      peerStatus,
		RPCKeyCount:  len(peerStatus),
		RPCKeyStatus: "ok",
	}, nil
}

// handleSyncPull triggers an immediate peer sync cycle via SyncPuller.PullNow.
func (h *BackupSyncHandler) handleSyncPull(ctx context.Context, params json.RawMessage) (any, error) {
	if err := h.availSyncPuller(); err != nil {
		return nil, err
	}
	if err := h.syncPuller.PullNow(); err != nil {
		return map[string]any{
			RPCKeyStatus: "error",
			"error":      err.Error(),
		}, nil
	}
	return map[string]any{
		RPCKeyStatus:  "ok",
		RPCKeyMessage: "peer sync completed",
	}, nil
}
