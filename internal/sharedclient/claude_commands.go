// Package sharedclient provides shared client utilities for meept.
package sharedclient

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// discoverClaudeCommands scans ~/.claude/commands/ for markdown command files
// and returns a map of command name to CustomCommand.
//
// This provides compatibility with Claude Code's slash command format.
// Claude Code stores commands in ~/.claude/commands/<name>.md with YAML frontmatter.
func discoverClaudeCommands(claudeCommandsPath string) map[string]CustomCommand {
	cmds := make(map[string]CustomCommand)

	// Expand ~ in path
	if strings.HasPrefix(claudeCommandsPath, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return cmds
		}
		claudeCommandsPath = filepath.Join(homeDir, claudeCommandsPath[1:])
	}

	entries, err := os.ReadDir(claudeCommandsPath)
	if err != nil {
		// Directory doesn't exist - this is fine, just return empty
		return cmds
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		cmd, err := parseCommandFile(filepath.Join(claudeCommandsPath, entry.Name()))
		if err != nil {
			slog.Warn("Failed to parse Claude command file",
				"path", entry.Name(),
				"error", err)
			continue
		}

		if cmd.Name == "" {
			slog.Warn("Claude command has no name, skipping",
				"path", entry.Name())
			continue
		}

		cmds[cmd.Name] = cmd
	}

	return cmds
}

// claudeCommandsPath returns the path to ~/.claude/commands/
func claudeCommandsPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".claude", "commands")
}
