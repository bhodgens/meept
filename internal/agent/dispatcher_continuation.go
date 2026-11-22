package agent

// 02-routing/03 T1: draft-refinement continuation route.
//
// A non-command follow-up on a session that holds a PARKED PLANNING task
// with plan_draft metadata refines THAT task's draft (SaveDraft on the
// same task id) instead of minting a new task. Explicit new-work
// commands fall through to normal task creation. The draft CONTENT
// refinement (LLM brainstorm loop) is 06-planning/03; this seam only
// routes the FULL follow-up text to the draft store and replies with the
// updated draft status.

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// threadConvIDForResult returns the turn's resolved thread conversation
// id for DispatchResult (02-routing/03 T3). Only populated when a thread
// router is wired; empty otherwise (session id remains the key).
func threadConvIDForResult(d *Dispatcher, sessionID, conversationID string) string {
	if d == nil || d.threadRouter == nil {
		return ""
	}
	return conversationID
}

// tryDraftContinuation intercepts refinement follow-ups before task
// creation. Returns (result, true, nil) when the turn was consumed as a
// draft refinement; (nil, false, nil) when normal routing must proceed.
// A SaveDraft failure is returned as an error (never silently mints a
// new task while the parked draft goes stale).
func (d *Dispatcher) tryDraftContinuation(ctx context.Context, input, sessionID, conversationID, agentOverride string) (*DispatchResult, bool, error) {
	if d.taskStore == nil {
		return nil, false, nil
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") {
		return nil, false, nil
	}
	// An explicit client agent override names a target executor: that is
	// a dispatch demand, not a draft refinement.
	if agentOverride != "" {
		return nil, false, nil
	}
	if looksLikeNewWorkCommand(trimmed) {
		return nil, false, nil
	}

	parked := d.findParkedDraftTask(sessionID, conversationID)
	if parked == nil {
		return nil, false, nil
	}

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore: d.taskStore,
		Logger:    d.logger,
	})
	draft, ok := sp.DraftFor(parked.ID)
	if !ok || draft == nil {
		return nil, false, nil
	}

	// Route the FULL follow-up text (C3: never Intent.Summary) to the
	// draft seam on the SAME task. SaveDraft enforces the planning-state
	// guard and advances the version.
	markdown := draft.Markdown + "\n## Follow-up refinement\n\n" + trimmed + "\n"
	if err := sp.SaveDraft(parked.ID, markdown); err != nil {
		return nil, false, fmt.Errorf("draft continuation for task %s: %w", parked.ID, err)
	}
	updated, _ := sp.DraftFor(parked.ID)
	version := draft.Version + 1
	if updated != nil {
		version = updated.Version
	}

	intent := &Intent{
		Type:          string(IntentChat),
		Confidence:    1.0,
		AgentType:     config.AgentIDChat,
		Summary:       extractSummary(trimmed),
		OriginalInput: input,
	}
	result := &DispatchResult{
		Task:          parked,
		AgentID:       config.AgentIDChat,
		Intent:        intent,
		OriginalInput: input,
		Response: fmt.Sprintf(
			"Updated the plan draft for task %s (version %d) with your refinement: %s",
			parked.ID, version, trimmed),
	}
	if d.threadRouter != nil {
		result.ThreadConversationID = conversationID
	}
	if d.sessionTracker != nil {
		d.sessionTracker.RecordIntent(sessionID, intent, intent.AgentType)
	}
	d.logger.Info("Follow-up refined parked plan draft",
		"session", sessionID,
		"task", parked.ID,
		"draft_version", version,
	)
	return result, true, nil
}

// findParkedDraftTask returns the most recent StatePlanning task linked
// to the session (by session id or either conversation id) that carries
// plan_draft metadata. Nil when none exists.
func (d *Dispatcher) findParkedDraftTask(sessionID, conversationID string) *task.Task {
	keys := []string{sessionID}
	if d.sessionStore != nil {
		if sess := d.sessionStore.Get(sessionID); sess != nil && sess.ConversationID != "" {
			keys = append(keys, sess.ConversationID)
		} else if sess := d.sessionStore.GetByConversationID(sessionID); sess != nil && sess.ConversationID != "" {
			keys = append(keys, sess.ConversationID)
		}
	}
	if conversationID != "" {
		keys = append(keys, conversationID)
	}

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore: d.taskStore,
		Logger:    d.logger,
	})

	seen := make(map[string]bool)
	var parked *task.Task
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		tasks, err := d.taskStore.GetTasksForSession(key)
		if err != nil {
			d.logger.Debug("draft continuation: session task lookup failed", "key", key, "error", err)
			continue
		}
		for _, t := range tasks {
			if t == nil || t.State != task.StatePlanning {
				continue
			}
			if _, ok := sp.DraftFor(t.ID); !ok {
				continue
			}
			if parked == nil || t.CreatedAt.After(parked.CreatedAt) {
				parked = t
			}
		}
	}
	return parked
}

// looksLikeNewWorkCommand discriminates explicit fresh-work commands
// from refinements of the parked draft. A creation verb paired with a
// concrete artifact noun (file/directory/task/...) is new work; bare
// refinement phrasing ("make it five languages instead", "also add a
// dry-run flag") is not.
func looksLikeNewWorkCommand(input string) bool {
	lower := strings.ToLower(input)

	// Unambiguous fresh-work phrases.
	for _, phrase := range []string{
		"new task", "new plan", "start over", "start a new", "separate task",
		"create a file", "create file", "create a new file", "write a file",
		"create a directory", "create a folder", "make a file", "new file",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	// Creation verb + artifact noun in the same turn.
	verbs := []string{"create", "write", "build", "generate", "scaffold", "implement"}
	artifacts := []string{"file", "directory", "folder", "script named", "task", "project", "repository", "repo"}
	hasVerb := false
	for _, v := range verbs {
		if strings.Contains(lower, v) {
			hasVerb = true
			break
		}
	}
	if !hasVerb {
		return false
	}
	for _, a := range artifacts {
		if strings.Contains(lower, a) {
			return true
		}
	}
	return false
}
