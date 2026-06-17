package llm

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type CompactorConfig struct {
	ReserveTokens     int
	KeepRecentTokens  int
	MaxResponseTokens int
	SummaryFormat     string
	Strategy          string // "structured" | "handoff" | "off"
	TrackFileOps      bool
	TimeoutSeconds    int
}

func DefaultCompactorConfig() CompactorConfig {
	return CompactorConfig{
		ReserveTokens:     16384,
		KeepRecentTokens:  20000,
		MaxResponseTokens: 13107,
		SummaryFormat:     "structured",
		Strategy:          "structured",
		TrackFileOps:      true,
		TimeoutSeconds:    30,
	}
}

type CutResult struct {
	CutIndex       int
	ToCompact      []ChatMessage
	ToKeep         []ChatMessage
	SystemMsgs     []ChatMessage
	SplitTurn      bool
	SplitTurnIndex int
}

type FileOperationSet struct {
	Read    map[string]bool
	Written map[string]bool
	Edited  map[string]bool
}

func NewFileOperationSet() *FileOperationSet {
	return &FileOperationSet{Read: make(map[string]bool), Written: make(map[string]bool), Edited: make(map[string]bool)}
}

// snapshotFileOps returns a deep-copied snapshot of the receiver. The caller
// must hold c.mu when calling this on c.fileOps so that the source maps are
// not mutated during the copy.
func (c *ContextCompactor) snapshotFileOps() *FileOperationSet {
	if c.fileOps == nil {
		return NewFileOperationSet()
	}
	snap := &FileOperationSet{
		Read:    make(map[string]bool, len(c.fileOps.Read)),
		Written: make(map[string]bool, len(c.fileOps.Written)),
		Edited:  make(map[string]bool, len(c.fileOps.Edited)),
	}
	for k, v := range c.fileOps.Read {
		snap.Read[k] = v
	}
	for k, v := range c.fileOps.Written {
		snap.Written[k] = v
	}
	for k, v := range c.fileOps.Edited {
		snap.Edited[k] = v
	}
	return snap
}

func (f *FileOperationSet) Merge(other *FileOperationSet) {
	if other == nil {
		return
	}
	for k := range other.Read {
		f.Read[k] = true
	}
	for k := range other.Written {
		f.Written[k] = true
	}
	for k := range other.Edited {
		f.Edited[k] = true
	}
}

func (f *FileOperationSet) FileCount() int {
	if f == nil {
		return 0
	}
	return len(f.Read) + len(f.Written) + len(f.Edited)
}

func (f *FileOperationSet) FormatCompact() string {
	if f == nil || f.FileCount() == 0 {
		return ""
	}
	var sb strings.Builder
	for p := range f.Read {
		fmt.Fprintf(&sb, "read: %s\n", p)
	}
	for p := range f.Written {
		fmt.Fprintf(&sb, "write: %s\n", p)
	}
	for p := range f.Edited {
		fmt.Fprintf(&sb, "edit: %s\n", p)
	}
	return sb.String()
}

type CompactResult struct {
	Messages       []ChatMessage
	Compacted      bool
	TokensBefore   int
	TokensAfter    int
	SummaryContent string
	FileOps        *FileOperationSet
	SplitTurn      bool
}

type ContextCompactor struct {
	config      CompactorConfig
	summarizer  Chatter
	tokenizer   Tokenizer
	logger      *slog.Logger
	mu          sync.RWMutex
	fileOps     *FileOperationSet
	lastSummary string
}

func NewContextCompactor(cfg CompactorConfig, summarizer Chatter, tokenizer Tokenizer, logger *slog.Logger) *ContextCompactor {
	if logger == nil {
		logger = slog.Default()
	}
	if tokenizer == nil {
		tokenizer = &HeuristicTokenizer{}
	}
	return &ContextCompactor{config: cfg, summarizer: summarizer, tokenizer: tokenizer, logger: logger, fileOps: NewFileOperationSet()}
}

