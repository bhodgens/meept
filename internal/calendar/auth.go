// Package calendar provides Google Calendar integration for meept.
package calendar

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/auth"
)

const providerID = "google-calendar"

// GetAccessToken resolves a valid access token for the Google Calendar API
// using the shared OAuth device-code token store. If the token is expired
// or missing, it is refreshed automatically. If no token is stored at all,
// the caller is directed to run 'meept config oauth connect google-calendar'.
func GetAccessToken(ctx context.Context, tokenStore *auth.TokenStore) (string, error) {
	providerCfg, err := auth.ResolveProviderConfig(providerID)
	if err != nil {
		return "", fmt.Errorf("resolve calendar oauth config: %w", err)
	}

	return tokenStore.GetValidToken(ctx, providerID, providerCfg.DeviceFlowConfig())
}
