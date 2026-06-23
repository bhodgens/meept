package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

func TestFileWatcherHook_MatchesPattern(t *testing.T) {
	hook := NewFileWatcherHook("*.go", 0, nil, nil)

	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"test.go", true},
		{"main.go.bak", false},
		{"dir/main.go", true},
	}

	for _, tt := range tests {
		got := hook.matchesPattern(tt.path)
		if got != tt.want {
			t.Errorf("matchesPattern(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFileWatcherHook_ShouldIgnore(t *testing.T) {
	hook := NewFileWatcherHook("*.go", 0, []string{".git/", "node_modules/"}, nil)

	tests := []struct {
		path string
		want bool
	}{
		{".git/config", true},
		{"node_modules/pkg/index.js", true},
		{"main.go", false},
		{"src/main.go", false},
	}

	for _, tt := range tests {
		got := hook.shouldIgnore(tt.path)
		if got != tt.want {
			t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestFileWatcherHook_StartStop(t *testing.T) {
	hook := NewFileWatcherHook("*.go", 0, nil, slog.Default())

	ctx := context.Background()
	if err := hook.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := hook.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if err := hook.Stop(); err != nil {
		t.Fatalf("Stop() second call error = %v", err)
	}
}

func TestFileWatcherHook_GlobMatching(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "test_test.go", true},
		{"*_test.go", "foo_test.go", true},
		{"*_test.go", "main.go", false},
	}

	for _, tt := range tests {
		hook := NewFileWatcherHook(tt.pattern, 0, nil, nil)
		got := hook.matchesPattern(tt.path)
		if got != tt.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestFileWatcherHook_Callback(t *testing.T) {
	var called int32
	hook := NewFileWatcherHook("*.go", 10*time.Millisecond, nil, slog.Default())
	hook.Callback = func(path string) {
		atomic.AddInt32(&called, 1)
	}

	ctx := context.Background()
	if err := hook.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer hook.Stop()

	time.Sleep(50 * time.Millisecond)
	t.Logf("callback called %d times", atomic.LoadInt32(&called))
}

func TestFileWatcherHook_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	var triggered int32
	hook := NewFileWatcherHook("*.txt", 50*time.Millisecond, nil, slog.Default())
	hook.Callback = func(path string) {
		atomic.AddInt32(&triggered, 1)
	}

	ctx := context.Background()
	if err := hook.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer hook.Stop()

	time.Sleep(100 * time.Millisecond)
	f, err := os.Create("test.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	time.Sleep(200 * time.Millisecond)
	t.Logf("triggered = %d", atomic.LoadInt32(&triggered))
}

func TestFileWatcherHook_AsyncCallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	var triggered int32
	hook := NewFileWatcherHook("*.txt", 50*time.Millisecond, nil, slog.Default())
	hook.Async = true
	hook.Callback = func(path string) {
		atomic.AddInt32(&triggered, 1)
		time.Sleep(20 * time.Millisecond) // simulate I/O
	}

	ctx := context.Background()
	if err := hook.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer hook.Stop()

	time.Sleep(100 * time.Millisecond)
	f, err := os.Create("test_async.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Wait for debounce + callback.
	time.Sleep(200 * time.Millisecond)
	// Stop drains the wg so async goroutine completes.
	if err := hook.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := atomic.LoadInt32(&triggered); got < 1 {
		t.Fatalf("triggered = %d, want >= 1", got)
	}
}

func TestFileWatcherHook_AsyncRewake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	mb := bus.New(nil, slog.Default())
	sub := mb.Subscribe("test-fw-rewake", HookAsyncRewakeTopic)

	var triggered int32
	hook := NewFileWatcherHook("*.txt", 50*time.Millisecond, nil, slog.Default())
	hook.Async = true
	hook.AsyncRewake = true
	hook.SetBus(mb)
	hook.SetSessionID("fw-test-session")
	hook.Callback = func(path string) {
		atomic.AddInt32(&triggered, 1)
	}

	ctx := context.Background()
	if err := hook.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer hook.Stop()

	time.Sleep(100 * time.Millisecond)
	f, err := os.Create("rewake_test.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Wait for rewake bus signal.
	select {
	case msg := <-sub.Channel:
		if msg.Topic != HookAsyncRewakeTopic {
			t.Errorf("rewake topic = %q, want %q", msg.Topic, HookAsyncRewakeTopic)
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["session_id"] != "fw-test-session" {
			t.Errorf("session_id = %v, want fw-test-session", payload["session_id"])
		}
		if payload["hook_type"] != "file_watcher" {
			t.Errorf("hook_type = %v, want file_watcher", payload["hook_type"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rewake signal")
	}

	if got := atomic.LoadInt32(&triggered); got < 1 {
		t.Fatalf("callback triggered = %d, want >= 1", got)
	}
}

func TestFileWatcherHook_SetBus_NilSafe(t *testing.T) {
	hook := &FileWatcherHook{}
	// Must not panic.
	hook.SetBus((*bus.MessageBus)(nil))
	hook.SetSessionID("")
}
