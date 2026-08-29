package acp

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func fakeAgentBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeagent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func startEcho(t *testing.T, mode string) *Session {
	t.Helper()
	bin := fakeAgentBin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	s, err := Start(ctx, SessionConfig{
		AgentID:     "echo",
		Command:     []string{bin, "-mode", mode},
		Cwd:         t.TempDir(),
		DialTimeout: 5 * time.Second,
		CallTimeout: 8 * time.Second,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return s
}

func TestSession_HandshakeReady(t *testing.T) {
	s := startEcho(t, "echo")
	if s.State() != StateReady {
		t.Fatalf("state = %v, want ready", s.State())
	}
}

func TestSession_HandshakeTimeout(t *testing.T) {
	bin := fakeAgentBin(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := Start(ctx, SessionConfig{
		AgentID:     "slow",
		Command:     []string{bin, "-mode", "slow"},
		Cwd:         t.TempDir(),
		DialTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestSession_CommandNotFound(t *testing.T) {
	_, err := Start(context.Background(), SessionConfig{
		AgentID: "missing",
		Command: []string{"/no/such/acp-agent-bin"},
		Cwd:     t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSession_BadHandshake(t *testing.T) {
	bin := fakeAgentBin(t)
	_, err := Start(context.Background(), SessionConfig{
		AgentID:     "bad",
		Command:     []string{bin, "-mode", "badhandshake"},
		Cwd:         t.TempDir(),
		DialTimeout: 3 * time.Second,
	})
	if err == nil {
		t.Fatal("expected handshake error")
	}
}

func TestSession_EchoSend(t *testing.T) {
	s := startEcho(t, "echo")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got, err := s.Send(ctx, "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestSession_CloseIdempotent(t *testing.T) {
	s := startEcho(t, "echo")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestSession_DieMidFlight(t *testing.T) {
	bin := fakeAgentBin(t)
	_, err := Start(context.Background(), SessionConfig{
		AgentID:     "die",
		Command:     []string{bin, "-mode", "die"},
		Cwd:         t.TempDir(),
		DialTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected die during handshake")
	}
}

func TestSession_PermissionPermissive(t *testing.T) {
	bin := fakeAgentBin(t)
	s, err := Start(context.Background(), SessionConfig{
		AgentID:        "perm",
		Command:        []string{bin, "-mode", "permission"},
		Cwd:            t.TempDir(),
		DialTimeout:    5 * time.Second,
		CallTimeout:    8 * time.Second,
		PermissionMode: "permissive",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	got, err := s.Send(ctx, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestSession_PermissionDeny(t *testing.T) {
	bin := fakeAgentBin(t)
	s, err := Start(context.Background(), SessionConfig{
		AgentID:        "perm",
		Command:        []string{bin, "-mode", "permission"},
		Cwd:            t.TempDir(),
		DialTimeout:    5 * time.Second,
		CallTimeout:    8 * time.Second,
		PermissionMode: "deny",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := s.Send(ctx, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
}
