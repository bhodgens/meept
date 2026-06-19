# Vision Model & Multimodal Image Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add vision model support with multimodal image attachments — file-store uploads, Parts-based message format, description-as-cache vision pipeline, and full Flutter/TUI attachment UX.

**Architecture:** Images are uploaded to a file store (`~/.meept/uploads/`) with SHA dedup, referenced by ID in `ChatMessage.Parts[]`. A vision pre-flight step generates a cached description before the main agent turn. Provider serializers (Anthropic + OpenAI) emit native image blocks when description is empty, text substitution when cached. Flutter gets a redesigned input area with attachment button, chip display, drag-drop, and paste support.

**Tech Stack:** Go 1.24+ (daemon), Flutter/Dart (GUI), Bubble Tea (TUI), SQLite (session + uploads persistence), OpenAI/Anthropic vision APIs

**Spec:** `docs/superpowers/specs/2026-06-18-vision-model-design.md`

---

## File Structure

### New files

| File | Responsibility |
|------|---------------|
| `internal/services/upload_service.go` | UploadService: CRUD for uploads, SHA dedup, MIME validation, GC support |
| `internal/services/upload_service_test.go` | UploadService unit tests |
| `internal/agent/vision_preflight.go` | Vision pre-flight: detect undescribed images, call vision model, cache description |
| `internal/agent/vision_preflight_test.go` | Vision pre-flight unit tests |
| `internal/comm/http/upload_handlers.go` | HTTP handlers for `/api/v1/uploads` endpoints |
| `internal/llm/multimodal_test.go` | ContentPart serialization tests |
| `internal/llm/content_parts.go` | ContentPart type, ImageRef type, ContentFromParts helper |

### Modified files

| File | Lines to modify | What changes |
|------|-----------------|-------------|
| `internal/llm/models.go:22-62` | ChatMessage struct + ToOpenAIDict | Add `Parts` field, multimodal serialization |
| `internal/llm/interface.go` | After line 75 | Add `UploadStore` interface |
| `internal/llm/anthropic.go:432-654` | anthropicContent struct + buildRequest | Add image source fields, partsToAnthropicContent |
| `internal/llm/client.go:110,1291-1303` | Client struct + SwitchModel | Add uploadStore field, WithUploadStore option |
| `internal/session/store.go:10-22` | Message struct | Add Parts field |
| `internal/session/store_sqlite.go:95-128,703-802` | Schema + SaveMessages + GetMessages | Add parts column, serialize/deserialize |
| `internal/session/session.go:286-302` | MemoryStore.SaveMessages | Pass through Parts |
| `internal/services/service.go:31-55` | ServiceRegistry | Add Upload field |
| `internal/services/chat_service.go:28-32` | ChatRequest | Add Parts field |
| `internal/comm/http/server.go:886-934` | setupRESTRoutes | Register upload endpoints |
| `internal/comm/http/api_handlers.go:67-90` | handleChat | Forward Parts from request |
| `internal/config/schema.go:258-265` | DaemonConfig | Add Uploads sub-config |
| `internal/daemon/components.go` | After services init | Wire UploadService |
| `internal/tui/models/chat.go:146-148,1572-1596,2687-2735` | attachments + doSendMessage + detectAndAttachFile | Upload image files, build Parts on send |
| `ui/flutter_ui/lib/models/api_models.dart:174-221` | ChatMessage freezed model | Add parts field |
| `ui/flutter_ui/lib/services/sdk_client.dart` | ChatRequest + upload method | Add parts, uploadFile method |
| `ui/flutter_ui/lib/features/chat/chat_input.dart` | Full input area | Redesign for multimodal |
| `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart` | Message rendering | Render image part chips |

---

## Task 1: ContentPart and ImageRef types

**Files:**
- Create: `internal/llm/content_parts.go`
- Test: `internal/llm/multimodal_test.go`

- [ ] **Step 1: Write failing test for ContentPart types and ContentFromParts**

Create `internal/llm/multimodal_test.go`:

```go
package llm

import (
	"encoding/json"
	"testing"
)

func TestContentPartTextJSON(t *testing.T) {
	p := ContentPart{Type: "text", Text: "hello world"}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got ContentPart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Type != "text" || got.Text != "hello world" {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}

func TestContentPartImageJSON(t *testing.T) {
	p := ContentPart{
		Type: "image_url",
		ImageURL: &ImageRef{
			URL:      "file://abc123.png",
			MIMEType: "image/png",
			Width:    800,
			Height:   600,
		},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got ContentPart
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.ImageURL == nil || got.ImageURL.URL != "file://abc123.png" {
		t.Errorf("image roundtrip mismatch: got %+v", got)
	}
}

func TestContentFromPartsWithDescription(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: "What is this?"},
		{Type: "image_url", ImageURL: &ImageRef{
			URL:         "file://abc.png",
			Description: "A red circle on white background",
		}},
	}
	result := ContentFromParts(parts, true)
	expected := "What is this?\n[image: A red circle on white background]"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestContentFromPartsWithoutDescription(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: "Check this"},
		{Type: "image_url", ImageURL: &ImageRef{URL: "file://abc.png"}},
	}
	result := ContentFromParts(parts, false)
	expected := "Check this\n[image: file://abc.png]"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestContentFromPartsTextOnly(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: "just text"},
	}
	result := ContentFromParts(parts, true)
	if result != "just text" {
		t.Errorf("expected %q, got %q", "just text", result)
	}
}

func TestContentFromPartsEmpty(t *testing.T) {
	result := ContentFromParts(nil, true)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestContentPart -v`
Expected: FAIL — `undefined: ContentPart`

- [ ] **Step 3: Create ContentPart types and ContentFromParts helper**

Create `internal/llm/content_parts.go`:

```go
package llm

import (
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestContentPart -v`
Expected: PASS

- [ ] **Step 5: Run ContentFromParts tests**

Run: `go test ./internal/llm/ -run TestContentFromParts -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/llm/content_parts.go internal/llm/multimodal_test.go
git commit -m "feat(llm): add ContentPart, ImageRef types and ContentFromParts helper"
```

---

## Task 2: Add Parts to ChatMessage and update OpenAI serialization

**Files:**
- Modify: `internal/llm/models.go` (ChatMessage struct at line 22, ToOpenAIDict at line 43)
- Test: `internal/llm/multimodal_test.go` (append)

- [ ] **Step 1: Write failing test for ChatMessage with Parts**

Append to `internal/llm/multimodal_test.go`:

```go
func TestChatMessageWithPartsOpenAIDict(t *testing.T) {
	msg := ChatMessage{
		Role: RoleUser,
		Parts: []ContentPart{
			{Type: "text", Text: "What's in this image?"},
			{Type: "image_url", ImageURL: &ImageRef{
				URL:      "file://abc123.png",
				MIMEType: "image/png",
			}},
		},
	}
	dict := msg.ToOpenAIDict()

	role, ok := dict["role"].(string)
	if !ok || role != "user" {
		t.Fatalf("expected role 'user', got %v", dict["role"])
	}
	content, ok := dict["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected content to be []map[string]any, got %T", dict["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	if content[0]["type"] != "text" || content[0]["text"] != "What's in this image?" {
		t.Errorf("text part mismatch: %+v", content[0])
	}
	if content[1]["type"] != "image_url" {
		t.Errorf("expected image_url type, got %v", content[1]["type"])
	}
}

func TestChatMessageWithDescribedImageOpenAIDict(t *testing.T) {
	msg := ChatMessage{
		Role: RoleUser,
		Parts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageRef{
				URL:         "file://abc.png",
				Description: "A blue square",
			}},
		},
	}
	dict := msg.ToOpenAIDict()
	content := dict["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content))
	}
	// Described images substitute as text
	if content[0]["type"] != "text" {
		t.Errorf("expected text substitution, got %v", content[0]["type"])
	}
	if content[0]["text"] != "[image: A blue square]" {
		t.Errorf("expected description text, got %v", content[0]["text"])
	}
}

func TestChatMessageWithoutPartsOpenAIDict(t *testing.T) {
	msg := ChatMessage{
		Role:    RoleUser,
		Content: "plain text message",
	}
	dict := msg.ToOpenAIDict()
	content, ok := dict["content"].(string)
	if !ok || content != "plain text message" {
		t.Errorf("expected string content, got %v", dict["content"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestChatMessageWith -v`
Expected: FAIL — `Parts` field doesn't exist on ChatMessage

- [ ] **Step 3: Add Parts field to ChatMessage**

In `internal/llm/models.go`, modify the ChatMessage struct (line 22) to add the `Parts` field after `Content`:

