package llm

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// ContentPart is one block of a multimodal message. At least one of Text or
// ImageURL is non-empty. When Description is populated on ImageURL, downstream
// code MAY substitute the description text for the image bytes (see vision
// cache policy in the agent pre-flight).
type ContentPart struct {
	Type     string    `json:"type"` // "text" | "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageRef `json:"image_url,omitempty"`
}

// ImageRef references a stored upload. URL is always populated; the daemon
// rewrites it to a data URL before sending to the LLM. Description is the
// cached vision-model description (populated lazily after first analysis).
type ImageRef struct {
	URL         string `json:"url"`                   // "file://<sha256>.<ext>" or "data:..."
	Description string `json:"description,omitempty"` // Cached vision description
	MIMEType    string `json:"mime_type,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// ContentFromParts synthesizes a flat text string from a slice of content parts.
// When useDescription is true and an image part has a Description, the description
// is substituted as "[image: <description>]". Otherwise the URL is used.
// Used by FTS5 search, summarization, context compaction, memory injection,
// and the main agent turn after pre-flight has cached the description.
func ContentFromParts(parts []ContentPart, useDescription bool) string {
	if len(parts) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString("\n")
		}
		switch p.Type {
		case "text":
			sb.WriteString(p.Text)
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			if useDescription && p.ImageURL.Description != "" {
				sb.WriteString(fmt.Sprintf("[image: %s]", p.ImageURL.Description))
			} else {
				sb.WriteString(fmt.Sprintf("[image: %s]", p.ImageURL.URL))
			}
		}
	}
	return sb.String()
}

// HasImageParts returns true if any part in the slice is an image_url type.
func HasImageParts(parts []ContentPart) bool {
	for _, p := range parts {
		if p.Type == "image_url" && p.ImageURL != nil {
			return true
		}
	}
	return false
}

// HasUndescribedImages returns true if any image part lacks a Description.
func HasUndescribedImages(parts []ContentPart) bool {
	for _, p := range parts {
		if p.Type == "image_url" && p.ImageURL != nil && p.ImageURL.Description == "" {
			return true
		}
	}
	return false
}

// resolveImageURL converts a file:// reference to a data: URL by loading
// bytes from the upload store. If the URL is already a data: URL, it passes
// through unchanged. If store is nil and the URL is file://, an error is returned.
func resolveImageURL(url string, store UploadStore) (string, error) {
	if strings.HasPrefix(url, "data:") {
		return url, nil
	}
	if strings.HasPrefix(url, "file://") {
		if store == nil {
			return "", fmt.Errorf("upload store not configured: cannot resolve %q", url)
		}
		// Extract ID from file://<id> format
		id := strings.TrimPrefix(url, "file://")
		ctx := context.Background()
		data, mimeType, err := store.Load(ctx, id)
		if err != nil {
			return "", fmt.Errorf("failed to load upload %q: %w", id, err)
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
	}
	// Pass through http(s) URLs and other formats
	return url, nil
}

// parseDataURL extracts the MIME type and base64 data from a data: URL.
func parseDataURL(dataURL string) (mimeType string, data string) {
	// Format: data:<mime>;base64,<data>
	if !strings.HasPrefix(dataURL, "data:") {
		return "image/png", dataURL // fallback
	}
	rest := strings.TrimPrefix(dataURL, "data:")
	semicolonIdx := strings.Index(rest, ";")
	commaIdx := strings.Index(rest, ",")
	if semicolonIdx < 0 || commaIdx < 0 || semicolonIdx > commaIdx {
		return "image/png", rest
	}
	mimeType = rest[:semicolonIdx]
	data = rest[commaIdx+1:]
	return mimeType, data
}
