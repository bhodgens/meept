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

// TestVisionPreflightNeedsRetryAfterFailure (M1) verifies that a ref whose
// analysis failed on a previous turn is picked up by needsVisionPreflight on
// the next turn, even though its Description remains empty. Before the fix,
// failure sentinels were written into Description and subsequent turns
// skipped the ref forever.
func TestVisionPreflightNeedsRetryAfterFailure(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: &llm.ImageRef{
				URL:            "file://abc.png",
				AnalysisFailed: true, // prior turn failed
			}},
		}},
	}
	if !needsVisionPreflight(messages) {
		t.Error("expected needsVisionPreflight to be true when AnalysisFailed is set")
	}
	if !llm.HasUndescribedImages(messages[0].Parts) {
		t.Error("expected HasUndescribedImages to be true when AnalysisFailed is set")
	}
}

// TestVisionPreflightRetriesFailedRef (M1) verifies that runVisionPreflight
// retries a ref marked AnalysisFailed from a prior turn and, on success,
// clears the flag and populates Description. The mock chatter returns an
// error on the first call (simulating the prior failed attempt — already
// reflected in the ref state) and a real description on the next call.
func TestVisionPreflightRetriesFailedRef(t *testing.T) {
	ref := &llm.ImageRef{
		URL:            "file://retry.png",
		AnalysisFailed: true,
	}
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: ref},
		}},
	}

	chatter := newMockChatter(&llm.Response{Content: "a red square"})
	if err := runVisionPreflight(context.Background(), messages, chatter, nil, nil); err != nil {
		t.Fatalf("runVisionPreflight returned error: %v", err)
	}
	if ref.Description != "a red square" {
		t.Errorf("expected Description=%q, got %q", "a red square", ref.Description)
	}
	if ref.AnalysisFailed {
		t.Error("expected AnalysisFailed to be cleared after successful retry")
	}
}

// TestVisionPreflightFailedRefStaysRetryable (M1) verifies that when a vision
// pre-flight attempt fails, the ref's AnalysisFailed flag is set (so the next
// turn can retry) and no sentinel string is written to Description.
func TestVisionPreflightFailedRefStaysRetryable(t *testing.T) {
	ref := &llm.ImageRef{URL: "file://fail.png"}
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: ref},
		}},
	}

	// mockChatter returns "no more mock responses" after the queue empties,
	// so both the initial call and the retry will fail.
	chatter := newMockChatter()
	if err := runVisionPreflight(context.Background(), messages, chatter, nil, nil); err != nil {
		t.Fatalf("runVisionPreflight returned error: %v", err)
	}
	if !ref.AnalysisFailed {
		t.Error("expected AnalysisFailed=true after failure")
	}
	if ref.Description != "" {
		t.Errorf("expected Description to remain empty (no sentinel), got %q", ref.Description)
	}
	// Crucially, a subsequent turn must still see this image as needing work.
	if !needsVisionPreflight(messages) {
		t.Error("expected needsVisionPreflight to be true after a failed attempt")
	}
}

// TestVisionPreflightPropagatesDescriptionToDuplicateURLs (M3) verifies that
// when two ContentParts reference the same image URL, a single vision call
// populates the description on both refs. Before the fix, only the first ref
// for each URL was added to the describe queue and the second remained stale.
func TestVisionPreflightPropagatesDescriptionToDuplicateURLs(t *testing.T) {
	const url = "file://dup.png"
	refA := &llm.ImageRef{URL: url}
	refB := &llm.ImageRef{URL: url}
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: refA},
			{Type: "image_url", ImageURL: refB},
		}},
	}

	chatter := newMockChatter(&llm.Response{Content: "a blue circle"})
	if err := runVisionPreflight(context.Background(), messages, chatter, nil, nil); err != nil {
		t.Fatalf("runVisionPreflight returned error: %v", err)
	}

	if refA.Description != "a blue circle" {
		t.Errorf("refA: expected Description=%q, got %q", "a blue circle", refA.Description)
	}
	if refB.Description != "a blue circle" {
		t.Errorf("refB: expected Description=%q, got %q (propagation failed)", "a blue circle", refB.Description)
	}
	if refA.AnalysisFailed || refB.AnalysisFailed {
		t.Error("expected AnalysisFailed=false on both refs after success")
	}
}

// TestVisionPreflightPropagatesFailureToDuplicateURLs (M3) verifies that when
// the vision call for a URL fails, every ref with that URL is marked
// AnalysisFailed so the next turn retries all of them.
func TestVisionPreflightPropagatesFailureToDuplicateURLs(t *testing.T) {
	const url = "file://dupfail.png"
	refA := &llm.ImageRef{URL: url}
	refB := &llm.ImageRef{URL: url}
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: refA},
			{Type: "image_url", ImageURL: refB},
		}},
	}

	chatter := newMockChatter() // empty queue -> every Chat call errors
	if err := runVisionPreflight(context.Background(), messages, chatter, nil, nil); err != nil {
		t.Fatalf("runVisionPreflight returned error: %v", err)
	}

	if !refA.AnalysisFailed {
		t.Error("refA: expected AnalysisFailed=true after failure")
	}
	if !refB.AnalysisFailed {
		t.Error("refB: expected AnalysisFailed=true after failure (propagation failed)")
	}
}

// TestVisionPreflightEmptyResponseSetsFailureFlag (M1) verifies that an empty
// vision-model response is treated as a retryable failure rather than being
// silently swallowed via a sentinel string in Description.
func TestVisionPreflightEmptyResponseSetsFailureFlag(t *testing.T) {
	ref := &llm.ImageRef{URL: "file://empty.png"}
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: ref},
		}},
	}

	chatter := newMockChatter(&llm.Response{Content: ""})
	if err := runVisionPreflight(context.Background(), messages, chatter, nil, nil); err != nil {
		t.Fatalf("runVisionPreflight returned error: %v", err)
	}
	if !ref.AnalysisFailed {
		t.Error("expected AnalysisFailed=true after empty response")
	}
	if ref.Description != "" {
		t.Errorf("expected Description to remain empty, got %q", ref.Description)
	}
}

// TestCollectUndescribedImageRefsIncludesFailedRefs (M1) verifies the
// collector includes refs whose AnalysisFailed flag is set even when they
// have no Description, so the retry path actually re-queues them.
func TestCollectUndescribedImageRefsIncludesFailedRefs(t *testing.T) {
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{
			{Type: "image_url", ImageURL: &llm.ImageRef{
				URL:            "file://fresh.png",
				AnalysisFailed: false,
			}},
			{Type: "image_url", ImageURL: &llm.ImageRef{
				URL:            "file://retry.png",
				AnalysisFailed: true, // prior failure, needs retry
			}},
		}},
	}
	refs := collectUndescribedImageRefs(messages)
	if len(refs) != 2 {
		t.Errorf("expected 2 refs (fresh + failed), got %d", len(refs))
	}
}

