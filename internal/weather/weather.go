// Package weather provides a keyless open-meteo forecast fetcher, a
// per-location TTL cache with singleflight dedup and stale-on-error
// fallback, and deterministic trade-risk alerts (AlertsFor) for the
// situational-awareness feature (plan 20261110-situational, leaf 03/01).
//
// # HTTP client construction / SSRF note
//
// The production default client is a plain http.Client with a
// DefaultTimeout (10s). The SSRF-guard construction pattern from
// internal/tools/builtin/web_fetch.go:158 + ssrf.go is deliberately NOT
// imported here: that helper lives in package builtin, which imports
// internal/agent; this package is consumed by internal/moment, which the
// agent prompt path imports, so importing builtin would risk an import
// cycle. This is acceptable for this package because the production URL
// host is a compile-time constant (DefaultBaseURL =
// "https://api.open-meteo.com") and never user input: the only caller-
// controlled inputs are numeric lat/lon coordinates, which are formatted
// with %.4f and cannot inject URLs. Tests inject Config.BaseURL/
// Config.Client to point at an httptest server (the gate test pins this
// seam); any future wiring that lets a config-provided BaseURL reach this
// package must rebuild the client with the SSRF guard at that call site.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Frozen contract constants (master.md "internal/weather package" block).
const (
	// DefaultBaseURL is the production open-meteo base URL (keyless).
	DefaultBaseURL = "https://api.open-meteo.com"
	// DefaultTTL is the cache entry lifetime: 30 minutes.
	DefaultTTL = 30 * time.Minute
	// DefaultTimeout is the per-request fetch timeout: 10 seconds.
	DefaultTimeout = 10 * time.Second

	// keyPrecision is the cache-key rounding: 0.01 degrees (two shops in
	// one town share a fetch).
	keyPrecision = 100.0
)

// Outlook is a compact weather snapshot for one location.
type Outlook struct {
	NowC, TodayHighC, TodayLowC, WindKph float64
	RainProbToday                        int // 0-100
	Fetched                              time.Time
}

// Fetcher fetches an Outlook for a lat/lon point.
type Fetcher interface {
	Fetch(ctx context.Context, lat, lon float64) (Outlook, error)
}

// ---------------------------------------------------------------------------
// open-meteo fetcher
// ---------------------------------------------------------------------------

// Config is the base-URL/client test seam. Empty BaseURL -> DefaultBaseURL;
// nil Client -> a plain http.Client with DefaultTimeout (see package doc
// for the SSRF rationale).
type Config struct {
	BaseURL string
	Client  *http.Client
}

// OpenMeteo is a keyless open-meteo Fetcher. It implements Fetcher.
type OpenMeteo struct {
	baseURL string
	client  *http.Client
}

// New builds an OpenMeteo fetcher from cfg, applying the defaults above.
func New(cfg Config) *OpenMeteo {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &OpenMeteo{baseURL: base, client: client}
}

// openMeteoResponse mirrors the pinned wire shape. open-meteo sends JSON
// numbers; precipitation is decoded as float and narrowed to int.
type openMeteoResponse struct {
	Current struct {
		Temperature2M *float64 `json:"temperature_2m"`
	} `json:"current"`
	Daily struct {
		Temperature2MMax []float64 `json:"temperature_2m_max"`
		Temperature2MMin []float64 `json:"temperature_2m_min"`
		PrecipProbMax    []float64 `json:"precipitation_probability_max"`
		WindSpeed10MMax  []float64 `json:"wind_speed_10m_max"`
	} `json:"daily"`
}

