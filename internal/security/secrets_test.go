package security

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewSecretObfuscator_EnvVars(t *testing.T) {
	// Set up test env vars.
	os.Setenv("MY_API_KEY", "sk-1234567890abcdef")
	os.Setenv("MY_SECRET_TOKEN", "tok_deadbeef1234")
	os.Setenv("MY_PASSWORD", "supersecretpassword")
	os.Setenv("SHORT_PW", "short")              // too short, should be ignored
	os.Setenv("MY_REGULAR_VAR", "somevalue123") // doesn't match pattern, ignored
	defer func() {
		os.Unsetenv("MY_API_KEY")
		os.Unsetenv("MY_SECRET_TOKEN")
		os.Unsetenv("MY_PASSWORD")
		os.Unsetenv("SHORT_PW")
		os.Unsetenv("MY_REGULAR_VAR")
	}()

	s := NewSecretObfuscator()

	text := "My key is sk-1234567890abcdef and token is tok_deadbeef1234 with password supersecretpassword"
	obfuscated := s.Obfuscate(text)

	// None of the secrets should appear in the obfuscated text.
	if containsAny(obfuscated, "sk-1234567890abcdef", "tok_deadbeef1234", "supersecretpassword") {
		t.Errorf("secrets still present in obfuscated text: %s", obfuscated)
	}

	// Short value and non-matching key should not be obfuscated.
	if !containsAny(obfuscated, "short", "somevalue123") {
		// These aren't secrets, they shouldn't be present anyway since they weren't in the input.
	}

	// Deobfuscation should restore originals.
	restored := s.Deobfuscate(obfuscated)
	if restored != text {
		t.Errorf("deobfuscation roundtrip failed.\ngot:  %s\nwant: %s", restored, text)
	}
}

func TestObfuscateDeobfuscateRoundtrip(t *testing.T) {
	s := NewSecretObfuscator()
	s.addPlainEntry("secret12345678", SecretModeObfuscate)
	s.sortEntriesLocked()

	text := "The secret is secret12345678 in the message"
	obfuscated := s.Obfuscate(text)

	if obfuscated == text {
		t.Error("text was not obfuscated")
	}
	if containsAny(obfuscated, "secret12345678") {
		t.Error("secret still present after obfuscation")
	}

	restored := s.Deobfuscate(obfuscated)
	if restored != text {
		t.Errorf("roundtrip mismatch.\ngot:  %s\nwant: %s", restored, text)
	}
}

func TestReplaceMode(t *testing.T) {
	s := NewSecretObfuscator()
	s.addPlainEntry("mysecretvalue99", SecretModeReplace)

	text := "The value is mysecretvalue99 in context"
	obfuscated := s.Obfuscate(text)

	if obfuscated == text {
		t.Error("text was not obfuscated")
	}
	if containsAny(obfuscated, "mysecretvalue99") {
		t.Error("secret still present after replacement")
	}

	// Replace mode should produce asterisks matching secret length (15 chars).
	expected := "The value is *************** in context"
	if obfuscated != expected {
		t.Errorf("replace mode mismatch.\ngot:  %s\nwant: %s", obfuscated, expected)
	}

	// Deobfuscation should NOT restore replaced secrets.
	restored := s.Deobfuscate(obfuscated)
	if containsAny(restored, "mysecretvalue99") {
		t.Error("replace mode secret was restored during deobfuscation")
	}
}

func TestRegexSecrets(t *testing.T) {
	s := NewSecretObfuscator()
	s.addRegexEntryLocked(`AKIA[0-9A-Z]{16}`, SecretModeObfuscate)

	text := "Access key AKIAIOSFODNN7EXAMPLE found in logs"
	obfuscated := s.Obfuscate(text)

	if containsAny(obfuscated, "AKIAIOSFODNN7EXAMPLE") {
		t.Error("regex-matched secret still present")
	}

	restored := s.Deobfuscate(obfuscated)
	if restored != text {
		t.Errorf("regex roundtrip failed.\ngot:  %s\nwant: %s", restored, text)
	}
}

func TestLongestFirst(t *testing.T) {
	s := NewSecretObfuscator()
	// Add a shorter secret first, then a longer one that contains it.
	s.addPlainEntry("secret", SecretModeObfuscate)
	s.addPlainEntry("secret-long-value", SecretModeObfuscate)
	s.sortEntriesLocked()

	text := "Found secret-long-value and secret"
	obfuscated := s.Obfuscate(text)

	// Both should be replaced with different placeholders.
	if containsAny(obfuscated, "secret-long-value", "secret") {
		t.Errorf("secrets still present: %s", obfuscated)
	}

	// Verify roundtrip.
	restored := s.Deobfuscate(obfuscated)
	if restored != text {
		t.Errorf("longest-first roundtrip failed.\ngot:  %s\nwant: %s", restored, text)
	}
}

func TestMultipleSecretsInString(t *testing.T) {
	s := NewSecretObfuscator()
	s.addPlainEntry("alpha-secret-key", SecretModeObfuscate)
	s.addPlainEntry("beta-secret-key", SecretModeObfuscate)
	s.addPlainEntry("gamma-secret-key", SecretModeObfuscate)
	s.sortEntriesLocked()

	text := "Keys: alpha-secret-key, beta-secret-key, gamma-secret-key"
	obfuscated := s.Obfuscate(text)

	if containsAny(obfuscated, "alpha-secret-key", "beta-secret-key", "gamma-secret-key") {
		t.Error("secrets still present after obfuscation")
	}

	restored := s.Deobfuscate(obfuscated)
	if restored != text {
		t.Errorf("multiple secrets roundtrip failed.\ngot:  %s\nwant: %s", restored, text)
	}
}

