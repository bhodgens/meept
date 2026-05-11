package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ParseCLAUDEMD parses a CLAUDE.md file
func ParseCLAUDEMD(path string) (*CLAUDEDocument, error) {
	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CLAUDE.md: %w", err)
	}

	// Get file info for modification time
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat CLAUDE.md: %w", err)
	}

	// Extract working directory from file path
	workingDir := filepath.Dir(path)
	workingDir, err = NormalizePath(workingDir)
	if err != nil {
		workingDir = filepath.Dir(path)
	}

	doc := &CLAUDEDocument{
		Path:         path,
		RawContent:   string(content),
		WorkingDir:   workingDir,
		LastModified: info.ModTime(),
	}

	// Parse the document
	parseDocument(doc)

	return doc, nil
}

// parseDocument parses the document structure
func parseDocument(doc *CLAUDEDocument) {
	// Extract sections
	doc.BuildCommands = extractBuildCommands(doc.RawContent)
	doc.Architecture = extractArchitectureSection(doc.RawContent)
	doc.Components = extractComponents(doc.RawContent)
	doc.Agents = extractAgents(doc.RawContent)
	doc.SecurityLayers = extractSecurityLayers(doc.RawContent)
	doc.Configuration = extractConfiguration(doc.RawContent)
	doc.Conventions = extractConventions(doc.RawContent)
	doc.ProjectStructure = extractProjectStructure(doc.RawContent)
}

// extractBuildCommands extracts build commands from the document
func extractBuildCommands(content string) []BuildCommand {
	var commands []BuildCommand

	// Find build commands section
	section := findSection(content, "Build Commands", "Build", "Build & Development Commands")
	if section == "" {
		return commands
	}

	// Parse code blocks in the section
	codeBlocks := extractCodeBlocks(section, "bash", "sh", "shell")

	for _, block := range codeBlocks {
		lines := strings.SplitSeq(block, "\n")
		for line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			// Simple command extraction
			cmd := BuildCommand{
				Command:  line,
				Category: inferCommandCategory(line),
			}

			// Try to extract description from preceding comment
			commands = append(commands, cmd)
		}
	}

	// Also look for command descriptions in the section
	descriptions := findCommandDescriptions(section)
	for _, desc := range descriptions {
		commands = append(commands, BuildCommand{
			Description: desc.description,
			Command:     desc.command,
			Category:    inferCommandCategory(desc.command),
		})
	}

	return commands
}

// commandDescription represents a command with its description
type commandDescription struct {
	description string
	command     string
}

// findCommandDescriptions finds commands with descriptions
func findCommandDescriptions(section string) []commandDescription {
	var descriptions []commandDescription

	lines := strings.Split(section, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])

		// Look for "```bash" or similar
		if after, ok := strings.CutPrefix(line, "```"); ok {
			lang := after
			if lang == "bash" || lang == "sh" || lang == "shell" {
				// Next line is the command
				if i+1 < len(lines) {
					command := strings.TrimSpace(lines[i+1])
					description := ""
					// Look back for description
					for j := i - 1; j >= 0 && j >= i-5; j-- {
						prevLine := strings.TrimSpace(lines[j])
						if prevLine != "" && !strings.HasPrefix(prevLine, "#") {
							description = prevLine
							break
						}
					}

					if command != "" {
						descriptions = append(descriptions, commandDescription{
							description: description,
							command:     command,
						})
					}
				}
			}
		}
	}

	return descriptions
}

// inferCommandCategory infers the category of a command
func inferCommandCategory(command string) string {
	cmd := strings.ToLower(command)

	switch {
	case strings.Contains(cmd, "build"):
		return "build"
	case strings.Contains(cmd, "test"):
		return "test"
	case strings.Contains(cmd, "lint") || strings.Contains(cmd, "check"):
		return "lint"
	case strings.Contains(cmd, "run") || strings.Contains(cmd, "start"):
		return "run"
	case strings.Contains(cmd, "deploy"):
		return "deploy"
	case strings.Contains(cmd, "install"):
		return "install"
	case strings.Contains(cmd, "clean"):
		return "clean"
	case strings.Contains(cmd, "format") || strings.Contains(cmd, "fmt"):
		return "format"
	default:
		return "other"
	}
}