```go
type ChatMessage struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Parts      []ContentPart `json:"parts,omitempty"` // Non-empty => takes precedence for LLM serialization
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	IsToolError bool      `json:"-"`
	SummaryLevel int      `json:"-"`
	Critical bool         `json:"-"`
}
```

- [ ] **Step 4: Update ToOpenAIDict for multimodal serialization**

In `internal/llm/models.go`, replace `ToOpenAIDict` (line 43) with:

```go
func (m *ChatMessage) ToOpenAIDict() map[string]any {
	msg := map[string]any{
		"role": string(m.Role),
	}
	if len(m.Parts) > 0 {
		content := make([]map[string]any, 0, len(m.Parts))
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				content = append(content, map[string]any{
					"type": "text",
					"text": p.Text,
				})
			case "image_url":
				if p.ImageURL == nil {
					continue
				}
				if p.ImageURL.Description != "" {
					content = append(content, map[string]any{
						"type": "text",
						"text": fmt.Sprintf("[image: %s]", p.ImageURL.Description),
					})
				} else {
					content = append(content, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": p.ImageURL.URL,
						},
					})
				}
			}
		}
		msg["content"] = content
	} else {
		msg["content"] = m.Content
	}
	if m.Name != "" {
		msg["name"] = m.Name
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]map[string]any, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			calls[i] = tc.ToOpenAIDict()
		}
		msg["tool_calls"] = calls
	}
	if m.ToolCallID != "" {
		msg["tool_call_id"] = m.ToolCallID
	}
	return msg
}
```

Add `"fmt"` to imports if not already present.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestChatMessage -v`
Expected: PASS

- [ ] **Step 6: Verify full package compiles**

Run: `go build ./internal/llm/...`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add internal/llm/models.go internal/llm/multimodal_test.go
git commit -m "feat(llm): add Parts field to ChatMessage, multimodal OpenAI serialization"
```

---

## Task 3: UploadStore interface and Client/Anthropic integration

**Files:**
- Modify: `internal/llm/interface.go` (add UploadStore interface after line 75)
- Modify: `internal/llm/client.go` (add uploadStore field, WithUploadStore option)
- Test: `internal/llm/multimodal_test.go` (append)

- [ ] **Step 1: Write failing test for resolveImageURL helper**

Append to `internal/llm/multimodal_test.go`:

```go
func TestResolveImageURLWithDataScheme(t *testing.T) {
	// Already a data URL — should pass through unchanged
	url := "data:image/png;base64,iVBORw0KGgo="
	resolved, err := resolveImageURL(url, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != url {
		t.Errorf("expected pass-through, got %q", resolved)
	}
}

func TestResolveImageURLWithUploadStore(t *testing.T) {
	store := &mockUploadStore{
		data:     []byte{0x89, 0x50, 0x4E, 0x47}, // PNG magic bytes
		mimeType: "image/png",
	}
	// file:// URL — needs upload store to load bytes
	resolved, err := resolveImageURL("file://abc123.png", store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(resolved, "data:image/png;base64,") {
		t.Errorf("expected data URL, got %q", resolved)
	}
}

type mockUploadStore struct {
	data     []byte
	mimeType string
	err      error
}

func (m *mockUploadStore) Load(ctx context.Context, id string) ([]byte, string, error) {
	if m.err != nil {
		return nil, "", m.err
	}
	return m.data, m.mimeType, nil
}
```

Add `"context"` and `"strings"` to test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestResolveImageURL -v`
Expected: FAIL — `undefined: resolveImageURL`, `undefined: UploadStore`

- [ ] **Step 3: Add UploadStore interface**

In `internal/llm/interface.go`, append after the `TokenResolver` interface (after line 75):

```go
// UploadStore provides access to uploaded file storage. The LLM client uses it
// to resolve file:// references into base64 data URLs for provider APIs.
type UploadStore interface {
	Load(ctx context.Context, id string) (data []byte, mimeType string, err error)
}
```

Add `"context"` to the imports in `interface.go`.

- [ ] **Step 4: Add resolveImageURL helper**

In `internal/llm/content_parts.go`, append:

```go
import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

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
```

Note: consolidate the imports at the top of `content_parts.go` — merge the two import blocks.

- [ ] **Step 5: Add uploadStore to Client and WithUploadStore option**

In `internal/llm/client.go`, add a field to the `Client` struct. Find the struct definition and add:

```go
	uploadStore UploadStore
```

Add the option function near the other options (after line 110 area):

```go
// WithUploadStore sets the upload store for resolving image file references.
func WithUploadStore(store UploadStore) ClientOption {
	return func(c *Client) {
		if store != nil {
			c.uploadStore = store
		}
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestResolveImageURL -v`
Expected: PASS

- [ ] **Step 7: Verify full build**

Run: `go build ./internal/llm/...`
Expected: No errors

- [ ] **Step 8: Commit**

```bash
git add internal/llm/interface.go internal/llm/content_parts.go internal/llm/client.go internal/llm/multimodal_test.go
git commit -m "feat(llm): add UploadStore interface, resolveImageURL helper, WithUploadStore option"
```

---

## Task 4: Anthropic multimodal serialization

**Files:**
- Modify: `internal/llm/anthropic.go` (anthropicContent struct at line 437, buildRequest at line 534)

- [ ] **Step 1: Write failing test for Anthropic image serialization**

Append to `internal/llm/multimodal_test.go`:

```go
func TestAnthropicPartsToContentTextOnly(t *testing.T) {
	c := NewAnthropicClient(&ModelConfig{ModelID: "test"})
	parts := []ContentPart{
		{Type: "text", Text: "hello"},
	}
	content := c.partsToAnthropicContent(parts, nil)
	if len(content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(content))
	}
	if content[0].Type != "text" || content[0].Text != "hello" {
		t.Errorf("mismatch: %+v", content[0])
	}
}

func TestAnthropicPartsToContentImageWithDescription(t *testing.T) {
	c := NewAnthropicClient(&ModelConfig{ModelID: "test"})
	parts := []ContentPart{
		{Type: "image_url", ImageURL: &ImageRef{
			URL:         "file://abc.png",
			Description: "A cat sitting on a desk",
		}},
	}
	content := c.partsToAnthropicContent(parts, nil)
	if len(content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(content))
	}
	// Described images substitute as text
	if content[0].Type != "text" {
		t.Errorf("expected text substitution, got %s", content[0].Type)
	}
	if !strings.Contains(content[0].Text, "A cat sitting on a desk") {
		t.Errorf("expected description in text, got %q", content[0].Text)
	}
}