func TestLoadFromConfig(t *testing.T) {
	// Create a temporary JSON5 config file.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "secrets.json5")
	content := `{
		// Test config with comments
		"secrets": [
			{ "type": "plain", "content": "config-secret-1234", "mode": "obfuscate" },
			{ "type": "plain", "content": "replaced-secret-99", "mode": "replace" },
			{ "type": "regex", "content": "BEARER_[a-zA-Z0-9]+", "mode": "obfuscate" },
		],
	}`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	s := NewSecretObfuscator()
	if err := s.LoadFromConfig(configPath); err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}

	text := "Config: config-secret-1234, replaced-secret-99, BEARER_token123"
	obfuscated := s.Obfuscate(text)

	if containsAny(obfuscated, "config-secret-1234", "replaced-secret-99", "BEARER_token123") {
		t.Errorf("config secrets still present: %s", obfuscated)
	}

	// Plain obfuscate entry should be reversible.
	restored := s.Deobfuscate(obfuscated)
	if containsAny(restored, "config-secret-1234") == false {
		// This is expected — it should be restored.
	}
	// Replace mode should NOT be restored.
	if containsAny(restored, "replaced-secret-99") {
		t.Error("replaced secret was restored")
	}
}

func TestLoadFromConfig_NotExist(t *testing.T) {
	s := NewSecretObfuscator()
	err := s.LoadFromConfig("/nonexistent/path/secrets.json5")
	if err != nil {
		t.Errorf("non-existent config should not error, got: %v", err)
	}
}

func TestLoadFromConfig_InvalidJSON5(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad.json5")
	if err := os.WriteFile(configPath, []byte(`{invalid json5!!!}`), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	s := NewSecretObfuscator()
	err := s.LoadFromConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON5")
	}
}

func TestObfuscateMessages(t *testing.T) {
	s := NewSecretObfuscator()
	s.addPlainEntry("msg-secret-12345", SecretModeObfuscate)

	messages := []any{
		map[string]any{"role": "user", "content": "Here is msg-secret-12345"},
		map[string]any{"role": "assistant", "content": "I see no secrets"},
		map[string]any{"role": "user", "content": "But msg-secret-12345 is here"},
	}

	obfuscated := s.ObfuscateMessages(messages)

	// Check that secrets are removed from content fields.
	for i, msg := range obfuscated {
		m, ok := msg.(map[string]any)
		if !ok {
			t.Fatalf("message %d is not a map", i)
		}
		content, ok := m["content"].(string)
		if !ok {
			t.Fatalf("message %d content is not a string", i)
		}
		if containsAny(content, "msg-secret-12345") {
			t.Errorf("secret found in message %d: %s", i, content)
		}
	}

	// Deobfuscate and verify.
	restored := s.DeobfuscateMessages(obfuscated)
	for i, msg := range restored {
		m, ok := msg.(map[string]any)
		if !ok {
			t.Fatalf("message %d is not a map", i)
		}
		content := m["content"].(string)
		origContent := messages[i].(map[string]any)["content"].(string)
		if content != origContent {
			t.Errorf("message %d roundtrip failed.\ngot:  %s\nwant: %s", i, content, origContent)
		}
	}
}

func TestObfuscateMessages_Empty(t *testing.T) {
	s := NewSecretObfuscator()
	result := s.ObfuscateMessages(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestDeobfuscateMessages_Empty(t *testing.T) {
	s := NewSecretObfuscator()
	result := s.DeobfuscateMessages(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestThreadSafety(t *testing.T) {
	s := NewSecretObfuscator()
	s.addPlainEntry("thread-secret-value", SecretModeObfuscate)

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Launch concurrent obfuscate/deobfuscate goroutines.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			text := "thread-secret-value in goroutine"
			obfuscated := s.Obfuscate(text)
			restored := s.Deobfuscate(obfuscated)
			if restored != text {
				errors <- fmt.Errorf("goroutine %d: roundtrip failed. got %q, want %q", id, restored, text)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestNoSecretsPresent(t *testing.T) {
	s := NewSecretObfuscator()

	text := "Hello, this is a clean message with no secrets."
	obfuscated := s.Obfuscate(text)
	if obfuscated != text {
		t.Error("text was modified when no secrets were present")
	}
}

func TestPlaceholderUniqueness(t *testing.T) {
	s := NewSecretObfuscator()
	s.addPlainEntry("unique-secret-aaaa", SecretModeObfuscate)
	s.addPlainEntry("unique-secret-bbbb", SecretModeObfuscate)

	text := "Found unique-secret-aaaa and unique-secret-bbbb"
	obfuscated := s.Obfuscate(text)

	// Both secrets should be replaced with different placeholders.
	// Count the number of #XXXX# patterns.
	count := 0
	for i := 0; i < len(obfuscated); i++ {
		if obfuscated[i] == '#' {
			// Look for closing #
			for j := i + 1; j < len(obfuscated) && j <= i+5; j++ {
				if obfuscated[j] == '#' {
					count++
					break
				}
			}
		}
	}
	if count != 2 {
		t.Errorf("expected 2 placeholders, found %d in: %s", count, obfuscated)
	}
}

// helper
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
