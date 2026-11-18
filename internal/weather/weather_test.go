package weather

// 00T-weather gate — plan 20261110-situational leaf 03-weather/00T (§W1-§W3).
// TESTS ONLY: this file is the executable spec for leaf 03/01. RED posture is
// expected until the implementation lands (undefined symbols below).
//
// Pinned seams — the implementation leaf MUST provide EXACTLY these symbols
// (frozen contract, master.md "internal/weather package" block, plus the
// test seams this gate needs):
//
//	type Outlook struct {
//	    NowC, TodayHighC, TodayLowC, WindKph float64
//	    RainProbToday                        int // 0-100
//	    Fetched                              time.Time
//	}
//	type Fetcher interface {
//	    Fetch(ctx context.Context, lat, lon float64) (Outlook, error)
//	}
//	func AlertsFor(trades []string, o Outlook) []string
//
//	const DefaultBaseURL = "https://api.open-meteo.com" // production base URL
//	const DefaultTTL     = 30 * time.Minute             // cache TTL (frozen)
//	const DefaultTimeout = 10 * time.Second             // fetch timeout (frozen)
//
//	// Config is the base-URL test seam. Empty BaseURL -> DefaultBaseURL;
//	// nil Client -> a client built with DefaultTimeout. The production
//	// client CONSTRUCTION must follow the SSRF-guard pattern
//	// (internal/tools/builtin/web_fetch.go:158 + ssrf.go) at the wiring
//	// site — NOT required by these tests, which only ever talk to an
//	// httptest server (guard belongs to the impl review checklist).
//	type Config struct {
//	    BaseURL string
//	    Client  *http.Client
//	}
//	func New(cfg Config) *OpenMeteo // *OpenMeteo implements Fetcher
//
//	// NewCache wraps any Fetcher with the frozen cache contract:
//	// per-(lat,lon) entries keyed with lat/lon ROUNDED TO 0.01 DEGREES
//	// (two shops in one town share a fetch), TTL expiry, singleflight
//	// (golang.org/x/sync v0.24.0 IS in go.mod — use
//	// golang.org/x/sync/singleflight), and stale-on-error: a fetch that
//	// fails AFTER the TTL expires returns the last good Outlook with no
//	// user-facing error and a log line containing "stale".
//	// A nil logger falls back to slog.Default().
//	func NewCache(f Fetcher, ttl time.Duration, logger *slog.Logger) *Cache
//	// *Cache implements Fetcher.
//
// Pinned upstream wire shape (canned JSON the httptest server serves;
// request: GET {BaseURL}/v1/forecast?latitude=..&longitude=..&...):
//
//	{"current": {"temperature_2m": 3.0},
//	 "daily": {"temperature_2m_max": [12.0],
//	           "temperature_2m_min": [-2.0],
//	           "precipitation_probability_max": [60],
//	           "wind_speed_10m_max": [55.0]}}
//
// Field mapping (exact): NowC <- current.temperature_2m;
// TodayHighC <- daily.temperature_2m_max[0]; TodayLowC <-
// daily.temperature_2m_min[0]; WindKph <- daily.wind_speed_10m_max[0];
// RainProbToday <- daily.precipitation_probability_max[0] (int);
// Fetched <- non-zero (set by the fetcher).

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

const cannedOpenMeteo = `{"current":{"temperature_2m":3.0},` +
	`"daily":{"temperature_2m_max":[12.0],"temperature_2m_min":[-2.0],` +
	`"precipitation_probability_max":[60],"wind_speed_10m_max":[55.0]}}`

// newCannedServer serves cannedOpenMeteo and counts hits.
func newCannedServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(cannedOpenMeteo))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func newTestFetcher(t *testing.T, url string) Fetcher {
	t.Helper()
	return New(Config{BaseURL: url, Client: &http.Client{Timeout: 2 * time.Second}})
}

