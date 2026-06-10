package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/calendar"
	"github.com/caimlas/meept/internal/config"
)

func newCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "google calendar integration",
		Long: `Manage Google Calendar integration.

Subcommands:
  auth   - Authenticate with Google Calendar via OAuth device code
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
		Long: `Authenticate meept with Google Calendar using the OAuth device-code flow.

This is a convenience shortcut for 'meept config oauth connect google-calendar'.
OAuth tokens are stored in ~/.meept/oauth/google-calendar.json (encrypted).`,
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
	fmt.Println("To authenticate Google Calendar, run:")
	fmt.Println()
	fmt.Println("  meept config oauth connect google-calendar")
	fmt.Println()
	fmt.Println("This starts the OAuth device-code flow. Open the printed URL,")
	fmt.Println("enter the code, and authorize the application.")
	return nil
}

func runCalendarToday(cmd *cobra.Command, args []string) error {
	cfg, err := loadCalendarConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, err := buildCalendarTokenStore()
	if err != nil {
		return err
	}

	token, err := calendar.GetAccessToken(cmd.Context(), store)
	if err != nil {
		return fmt.Errorf("authentication required: %w\nRun 'meept config oauth connect google-calendar' to authenticate", err)
	}

	calendarID := cfg.CalendarID
	if calendarID == "" {
		calendarID = "primary"
	}

	client, err := calendar.NewClient(calendar.ClientConfig{
		AccessToken: token,
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

// buildCalendarTokenStore creates an encryption key and returns an initialised
// TokenStore for calendar OAuth operations.
func buildCalendarTokenStore() (*auth.TokenStore, error) {
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

// loadCalendarConfig loads the meept config and returns the calendar section.
func loadCalendarConfig() (*config.CalendarConfig, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		def := config.DefaultConfig()
		return &def.Calendar, nil
	}
	return &cfg.Calendar, nil
}
