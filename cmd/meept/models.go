package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/caimlas/meept/internal/llm"
	"github.com/spf13/cobra"
)

func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage LLM models and providers",
		Long: `Manage LLM models and providers interactively.

Examples:
  meept models                    # Interactive setup wizard
  meept models list               # List all configured models
  meept models providers          # List available providers
  meept models add                # Add a new provider/model
  meept models remove <ref>       # Remove a model
  meept models set-default <ref>  # Set default model
  meept models config             # Show/edit configuration`,
	}

	cmd.AddCommand(newModelsListCmd())
	cmd.AddCommand(newModelsProvidersCmd())
	cmd.AddCommand(newModelsAddCmd())
	cmd.AddCommand(newModelsRemoveCmd())
	cmd.AddCommand(newModelsSetDefaultCmd())
	cmd.AddCommand(newModelsConfigCmd())
	cmd.AddCommand(newModelsSetupCmd())
	cmd.AddCommand(newModelsCredentialsCmd())

	return cmd
}

// newModelsSetupCmd creates the interactive setup wizard
func newModelsSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive model setup wizard",
		RunE:  runModelsSetup,
	}
}

func runModelsSetup(cmd *cobra.Command, args []string) error {
	// Use the model picker
	provider, model, err := llm.RunModelPicker(llm.ModelPickerConfig{
		Title:    "Model Setup Wizard",
		ShowHelp: true,
		AllowCustom: true,
	})
	if err != nil {
		return fmt.Errorf("model picker failed: %w", err)
	}

	if provider == nil || model == nil {
		fmt.Println("Setup cancelled")
		return nil
	}

	fmt.Printf("\nSelected provider: %s (%s)\n", provider.Name, provider.ID)
	fmt.Printf("Selected model: %s (%s)\n", model.Name, model.ModelID)

	// Check for API key
	if provider.AuthType == llm.AuthAPIKey {
		apiKey := os.Getenv(provider.APIKeyEnvVar)
		if apiKey == "" {
			fmt.Printf("\nWarning: API key not found in %s\n", provider.APIKeyEnvVar)
			fmt.Printf("You can set it with: meept models credentials add %s\n", provider.ID)
		}
	}

	// Save to config
	if err := saveModelToConfig(provider.ID, model); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println("\nModel configuration saved!")
	return nil
}

func saveModelToConfig(providerID string, model *llm.ModelCatalogEntry) error {
	cfg, err := loadOrCreateModelsConfig()
	if err != nil {
		return err
	}

	// Check if provider exists
	if _, exists := cfg.Providers[providerID]; !exists {
		// Add new provider
		providerDef, ok := llm.GetProviderByID(providerID)
		if !ok {
			return fmt.Errorf("provider not found: %s", providerID)
		}

		cfg.Providers[providerID] = llm.ProviderConfig{
			API: string(providerDef.Transport),
			Options: llm.ProviderOptionsConfig{
				BaseURL: providerDef.BaseURL,
				APIKey:  "${" + providerDef.APIKeyEnvVar + "}",
				Timeout: 300,
			},
			Models: make(map[string]llm.ModelDef),
		}
	}

	// Add model if not exists
	if _, exists := cfg.Providers[providerID].Models[model.ModelID]; !exists {
		cfg.Providers[providerID].Models[model.ModelID] = llm.ModelDef{
			Name:         model.ModelID,
			Capabilities: model.Capabilities,
			InputCost:    model.InputCost,
			OutputCost:   model.OutputCost,
			ContextLimit: model.ContextWindow,
			MaxOutput:    model.MaxOutput,
			Temperature:  0.7,
		}
	}

	// Set as default if no default is set
	if cfg.Model == "" {
		cfg.Model = fmt.Sprintf("%s/%s", providerID, model.ModelID)
	}

	return writeModelsConfig(cfg)
}

func loadOrCreateModelsConfig() (*llm.ProvidersConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".meept", "models.json5")

	// Try to load existing config
	if _, err := os.Stat(configPath); err == nil {
		return llm.LoadProvidersConfig(configPath)
	}

	// Create new config
	return &llm.ProvidersConfig{
		Providers: make(map[string]llm.ProviderConfig),
	}, nil
}

