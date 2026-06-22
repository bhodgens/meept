package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	sid "github.com/caimlas/meept/pkg/id"
)

// BranchSummarizer interface for testability.
type BranchSummarizer interface {
	SummarizeBranch(ctx context.Context, req SummarizeBranchRequest) (*SummarizeBranchResult, error)
}

// BranchManager orchestrates branch navigation, summarization, and tree updates.
type BranchManager struct {
	store      Store
	summarizer BranchSummarizer
	logger     *slog.Logger
	config     config.SessionConfig
}

// NewBranchManager creates a new BranchManager.
func NewBranchManager(store Store, summarizer BranchSummarizer, cfg config.SessionConfig, logger *slog.Logger) *BranchManager {
	if logger == nil {
		logger = slog.Default()
	}
	// Apply defaults
	if cfg.BranchSummaryThreshold == 0 {
		cfg.BranchSummaryThreshold = 5
	}
	return &BranchManager{
		store:      store,
		summarizer: summarizer,
		logger:     logger,
		config:     cfg,
	}
}

// NavigationResult contains the outcome of a branch navigation operation.
type NavigationResult struct {
	OldLeafID     int64  `json:"old_leaf_id"`
	NewLeafID     int64  `json:"new_leaf_id"`
	NewBranchID   string `json:"new_branch_id"`
	Summary       string `json:"summary,omitempty"`
	AbandonedMsgs int    `json:"abandoned_msgs"`
}

// NavigateToBranch orchestrates full branch navigation:
//  1. Validate session and target
//  2. Collect abandoned messages between target and current leaf
//  3. If abandoned messages >= threshold, summarize them
//  4. Insert summary entry (entry_type='summary') as child of fork point
//  5. Insert branch_point entry (entry_type='branch_point') at fork
//  6. Update leaf pointer to target
//  7. Return NavigationResult
func (bm *BranchManager) NavigateToBranch(ctx context.Context, sessionID string, targetMessageID int64) (*NavigationResult, error) {
	// Check if branches are enabled in config
	if !bm.config.BranchesEnabled {
		return nil, fmt.Errorf("branches are disabled (enable via session.branches_enabled in config)")
	}

	// Validate session exists
	session := bm.store.Get(sessionID)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Get current leaf
	leafID, err := bm.store.GetLeafMessageID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get leaf message id: %w", err)
	}
	if leafID == 0 {
		return nil, fmt.Errorf("no active branch (leaf not set)")
	}

	// If target is already the current leaf, no-op
	if targetMessageID == leafID {
		return &NavigationResult{
			OldLeafID:     leafID,
			NewLeafID:     leafID,
			NewBranchID:   "",
			Summary:       "",
			AbandonedMsgs: 0,
		}, nil
	}

	// Get path from root to current leaf
	path, err := bm.store.GetMessagePath(sessionID, leafID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message path: %w", err)
	}

	// Find targetMessageID in the path
	targetIdx := -1
	for i, msg := range path {
		if msg.ID == targetMessageID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return nil, fmt.Errorf("target message %d not found in current branch path", targetMessageID)
	}

	// Collect abandoned = path[targetIdx+1:]
	abandoned := path[targetIdx+1:]
	oldBranchID := BranchMain
	if len(abandoned) > 0 {
		oldBranchID = abandoned[0].BranchID
		if oldBranchID == "" {
			oldBranchID = BranchMain
		}
	} else if len(path) > 0 {
		oldBranchID = path[len(path)-1].BranchID
		if oldBranchID == "" {
			oldBranchID = BranchMain
		}
	}

	// Generate new branch ID
	newBranchID := sid.Generate("branch-")

	var summary string

	// If abandoned messages >= threshold, summarize them
	if len(abandoned) >= bm.config.BranchSummaryThreshold && bm.summarizer != nil {
		chatMsgs := AssembleBranchForMessages(bm.store, abandoned)
		result, err := bm.summarizer.SummarizeBranch(ctx, SummarizeBranchRequest{
			Messages: chatMsgs,
			BranchID: oldBranchID,
		})
		if err != nil {
			bm.logger.Warn("Branch summarization failed, continuing without summary",
				"error", err,
				"branch_id", oldBranchID,
			)
		} else if result != nil {
			summary = result.Summary
			// Insert summary entry as child of fork point
			summaryMsg := Message{
				SessionID: sessionID,
				ParentID:  &targetMessageID,
				Role:      "system",
				Content:   summary,
				Timestamp: time.Now().UTC(),
				EntryType: "summary",
				BranchID:  oldBranchID,
			}
			if err := bm.store.SaveMessages(sessionID, []Message{summaryMsg}); err != nil {
				bm.logger.Error("Failed to save branch summary",
					"error", err,
					"session_id", sessionID,
				)
				// Continue even if summary save fails
			} else {
				bm.logger.Info("Branch summary saved",
					"session_id", sessionID,
					"branch_id", oldBranchID,
					"abandoned_msgs", len(abandoned),
				)
			}
		}
	}

	// Insert branch_point entry at fork
	branchPointMsg := Message{
		SessionID: sessionID,
		ParentID:  &targetMessageID,
		Role:      "system",
		Content:   fmt.Sprintf("branch point: %s -> %s", oldBranchID, newBranchID),
		Timestamp: time.Now().UTC(),
		EntryType: "branch_point",
		BranchID:  newBranchID,
	}
	if err := bm.store.SaveMessages(sessionID, []Message{branchPointMsg}); err != nil {
		return nil, fmt.Errorf("failed to save branch point: %w", err)
	}

	// Update leaf pointer to target
	oldLeaf, err := bm.store.NavigateToBranch(sessionID, targetMessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to update leaf: %w", err)
	}

	bm.logger.Info("Branch navigation completed",
		"session_id", sessionID,
		"old_leaf", oldLeaf,
		"new_leaf", targetMessageID,
		"old_branch", oldBranchID,
		"new_branch", newBranchID,
		"abandoned_msgs", len(abandoned),
		"has_summary", summary != "",
	)

	return &NavigationResult{
		OldLeafID:     oldLeaf,
		NewLeafID:     targetMessageID,
		NewBranchID:   newBranchID,
		Summary:       summary,
		AbandonedMsgs: len(abandoned),
	}, nil
}

