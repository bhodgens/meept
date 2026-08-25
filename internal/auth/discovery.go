package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// discoveryHTTPClient is the HTTP client used for OIDC discovery requests.
// It mirrors deviceFlowHTTPClient: a client-level timeout so hung TCP
// connections don't block forever, with per-request context semantics on
// top of this cap.
var discoveryHTTPClient = &http.Client{Timeout: 30 * time.Second}

// ResolveTokenEndpoint fetches an OIDC discovery document (RFC 8414
// .well-known/openid-configuration) and returns its token_endpoint value.
// It performs no logging; errors are returned to the caller.
func ResolveTokenEndpoint(ctx context.Context, discoveryURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", fmt.Errorf("create discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := discoveryHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read discovery response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discovery endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var doc struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("parse discovery response: %w", err)
	}

	if doc.TokenEndpoint == "" {
		return "", fmt.Errorf("discovery response missing token_endpoint")
	}

	return doc.TokenEndpoint, nil
}
