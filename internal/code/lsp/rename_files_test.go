package lsp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestWillRenameFiles tests the WillRenameFiles client method
func TestWillRenameFiles(t *testing.T) {
	t.Run("basic call structure", func(t *testing.T) {
		// Create a mock transport that returns a valid response
		mockResponse := WorkspaceEditWithOperations{
			Changes: map[string][]TextEdit{
				"file:///test/old.go": {
					{
						Range: Range{
							Start: Position{Line: 0, Character: 0},
							End:   Position{Line: 0, Character: 10},
						},
						NewText: "new content",
					},
				},
			},
		}

		responseBytes, _ := json.Marshal(mockResponse)

		// Create mock transport
		mockTransport := &mockTransport{
			readData: []byte(`{"jsonrpc":"2.0","id":1,"result":` + string(responseBytes) + `}`),
		}

		client := NewClient(mockTransport)
		client.Start(context.Background())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Initialize with mock server
		if err := client.Initialize(ctx, "file:///test"); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		// Call WillRenameFiles
		result, err := client.WillRenameFiles(ctx, "file:///test/old.go", "file:///test/new.go")
		if err != nil {
			t.Fatalf("WillRenameFiles failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if len(result.Changes) != 1 {
			t.Errorf("Expected 1 change, got %d", len(result.Changes))
		}
	})

	t.Run("null response handling", func(t *testing.T) {
		mockTransport := &mockTransport{
			readData: []byte(`{"jsonrpc":"2.0","id":1,"result":null}`),
		}

		client := NewClient(mockTransport)
		client.Start(context.Background())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Initialize(ctx, "file:///test"); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		result, err := client.WillRenameFiles(ctx, "file:///test/old.go", "file:///test/new.go")
		if err != nil {
			t.Fatalf("WillRenameFiles failed: %v", err)
		}

		if result != nil {
			t.Error("Expected nil result for null response")
		}
	})
}

// TestHasWillRenameFiles tests the capability detection
func TestHasWillRenameFiles(t *testing.T) {
	tests := []struct {
		name     string
		caps     ServerCapabilities
		expected bool
	}{
		{
			name: "with will rename capability",
			caps: ServerCapabilities{
				WorkspaceFileOperations: &WorkspaceFileOperationCapabilities{
					WillRename: &FileOperationOptions{Recursive: true},
				},
			},
			expected: true,
		},
		{
			name:     "without workspace file operations",
			caps:     ServerCapabilities{},
			expected: false,
		},
		{
			name: "with nil will rename",
			caps: ServerCapabilities{
				WorkspaceFileOperations: &WorkspaceFileOperationCapabilities{
					WillRename: nil,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := NewCapabilities(tt.caps)
			result := caps.HasWillRenameFiles()
			if result != tt.expected {
				t.Errorf("HasWillRenameFiles() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestManagerWillRenameFiles tests the manager's WillRenameFiles wrapper
func TestManagerWillRenameFiles(t *testing.T) {
	t.Run("no servers available", func(t *testing.T) {
		manager := NewManager(LSPConfig{})

		ctx := context.Background()
		result, err := manager.WillRenameFiles(ctx, "file:///old.go", "file:///new.go")

		if err == nil {
			t.Error("Expected error when no servers available")
		}
		if result != nil {
			t.Error("Expected nil result when no servers available")
		}
	})
}

// mockTransport implements the Transport interface for testing
type mockTransport struct {
	readData []byte
}

func (m *mockTransport) Read(ctx context.Context) ([]byte, error) {
	return m.readData, nil
}

func (m *mockTransport) Write(ctx context.Context, data []byte) error {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}