func writeModelsConfig(cfg *llm.ProvidersConfig) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".meept")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "models.json5")

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

// newModelsListCmd creates the models list command
func newModelsListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured models",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsList(jsonOutput)
		},
	}

	cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output as JSON")
	return cmd
}

func runModelsList(jsonOutput bool) error {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Table output
	fmt.Println("Configured Models:")
	for providerID, provider := range cfg.Providers {
		fmt.Printf("[%s]\n", providerID)
		for modelID, model := range provider.Models {
			current := ""
			if cfg.Model == fmt.Sprintf("%s/%s", providerID, modelID) {
				current = " (default)"
			}
			fmt.Printf("  - %s: %s%s\n", modelID, model.Name, current)
		}
		fmt.Println()
	}
	return nil
}

// newModelsProvidersCmd creates the providers list command
func newModelsProvidersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "List available providers",
		RunE:  runModelsProviders,
	}
}

func runModelsProviders(cmd *cobra.Command, args []string) error {
	fmt.Println("Available Providers:")
	for _, p := range llm.CanonicalProviders {
		fmt.Printf("  %-20s %s\n", p.Name, p.ID)
		fmt.Printf("    Auth: %s, Transport: %s\n", p.AuthType, p.Transport)
		if p.APIKeyEnvVar != "" {
			fmt.Printf("    Env: %s\n", p.APIKeyEnvVar)
		}
		if len(p.Supports) > 0 {
			fmt.Printf("    Supports: %s\n", p.Supports)
		}
		fmt.Println()
	}
	return nil
}

// newModelsAddCmd creates the model add command
func newModelsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add",
		Short: "Add a new provider/model",
		RunE:  runModelsAdd,
	}
}

func runModelsAdd(cmd *cobra.Command, args []string) error {
	// Launch interactive picker
	provider, model, err := llm.RunModelPicker(llm.ModelPickerConfig{
		Title: "Add Model",
	})
	if err != nil {
		return err
	}
	if provider == nil || model == nil {
		fmt.Println("Cancelled")
		return nil
	}

	if err := saveModelToConfig(provider.ID, model); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Added %s/%s\n", provider.ID, model.ModelID)
	return nil
}

// newModelsRemoveCmd creates the model remove command
func newModelsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <provider/model>",
		Short: "Remove a model",
		Args:  cobra.ExactArgs(1),
		RunE:  runModelsRemove,
	}
}

func runModelsRemove(cmd *cobra.Command, args []string) error {
	modelRef := args[0]

	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Parse ref
	parts := splitModelRef(modelRef)
	if len(parts) != 2 {
		return fmt.Errorf("invalid model reference: %s (use provider/model format)", modelRef)
	}

	providerID, modelID := parts[0], parts[1]

	provider, exists := cfg.Providers[providerID]
	if !exists {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	if _, exists := provider.Models[modelID]; !exists {
		return fmt.Errorf("model not found: %s/%s", providerID, modelID)
	}

	delete(provider.Models, modelID)

	// Remove provider if no models left
	if len(provider.Models) == 0 {
		delete(cfg.Providers, providerID)
	}

	// Clear default if removed model was default
	if cfg.Model == modelRef {
		cfg.Model = ""
	}

	return writeModelsConfig(cfg)
}

// newModelsSetDefaultCmd creates the set-default command
func newModelsSetDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-default <provider/model>",
		Short: "Set the default model",
		Args:  cobra.ExactArgs(1),
		RunE:  runModelsSetDefault,
	}
}

