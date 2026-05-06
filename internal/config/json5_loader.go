package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

// LoadJSON5 reads a JSON5 file and unmarshals into v using hujson for standardization.
// Environment variables in the form ${VAR_NAME} or $VAR_NAME are expanded before parsing.
func LoadJSON5(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Expand environment variables in the raw JSON5 content
	content := expandEnvVars(string(data))
	// Standardize JSON5 to standard JSON using hujson
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}

// LoadJSON5WithDefault loads JSON5 from path. If the file does not exist, it
// leaves v unchanged and returns nil. Any other error is returned as-is.
func LoadJSON5WithDefault(path string, v any) error {
	if err := LoadJSON5(path, v); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}
