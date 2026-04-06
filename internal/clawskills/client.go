// Package clawskills provides the ClawHub registry client for third-party skills.
package clawskills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultBaseURL     = "https://clawhub.ai"
	defaultTimeout     = 30 * time.Second
	defaultMaxRequests = 100
	defaultWindowSecs  = 60
	maxDownloadBytes   = 10 * 1024 * 1024  // 10 MB
	maxFileBytes       = 200 * 1024        // 200 KB
	cacheTTL           = 5 * time.Minute
)

// APIError represents an error from the ClawHub API.
type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ClawHub API error %d: %s", e.StatusCode, e.Detail)
}

// rateLimiter implements a sliding-window rate limiter.
type rateLimiter struct {
	mu           sync.Mutex
	maxRequests  int
	windowSecs   float64
	timestamps   []time.Time
}

func newRateLimiter(maxRequests int, windowSecs float64) *rateLimiter {
	return &rateLimiter{
		maxRequests: maxRequests,
		windowSecs:  windowSecs,
		timestamps:  make([]time.Time, 0, maxRequests),
	}
}

func (r *rateLimiter) acquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.purge()
	if len(r.timestamps) >= r.maxRequests {
		return false
	}
	r.timestamps = append(r.timestamps, time.Now())
	return true
}

func (r *rateLimiter) waitTime() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.purge()
	if len(r.timestamps) < r.maxRequests {
		return 0
	}
	if len(r.timestamps) == 0 {
		return time.Duration(r.windowSecs) * time.Second
	}
	oldest := r.timestamps[0]
	wait := oldest.Add(time.Duration(r.windowSecs) * time.Second).Sub(time.Now())
	if wait < 0 {
		return 0
	}
	return wait
}

func (r *rateLimiter) purge() {
	cutoff := time.Now().Add(-time.Duration(r.windowSecs) * time.Second)
	i := 0
	for ; i < len(r.timestamps); i++ {
		if !r.timestamps[i].Before(cutoff) {
			break
		}
	}
	r.timestamps = r.timestamps[i:]
}

// cacheEntry represents a cached response.
type cacheEntry struct {
	data    any
	expires time.Time
}

// responseCache is an in-memory TTL cache for GET responses.
type responseCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	store map[string]*cacheEntry
}

func newResponseCache(ttl time.Duration) *responseCache {
	return &responseCache{
		ttl:   ttl,
		store: make(map[string]*cacheEntry),
	}
}

func (c *responseCache) get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.store[key]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.data, true
}

func (c *responseCache) put(key string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = &cacheEntry{data: data, expires: time.Now().Add(c.ttl)}
}

func (c *responseCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, key)
}

func (c *responseCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]*cacheEntry)
}

// Client is the ClawHub API client.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	rateLimiter *rateLimiter
	cache       *responseCache
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithBaseURL sets the base URL.
func WithBaseURL(url string) ClientOption {
	return func(c *Client) { c.baseURL = url }
}

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) { c.httpClient.Timeout = timeout }
}

// NewClient creates a new ClawHub API client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		rateLimiter: newRateLimiter(defaultMaxRequests, defaultWindowSecs),
		cache:       newResponseCache(cacheTTL),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// getJSON issues a GET request and returns the parsed JSON body.
func (c *Client) getJSON(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	if !c.rateLimiter.acquire() {
		wait := c.rateLimiter.waitTime()
		return nil, &APIError{StatusCode: 429, Detail: fmt.Sprintf("Rate limit exceeded, retry after %.1fs", wait.Seconds())}
	}

	cacheKey := path
	if len(params) > 0 {
		cacheKey = path + "?" + params.Encode()
	}

	if cached, ok := c.cache.get(cacheKey); ok {
		return cached.(json.RawMessage), nil
	}

	fullURL := c.baseURL + path
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "meept-clawskills/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: string(body)}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	c.cache.put(cacheKey, json.RawMessage(data))
	return data, nil
}

// Search searches ClawHub for skills matching the query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("limit", fmt.Sprintf("%d", limit))

	data, err := c.getJSON(ctx, "/api/v1/search", params)
	if err != nil {
		return nil, err
	}

	// Try to parse as array first, then as object with "results" field
	var results []SearchResult
	if err := json.Unmarshal(data, &results); err == nil {
		return results, nil
	}

	var wrapper struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Results, nil
}

// ListRemote lists skills from the ClawHub registry.
func (c *Client) ListRemote(ctx context.Context, limit int, sort string) ([]RemoteSkill, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("sort", sort)

	data, err := c.getJSON(ctx, "/api/v1/skills", params)
	if err != nil {
		return nil, err
	}

	var skills []RemoteSkill
	if err := json.Unmarshal(data, &skills); err == nil {
		return skills, nil
	}

	var wrapper struct {
		Skills []RemoteSkill `json:"skills"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Skills, nil
}

// SkillDetail fetches detail for a specific skill by slug.
func (c *Client) SkillDetail(ctx context.Context, slug string) (*RemoteSkill, error) {
	data, err := c.getJSON(ctx, "/api/v1/skills/"+slug, nil)
	if err != nil {
		return nil, err
	}

	var skill RemoteSkill
	if err := json.Unmarshal(data, &skill); err != nil {
		return nil, err
	}
	return &skill, nil
}

// SkillVersions fetches the version history for a skill.
func (c *Client) SkillVersions(ctx context.Context, slug string) ([]SkillVersion, error) {
	data, err := c.getJSON(ctx, "/api/v1/skills/"+slug+"/versions", nil)
	if err != nil {
		return nil, err
	}

	var versions []SkillVersion
	if err := json.Unmarshal(data, &versions); err == nil {
		return versions, nil
	}

	var wrapper struct {
		Versions []SkillVersion `json:"versions"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Versions, nil
}

// SkillFile fetches raw file content from a skill.
func (c *Client) SkillFile(ctx context.Context, slug, path, version string) (string, error) {
	if !c.rateLimiter.acquire() {
		return "", &APIError{StatusCode: 429, Detail: "Rate limit exceeded"}
	}

	params := url.Values{}
	params.Set("path", path)
	if version != "" {
		params.Set("version", version)
	}

	fullURL := c.baseURL + "/api/v1/skills/" + slug + "/file?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "meept-clawskills/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return "", &APIError{StatusCode: resp.StatusCode, Detail: string(body)}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return "", err
	}

	if len(data) > maxFileBytes {
		return "", &APIError{StatusCode: 413, Detail: fmt.Sprintf("File exceeds %d byte limit", maxFileBytes)}
	}

	return string(data), nil
}

// ResolveVersion resolves the latest version for a skill.
func (c *Client) ResolveVersion(ctx context.Context, slug string) (*ResolveResult, error) {
	params := url.Values{}
	params.Set("slug", slug)

	data, err := c.getJSON(ctx, "/api/v1/resolve", params)
	if err != nil {
		return nil, err
	}

	var result ResolveResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Download downloads a skill ZIP archive with streaming + SHA-256 verification.
func (c *Client) Download(ctx context.Context, slug, version string) (*DownloadResult, error) {
	if !c.rateLimiter.acquire() {
		return nil, &APIError{StatusCode: 429, Detail: "Rate limit exceeded"}
	}

	params := url.Values{}
	params.Set("slug", slug)
	if version != "" {
		params.Set("version", version)
	}

	fullURL := c.baseURL + "/api/v1/download?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "meept-clawskills/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: string(body)}
	}

	hasher := sha256.New()
	reader := io.TeeReader(io.LimitReader(resp.Body, maxDownloadBytes+1), hasher)

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	if len(data) > maxDownloadBytes {
		return nil, &APIError{
			StatusCode: 413,
			Detail:     fmt.Sprintf("Archive exceeds %d MB limit", maxDownloadBytes/(1024*1024)),
		}
	}

	return &DownloadResult{
		Data:   data,
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Size:   int64(len(data)),
	}, nil
}

// Close closes the client (no-op for now, but good for interface compatibility).
func (c *Client) Close() error {
	c.cache.clear()
	return nil
}