func (c *ContextCompactor) Compact(ctx context.Context, messages []ChatMessage) CompactResult {
	// D9 FIX: Copy config/state under lock to minimize lock hold time.
	// LLM summarizer calls are made without holding the mutex to avoid
	// serializing all compactions system-wide.
	c.mu.Lock()
	strategy := c.config.Strategy
	if strategy == "" {
		strategy = c.config.SummaryFormat
	}
	summarizer := c.summarizer
	timeout := time.Duration(c.config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	c.mu.Unlock()

	// Strategy "off" skips compaction entirely.
	if strategy == "off" {
		result := CompactResult{TokensBefore: c.countTokens(messages), Messages: messages}
		return result
	}

	// Run tool output pruning before any compaction logic.
	messages = c.pruneToolOutputs(messages)

	tokensBefore := c.countTokens(messages)
	result := CompactResult{TokensBefore: tokensBefore}
	if summarizer == nil {
		result.Messages = messages
		return result
	}

	cut := c.findCutPoint(messages)
	result.SplitTurn = cut.SplitTurn
	conversationText := c.serializeMessages(cut.ToCompact)
	if conversationText == "" {
		result.Messages = messages
		return result
	}

	// D9: LLM calls made without mutex held
	var summary string
	if cut.SplitTurn && cut.SplitTurnIndex >= 0 && cut.SplitTurnIndex < len(cut.ToCompact) {
		var err error
		summary, err = c.compactSplitTurn(ctx, cut, timeout)
		if err != nil {
			c.logger.Warn("split-turn compaction failed, falling back to single summary", "error", err)
			summary = ""
		}
	}
	if summary == "" {
		var err error
		summary, err = c.summarizeMessages(ctx, cut.ToCompact, timeout)
		if err != nil {
			c.logger.Warn("compaction summarization failed", "error", err)
			result.Messages = messages
			return result
		}
		if summary == "" {
			result.Messages = messages
			return result
		}
	}

	// D9: Re-lock only for updating shared state
	c.mu.Lock()
	extract := c.parseSummaryResponse(summary)
	c.updateFileOps(extract)
	// Snapshot fileOps under the lock so concurrent Compact callers don't race
	// on the shared map (FileCount/FormatCompact read while updateFileOps writes).
	fileOpsSnap := c.snapshotFileOps()
	result.FileOps = fileOpsSnap
	compactionMsg := c.buildCompactionMessage(summary, fileOpsSnap)
	result.SummaryContent = summary
	c.lastSummary = summary
	c.mu.Unlock()

	final := make([]ChatMessage, 0, len(cut.SystemMsgs)+1+len(cut.ToKeep))
	final = append(final, cut.SystemMsgs...)
	final = append(final, compactionMsg)
	final = append(final, cut.ToKeep...)
	result.Messages = final
	result.Compacted = true
	result.TokensAfter = c.countTokens(final)
	// Snapshot fileOps.FileCount under the lock to avoid racing with updateFileOps.
	c.mu.RLock()
	filesTracked := c.fileOps.FileCount()
	c.mu.RUnlock()
	c.logger.Info("context compacted", "tokens_before", tokensBefore, "tokens_after", result.TokensAfter, "split_turn", result.SplitTurn, "files_tracked", filesTracked)
	return result
}

// compactSplitTurn handles the case where the cut point lands mid-turn.
// It produces two summaries (history + turn prefix) and merges them.
// Both LLM calls share the overall timeout budget derived from ctx.
func (c *ContextCompactor) compactSplitTurn(ctx context.Context, cut CutResult, timeout time.Duration) (result string, err error) {
	historyMessages := cut.ToCompact[:cut.SplitTurnIndex]
	turnPrefixMessages := cut.ToCompact[cut.SplitTurnIndex:]

	// If history is empty, just summarize the turn prefix as a regular summary.
	if len(historyMessages) == 0 {
		return c.summarizeMessages(ctx, turnPrefixMessages, timeout)
	}

	// If turn prefix is empty, just summarize the history.
	if len(turnPrefixMessages) == 0 {
		return c.summarizeMessages(ctx, historyMessages, timeout)
	}

	// Use a shared deadline for both LLM calls so the total does not exceed timeout.
	deadline := time.Now().Add(timeout)
	sharedCtx, sharedCancel := context.WithDeadline(ctx, deadline)
	defer sharedCancel()

	// Generate history summary (full structured summary of all messages before the split).
	halfTimeout := max(timeout/2, 5*time.Second)
	historySummary, err := c.summarizeMessages(sharedCtx, historyMessages, halfTimeout)
	if err != nil {
		return "", fmt.Errorf("history summarization failed: %w", err)
	}

	// Generate turn prefix summary (brief summary of the partial turn).
	turnPrefixText := c.serializeMessages(turnPrefixMessages)
	turnPrefixSummary := ""
	if turnPrefixText != "" {
		prompt := c.buildTurnPrefixPrompt(turnPrefixText)
		sumCtx, cancel := context.WithDeadline(sharedCtx, deadline)
		defer cancel()
		resp, err := c.summarizer.Chat(sumCtx, []ChatMessage{{Role: RoleUser, Content: prompt}})
		if err != nil {
			c.logger.Warn("turn prefix summarization failed, using raw text", "error", err)
			turnPrefixSummary = turnPrefixText
		} else if resp.Content != "" {
			turnPrefixSummary = resp.Content
		}
	}

	// Merge both summaries.
	if turnPrefixSummary == "" {
		return historySummary, nil
	}
	return historySummary + "\n\n## In-Progress Turn (compacted mid-turn)\n" + turnPrefixSummary, nil
}

// summarizeMessages is a helper that builds a prompt for the given messages
// and calls the LLM summarizer.
func (c *ContextCompactor) summarizeMessages(ctx context.Context, messages []ChatMessage, timeout time.Duration) (result string, err error) {
	conversationText := c.serializeMessages(messages)
	if conversationText == "" {
		return "", nil
	}
	// Snapshot lastSummary under the lock since Compact writes it concurrently.
	c.mu.RLock()
	lastSummary := c.lastSummary
	c.mu.RUnlock()
	prompt := c.buildSummaryPrompt(conversationText, lastSummary)
	sumCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := c.summarizer.Chat(sumCtx, []ChatMessage{{Role: RoleUser, Content: prompt}})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

const turnPrefixPrompt = `Summarize concisely what the assistant was doing in this partial turn.
Focus on:
- What tool was being called and why
- What the tool returned (if available)
- What the assistant was about to do next

Keep the summary brief (2-4 sentences).

<partial_turn>
%s
</partial_turn>`

// buildTurnPrefixPrompt builds the prompt for summarizing a partial turn.
func (c *ContextCompactor) buildTurnPrefixPrompt(turnText string) string {
	return fmt.Sprintf(turnPrefixPrompt, turnText)
}

// LastSummary returns the most recent compaction summary.
// Acquires RLock because Compact() writes c.lastSummary under the write lock.
func (c *ContextCompactor) LastSummary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSummary
}

// FileOperations returns a snapshot of the tracked file operations.
// Acquires RLock because Compact() writes c.fileOps under the write lock.
// The returned pointer is a snapshot copy, safe for independent use.
func (c *ContextCompactor) FileOperations() *FileOperationSet {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.fileOps == nil {
		return nil
	}
	snap := &FileOperationSet{
		Read:    make(map[string]bool, len(c.fileOps.Read)),
		Written: make(map[string]bool, len(c.fileOps.Written)),
		Edited:  make(map[string]bool, len(c.fileOps.Edited)),
	}
	for k, v := range c.fileOps.Read {
		snap.Read[k] = v
	}
	for k, v := range c.fileOps.Written {
		snap.Written[k] = v
	}
	for k, v := range c.fileOps.Edited {
		snap.Edited[k] = v
	}
	return snap
}

func (c *ContextCompactor) findCutPoint(messages []ChatMessage) CutResult {
	keepBudget := c.config.KeepRecentTokens
	if keepBudget <= 0 {
		keepBudget = 20000
	}
	var systemMsgs, nonSystem []ChatMessage
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			nonSystem = append(nonSystem, msg)
		}
	}
	if len(nonSystem) == 0 {
		return CutResult{SystemMsgs: systemMsgs, ToKeep: nonSystem}
	}

	tokenCount := 0
	cutIdx := len(nonSystem)
	for i := range slices.Backward(nonSystem) {
		msgTokens := c.countMessageTokens(nonSystem[i])
		if tokenCount+msgTokens > keepBudget && i < len(nonSystem)-1 {
			cutIdx = i + 1
			break
		}
		tokenCount += msgTokens
	}
	if cutIdx == 0 || cutIdx >= len(nonSystem) {
		return CutResult{SystemMsgs: systemMsgs, ToKeep: nonSystem}
	}

	adjustedIdx := c.adjustCutPoint(nonSystem, cutIdx)
	split, splitIdx := c.findSplitTurnBoundary(nonSystem[:adjustedIdx])
	return CutResult{CutIndex: adjustedIdx, ToCompact: nonSystem[:adjustedIdx], ToKeep: nonSystem[adjustedIdx:], SystemMsgs: systemMsgs, SplitTurn: split, SplitTurnIndex: splitIdx}
}

