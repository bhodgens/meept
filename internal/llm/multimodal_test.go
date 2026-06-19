package llm

import (
	"context"
	"encoding/json"
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