func TestAnthropicPartsToContentImageBytes(t *testing.T) {
	store := &mockUploadStore{
		data:     []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A},
		mimeType: "image/png",
	}
	c := NewAnthropicClient(&ModelConfig{ModelID: "test"}, WithAnthropicUploadStore(store))
	parts := []ContentPart{
		{Type: "image_url", ImageURL: &ImageRef{
			URL:      "file://abc.png",
			MIMEType: "image/png",
		}},
	}
	content := c.partsToAnthropicContent(parts, store)
	if len(content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(content))
	}
	if content[0].Type != "image" {
		t.Errorf("expected image type, got %s", content[0].Type)
	}
	if content[0].Source == nil {
		t.Fatal("expected non-nil Source")
	}
	if content[0].Source.MediaType != "image/png" {
		t.Errorf("expected png, got %s", content[0].Source.MediaType)
	}
	if content[0].Source.Type != "base64" {
		t.Errorf("expected base64, got %s", content[0].Source.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/ -run TestAnthropicParts -v`
Expected: FAIL — `c.partsToAnthropicContent undefined`

- [ ] **Step 3: Add image fields to anthropicContent and add anthropicImageSource**

In `internal/llm/anthropic.go`, modify the `anthropicContent` struct (line 437) to add image source:

```go
type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// For tool results
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"`
	// For tool use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// For images
	Source *anthropicImageSource `json:"source,omitempty"`
}

// anthropicImageSource holds base64-encoded image data for the Anthropic API.
type anthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", etc.
	Data      string `json:"data"`       // base64-encoded image bytes
}
```

- [ ] **Step 4: Add partsToAnthropicContent method and WithAnthropicUploadStore option**

Add the option after the existing `WithAnthropicTokenCache` (around line 96):

```go
// WithAnthropicUploadStore sets the upload store for resolving image file references.
func WithAnthropicUploadStore(store UploadStore) AnthropicClientOption {
	return func(c *AnthropicClient) {
		if store != nil {
			c.uploadStore = store
		}
	}
}
```

Add `uploadStore UploadStore` field to the `AnthropicClient` struct (line 39).

Add the method after `buildRequest` (after line 654):

```go
// partsToAnthropicContent converts ContentParts to Anthropic content blocks.
// When an image has a Description, it is substituted as text (cached vision result).
// When store is available and description is empty, image bytes are loaded and
// sent as an image source block.
func (c *AnthropicClient) partsToAnthropicContent(parts []ContentPart, store UploadStore) []anthropicContent {
	var out []anthropicContent
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, anthropicContent{
				Type: ContentTypeText,
				Text: p.Text,
			})
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			if p.ImageURL.Description != "" {
				out = append(out, anthropicContent{
					Type: ContentTypeText,
					Text: fmt.Sprintf("[image: %s]", p.ImageURL.Description),
				})
			} else {
				dataURL, err := resolveImageURL(p.ImageURL.URL, store)
				if err != nil {
					c.logger.Warn("Failed to resolve image URL", "url", p.ImageURL.URL, "error", err)
					out = append(out, anthropicContent{
						Type: ContentTypeText,
						Text: fmt.Sprintf("[image: unable to load %s]", p.ImageURL.URL),
					})
					continue
				}
				mimeType, data := parseDataURL(dataURL)
				out = append(out, anthropicContent{
					Type: "image",
					Source: &anthropicImageSource{
						Type:      "base64",
						MediaType: mimeType,
						Data:      data,
					},
				})
			}
		}
	}
	return out
}
```

- [ ] **Step 5: Update buildRequest to use Parts when present**

In `internal/llm/anthropic.go`, modify `buildRequest` (line 569) — the `RoleUser, RoleAssistant` case:

```go
		case RoleUser, RoleAssistant:
			msgIndexToAPIIndex[i] = len(apiMessages)
			if len(msg.Parts) > 0 {
				apiMessages = append(apiMessages, anthropicMessage{
					Role:    string(msg.Role),
					Content: c.partsToAnthropicContent(msg.Parts, c.uploadStore),
				})
			} else {
				apiMessages = append(apiMessages, anthropicMessage{
					Role: string(msg.Role),
					Content: []anthropicContent{{
						Type: ContentTypeText,
						Text: msg.Content,
					}},
				})
			}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/llm/ -run TestAnthropicParts -v`
Expected: PASS

- [ ] **Step 7: Verify full build**

Run: `go build ./internal/llm/...`
Expected: No errors

- [ ] **Step 8: Commit**

```bash
git add internal/llm/anthropic.go internal/llm/multimodal_test.go
git commit -m "feat(llm): add Anthropic multimodal image serialization"
```

---

## Task 5: Session Message Parts field and SQLite migration

**Files:**
- Modify: `internal/session/store.go` (Message struct at line 10)
- Modify: `internal/session/store_sqlite.go` (schema at line 95, migrations at line 112, SaveMessages at line 703, GetMessages at line 749)

- [ ] **Step 1: Add Parts to session.Message struct**

In `internal/session/store.go`, modify the `Message` struct (line 10) to add:

```go
import (
	"context"
	"errors"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

type Message struct {
	ID         int64               `json:"id"`
	SessionID  string              `json:"session_id"`
	ParentID   *int64              `json:"parent_id,omitempty"`
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	Parts      []llm.ContentPart   `json:"parts,omitempty"` // NEW
	Timestamp  time.Time           `json:"timestamp"`
	EntryType  string              `json:"entry_type"`
	BranchID   string              `json:"branch_id"`
	Model      string              `json:"model,omitempty"`
	Name       string              `json:"name,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}
```

- [ ] **Step 2: Add SQLite column migration**

In `internal/session/store_sqlite.go`, after line 123 (after the `tool_call_id` migration), add:

```go
	// Add parts column for multimodal content (JSON array of ContentPart)
	s.migrationAddColumn("ALTER TABLE session_messages ADD COLUMN parts TEXT", "parts")
```

- [ ] **Step 3: Update SaveMessages to serialize Parts**

In `internal/session/store_sqlite.go`, modify `SaveMessages` (line 717-739). The INSERT statement and Exec call need a new `parts` parameter:

```go
	stmt, err := tx.Prepare(`
		INSERT INTO session_messages (session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id, parts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, msg := range messages {
		var entryType, branchID string
		if msg.EntryType != "" {
			entryType = msg.EntryType
		} else {
			entryType = KeyMessage
		}
		if msg.BranchID != "" {
			branchID = msg.BranchID
		} else {
			branchID = BranchMain
		}

		var partsJSON interface{}
		if len(msg.Parts) > 0 {
			partsJSON, err = json.Marshal(msg.Parts)
			if err != nil {
				return fmt.Errorf("failed to marshal message parts: %w", err)
			}
		}

		_, err := stmt.Exec(sessionID, msg.Role, msg.Content, msg.Timestamp.Format(time.RFC3339),
			msg.ParentID, entryType, branchID, msg.Model, msg.Name, msg.ToolCallID, partsJSON) //nolint:mutexio // mutex serializes sqlite connection access
		if err != nil {
			return fmt.Errorf("failed to insert message: %w", err)
		}
	}
```

Ensure `"encoding/json"` is in the imports of `store_sqlite.go`.

- [ ] **Step 4: Update GetMessages to deserialize Parts**

In `internal/session/store_sqlite.go`, modify `GetMessages` (line 753-794). Add `parts` to the SELECT and scan:

Change the query to:
```sql
		SELECT id, session_id, role, content, timestamp, parent_id, entry_type, branch_id, model, name, tool_call_id, parts
		FROM session_messages
		WHERE session_id = ?
		ORDER BY id
		LIMIT ? OFFSET ?
```

Add scanning after the existing fields:
```go
		var partsJSON sql.NullString
		if err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &msg.Content, &ts, &parentID, &entryType, &branchID, &model, &name, &toolCallID, &partsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		// ... existing field assignment ...
		if partsJSON.Valid && partsJSON.String != "" {
			if err := json.Unmarshal([]byte(partsJSON.String), &msg.Parts); err != nil {
				s.logger.Debug("failed to unmarshal parts", "error", err)
			}
		}
```

- [ ] **Step 5: Verify build compiles**

Run: `go build ./internal/session/...`
Expected: No errors

- [ ] **Step 6: Run existing session tests to verify no regressions**

Run: `go test ./internal/session/... -v -count=1`
Expected: PASS (existing tests should still pass since Parts is omitempty)

- [ ] **Step 7: Commit**

```bash
git add internal/session/store.go internal/session/store_sqlite.go
git commit -m "feat(session): add Parts column to session_messages, serialize/deserialize multimodal"
```

---

## Task 6: UploadService — storage, CRUD, and dedup

**Files:**
- Create: `internal/services/upload_service.go`
- Create: `internal/services/upload_service_test.go`

- [ ] **Step 1: Write failing test for UploadService**

Create `internal/services/upload_service_test.go`:

```go
package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadServiceUpload(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	// Create a tiny test PNG
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}

	upload, err := svc.Upload(t.Context(), bytes.NewReader(buf.Bytes()), "test.png", "image/png")
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// Verify ID is SHA-256 hash of content
	hash := sha256.Sum256(buf.Bytes())
	expectedID := hex.EncodeToString(hash[:])
	if upload.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, upload.ID)
	}

	if upload.MIMEType != "image/png" {
		t.Errorf("expected MIME image/png, got %s", upload.MIMEType)
	}
	if upload.Width != 4 || upload.Height != 4 {
		t.Errorf("expected 4x4, got %dx%d", upload.Width, upload.Height)
	}

	// File should exist on disk
	if _, err := os.Stat(upload.Path); err != nil {
		t.Errorf("file not on disk: %v", err)
	}
}