func (c *ContextCompactor) adjustCutPoint(messages []ChatMessage, cutIdx int) int {
	if cutIdx <= 0 || cutIdx >= len(messages) {
		return cutIdx
	}
	start := cutIdx
	for start > 0 && messages[start-1].Role == RoleTool {
		start--
		//nolint:gosec // index bounded by upstream check
		if start > 0 && messages[start-1].Role == RoleAssistant && len(messages[start-1].ToolCalls) > 0 {
			start--
		}
	}
	if start < cutIdx {
		cutIdx = start
	}
	for i := cutIdx; i < len(messages); i++ {
		if messages[i].Role == RoleUser {
			return i
		}
	}
	return cutIdx
}

func (c *ContextCompactor) isSplitTurn(messages []ChatMessage, cutIdx int) bool {
	if cutIdx <= 0 || cutIdx >= len(messages) {
		return false
	}
	if messages[cutIdx].Role == RoleTool {
		for i := cutIdx - 1; i >= 0; i-- {
			if messages[i].Role == RoleAssistant && len(messages[i].ToolCalls) > 0 {
				return true
			}
			if messages[i].Role == RoleUser {
				return false
			}
		}
	}
	if messages[cutIdx].Role == RoleAssistant && len(messages[cutIdx].ToolCalls) > 0 {
		for i := cutIdx + 1; i < len(messages); i++ {
			if messages[i].Role == RoleTool {
				return true
			}
			if messages[i].Role == RoleUser {
				return false
			}
		}
	}
	return false
}

