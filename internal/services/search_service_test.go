package services

import (
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/session"
)

func TestSearchSemantic_KeywordFallback(t *testing.T) {
	t.Parallel()

	// With a nil memory manager, semantic mode is unavailable, so the service
	// must fall back to keyword search.
	store := session.NewMemoryStore(nil)
	svc := NewSearchService(store, nil, nil, nil)

	// Seed the store so keyword search has something to find.
	sess, err := store.Create("golang testing session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "how do I write golang tests"},
	}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	resp, err := svc.SearchSemantic(t.Context(), SemanticSearchRequest{
		Query: "golang",
	})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if resp.Mode != "keyword" {
		t.Errorf("Mode = %q, want %q", resp.Mode, "keyword")
	}
	// Keyword fallback searches session message content.
	if len(resp.Results) == 0 {
		t.Error("expected at least one keyword result")
	}
}

func TestSearchSemantic_EmptyQuery(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	svc := NewSearchService(store, nil, nil, nil)

	_, err := svc.SearchSemantic(t.Context(), SemanticSearchRequest{
		Query: "",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSearchSemantic_ScopeFilter(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	svc := NewSearchService(store, nil, nil, nil)

	// Seed a session with a matching message so the "messages" scope would
	// return hits.
	sess, _ := store.Create("alpha session")
	_ = store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "alpha keyword here"},
	})

	// Restrict scope to "tasks" only; there are no task results since
	// taskRegistry is nil.
	resp, err := svc.SearchSemantic(t.Context(), SemanticSearchRequest{
		Query: "alpha",
		Scope: "tasks",
	})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	for _, r := range resp.Results {
		if r.Type == "message" || r.Type == "session" {
			t.Errorf("scope filter leaked: got type %q in results", r.Type)
		}
	}
}

func TestSearch_KeywordBasic(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	svc := NewSearchService(store, nil, nil, nil)

	// Search() keyword matches session names/descriptions, and the memory
	// store's List() only returns sessions that have an assistant message.
	sess, _ := store.Create("beta session")
	_ = store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "response"},
	})

	results, err := svc.Search(t.Context(), SearchRequest{Query: "beta"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result matching session name 'beta session'")
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	t.Parallel()

	svc := NewSearchService(nil, nil, nil, nil)
	_, err := svc.Search(t.Context(), SearchRequest{Query: ""})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

// M9 pin: SearchSemantic with scope=sessions must find a session by its
// title. The "sessions" alias maps to the messages lane, but the session
// title/description keyword match must still fire for it (its own comment
// promises "all/sessions scope"), not only for "all".
func TestSearchSemantic_SessionsScopeFindsSessionByTitle(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	svc := NewSearchService(store, nil, nil, nil)

	// The query word appears ONLY in the session title — no message
	// content can match, so a hit must come from the title lane.
	sess, err := store.Create("zephyr planning session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "unrelated content"},
		{Role: "assistant", Content: "still unrelated"},
	}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	resp, err := svc.SearchSemantic(t.Context(), SemanticSearchRequest{
		Query: "zephyr",
		Scope: "sessions",
	})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	found := false
	for _, r := range resp.Results {
		if r.Type == "session" && r.ID == sess.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("scope=sessions did not return the session titled %q (results=%+v)", sess.Name, resp.Results)
	}
}

// The title lane must NOT fire for scope=messages: that scope is message
// content only, and widening it would change its contract.
func TestSearchSemantic_MessagesScopeExcludesSessionTitles(t *testing.T) {
	t.Parallel()

	store := session.NewMemoryStore(nil)
	svc := NewSearchService(store, nil, nil, nil)

	sess, err := store.Create("quokka planning session")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SaveMessages(sess.ID, []session.Message{
		{Role: "user", Content: "unrelated content"},
	}); err != nil {
		t.Fatalf("SaveMessages: %v", err)
	}

	resp, err := svc.SearchSemantic(t.Context(), SemanticSearchRequest{
		Query: "quokka",
		Scope: "messages",
	})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	for _, r := range resp.Results {
		if r.Type == "session" {
			t.Errorf("scope=messages leaked a session-title result: %+v", r)
		}
	}
}
