package security

import (
	"regexp"
	"strings"
	"sync"
)

// StrictnessLevel controls how aggressively the sanitizer filters input.
type StrictnessLevel int

const (
	StrictnessPermissive StrictnessLevel = iota
	StrictnessStandard
	StrictnessStrict
)

// String returns the human-readable name of the strictness level.
func (s StrictnessLevel) String() string {
	switch s {
	case StrictnessPermissive:
		return "permissive"
	case StrictnessStandard:
		return "standard"
	case StrictnessStrict:
		return "strict"
	default:
		return "unknown"
	}
}

// Warning represents a detected threat or modification warning.
type Warning struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// SanitizationResult holds the outcome of sanitization.
type SanitizationResult struct {
	CleanText       string    `json:"clean_text"`
	WasModified     bool      `json:"was_modified"`
	ThreatsDetected []Warning `json:"threats_detected,omitempty"`
}

// OutputScanResult holds the result of credential detection.
type OutputScanResult struct {
	HasCredentials bool      `json:"has_credentials"`
	Warnings       []Warning `json:"warnings,omitempty"`
	RedactedText   string    `json:"redacted_text"`
}

// injectionPattern holds a compiled pattern with metadata.
type injectionPattern struct {
	Pattern  *regexp.Regexp
	Label    string
	MinLevel StrictnessLevel
}

// structuralToken holds a token to escape.
type structuralToken struct {
	Pattern *regexp.Regexp
	Display string
}

var (
	injectionPatterns  []injectionPattern
	structuralTokens   []structuralToken
	roleMarkerRE       *regexp.Regexp
	credentialPatterns []*credentialPattern
	initOnce           sync.Once
)

type credentialPattern struct {
	Pattern *regexp.Regexp
	Label   string
}