func (c *ContextCompactor) findSplitTurnBoundary(toCompact []ChatMessage) (_ bool, _ int) {
	if len(toCompact) == 0 {
		return false, -1
	}
	for i := range slices.Backward(toCompact) {
		msg := toCompact[i]
		if msg.Role == RoleAssistant && len(msg.ToolCalls) > 0 {
			hasResults := false
			for j := i + 1; j < len(toCompact); j++ {
				if toCompact[j].Role == RoleTool {
					hasResults = true
					break
				}
				if toCompact[j].Role == RoleUser {
					break
				}
			}
			if !hasResults {
				return true, i
			}
		}
	}
	return false, -1
}

func (c *ContextCompactor) serializeMessages(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case RoleUser:
			fmt.Fprintf(&sb, "[User]: %s\n", msg.Content)
		case RoleAssistant:
			fmt.Fprintf(&sb, "[Assistant]: %s\n", msg.Content)
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&sb, "  [Tool Call]: %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
		case RoleTool:
			content := msg.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "  [Tool Result]: %s\n", content)
		}
	}
	return sb.String()
}

const structuredCompactionPrompt = `You are summarizing a conversation to preserve context for continued work.
Extract the following structured information:

## Goal
[What the user is trying to accomplish]

## Constraints
[Requirements, restrictions, or preferences mentioned]

## Progress
[What has been done so far, including approach attempts and outcomes]

## Key Decisions
- [list key decisions made, one per line]

## Files
- [list all file paths READ, one per line, prefixed with "read: "]
- [list all file paths WRITTEN/CREATED, one per line, prefixed with "write: "]
- [list all file paths EDITED/MODIFIED, one per line, prefixed with "edit: "]

## Important Discoveries
- [list important findings, one per line]

## Errors Encountered
- [list errors or failures encountered and what was learned, one per line]

## Next Steps
[What remains to be done, in order of priority]

## Critical Context
[Any context that must be preserved for the work to continue correctly]

<conversation>
%s
</conversation>`

