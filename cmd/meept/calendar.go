package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/calendar"
	"github.com/caimlas/meept/internal/config"
)

func newCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "google calendar integration",
		Long: `Manage Google Calendar integration.

Subcommands:
  auth   - Authenticate with Google Calendar via OAuth2
  today  - Show today's calendar events`,
	}

	cmd.AddCommand(newCalendarAuthCmd())
	cmd.AddCommand(newCalendarTodayCmd())

	return cmd
}

func newCalendarAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "authenticate with google calendar",
		Long: `Start the OAuth2 flow to grant meept access to your Google Calendar.

This opens a local HTTP server on port 8888 to receive the callback.
Open the printed URL in your browser to authorize.`,
		RunE: runCalendarAuth,
	}
}

func newCalendarTodayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "today",
		Short: "show today's calendar events",
		RunE:  runCalendarToday,
	}
}

func runCalendarAuth(cmd *cobra.Command, args []string) error {
	cfg, err := loadCalendarConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return fmt.Errorf("calendar client_id and client_secret must be configured in meept.toml or via environment variables")
	}

	redirectURI := cfg.RedirectURI
	if redirectURI == "" {
		redirectURI = "http://localhost:8888/callback"
	}

	oauthCfg := calendar.DefaultOAuth2Config(cfg.ClientID, cfg.ClientSecret, redirectURI)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}
	tokenPath := filepath.Join(homeDir, ".meept", "calendar_token.json")
	auth := calendar.NewOAuth2Authenticator(oauthCfg, tokenPath)

	// Generate state for CSRF protection
	state, err := generateRandomState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	authURL := auth.AuthURL(state)
	fmt.Printf("Open this URL in your browser to authorize meept:\n\n%s\n\n", authURL)
	fmt.Println("Waiting for authorization on http://localhost:8888/callback ...")

	// Start local server to receive callback
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusForbidden)
			errCh <- fmt.Errorf("OAuth state mismatch")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "no code in response", http.StatusBadRequest)
			errCh <- fmt.Errorf("no authorization code received")
			return
		}
		codeCh <- code
		fmt.Fprintln(w, "Authorization successful! You can close this window.")
	})

	server := &http.Server{Addr: ":8888", Handler: mux} //nolint:gosec // local dev server, not exposed to network
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Wait for code or error
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return fmt.Errorf("OAuth callback failed: %w", err)
	case <-cmd.Context().Done():
		return cmd.Context().Err()
	}

	// Exchange code for token
	token, err := auth.Exchange(cmd.Context(), code)
	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	if err := auth.SaveToken(token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Println("Calendar authentication successful! Token saved.")
	return nil
}

func runCalendarToday(cmd *cobra.Command, args []string) error {
	cfg, err := loadCalendarConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return fmt.Errorf("calendar not configured. Set client_id and client_secret in meept.toml")
	}

	redirectURI := cfg.RedirectURI
	if redirectURI == "" {
		redirectURI = "http://localhost:8888/callback"
	}

	oauthCfg := calendar.DefaultOAuth2Config(cfg.ClientID, cfg.ClientSecret, redirectURI)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}
	tokenPath := filepath.Join(homeDir, ".meept", "calendar_token.json")
	auth := calendar.NewOAuth2Authenticator(oauthCfg, tokenPath)

	token, err := auth.GetValidToken(cmd.Context())
	if err != nil {
		return fmt.Errorf("authentication required: %w\nRun 'meept calendar auth' first", err)
	}

	calendarID := cfg.CalendarID
	if calendarID == "" {
		calendarID = "primary"
	}

	client, err := calendar.NewClient(calendar.ClientConfig{
		AccessToken: token.AccessToken,
		CalendarID:  calendarID,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to create calendar client: %w", err)
	}

	events, err := client.GetToday(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to get today's events: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("No events scheduled for today.")
		return nil
	}

	fmt.Printf("Today's events (%d):\n", len(events))
	for i, e := range events {
		start, err := e.Start.Time()
		if err != nil {
			fmt.Printf("  %d. %s\n", i+1, e.Summary)
			continue
		}
		line := fmt.Sprintf("  %d. %s - %s", i+1, start.Format("15:04"), e.Summary)
		if e.Location != "" {
			line += fmt.Sprintf(" (%s)", e.Location)
		}
		fmt.Println(line)
	}

	return nil
}

// loadCalendarConfig loads the meept config and returns the calendar section.
func loadCalendarConfig() (*config.CalendarConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(homeDir, ".meept", "meept.toml")
	cfg, err := config.Load(configPath)
	if err != nil {
		// Fall back to defaults
		def := config.DefaultConfig()
		return &def.Calendar, err
	}

	return &cfg.Calendar, nil
}

// generateRandomState creates a random OAuth state string.
func generateRandomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
