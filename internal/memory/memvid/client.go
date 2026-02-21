// Package memvid provides a Go client for the memvid memory service.
package memvid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client provides access to a memvid service via HTTP.
type Client struct {
	endpoint   string
	zone       string
	httpClient *http.Client
}

// ClientConfig holds configuration for creating a Client.
type ClientConfig struct {
	// Endpoint is the base URL for the memvid service (e.g., "http://localhost:8765")
	Endpoint string
	// Zone is the memory zone for isolation (e.g., "personality", "episodic", "task:code")
	Zone string
	// Timeout is the HTTP client timeout
	Timeout time.Duration
}

// Memory represents a stored memory item.
type Memory struct {
	ID        string         `json:"id"`
	Content   string         `json:"content"`
	Zone      string         `json:"zone"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// MemoryResult is a memory returned from search with relevance score.
type MemoryResult struct {
	Memory         Memory  `json:"memory"`
	RelevanceScore float64 `json:"relevance_score"`
}

// StoreRequest is the request payload for storing a memory.
type StoreRequest struct {
	Content  string         `json:"content"`
	Zone     string         `json:"zone"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// StoreResponse is the response from storing a memory.
type StoreResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// SearchRequest is the request payload for searching memories.
type SearchRequest struct {
	Query string `json:"query"`
	Zone  string `json:"zone,omitempty"`
	Limit int    `json:"limit"`
}

// SearchResponse is the response from searching memories.
type SearchResponse struct {
	Results []MemoryResult `json:"results"`
	Total   int            `json:"total"`
	Error   string         `json:"error,omitempty"`
}

// GetRequest is the request payload for getting memories by IDs.
type GetRequest struct {
	IDs  []string `json:"ids"`
	Zone string   `json:"zone,omitempty"`
}

// GetResponse is the response from getting memories.
type GetResponse struct {
	Memories []Memory `json:"memories"`
	Error    string   `json:"error,omitempty"`
}

// DeleteRequest is the request payload for deleting a memory.
type DeleteRequest struct {
	ID   string `json:"id"`
	Zone string `json:"zone,omitempty"`
}

// DeleteResponse is the response from deleting a memory.
type DeleteResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// HealthResponse is the response from the health check endpoint.
type HealthResponse struct {
	Status    string `json:"status"`
	Zones     int    `json:"zones"`
	Memories  int    `json:"memories"`
	DiskUsage int64  `json:"disk_usage_bytes"`
}

// NewClient creates a new memvid client.
func NewClient(cfg ClientConfig) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		endpoint: cfg.Endpoint,
		zone:     cfg.Zone,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Store persists content in the memory store.
func (c *Client) Store(ctx context.Context, content string, metadata map[string]any) (string, error) {
	req := StoreRequest{
		Content:  content,
		Zone:     c.zone,
		Metadata: metadata,
	}

	var resp StoreResponse
	if err := c.post(ctx, "/store", req, &resp); err != nil {
		return "", fmt.Errorf("store request failed: %w", err)
	}

	if resp.Error != "" {
		return "", fmt.Errorf("store error: %s", resp.Error)
	}

	return resp.ID, nil
}

// StoreWithZone persists content in a specific zone.
func (c *Client) StoreWithZone(ctx context.Context, content string, zone string, metadata map[string]any) (string, error) {
	req := StoreRequest{
		Content:  content,
		Zone:     zone,
		Metadata: metadata,
	}

	var resp StoreResponse
	if err := c.post(ctx, "/store", req, &resp); err != nil {
		return "", fmt.Errorf("store request failed: %w", err)
	}

	if resp.Error != "" {
		return "", fmt.Errorf("store error: %s", resp.Error)
	}

	return resp.ID, nil
}

// Search finds memories matching the query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	if limit <= 0 {
		limit = 10
	}

	req := SearchRequest{
		Query: query,
		Zone:  c.zone,
		Limit: limit,
	}

	var resp SearchResponse
	if err := c.post(ctx, "/search", req, &resp); err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("search error: %s", resp.Error)
	}

	return resp.Results, nil
}

// SearchAllZones finds memories across all zones.
func (c *Client) SearchAllZones(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	if limit <= 0 {
		limit = 10
	}

	req := SearchRequest{
		Query: query,
		Zone:  "", // Empty zone searches all
		Limit: limit,
	}

	var resp SearchResponse
	if err := c.post(ctx, "/search", req, &resp); err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("search error: %s", resp.Error)
	}

	return resp.Results, nil
}

// GetByIDs retrieves memories by their IDs.
func (c *Client) GetByIDs(ctx context.Context, ids []string) ([]Memory, error) {
	req := GetRequest{
		IDs:  ids,
		Zone: c.zone,
	}

	var resp GetResponse
	if err := c.post(ctx, "/get", req, &resp); err != nil {
		return nil, fmt.Errorf("get request failed: %w", err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("get error: %s", resp.Error)
	}

	return resp.Memories, nil
}

// Delete removes a memory by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	req := DeleteRequest{
		ID:   id,
		Zone: c.zone,
	}

	var resp DeleteResponse
	if err := c.post(ctx, "/delete", req, &resp); err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}

	if resp.Error != "" {
		return fmt.Errorf("delete error: %s", resp.Error)
	}

	return nil
}

// Health checks the memvid service health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.get(ctx, "/health", &resp); err != nil {
		return nil, fmt.Errorf("health request failed: %w", err)
	}

	return &resp, nil
}

// IsAvailable checks if the memvid service is reachable.
func (c *Client) IsAvailable(ctx context.Context) bool {
	resp, err := c.Health(ctx)
	return err == nil && resp.Status == "ok"
}

// Zone returns the current zone.
func (c *Client) Zone() string {
	return c.zone
}

// WithZone returns a new client configured for a different zone.
func (c *Client) WithZone(zone string) *Client {
	return &Client{
		endpoint:   c.endpoint,
		zone:       zone,
		httpClient: c.httpClient,
	}
}

// post sends a POST request to the memvid service.
func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server error: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}

// get sends a GET request to the memvid service.
func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server error: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}
