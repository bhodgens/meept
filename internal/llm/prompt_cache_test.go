package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyPromptSections(t *testing.T) {
	tests := []struct {
		name        string
		sections    []string
		wantStatic  []string
		wantDynamic []string
	}{
		{
			name:        "static before boundary, dynamic after",
			sections:    []string{"constitution", "capabilities", PromptCacheBoundary, "memory", "task context"},
			wantStatic:  []string{"constitution", "capabilities"},
			wantDynamic: []string{"memory", "task context"},
		},
		{
			name:        "boundary at start means all dynamic",
			sections:    []string{PromptCacheBoundary, "memory", "task"},
			wantStatic:  nil,
			wantDynamic: []string{"memory", "task"},
		},
		{
			name:        "boundary at end means all static",
			sections:    []string{"constitution", "tools", PromptCacheBoundary},
			wantStatic:  []string{"constitution", "tools"},
			wantDynamic: nil,
		},
		{
			name:        "empty sections are skipped",
			sections:    []string{"a", "", PromptCacheBoundary, "", "b"},
			wantStatic:  []string{"a"},
			wantDynamic: []string{"b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			static, dynamic := ClassifyPromptSections(tt.sections)
			assert.Equal(t, tt.wantStatic, static)
			assert.Equal(t, tt.wantDynamic, dynamic)
		})
	}
}

func TestClassifyPromptSectionsNoBoundary(t *testing.T) {
	sections := []string{"constitution", "capabilities", "tools"}
	static, dynamic := ClassifyPromptSections(sections)
	assert.Equal(t, sections, static)
	assert.Nil(t, dynamic)
}

func TestClassifyPromptSectionsEmpty(t *testing.T) {
	static, dynamic := ClassifyPromptSections(nil)
	assert.Nil(t, static)
	assert.Nil(t, dynamic)

	static, dynamic = ClassifyPromptSections([]string{})
	assert.Nil(t, static)
	assert.Nil(t, dynamic)

	static, dynamic = ClassifyPromptSections([]string{"", ""})
	assert.Nil(t, static)
	assert.Nil(t, dynamic)
}

func TestBuildSystemPromptBlocks(t *testing.T) {
	tests := []struct {
		name       string
		sections   []string
		wantLen    int
		wantScopes []CacheScope
	}{
		{
			name:       "static and dynamic blocks",
			sections:   []string{"a", "b", PromptCacheBoundary, "c"},
			wantLen:    2,
			wantScopes: []CacheScope{CacheScopeStatic, CacheScopeSession},
		},
		{
			name:       "only static",
			sections:   []string{"a", "b"},
			wantLen:    1,
			wantScopes: []CacheScope{CacheScopeStatic},
		},
		{
			name:       "only dynamic",
			sections:   []string{PromptCacheBoundary, "c"},
			wantLen:    1,
			wantScopes: []CacheScope{CacheScopeSession},
		},
		{
			name:       "empty input produces no blocks",
			sections:   nil,
			wantLen:    0,
			wantScopes: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := BuildSystemPromptBlocks(tt.sections)
			require.Len(t, blocks, tt.wantLen)
			for i, scope := range tt.wantScopes {
				assert.Equal(t, scope, blocks[i].CacheScope)
				assert.NotEmpty(t, blocks[i].Text)
			}
		})
	}
}

func TestBuildPrefixAwareKey(t *testing.T) {
	builder := NewCacheKeyBuilder(false)
	systemPrompt := []string{"constitution", "tools", PromptCacheBoundary, "memory"}
	msgs := []ChatMessage{{Role: "user", Content: "hello"}}

	key1 := builder.BuildPrefixAwareKey("model-a", systemPrompt, msgs)
	key2 := builder.BuildPrefixAwareKey("model-a", systemPrompt, msgs)

	assert.Equal(t, key1.PromptHash, key2.PromptHash, "same inputs must produce identical keys")
	assert.Equal(t, "model-a", key1.ModelID)
	assert.Contains(t, key1.PromptHash, ":", "key should contain colon-separated segments")
}

func TestBuildPrefixAwareKeyDynamicChange(t *testing.T) {
	builder := NewCacheKeyBuilder(false)
	msgs := []ChatMessage{{Role: "user", Content: "hello"}}

	key1 := builder.BuildPrefixAwareKey("m", []string{"static", PromptCacheBoundary, "dynamic-v1"}, msgs)
	key2 := builder.BuildPrefixAwareKey("m", []string{"static", PromptCacheBoundary, "dynamic-v2"}, msgs)

	assert.NotEqual(t, key1.PromptHash, key2.PromptHash, "different dynamic content must produce different keys")

	// Static prefix segment should be identical.
	assert.Equal(t, key1.PromptHash[:16], key2.PromptHash[:16], "static prefix must remain stable")
}

func TestBuildPrefixAwareKeyStaticChange(t *testing.T) {
	builder := NewCacheKeyBuilder(false)
	msgs := []ChatMessage{{Role: "user", Content: "hello"}}

	key1 := builder.BuildPrefixAwareKey("m", []string{"static-v1", PromptCacheBoundary, "dynamic"}, msgs)
	key2 := builder.BuildPrefixAwareKey("m", []string{"static-v2", PromptCacheBoundary, "dynamic"}, msgs)

	assert.NotEqual(t, key1.PromptHash, key2.PromptHash, "different static content must produce different keys")
	assert.NotEqual(t, key1.PromptHash[:16], key2.PromptHash[:16], "static prefix segment must differ")
}
