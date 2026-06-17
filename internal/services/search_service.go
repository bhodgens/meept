package services

import (
	"context"
	"strings"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"sort"
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
