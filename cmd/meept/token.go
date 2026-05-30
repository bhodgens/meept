// Token management commands for Meept CLI
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tailscale/hujson"
)

const (
	apiKeyPrefix = "meept_"
	apiKeyBytes  = 32
)

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "manage API tokens",
		Long:  "Generate, list, and revoke API tokens for authentication.",
	}

	cmd.AddCommand(newTokenGenerateCmd())
	cmd.AddCommand(newTokenListCmd())
	cmd.AddCommand(newTokenRevokeCmd())

	return cmd
}

// generateAPIKey creates a new cryptographically secure API key
func generateAPIKey() (string, error) {
	bytes := make([]byte, apiKeyBytes)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return apiKeyPrefix + base64.URLEncoding.EncodeToString(bytes), nil
}

// maskAPIKey returns a masked version of the key for display (e.g., "meept_...abcd")
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	prefix := ""
	if strings.HasPrefix(key, apiKeyPrefix) {
		prefix = apiKeyPrefix
	}
	suffix := key[len(key)-4:]
	return prefix + "..." + suffix
}

// configPaths returns the path to the main config file
func configPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".meept", "meept.json5"), nil
}

// loadConfigForModification loads the config file as hujson for modification
func loadConfigForModification() (hujson.Value, string, error) {
	cp, err := configPath()
	if err != nil {
		return hujson.Value{}, "", err
	}

	data, err := os.ReadFile(cp)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty config
			return hujson.Value{}, cp, nil
		}
		return hujson.Value{}, "", fmt.Errorf("failed to read config file: %w", err)
	}

	v, err := hujson.Parse(data)
	if err != nil {
		return hujson.Value{}, "", fmt.Errorf("failed to parse config JSON5: %w", err)
	}

	return v, cp, nil
}

// saveConfig saves the config file back to disk
func saveConfig(configPath string, v hujson.Value) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data := v.Pack()
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// extractAPIKeys reads the current API keys from config
func extractAPIKeys(v hujson.Value) ([]string, error) {
	packed := v.Pack()
	stdJSON, err := hujson.Standardize(packed)
	if err != nil {
		return nil, err
	}

	// Use gjson-like path extraction via json.Unmarshal
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(stdJSON, &cfg); err != nil {
		return nil, err
	}

	// Navigate to transport.http.api_keys
	transportRaw, ok := cfg["transport"]
	if !ok {
		return []string{}, nil
	}

	var transport map[string]json.RawMessage
	if err := json.Unmarshal(transportRaw, &transport); err != nil {
		return nil, err
	}

	httpRaw, ok := transport["http"]
	if !ok {
		return []string{}, nil
	}

	var httpCfg map[string]json.RawMessage
	if err := json.Unmarshal(httpRaw, &httpCfg); err != nil {
		return nil, err
	}

	apiKeysRaw, ok := httpCfg["api_keys"]
	if !ok {
		return []string{}, nil
	}

	var apiKeys []string
	if err := json.Unmarshal(apiKeysRaw, &apiKeys); err != nil {
		return nil, err
	}

	return apiKeys, nil
}

// modifyAPIKeysInConfig adds or removes an API key in the hujson AST
func modifyAPIKeysInConfig(v hujson.Value, keyToAdd, keyToRemove string) (hujson.Value, error) {
	packed := v.Pack()
	stdJSON, err := hujson.Standardize(packed)
	if err != nil {
		return hujson.Value{}, err
	}

	// Parse into a mutable structure
	var cfg map[string]interface{}
	if err := json.Unmarshal(stdJSON, &cfg); err != nil {
		return hujson.Value{}, err
	}

	// Ensure transport.http.api_keys exists
	transport, ok := cfg["transport"].(map[string]interface{})
	if !ok {
		transport = make(map[string]interface{})
		cfg["transport"] = transport
	}

	http, ok := transport["http"].(map[string]interface{})
	if !ok {
		http = make(map[string]interface{})
		transport["http"] = http
	}

	apiKeysRaw, ok := http["api_keys"]
	if !ok {
		apiKeysRaw = []interface{}{}
	}

	apiKeys, ok := apiKeysRaw.([]interface{})
	if !ok {
		apiKeys = []interface{}{}
	}

	if keyToAdd != "" {
		// Add key if not already present
		found := false
		for _, k := range apiKeys {
			if ks, ok := k.(string); ok && ks == keyToAdd {
				found = true
				break
			}
		}
		if !found {
			apiKeys = append(apiKeys, keyToAdd)
		}
	}

	if keyToRemove != "" {
		// Remove key
		newKeys := []interface{}{}
		for _, k := range apiKeys {
			if ks, ok := k.(string); ok && ks != keyToRemove {
				newKeys = append(newKeys, ks)
			}
		}
		apiKeys = newKeys
	}

	http["api_keys"] = apiKeys
	transport["http"] = http
	cfg["transport"] = transport

	// Convert back to JSON then to hujson
	newJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return hujson.Value{}, err
	}

	newV, err := hujson.Parse(newJSON)
	if err != nil {
		return hujson.Value{}, err
	}

	// Preserve comments from original by merging
	// For simplicity, just return the new parsed JSON5
	return newV, nil
}

func newTokenGenerateCmd() *cobra.Command {
	var saveToConfig bool

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "generate a new API token",
		Long:  "Generate a new cryptographically secure API token.\nUse --save to automatically add it to your config file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := generateAPIKey()
			if err != nil {
				return err
			}

			if saveToConfig {
				v, cp, err := loadConfigForModification()
				if err != nil {
					return err
				}

				v, err = modifyAPIKeysInConfig(v, key, "")
				if err != nil {
					return fmt.Errorf("failed to add key to config: %w", err)
				}

				if err := saveConfig(cp, v); err != nil {
					return err
				}

				fmt.Printf("Generated and saved API token: %s\n", key)
				fmt.Println("Restart the daemon for changes to take effect.")
			} else {
				fmt.Println(key)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&saveToConfig, "save", false, "Save the token directly to config file")

	return cmd
}

func newTokenListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list configured API tokens",
		Long:  "List all API tokens currently configured (masked for security).",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, _, err := loadConfigForModification()
			if err != nil {
				return err
			}

			keys, err := extractAPIKeys(v)
			if err != nil {
				return fmt.Errorf("failed to read API keys: %w", err)
			}

			if len(keys) == 0 {
				fmt.Println("No API tokens configured.")
				return nil
			}

			fmt.Printf("Configured API tokens (%d):\n", len(keys))
			for i, key := range keys {
				fmt.Printf("  %d. %s\n", i+1, maskAPIKey(key))
			}

			return nil
		},
	}

	return cmd
}

func newTokenRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <token>",
		Short: "revoke an API token",
		Long:  "Remove an API token from the configuration file.\nProvide the full token to revoke.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			v, cp, err := loadConfigForModification()
			if err != nil {
				return err
			}

			// Verify the key exists first
			keys, err := extractAPIKeys(v)
			if err != nil {
				return err
			}

			found := false
			for _, k := range keys {
				if k == key {
					found = true
					break
				}
			}

			if !found {
				return fmt.Errorf("API token not found in config")
			}

			v, err = modifyAPIKeysInConfig(v, "", key)
			if err != nil {
				return fmt.Errorf("failed to remove key from config: %w", err)
			}

			if err := saveConfig(cp, v); err != nil {
				return err
			}

			fmt.Printf("Revoked API token: %s\n", maskAPIKey(key))
			fmt.Println("Restart the daemon for changes to take effect.")

			return nil
		},
	}

	return cmd
}
