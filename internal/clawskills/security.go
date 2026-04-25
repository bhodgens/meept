// Package clawskills provides the ClawHub registry client for third-party skills.
package clawskills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Dangerous patterns to look for in skill files.
var dangerousPatterns = []*regexp.Regexp{
	// Shell command execution
	regexp.MustCompile(`(?i)(subprocess|os\.system|os\.popen|exec\.Command|cmd\.Run)`),
	// Network exfiltration
	regexp.MustCompile(`(?i)(curl|wget|httpx?\.post|requests\.post)\s*\([^)]*\b(api[_-]?key|secret|token|password)\b`),
	// File system operations on sensitive paths
	regexp.MustCompile(`(?i)(\/etc\/passwd|\/etc\/shadow|~\/\.ssh|~\/\.aws|~\/\.gnupg)`),
	// Eval/exec with dynamic code
	regexp.MustCompile(`(?i)\b(eval|exec)\s*\([^)]*\+`),
	// Base64 encoding of secrets
	regexp.MustCompile(`(?i)base64[.\s]*(encode|b64encode)\s*\([^)]*\b(password|secret|token|key)\b`),
}

// Suspicious patterns (warnings, not blocking).
var suspiciousPatterns = []*regexp.Regexp{
	// Any network access
	regexp.MustCompile(`(?i)(http\.client|requests\.|httpx\.|urllib)`),
	// File operations
	regexp.MustCompile(`(?i)(open\([^)]*['"](w|a|wb|ab)['"]|shutil\.(copy|move|rmtree))`),
	// Environment variable access
	regexp.MustCompile(`(?i)(os\.environ|os\.getenv|env\.get)`),
}

// Required files in a valid skill.
var requiredFiles = []string{
	"SKILL.md",
}

// Forbidden file extensions.
var forbiddenExtensions = []string{
	".exe", ".dll", ".so", ".dylib",
	".bat", ".cmd", ".ps1",
	".jar", ".class",
	".pyc", ".pyo",
}

// BlockedTools is the set of tools that clawskills are never allowed to use.
// These tools grant system-level access that third-party skills must not have.
var BlockedTools = []string{
	"shell_execute",   // Arbitrary shell commands
	"file_write",      // Direct file writing
	"file_delete",     // File deletion
	"sdk_install",     // SDK/package installation
	"security_bypass", // Security mechanism bypass
	"daemon_restart",  // Daemon control
	"config_write",    // Configuration modification
	"credential_set",  // Credential storage
}

// blockedToolPatterns are glob-like patterns for tool names that are blocked.
// Any tool whose name matches one of these patterns is denied.
var blockedToolPatterns = []string{
	"shell_*",
	"file_write*",
	"file_delete*",
	"security_*",
	"daemon_*",
	"config_*",
	"credential_*",
	"admin_*",
}

// SecurityChecker verifies skill downloads and extracted content.
type SecurityChecker struct {
	maxFileSize int64
}

// NewSecurityChecker creates a new SecurityChecker.
func NewSecurityChecker() *SecurityChecker {
	return &SecurityChecker{
		maxFileSize: 1024 * 1024, // 1 MB per file
	}
}

// VerifyDownload verifies a downloaded archive.
func (s *SecurityChecker) VerifyDownload(data []byte, expectedSHA256 string, verified bool) *VerificationResult {
	result := &VerificationResult{
		Valid:       true,
		SHA256Match: true,
		Signed:      verified,
	}

	// Verify SHA256
	hasher := sha256.New()
	hasher.Write(data)
	actualSHA256 := hex.EncodeToString(hasher.Sum(nil))

	if actualSHA256 != expectedSHA256 {
		result.Valid = false
		result.SHA256Match = false
		result.Errors = append(result.Errors, fmt.Sprintf("SHA256 mismatch: expected %s, got %s", expectedSHA256, actualSHA256))
	}

	// Warn if not verified
	if !verified {
		result.Warnings = append(result.Warnings, "Skill is not verified by ClawHub")
	}

	return result
}

