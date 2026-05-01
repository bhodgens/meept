package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/caimlas/meept/internal/context"
)

func main() {
	// Get working directory
	workingDir := "."
	if len(os.Args) > 1 {
		workingDir = os.Args[1]
	}

	// Create artifact manager with 5-minute cache
	manager := context.NewArtifactManager(5 * time.Minute)

	fmt.Printf("Scanning directory: %s\n\n", workingDir)

	// Scan for artifacts
	artifacts, err := manager.ScanDirectory(workingDir)
	if err != nil {
		log.Fatalf("Error scanning directory: %v", err)
	}

	// Display results
	fmt.Println("=== Claude Artifact Scan Results ===")

	if !artifacts.Available {
		fmt.Println("No Claude artifacts found in this directory.")
		fmt.Println("The system will work normally without artifact-aware context.")
		return
	}

	fmt.Println("✓ Claude artifacts detected!")

	// CLAUDE.md
	if artifacts.HasCLAUDEMD() {
		doc := artifacts.CLAUDEMD
		fmt.Println("📄 CLAUDE.md:")
		fmt.Printf("  Path: %s\n", doc.Path)
		fmt.Printf("  Working Dir: %s\n", doc.WorkingDir)
		fmt.Printf("  Last Modified: %s\n", doc.LastModified.Format(time.RFC3339))

		// Build commands
		if len(doc.BuildCommands) > 0 {
			fmt.Printf("\n  Build Commands (%d):\n", len(doc.BuildCommands))
			for i, cmd := range doc.BuildCommands {
				if i < 5 { // Show first 5
					if cmd.Description != "" {
						fmt.Printf("    • %s: %s\n", cmd.Description, cmd.Command)
					} else {
						fmt.Printf("    • %s\n", cmd.Command)
					}
				}
			}
			if len(doc.BuildCommands) > 5 {
				fmt.Printf("    ... and %d more\n", len(doc.BuildCommands)-5)
			}
		}

		// Architecture
		if doc.Architecture != nil {
			fmt.Printf("\n  Architecture:")
			if len(doc.Architecture.RequestFlow) > 0 {
				fmt.Printf("    Request Flow: %d steps\n", len(doc.Architecture.RequestFlow))
			}
			if len(doc.Components) > 0 {
				fmt.Printf("    Components: %d mappings\n", len(doc.Components))
			}
		}

		// Agents
		if len(doc.Agents) > 0 {
			fmt.Printf("\n  Agents (%d):\n", len(doc.Agents))
			for _, agent := range doc.Agents {
				fmt.Printf("    • %s (%s)\n", agent.ID, agent.Role)
			}
		}
	}

	// .claude/ directory
	if artifacts.HasClaudeDir() {
		claudeDir := artifacts.ClaudeDir
		fmt.Printf("\n📁 .claude/:\n")
		fmt.Printf("  Path: %s\n", claudeDir.Path)

		// Skills
		if len(claudeDir.Skills) > 0 {
			fmt.Printf("\n  Skills (%d):\n", len(claudeDir.Skills))
			skillsByCategory := make(map[string][]*context.Skill)
			for _, skill := range claudeDir.Skills {
				skillsByCategory[skill.Category] = append(skillsByCategory[skill.Category], skill)
			}

			for category, skills := range skillsByCategory {
				fmt.Printf("\n    %s (%d):\n", category, len(skills))
				for _, skill := range skills {
					fmt.Printf("      • %s v%s\n", skill.Name, skill.Version)
				}
			}
		}

		// Mind file
		if claudeDir.MindFile != "" {
			fmt.Printf("\n  Mind File: %s\n", claudeDir.MindFile)
		}

		// Session files
		if len(claudeDir.SessionFiles) > 0 {
			fmt.Printf("  Session Files: %d\n", len(claudeDir.SessionFiles))
		}
	}

	// Demonstrate context building
	fmt.Println("=== Context Building Demo ===")

	builder := context.NewContextBuilder(artifacts)

	// Test different task types
	tasks := []string{
		"build this project",
		"run tests",
		"write a new feature",
		"explain the architecture",
	}

	for _, task := range tasks {
		taskCtx := builder.BuildForTask(task)
		fmt.Printf("Task: \"%s\"\n", task)
		fmt.Printf("  Type: %s\n", taskCtx.TaskType)
		fmt.Printf("  Relevant Sections: %v\n", taskCtx.RelevantSections)

		// Get relevant commands
		commands := builder.GetRelevantCommands(task)
		if len(commands) > 0 {
			fmt.Printf("  Commands: %d suggested\n", len(commands))
		}

		// Find skills
		skill := builder.FindSkillForTask(task)
		if skill != nil {
			fmt.Printf("  Skill: %s (%s)\n", skill.Name, skill.Category)
		}

		// Find agents
		agent := builder.FindAgentForTask(task)
		if agent != nil {
			fmt.Printf("  Agent: %s (%s)\n", agent.Name, agent.Role)
		}

		fmt.Println()
	}

	// Show cache stats
	stats := manager.GetCacheStats()
	fmt.Println("=== Cache Statistics ===")
	fmt.Printf("  Directories: %d\n", stats["scanners"])
	fmt.Printf("  Cache Entries: %d\n", stats["cache_entries"])
}
