package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/auth"
)

func newOAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth",
		Short: "manage OAuth device-code connections",
		Long:  "Connect to, inspect, and disconnect OAuth device-code providers.\nThis is a subcommand of 'config'.",
	}

	cmd.AddCommand(newOAuthConnectCmd())
	cmd.AddCommand(newOAuthStatusCmd())
	cmd.AddCommand(newOAuthDisconnectCmd())

	return cmd
}

// newOAuthConnectCmd creates the "config oauth connect <provider>" command.
func newOAuthConnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <provider>",
		Short: "start device-code flow for a provider",
		Long: `Start the RFC 8628 device-code OAuth flow for the given provider.

Supported providers:
  github-models
  google-oauth
  google-calendar

The flow will print a URL and code. Open the URL in a browser, enter the
code, and authorize the application. The CLI will poll until authorization
completes, then save the token to disk (encrypted).`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return auth.RegisteredProviders(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: runOAuthConnect,
	}
}

// newOAuthStatusCmd creates the "config oauth status" command.
func newOAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show connected OAuth providers",
		Long:  "List all stored OAuth tokens and their connection status.",
		Args:  cobra.NoArgs,
		RunE:  runOAuthStatus,
	}
}

// newOAuthDisconnectCmd creates the "config oauth disconnect <provider>" command.
func newOAuthDisconnectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <provider>",
		Short: "remove stored token for a provider",
		Long: `Remove the stored OAuth token for the given provider.
The next connection will require a new device-code flow.`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return auth.RegisteredProviders(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: runOAuthDisconnect,
	}
}

// buildTokenStore creates an EncryptionKey (env var first, then machine key)
// and returns an initialised TokenStore.
func buildTokenStore() (*auth.TokenStore, error) {
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

// runOAuthConnect executes the device-code flow for the named provider.
func runOAuthConnect(cmd *cobra.Command, args []string) error {
	providerID := args[0]

	providerCfg, err := auth.ResolveProviderConfig(providerID)
	if err != nil {
		return err
	}

	store, err := buildTokenStore()
	if err != nil {
		return err
	}

	// Make the flow context cancellable via SIGINT/SIGTERM so the user can
	// abort the polling loop.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Debug("oauth connect interrupted by signal")
		cancel()
	}()

	flowCfg := providerCfg.DeviceFlowConfig()

	fmt.Printf("\nconnecting to %s...\n\n", providerID)

	// Step 1: request device code.
	dcr, err := auth.StartDeviceFlow(ctx, flowCfg)
	if err != nil {
		return fmt.Errorf("device code request failed: %w", err)
	}

	// Step 2: display instructions to user.
	fmt.Printf("  visit: %s\n", dcr.VerificationURI)
	fmt.Printf("  enter code: %s\n\n", dcr.UserCode)
	fmt.Print("  waiting for authorization...")

	// Step 3: poll for token.
	token, err := auth.PollForToken(ctx, flowCfg, dcr)
	if err != nil {
		// If the user pressed Ctrl+C, show a clean message.
		if ctx.Err() != nil {
			fmt.Println("\n  cancelled.")
			return nil
		}
		return fmt.Errorf("poll for token failed: %w", err)
	}

	fmt.Println(" \u2713")

	// Step 4: persist the token.
	if err := store.Save(providerID, token); err != nil {
		return fmt.Errorf("save token: %w", err)
	}

	fmt.Printf("\n  %s connected.\n", providerID)
	fmt.Printf("  token saved to %s/%s.json (encrypted)\n", store.Dir(), providerID)
	return nil
}

// runOAuthStatus lists stored tokens with human-readable expiry.
func runOAuthStatus(cmd *cobra.Command, args []string) error {
	store, err := buildTokenStore()
	if err != nil {
		return err
	}

	infos, err := store.List()
	if err != nil {
		return err
	}

	if len(infos) == 0 {
		fmt.Println("  no oauth connections.")
		return nil
	}

	fmt.Println()
	for _, info := range infos {
		now := time.Now()
		if info.Expiry.IsZero() || now.After(info.Expiry) {
			fmt.Printf("  %-20s  expired\n", info.Provider)
			continue
		}
		remaining := info.Expiry.Sub(now)
		fmt.Printf("  %-20s  connected  (expires in %s)\n", info.Provider, humanDuration(remaining))
	}
	fmt.Println()
	return nil
}

// runOAuthDisconnect deletes the stored token for the named provider.
func runOAuthDisconnect(cmd *cobra.Command, args []string) error {
	providerID := args[0]

	store, err := buildTokenStore()
	if err != nil {
		return err
	}

	if err := store.Delete(providerID); err != nil {
		return err
	}

	fmt.Printf("  %s disconnected.\n", providerID)
	return nil
}

// humanDuration formats a duration into a concise human-readable string.
func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
