// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RepoMapConfig holds all configuration for RepoMap generation.
type RepoMapConfig struct {
	// Enabled indicates whether RepoMap is enabled.
	Enabled bool
	// MaxMapTokens is the maximum tokens for the rendered map.
	MaxMapTokens int
	// MapMulNoFiles is the multiplier when no files are in chat.
	MapMulNoFiles float64
	// CacheDir is the directory for cache storage.
	CacheDir string
	// MaxCacheSize is the max cache size in bytes.
	MaxCacheSize int64
	// Damping is the PageRank damping factor.
	Damping float64
	// MaxIterations is the max PageRank iterations.
	MaxIterations int
	// ConvergenceTol is the PageRank convergence tolerance.
	ConvergenceTol float64
	// ContextLines is the number of lines of context to show.
	ContextLines int
	// MaxLineLength is the max line length before truncation.
	MaxLineLength int
}

// DefaultRepoMapConfig returns a RepoMapConfig with default values.
func DefaultRepoMapConfig() RepoMapConfig {
	return RepoMapConfig{
		Enabled:         true,
		MaxMapTokens:    1024,
		MapMulNoFiles:   8.0,
		CacheDir:        "~/.meept/repomap_cache",
		MaxCacheSize:    500 * 1024 * 1024,
		Damping:         0.85,
		MaxIterations:   100,
		ConvergenceTol:  1e-6,
		ContextLines:    2,
		MaxLineLength:   100,
	}
}

// ValidateRepoMapConfig validates the configuration.
func ValidateRepoMapConfig(config RepoMapConfig) error {
	if config.MaxMapTokens <= 0 {
		return nil // Use default
	}
	return nil
}

// RepoMapGenerator wraps all components needed to generate a RepoMap.
type RepoMapGenerator struct {
	extractor    *TagExtractor
	graph        *RepoGraphBuilder
	ranker       *PageRanker
	fit          *BudgetFitter
	renderer     *ContextRenderer
	cache        *MapCache
	tagCache     *TagCache
	config       RepoMapConfig
	logger       *slog.Logger
	watchedFiles []string
	mu           sync.RWMutex
}

// RepoGraphBuilder wraps graph construction.
type RepoGraphBuilder struct{}

// Build constructs the dependency graph from tags.
func (g *RepoGraphBuilder) Build(tags []Tag, chatFiles, mentionedIdentifiers []string) *RepoGraph {
	return BuildGraph(tags, chatFiles, mentionedIdentifiers)
}

// PageRanker wraps the PageRank computation.
type PageRanker struct{}

// Compute runs PageRank on the graph.
func (r *PageRanker) Compute(graph *RepoGraph, config PageRankConfig) RankedTags {
	return ComputeRank(graph, config)
}

// BudgetFitter wraps the token budget fitting.
type BudgetFitter struct{}

// Fit fits ranked tags to token budget.
func (f *BudgetFitter) Fit(ranked RankedTags, config FittingConfig, renderer RenderingProvider) RenderedMap {
	return FitToBudget(ranked, config, renderer)
}

// NewRepoMapGenerator creates a new RepoMapGenerator with all components.
func NewRepoMapGenerator(config RepoMapConfig, logger *slog.Logger, watchedFiles []string) (*RepoMapGenerator, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Apply defaults
	if config.MaxMapTokens == 0 {
		config.MaxMapTokens = 1024
	}
	if config.CacheDir == "" {
		homeDir := filepath.Join("~", ".meept", "repomap_cache")
		config.CacheDir = homeDir
	}
	if config.MapMulNoFiles == 0 {
		config.MapMulNoFiles = 8.0
	}

	// Create tag extractor
	extractor := NewTagExtractor(logger)

	// Create renderer
	rendererConfig := RendererConfig{
		MaxLineLength:  config.MaxLineLength,
		ContextLines:   config.ContextLines,
		EnableTreeView: true,
		MaxTagsPerFile: 20,
	}
	renderer := NewContextRenderer(rendererConfig, logger)

	// Create cache
	cacheConfig := CacheConfig{
		CacheDir:         config.CacheDir,
		MaxCacheSize:     config.MaxCacheSize,
		EnableMemoryCache: true,
		MemoryCacheSize:  100,
		Logger:           logger,
	}

	tagCache, err := NewTagCache(cacheConfig)
	if err != nil {
		logger.Warn("failed to create tag cache, continuing without disk cache", "error", err)
		// Continue without disk cache
		tagCache = nil
	}

	memCache := NewMapCache(cacheConfig)

	return &RepoMapGenerator{
		extractor:    extractor,
		graph:        &RepoGraphBuilder{},
		ranker:       &PageRanker{},
		fit:          &BudgetFitter{},
		renderer:     renderer,
		cache:        memCache,
		tagCache:     tagCache,
		config:       config,
		logger:       logger,
		watchedFiles: watchedFiles,
	}, nil
}

// UpdateWatchedFiles updates the list of files to analyze.
func (g *RepoMapGenerator) UpdateWatchedFiles(files []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.watchedFiles = files
}

// Generate creates a RepoMap for the given chat context.
// chatFiles are the files actively being discussed in the conversation.
// mentionedIdentifiers are the identifiers (functions, types, etc.) mentioned in the conversation.
func (g *RepoMapGenerator) Generate(ctx context.Context, chatFiles, mentionedIdentifiers []string) (*RenderedMap, error) {
	if !g.config.Enabled {
		return nil, nil
	}

	g.mu.RLock()
	watchedFiles := g.watchedFiles
	g.mu.RUnlock()

	if len(watchedFiles) == 0 {
		return nil, nil
	}

	// Check memory cache first
	if cached, ok := g.cache.Get(chatFiles, mentionedIdentifiers, 5*time.Minute); ok {
		g.logger.Debug("using cached RepoMap", "tokens", cached.Tokens)
		return &RenderedMap{
			Content: cached.Content,
			Tokens:  cached.Tokens,
		}, nil
	}

	// Step 1: Extract tags (with disk caching)
	tags, err := g.extractTags(ctx, watchedFiles)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return &RenderedMap{Content: "", Tokens: 0}, nil
	}

	// Step 2: Build graph
	graph := g.graph.Build(tags, chatFiles, mentionedIdentifiers)

	// Step 3: Compute PageRank
	personalization := g.buildPersonalization(chatFiles, mentionedIdentifiers)
	pageRankConfig := PageRankConfig{
		Damping:        g.config.Damping,
		MaxIterations:  g.config.MaxIterations,
		ConvergenceTol: g.config.ConvergenceTol,
		Personalization: personalization,
	}
	ranked := g.ranker.Compute(graph, pageRankConfig)

	if len(ranked) == 0 {
		return &RenderedMap{Content: "", Tokens: 0}, nil
	}

	// Step 4: Fit to budget
	fittingConfig := FittingConfig{
		MaxMapTokens:  calculateTargetTokens(g.config, chatFiles),
		Tolerance:     0.15,
		MapMulNoFiles: g.config.MapMulNoFiles,
		MinTags:       5,
		MaxTags:       500,
	}
	rendered := g.fit.Fit(ranked, fittingConfig, g.renderer)

	// Cache the result
	if rendered.Content != "" {
		g.cache.Set(chatFiles, mentionedIdentifiers, rendered)
	}

	return &rendered, nil
}

// extractTags extracts tags from watched files with caching.
func (g *RepoMapGenerator) extractTags(ctx context.Context, files []string) ([]Tag, error) {
	var allTags []Tag
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, len(files))

	for _, file := range files {
		wg.Add(1)
		go func(f string) {
			defer wg.Done()

			// Try disk cache first
			if g.tagCache != nil {
				mtime := getFileMtime(f)
				if !mtime.IsZero() {
					if cached, found, err := g.tagCache.Get(f, mtime); err == nil && found && len(cached) > 0 {
						mu.Lock()
						allTags = append(allTags, cached...)
						mu.Unlock()
						return
					}
				}
			}

			// Extract tags
			tags, err := g.extractor.ExtractTagsWithContext(ctx, f)
			if err != nil {
				errCh <- err
				return
			}

			// Cache the result
			if g.tagCache != nil && len(tags) > 0 {
				mtime := getFileMtime(f)
				_ = g.tagCache.Set(f, mtime, tags)
			}

			mu.Lock()
			allTags = append(allTags, tags...)
			mu.Unlock()
		}(file)
	}

	wg.Wait()
	close(errCh)

	// Log errors but don't fail
	var errs []string
	for err := range errCh {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 && g.logger != nil {
		g.logger.Debug("some files failed to extract", "errors", len(errs))
	}

	return allTags, nil
}

// buildPersonalization creates the personalization map for PageRank.
func (g *RepoMapGenerator) buildPersonalization(chatFiles, mentionedIdentifiers []string) map[string]float64 {
	pers := make(map[string]float64)

	// High bias for files actively in chat
	for _, file := range chatFiles {
		pers[file] = 3.0
	}

	// Medium bias for files matching mentioned identifiers
	for _, ident := range mentionedIdentifiers {
		g.mu.RLock()
		for _, file := range g.watchedFiles {
			if matchesPathComponents(filepath.Base(file), ident) {
				pers[file] += 1.5
			}
		}
		g.mu.RUnlock()
	}

	return pers
}

// calculateTargetTokens calculates the target token budget.
func calculateTargetTokens(config RepoMapConfig, chatFiles []string) int {
	target := config.MaxMapTokens

	// If no chat files and few identifiers mentioned, use MapMulNoFiles multiplier
	// to show more context (the user is exploring the repo)
	if len(chatFiles) == 0 {
		target = int(float64(target) * config.MapMulNoFiles)
	}

	// If many files mentioned, reduce budget per file
	if len(chatFiles) > 5 {
		target = target * 3 / 4
	}

	// Ensure minimum budget
	if target < 256 {
		target = 256
	}

	return target
}

// GenerateWithCache generates with explicit cache handling.
// This is useful when callers want to control caching behavior.
func (g *RepoMapGenerator) GenerateWithCache(ctx context.Context, chatFiles, mentionedIdentifiers []string, useCache bool) (*RenderedMap, error) {
	if !useCache {
		// Bypass cache by using a unique context
		oldCache := g.cache
		g.cache = NewMapCache(CacheConfig{EnableMemoryCache: false})
		defer func() { g.cache = oldCache }()
	}
	return g.Generate(ctx, chatFiles, mentionedIdentifiers)
}

// InvalidateCache invalidates all cached data.
func (g *RepoMapGenerator) InvalidateCache() {
	if g.cache != nil {
		g.cache.Clear()
	}
	if g.tagCache != nil {
		_ = g.tagCache.Clear()
	}
	if g.renderer != nil {
		g.renderer.ClearCache()
	}
}

// Stats returns statistics about the generator's caches.
func (g *RepoMapGenerator) Stats() (tagFiles int64, tagSize int64, mapEntries int, err error) {
	if g.tagCache != nil {
		tagFiles, tagSize, err = g.tagCache.Stats()
	}
	if g.cache != nil {
		mapEntries = g.cache.Size()
	}
	return tagFiles, tagSize, mapEntries, err
}

// ExtractIdentifiers extracts identifiers from a conversation message.
// This is a helper to convert raw text to identifiers for personalization.
func ExtractIdentifiers(text string) []string {
	// Simple extraction: find words that look like identifiers
	// (alphanumeric + underscore, not keywords)
	keywords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "can": true, "need": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"else": true, "when": true, "while": true, "for": true, "to": true,
		"from": true, "this": true, "that": true, "these": true, "those": true,
		"it": true, "its": true, "file": true, "files": true, "function": true,
		"functions": true, "class": true, "classes": true, "method": true,
		"methods": true, "variable": true, "variables": true, "use": true,
		"using": true, "used": true, "get": true, "set": true, "new": true,
		"create": true, "make": true, "add": true, "remove": true, "delete": true,
		"update": true, "change": true, "modify": true, "call": true, "called": true,
	}

	var identifiers []string
	words := strings.Fields(text)

	for _, word := range words {
		// Clean up the word
		word = strings.Trim(word, ".,;:!?()[]{}\"'`")
		word = strings.ToLower(word)

		// Skip short words and keywords
		if len(word) < 3 {
			continue
		}
		if keywords[word] {
			continue
		}

		// Check if it looks like an identifier (alphanumeric + underscore)
		isIdent := true
		for _, c := range word {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
				isIdent = false
				break
			}
		}

		if isIdent {
			identifiers = append(identifiers, word)
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, id := range identifiers {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}

	return unique
}