// Fetch GETs {base}/v1/forecast for (lat, lon) and maps the pinned fields:
// NowC <- current.temperature_2m; TodayHighC <- daily.temperature_2m_max[0];
// TodayLowC <- daily.temperature_2m_min[0]; WindKph <-
// daily.wind_speed_10m_max[0]; RainProbToday <-
// daily.precipitation_probability_max[0]; Fetched <- now.
func (o *OpenMeteo) Fetch(ctx context.Context, lat, lon float64) (Outlook, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/v1/forecast", nil)
	if err != nil {
		return Outlook{}, fmt.Errorf("weather: build request: %w", err)
	}
	q := req.URL.Query()
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("current", "temperature_2m,wind_speed_10m")
	q.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_probability_max,wind_speed_10m_max")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "1")
	req.URL.RawQuery = q.Encode()

	resp, err := o.client.Do(req)
	if err != nil {
		return Outlook{}, fmt.Errorf("weather: fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Outlook{}, fmt.Errorf("weather: upstream status %d", resp.StatusCode)
	}

	var parsed openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Outlook{}, fmt.Errorf("weather: decode: %w", err)
	}

	if parsed.Current.Temperature2M == nil {
		return Outlook{}, fmt.Errorf("weather: response missing current.temperature_2m")
	}
	daily := parsed.Daily
	if len(daily.Temperature2MMax) == 0 || len(daily.Temperature2MMin) == 0 ||
		len(daily.PrecipProbMax) == 0 || len(daily.WindSpeed10MMax) == 0 {
		return Outlook{}, fmt.Errorf("weather: response missing daily arrays")
	}

	return Outlook{
		NowC:          *parsed.Current.Temperature2M,
		TodayHighC:    daily.Temperature2MMax[0],
		TodayLowC:     daily.Temperature2MMin[0],
		WindKph:       daily.WindSpeed10MMax[0],
		RainProbToday: int(daily.PrecipProbMax[0]),
		Fetched:       time.Now(),
	}, nil
}

// ---------------------------------------------------------------------------
// cache
// ---------------------------------------------------------------------------

// cacheEntry holds the last good Outlook for a rounded (lat,lon) key.
type cacheEntry struct {
	outlook  Outlook
	storedAt time.Time // when the entry was written (TTL clock)
}

// Cache wraps a Fetcher with per-(lat,lon) TTL caching: keys rounded to
// 0.01 degrees, singleflight dedup of concurrent misses, and
// stale-on-error (a failed re-fetch after TTL expiry serves the last good
// Outlook with a "stale" WARN log instead of surfacing the error). A first
// fetch that fails with nothing cached propagates the error. It implements
// Fetcher.
type Cache struct {
	upstream Fetcher
	ttl      time.Duration
	log      *slog.Logger

	mu      sync.Mutex
	entries map[string]cacheEntry
	group   singleflight.Group
}

// NewCache wraps f with the frozen cache contract. ttl <= 0 -> DefaultTTL;
// a nil logger falls back to slog.Default().
func NewCache(f Fetcher, ttl time.Duration, logger *slog.Logger) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{
		upstream: f,
		ttl:      ttl,
		log:      logger,
		entries:  make(map[string]cacheEntry),
	}
}

// cacheKey rounds lat/lon to 0.01 degrees so nearby locations share one
// fetch bucket.
func cacheKey(lat, lon float64) string {
	rlat := strconv.FormatFloat(roundDeg(lat), 'f', 2, 64)
	rlon := strconv.FormatFloat(roundDeg(lon), 'f', 2, 64)
	return rlat + ":" + rlon
}

func roundDeg(v float64) float64 {
	return math.Round(v*keyPrecision) / keyPrecision
}

