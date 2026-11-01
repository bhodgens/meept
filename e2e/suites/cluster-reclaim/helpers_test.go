package clusterreclaim

import "os"

// writeFile creates parent dirs and writes content - the config-hook
// helper (WithConfigHook runs after dirs exist; cluster/ does not).
func writeFile(path, content string) error {
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
