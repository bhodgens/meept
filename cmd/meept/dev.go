package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
)

func newDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Developer mode commands",
		Long: `Developer tools for testing and debugging Meept.

Examples:
  meept dev models                  # List available models
  meept dev model                   # Show current model
  meept dev model 2                 # Switch to model by index
  meept dev model gala/dolphin-mistral-7b  # Switch to model by name
  meept dev test                    # Send test message to LLM
  meept dev config                  # Show current configuration`,
	}

	cmd.AddCommand(newDevModelsCmd())
	cmd.AddCommand(newDevModelCmd())
	cmd.AddCommand(newDevTestCmd())
	cmd.AddCommand(newDevConfigCmd())

	return cmd
}

func newDevModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List all configured models",
		RunE:  runDevModels,
	}
}

func newDevModelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "model [index|name]",
		Short: "Show or switch the current model",
		Long: `Without arguments, shows the current model.
With an argument, switches to that model (by index or name).

Examples:
  meept dev model                       # Show current
  meept dev model 1                     # Switch to model index 1
  meept dev model gala/dolphin-mistral-7b  # Switch by name`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDevModel,
	}
}

func newDevTestCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test message to the LLM",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevTest(message)
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "Hello! Please respond with a short greeting.", "Test message to send")

	return cmd
}

func newDevConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show current daemon configuration",
		RunE:  runDevConfig,
	}
}

func runDevModels(cmd *cobra.Command, args []string) error {
	client := tui.NewDaemonClient()

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept-daemon -f", err)
	}
	defer client.Close()

	result, err := client.Call("dev.list_models", nil)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	var resp struct {
		Models []struct {
			Index        int      `json:"index"`
			Provider     string   `json:"provider"`
			Model        string   `json:"model"`
			FullName     string   `json:"full_name"`
			BaseURL      string   `json:"base_url"`
			ContextLimit int      `json:"context_limit"`
			MaxOutput    int      `json:"max_output"`
			Capabilities []string `json:"capabilities"`
			Current      bool     `json:"current"`
		} `json:"models"`
		CurrentModel string `json:"current_model"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("Available Models (current: %s)\n\n", resp.CurrentModel)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tPROVIDER\tMODEL\tCONTEXT\tCAPABILITIES\t")
	fmt.Fprintln(w, "-\t--------\t-----\t-------\t------------\t")

	for _, m := range resp.Models {
		marker := " "
		if m.Current {
			marker = "*"
		}
		caps := strings.Join(m.Capabilities, ", ")
		fmt.Fprintf(w, "%s%d\t%s\t%s\t%dk\t%s\t\n",
			marker, m.Index, m.Provider, m.Model, m.ContextLimit/1000, caps)
	}
	w.Flush()

	fmt.Println("\nUse 'meept dev model <index>' or 'meept dev model <provider/model>' to switch")
	return nil
}

func runDevModel(cmd *cobra.Command, args []string) error {
	client := tui.NewDaemonClient()

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept-daemon -f", err)
	}
	defer client.Close()

	// No args - show current model
	if len(args) == 0 {
		result, err := client.Call("dev.current_model", nil)
		if err != nil {
			return fmt.Errorf("failed to get current model: %w", err)
		}

		var resp struct {
			Provider     string   `json:"provider"`
			Model        string   `json:"model"`
			FullName     string   `json:"full_name"`
			BaseURL      string   `json:"base_url"`
			ContextLimit int      `json:"context_limit"`
			MaxOutput    int      `json:"max_output"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.Unmarshal(result, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		fmt.Printf("Current Model: %s\n", resp.FullName)
		fmt.Printf("  Provider:    %s\n", resp.Provider)
		fmt.Printf("  Base URL:    %s\n", resp.BaseURL)
		fmt.Printf("  Context:     %d tokens\n", resp.ContextLimit)
		fmt.Printf("  Max Output:  %d tokens\n", resp.MaxOutput)
		fmt.Printf("  Capabilities: %s\n", strings.Join(resp.Capabilities, ", "))
		return nil
	}

	// Switch model by index or name
	selector := args[0]

	// Check if it's a numeric index
	params := map[string]any{}
	if idx, err := strconv.Atoi(selector); err == nil {
		params["index"] = idx
	} else {
		params["name"] = selector
	}

	result, err := client.Call("dev.switch_model", params)
	if err != nil {
		return fmt.Errorf("failed to switch model: %w", err)
	}

	var resp struct {
		Success  bool   `json:"success"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Success {
		fmt.Printf("Switched to: %s/%s\n", resp.Provider, resp.Model)
	} else {
		fmt.Printf("Failed: %s\n", resp.Message)
	}
	return nil
}

func runDevTest(message string) error {
	client := tui.NewDaemonClient()

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept-daemon -f", err)
	}
	defer client.Close()

	fmt.Printf("Sending test message: %q\n\n", message)

	result, err := client.Call("dev.test_llm", map[string]string{
		"message": message,
	})
	if err != nil {
		return fmt.Errorf("LLM test failed: %w", err)
	}

	var resp struct {
		Response string `json:"response"`
		Model    string `json:"model"`
		Tokens   struct {
			Prompt     int `json:"prompt"`
			Completion int `json:"completion"`
			Total      int `json:"total"`
		} `json:"tokens"`
		DurationMs int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("Model: %s\n", resp.Model)
	fmt.Printf("Duration: %dms\n", resp.DurationMs)
	fmt.Printf("Tokens: %d prompt + %d completion = %d total\n\n",
		resp.Tokens.Prompt, resp.Tokens.Completion, resp.Tokens.Total)
	fmt.Printf("Response:\n%s\n", resp.Response)
	return nil
}

func runDevConfig(cmd *cobra.Command, args []string) error {
	client := tui.NewDaemonClient()

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept-daemon -f", err)
	}
	defer client.Close()

	result, err := client.Call("dev.config", nil)
	if err != nil {
		return fmt.Errorf("failed to get config: %w", err)
	}

	// Pretty print JSON
	var out json.RawMessage
	if err := json.Unmarshal(result, &out); err != nil {
		fmt.Println(string(result))
		return nil
	}

	formatted, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(formatted))
	return nil
}
