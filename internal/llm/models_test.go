package llm

import "testing"

func TestTokenUsage_CacheFields(t *testing.T) {
	u := TokenUsage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		TotalTokens:      1200,
		CachedTokens:     800,
	}
	if u.CachedTokens != 800 {
		t.Errorf("CachedTokens = %d, want 800", u.CachedTokens)
	}
}

func TestChatResponseUsage_PromptTokensDetails(t *testing.T) {
	resp := ChatResponse{}
	resp.Usage.PromptTokens = 1000
	resp.Usage.CompletionTokens = 200
	resp.Usage.TotalTokens = 1200
	resp.Usage.PromptTokensDetails.CachedTokens = 800

	if resp.Usage.PromptTokensDetails.CachedTokens != 800 {
		t.Errorf("PromptTokensDetails.CachedTokens = %d, want 800", resp.Usage.PromptTokensDetails.CachedTokens)
	}
}
