package intake

// NOTE: gate tests for the github-intake-factory tree (leaf 00T) — F5.
// Intentionally RED: writeback.go (Contract 3: Outcome, WriteBack,
// NewWriteBack, Report) lands in leaf 03. The fakes pin the exact
// contract shapes: the effects key is
// EffectKey("intake_writeback", repo, ticketID, state-goalID) and the
// done label rides SetDoneLabel with the "factory:done" default.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"log/slog"
)

// ---------------------------------------------------------------------------
// WriteBack fakes.
// ---------------------------------------------------------------------------

// wbTokenStub is a static TokenSource for the write-back pool.
type wbTokenStub struct{}

func (wbTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_wb_testtoken", nil
}

// writebackFake serves the comment + label endpoints the WriteBack
// exercises and counts POSTs so the no-storm test can assert exact call
// counts. All state is mutex-guarded (httptest handlers run concurrently).
type writebackFake struct {
	*httptest.Server

	mu           sync.Mutex
	commentPosts int
	labelPosts   int
	commentBodie []string
	lastComment  map[string]any
}

func newWritebackFake() *writebackFake {
	f := &writebackFake{}
	mux := http.NewServeMux()
	// POST /repos/o/r/issues/42/comments
	mux.HandleFunc("/repos/o/r/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.commentPosts++
		f.commentBodie = append(f.commentBodie, fmt.Sprint(body["body"]))
		f.lastComment = body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":789,"body":"x","html_url":"https://github.com/o/r/issues/42#issuecomment-789"}`))
	})
	// POST /repos/o/r/issues/42/labels (AddLabelsToIssue)
	mux.HandleFunc("/repos/o/r/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		f.mu.Lock()
		f.labelPosts++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"factory:done"}]`))
	})
	f.Server = httptest.NewServer(mux)
	return f
}

func (f *writebackFake) counts() (comments, labels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commentPosts, f.labelPosts
}

func newWritebackPool(t *testing.T, fake *writebackFake) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  wbTokenStub{},
		BaseURL: fake.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// wbTicket is the ticket every write-back test targets (issue 42 on o/r —
// matching the fake's routes).
func wbTicket() Ticket {
	return Ticket{ID: "42", Repo: "o/r", Title: "t", Body: "mandate"}
}

// ---------------------------------------------------------------------------
// F5: comment path, label path, storm guard (leaf 03 Tasks 1-2)
// ---------------------------------------------------------------------------

// TestWriteBack_Report_Comment: Report posts exactly one comment via the
// pool; the effects key is Contract 3's composition; the receipt id/url
// are surfaced to the caller.
func TestWriteBack_Report_Comment(t *testing.T) {
	fake := newWritebackFake()
	ledger := effects.NewMemoryLedger()
	wb := NewWriteBack(newWritebackPool(t, fake), ledger,
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	outcome := Outcome{GoalID: "goal-abc", State: "healthy", Detail: "mandate satisfied"}
	commentID, url, err := wb.Report(context.Background(), wbTicket(), outcome)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}

	comments, _ := fake.counts()
	if comments != 1 {
		t.Fatalf("fake saw %d comment POSTs, want exactly 1", comments)
	}
	if commentID != 789 {
		t.Errorf("commentID = %d, want 789 (the fake's receipt id)", commentID)
	}
	if url == "" {
		t.Errorf("Report returned an empty url; want the receipt html_url")
	}

	// Body template: "[meept factory] goal <GoalID>: <State>\n<Detail>".
	fake.mu.Lock()
	body := ""
	if len(fake.commentBodie) > 0 {
		body = fake.commentBodie[0]
	}
	fake.mu.Unlock()
	wantSubstr := "[meept factory] goal goal-abc: healthy"
	if len(body) < len(wantSubstr) || body[:len(wantSubstr)] != wantSubstr {
		t.Errorf("comment body = %q, want prefix %q", body, wantSubstr)
	}

	// Contract 3 key composition pin: the ledger record exists under
	// EffectKey("intake_writeback", repo, ticketID, state-goalID).
	wantKey := effects.EffectKey("intake_writeback", "o/r", "42", "healthy-goal-abc")
	if rec, err := ledger.Get(context.Background(), wantKey); err != nil || rec == nil {
		t.Errorf("no ledger record under the contract key %s: rec=%v err=%v", wantKey, rec, err)
	} else {
		// Receipt shape: {"id":..., "html_url":...}.
		var rc map[string]any
		if err := json.Unmarshal(rec.Receipt, &rc); err != nil {
			t.Errorf("stored receipt is not JSON object shape: %v (%s)", err, string(rec.Receipt))
		} else if _, ok := rc["html_url"]; !ok {
			t.Errorf("stored receipt missing html_url: %s", string(rec.Receipt))
		}
	}
}

// TestWriteBack_HealthyAddsLabel: State "healthy" adds the done label;
// other states never touch the labels endpoint (F5, leaf 03 Task 2).
func TestWriteBack_HealthyAddsLabel(t *testing.T) {
	fake := newWritebackFake()
	wb := NewWriteBack(newWritebackPool(t, fake), effects.NewMemoryLedger(),
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	if _, _, err := wb.Report(context.Background(), wbTicket(),
		Outcome{GoalID: "g1", State: "healthy", Detail: "d"}); err != nil {
		t.Fatalf("healthy Report: %v", err)
	}
	comments, labels := fake.counts()
	if comments != 1 || labels != 1 {
		t.Fatalf("healthy outcome: comments=%d labels=%d, want 1/1 (done label added)", comments, labels)
	}

	// A different state must NOT add labels — a fresh fake.
	fake2 := newWritebackFake()
	wb2 := NewWriteBack(newWritebackPool(t, fake2), effects.NewMemoryLedger(),
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)
	if _, _, err := wb2.Report(context.Background(), wbTicket(),
		Outcome{GoalID: "g1", State: "at_risk", Detail: "d"}); err != nil {
		t.Fatalf("at_risk Report: %v", err)
	}
	comments2, labels2 := fake2.counts()
	if comments2 != 1 || labels2 != 0 {
		t.Fatalf("at_risk outcome: comments=%d labels=%d, want 1/0 (no label off-healthy)", comments2, labels2)
	}
}

// TestWriteBack_NoStorm: a second Report with the same (goalID, state)
// collides on the completed effects key and makes NO second POST — the
// receipt is reused, never re-posted (F5).
func TestWriteBack_NoStorm(t *testing.T) {
	fake := newWritebackFake()
	ledger := effects.NewMemoryLedger()
	wb := NewWriteBack(newWritebackPool(t, fake), ledger,
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	outcome := Outcome{GoalID: "g-storm", State: "healthy", Detail: "d"}
	id1, url1, err := wb.Report(context.Background(), wbTicket(), outcome)
	if err != nil {
		t.Fatalf("first Report: %v", err)
	}
	id2, url2, err := wb.Report(context.Background(), wbTicket(), outcome)
	if err != nil {
		t.Fatalf("second Report (same goal+state): %v", err)
	}

	comments, labels := fake.counts()
	if comments != 1 {
		t.Fatalf("fake saw %d comment POSTs after a repeat Report, want exactly 1 (no storm)", comments)
	}
	if labels != 1 {
		t.Fatalf("fake saw %d label POSTs after a repeat Report, want exactly 1", labels)
	}
	if id1 != id2 || url1 != url2 {
		t.Errorf("receipts differ across the repeat Report: (%d,%s) vs (%d,%s)", id1, url1, id2, url2)
	}
	_ = strconv.Itoa // keeps strconv referenced for the receipt-id formatting assertions leaf 03 extends additively
}