// VerifyExtracted verifies extracted skill content for security issues.
func (s *SecurityChecker) VerifyExtracted(skillPath string) error {
	// Check for required files
	for _, required := range requiredFiles {
		path := filepath.Join(skillPath, required)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("missing required file: %s", required)
		}
	}

	// Walk the extracted files
	var issues []string
	err := filepath.Walk(skillPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(skillPath, path)

		// Check for forbidden extensions
		ext := strings.ToLower(filepath.Ext(path))
		for _, forbidden := range forbiddenExtensions {
			if ext == forbidden {
				issues = append(issues, fmt.Sprintf("forbidden file type: %s", relPath))
			}
		}

		// Check file size
		if info.Size() > s.maxFileSize {
			issues = append(issues, fmt.Sprintf("file too large: %s (%d bytes)", relPath, info.Size()))
		}

		// Check text files for dangerous patterns
		if isTextFile(ext) {
			if err := s.checkFileContent(path, relPath, &issues); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk skill directory: %w", err)
	}

	if len(issues) > 0 {
		return fmt.Errorf("security issues found:\n- %s", strings.Join(issues, "\n- "))
	}

	return nil
}

// checkFileContent checks a file's content for dangerous patterns.
func (s *SecurityChecker) checkFileContent(path, relPath string, issues *[]string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	contentStr := string(content)

	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(contentStr) {
			*issues = append(*issues, fmt.Sprintf("dangerous pattern in %s: %s", relPath, pattern.String()))
		}
	}

	return nil
}

// ScanFile scans a single file for security issues.
func (s *SecurityChecker) ScanFile(path string) *VerificationResult {
	result := &VerificationResult{Valid: true}

	content, err := os.ReadFile(path)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to read file: %v", err))
		return result
	}

	contentStr := string(content)

	// Check dangerous patterns
	for _, pattern := range dangerousPatterns {
		if pattern.MatchString(contentStr) {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("dangerous pattern: %s", pattern.String()))
		}
	}

	// Check suspicious patterns
	for _, pattern := range suspiciousPatterns {
		if pattern.MatchString(contentStr) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("suspicious pattern: %s", pattern.String()))
		}
	}

	return result
}

// isTextFile checks if a file extension indicates a text file.
func isTextFile(ext string) bool {
	textExtensions := map[string]bool{
		".md":    true,
		".txt":   true,
		".py":    true,
		".go":    true,
		".js":    true,
		".ts":    true,
		".json":  true,
		".yaml":  true,
		".yml":   true,
		".toml":  true,
		".sh":    true,
		".bash":  true,
		".zsh":   true,
		".fish":  true,
		".lua":   true,
		".rb":    true,
		".pl":    true,
		".php":   true,
		".java":  true,
		".c":     true,
		".cpp":   true,
		".h":     true,
		".hpp":   true,
		".rs":    true,
		".swift": true,
		".kt":    true,
		".scala": true,
		".css":   true,
		".html":  true,
		".xml":   true,
	}
	return textExtensions[ext]
}

// IsToolBlocked checks whether a specific tool name is blocked for clawskills.
// It checks both the exact blocked list and the glob patterns.
func IsToolBlocked(toolName string) bool {
	lower := strings.ToLower(toolName)

	// Check exact blocklist
	for _, blocked := range BlockedTools {
		if lower == strings.ToLower(blocked) {
			return true
		}
	}

	// Check pattern-based blocklist
	for _, pattern := range blockedToolPatterns {
		if matchToolPattern(lower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// FilterTools takes a list of requested tool names and returns only those
// that are allowed for clawskills execution. Blocked tools are silently
// removed.
func FilterTools(tools []string) []string {
	allowed := make([]string, 0, len(tools))
	for _, t := range tools {
		if !IsToolBlocked(t) {
			allowed = append(allowed, t)
		}
	}
	return allowed
}

// matchToolPattern matches a tool name against a glob pattern with "*"
// wildcards. Supports only trailing wildcards (e.g., "shell_*").
func matchToolPattern(name, pattern string) bool {
	if !strings.Contains(pattern, "*") {
		return name == pattern
	}

	parts := strings.SplitN(pattern, "*", 2)
	if len(parts) != 2 {
		return name == pattern
	}

	prefix := parts[0]
	suffix := parts[1]

	if prefix != "" && !strings.HasPrefix(name, prefix) {
		return false
	}
	if suffix != "" && !strings.HasSuffix(name, suffix) {
		return false
	}
	return true
}

// EnforceRiskLevel ensures the risk level is at least "high".
// ClawSkills cannot be assigned "low" or "medium" risk.
// Returns the enforced risk level.
func EnforceRiskLevel(requested string) string {
	levels := map[string]int{"low": 1, "medium": 2, "high": 3}
	requestedLevel, ok := levels[strings.ToLower(requested)]
	if !ok || requestedLevel < 3 {
		return DefaultRiskLevel
	}
	return DefaultRiskLevel
}
