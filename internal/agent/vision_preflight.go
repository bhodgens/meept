package agent

import (
	"context"
	"fmt"
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
// across all messages that have an empty Description. These are the images
// that need to be analyzed by the vision model.
func collectUndescribedImageRefs(messages []llm.ChatMessage) []*llm.ImageRef {
	var refs []*llm.ImageRef
	for i := range messages {
		for j := range messages[i].Parts {
			p := &messages[i].Parts[j]
			if p.Type == "image_url" && p.ImageURL != nil && p.ImageURL.Description == "" {
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
	if !needsVisionPreflight(messages) {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Collect unique undescribed image refs by URL
	seen := make(map[string]bool)
	var toDescribe []*llm.ImageRef
	for i := range messages {
		for j := range messages[i].Parts {
			p := &messages[i].Parts[j]
			if p.Type != "image_url" || p.ImageURL == nil || p.ImageURL.Description != "" {
				continue
			}
			if !seen[p.ImageURL.URL] {
				seen[p.ImageURL.URL] = true
				toDescribe = append(toDescribe, p.ImageURL)
			}
		}
	}

	if len(toDescribe) == 0 {
		return nil
	}

	preflightCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, ref := range toDescribe {
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

			// Retry once
			resp, err = chatter.ChatWithProgress(preflightCtx, descMsg, nil)
			if err != nil {
				ref.Description = fmt.Sprintf("[image analysis failed: %v]", err)
				continue
			}
		}

		if resp != nil && resp.Content != "" {
			ref.Description = resp.Content
			logger.Debug("Vision pre-flight cached description",
				"url", ref.URL,
				"desc_len", len(resp.Content),
			)
		} else {
			ref.Description = "[image analysis returned empty]"
		}
	}

	return nil
}
