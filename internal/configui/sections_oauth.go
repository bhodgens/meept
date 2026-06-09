// internal/configui/sections_oauth.go
package configui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/auth"
)

// oauthProviderStatus represents the computed status for one provider.
type oauthProviderStatus struct {
	providerID string
	status     string // "connected", "expired", or "disconnected"
	expiry     time.Time
	scopes     []string
	hasRefresh bool
	fileMod    time.Time
}

func buildOAuthFields() []Field {
	store, err := buildOAuthTokenStore()
	if err != nil {
		return []Field{
			NewTextField("_error", "error", fmt.Sprintf("token store: %s", err.Error())),
		}
	}

	infos, err := store.List()
	if err != nil {
		return []Field{
			NewTextField("_error", "error", fmt.Sprintf("list tokens: %s", err.Error())),
		}
	}

	// Build a map of stored token info keyed by provider.
	infoMap := make(map[string]*auth.StoredTokenInfo, len(infos))
	for i := range infos {
		infoMap[infos[i].Provider] = &infos[i]
	}

	// Collect all registered provider IDs in sorted order.
	providerIDs := auth.RegisteredProviders()
	sort.Strings(providerIDs)

	// Compute status for each provider and build drilldown items.
	items := make([]DrilldownItem, 0, len(providerIDs))
	for _, pid := range providerIDs {
		status := computeProviderStatus(pid, infoMap[pid])
		displayName := fmt.Sprintf("%s  %s", pid, status.status)
		if status.status == "connected" && !status.expiry.IsZero() {
			displayName = fmt.Sprintf("%s  %s  (expires in %s)", pid, status.status, humanDuration(time.Until(status.expiry)))
		}
		items = append(items, DrilldownItem{
			Name:   displayName,
			Fields: buildOAuthProviderFields(store, pid, status),
		})
	}

	return []Field{
		NewDrilldownField("providers", "oauth providers", items),
	}
}

func computeProviderStatus(providerID string, info *auth.StoredTokenInfo) oauthProviderStatus {
	if info == nil {
		return oauthProviderStatus{
			providerID: providerID,
			status:     "disconnected",
		}
	}
	now := time.Now()
	if info.Expiry.IsZero() || now.After(info.Expiry) {
		return oauthProviderStatus{
			providerID: providerID,
			status:     "expired",
			expiry:     info.Expiry,
			scopes:     info.Scopes,
			hasRefresh: info.HasRefresh,
			fileMod:    info.FileModTime,
		}
	}
	return oauthProviderStatus{
		providerID: providerID,
		status:     "connected",
		expiry:     info.Expiry,
		scopes:     info.Scopes,
		hasRefresh: info.HasRefresh,
		fileMod:    info.FileModTime,
	}
}

func buildOAuthProviderFields(store *auth.TokenStore, providerID string, status oauthProviderStatus) []Field {
	fields := []Field{
		NewTextField("status", "connection status", status.status),
	}

	if !status.expiry.IsZero() {
		fields = append(fields, NewTextField("expiry", "token expiry", status.expiry.Format(time.RFC3339)))
	} else {
		fields = append(fields, NewTextField("expiry", "token expiry", "(none)"))
	}

	if !status.fileMod.IsZero() {
		fields = append(fields, NewTextField("last_refreshed", "last refreshed", status.fileMod.Format(time.RFC3339)))
	} else {
		fields = append(fields, NewTextField("last_refreshed", "last refreshed", "(never)"))
	}

	if len(status.scopes) > 0 {
		fields = append(fields, NewTextField("scopes", "scopes", strings.Join(status.scopes, ", ")))
	}

	fields = append(fields, NewTextField("refresh_token", "refresh token", formatBool(status.hasRefresh)))

	// Action field: connect or disconnect.
	switch status.status {
	case "connected":
		fields = append(fields, NewActionField("action", "disconnect", func() error {
			return store.Delete(providerID)
		}))
	case "expired":
		fields = append(fields, NewActionField("action", "reconnect", func() error {
			return fmt.Errorf("reconnect must be run from the CLI: meept config oauth connect %s", providerID)
		}))
	default:
		fields = append(fields, NewActionField("action", "connect", func() error {
			return fmt.Errorf("connect must be run from the CLI: meept config oauth connect %s", providerID)
		}))
	}

	return fields
}

// buildOAuthTokenStore creates an EncryptionKey (env var first, then machine
// key) and returns an initialised TokenStore.
func buildOAuthTokenStore() (*auth.TokenStore, error) {
	userKey := os.Getenv("MEEPT_OAUTH_ENCRYPTION_KEY")
	enc, err := auth.NewEncryptionKey(userKey)
	if err != nil {
		return nil, fmt.Errorf("create encryption key: %w", err)
	}
	store := auth.NewTokenStore(enc)
	if err := store.Init(); err != nil {
		return nil, fmt.Errorf("init token store: %w", err)
	}
	return store, nil
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
