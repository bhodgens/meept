package llm

import (
	"errors"
	"strings"
	"testing"
)

// TestRefusalError_NonRetryable pins the leaf-01 contract: RefusalError is
// NonRetryable, its Error() carries the refusal facts, and Unwrap of a
// Cause-less error is nil.
func TestRefusalError_NonRetryable(t *testing.T) {
	e := &RefusalError{ProviderID: "zai", ModelID: "glm-4.7", Source: "finish_reason", FinishReason: "content_filter"}
	if !e.NonRetryable() {
		t.Fatal("refusal must be NonRetryable")
	}
	var _ NonRetryableError = e
	if !strings.Contains(e.Error(), "model refusal") || !strings.Contains(e.Error(), "zai") {
		t.Fatalf("Error() missing facts: %q", e.Error())
	}
	if errors.Unwrap(e) != nil {
		t.Fatal("Unwrap of Cause-less error must be nil")
	}
}

// TestDetectRefusal maps finish/stop reason strings onto RefusalError
// (table-driven, leaf-01 Task 2).
func TestDetectRefusal(t *testing.T) {
	cases := []struct {
		name           string
		finishReason   string
		wantNil        bool
		wantSource     string
		wantFinishReas string
	}{
		{"refusal stop reason", "refusal", false, "stop_reason", "refusal"},
		{"refusal case-insensitive", "REFUSAL", false, "stop_reason", "REFUSAL"},
		{"content_filter finish reason", "content_filter", false, "finish_reason", "content_filter"},
		{"content_filter case-insensitive", "CONTENT_FILTER", false, "finish_reason", "CONTENT_FILTER"},
		{"stop is not a refusal", "stop", true, "", ""},
		{"empty is not a refusal", "", true, "", ""},
		{"length is not a refusal", "length", true, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectRefusal("zai", "glm-4.7", tc.finishReason)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("DetectRefusal(%q) = %+v, want nil", tc.finishReason, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("DetectRefusal(%q) = nil, want refusal error", tc.finishReason)
			}
			if got.Source != tc.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tc.wantSource)
			}
			if got.FinishReason != tc.wantFinishReas {
				t.Errorf("FinishReason = %q, want %q", got.FinishReason, tc.wantFinishReas)
			}
			if got.ProviderID != "zai" || got.ModelID != "glm-4.7" {
				t.Errorf("provider/model = %q/%q, want zai/glm-4.7", got.ProviderID, got.ModelID)
			}
		})
	}
}

// TestDetectRefusal_EmptyModelIDAllowed: streaming paths may not know the
// model yet; empty modelID must still classify.
func TestDetectRefusal_EmptyModelIDAllowed(t *testing.T) {
	got := DetectRefusal("anthropic", "", "refusal")
	if got == nil {
		t.Fatal("DetectRefusal with empty modelID = nil, want refusal error")
	}
}

// TestDetectRefusalFromBody classifies typed safeguard error bodies
// conservatively (leaf-01 Task 3).
func TestDetectRefusalFromBody(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		statusCode int
		wantNil    bool
	}{
		{"safeguards flagged text", `{"error":{"message":"Your request has been flagged. Our safeguards flagged this message."}}`, 403, false},
		{"content policy violation", "Error: content policy violation detected", 400, false},
		{"flagged this message", "we have flagged this message as violating usage policies", 403, false},
		{"content_filter error code key", `{"error":{"code":"content_filter","message":"filtered"}}`, 400, false},
		{"case-insensitive markers", "SAFEGUARDS FLAGGED THIS REQUEST", 403, false},
		{"connection refused is not a refusal", "connection refused", 500, true},
		{"internal server error is not a refusal", "internal server error", 500, true},
		{"empty body", "", 500, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectRefusalFromBody("zai", "glm-4.7", tc.statusCode, tc.body)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("DetectRefusalFromBody(%q) = %+v, want nil", tc.body, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("DetectRefusalFromBody(%q) = nil, want refusal error", tc.body)
			}
			if got.Source != "error_body" {
				t.Errorf("Source = %q, want error_body", got.Source)
			}
			if got.StatusCode != tc.statusCode {
				t.Errorf("StatusCode = %d, want %d", got.StatusCode, tc.statusCode)
			}
			if got.Message == "" {
				t.Error("Message must carry the body text")
			}
		})
	}
}

// TestDetectRefusalFromBody_MessageTruncatedTo500 pins the 500-char cap on
// the carried Message.
func TestDetectRefusalFromBody_MessageTruncatedTo500(t *testing.T) {
	long := strings.Repeat("safeguards flagged this message. ", 100) // > 500 chars
	got := DetectRefusalFromBody("zai", "glm-4.7", 403, long)
	if got == nil {
		t.Fatal("DetectRefusalFromBody = nil, want refusal error")
	}
	if len(got.Message) > 500 {
		t.Errorf("Message length = %d, want <= 500", len(got.Message))
	}
}

// TestDetectRefusalFromBody_NoMatchOnBroadKeywords pins the CONSERVATIVE
// marker list: broad words like "policy" or "flagged" alone must NOT match.
func TestDetectRefusalFromBody_NoMatchOnBroadKeywords(t *testing.T) {
	for _, body := range []string{"our policy is friendly", "flagged for review by support"} {
		if got := DetectRefusalFromBody("zai", "glm-4.7", 403, body); got != nil {
			t.Errorf("DetectRefusalFromBody(%q) = %+v, want nil (marker too broad)", body, got)
		}
	}
}
