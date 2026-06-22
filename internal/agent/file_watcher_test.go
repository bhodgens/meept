package agent

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
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