func TestUploadServiceDedup(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

	first, err := svc.Upload(t.Context(), bytes.NewReader(data), "a.png", "image/png")
	if err != nil {
		t.Fatalf("first upload failed: %v", err)
	}
	second, err := svc.Upload(t.Context(), bytes.NewReader(data), "b.png", "image/png")
	if err != nil {
		t.Fatalf("second upload failed: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("dedup failed: %s != %s", first.ID, second.ID)
	}
}

func TestUploadServiceRejectDisallowedMIME(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	_, err := svc.Upload(t.Context(), bytes.NewReader([]byte("data")), "test.bmp", "image/bmp")
	if err == nil {
		t.Fatal("expected error for disallowed MIME type")
	}
}

func TestUploadServiceLoad(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	upload, err := svc.Upload(t.Context(), bytes.NewReader(data), "test.png", "image/png")
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	loaded, mime, err := svc.Load(t.Context(), upload.ID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !bytes.Equal(loaded, data) {
		t.Errorf("loaded data mismatch")
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}
}

func TestUploadServiceGC(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUploadService(tmpDir, 20, []string{"image/png"})

	// Create a file directly (simulate orphaned upload)
	path := filepath.Join(tmpDir, "orphan.png")
	if err := os.WriteFile(path, []byte("orphan"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// GC should not delete files newer than retention
	deleted, err := svc.GCSweep(7 * 24 * 3600) // 7 days
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("expected 0 deletions for new files, got %d", len(deleted))
	}

	// Verify file still exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/services/ -run TestUpload -v`
Expected: FAIL — `undefined: NewUploadService`

- [ ] **Step 3: Implement UploadService**

Create `internal/services/upload_service.go`:

```go
package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

// Upload describes a stored file upload.
type Upload struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	RefCount  int       `json:"ref_count"`
}

// UploadService manages file uploads with SHA-256 dedup and refcounting.
type UploadService struct {
	mu            sync.Mutex
	dir           string
	maxSizeBytes  int64
	allowedTypes  map[string]bool
	dbPath        string
	logger        *slog.Logger
}

// NewUploadService creates a new UploadService.
// dir is the storage directory (e.g. ~/.meept/uploads).
// maxSizeMB is the maximum upload size in megabytes.
// allowedTypes is the list of accepted MIME types.
func NewUploadService(dir string, maxSizeMB int, allowedTypes []string) *UploadService {
	allowed := make(map[string]bool)
	for _, t := range allowedTypes {
		allowed[t] = true
	}
	return &UploadService{
		dir:          dir,
		maxSizeBytes: int64(maxSizeMB) * 1024 * 1024,
		allowedTypes: allowed,
		dbPath:       filepath.Join(dir, "uploads.json"),
		logger:       slog.Default(),
	}
}

// Upload stores a file, returning the upload descriptor. If the content hash
// matches an existing upload, the existing record is returned (dedup).
func (s *UploadService) Upload(ctx context.Context, reader io.Reader, filename string, mimeType string) (*Upload, error) {
	if !s.allowedTypes[mimeType] {
		return nil, fmt.Errorf("MIME type %q not allowed; accepted: %v", mimeType, s.allowedTypesList())
	}

	// Read all bytes (limited by maxSizeBytes)
	data, err := io.ReadAll(io.LimitReader(reader, s.maxSizeBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read upload data: %w", err)
	}
	if int64(len(data)) > s.maxSizeBytes {
		return nil, fmt.Errorf("upload exceeds maximum size of %d bytes", s.maxSizeBytes)
	}

	// Compute SHA-256 hash
	hash := sha256.Sum256(data)
	id := hex.EncodeToString(hash[:])

	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure storage dir exists
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	// Check for existing upload (dedup)
	records := s.loadRecords()
	if existing, ok := records[id]; ok {
		// Verify file exists on disk
		if _, err := os.Stat(existing.Path); err == nil {
			existing.RefCount++
			records[id] = existing
			_ = s.saveRecords(records)
			return &existing, nil
		}
		// File missing — fall through to re-store
	}

	// Determine extension from filename
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = mimeToExt(mimeType)
	}

	path := filepath.Join(s.dir, id+ext)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write upload file: %w", err)
	}

	// Extract image dimensions
	width, height := imageDimensions(data, mimeType)

	upload := Upload{
		ID:        id,
		Path:      path,
		MimeType:  mimeType,
		SizeBytes: int64(len(data)),
		Width:     width,
		Height:    height,
		CreatedAt: time.Now().UTC(),
		RefCount:  1,
	}

	records[id] = upload
	if err := s.saveRecords(records); err != nil {
		s.logger.Warn("failed to save upload records", "error", err)
	}

	return &upload, nil
}

// Load returns the raw bytes and MIME type for an upload by ID.
func (s *UploadService) Load(ctx context.Context, id string) ([]byte, string, error) {
	s.mu.Lock()
	records := s.loadRecords()
	upload, ok := records[id]
	s.mu.Unlock()

	if !ok {
		return nil, "", fmt.Errorf("upload not found: %s", id)
	}

	data, err := os.ReadFile(upload.Path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read upload file: %w", err)
	}
	return data, upload.MimeType, nil
}

// Get returns upload metadata by ID.
func (s *UploadService) Get(ctx context.Context, id string) (*Upload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.loadRecords()
	upload, ok := records[id]
	if !ok {
		return nil, fmt.Errorf("upload not found: %s", id)
	}
	return &upload, nil
}

// Release decrements the refcount for an upload.
func (s *UploadService) Release(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.loadRecords()
	upload, ok := records[id]
	if !ok {
		return fmt.Errorf("upload not found: %s", id)
	}
	if upload.RefCount > 0 {
		upload.RefCount--
	}
	records[id] = upload
	return s.saveRecords(records)
}

// Acquire increments the refcount for an upload.
func (s *UploadService) Acquire(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.loadRecords()
	upload, ok := records[id]
	if !ok {
		return fmt.Errorf("upload not found: %s", id)
	}
	upload.RefCount++
	records[id] = upload
	return s.saveRecords(records)
}

// GCSweep deletes files with refcount=0 older than maxAge. Returns deleted IDs.
func (s *UploadService) GCSweep(maxAgeSeconds int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records := s.loadRecords()
	cutoff := time.Now().UTC().Add(-time.Duration(maxAgeSeconds) * time.Second)
	var deleted []string

	for id, upload := range records {
		if upload.RefCount > 0 {
			continue
		}
		if upload.CreatedAt.After(cutoff) {
			continue
		}
		// Delete file
		if err := os.Remove(upload.Path); err != nil && !os.IsNotExist(err) {
			s.logger.Warn("failed to delete upload file", "id", id, "error", err)
			continue
		}
		delete(records, id)
		deleted = append(deleted, id)
	}

	if err := s.saveRecords(records); err != nil {
		s.logger.Warn("failed to save records after GC", "error", err)
	}

	return deleted, nil
}

// loadRecords reads the uploads metadata from the JSON store.
func (s *UploadService) loadRecords() map[string]Upload {
	data, err := os.ReadFile(s.dbPath)
	if err != nil {
		return make(map[string]Upload)
	}
	var records map[string]Upload
	if err := json.Unmarshal(data, &records); err != nil {
		return make(map[string]Upload)
	}
	if records == nil {
		return make(map[string]Upload)
	}
	return records
}

// saveRecords writes the uploads metadata to the JSON store.
func (s *UploadService) saveRecords(records map[string]Upload) error {
	data, err := json.Marshal(records)
	if err != nil {
		return fmt.Errorf("failed to marshal records: %w", err)
	}
	return os.WriteFile(s.dbPath, data, 0644)
}

func (s *UploadService) allowedTypesList() string {
	types := make([]string, 0, len(s.allowedTypes))
	for t := range s.allowedTypes {
		types = append(types, t)
	}
	return strings.Join(types, ", ")
}

// imageDimensions extracts width and height from image bytes.
func imageDimensions(data []byte, mimeType string) (width, height int) {
	cfg, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// mimeToExt returns a file extension for a MIME type.
func mimeToExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}
```

Note: `golang.org/x/image/webp` may need to be added via `go get golang.org/x/image/webp`. If it's not available, remove the webp import and the `_ "golang.org/x/image/webp"` blank import — WebP dimension detection will silently fail (returns 0x0), which is non-blocking.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/services/ -run TestUpload -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/services/upload_service.go internal/services/upload_service_test.go
git commit -m "feat(services): add UploadService with SHA dedup, MIME validation, GC support"
```

---

## Task 7: HTTP upload endpoints

**Files:**
- Create: `internal/comm/http/upload_handlers.go`
- Modify: `internal/comm/http/server.go:887` (register routes in setupRESTRoutes)
- Modify: `internal/services/service.go:31` (add Upload to ServiceRegistry)

- [ ] **Step 1: Add Upload to ServiceRegistry**

In `internal/services/service.go`, add to `ServiceRegistry` (line 31):

```go
type ServiceRegistry struct {
	Chat         *ChatService
	Memory       *MemoryService
	Task         *TaskService
	Queue        *QueueService
	Session      *SessionService
	SessionStore session.Store
	Worker       *WorkerService
	Pipeline     *PipelineService
	Skills       *SkillsService
	SelfImprove  *SelfImproveService
	Cache        *CacheService
	Security     *SecurityService
	Scheduler    *SchedulerService
	Bus          *BusService
	Templates    *TemplatesService
	Daemon       *DaemonService
	Model        *ModelService
	Calendar     *CalendarService
	Terminal     *TerminalService
	Project      *ProjectService
	Plan         *PlanService
	Runtime      *RuntimeService
	Search       *SearchService
	Upload       *UploadService // NEW
}
```

- [ ] **Step 2: Add upload handlers**

Create `internal/comm/http/upload_handlers.go`:

```go
package http

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/caimlas/meept/internal/services"
)

// handleUploadCreate handles POST /api/v1/uploads.
// Accepts multipart/form-data with a "file" field, or JSON with base64 data.
func (s *Server) handleUploadCreate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		s.handleMultipartUpload(w, r)
		return
	}

	// JSON with base64-encoded data
	if strings.HasPrefix(contentType, "application/json") {
		s.handleJSONUpload(w, r)
		return
	}

	s.writeError(w, http.StatusUnsupportedMediaType, "expected multipart/form-data or application/json")
}

func (s *Server) handleMultipartUpload(w http.ResponseWriter, r *http.Request) {
	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, s.services.Upload.MaxSizeBytes())

	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB buffer
		s.writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	upload, err := s.services.Upload.Upload(r.Context(), file, header.Filename, mimeType)
	if err != nil {
		if strings.Contains(err.Error(), "not allowed") {
			s.writeError(w, http.StatusUnsupportedMediaType, err.Error())
		} else if strings.Contains(err.Error(), "exceeds maximum") {
			s.writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		} else {
			s.writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"uploads": []any{upload},
	})
}