func runModelsSetDefault(cmd *cobra.Command, args []string) error {
	modelRef := args[0]

	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Validate reference exists
	parts := splitModelRef(modelRef)
	if len(parts) != 2 {
		return fmt.Errorf("invalid model reference: %s (use provider/model format)", modelRef)
	}

	providerID, modelID := parts[0], parts[1]

	provider, exists := cfg.Providers[providerID]
	if !exists {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	if _, exists := provider.Models[modelID]; !exists {
		return fmt.Errorf("model not found: %s/%s", providerID, modelID)
	}

	cfg.Model = modelRef
	return writeModelsConfig(cfg)
}

// newModelsConfigCmd creates the config command
func newModelsConfigCmd() *cobra.Command {
	var editor string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or edit configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsConfig(editor)
		},
	}

	cmd.Flags().StringVarP(&editor, "editor", "e", "", "Editor to use (default: $EDITOR)")
	return cmd
}

func runModelsConfig(editor string) error {
	homeDir, _ := os.UserHomeDir()
	configPath := filepath.Join(homeDir, ".meept", "models.json5")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s", configPath)
	}

	if editor != "" {
		// Open in editor
		cmd := filepath.Base(editor)
		args := []string{configPath}
		if err := execCommand(cmd, args...); err != nil {
			return fmt.Errorf("failed to open editor: %w", err)
		}
	} else {
		// Show config
		data, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	}
	return nil
}

// splitModelRef splits a "provider/model" reference
func splitModelRef(ref string) []string {
	for i, c := range ref {
		if c == '/' {
			return []string{ref[:i], ref[i+1:]}
		}
	}
	return []string{ref}
}

// execCommand executes a command with the given arguments.
func execCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec // path is constructed from known config values
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Credential management commands

func newModelsCredentialsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credentials",
		Short: "Manage API credentials",
	}

	cmd.AddCommand(newModelsCredAddCmd())
	cmd.AddCommand(newModelsCredRemoveCmd())
	cmd.AddCommand(newModelsCredListCmd())

	return cmd
}

func newModelsCredAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <provider_id>",
		Short: "Add API credential for a provider",
		Args:  cobra.ExactArgs(1),
		RunE:  runModelsCredAdd,
	}
}

func newModelsCredRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <provider_id>",
		Short: "Remove API credential",
		Args:  cobra.ExactArgs(1),
		RunE:  runModelsCredRemove,
	}
}

func newModelsCredListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored credentials",
		RunE:  runModelsCredList,
	}
}

func runModelsCredAdd(cmd *cobra.Command, args []string) error {
	providerID := args[0]

	// Validate provider exists
	provider, ok := llm.GetProviderByID(providerID)
	if !ok {
		return fmt.Errorf("unknown provider: %s", providerID)
	}

	// Get credential store
	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".meept")
	cs, err := llm.NewCredentialStore(stateDir)
	if err != nil {
		return fmt.Errorf("failed to open credential store: %w", err)
	}

	// Prompt for API key
	fmt.Printf("Enter API key for %s (%s): ", provider.Name, provider.APIKeyEnvVar)
	var apiKey string
	_, _ = fmt.Scanln(&apiKey)

	if err := cs.Set(providerID, apiKey); err != nil {
		return fmt.Errorf("failed to save credential: %w", err)
	}

	fmt.Println("Credential saved successfully")
	return nil
}

func runModelsCredRemove(cmd *cobra.Command, args []string) error {
	providerID := args[0]

	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".meept")
	cs, err := llm.NewCredentialStore(stateDir)
	if err != nil {
		return fmt.Errorf("failed to open credential store: %w", err)
	}

	if err := cs.Delete(providerID); err != nil {
		return fmt.Errorf("failed to remove credential: %w", err)
	}

	fmt.Println("Credential removed")
	return nil
}

func runModelsCredList(cmd *cobra.Command, args []string) error {
	homeDir, _ := os.UserHomeDir()
	stateDir := filepath.Join(homeDir, ".meept")
	cs, err := llm.NewCredentialStore(stateDir)
	if err != nil {
		return fmt.Errorf("failed to open credential store: %w", err)
	}

	providers := cs.List()
	if len(providers) == 0 {
		fmt.Println("No credentials stored")
		return nil
	}

	fmt.Println("Stored credentials:")
	for _, id := range providers {
		fmt.Printf("  - %s\n", id)
	}
	return nil
}
