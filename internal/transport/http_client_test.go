package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHTTPClient(t *testing.T) {
	client := NewHTTPClient("http://localhost:9999", 5*time.Second)
	if client == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
	client.Close()
}

func TestNewHTTPClient_ZeroTimeout(t *testing.T) {
	// Zero timeout should use the default of 120s
	client := NewHTTPClient("http://localhost:9999", 0)
	if client == nil {
		t.Fatal("NewHTTPClient with zero timeout returned nil")
	}
	client.Close()
}

func TestHTTPClient_Connect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	err := client.Connect()
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
}

func TestHTTPClient_Connect_NotRunning(t *testing.T) {
	client := NewHTTPClient("http://localhost:1", 1*time.Second)
	defer client.Close()

	err := client.Connect()
	if err == nil {
		t.Error("Connect() to non-running server should fail")
	}
}

func TestHTTPClient_Connect_WrongStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	err := client.Connect()
	if err == nil {
		t.Error("Connect() should fail when server returns non-200")
	}
}

func TestHTTPClient_IsConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	if !client.IsConnected() {
		t.Error("IsConnected() should return true when health endpoint returns 200")
	}
}

func TestHTTPClient_IsConnected_False(t *testing.T) {
	client := NewHTTPClient("http://localhost:1", 1*time.Second)
	defer client.Close()

	if client.IsConnected() {
		t.Error("IsConnected() should return false when server is not reachable")
	}
}

func TestHTTPClient_Close(t *testing.T) {
	client := NewHTTPClient("http://localhost:9999", 5*time.Second)
	err := client.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestHTTPClient_SetTimeout(t *testing.T) {
	client := NewHTTPClient("http://localhost:9999", 5*time.Second)
	defer client.Close()

	// SetTimeout should not panic
	client.SetTimeout(10 * time.Second)
}

func TestHTTPClient_Chat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/chat":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"reply": "hello from daemon"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	reply, err := client.Chat("hi", "conv-123")
	if err != nil {
		t.Fatalf("Chat() failed: %v", err)
	}
	if reply != "hello from daemon" {
		t.Errorf("Chat() reply = %q, want %q", reply, "hello from daemon")
	}
}

func TestHTTPClient_Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"result": {
				"status": "running",
				"uptime_seconds": 3600,
				"tokens_used": 100,
				"tokens_remaining": 900,
				"budget_used": 0.05,
				"budget_remaining": 0.95,
				"registered_methods": ["chat", "status"],
				"bus_subscribers": 3
			}
		}`))
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("Status().Status = %q, want %q", status.Status, "running")
	}
	if status.UptimeSeconds != 3600 {
		t.Errorf("Status().UptimeSeconds = %v, want %v", status.UptimeSeconds, 3600)
	}
}

func TestHTTPClient_Call(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/bus/call" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"result": {"key": "value"}}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, 5*time.Second)
	defer client.Close()

	result, err := client.Call("test.method", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("Call() failed: %v", err)
	}
	// json.RawMessage preserves the exact bytes from the response
	expected := `{"key": "value"}`
	if string(result) != expected {
		t.Errorf("Call() result = %s, want %s", string(result), expected)
	}
}