// extractCodeBlocks extracts code blocks with specific languages
func extractCodeBlocks(content string, languages ...string) []string {
	var blocks []string

	// Pattern for code blocks
	pattern := regexp.MustCompile("```(" + strings.Join(languages, "|") + ")\n([^`]+)\n```")
	matches := pattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 2 {
			blocks = append(blocks, strings.TrimSpace(match[2]))
		}
	}

	return blocks
}

// findSection finds a section in the document
func findSection(content string, titles ...string) string {
	lines := strings.Split(content, "\n")
	var section strings.Builder
	inSection := false
	level := 0
	levelFound := false

	for i := range lines {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check if this line starts a section we want
		if !levelFound {
			found := false
			for _, title := range titles {
				// Match any heading level (# Title, ## Title, ### Title, etc.)
				if strings.HasPrefix(trimmed, "#") {
					// Extract heading level and text
					hashes := 0
					for _, ch := range trimmed {
						if ch == '#' {
							hashes++
						} else {
							break
						}
					}
					headingText := strings.TrimSpace(trimmed[hashes:])
					if headingText == title {
						inSection = true
						level = hashes
						levelFound = true
						found = true
						break
					}
				}
			}
			if found {
				continue
			}
		}

		if inSection {
			// Check if we've reached another section at same or higher level
			if strings.HasPrefix(trimmed, "#") {
				currentLevel := strings.Count(trimmed, "#")
				if currentLevel <= level {
					break
				}
			}

			// Add line to section
			if section.Len() > 0 {
				section.WriteString("\n")
			}
			section.WriteString(line)
		}
	}

	return section.String()
}

// extractArchitectureSection extracts architecture information
func extractArchitectureSection(content string) *ArchitectureSection {
	section := findSection(content, "Architecture Overview", "Architecture", "Architecture Diagram")
	if section == "" {
		return nil
	}

	arch := &ArchitectureSection{
		RequestFlow: extractFlowSteps(section),
		DataFlow:    extractDataFlow(section),
	}

	// Extract key components table
	arch.KeyComponents = extractComponentsTable(section)

	// Extract security layers
	arch.SecurityLayers = extractSecurityLayers(section)

	return arch
}

// extractFlowSteps extracts request flow steps
func extractFlowSteps(section string) []string {
	var steps []string

	// Look for flow diagram or numbered list
	lines := strings.SplitSeq(section, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		// Match numbered list items
		if match := regexp.MustCompile(`^(\d+\.|•|\-)\s+(.+)`).FindStringSubmatch(trimmed); len(match) > 2 {
			steps = append(steps, match[2])
		}
	}

	return steps
}

// extractDataFlow extracts data flow information
func extractDataFlow(section string) []DataFlowStep {
	var steps []DataFlowStep

	// Look for "->" arrows indicating flow
	lines := strings.SplitSeq(section, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "->") {
			parts := strings.Split(trimmed, "->")
			if len(parts) >= 2 {
				step := DataFlowStep{
					From:   strings.TrimSpace(parts[0]),
					To:     strings.TrimSpace(parts[len(parts)-1]),
					Action: trimmed,
				}
				steps = append(steps, step)
			}
		}
	}

	return steps
}

// extractComponents extracts component mappings
func extractComponents(content string) []ComponentMapping {
	section := findSection(content, "Key Components", "Components")
	if section == "" {
		return nil
	}

	return extractComponentsTable(section)
}

// extractComponentsTable extracts a components table
func extractComponentsTable(section string) []ComponentMapping {
	var components []ComponentMapping

	lines := strings.Split(section, "\n")
	var inTable bool

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for table start (has | and not separator line)
		if strings.Contains(trimmed, "|") && !inTable && !strings.HasPrefix(trimmed, "|---") {
			inTable = true
			continue
		}

		// Check for separator line
		if inTable && strings.HasPrefix(trimmed, "|---") {
			continue
		}

		// Parse table row
		if inTable && strings.HasPrefix(trimmed, "|") {
			cells := parseTableRow(trimmed)
			if len(cells) >= 2 {
				component := ComponentMapping{
					Layer:    strings.TrimSpace(cells[0]),
					Packages: parsePackageList(strings.TrimSpace(cells[1])),
				}
				components = append(components, component)
			}
		}
	}

	return components
}

