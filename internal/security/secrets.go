package security

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/tailscale/hujson"
)

// SecretMode determines how a secret is handled.
type SecretMode string

const (
	SecretModeObfuscate SecretMode = "obfuscate" // reversible via placeholders
	SecretModeReplace   SecretMode = "replace"   // one-way replacement
)

// SecretEntry represents a single secret to obfuscate, as loaded from config.
type SecretEntry struct {
	Type    string     `json:"type"`    // "plain" or "regex"
	Content string     `json:"content"` // the secret string or regex pattern
	Mode    SecretMode `json:"mode"`    // "obfuscate" or "replace"
}

// secretEntry is the internal representation sorted by content length.
type secretEntry struct {
	content  string
	mode     SecretMode
	length   int
	compiled *regexp.Regexp // non-nil for regex type
}

// SecretObfuscator manages secret detection and obfuscation.
// It is safe for concurrent use.
type SecretObfuscator struct {
	mu             sync.RWMutex
	entries        []secretEntry
	obfuscateMap   map[string]string // placeholder -> original
	deobfuscateMap map[string]string // original -> placeholder
	compiledRegex  []*regexp.Regexp
	counter        uint64 // monotonic counter for placeholder generation
	logger         *slog.Logger
}

// envSecretPattern matches environment variable names that likely hold secrets.
var envSecretPattern = regexp.MustCompile(`(?i)(?:KEY|SECRET|TOKEN|PASSWORD|PASS|AUTH|CREDENTIAL|PRIVATE|OAUTH)(?:_|$)`)

// NewSecretObfuscator creates a new obfuscator populated with environment variable secrets.
func NewSecretObfuscator() *SecretObfuscator {
	s := &SecretObfuscator{
		obfuscateMap:   make(map[string]string),
		deobfuscateMap: make(map[string]string),
		logger:         slog.Default().With("component", "secret-obfuscator"),
	}
	s.loadEnvSecrets()
	return s
}

// loadEnvSecrets reads environment variables matching the secret pattern
// and registers those with values >= 8 characters.
func (s *SecretObfuscator) loadEnvSecrets() {
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || len(value) < 8 {
			continue
		}
		if !envSecretPattern.MatchString(key) {
			continue
		}
		s.addPlainEntry(value, SecretModeObfuscate)
	}
}

// LoadFromConfig loads explicit secrets from a JSON5 config file.
// The file format is: { "secrets": [{ "type": "plain"|"regex", "content": "...", "mode": "obfuscate"|"replace" }] }
func (s *SecretObfuscator) LoadFromConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.logger.Debug("secrets config file not found, skipping", "path", path)
			return nil
		}
		return fmt.Errorf("reading secrets config: %w", err)
	}

	stdJSON, err := hujson.Standardize(data)
	if err != nil {
		return fmt.Errorf("parsing secrets config JSON5: %w", err)
	}

	var cfg struct {
		Secrets []SecretEntry `json:"secrets"`
	}
	if err := json.Unmarshal(stdJSON, &cfg); err != nil {
		return fmt.Errorf("unmarshaling secrets config: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range cfg.Secrets {
		switch entry.Type {
		case "plain", "":
			s.addPlainEntryLocked(entry.Content, entry.Mode)
		case "regex":
			s.addRegexEntryLocked(entry.Content, entry.Mode)
		default:
			s.logger.Warn("unknown secret entry type, skipping", "type", entry.Type)
		}
	}

	s.sortEntriesLocked()
	return nil
}

// addPlainEntry adds a plain-text secret entry (thread-safe wrapper).
func (s *SecretObfuscator) addPlainEntry(content string, mode SecretMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addPlainEntryLocked(content, mode)
	s.sortEntriesLocked()
}

// addPlainEntryLocked adds a plain-text secret entry (caller must hold lock).
func (s *SecretObfuscator) addPlainEntryLocked(content string, mode SecretMode) {
	if content == "" {
		return
	}
	if mode == "" {
		mode = SecretModeObfuscate
	}
	// Skip duplicates.
	if _, exists := s.deobfuscateMap[content]; exists {
		return
	}
	s.entries = append(s.entries, secretEntry{
		content: content,
		mode:    mode,
		length:  len(content),
	})
}

// addRegexEntryLocked adds a regex-pattern secret entry (caller must hold lock).
func (s *SecretObfuscator) addRegexEntryLocked(pattern string, mode SecretMode) {
	if pattern == "" {
		return
	}
	if mode == "" {
		mode = SecretModeObfuscate
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		s.logger.Warn("invalid regex in secrets config, skipping", "pattern", pattern, "error", err)
		return
	}
	s.entries = append(s.entries, secretEntry{
		content:  pattern,
		mode:     mode,
		length:   0, // regex entries sort after plain entries
		compiled: re,
	})
	s.compiledRegex = append(s.compiledRegex, re)
}