const iterativeUpdatePrompt = `You are updating a conversation summary with new context.

## Previous Summary
%s

## New Messages Since Last Summary
%s

Produce an updated summary in the same structured format. Preserve all information from the
previous summary that is still relevant. Add new information from the new messages.
Remove information that is no longer relevant or has been superseded.

Use these sections:
## Goal
## Constraints
## Progress
## Key Decisions
## Files (use "read: ", "write: ", "edit: " prefixes)
## Important Discoveries
## Errors Encountered
## Next Steps
## Critical Context`

const narrativeCompactionPrompt = `Summarize the following conversation concisely, preserving:
- What the user is trying to accomplish
- Key decisions made
- Files read, written, or edited
- Important discoveries and errors encountered
- What remains to be done
- Any critical context (API endpoints, config values, commands)

<conversation>
%s
</conversation>`

func (c *ContextCompactor) buildSummaryPrompt(conversationText, existingSummary string) string {
	strategy := c.config.Strategy
	if strategy == "" {
		strategy = c.config.SummaryFormat
	}

	// Handoff strategy always uses the handoff prompt (no iterative update).
	if strategy == "handoff" {
		return fmt.Sprintf(handoffCompactionPrompt, conversationText)
	}

	if existingSummary != "" && c.config.SummaryFormat != "narrative" {
		return fmt.Sprintf(iterativeUpdatePrompt, existingSummary, conversationText)
	}
	if c.config.SummaryFormat == "narrative" {
		return fmt.Sprintf(narrativeCompactionPrompt, conversationText)
	}
	return fmt.Sprintf(structuredCompactionPrompt, conversationText)
}

const handoffCompactionPrompt = `You are producing a handoff summary for another agent that will continue this work.
Capture exact technical state, not abstractions. The next agent needs precise information to resume without loss.

## Goal
[What the user is trying to accomplish, stated exactly as given]

## Current State
[Exact current state: what is working, what is broken, what is in progress]

## Files
- [list every file path touched, prefixed with "read: ", "write: ", or "edit: "]
- include the exact path as used in the conversation, not paraphrased

## Commands Run
- [list every shell command executed and its exact result, one per line]

## Test Results
- [paste exact test output, error messages, and failure lines — not paraphrased]

## Errors Encountered
- [paste exact error messages, stack traces, and diagnostic output — not paraphrased]

## Git State
[exact branch name, last commit hash, uncommitted changes summary]

## Symbols
[exact function names, type names, variable names, and line numbers referenced]

## Key Decisions
- [list key decisions made with their rationale, one per line]

## Next Steps
[ordered list of what remains, with exact file paths and symbol names where applicable]

## Critical Context
[Any exact values, API endpoints, config values, or identifiers that must be preserved verbatim]

<conversation>
%s
</conversation>`