func (s *Server) handleJSONUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data     string `json:"data"`      // base64-encoded
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
	}
	if !s.readJSON(w, r, &req) {
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid base64 data")
		return
	}

	upload, err := s.services.Upload.Upload(r.Context(), strings.NewReader(string(data)), req.Filename, req.MimeType)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"uploads": []any{upload},
	})
}

// handleUploadGet handles GET /api/v1/uploads/{id}.
// Returns raw file bytes with the correct Content-Type.
func (s *Server) handleUploadGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	data, mimeType, err := s.services.Upload.Load(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "upload not found")
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
}

// handleUploadMetadata handles GET /api/v1/uploads/{id}/metadata.
func (s *Server) handleUploadMetadata(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	upload, err := s.services.Upload.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "upload not found")
		return
	}

	s.writeJSON(w, http.StatusOK, upload)
}

// handleUploadDelete handles DELETE /api/v1/uploads/{id}.
func (s *Server) handleUploadDelete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	if err := s.services.Upload.Release(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "unreferenced"})
}

var _ = io.EOF // ensure io is used
var _ = json.Marshal
var _ = slog.Default
```

- [ ] **Step 3: Add MaxSizeBytes method to UploadService**

In `internal/services/upload_service.go`, add:

```go
// MaxSizeBytes returns the configured maximum upload size.
func (s *UploadService) MaxSizeBytes() int64 {
	return s.maxSizeBytes
}
```

- [ ] **Step 4: Register upload routes**

In `internal/comm/http/server.go`, in `setupRESTRoutes` (line 887), add after the chat endpoints (around line 934):

```go
	// Upload endpoints
	mux.HandleFunc("POST /api/v1/uploads", s.handleUploadCreate)
	mux.HandleFunc("GET /api/v1/uploads/{id}", s.handleUploadGet)
	mux.HandleFunc("GET /api/v1/uploads/{id}/metadata", s.handleUploadMetadata)
	mux.HandleFunc("DELETE /api/v1/uploads/{id}", s.handleUploadDelete)
```

- [ ] **Step 5: Verify build compiles**

Run: `go build ./internal/comm/http/... && go build ./internal/services/...`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add internal/comm/http/upload_handlers.go internal/comm/http/server.go internal/services/service.go internal/services/upload_service.go
git commit -m "feat(http): add upload endpoints (POST/GET/DELETE /api/v1/uploads)"
```

---

## Task 8: Add Parts to ChatRequest and forward to agent

**Files:**
- Modify: `internal/services/chat_service.go:28-32` (ChatRequest struct)
- Modify: `internal/comm/http/api_handlers.go:69-90` (handleChat)

- [ ] **Step 1: Add Parts to ChatRequest**

In `internal/services/chat_service.go`, modify `ChatRequest` (line 28):

```go
import (
	"github.com/caimlas/meept/internal/llm"
	// ... existing imports
)

type ChatRequest struct {
	Message        string              `json:"message"`
	Parts          []llm.ContentPart   `json:"parts,omitempty"` // NEW
	ConversationID string              `json:"conversation_id"`
	AgentID        string              `json:"agent_id,omitempty"`
}
```

- [ ] **Step 2: Wire UploadService in daemon components**

In `internal/daemon/components.go`, find where services are initialized (look for `ServiceRegistry` or `services.Config`). Add the UploadService:

```go
	// Create upload service
	uploadDir := filepath.Join(d.config.Daemon.DataDir, "uploads")
	uploadSvc := services.NewUploadService(uploadDir, 20, []string{"image/png", "image/jpeg", "image/gif", "image/webp"})
```

Then add to the service registry wiring:
```go
	registry.Upload = uploadSvc
```

- [ ] **Step 3: Verify build compiles**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/services/chat_service.go internal/daemon/components.go
git commit -m "feat(services): add Parts to ChatRequest, wire UploadService in daemon"
```

---

## Task 9: Vision pre-flight

**Files:**
- Create: `internal/agent/vision_preflight.go`
- Create: `internal/agent/vision_preflight_test.go`

- [ ] **Step 1: Write failing test for vision pre-flight**

Create `internal/agent/vision_preflight_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestVisionPreflightNoImages(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "hello"},
	}
	result := needsVisionPreflight(messages)
	if result {
		t.Error("expected false for text-only messages")
	}
}

func TestVisionPreflightWithUndescribedImage(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: &llm.ImageRef{URL: "file://abc.png"}},
		}},
	}
	result := needsVisionPreflight(messages)
	if !result {
		t.Error("expected true for undescribed image")
	}
}

func TestVisionPreflightWithDescribedImage(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: &llm.ImageRef{
				URL:         "file://abc.png",
				Description: "already described",
			}},
		}},
	}
	result := needsVisionPreflight(messages)
	if result {
		t.Error("expected false for already-described image")
	}
}