// discardLogger keeps test output clean while still exercising the logger
// seam.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// captureHandler records "LEVEL message key=value" lines (weather-package
// copy of the daemon ledger_integrity_test idiom; distinct name so sibling
// gates cannot collide).
type captureHandler struct {
	mu    sync.Mutex
	lines []string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Level.String())
	sb.WriteString(" ")
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.Key)
		sb.WriteString("=")
		sb.WriteString(a.Value.String())
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines = append(h.lines, sb.String())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) contains(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// stubFetcher is an injectable Fetcher for cache tests. mode: "ok" returns a
// fixed Outlook, "fail" returns an error, "slow" blocks on gate. hits counts
// inner executions.
type stubFetcher struct {
	mu       sync.Mutex
	hits     int
	mode     string // "ok" | "fail"
	gate     chan struct{}
	started  chan struct{}
	outlookF Outlook
}

func (s *stubFetcher) Fetch(ctx context.Context, lat, lon float64) (Outlook, error) {
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.gate != nil {
		<-s.gate
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits++
	if s.mode == "fail" {
		return Outlook{}, fmt.Errorf("stub: upstream down")
	}
	return s.outlookF, nil
}

func (s *stubFetcher) hitCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

// hasAlert reports whether want appears exactly once in got.
func exactlyOnce(got []string, want string) bool {
	n := 0
	for _, g := range got {
		if g == want {
			n++
		}
	}
	return n == 1
}

// ---------------------------------------------------------------------------
// §W1 — fetch parse: field mapping, bad JSON, timeout, query shape
// ---------------------------------------------------------------------------

func TestFetch_FieldMapping(t *testing.T) {
	srv, hits := newCannedServer(t)
	f := newTestFetcher(t, srv.URL)

	o, err := f.Fetch(context.Background(), 44.0, -103.2)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hits = %d, want 1", hits.Load())
	}
	if o.NowC != 3.0 {
		t.Errorf("NowC = %v, want 3.0", o.NowC)
	}
	if o.TodayHighC != 12.0 {
		t.Errorf("TodayHighC = %v, want 12.0", o.TodayHighC)
	}
	if o.TodayLowC != -2.0 {
		t.Errorf("TodayLowC = %v, want -2.0", o.TodayLowC)
	}
	if o.WindKph != 55.0 {
		t.Errorf("WindKph = %v, want 55.0", o.WindKph)
	}
	if o.RainProbToday != 60 {
		t.Errorf("RainProbToday = %d, want 60", o.RainProbToday)
	}
	if o.Fetched.IsZero() {
		t.Error("Fetched must be set to fetch time")
	}
}

func TestFetch_QueryShape(t *testing.T) {
	var gotPath string
	var gotLat, gotLon float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		q := r.URL.Query()
		gotLat, _ = strconv.ParseFloat(q.Get("latitude"), 64)
		gotLon, _ = strconv.ParseFloat(q.Get("longitude"), 64)
		_, _ = w.Write([]byte(cannedOpenMeteo))
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(t, srv.URL)
	if _, err := f.Fetch(context.Background(), 44.5, -103.25); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotPath != "/v1/forecast" {
		t.Errorf("path = %q, want /v1/forecast", gotPath)
	}
	if gotLat != 44.5 || gotLon != -103.25 {
		t.Errorf("coords = (%v, %v), want (44.5, -103.25)", gotLat, gotLon)
	}
}

func TestFetch_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"current": trunca`))
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(t, srv.URL)
	if _, err := f.Fetch(context.Background(), 44.0, -103.0); err == nil {
		t.Fatal("malformed JSON must return an error")
	}
}

func TestFetch_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(t, srv.URL)
	if _, err := f.Fetch(context.Background(), 44.0, -103.0); err == nil {
		t.Fatal("HTTP 500 must return an error")
	}
}

func TestFetch_ContextTimeoutHonored(t *testing.T) {
	// Frozen contract: 10s client timeout (DefaultTimeout). The test proves
	// ctx cancellation propagates to the in-flight request: a handler that
	// outlives a short ctx must NOT make Fetch block.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(cannedOpenMeteo))
	}))
	t.Cleanup(srv.Close)

	f := newTestFetcher(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := f.Fetch(ctx, 44.0, -103.0)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expired ctx must yield an error")
	}
	if elapsed > 1*time.Second {
		t.Errorf("Fetch blocked %v after ctx expiry; ctx must abort the request", elapsed)
	}
}

func TestDefaultConstants(t *testing.T) {
	// Frozen numbers from the contract block.
	if DefaultTTL != 30*time.Minute {
		t.Errorf("DefaultTTL = %v, want 30m", DefaultTTL)
	}
	if DefaultTimeout != 10*time.Second {
		t.Errorf("DefaultTimeout = %v, want 10s", DefaultTimeout)
	}
	if DefaultBaseURL != "https://api.open-meteo.com" {
		t.Errorf("DefaultBaseURL = %q, want https://api.open-meteo.com", DefaultBaseURL)
	}
}

// ---------------------------------------------------------------------------
// §W2 — cache: TTL hit counter, singleflight, stale-on-error, 0.01° key
// ---------------------------------------------------------------------------

func TestCache_SecondCallWithinTTLServesCached(t *testing.T) {
	srv, hits := newCannedServer(t)
	c := NewCache(newTestFetcher(t, srv.URL), time.Minute, discardLogger())

	ctx := context.Background()
	o1, err := c.Fetch(ctx, 44.0, -103.0)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	o2, err := c.Fetch(ctx, 44.0, -103.0)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("upstream hits = %d, want 1 (two calls inside TTL = one fetch)", got)
	}
	if o1.NowC != o2.NowC || o1.TodayLowC != o2.TodayLowC {
		t.Errorf("cached value drifted: %+v vs %+v", o1, o2)
	}
}

func TestCache_ConcurrentBurstSingleflight(t *testing.T) {
	stub := &stubFetcher{
		mode:     "ok",
		gate:     make(chan struct{}),
		started:  make(chan struct{}, 8),
		outlookF: Outlook{NowC: 5, RainProbToday: 10},
	}
	c := NewCache(stub, time.Minute, discardLogger())

	var wg sync.WaitGroup
	results := make([]Outlook, 5)
	for i := range 5 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, err := c.Fetch(context.Background(), 44.0, -103.0)
			if err != nil {
				t.Errorf("burst Fetch[%d]: %v", i, err)
			}
			results[i] = o
		}(i)
	}
	// Singleflight means ONLY the leader enters the inner fetcher; the
	// other four join its flight. Wait for the leader to be blocked on the
	// gate, give the followers time to join, then release. Without
	// singleflight the blocked leader would leave no cache entry and each
	// follower would fire its own upstream call (hits > 1 => RED).
	select {
	case <-stub.started:
	case <-time.After(2 * time.Second):
		t.Fatal("burst never entered the inner fetcher")
	}
	time.Sleep(100 * time.Millisecond)
	close(stub.gate)
	wg.Wait()

	if got := stub.hitCount(); got != 1 {
		t.Errorf("singleflight: inner fetcher ran %d times, want 1 for a concurrent burst on one key", got)
	}
	for i, o := range results {
		if o.NowC != 5 {
			t.Errorf("results[%d] = %+v, want the shared Outlook", i, o)
		}
	}
}

func TestCache_DistantKeysFetchSeparately(t *testing.T) {
	srv, hits := newCannedServer(t)
	c := NewCache(newTestFetcher(t, srv.URL), time.Minute, discardLogger())

	if _, err := c.Fetch(context.Background(), 44.0, -103.0); err != nil {
		t.Fatalf("Fetch A: %v", err)
	}
	if _, err := c.Fetch(context.Background(), 48.0, -90.0); err != nil {
		t.Fatalf("Fetch B: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("upstream hits = %d, want 2 (distinct locations)", got)
	}
}

func TestCache_KeyRoundsTo001Deg(t *testing.T) {
	// Frozen: key = rounded lat/lon at 0.01° (0.003° offset shares a key;
	// 0.05° offset does not) — multiple employees at one shop share fetches.
	srv, hits := newCannedServer(t)
	c := NewCache(newTestFetcher(t, srv.URL), time.Minute, discardLogger())

	if _, err := c.Fetch(context.Background(), 44.000, -103.200); err != nil {
		t.Fatalf("Fetch exact: %v", err)
	}
	if _, err := c.Fetch(context.Background(), 44.003, -103.204); err != nil {
		t.Fatalf("Fetch near (0.003 deg): %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("0.003 deg offset: hits = %d, want 1 (same 0.01-deg bucket)", got)
	}
	if _, err := c.Fetch(context.Background(), 44.050, -103.200); err != nil {
		t.Fatalf("Fetch far (0.05 deg): %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("0.05 deg offset: hits = %d, want 2 (different bucket)", got)
	}
}

func TestCache_StaleOnError(t *testing.T) {
	stub := &stubFetcher{
		mode:     "ok",
		outlookF: Outlook{NowC: 3, TodayHighC: 12, TodayLowC: -2, WindKph: 20, RainProbToday: 60},
	}
	h := &captureHandler{}
	c := NewCache(stub, 10*time.Millisecond, slog.New(h))

	if _, err := c.Fetch(context.Background(), 44.0, -103.0); err != nil {
		t.Fatalf("seed Fetch: %v", err)
	}

	// Expire the TTL, then make the upstream fail.
	time.Sleep(40 * time.Millisecond)
	stub.mu.Lock()
	stub.mode = "fail"
	stub.mu.Unlock()

	o, err := c.Fetch(context.Background(), 44.0, -103.0)
	if err != nil {
		t.Fatalf("stale-on-error must NOT surface the upstream error to callers: %v", err)
	}
	if o.NowC != 3 || o.TodayLowC != -2 || o.RainProbToday != 60 {
		t.Errorf("stale value = %+v, want the last good Outlook", o)
	}
	if !h.contains("stale") {
		t.Errorf("stale fallback must be logged (line containing 'stale'); got: %v", h.lines)
	}
}

func TestCache_ErrorWithNoPriorValuePropagates(t *testing.T) {
	stub := &stubFetcher{mode: "fail"}
	c := NewCache(stub, time.Minute, discardLogger())
	if _, err := c.Fetch(context.Background(), 44.0, -103.0); err == nil {
		t.Fatal("first fetch failing with nothing cached must return the error")
	}
}

// ---------------------------------------------------------------------------
// §W3 — AlertsFor truth table (FROZEN thresholds + FROZEN strings)
// ---------------------------------------------------------------------------
//
// "concrete":  TodayLowC<0  => "cold — concrete pour at risk"
//              RainProbToday>=40 => "rain — exposed concrete work at risk"
// "roofing":   WindKph>=50  => "high wind — roofing work at risk"
//              RainProbToday>=40 => "rain — roofing work at risk"
// "earthwork": RainProbToday>=60 => "rain — excavation at risk"
// unknown trades -> no alerts (D5: never fabricate).

const (
	alertConcreteCold  = "cold — concrete pour at risk"
	alertConcreteRain  = "rain — exposed concrete work at risk"
	alertRoofingWind   = "high wind — roofing work at risk"
	alertRoofingRain   = "rain — roofing work at risk"
	alertEarthworkRain = "rain — excavation at risk"
)

func baseOutlook() Outlook {
	return Outlook{NowC: 10, TodayHighC: 15, TodayLowC: 5, WindKph: 20, RainProbToday: 10}
}

func TestAlertsFor_TruthTable(t *testing.T) {
	tests := []struct {
		name   string
		trades []string
		modify func(*Outlook)
		want   []string // exact alerts, each expected exactly once; empty => want NO alerts
	}{
		{
			name:   "concrete cold: low -2 -> risk",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.TodayLowC = -2 },
			want:   []string{alertConcreteCold},
		},
		{
			name:   "concrete cold boundary: low -0.5 -> risk",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.TodayLowC = -0.5 },
			want:   []string{alertConcreteCold},
		},
		{
			name:   "concrete cold boundary: low 0 -> none (strict <0)",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.TodayLowC = 0 },
			want:   nil,
		},
		{
			name:   "concrete rain 40 boundary -> risk",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.RainProbToday = 40 },
			want:   []string{alertConcreteRain},
		},
		{
			name:   "concrete rain 60 -> risk",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.RainProbToday = 60 },
			want:   []string{alertConcreteRain},
		},
		{
			name:   "concrete rain 39 -> none",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.RainProbToday = 39 },
			want:   nil,
		},
		{
			name:   "concrete cold AND rain -> both",
			trades: []string{"concrete"},
			modify: func(o *Outlook) { o.TodayLowC = -2; o.RainProbToday = 60 },
			want:   []string{alertConcreteCold, alertConcreteRain},
		},
		{
			name:   "roofing wind 50 boundary -> risk",
			trades: []string{"roofing"},
			modify: func(o *Outlook) { o.WindKph = 50 },
			want:   []string{alertRoofingWind},
		},
		{
			name:   "roofing wind 55 -> risk",
			trades: []string{"roofing"},
			modify: func(o *Outlook) { o.WindKph = 55 },
			want:   []string{alertRoofingWind},
		},
		{
			name:   "roofing wind 49 -> none",
			trades: []string{"roofing"},
			modify: func(o *Outlook) { o.WindKph = 49 },
			want:   nil,
		},
		{
			name:   "roofing rain 39 -> none",
			trades: []string{"roofing"},
			modify: func(o *Outlook) { o.RainProbToday = 39 },
			want:   nil,
		},
		{
			name:   "roofing rain 40 -> risk",
			trades: []string{"roofing"},
			modify: func(o *Outlook) { o.RainProbToday = 40 },
			want:   []string{alertRoofingRain},
		},
		{
			name:   "earthwork rain 55 -> none",
			trades: []string{"earthwork"},
			modify: func(o *Outlook) { o.RainProbToday = 55 },
			want:   nil,
		},
		{
			name:   "earthwork rain 60 -> risk",
			trades: []string{"earthwork"},
			modify: func(o *Outlook) { o.RainProbToday = 60 },
			want:   []string{alertEarthworkRain},
		},
		{
			name:   "earthwork wind 90 -> none (wind not earthwork's rule)",
			trades: []string{"earthwork"},
			modify: func(o *Outlook) { o.WindKph = 90 },
			want:   nil,
		},
		{
			name:   "unknown trade -> empty, never fabricate",
			trades: []string{"plumbing"},
			modify: func(o *Outlook) { o.TodayLowC = -20; o.RainProbToday = 100; o.WindKph = 120 },
			want:   nil,
		},
		{
			name:   "empty trades -> empty",
			trades: nil,
			modify: func(o *Outlook) { o.TodayLowC = -20 },
			want:   nil,
		},
		{
			name:   "multi-trade union with duplicate trade -> deduped",
			trades: []string{"concrete", "roofing", "concrete"},
			modify: func(o *Outlook) { o.RainProbToday = 40 },
			want:   []string{alertConcreteRain, alertRoofingRain},
		},
		{
			name:   "multi-trade union: concrete+earthwork rain 60 -> distinct texts",
			trades: []string{"concrete", "earthwork"},
			modify: func(o *Outlook) { o.RainProbToday = 60 },
			want:   []string{alertConcreteRain, alertEarthworkRain},
		},
		{
			name:   "full storm: all three trades fire",
			trades: []string{"concrete", "roofing", "earthwork"},
			modify: func(o *Outlook) {
				o.TodayLowC = -2
				o.WindKph = 55
				o.RainProbToday = 70
			},
			want: []string{
				alertConcreteCold, alertConcreteRain,
				alertRoofingWind, alertRoofingRain,
				alertEarthworkRain,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := baseOutlook()
			tt.modify(&o)
			got := AlertsFor(tt.trades, o)
			if len(tt.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("AlertsFor = %q, want empty", got)
				}
				return
			}
			for _, w := range tt.want {
				if !exactlyOnce(got, w) {
					t.Errorf("AlertsFor = %q, want %q exactly once", got, w)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("AlertsFor len = %d (%q), want exactly %d alerts", len(got), got, len(tt.want))
			}
		})
	}
}
