package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanAWSKey(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  string
		match bool
	}{
		{"valid access key", "my key is AKIAIOSFODNN7EXAMPLE ok", "aws_access_key", true},
		{"no match", "nothing secret here", "", false},
		{"partial prefix only", "AKIA12345", "", false},
	}
	s := NewSecretScanner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := s.Scan(tt.text)
			if tt.match {
				require.Contains(t, matches, tt.want)
			} else {
				assert.NotContains(t, matches, "aws_access_key")
			}
		})
	}
}

func TestScanGitHubToken(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  string
		match bool
	}{
		{"classic token", "token=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij1234", "github_token", true},
		{"fine grained", "pat=github_pat_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUV", "github_fine_grained", true},
		{"no match", "just some text", "", false},
	}
	s := NewSecretScanner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := s.Scan(tt.text)
			if tt.match {
				require.Contains(t, matches, tt.want)
			} else {
				assert.Empty(t, matches)
			}
		})
	}
}

func TestScanOpenAIKey(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  string
		match bool
	}{
		{"valid key", "sk-" + strings.Repeat("A", 20) + "T3BlbkFJ" + strings.Repeat("B", 20), "openai_key", true},
		{"no match", "sk-short", "", false},
	}
	s := NewSecretScanner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := s.Scan(tt.text)
			if tt.match {
				require.Contains(t, matches, tt.want)
			} else {
				assert.NotContains(t, matches, "openai_key")
			}
		})
	}
}

func TestScanPrivateKey(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  string
		match bool
	}{
		{"rsa header", "-----BEGIN RSA PRIVATE KEY-----\nMIIE...", "private_key_header", true},
		{"openssh header", "-----BEGIN OPENSSH PRIVATE KEY-----", "private_key_header", true},
		{"ec header", "-----BEGIN EC PRIVATE KEY-----", "private_key_header", true},
		{"no match", "-----BEGIN PUBLIC KEY-----", "", false},
	}
	s := NewSecretScanner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := s.Scan(tt.text)
			if tt.match {
				require.Contains(t, matches, tt.want)
			} else {
				assert.NotContains(t, matches, "private_key_header")
			}
		})
	}
}

func TestScanCleanText(t *testing.T) {
	s := NewSecretScanner()
	matches := s.Scan("This is a perfectly normal log line with no secrets at all.")
	assert.Empty(t, matches)
}

func TestScanAndReport(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		expectEmpty bool
		contains    string
	}{
		{"clean text", "nothing to see here", true, ""},
		{"secret found", "key=AKIAIOSFODNN7EXAMPLE", false, "WARNING: potential secrets detected"},
		{"report lists rule", "key=AKIAIOSFODNN7EXAMPLE", false, "aws_access_key"},
	}
	s := NewSecretScanner()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := s.ScanAndReport(tt.text)
			if tt.expectEmpty {
				assert.Empty(t, report)
			} else {
				require.NotEmpty(t, report)
				assert.Contains(t, report, tt.contains)
			}
		})
	}
}

func TestScanMultipleMatches(t *testing.T) {
	text := `AKIAIOSFODNN7EXAMPLE and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij1234`
	s := NewSecretScanner()
	matches := s.Scan(text)
	require.Len(t, matches, 2)
	assert.Contains(t, matches, "aws_access_key")
	assert.Contains(t, matches, "github_token")

	report := s.ScanAndReport(text)
	assert.True(t, strings.Contains(report, "aws_access_key"))
	assert.True(t, strings.Contains(report, "github_token"))
}
