package intake

// Pin tests for the bughunt D7/D8 fixes. TESTS ONLY.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
)

// ---------------------------------------------------------------------------
// D7: label failure -> effects not completed -> next poll retries label
// ---------------------------------------------------------------------------

// d7FlakyLabelFake serves the comment endpoint normally but FAILS the
// first N label POSTs with a 500. It EMBEDS writebackFake so the
// committed newWritebackPool helper accepts it.
type d7FlakyLabelFake struct {
	*writebackFake
	mu            sync.Mutex
	labelFailures int // remaining 500s on the label endpoint
	labelPosts    int
}

func newD7FlakyLabelFake(initialFailures int) *d7FlakyLabelFake {
	f := &d7FlakyLabelFake{
		writebackFake: newWritebackFake(),
		labelFailures: initialFailures,
	}
	// Wrap the embedded fake's mux: serve the flaky label handler on a
	// new server, delegating comments to the embedded fake's handler
	// behavior via its own server URL is unnecessary — the writebackFake
	// constructor built its own mux; we just need the label endpoint to
	// override. Serve a mux that (a) proxies comments to the embedded
	// server and (b) serves flaky labels locally.
	inner := f.URL
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		// Delegate to the embedded fake (it records the count).
		resp, err := http.Post(inner+"/repos/o/r/issues/42/comments", "application/json", r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
	mux.HandleFunc("/repos/o/r/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.labelPosts++
		fail := f.labelFailures > 0
		if fail {
			f.labelFailures--
		}
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"factory:done"}]`))
	})
	f.Server = httptest.NewServer(mux)
	return f
}

func (f *d7FlakyLabelFake) counts() (comments, labels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commentPosts, f.labelPosts
}

func TestLabelFailureLeavesEffectRetryable(t *testing.T) {
	fake := newD7FlakyLabelFake(1) // the FIRST label call fails
	ledger := effects.NewMemoryLedger()
	pool := newWritebackPool(t, fake.writebackFake)
	wb := NewWriteBack(pool, ledger,
		githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	o := Outcome{GoalID: "goal-d7", State: "healthy", Detail: "done"}
	ticket := wbTicket()

	if _, _, err := wb.Report(context.Background(), ticket, o); err == nil {
		t.Fatal("Report succeeded despite the label endpoint failing — the label failure must surface")
	}

	key := effects.EffectKey("intake_writeback", "o/r", "42", "healthy-goal-d7")
	rec, err := ledger.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("effect get %s: %v", key, err)
	}
	if rec.State == effects.StateCompleted {
		t.Fatal("effect COMPLETED despite a failed label call — the D7 regression (a failed label was permanent)")
	}

	// Second poll (label endpoint healthy now): the effect must complete.
	if _, _, err := wb.Report(context.Background(), ticket, o); err != nil {
		t.Fatalf("retry Report: %v", err)
	}
	rec, err = ledger.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("effect get after retry: %v", err)
	}
	if rec.State != effects.StateCompleted {
		t.Fatalf("effect state after the healthy retry = %q, want completed", rec.State)
	}
	// The comment must NOT be re-posted (the receipt was reused).
	comments, labels := fake.counts()
	if comments != 1 {
		t.Fatalf("comments posted = %d, want 1 (the retry must reuse the receipt, not re-comment)", comments)
	}
	if labels < 2 {
		t.Fatalf("label posts = %d, want >= 2 (the retry must re-add the label)", labels)
	}
}

// ---------------------------------------------------------------------------
// D8: concurrent Poll calls — no race, single-flight honored
// ---------------------------------------------------------------------------

func TestPollConcurrentSingleFlight(t *testing.T) {
	// A slow fake: the endpoint blocks until released, guaranteeing the
	// first Poll is still in flight when the other goroutines fire.
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  intakeTokenStub{},
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)

	p := NewGitHubPoller(pool, githubsvc.OwnerRepo{Owner: "o", Repo: "r"},
		Filter{}, slog.DiscardHandler)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, errs[idx] = p.Poll(context.Background())
		}(i)
	}
	close(start)
	// Give the goroutines a beat to pile onto the in-flight poll, then
	// release the endpoint.
	var inFlight bool
	for i := 0; i < 50 && !inFlight; i++ {
		p.pollMu.Lock()
		inFlight = p.pollRunning
		p.pollMu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	close(release)
	wg.Wait()

	var (
		realPolls int
		skips     int
	)
	for i := range n {
		if errors.Is(errs[i], ErrPollInProgress) {
			skips++
			continue
		}
		if errs[i] != nil {
			t.Fatalf("poll %d: %v", i, errs[i])
		}
		realPolls++
	}
	if realPolls == 0 {
		t.Fatal("every concurrent Poll skipped; at least one must run")
	}
	if skips == 0 {
		t.Fatal("no single-flight skips observed; overlapping polls paged concurrently (the D8 race)")
	}
}

// TestPollWatermarkAdvancesMonotonic pins the mutex-guarded watermark
// write: after one successful poll the watermark is recorded (D8's
// unsynchronized fields are gone).
func TestPollWatermarkAdvancesMonotonic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]ghIssue{intakeIssue(7, "t", time.Now())})
	}))
	defer srv.Close()

	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  intakeTokenStub{},
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)

	p := NewGitHubPoller(pool, githubsvc.OwnerRepo{Owner: "o", Repo: "r"},
		Filter{}, slog.DiscardHandler)
	tickets, err := p.Poll(context.Background())
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("tickets = %d, want 1", len(tickets))
	}
	p.pollMu.Lock()
	had := p.hasWatermark
	p.pollMu.Unlock()
	if !had {
		t.Fatal("watermark not advanced after a successful poll")
	}
}
