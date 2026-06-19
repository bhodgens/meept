package agent

import (
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