// initPatterns initializes all compiled patterns.
func initPatterns() {
	initOnce.Do(func() {
		// Injection patterns
		injectionPatterns = []injectionPattern{
			// Instruction override attempts
			{
				Pattern:  regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier|preceding)\s+(instructions?|prompts?|rules?|guidelines?|directions?)`),
				Label:    LabelInstructionOverride,
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)(disregard|forget|override|bypass|skip)\s+(all\s+)?(previous|prior|above|earlier|preceding)?\s*(instructions?|prompts?|rules?|guidelines?|directions?|constraints?)`),
				Label:    LabelInstructionOverride,
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)(you\s+are\s+now|act\s+as|pretend\s+(to\s+be|you\s+are)|roleplay\s+as|switch\s+to\s+role|enter\s+.{0,20}\s+mode)`),
				Label:    "role_switch_attempt",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)new\s+instructions?\s*:`),
				Label:    "instruction_injection",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)(do\s+not|don'?t)\s+(follow|obey|listen\s+to)\s+(your|the|any)\s+(rules?|instructions?|guidelines?|system\s+prompt)`),
				Label:    LabelInstructionOverride,
				MinLevel: StrictnessPermissive,
			},
			// Role markers in user text
			{
				Pattern:  regexp.MustCompile(`(?im)^\s*system\s*:`),
				Label:    "role_marker_system",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?im)^\s*assistant\s*:`),
				Label:    "role_marker_assistant",
				MinLevel: StrictnessPermissive,
			},
			{
				// Tightened pattern: requires quoted value to avoid false positives on prompt-injection vectors
				Pattern:  regexp.MustCompile(`(?im)^\s*user\s*:\s*"`),
				Label:    "role_marker_user",
				MinLevel: StrictnessStandard,
			},
			// Markdown code-fence role injection
			{
				Pattern:  regexp.MustCompile("(?i)```\\s*system"),
				Label:    "markdown_role_injection",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile("(?i)```\\s*assistant"),
				Label:    "markdown_role_injection",
				MinLevel: StrictnessPermissive,
			},
			// Special token injection (ChatML / Llama / Mistral)
			{
				Pattern:  regexp.MustCompile(`(?i)<\|im_start\|>`),
				Label:    "special_token_chatml",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<\|im_end\|>`),
				Label:    "special_token_chatml",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)\[INST\]`),
				Label:    "special_token_llama",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)\[/INST\]`),
				Label:    "special_token_llama",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<<SYS>>`),
				Label:    "special_token_llama_sys",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<</SYS>>`),
				Label:    "special_token_llama_sys",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<\|system\|>`),
				Label:    LabelSpecialTokenPhi,
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<\|user\|>`),
				Label:    LabelSpecialTokenPhi,
				MinLevel: StrictnessStandard,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<\|assistant\|>`),
				Label:    LabelSpecialTokenPhi,
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)<\|endoftext\|>`),
				Label:    "special_token_eos",
				MinLevel: StrictnessPermissive,
			},
			// STRICT-only: aggressive heuristics
			// Word boundary added to avoid matching "show instructions" in legitimate contexts
			{
				Pattern:  regexp.MustCompile(`(?i)\b(reveal|show|print|output|display|tell\s+me)\s+(your\s+)?(system\s+prompt|instructions?|hidden\s+prompt|rules?)\b`),
				Label:    "prompt_extraction_attempt",
				MinLevel: StrictnessStrict,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)(repeat|echo|recite)\s+(everything|all|the\s+text)\s+(above|before|so\s+far)`),
				Label:    "prompt_extraction_attempt",
				MinLevel: StrictnessStrict,
			},
			// FIX #SECURITY: Social engineering detection patterns
			// Authority claims
			// Word boundary added to avoid matching "I am developer" in legitimate coding contexts
			{
				Pattern:  regexp.MustCompile(`(?i)\b(i\s+am|this\s+is)\s+(your?\s+)?(admin|administrator|owner|boss|manager|supervisor|developer|system\s+admin|root\s+user)\b`),
				Label:    "social_engineering_authority",
				MinLevel: StrictnessStandard,
			},
			// Word boundary added to avoid matching "As your developer, I recommend" in legitimate contexts
			{
				Pattern:  regexp.MustCompile(`(?i)\b(as\s+your?\s+)?(creator|developer|admin|owner)\s*,?\s*(i\s+)?(order|command|authorize|instruct|direct)\s+you\b`),
				Label:    "social_engineering_authority",
				MinLevel: StrictnessStandard,
			},
			// Urgency triggers
			{
				Pattern:  regexp.MustCompile(`(?i)(urgent|emergency|critical|immediate|asap|right\s+now|instantly|without\s+delay|immediately)`),
				Label:    "social_engineering_urgency",
				MinLevel: StrictnessPermissive,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)(this\s+is\s+not\s+a\s+(drill|test|simulation)|time\s+sensitive|act\s+fast|no\s+time)`),
				Label:    "social_engineering_urgency",
				MinLevel: StrictnessStandard,
			},
			// Credential requests
			{
				Pattern:  regexp.MustCompile(`(?i)(what\s+is|give\s+me|show\s+me|tell\s+me|share|provide)\s+(your?\s+)?(password|api\s+key|secret|token|credential|private\s+key|auth\s+code)`),
				Label:    "credential_request_attempt",
				MinLevel: StrictnessStandard,
			},
			{
				Pattern:  regexp.MustCompile(`(?i)(send\s+me|transfer|wire|pay)\s+(money|funds|crypto|bitcoin|payment)`),
				Label:    "financial_request_attempt",
				MinLevel: StrictnessStrict,
			},
			// Emotional manipulation
			{
				Pattern:  regexp.MustCompile(`(?i)(please\s+i'?m\s+(begging|desperate)|help\s+me\s+(please|or\s+i('|i'?ll))|this\s+is\s+(life\s+or\s+death|critical|extremely\s+important))`),
				Label:    "social_engineering_emotional",
				MinLevel: StrictnessPermissive,
			},
			// Trust building attempts
			// Word boundary added to avoid false positives on partial matches
			{
				Pattern:  regexp.MustCompile(`(?i)\b(trust\s+me|you\s+can\s+(trust|rely)\s+on\s+me|we'?re\s+friends|I'?m\s+(here\s+to\s+)?help\s+you)\b`),
				Label:    "social_engineering_trust",
				MinLevel: StrictnessPermissive,
			},
		}

		// Structural tokens to escape
		structuralTokens = []structuralToken{
			{Pattern: regexp.MustCompile(`(?i)<\|im_start\|>`), Display: "<|im_start|>"},
			{Pattern: regexp.MustCompile(`(?i)<\|im_end\|>`), Display: "<|im_end|>"},
			{Pattern: regexp.MustCompile(`(?i)\[INST\]`), Display: "[INST]"},
			{Pattern: regexp.MustCompile(`(?i)\[/INST\]`), Display: "[/INST]"},
			{Pattern: regexp.MustCompile(`(?i)<<SYS>>`), Display: "<<SYS>>"},
			{Pattern: regexp.MustCompile(`(?i)<</SYS>>`), Display: "<</SYS>>"},
			{Pattern: regexp.MustCompile(`(?i)<\|system\|>`), Display: "<|system|>"},
			{Pattern: regexp.MustCompile(`(?i)<\|user\|>`), Display: "<|user|>"},
			{Pattern: regexp.MustCompile(`(?i)<\|assistant\|>`), Display: "<|assistant|>"},
			{Pattern: regexp.MustCompile(`(?i)<\|endoftext\|>`), Display: "<|endoftext|>"},
		}

		// Role marker pattern
		roleMarkerRE = regexp.MustCompile(`(?im)^\s*(system|assistant|user)\s*:\s*`)

		// Credential detection patterns
		credentialPatterns = []*credentialPattern{
			// API keys
			{Pattern: regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[=:]\s*['"]?([a-zA-Z0-9_\-]{20,})['"]?`), Label: "api_key"},
			{Pattern: regexp.MustCompile(`(?i)sk-[a-zA-Z0-9]{32,}`), Label: "openai_key"},
			{Pattern: regexp.MustCompile(`(?i)ANTHROPIC_API_KEY\s*[=:]\s*['"]?([a-zA-Z0-9_\-]{32,})['"]?`), Label: "anthropic_key"},
			// Tokens
			{Pattern: regexp.MustCompile(`(?i)(access[_-]?token|auth[_-]?token|bearer)\s*[=:]\s*['"]?([a-zA-Z0-9_\-\.]{20,})['"]?`), Label: "access_token"},
			{Pattern: regexp.MustCompile(`(?i)ghp_[a-zA-Z0-9]{36}`), Label: "github_token"},
			{Pattern: regexp.MustCompile(`(?i)gho_[a-zA-Z0-9]{36}`), Label: "github_oauth_token"},
			{Pattern: regexp.MustCompile(`(?i)glpat-[a-zA-Z0-9\-]{20}`), Label: "gitlab_token"},
			// Passwords
			{Pattern: regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[=:]\s*['"]?([^\s'"]{8,})['"]?`), Label: "password"},
			// AWS
			{Pattern: regexp.MustCompile(`AKIA[A-Z0-9]{16}`), Label: "aws_access_key"},
			{Pattern: regexp.MustCompile(`(?i)aws[_-]?secret[_-]?access[_-]?key\s*[=:]\s*['"]?([a-zA-Z0-9/+]{40})['"]?`), Label: "aws_secret_key"},
			// Private keys
			{Pattern: regexp.MustCompile(`-{5}BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-{5}`), Label: "private_key"},
			{Pattern: regexp.MustCompile(`-{5}BEGIN PGP PRIVATE KEY BLOCK-{5}`), Label: "pgp_private_key"},
			// Database connection strings
			{Pattern: regexp.MustCompile(`(?i)(mongodb|postgres|mysql|redis)://\S+:\S+@\S+`), Label: "database_url"},
			// JWT tokens
			{Pattern: regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`), Label: "jwt_token"},
			// Slack
			{Pattern: regexp.MustCompile(`xox[baprs]-\d{10,13}-\d{10,13}[a-zA-Z0-9-]*`), Label: "slack_token"},
			// Stripe
			{Pattern: regexp.MustCompile(`(?i)sk_live_[a-zA-Z0-9]{24,}`), Label: "stripe_key"},
			{Pattern: regexp.MustCompile(`(?i)rk_live_[a-zA-Z0-9]{24,}`), Label: "stripe_restricted_key"},
		}
	})
}

// InputSanitizer provides two-layer sanitization for untrusted text.
type InputSanitizer struct {
	Strictness StrictnessLevel
}

// NewInputSanitizer creates a new input sanitizer with the given strictness.
func NewInputSanitizer(strictness StrictnessLevel) *InputSanitizer {
	initPatterns()
	return &InputSanitizer{Strictness: strictness}
}

// Sanitize runs the full sanitization pipeline on the input text.
func (s *InputSanitizer) Sanitize(text string) SanitizationResult {
	// Layer 1: Pattern detection
	threats := s.detectPatterns(text)

	// Layer 2: Structural cleanup
	cleanText, structModified := s.sanitizeStructure(text)

	wasModified := structModified || len(threats) > 0

	return SanitizationResult{
		CleanText:       cleanText,
		WasModified:     wasModified,
		ThreatsDetected: threats,
	}
}

// IsSafe performs a quick safety check without modifying the text.
func (s *InputSanitizer) IsSafe(text string) bool {
	return len(s.detectPatterns(text)) == 0
}

// detectPatterns scans for injection patterns at the current strictness level.
func (s *InputSanitizer) detectPatterns(text string) []Warning {
	seen := make(map[string]bool)
	var threats []Warning

	for _, p := range injectionPatterns {
		if s.Strictness < p.MinLevel {
			continue
		}
		if p.Pattern.MatchString(text) && !seen[p.Label] {
			seen[p.Label] = true
			threats = append(threats, Warning{
				Type:    p.Label,
				Message: "Detected injection pattern: " + p.Label,
			})
		}
	}

	return threats
}

// sanitizeStructure escapes special tokens and strips role markers.
func (s *InputSanitizer) sanitizeStructure(text string) (string, bool) {
	modified := false

	// Escape special tokens by inserting a zero-width space
	for _, tok := range structuralTokens {
		if tok.Pattern.MatchString(text) {
			// Insert zero-width space after first character
			replacement := string(tok.Display[0]) + "\u200b" + tok.Display[1:]
			text = tok.Pattern.ReplaceAllString(text, replacement)
			modified = true
		}
	}

	// Strip role markers at line beginnings (STANDARD and above)
	if s.Strictness >= StrictnessStandard {
		newText := roleMarkerRE.ReplaceAllString(text, "")
		if newText != text {
			modified = true
			text = newText
		}
	}

	return text, modified
}

// OutputMonitor detects credentials and sensitive data in output text.
type OutputMonitor struct{}

// NewOutputMonitor creates a new output monitor.
func NewOutputMonitor() *OutputMonitor {
	initPatterns()
	return &OutputMonitor{}
}

// redactCredential redacts a credential match by showing first and last 4 characters
// with asterisks in between. For short secrets (<=8 chars), all characters are replaced.
func redactCredential(match string) string {
	if len(match) <= 8 {
		return strings.Repeat("*", len(match))
	}
	return match[:4] + strings.Repeat("*", len(match)-8) + match[len(match)-4:]
}

// Scan checks text for credential leaks and returns warnings.
func (o *OutputMonitor) Scan(text string) OutputScanResult {
	var warnings []Warning
	redacted := text

	for _, cp := range credentialPatterns {
		if cp.Pattern.MatchString(text) {
			warnings = append(warnings, Warning{
				Type:    cp.Label,
				Message: "Detected potential credential: " + cp.Label,
			})
			// Redact the match
			redacted = cp.Pattern.ReplaceAllStringFunc(redacted, redactCredential)
		}
	}

	return OutputScanResult{
		HasCredentials: len(warnings) > 0,
		Warnings:       warnings,
		RedactedText:   redacted,
	}
}

// HasCredentials checks if text contains any credential patterns.
func (o *OutputMonitor) HasCredentials(text string) bool {
	for _, cp := range credentialPatterns {
		if cp.Pattern.MatchString(text) {
			return true
		}
	}
	return false
}

// DetectAndRedact detects credentials and returns redacted text.
func (o *OutputMonitor) DetectAndRedact(text string) (string, bool) {
	result := o.Scan(text)
	return result.RedactedText, result.HasCredentials
}
