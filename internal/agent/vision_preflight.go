package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// visionPreflightSystemPrompt is the system prompt for the vision description call.
const visionPreflightSystemPrompt = "Describe this image in detail. Include any text visible in the image (OCR), key objects, layout, colors, and any notable features. Be concise but thorough."

// needsVisionPreflight returns true if any message in the slice contains
// an image part with an empty Description.
func needsVisionPreflight(messages []llm.ChatMessage) bool {
	for _, msg := range messages {
		if llm.HasUndescribedImages(msg.Parts) {
			return true
		}
	}
	return false
}

// collectUndescribedImageRefs returns pointers to all ImageRef values
// across all messages that have an empty Description OR have AnalysisFailed
// set (meaning a prior attempt failed and should be retried). These are the
// images that need to be analyzed by the vision model.
func collectUndescribedImageRefs(messages []llm.ChatMessage) []*llm.ImageRef {
	var refs []*llm.ImageRef
	for i := range messages {
		for j := range messages[i].Parts {
			p := &messages[i].Parts[j]
			if p.Type == "image_url" && p.ImageURL != nil &&
				(p.ImageURL.Description == "" || p.ImageURL.AnalysisFailed) {
				refs = append(refs, p.ImageURL)
			}
		}
	}
	return refs
}

// runVisionPreflight analyzes undescribed images in the messages slice.
// For each unique image (by URL), it sends a single-turn request to a
// vision-capable model and stores the resulting description on the ImageRef.
// Already-described images are skipped.
//
// The messages slice is modified in-place — ImageRef.Description fields are
// populated as descriptions are obtained.
func runVisionPreflight(ctx context.Context, messages []llm.ChatMessage, chatter llm.Chatter, uploadStore llm.UploadStore, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if !needsVisionPreflight(messages) {
		logger.Debug("vision preflight skipped: no undescribed images")
		return nil
	}

	// Collect unique undescribed image refs by URL. A ref needs analysis when
	// its Description is empty OR its AnalysisFailed flag is set (prior turn's
	// attempt failed and should be retried). The first ref encountered for a
	// given URL is the canonical one we actually send to the vision model; any
	// additional refs with the same URL are propagated after the call so they
	// do not remain stale (M3 fix).
	seen := make(map[string]bool)
	var toDescribe []*llm.ImageRef
	for i := range messages {
		for j := range messages[i].Parts {
			p := &messages[i].Parts[j]
			if p.Type != "image_url" || p.ImageURL == nil {
				continue
			}
			if p.ImageURL.Description != "" && !p.ImageURL.AnalysisFailed {
				continue
			}
			if !seen[p.ImageURL.URL] {
				seen[p.ImageURL.URL] = true
				// Clear the failure flag before retrying so a clean-slate
				// failure path can re-set it if this attempt also fails.
				p.ImageURL.AnalysisFailed = false
				toDescribe = append(toDescribe, p.ImageURL)
			}
		}
	}

	if len(toDescribe) == 0 {
		return nil
	}

	for _, ref := range toDescribe {
		// Per-image timeout: each image gets its own 30s budget so one slow
		// response cannot starve subsequent images. The retry below shares
		// the same per-image context, which is intentional — if the first
		// attempt burned most of the budget, the retry fails fast rather
		// than accumulating a long tail of timed-out calls.
		preflightCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		// Build a single-turn request for this image
		descMsg := []llm.ChatMessage{
			{Role: llm.RoleSystem, Content: visionPreflightSystemPrompt},
			{
				Role: llm.RoleUser,
				Parts: []llm.ContentPart{
					{Type: "image_url", ImageURL: ref},
				},
			},
		}

		resp, err := chatter.ChatWithProgress(preflightCtx, descMsg, nil)
		if err != nil {
			logger.Warn("Vision pre-flight failed", "url", ref.URL, "error", err)

			// Retry once on the same per-image context.
			resp, err = chatter.ChatWithProgress(preflightCtx, descMsg, nil)
			if err != nil {
				cancel()
				// Mark the ref as failed so a subsequent turn retries it.
				// Keep Description empty so ContentFromParts falls back to
				// the URL rather than emitting a confusing sentinel.
				ref.AnalysisFailed = true
				setImageRefStateByURL(messages, ref.URL, "", true, logger)
				continue
			}
		}
		cancel()

		if resp != nil && resp.Content != "" {
			ref.Description = resp.Content
			ref.AnalysisFailed = false
			// Propagate to any other refs with the same URL (M3 fix).
			setImageRefStateByURL(messages, ref.URL, resp.Content, false, logger)
			logger.Debug("Vision pre-flight cached description",
				"url", ref.URL,
				"desc_len", len(resp.Content),
			)
		} else {
			// Empty response: treat as a retryable failure rather than
			// writing a sentinel string that would mask the empty state.
			ref.AnalysisFailed = true
			setImageRefStateByURL(messages, ref.URL, "", true, logger)
		}
	}

	return nil
}

// setImageRefStateByURL updates every image_url part in messages whose URL
// matches url, applying the given description and AnalysisFailed flag. The
// canonical ref (already modified by the caller) is skipped. This is the M3
// fix: duplicate-URL refs were previously left stale because only the first
// ref for each URL was added to the describe queue.
func setImageRefStateByURL(messages []llm.ChatMessage, url, description string, failed bool, logger *slog.Logger) {
	for i := range messages {
		for j := range messages[i].Parts {
			p := &messages[i].Parts[j]
			if p.Type != "image_url" || p.ImageURL == nil {
				continue
			}
			if p.ImageURL.URL != url {
				continue
			}
			// Update in place; the canonical ref is updated identically by
			// the caller, so we don't need to special-case it.
			p.ImageURL.Description = description
			p.ImageURL.AnalysisFailed = failed
		}
	}
}