// getCompactionPrompt returns the compaction prompt template based on the
// configured strategy. Returns an empty string when strategy is "off".
func (c *ContextCompactor) getCompactionPrompt(strategy string) string {
	switch strategy {
	case "off":
		return ""
	case "handoff":
		return handoffCompactionPrompt
	default:
		return structuredCompactionPrompt
	}
}

var compactionSectionRe = regexp.MustCompile(`(?m)^##\s*(Goal|Constraints|Progress|Key Decisions|Files|Important Discoveries|Errors Encountered|Next Steps|Critical Context)\s*$`)

func (c *ContextCompactor) parseSummaryResponse(raw string) SummaryExtract {
	var ext SummaryExtract
	sections := splitCompactionSections(raw)
	if len(sections) > 0 {
		ext.Decisions = parseBulletItems(sections["Key Decisions"])
		ext.FilePaths = parseBulletItems(sections["Files"])
		ext.UnresolvedQuestions = parseBulletItems(sections["Constraints"])
		ext.TaskState = strings.TrimSpace(sections["Progress"])
		ext.KeyFindings = parseBulletItems(sections["Important Discoveries"])
		for _, line := range parseBulletItems(sections["Files"]) {
			switch {
			case strings.HasPrefix(line, "read: "):
				ext.FileReads = append(ext.FileReads, strings.TrimPrefix(line, "read: "))
			case strings.HasPrefix(line, "write: "):
				ext.FileWrites = append(ext.FileWrites, strings.TrimPrefix(line, "write: "))
			case strings.HasPrefix(line, "edit: "):
				ext.FileEdits = append(ext.FileEdits, strings.TrimPrefix(line, "edit: "))
			}
		}
		ext.ErrorsEncountered = parseBulletItems(sections["Errors Encountered"])
		return ext
	}
	return parseStructuredSummary(raw)
}

func splitCompactionSections(raw string) map[string]string {
	result := make(map[string]string)
	matches := compactionSectionRe.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return result
	}
	for i, m := range matches {
		name := raw[m[2]:m[3]]
		start := m[1]
		end := len(raw)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		result[name] = raw[start:end]
	}
	return result
}

func (c *ContextCompactor) updateFileOps(summary SummaryExtract) {
	if !c.config.TrackFileOps || c.fileOps == nil {
		return
	}
	for _, f := range summary.FileReads {
		c.fileOps.Read[f] = true
	}
	for _, f := range summary.FileWrites {
		c.fileOps.Written[f] = true
	}
	for _, f := range summary.FileEdits {
		c.fileOps.Edited[f] = true
	}
	for _, f := range summary.FilePaths {
		switch {
		case strings.HasPrefix(f, "read: "):
			c.fileOps.Read[strings.TrimPrefix(f, "read: ")] = true
		case strings.HasPrefix(f, "write: "):
			c.fileOps.Written[strings.TrimPrefix(f, "write: ")] = true
		case strings.HasPrefix(f, "edit: "):
			c.fileOps.Edited[strings.TrimPrefix(f, "edit: ")] = true
		default:
			c.fileOps.Read[f] = true
		}
	}
}

func (c *ContextCompactor) buildCompactionMessage(summary string, fileOps *FileOperationSet) ChatMessage {
	var sb strings.Builder
	sb.WriteString("[Compacted Context] ")
	sb.WriteString(summary)
	if fileOps != nil && fileOps.FileCount() > 0 {
		sb.WriteString("\n\n## Cumulative File Operations\n")
		sb.WriteString(fileOps.FormatCompact())
	}
	return ChatMessage{Role: RoleSystem, Content: sb.String()}
}

