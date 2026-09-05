package auditlog

import "testing"

func TestSanitizePayload_BlocksSecretKeys(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"token", "api_token"},
		{"secret", "SECRET_KEY"},
		{"password", "user_password"},
		{"api_key", "api_key"},
		{"apikey", "apikey"},
		{"authorization", "Authorization"},
		{"credential", "aws_credential"},
		{"private_key", "client_private_key"},
		{"bearer", "bearer_token"},
		{"cookie", "session_cookie"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := map[string]any{tt.key: "supersecret", "keep": "visible"}
			out := SanitizePayload(in)
			if out[tt.key] != "[redacted]" {
				t.Fatalf("key %q: got %v", tt.key, out[tt.key])
			}
			if out["keep"] != "visible" {
				t.Fatalf("sibling key altered: %v", out["keep"])
			}
			if in[tt.key] != "supersecret" {
				t.Fatal("input map must not be mutated (deep copy)")
			}
		})
	}
}

func TestSanitizePayload_ScrubsCredentialLookingStrings(t *testing.T) {
	in := map[string]any{
		"evidence": "connected with sk-abcdefghijklmnopqrstuvwxyz012345 ok",
		"n":        42,
	}
	out := SanitizePayload(in)
	s, ok := out["evidence"].(string)
	if !ok {
		t.Fatalf("evidence not a string: %T", out["evidence"])
	}
	if s == in["evidence"] {
		t.Fatal("credential-looking string must be scrubbed")
	}
	if out["n"] != 42 {
		t.Fatalf("non-string values preserved: %v", out["n"])
	}
}

func TestSanitizePayload_TruncatesLongStrings(t *testing.T) {
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	out := SanitizePayload(map[string]any{"evidence": string(long)})
	s := out["evidence"].(string)
	if len(s) > 4200 {
		t.Fatalf("string not truncated: %d bytes", len(s))
	}
	if !hasSuffix(s, "...[truncated]") {
		t.Fatalf("missing truncation marker: %q", s[len(s)-20:])
	}
}

func TestSanitizePayload_RecursesAndHandlesNil(t *testing.T) {
	in := map[string]any{
		"nested": map[string]any{"password": "x", "deep": []any{"tok", 1}},
		"nilly":  nil,
	}
	out := SanitizePayload(in)
	nested := out["nested"].(map[string]any)
	if nested["password"] != "[redacted]" {
		t.Fatalf("nested secret leaked: %v", nested["password"])
	}
	if out["nilly"] != nil {
		t.Fatalf("nil must stay nil: %v", out["nilly"])
	}
}

func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }
