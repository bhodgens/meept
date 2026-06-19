package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// SearchResult represents a single search result from any scope.
type SearchResult struct {
	Type      string  `json:"type"` // "session", "task", "memory", "plan"
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	Relevance float64 `json:"relevance"`
}

// SearchRequest contains parameters for a cross-scope search.
type SearchRequest struct {
	Query string `json:"query"`
	Scope string `json:"scope,omitempty"` // "sessions", "tasks", "memories", "plans", or "all" (default)
	Limit int    `json:"limit,omitempty"` // max results per scope before ranking (default 20 total)
}

// SearchService performs full-text search across sessions, tasks, memories, and plans.
type SearchService struct {
	sessionStore session.Store
	taskRegistry *task.Registry
	memoryMgr    *memory.Manager
	planStore    plan.PlanStore
}

// NewSearchService creates a search service. Nil dependencies are allowed;
// the corresponding scopes will simply return no results.
func NewSearchService(
	sessionStore session.Store,
	taskRegistry *task.Registry,
	memoryMgr *memory.Manager,
	planStore plan.PlanStore,
) *SearchService {
	return &SearchService{
		sessionStore: sessionStore,
		taskRegistry: taskRegistry,
		memoryMgr:    memoryMgr,
		planStore:    planStore,
	}
}

// Search searches across the requested scopes and returns combined results ranked by relevance.
func (s *SearchService) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	if req.Query == "" {
		return nil, wrapError("search", "Search", ErrInvalidInput)
	}

	if req.Scope == "" {
		req.Scope = "all"
	}

	totalLimit := req.Limit
	if totalLimit <= 0 {
		totalLimit = 20
	}

	var allResults []SearchResult

	query := strings.ToLower(req.Query)

	if req.Scope == "all" || req.Scope == "sessions" {
		allResults = append(allResults, s.searchSessions(ctx, query)...)
	}
	if req.Scope == "all" || req.Scope == "tasks" {
		allResults = append(allResults, s.searchTasks(ctx, query)...)
	}
	if req.Scope == "all" || req.Scope == "memories" {
		allResults = append(allResults, s.searchMemories(ctx, query)...)
	}
	if req.Scope == "all" || req.Scope == "plans" {
		allResults = append(allResults, s.searchPlans(ctx, query)...)
	}

	// Sort by relevance descending, then by type for stable ordering
	sort.SliceStable(allResults, func(i, j int) bool {
		if allResults[i].Relevance != allResults[j].Relevance {
			return allResults[i].Relevance > allResults[j].Relevance
		}
		return allResults[i].Type < allResults[j].Type
	})

	if len(allResults) > totalLimit {
		allResults = allResults[:totalLimit]
	}

	return allResults, nil
}

// searchSessions searches session names and descriptions by keyword.
func (s *SearchService) searchSessions(ctx context.Context, query string) []SearchResult {
	if s.sessionStore == nil {
		return nil
	}

	sessions, err := s.sessionStore.List()
	if err != nil {
		return nil
	}

	var results []SearchResult
	for _, sess := range sessions {
		score := keywordRelevance(query, sess.Name, sess.Description)
		if score > 0 {
			snippet := sess.Description
			if snippet == "" {
				snippet = sess.Name
			}
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			results = append(results, SearchResult{
				Type:      "session",
				ID:        sess.ID,
				Title:     sess.Name,
				Snippet:   snippet,
				Relevance: score,
			})
		}
	}
	return results
}

// searchTasks searches task names and descriptions by keyword.
func (s *SearchService) searchTasks(ctx context.Context, query string) []SearchResult {
	if s.taskRegistry == nil {
		return nil
	}

	tasks, err := s.taskRegistry.List(ctx, nil, 100)
	if err != nil {
		return nil
	}

	var results []SearchResult
	for _, t := range tasks {
		score := keywordRelevance(query, t.Name, t.Description)
		if score > 0 {
			snippet := t.Description
			if snippet == "" {
				snippet = t.Name
			}
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			results = append(results, SearchResult{
				Type:      "task",
				ID:        t.ID,
				Title:     t.Name,
				Snippet:   snippet,
				Relevance: score,
			})
		}
	}
	return results
}

// searchMemories uses the memory manager's FTS5 search.
func (s *SearchService) searchMemories(ctx context.Context, query string) []SearchResult {
	if s.memoryMgr == nil {
		return nil
	}

	memResults, err := s.memoryMgr.Search(ctx, memory.MemoryQuery{
		Query: query,
		Limit: 20,
	})
	if err != nil {
		return nil
	}

	var results []SearchResult
	for _, mr := range memResults {
		snippet := mr.Memory.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		title := mr.Memory.Category
		if title == "" {
			title = string(mr.Memory.Type)
		}
		results = append(results, SearchResult{
			Type:      "memory",
			ID:        mr.Memory.ID,
			Title:     title,
			Snippet:   snippet,
			Relevance: mr.RelevanceScore,
		})
	}
	return results
}

// searchPlans searches plan titles and descriptions by keyword.
func (s *SearchService) searchPlans(ctx context.Context, query string) []SearchResult {
	if s.planStore == nil {
		return nil
	}

	plans, err := s.planStore.ListPlans(ctx, "", 100)
	if err != nil {
		return nil
	}

	var results []SearchResult
	for _, p := range plans {
		score := keywordRelevance(query, p.Title, p.Description)
		if score > 0 {
			snippet := p.Description
			if snippet == "" {
				snippet = p.Title
			}
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			results = append(results, SearchResult{
				Type:      "plan",
				ID:        p.ID,
				Title:     p.Title,
				Snippet:   snippet,
				Relevance: score,
			})
		}
	}
	return results
}

// keywordRelevance computes a simple relevance score [0,1] for a query against
// title and description fields. It checks for exact substring matches and
// weights title matches higher than description matches.
func keywordRelevance(query, title, desc string) float64 {
	lowerTitle := strings.ToLower(title)
	lowerDesc := strings.ToLower(desc)

	if lowerTitle == "" && lowerDesc == "" {
		return 0
	}

	var score float64

	// Title match (higher weight)
	if strings.Contains(lowerTitle, query) {
		score += 0.8
		// Exact title match gets the highest score
		if lowerTitle == query {
			score += 0.2
		}
		// Word boundary match bonus
		if strings.Contains(" "+lowerTitle+" ", " "+query+" ") {
			score += 0.1
		}
	}

	// Description match (lower weight)
	if strings.Contains(lowerDesc, query) {
		score += 0.4
	}

	return score
}

// SemanticSearchRequest contains parameters for a semantic+keyword search.
type SemanticSearchRequest struct {
	Query string `json:"query"`
	Scope string `json:"scope,omitempty"` // "all", "sessions", "messages", "memories", "tasks", "plans"
	Limit int    `json:"limit,omitempty"`
}

// SemanticSearchResponse is the result of a SearchSemantic call.
type SemanticSearchResponse struct {
	Results []SearchResult `json:"results"`
	Mode    string         `json:"mode"` // "semantic" or "keyword" (fallback)
	Err     string         `json:"err,omitempty"`
}

// SearchSemantic performs a semantic search across the requested scopes.
// If no embedding provider is available or the embedding call fails, it
// falls back to keyword search and sets Mode = "keyword" in the response.
func (s *SearchService) SearchSemantic(ctx context.Context, req SemanticSearchRequest) (SemanticSearchResponse, error) {
	if req.Query == "" {
		return SemanticSearchResponse{}, wrapError("search", "SearchSemantic", ErrInvalidInput)
	}
	if req.Scope == "" {
		req.Scope = "all"
	}
	// "sessions" and "messages" both map to the session message store;
	// accept either alias.
	scope := req.Scope
	if scope == "sessions" {
		scope = "messages"
	}
	totalLimit := req.Limit
	if totalLimit <= 0 {
		totalLimit = 20
	}

	// Determine whether semantic mode is available.
	var queryEmb []float32
	mode := "semantic"
	semanticOK := true
	if s.memoryMgr == nil {
		semanticOK = false
	} else if embedder := s.memoryMgr.Embedder(); embedder == nil {
		semanticOK = false
	} else {
		emb, err := embedder.GenerateEmbedding(ctx, req.Query)
		if err != nil || len(emb) == 0 {
			semanticOK = false
		} else {
			queryEmb = emb
		}
	}
	if !semanticOK {
		mode = "keyword"
	}

	var allResults []SearchResult
	lowerQuery := strings.ToLower(req.Query)

	// messages scope: session message content
	if scope == "all" || scope == "messages" {
		if semanticOK {
			ms, err := s.sessionStore.SearchMessagesSemantic(ctx, queryEmb, totalLimit)
			if err == nil {
				allResults = append(allResults, sessionResultsToSearchResults(ms)...)
			} else if !errors.Is(err, session.ErrSemanticUnavailable) {
				// Unexpected error — log via empty/zero and fall through to keyword
			}
		}
		// Always also run keyword on messages: FTS covers everything and
		// complements vector hits with exact-match boosts.
		if s.sessionStore != nil {
			ms, err := s.sessionStore.SearchMessages(ctx, req.Query, totalLimit)
			if err == nil {
				allResults = append(allResults, sessionResultsToSearchResults(ms)...)
			}
		}
		// Also include session name/description keyword matches for the
		// "all"/"sessions" scope so users can find a session by title.
		if scope == "all" && s.sessionStore != nil {
			allResults = append(allResults, s.searchSessions(ctx, lowerQuery)...)
		}
	}

	if scope == "all" || scope == "memories" {
		if semanticOK && s.memoryMgr != nil {
			mem, err := s.memoryMgr.SearchSemantic(ctx, req.Query, totalLimit)
			if err == nil {
				allResults = append(allResults, memoryResultsToSearchResults(mem)...)
			}
		} else if s.memoryMgr != nil {
			allResults = append(allResults, s.searchMemories(ctx, lowerQuery)...)
		}
	}

	if scope == "all" || scope == "tasks" {
		allResults = append(allResults, s.searchTasks(ctx, lowerQuery)...)
	}

	if scope == "all" || scope == "plans" {
		allResults = append(allResults, s.searchPlans(ctx, lowerQuery)...)
	}

	// De-duplicate by (Type, ID) keeping the highest relevance.
	allResults = dedupeAndSort(allResults)
	if len(allResults) > totalLimit {
		allResults = allResults[:totalLimit]
	}

	return SemanticSearchResponse{
		Results: allResults,
		Mode:    mode,
	}, nil
}

func sessionResultsToSearchResults(ms []session.MessageSearchResult) []SearchResult {
	if len(ms) == 0 {
		return nil
	}
	out := make([]SearchResult, 0, len(ms))
	for _, m := range ms {
		title := m.Role
		if title == "" {
			title = "message"
		}
		snippet := m.Snippet
		if snippet == "" {
			snippet = m.Content
		}
		out = append(out, SearchResult{
			Type:      "message",
			ID:        fmt.Sprintf("%s:%d", m.SessionID, m.MessageID),
			Title:     title,
			Snippet:   snippet,
			Relevance: clampRelevance(m.Relevance),
		})
	}
	return out
}

func memoryResultsToSearchResults(mem []memory.MemoryResult) []SearchResult {
	if len(mem) == 0 {
		return nil
	}
	out := make([]SearchResult, 0, len(mem))
	for _, mr := range mem {
		snippet := mr.Memory.Content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		title := mr.Memory.Category
		if title == "" {
			title = string(mr.Memory.Type)
		}
		out = append(out, SearchResult{
			Type:      "memory",
			ID:        mr.Memory.ID,
			Title:     title,
			Snippet:   snippet,
			Relevance: clampRelevance(mr.RelevanceScore),
		})
	}
	return out
}

func clampRelevance(r float64) float64 {
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

func dedupeAndSort(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}
	byKey := make(map[string]int, len(results))
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		key := r.Type + ":" + r.ID
		if idx, ok := byKey[key]; ok {
			if r.Relevance > out[idx].Relevance {
				out[idx] = r
			}
		} else {
			byKey[key] = len(out)
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Relevance != out[j].Relevance {
			return out[i].Relevance > out[j].Relevance
		}
		return out[i].Type < out[j].Type
	})
	return out
}