// parseTableRow parses a markdown table row
func parseTableRow(row string) []string {
	// Remove leading/trailing pipes
	row = strings.Trim(row, "|")
	// Split by pipe
	parts := strings.Split(row, "|")

	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}

	return cells
}

// parsePackageList parses a list of packages
func parsePackageList(s string) []string {
	var packages []string

	// Split by comma
	parts := strings.SplitSeq(s, ",")
	for part := range parts {
		pkg := strings.TrimSpace(part)
		if pkg != "" {
			packages = append(packages, pkg)
		}
	}

	return packages
}

// extractAgents extracts agent definitions
func extractAgents(content string) []AgentDefinition {
	section := findSection(content, "Multi-Agent Architecture", "Agents", "Coworker Awareness")
	if section == "" {
		return nil
	}

	var agents []AgentDefinition

	// Look for agent tables
	lines := strings.Split(section, "\n")
	var inTable bool
	var idIndex, roleIndex, purposeIndex = -1, -1, -1

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for table start (contains "agent" and has |)
		if strings.Contains(strings.ToLower(trimmed), "agent") && strings.Contains(trimmed, "|") && !inTable {
			inTable = true
			headers := parseTableRow(trimmed)
			for i, header := range headers {
				lowerHeader := strings.ToLower(header)
				if strings.Contains(lowerHeader, "id") {
					idIndex = i
				} else if strings.Contains(lowerHeader, "role") {
					roleIndex = i
				} else if strings.Contains(lowerHeader, "purpose") {
					purposeIndex = i
				}
			}
			continue
		}

		// Check for separator line
		if inTable && strings.HasPrefix(trimmed, "|---") {
			continue
		}

		// Parse table row
		if inTable && strings.HasPrefix(trimmed, "|") {
			cells := parseTableRow(trimmed)
			if idIndex >= 0 && len(cells) > idIndex {
				agent := AgentDefinition{
					ID:   strings.TrimSpace(cells[idIndex]),
					Name: strings.TrimSpace(cells[idIndex]),
				}
				if roleIndex >= 0 && len(cells) > roleIndex {
					agent.Role = strings.TrimSpace(cells[roleIndex])
				}
				if purposeIndex >= 0 && len(cells) > purposeIndex {
					agent.Purpose = strings.TrimSpace(cells[purposeIndex])
				}
				agents = append(agents, agent)
			}
		}
	}

	return agents
}

// extractSecurityLayers extracts security layer definitions
func extractSecurityLayers(content string) []SecurityLayer {
	var layers []SecurityLayer

	section := findSection(content, "Security Layers", "Security")
	if section == "" {
		return layers
	}

	// Look for numbered lists of security layers
	lines := strings.SplitSeq(section, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)

		// Match numbered list
		if match := regexp.MustCompile(`^(\d+)\.\s+\*\*(.+?)\*\*\s*:\s*(.+)`).FindStringSubmatch(trimmed); len(match) > 3 {
			layer := SecurityLayer{
				Name:        match[2],
				Description: match[3],
			}
			layers = append(layers, layer)
		}
	}

	return layers
}

// extractConfiguration extracts configuration file references
func extractConfiguration(content string) []ConfigReference {
	var configs []ConfigReference

	section := findSection(content, "Configuration")
	if section == "" {
		return configs
	}

	// Look for file paths in code blocks
	codeBlocks := extractCodeBlocks(section, "toml", "json", "yaml", "yml")

	for _, block := range codeBlocks {
		// Extract paths like "~/.meept/meept.toml"
		pathPattern := regexp.MustCompile(`[~/][^\s\)]+\.(toml|json|yaml|yml)`)
		matches := pathPattern.FindAllString(block, -1)

		for _, match := range matches {
			config := ConfigReference{
				Path:   match,
				Format: getFileExtension(match),
			}
			configs = append(configs, config)
		}
	}

	return configs
}

