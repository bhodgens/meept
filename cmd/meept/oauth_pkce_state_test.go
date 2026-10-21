package main

import (
	"strings"
	"testing"
)

func TestReadPastedCodeWithState_splitMatch(t *testing.T) {
	expectedState := "abc123"
	pasted := "codexyz#abc123"
	reader := strings.NewReader(pasted)

	code, err := readPastedCodeWithState(reader, expectedState)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if code != "codexyz" {
		t.Fatalf("expected code 'codexyz', got: %q", code)
	}
}

func TestReadPastedCodeWithState_splitMismatch(t *testing.T) {
	expectedState := "abc123"
	pasted := "codexyz#wrongstate"
	reader := strings.NewReader(pasted)

	code, err := readPastedCodeWithState(reader, expectedState)
	if err == nil {
		t.Fatalf("expected error, got: %q", code)
	}
	if !strings.Contains(err.Error(), "oauth state mismatch") {
		t.Fatalf("expected 'oauth state mismatch' error, got: %v", err)
	}
}

func TestReadPastedCodeWithState_bareCodePassthrough(t *testing.T) {
	expectedState := "abc123"
	pasted := "codexyz"
	reader := strings.NewReader(pasted)

	code, err := readPastedCodeWithState(reader, expectedState)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if code != "codexyz" {
		t.Fatalf("expected code 'codexyz', got: %q", code)
	}
}

func TestReadPastedCodeWithState_emptyCode(t *testing.T) {
	expectedState := "abc123"
	pasted := ""
	reader := strings.NewReader(pasted)

	code, err := readPastedCodeWithState(reader, expectedState)
	if err == nil {
		t.Fatalf("expected error, got: %q", code)
	}
	// An empty reader results in "no code entered" because scanner.Scan() returns false
	if !strings.Contains(err.Error(), "no code entered") {
		t.Fatalf("expected 'no code entered' error, got: %v", err)
	}
}

func TestReadPastedCodeWithState_codeOnlyWithHash(t *testing.T) {
	// Edge case: code part is empty (e.g., "#state")
	expectedState := "abc123"
	pasted := "#abc123"
	reader := strings.NewReader(pasted)

	code, err := readPastedCodeWithState(reader, expectedState)
	if err == nil {
		t.Fatalf("expected error, got: %q", code)
	}
	if !strings.Contains(err.Error(), "empty code in pasted value") {
		t.Fatalf("expected 'empty code in pasted value' error, got: %v", err)
	}
}