// sortEntriesLocked sorts entries by descending content length so longer
// secrets are replaced first (caller must hold lock).
func (s *SecretObfuscator) sortEntriesLocked() {
	sort.SliceStable(s.entries, func(i, j int) bool {
		return s.entries[i].length > s.entries[j].length
	})
}

// placeholder generates a unique placeholder string like #AB12#.
func (s *SecretObfuscator) placeholder(index uint64) string {
	h := xxhash.Sum64([]byte(fmt.Sprintf("secret-%d", index)))
	return fmt.Sprintf("#%04X#", h&0xFFFF)
}

// Obfuscate replaces all secrets in the text with placeholders or replacement strings.
func (s *SecretObfuscator) Obfuscate(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := text

	for i := range s.entries {
		entry := &s.entries[i]

		if entry.compiled != nil {
			// Regex entry: find all matches and replace each.
			matches := entry.compiled.FindAllString(result, -1)
			seen := make(map[string]bool)
			for _, match := range matches {
				if seen[match] || match == "" {
					continue
				}
				seen[match] = true
				replacement := s.replacementFor(match, entry.mode)
				result = strings.ReplaceAll(result, match, replacement)
			}
		} else {
			// Plain text entry.
			if !strings.Contains(result, entry.content) {
				continue
			}
			replacement := s.replacementFor(entry.content, entry.mode)
			result = strings.ReplaceAll(result, entry.content, replacement)
		}
	}

	return result
}

// replacementFor returns the obfuscation placeholder or one-way replacement for a secret.
// Caller must hold s.mu.
func (s *SecretObfuscator) replacementFor(secret string, mode SecretMode) string {
	if mode == SecretModeReplace {
		return strings.Repeat("*", len(secret))
	}

	// Obfuscate mode: use a reversible placeholder.
	if ph, ok := s.deobfuscateMap[secret]; ok {
		return ph
	}

	// Generate a new unique placeholder.
	s.counter++
	ph := s.placeholder(s.counter)

	// Ensure uniqueness (extremely unlikely collision, but be safe).
	for _, exists := s.obfuscateMap[ph]; exists; {
		s.counter++
		ph = s.placeholder(s.counter)
	}

	s.obfuscateMap[ph] = secret
	s.deobfuscateMap[secret] = ph
	return ph
}

// Deobfuscate replaces placeholders back with original secrets.
func (s *SecretObfuscator) Deobfuscate(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := text
	for placeholder, original := range s.obfuscateMap {
		result = strings.ReplaceAll(result, placeholder, original)
	}
	return result
}

// ChatMessage is a minimal interface for message-like objects with a Content field.
// We use this to avoid importing the llm package (which would create a circular dependency).
type ChatMessage struct {
	Role    string
	Content string
}

// ObfuscateMessages obfuscates secrets in a slice of ChatMessage-like objects.
// The input is []any where each element is a map[string]any with a "content" field,
// or a struct with a Content field. Returns a new slice with obfuscated content.
func (s *SecretObfuscator) ObfuscateMessages(messages []any) []any {
	if len(messages) == 0 {
		return messages
	}

	result := make([]any, len(messages))
	for i, msg := range messages {
		result[i] = s.obfuscateMessage(msg)
	}
	return result
}

// DeobfuscateMessages reverses obfuscation on a slice of messages.
func (s *SecretObfuscator) DeobfuscateMessages(messages []any) []any {
	if len(messages) == 0 {
		return messages
	}

	result := make([]any, len(messages))
	for i, msg := range messages {
		result[i] = s.deobfuscateMessage(msg)
	}
	return result
}

// obfuscateMessage handles a single message (map or struct).
func (s *SecretObfuscator) obfuscateMessage(msg any) any {
	switch m := msg.(type) {
	case map[string]any:
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		if content, ok := cp["content"].(string); ok {
			cp["content"] = s.Obfuscate(content)
		}
		return cp
	default:
		// Cannot obfuscate unknown types; return as-is.
		return msg
	}
}

// deobfuscateMessage handles a single message (map or struct).
func (s *SecretObfuscator) deobfuscateMessage(msg any) any {
	switch m := msg.(type) {
	case map[string]any:
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		if content, ok := cp["content"].(string); ok {
			cp["content"] = s.Deobfuscate(content)
		}
		return cp
	default:
		return msg
	}
}