// getFileExtension returns the file extension without the dot
func getFileExtension(path string) string {
	if idx := strings.LastIndex(path, "."); idx != -1 {
		return path[idx+1:]
	}
	return ""
}

// extractConventions extracts coding conventions
func extractConventions(content string) *CodeConventions {
	section := findSection(content, "Code Conventions", "Conventions", "UI Conventions")
	if section == "" {
		return nil
	}

	conventions := &CodeConventions{
		Language:     inferLanguage(content),
		Patterns:     extractPatterns(section),
		UIDirectives: extractUIDirectives(section),
	}

	return conventions
}

// inferLanguage infers the programming language
func inferLanguage(content string) string {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "go ") || strings.Contains(lower, "golang"):
		return "go"
	case strings.Contains(lower, "python"):
		return "python"
	case strings.Contains(lower, "javascript") || strings.Contains(lower, "typescript"):
		return "javascript"
	default:
		return "unknown"
	}
}

// extractPatterns extracts code patterns
func extractPatterns(section string) []string {
	var patterns []string

	lines := strings.SplitSeq(section, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		// Match list items
		if match := regexp.MustCompile(`^[-*]\s+(.+)`).FindStringSubmatch(trimmed); len(match) > 1 {
			patterns = append(patterns, match[1])
		}
	}

	return patterns
}

// extractUIDirectives extracts UI-specific conventions
func extractUIDirectives(section string) []string {
	var directives []string

	lines := strings.SplitSeq(section, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		// Look for "lowercase" or UI-related directives
		if strings.Contains(strings.ToLower(trimmed), "lowercase") ||
			strings.Contains(strings.ToLower(trimmed), "ui element") {
			directives = append(directives, trimmed)
		}
	}

	return directives
}

// extractProjectStructure extracts project structure information
func extractProjectStructure(content string) *ProjectTree {
	section := findSection(content, "Project Structure", "Directory Structure")
	if section == "" {
		return nil
	}

	tree := &ProjectTree{
		Root:        "",
		Directories: []string{},
		Files:       []string{},
	}

	// Parse tree structure from code block
	codeBlocks := extractCodeBlocks(section, "")
	for _, block := range codeBlocks {
		lines := strings.SplitSeq(block, "\n")
		for line := range lines {
			trimmed := strings.TrimSpace(line)
			// Identify directories (ending with /)
			if strings.HasSuffix(trimmed, "/") {
				tree.Directories = append(tree.Directories, trimmed)
			}
			// Identify files
			if trimmed != "" && !strings.HasSuffix(trimmed, "/") {
				tree.Files = append(tree.Files, trimmed)
			}
		}
	}

	return tree
}

// GetCommandsForContext returns commands relevant to a context
func (cd *CLAUDEDocument) GetCommandsForContext(ctx string) []BuildCommand {
	var relevant []BuildCommand

	ctxLower := strings.ToLower(ctx)

	for _, cmd := range cd.BuildCommands {
		// Check if command category matches
		if strings.Contains(ctxLower, cmd.Category) {
			relevant = append(relevant, cmd)
			continue
		}

		// Check if any context keyword matches
		for _, keyword := range cmd.Context {
			if strings.Contains(ctxLower, strings.ToLower(keyword)) {
				relevant = append(relevant, cmd)
				break
			}
		}
	}

	return relevant
}

// GetAgentForTask finds an agent suitable for a task
func (cd *CLAUDEDocument) GetAgentForTask(task string) *AgentDefinition {
	taskLower := strings.ToLower(task)

	for i := range cd.Agents {
		agent := &cd.Agents[i]

		// Check capabilities
		for _, capability := range agent.Capabilities {
			if strings.Contains(taskLower, strings.ToLower(capability)) {
				return agent
			}
		}

		// Check purpose
		if strings.Contains(taskLower, strings.ToLower(agent.Purpose)) {
			return agent
		}

		// Check role
		if strings.Contains(taskLower, strings.ToLower(agent.Role)) {
			return agent
		}
	}

	return nil
}
