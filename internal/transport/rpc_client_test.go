package transport

import (
	"testing"
	"time"
)

func TestNewRPCClient(t *testing.T) {
	client := NewRPCClient("/tmp/test-meept.sock", 5*time.Second)
	if client == nil {
		t.Fatal("NewRPCClient returned nil")
	}
	defer client.Close()
}

func TestNewRPCClient_ZeroTimeout(t *testing.T) {
	// Zero timeout should not panic; the underlying RPCClient uses its own default
	client := NewRPCClient("/tmp/test-meept.sock", 0)
	if client == nil {
		t.Fatal("NewRPCClient with zero timeout returned nil")
	}
	defer client.Close()
}

func TestRPCAdapter_NotConnected(t *testing.T) {
	client := NewRPCClient("/tmp/nonexistent-meept.sock", 1*time.Second)
	defer client.Close()

	if client.IsConnected() {
		t.Error("newly created RPC adapter should not be connected")
	}
}

func TestRPCAdapter_CloseWithoutConnect(t *testing.T) {
	client := NewRPCClient("/tmp/test-meept.sock", 5*time.Second)
	err := client.Close()
	if err != nil {
		t.Errorf("Close() without Connect() returned error: %v", err)
	}
}

func TestRPCAdapter_SetTimeout(t *testing.T) {
	client := NewRPCClient("/tmp/test-meept.sock", 5*time.Second)
	defer client.Close()

	// SetTimeout should not panic
	client.SetTimeout(10 * time.Second)
}

func TestRPCAdapter_ConnectFails(t *testing.T) {
	client := NewRPCClient("/tmp/nonexistent-meept-xyz.sock", 1*time.Second)
	defer client.Close()

	err := client.Connect()
	if err == nil {
		t.Error("Connect() to nonexistent socket should fail")
	}
}