// GetBranches returns branch metadata for a session.
func (bm *BranchManager) GetBranches(ctx context.Context, sessionID string) ([]Branch, error) {
	session := bm.store.Get(sessionID)
	if session == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return bm.store.GetMessageBranches(sessionID)
}

// AssembleBranchForMessages loads tool calls and assembles ChatMessages from session messages.
func AssembleBranchForMessages(store Store, messages []Message) []llm.ChatMessage {
	toolCallsMap, err := LoadToolCallsForMessages(store, messages)
	if err != nil {
		// If tool calls fail to load, continue without them
		slog.Default().Warn("Failed to load tool calls for branch assembly",
			"error", err,
			"msg_count", len(messages),
		)
		toolCallsMap = nil
	}
	return AssembleBranch(messages, toolCallsMap)
}

// handleBranchNavigate handles the session.branch.navigate bus topic.
func handleBranchNavigate(bm *BranchManager, payload json.RawMessage) (any, error) {
	var params struct {
		SessionID       string `json:"session_id"`
		TargetMessageID int64  `json:"target_message_id"`
	}
	if err := json.Unmarshal(payload, &params); err != nil {
		return nil, err
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if params.TargetMessageID == 0 {
		return nil, fmt.Errorf("target_message_id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := bm.NavigateToBranch(ctx, params.SessionID, params.TargetMessageID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// handleBranchesList handles the session.branches.list bus topic.
func handleBranchesList(bm *BranchManager, payload json.RawMessage) (any, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(payload, &params); err != nil {
		return nil, err
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	branches, err := bm.GetBranches(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"branches": branches}, nil
}
