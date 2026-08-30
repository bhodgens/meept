package tui

import (
	"encoding/json"
	"fmt"
)

func strMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func boolMap(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func intMap(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	}
	return 0
}

func anyArray(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func intArraySize(m map[string]any, key string) int {
	v, _ := m[key].([]any)
	return len(v)
}

// evalListPayload parses the eval.list RPC response.
func evalListPayload(result json.RawMessage) ([]map[string]any, error) {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(result, &rawMap); err != nil {
		return nil, fmt.Errorf("parse eval.list: %w", err)
	}
	if errRaw, ok := rawMap["error"]; ok && string(errRaw) != `"null"` && string(errRaw) != `""` {
		var errStr string
		json.Unmarshal(errRaw, &errStr)
		return nil, fmt.Errorf("eval.list error: %s", errStr)
	}
	runsWithRunsRaw, ok := rawMap["runs"]
	if !ok {
		return nil, fmt.Errorf("eval.list: missing runs field")
	}
	var runs []map[string]any
	if err := json.Unmarshal(runsWithRunsRaw, &runs); err != nil {
		return nil, fmt.Errorf("parse runs: %w", err)
	}
	return runs, nil
}
