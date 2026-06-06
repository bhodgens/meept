package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

// LoadJSON5 reads a JSON5 file, expands environment variables, standardizes to JSON, and unmarshals into v.
func LoadJSON5(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Expand env vars in raw content
	content := expandEnvVars(string(data))
	// Standardize JSON5 to JSON
	stdJSON, err := hujson.Standardize([]byte(content))
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}

// LoadJSON5WithDefault loads JSON5 from path, or returns default if not found.
func LoadJSON5WithDefault(path string, v any) error {
	if err := LoadJSON5(path, v); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// UnmarshalJSON5 parses JSON5-encoded data into v. It standardizes the
// JSON5 to standard JSON before unmarshaling.
func UnmarshalJSON5(data []byte, v any) error {
	stdJSON, err := hujson.Standardize(data)
	if err != nil {
		return fmt.Errorf("failed to parse JSON5: %w", err)
	}
	return json.Unmarshal(stdJSON, v)
}
