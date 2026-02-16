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