func TestVisionPreflightCollectImageRefs(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "text", Text: "check this"},
			{Type: "image_url", ImageURL: &llm.ImageRef{URL: "file://a.png"}},
			{Type: "image_url", ImageURL: &llm.ImageRef{URL: "file://b.png"}},
		}},
	}
	refs := collectUndescribedImageRefs(messages)
	if len(refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(refs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestVisionPreflight -v`
Expected: FAIL — `undefined: needsVisionPreflight`

- [ ] **Step 3: Implement vision pre-flight helpers**

Create `internal/agent/vision_preflight.go`:

```go
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
				refs = append(refs, p.ImageRef)
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
				toDescribe = append(toDescribe, p.ImageURL.URL, p.ImageRef)
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
```

Fix the `collectUndescribedImageRefs` to return `[]*llm.ImageRef` correctly and fix the `toDescribe` collection in `runVisionPreflight` — the inner loop should collect pointers properly:

```go
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
```

And in `runVisionPreflight`, the collection loop should be:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestVisionPreflight -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/vision_preflight.go internal/agent/vision_preflight_test.go
git commit -m "feat(agent): add vision pre-flight for image description caching"
```

---

## Task 10: Integrate vision pre-flight into AgentLoop

**Files:**
- Modify: `internal/agent/loop.go` (around line 1770-1795, before LLM call)

- [ ] **Step 1: Add upload store and vision model resolver to AgentLoop**

In `internal/agent/loop.go`, add fields to the `AgentLoop` struct:
```go
	uploadStore llm.UploadStore // for resolving image references
```

Add a setter:
```go
// SetUploadStore sets the upload store for image resolution.
func (l *AgentLoop) SetUploadStore(store llm.UploadStore) {
	if store != nil {
		l.uploadStore = store
	}
}
```

- [ ] **Step 2: Add pre-flight call before the main LLM turn**

In `internal/agent/loop.go`, in the main iteration loop (around line 1775, after `messages := conv.GetWindowedMessages(effectiveBudget)` and before the `chatOpts` building), add:

```go
		// Vision pre-flight: analyze undescribed images before the main turn
		if needsVisionPreflight(messages) && l.resolver != nil {
			// Find a vision-capable model
			visionModels := l.resolver.FindByCapabilities([]string{llm.CapImages})
			if len(visionModels) > 0 {
				visionClient := llm.NewClient(visionModels[0], llm.WithUploadStore(l.uploadStore))
				if err := runVisionPreflight(ctx, messages, visionClient, l.uploadStore, l.logger); err != nil {
					l.logger.Warn("Vision pre-flight completed with errors", "error", err)
				}
			} else {
				l.logger.Warn("Image in message but no vision-capable model configured")
			}
		}
```

- [ ] **Step 3: Verify build compiles**

Run: `go build ./internal/agent/...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/agent/loop.go
git commit -m "feat(agent): integrate vision pre-flight into AgentLoop before main LLM turn"
```

---

## Task 11: Flutter API models for multimodal

**Files:**
- Modify: `ui/flutter_ui/lib/models/api_models.dart` (add ChatMessagePart, ImageRef, update ChatMessage)
- Regenerate: `ui/flutter_ui/lib/models/api_models.freezed.dart`, `api_models.g.dart`

- [ ] **Step 1: Add ChatMessagePart and ImageRef classes**

In `ui/flutter_ui/lib/models/api_models.dart`, add before the ChatMessage class (around line 174):

```dart
// ===== Multimodal Content Part Models =====

/// One block of a multimodal message. Either text or imageUrl is non-null.
class ChatMessagePart {
  final String type; // 'text' | 'image_url'
  final String? text;
  final ImageRefData? imageUrl;

  const ChatMessagePart({required this.type, this.text, this.imageUrl});

  ChatMessagePart.text(String t)
      : type = 'text',
        text = t,
        imageUrl = null;

  ChatMessagePart.image(ImageRefData r)
      : type = 'image_url',
        text = null,
        imageUrl = r;

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{'type': type};
    if (type == 'text' && text != null) {
      json['text'] = text;
    } else if (type == 'image_url' && imageUrl != null) {
      json['image_url'] = imageUrl!.toJson();
    }
    return json;
  }

  factory ChatMessagePart.fromJson(Map<String, dynamic> json) {
    final type = json['type'] as String? ?? 'text';
    if (type == 'text') {
      return ChatMessagePart(type: type, text: json['text'] as String?);
    }
    final imgJson = json['image_url'] as Map<String, dynamic>?;
    return ChatMessagePart(
      type: type,
      imageUrl: imgJson != null ? ImageRefData.fromJson(imgJson) : null,
    );
  }
}

/// Image reference with optional cached description.
class ImageRefData {
  final String url;
  final String? description;
  final String? mimeType;
  final int? width;
  final int? height;

  const ImageRefData({
    required this.url,
    this.description,
    this.mimeType,
    this.width,
    this.height,
  });

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{'url': url};
    if (description != null) json['description'] = description;
    if (mimeType != null) json['mime_type'] = mimeType;
    if (width != null) json['width'] = width;
    if (height != null) json['height'] = height;
    return json;
  }

  factory ImageRefData.fromJson(Map<String, dynamic> json) {
    return ImageRefData(
      url: json['url'] as String? ?? '',
      description: json['description'] as String?,
      mimeType: json['mime_type'] as String?,
      width: json['width'] as int?,
      height: json['height'] as int?,
    );
  }
}
```

- [ ] **Step 2: Add parts field to ChatMessage freezed model**

In `ui/flutter_ui/lib/models/api_models.dart`, modify the `ChatMessage` factory (line 178):

```dart
@freezed
class ChatMessage with _$ChatMessage {
  const ChatMessage._();

  const factory ChatMessage({
    required String id,
    required String role,
    required String content,
    required DateTime timestamp,
    @JsonKey(name: 'session_id') String? sessionId,
    @JsonKey(name: 'tool_calls') List<String>? toolCalls,
    @Default([]) List<ChatMessagePart> parts,
  }) = _ChatMessage;
  // ... rest unchanged
}
```

- [ ] **Step 3: Add Attachment model for UI state**

In `ui/flutter_ui/lib/models/api_models.dart`, add:

```dart
/// UI-side representation of an uploaded attachment awaiting send.
class Attachment {
  final String uploadId;
  final String filename;
  final String mimeType;
  final int sizeBytes;

  const Attachment({
    required this.uploadId,
    required this.filename,
    required this.mimeType,
    required this.sizeBytes,
  });
}
```

- [ ] **Step 4: Regenerate freezed files**

Run:
```bash
cd ui/flutter_ui && dart run build_runner build --delete-conflicting-outputs
```
Expected: Generated files updated without errors.

- [ ] **Step 5: Commit**

```bash
git add ui/flutter_ui/lib/models/api_models.dart ui/flutter_ui/lib/models/api_models.freezed.dart ui/flutter_ui/lib/models/api_models.g.dart
git commit -m "feat(flutter): add ChatMessagePart, ImageRefData, Attachment multimodal models"
```

---

## Task 12: Flutter SDK client — upload method and Parts in ChatRequest

**Files:**
- Modify: `ui/flutter_ui/lib/services/sdk_client.dart`

- [ ] **Step 1: Add upload method and update ChatRequest in SDK client**

In `ui/flutter_ui/lib/services/sdk_client.dart`, add an upload method:

```dart
  /// Upload a file to the daemon. Returns the upload descriptor JSON.
  Future<Map<String, dynamic>?> uploadFile(
    Uint8List bytes,
    String filename,
    String mimeType,
  ) async {
    final uri = Uri.parse('$baseUrl/api/v1/uploads');
    final request = http.MultipartRequest('POST', uri)
      ..headers['Authorization'] = 'Bearer $apiKey'
      ..files.add(
        http.MultipartFile.fromBytes(
          'file',
          bytes,
          filename: filename,
          contentType: MediaType.parse(mimeType),
        ),
      );

    final response = await request.send();
    if (response.statusCode == 201) {
      final body = await response.stream.bytesToString();
      return jsonDecode(body) as Map<String, dynamic>;
    }
    return null;
  }
```

Update the `sendMessage` or `sendChat` method to accept optional `parts`:

```dart
  Future<Map<String, dynamic>?> sendChat({
    required String sessionId,
    required String text,
    String? agentId,
    List<Map<String, dynamic>>? parts,
  }) async {
    final body = <String, dynamic>{
      'message': text,
      'conversation_id': sessionId,
    };
    if (agentId != null) body['agent_id'] = agentId;
    if (parts != null && parts.isNotEmpty) body['parts'] = parts;

    final response = await http.post(
      Uri.parse('$baseUrl/api/v1/chat'),
      headers: _headers(),
      body: jsonEncode(body),
    );
    // ... existing response handling
  }
```

Add necessary imports (`dart:typed_data`, `package:http/http.dart`, `package:http_parser/http_parser.dart`).

- [ ] **Step 2: Verify Flutter analysis passes**

Run:
```bash
cd ui/flutter_ui && dart analyze lib/services/sdk_client.dart
```
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/services/sdk_client.dart
git commit -m "feat(flutter): add uploadFile method and Parts support in sendChat"
```

---

## Task 13: Flutter chat input redesign

**Files:**
- Modify: `ui/flutter_ui/lib/features/chat/chat_input.dart` (full redesign of input composition)

This is the largest UI task. The existing `_preparePayload(String) -> String` is replaced with multimodal-aware logic.

- [ ] **Step 1: Redesign _ChatInputState attachments**

Replace `final List<String> _attachments = []` with:

```dart
  // File attachments - uploaded files pending send
  final List<Attachment> _attachments = [];
```

- [ ] **Step 2: Replace _preparePayload with multimodal builder**

Replace the existing `_preparePayload` method:

```dart
  /// Build content parts from the current input text and attachments.
  List<Map<String, dynamic>> _buildParts(String text) {
    final parts = <Map<String, dynamic>>[];

    // Add attachment parts first
    for (final attachment in _attachments) {
      parts.add({
        'type': 'image_url',
        'image_url': {'url': 'file://${attachment.uploadId}'},
      });
    }

    // Add text part
    final expanded = _expandPastes(text.trim());
    if (expanded.isNotEmpty) {
      parts.add({'type': 'text', 'text': expanded});
    }

    return parts;
  }
```

- [ ] **Step 3: Update _sendNormal to use Parts**

```dart
  void _sendNormal(String text) {
    final parts = _buildParts(text);
    if (parts.isEmpty) return;

    // Check for slash commands (text-only, no attachments)
    if (_attachments.isEmpty) {
      final expanded = _expandPastes(text.trim());
      if (_tryHandleSlashCommand(expanded)) {
        _resetInputState();
        return;
      }
    }

    final chatNotifier = ref.read(chatProvider.notifier);
    final activeAgent = ref.read(activeAgentProvider);

    // Send with structured parts
    chatNotifier.sendMessageWithParts(
      sessionId: widget.sessionId,
      parts: parts,
      agentId: activeAgent?.id ?? 'coder',
    );

    _resetInputState();
  }
```

- [ ] **Step 4: Add attachment button to the left of the text field**

In `build()`, modify the `Row` that contains the text field and send button (around line 544-576):

```dart
            Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: [
                const SizedBox(width: 8),
                // Attachment button (left of text field)
                _buildAttachmentButton(),
                const SizedBox(width: 4),
                Expanded(
                  child: TextField(
                    // ... existing TextField config
                  ),
                ),
                const SizedBox(width: 8),
                _buildSendButton(),
              ],
            ),
```

Add the attachment button builder:

```dart
  Widget _buildAttachmentButton() {
    return GestureDetector(
      onTap: _pickFile,
      child: Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: CyberpunkColors.black,
          borderRadius: BorderRadius.circular(4),
        ),
        child: const Icon(
          Icons.attach_file,
          color: CyberpunkColors.greenSuccess,
          size: 18,
        ),
      ),
    );
  }

  Future<void> _pickFile() async {
    final result = await FilePicker.platform.pickFiles(
      type: FileType.image,
      allowMultiple: false,
    );
    if (result == null || result.files.isEmpty) return;

    final file = result.files.first;
    final bytes = file.bytes;
    if (bytes == null) return;

    // Upload to daemon
    final sdk = ref.read(sdkClientProvider);
    final upload = await sdk.uploadFile(
      bytes,
      file.name,
      file.extension != null ? 'image/${file.extension}' : 'image/png',
    );
    if (upload == null) return;

    final uploads = upload['uploads'] as List?;
    if (uploads == null || uploads.isEmpty) return;

    final uploadData = uploads.first as Map<String, dynamic>;
    setState(() {
      _attachments.add(Attachment(
        uploadId: uploadData['id'] as String,
        filename: file.name,
        mimeType: uploadData['mime_type'] as String? ?? 'image/png',
        sizeBytes: uploadData['size_bytes'] as int? ?? 0,
      ));
    });
  }