// Fetch serves a fresh cached entry when present; otherwise it dedups
// concurrent misses via singleflight and re-fetches, caching the result.
// On re-fetch failure with a prior good entry it serves the stale value
// (logging a "stale" line); with no prior entry the error propagates.
func (c *Cache) Fetch(ctx context.Context, lat, lon float64) (Outlook, error) {
	key := cacheKey(lat, lon)

	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Since(e.storedAt) < c.ttl {
		o := e.outlook
		c.mu.Unlock()
		return o, nil
	}
	c.mu.Unlock()

	v, err, _ := c.group.Do(key, func() (any, error) {
		// Another flight may have completed between our check and Do().
		c.mu.Lock()
		if e, ok := c.entries[key]; ok && time.Since(e.storedAt) < c.ttl {
			o := e.outlook
			c.mu.Unlock()
			return o, nil
		}
		c.mu.Unlock()

		o, ferr := c.upstream.Fetch(ctx, lat, lon)
		if ferr != nil {
			c.mu.Lock()
			stale, ok := c.entries[key]
			c.mu.Unlock()
			if ok {
				c.log.Warn("weather: serving stale cache after fetch error",
					"stale", true,
					"key", key,
					"fetched", stale.outlook.Fetched.UTC().Format(time.RFC3339),
					"error", ferr.Error())
				return stale.outlook, nil
			}
			return Outlook{}, ferr
		}

		c.mu.Lock()
		c.entries[key] = cacheEntry{outlook: o, storedAt: time.Now()}
		c.mu.Unlock()
		return o, nil
	})
	if err != nil {
		return Outlook{}, err
	}
	return v.(Outlook), nil
}

// ---------------------------------------------------------------------------
// alerts (frozen thresholds + frozen strings)
// ---------------------------------------------------------------------------

// PeekOutlook returns the last GOOD Outlook stored for (lat,lon) — inside
// TTL or past it — without triggering any upstream fetch. This is the
// moment-feed seam: the renderer may only show weather the refresh job
// already warmed (cold cache -> absent, never a per-turn fetch). The bool
// is false when nothing is cached for the bucket.
func (c *Cache) PeekOutlook(lat, lon float64) (Outlook, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[cacheKey(lat, lon)]
	if !ok {
		return Outlook{}, false
	}
	return e.outlook, true
}

// Frozen, owner-tunable-later thresholds.
const (
	// ConcreteColdThresholdC: concrete alerts when TodayLowC < this.
	ConcreteColdThresholdC = 0.0
	// ConcreteRainThresholdPct: concrete alerts at RainProbToday >= this.
	ConcreteRainThresholdPct = 40
	// RoofingWindThresholdKph: roofing alerts at WindKph >= this.
	RoofingWindThresholdKph = 50.0
	// RoofingRainThresholdPct: roofing alerts at RainProbToday >= this.
	RoofingRainThresholdPct = 40
	// EarthworkRainThresholdPct: earthwork alerts at RainProbToday >= this.
	EarthworkRainThresholdPct = 60
)

// AlertsFor returns the frozen risk-alert strings for the given trades
// against o: concrete (TodayLowC<0, rain>=40), roofing (wind>=50,
// rain>=40), earthwork (rain>=60). Duplicate trades collapse; unknown
// trades never fabricate alerts. Result order follows first-appearance
// trade order, rule order within a trade.
func AlertsFor(trades []string, o Outlook) []string {
	var alerts []string
	seen := make(map[string]bool, len(trades))
	for _, raw := range trades {
		trade := normalizeTrade(raw)
		if trade == "" || seen[trade] {
			continue
		}
		seen[trade] = true
		switch trade {
		case "concrete":
			if o.TodayLowC < ConcreteColdThresholdC {
				alerts = append(alerts, "cold — concrete pour at risk")
			}
			if o.RainProbToday >= ConcreteRainThresholdPct {
				alerts = append(alerts, "rain — exposed concrete work at risk")
			}
		case "roofing":
			if o.WindKph >= RoofingWindThresholdKph {
				alerts = append(alerts, "high wind — roofing work at risk")
			}
			if o.RainProbToday >= RoofingRainThresholdPct {
				alerts = append(alerts, "rain — roofing work at risk")
			}
		case "earthwork":
			if o.RainProbToday >= EarthworkRainThresholdPct {
				alerts = append(alerts, "rain — excavation at risk")
			}
		}
	}
	return alerts
}

func normalizeTrade(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
