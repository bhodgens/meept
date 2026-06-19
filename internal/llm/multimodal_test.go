package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// Task 2: ChatMessage with Parts
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

// Task 3: resolveImageURL helper tests
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

// Task 4: Anthropic parts serialization tests
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

// TestToOpenAIDictWithStore_ResolvesFileURL verifies that ToOpenAIDictWithStore
// converts file:// image references into data: URLs via the UploadStore,
// matching the behavior the OpenAI wire format requires.
func TestToOpenAIDictWithStore_ResolvesFileURL(t *testing.T) {
	store := &mockUploadStore{
		data:     []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A},
		mimeType: "image/png",
	}
	msg := ChatMessage{
		Role: RoleUser,
		Parts: []ContentPart{
			{Type: "text", Text: "describe this"},
			{Type: "image_url", ImageURL: &ImageRef{
				URL:      "file://test-id",
				MIMEType: "image/png",
			}},
		},
	}
	dict := msg.ToOpenAIDictWithStore(store)

	content, ok := dict["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected content to be []map[string]any, got %T", dict["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	if content[1]["type"] != "image_url" {
		t.Fatalf("expected image_url type, got %v", content[1]["type"])
	}
	imgURL, ok := content[1]["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("expected image_url to be map[string]any, got %T", content[1]["image_url"])
	}
	url, ok := imgURL["url"].(string)
	if !ok {
		t.Fatalf("expected url to be string, got %T", imgURL["url"])
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("expected data URL, got %q", url)
	}
	if strings.Contains(url, "file://") {
		t.Errorf("file:// URL was not resolved: %q", url)
	}
}

// TestToOpenAIDictWithStore_NilStorePassesThrough verifies that a nil store
// preserves the legacy behavior (URLs are emitted verbatim), ensuring
// backward compatibility for callers that have not been wired up yet.
func TestToOpenAIDictWithStore_NilStorePassesThrough(t *testing.T) {
	msg := ChatMessage{
		Role: RoleUser,
		Parts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageRef{
				URL:      "file://abc123.png",
				MIMEType: "image/png",
			}},
		},
	}
	dict := msg.ToOpenAIDictWithStore(nil)
	content := dict["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content))
	}
	imgURL := content[0]["image_url"].(map[string]any)
	url := imgURL["url"].(string)
	if url != "file://abc123.png" {
		t.Errorf("expected verbatim file:// URL, got %q", url)
	}
}

// TestToOpenAIDictWithStore_LoadErrorFallsBackToText verifies that when the
// upload store fails to load the image, the image part is replaced with a
// text placeholder so the request still succeeds (matching the Anthropic
// client behavior at anthropic.go:708-711).
func TestToOpenAIDictWithStore_LoadErrorFallsBackToText(t *testing.T) {
	store := &mockUploadStore{
		err: fmt.Errorf("disk read failure"),
	}
	msg := ChatMessage{
		Role: RoleUser,
		Parts: []ContentPart{
			{Type: "image_url", ImageURL: &ImageRef{
				URL: "file://missing",
			}},
		},
	}
	dict := msg.ToOpenAIDictWithStore(store)
	content := dict["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content))
	}
	if content[0]["type"] != "text" {
		t.Errorf("expected text fallback, got %v", content[0]["type"])
	}
	text, _ := content[0]["text"].(string)
	if !strings.Contains(text, "unable to load") {
		t.Errorf("expected 'unable to load' placeholder, got %q", text)
	}
}

// TestToOpenAIDictBackwardCompat verifies the no-arg ToOpenAIDict() still
// produces the legacy verbatim-URL behavior (it delegates to the nil-store path).
func TestToOpenAIDictBackwardCompat(t *testing.T) {
	msg := ChatMessage{
		Role: RoleUser,
		Parts: []ContentPart{
			{Type: "text", Text: "hello"},
			{Type: "image_url", ImageURL: &ImageRef{URL: "file://x.png"}},
		},
	}
	dict := msg.ToOpenAIDict()
	content := dict["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content))
	}
	if content[0]["type"] != "text" || content[0]["text"] != "hello" {
		t.Errorf("text part mismatch: %+v", content[0])
	}
	if content[1]["type"] != "image_url" {
		t.Errorf("expected image_url, got %v", content[1]["type"])
	}
	imgURL := content[1]["image_url"].(map[string]any)
	if url := imgURL["url"].(string); url != "file://x.png" {
		t.Errorf("expected verbatim URL, got %q", url)
	}
}
