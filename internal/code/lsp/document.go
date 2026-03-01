package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DocumentManager tracks open documents and synchronizes with LSP servers.
type DocumentManager struct {
	client   *Client
	mu       sync.RWMutex
	docs     map[string]*Document
	versions map[string]int
}

// Document represents an open document.
type Document struct {
	URI        string
	LanguageID string
	Version    int
	Content    string
}

// NewDocumentManager creates a new document manager.
func NewDocumentManager(client *Client) *DocumentManager {
	return &DocumentManager{
		client:   client,
		docs:     make(map[string]*Document),
		versions: make(map[string]int),
	}
}

// OpenFile opens a file and notifies the LSP server.
func (m *DocumentManager) OpenFile(ctx context.Context, filePath string) (*Document, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	uri := PathToURI(absPath)

	// Check if already open
	m.mu.RLock()
	if doc, ok := m.docs[uri]; ok {
		m.mu.RUnlock()
		return doc, nil
	}
	m.mu.RUnlock()

	// Read file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Detect language
	languageID := DetectLanguageID(absPath)

	// Increment version
	m.mu.Lock()
	m.versions[uri]++
	version := m.versions[uri]

	doc := &Document{
		URI:        uri,
		LanguageID: languageID,
		Version:    version,
		Content:    string(content),
	}
	m.docs[uri] = doc
	m.mu.Unlock()

	// Notify server
	params := DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    version,
			Text:       string(content),
		},
	}

	if err := m.client.Notify(ctx, "textDocument/didOpen", params); err != nil {
		m.mu.Lock()
		delete(m.docs, uri)
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to notify didOpen: %w", err)
	}

	return doc, nil
}

// CloseFile closes a document and notifies the LSP server.
func (m *DocumentManager) CloseFile(ctx context.Context, filePath string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	uri := PathToURI(absPath)

	m.mu.Lock()
	_, ok := m.docs[uri]
	if ok {
		delete(m.docs, uri)
	}
	m.mu.Unlock()

	if !ok {
		return nil // Not open, nothing to do
	}

	params := DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	return m.client.Notify(ctx, "textDocument/didClose", params)
}

// UpdateFile updates a document's content and notifies the LSP server.
func (m *DocumentManager) UpdateFile(ctx context.Context, filePath string, newContent string) error {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	uri := PathToURI(absPath)

	m.mu.Lock()
	doc, ok := m.docs[uri]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("document not open: %s", filePath)
	}

	m.versions[uri]++
	doc.Version = m.versions[uri]
	doc.Content = newContent
	m.mu.Unlock()

	params := DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: doc.Version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: newContent}, // Full sync
		},
	}

	return m.client.Notify(ctx, "textDocument/didChange", params)
}

// GetDocument returns a document by path.
func (m *DocumentManager) GetDocument(filePath string) (*Document, bool) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, false
	}
	uri := PathToURI(absPath)

	m.mu.RLock()
	doc, ok := m.docs[uri]
	m.mu.RUnlock()

	return doc, ok
}

// IsOpen checks if a document is open.
func (m *DocumentManager) IsOpen(filePath string) bool {
	_, ok := m.GetDocument(filePath)
	return ok
}

// CloseAll closes all open documents.
func (m *DocumentManager) CloseAll(ctx context.Context) error {
	m.mu.RLock()
	uris := make([]string, 0, len(m.docs))
	for uri := range m.docs {
		uris = append(uris, uri)
	}
	m.mu.RUnlock()

	var lastErr error
	for _, uri := range uris {
		params := DidCloseTextDocumentParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
		}
		if err := m.client.Notify(ctx, "textDocument/didClose", params); err != nil {
			lastErr = err
		}
	}

	m.mu.Lock()
	m.docs = make(map[string]*Document)
	m.mu.Unlock()

	return lastErr
}

// PathToURI converts a file path to a file:// URI.
func PathToURI(path string) string {
	// Handle Windows paths
	if len(path) > 2 && path[1] == ':' {
		// Windows: C:\path -> file:///C:/path
		return "file:///" + strings.ReplaceAll(path, "\\", "/")
	}
	// Unix: /path -> file:///path
	return "file://" + path
}

// URIToPath converts a file:// URI to a file path.
func URIToPath(uri string) string {
	if strings.HasPrefix(uri, "file:///") {
		path := uri[7:] // Remove "file://"
		// Handle Windows paths (file:///C:/...)
		if len(path) > 2 && path[2] == ':' {
			return path[1:] // Remove leading /
		}
		return path
	}
	return uri
}

// DetectLanguageID returns the LSP language ID for a file.
func DetectLanguageID(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx":
		return "cpp"
	case ".java":
		return "java"
	case ".rb":
		return "ruby"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".sh", ".bash":
		return "shellscript"
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	default:
		return "plaintext"
	}
}