```

- [ ] **Step 5: Add attachment chips display above the text input**

In `build()`, above the `Row` containing the text field, add:

```dart
            // Attachment chips
            if (_attachments.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: _attachments.map((a) {
                      return Padding(
                        padding: const EdgeInsets.only(right: 4),
                        child: GestureDetector(
                          onTap: () => _removeAttachment(a),
                          child: Text(
                            '[${a.filename}]',
                            style: CyberpunkTypography.bodySmall.copyWith(
                              color: CyberpunkColors.greenSuccess,
                              fontSize: 11,
                            ),
                          ),
                        ),
                      );
                    }).toList(),
                  ),
                ),
              ),
```

Add the remove method:
```dart
  void _removeAttachment(Attachment attachment) {
    setState(() {
      _attachments.remove(attachment);
    });
  }
```

- [ ] **Step 6: Add drop target wrapper to the chat view**

In `ui/flutter_ui/lib/features/chat/chat_view.dart`, wrap the chat view content with a `DropTarget` (from the `desktop_drop` package). Add to pubspec.yaml if not present.

In `chat_view.dart`:
```dart
import 'package:desktop_drop/desktop_drop.dart';

// Wrap the chat content:
DropTarget(
  onDragDone: (details) {
    // Forward dropped files to chat input
    _handleDroppedFiles(details.files);
  },
  onDragEntered: (detail) => setState(() => _isDragging = true),
  onDragExited: (detail) => setState(() => _isDragging = false),
  child: _isDragging
      ? Stack(children: [content, _buildDropOverlay()])
      : content,
)
```

- [ ] **Step 7: Add clipboard image paste detection**

In `chat_input.dart`, extend the `_onTextChanged` or use a `RawKeyboardListener` / `KeyboardListener` widget to detect paste of image data. Use `super_clipboard` or `pasteboard` package:

```dart
  // In _handleKeyEvent, detect Cmd+V / Ctrl+V with image clipboard:
  if (event is KeyDownEvent &&
      event.logicalKey == LogicalKeyboardKey.keyV &&
      HardwareKeyboard.instance.isControlOrMetaPressed) {
    _tryPasteImage();
    return KeyEventResult.ignored; // allow normal text paste too
  }

  Future<void> _tryPasteImage() async {
    final clipboard = SystemClipboard.instance;
    if (clipboard == null) return;

    final reader = await clipboard.read();
    final imageFormat = await reader.readValue(Formats.png);
    if (imageFormat == null) return;

    // Upload pasted image bytes
    final sdk = ref.read(sdkClientProvider);
    final upload = await sdk.uploadFile(
      imageFormat,
      'pasted-${DateTime.now().millisecondsSinceEpoch}.png',
      'image/png',
    );
    if (upload == null) return;

    final uploads = upload['uploads'] as List?;
    if (uploads == null || uploads.isEmpty) return;

    final uploadData = uploads.first as Map<String, dynamic>;
    setState(() {
      _attachments.add(Attachment(
        uploadId: uploadData['id'] as String,
        filename: 'pasted.png',
        mimeType: 'image/png',
        sizeBytes: uploadData['size_bytes'] as int? ?? 0,
      ));
    });
  }
```

- [ ] **Step 8: Update _resetInputState to clear attachments**

```dart
  void _resetInputState() {
    _controller.text = '';
    _previousText = '';
    _pasteStore.clear();
    _pasteCounter = 0;
    _attachments.clear();
    _ghostText = null;
    _showSlashAutocomplete = false;
    _slashQuery = '';
  }
```

- [ ] **Step 9: Verify Flutter analysis passes**

Run:
```bash
cd ui/flutter_ui && dart analyze lib/features/chat/chat_input.dart
```
Expected: No errors

- [ ] **Step 10: Commit**

```bash
git add ui/flutter_ui/lib/features/chat/chat_input.dart ui/flutter_ui/lib/features/chat/chat_view.dart
git commit -m "feat(flutter): redesign chat input for multimodal attachments (button, chips, drag-drop, paste)"
```

---

## Task 14: Flutter message bubble rendering for image parts

**Files:**
- Modify: `ui/flutter_ui/lib/features/chat/chat_message_bubble.dart`

- [ ] **Step 1: Update ChatMessageBubble to render image parts as chips**

In `chat_message_bubble.dart`, find where message content is rendered. Add image part display:

```dart
// After the text content, render image chips
if (message.parts != null && message.parts!.isNotEmpty) {
  final imageParts = message.parts!
      .where((p) => p.type == 'image_url')
      .toList();
  if (imageParts.isNotEmpty) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Existing text content widget
        ...,

        // Image attachment chips
        if (imageParts.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Wrap(
              spacing: 4,
              children: imageParts.map((p) {
                final filename = p.imageUrl?.url.split('/').last ?? 'image';
                return Text(
                  '[${filename}]',
                  style: CyberpunkTypography.bodySmall.copyWith(
                    color: CyberpunkColors.greenSuccess,
                    fontSize: 11,
                  ),
                );
              }).toList(),
            ),
          ),
      ],
    );
  }
}
```

- [ ] **Step 2: Verify Flutter analysis passes**

Run:
```bash
cd ui/flutter_ui && dart analyze lib/features/chat/chat_message_bubble.dart
```
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add ui/flutter_ui/lib/features/chat/chat_message_bubble.dart
git commit -m "feat(flutter): render image attachment chips in message bubbles"
```

---

## Task 15: TUI multimodal — upload via RPC and Parts on send

**Files:**
- Modify: `internal/tui/models/chat.go` (detectAndAttachFile ~line 2687, doSendMessage ~line 1572, attachments ~line 146)
- Modify: `internal/tui/rpc.go` (add upload RPC method)

- [ ] **Step 1: Add image extension detection to detectAndAttachFile**

In `internal/tui/models/chat.go`, extend `detectAndAttachFile` (line 2687) to check for image file extensions and tag the attachment as an image:

```go
// imageExtensions are file extensions that should be treated as image uploads.
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
}

// isImageFile returns true if the path has an image file extension.
func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}
```

Modify the attachment storage to track whether each attachment is an image upload:

```go
// Change attachments field type:
type attachmentEntry struct {
	Path     string // original path (for display)
	UploadID string // populated for image uploads
	IsImage  bool
	Filename string
}
```

Change the field on ChatModel:
```go
	attachments []attachmentEntry
```

- [ ] **Step 2: Update doSendMessage to build Parts**

In `internal/tui/models/chat.go`, modify `doSendMessage` (line 1572) to build Parts when image attachments are present:

```go
func (m *ChatModel) doSendMessage() tea.Cmd {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" && len(m.attachments) == 0 {
		return nil
	}

	actualText := m.expandPasteTokens(text)

	// Build parts if we have image attachments
	var parts []llm.ContentPart
	for _, att := range m.attachments {
		if att.IsImage && att.UploadID != "" {
			parts = append(parts, llm.ContentPart{
				Type:     "image_url",
				ImageURL: &llm.ImageRef{URL: "file://" + att.UploadID},
			})
		} else {
			// Non-image attachment: include as text reference
			actualText = fmt.Sprintf("[Attached file: %s]\n%s", att.Path, actualText)
		}
	}

	if len(parts) > 0 {
		parts = append(parts, llm.ContentPart{Type: "text", Text: actualText})
	}

	m.textarea.Reset()
	m.attachments = nil
	m.compressedPastes = make(map[int]string)
	m.pasteCounter = 0

	// Route based on agent state
	// ... existing routing logic, but pass parts alongside text
	_ = parts // Forward to RPC call
	return nil
}
```

Note: the RPC payload struct for sending messages needs to be extended with `parts`. This involves the TUI's RPC client.

- [ ] **Step 3: Add upload RPC method to TUI RPC client**

In `internal/tui/rpc.go`, add:

```go
// UploadFile uploads a file via the daemon's upload service.
// Returns the upload ID.
func (c *RPCClient) UploadFile(ctx context.Context, filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Determine MIME type
	ext := strings.ToLower(filepath.Ext(filePath))
	mimeType := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	result, err := c.Call("upload.upload", map[string]any{
		"data":      encoded,
		"filename":  filepath.Base(filePath),
		"mime_type": mimeType,
	})
	if err != nil {
		return "", err
	}

	// Extract upload ID from result
	if m, ok := result.(map[string]any); ok {
		if id, ok := m["id"].(string); ok {
			return id, nil
		}
	}
	return "", fmt.Errorf("unexpected upload response")
}
```

- [ ] **Step 4: Verify build compiles**

Run: `go build ./internal/tui/...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/tui/models/chat.go internal/tui/rpc.go
git commit -m "feat(tui): upload image files via RPC, build Parts on send"
```

---

## Task 16: Config schema for uploads

**Files:**
- Modify: `internal/config/schema.go:258-265` (DaemonConfig)
- Modify: `config/meept.json5` (add uploads config section)

- [ ] **Step 1: Add UploadsConfig struct**

In `internal/config/schema.go`, after DaemonConfig (line 265):

```go
// UploadsConfig configures the file upload service.
type UploadsConfig struct {
	Enabled         bool     `json:"enabled"          toml:"enabled"`
	MaxSizeMB       int      `json:"max_size_mb"      toml:"max_size_mb"`
	AllowedTypes    []string `json:"allowed_types"    toml:"allowed_types"`
	GCRetentionDays int      `json:"gc_retention_days" toml:"gc_retention_days"`
	GCIntervalHours int      `json:"gc_interval_hours" toml:"gc_interval_hours"`
}
```

Add to `DaemonConfig`:
```go
type DaemonConfig struct {
	SocketPath        string        `json:"socket_path"        toml:"socket_path"`
	PIDFile           string        `json:"pid_file"            toml:"pid_file"`
	LogLevel          string        `json:"log_level"           toml:"log_level"`
	DataDir           string        `json:"data_dir"            toml:"data_dir"`
	ShutdownTimeout   string        `json:"shutdown_timeout"    toml:"shutdown_timeout"`
	ChatTimeoutSeconds int          `json:"chat_timeout_seconds" toml:"chat_timeout_seconds"`
	Uploads           UploadsConfig `json:"uploads"             toml:"uploads"`
}
```

- [ ] **Step 2: Add uploads config to config template**

In `config/meept.json5`, add under the daemon section:

```json5
  daemon: {
    // ... existing fields
    uploads: {
      enabled: true,
      max_size_mb: 20,
      allowed_types: ["image/png", "image/jpeg", "image/gif", "image/webp"],
      gc_retention_days: 7,
      gc_interval_hours: 24,
    },
  },
```

- [ ] **Step 3: Wire config into UploadService initialization**

In `internal/daemon/components.go`, update the UploadService creation to use config:

```go
	uploadCfg := d.config.Daemon.Uploads
	if uploadCfg.MaxSizeMB == 0 {
		uploadCfg.MaxSizeMB = 20
	}
	if len(uploadCfg.AllowedTypes) == 0 {
		uploadCfg.AllowedTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
	}
	uploadDir := filepath.Join(d.config.Daemon.DataDir, "uploads")
	uploadSvc := services.NewUploadService(uploadDir, uploadCfg.MaxSizeMB, uploadCfg.AllowedTypes)
```

- [ ] **Step 4: Verify build compiles**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go config/meept.json5 internal/daemon/components.go
git commit -m "feat(config): add uploads configuration block (max_size, allowed_types, GC)"
```

---

## Task 17: Wire upload RPC handler for TUI

**Files:**
- Modify: `internal/tui/rpc.go` (or wherever TUI bus handlers are registered)
- Create or modify the daemon-side handler for `upload.upload` bus topic

- [ ] **Step 1: Add upload bus handler in daemon**

In the daemon, register a handler for the `upload.upload` bus topic that delegates to UploadService:

```go
// In internal/daemon/ or wherever bus handlers are registered:
func registerUploadHandler(bus *bus.MessageBus, uploadSvc *services.UploadService, logger *slog.Logger) {
	handler := bus.NewSubscriptionHandler(bus, logger.With("component", "upload-handler"))
	handler.Subscribe("upload.upload", func(ctx context.Context, topic string, msg any) {
		busMsg := msg.(*models.BusMessage)
		var params struct {
			Data     string `json:"data"` // base64-encoded
			Filename string `json:"filename"`
			MimeType string `json:"mime_type"`
		}
		if err := json.Unmarshal(busMsg.Payload, &params); err != nil {
			// send error response
			return
		}
		data, err := base64.StdEncoding.DecodeString(params.Data)
		if err != nil {
			return
		}
		upload, err := uploadSvc.Upload(ctx, bytes.NewReader(data), params.Filename, params.MimeType)
		if err != nil {
			return
		}
		// Send response with upload descriptor
	})
}
```

- [ ] **Step 2: Verify build compiles**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/
git commit -m "feat(daemon): register upload.upload bus handler for TUI file uploads"
```

---

## Task 18: Run full test suite and fix regressions

- [ ] **Step 1: Run Go tests**

Run: `go test ./... -count=1 2>&1 | tail -30`
Expected: All tests pass

- [ ] **Step 2: Run Go build**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 3: Run Flutter analysis**

Run: `cd ui/flutter_ui && dart analyze lib/ 2>&1 | tail -20`
Expected: No errors

- [ ] **Step 4: Fix any regressions found**

Fix any issues discovered in steps 1-3.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "fix: resolve regressions from vision model integration"
```

---

## Self-Review Notes

**Spec coverage check:**
- Data model (ContentPart, ImageRef, ChatMessage.Parts): Tasks 1-2
- Upload storage and HTTP endpoints: Tasks 6-7
- Session persistence (parts column, serialization): Task 5
- Vision routing and model selection: Task 10 (pre-flight + capability swap)
- Vision description cache flow: Tasks 9-10
- Provider serialization (Anthropic + OpenAI): Tasks 2, 4
- Flutter GUI redesign (input, chips, button, drag-drop, paste): Tasks 11-13
- Flutter message bubble rendering: Task 14
- TUI multimodal (upload RPC, Parts on send): Tasks 15, 17
- Config schema: Task 16
- UploadService (dedup, GC, MIME validation): Task 6
- ChatRequest Parts forwarding: Task 8
- Full regression test: Task 18

**Type consistency check:**
- `ContentPart` used consistently across all Go tasks
- `ImageRef.URL` and `ImageRef.Description` field names match in Go and Flutter (`ImageRefData.url`, `.description`)
- `UploadService.Upload()`, `.Load()`, `.Get()`, `.Release()`, `.Acquire()` method names consistent
- Flutter `ChatMessagePart` and `Attachment` types don't conflict
- `needsVisionPreflight`, `collectUndescribedImageRefs`, `runVisionPreflight` — names consistent across Tasks 9-10
- `CapImages` constant already exists in `provider_registry.go:36`

**Gap:** Vision specialist agent registration (`@vision` routing) is partially covered in Task 10 but the dispatcher-level routing (steps 1-4 of the routing precedence) needs explicit wiring. This should be added as part of Task 10 step 2 — when the agent loop detects images, it should also check for `@vision` in the message text and route accordingly. The existing model-reassignment parser handles the `@model` case; `@vision` extends it.