// pruneToolOutputs truncates oversized tool result messages that are outside
// the most recent protected window. It runs before compaction to reduce token
// pressure from verbose tool outputs.
//
// Algorithm:
//  1. Walk messages backward.
//  2. For tool result messages, estimate token count.
//  3. Track a running total of protected tokens (most recent 40 000 tokens of
//     tool output). Messages within the protected window are never pruned.
//  4. Never prune tool results from "file_read" or "memory_search" tools.
//  5. Only prune when total savings would exceed 20 000 tokens.
//  6. Replace pruned output with "[Output truncated — N tokens saved]".
func (c *ContextCompactor) pruneToolOutputs(messages []ChatMessage) []ChatMessage {
	const (
		protectedBudget = 40000 // protect most recent N tokens of tool output
		minSavings      = 20000 // only prune if we can save at least this many
	)

	// Build a lookup from ToolCallID -> tool name by scanning assistant messages.
	toolNameByID := make(map[string]string)
	for _, msg := range messages {
		if msg.Role == RoleAssistant {
			for _, tc := range msg.ToolCalls {
				toolNameByID[tc.ID] = tc.Function.Name
			}
		}
	}

	type pruneCandidate struct {
		index  int
		tokens int
	}

	var candidates []pruneCandidate
	totalSavings := 0
	protectedTokens := 0

	// Walk backward to determine which tool outputs are outside the protected window.
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != RoleTool {
			continue
		}

		tokens := c.tokenizer.CountTokens(msg.Content)

		// Check if this tool output is from a protected tool.
		toolName := toolNameByID[msg.ToolCallID]
		if toolName == "file_read" || toolName == "memory_search" {
			continue // never prune these
		}

		// If within the protected budget, skip.
		protectedTokens += tokens
		if protectedTokens <= protectedBudget {
			continue
		}

		// Candidate for pruning — compute how much we'd save by truncating.
		// We leave a small stub so the message still exists.
		stubTokens := c.tokenizer.CountTokens(fmt.Sprintf("[Output truncated — %d tokens saved]", tokens))
		savings := tokens - stubTokens
		if savings > 0 {
			candidates = append(candidates, pruneCandidate{index: i, tokens: savings})
			totalSavings += savings
		}
	}

	// Only prune if total savings exceed the minimum threshold.
	if totalSavings < minSavings || len(candidates) == 0 {
		return messages
	}

	// Build a new slice with pruned messages.
	result := make([]ChatMessage, len(messages))
	copy(result, messages)

	for _, cand := range candidates {
		orig := result[cand.index]
		truncationNotice := fmt.Sprintf("[Output truncated — %d tokens saved]", cand.tokens)
		result[cand.index] = ChatMessage{
			Role:       orig.Role,
			Content:    truncationNotice,
			Name:       orig.Name,
			ToolCalls:  orig.ToolCalls,
			ToolCallID: orig.ToolCallID,
		}
	}

	c.logger.Info("pruned oversized tool outputs",
		"candidates", len(candidates),
		"tokens_saved", totalSavings,
	)
	return result
}

func (c *ContextCompactor) countTokens(messages []ChatMessage) int {
	total := 0
	for _, msg := range messages {
		total += c.countMessageTokens(msg)
	}
	return total
}

// countMessageTokens returns the token count for a single ChatMessage,
// accounting for Content, ToolCalls, ToolCallID, and Name fields.
func (c *ContextCompactor) countMessageTokens(msg ChatMessage) int {
	total := c.tokenizer.CountTokens(msg.Content)
	for _, tc := range msg.ToolCalls {
		total += c.tokenizer.CountTokens(tc.Function.Name)
		total += c.tokenizer.CountTokens(tc.Function.Arguments)
	}
	if msg.ToolCallID != "" {
		total += c.tokenizer.CountTokens(msg.ToolCallID)
	}
	if msg.Name != "" {
		total += c.tokenizer.CountTokens(msg.Name)
	}
	return total
}
