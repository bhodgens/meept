package rpc

import (
	"testing"
	"time"
)

// TestProxyHandler_ChatTimeoutFloor pins the chat proxy default: a fresh
// handler registers the chat proxy with the legacy 120s bound (the floor),
// before any daemon wiring.
func TestProxyHandler_ChatTimeoutFloor(t *testing.T) {
	p := NewProxyHandler(nil) // bus unused for registration-shape assertions
	if p.chatTimeout != 120*time.Second {
		t.Fatalf("chatTimeout = %v, want the 120s legacy floor", p.chatTimeout)
	}
}

// TestProxyHandler_SetChatProxyTimeout pins the setter contract: the daemon
// raises the bound to max(120s, sync_wait_max + 15s); non-positive values
// are ignored so the floor survives a zero-value config field.
func TestProxyHandler_SetChatProxyTimeout(t *testing.T) {
	p := NewProxyHandler(nil)

	p.SetChatProxyTimeout(0)
	if p.chatTimeout != 120*time.Second {
		t.Fatalf("chatTimeout = %v after SetChatProxyTimeout(0), want the floor kept", p.chatTimeout)
	}

	p.SetChatProxyTimeout(-5 * time.Second)
	if p.chatTimeout != 120*time.Second {
		t.Fatalf("chatTimeout = %v after negative set, want the floor kept", p.chatTimeout)
	}

	p.SetChatProxyTimeout(10 * time.Minute)
	if p.chatTimeout != 10*time.Minute {
		t.Fatalf("chatTimeout = %v, want the raised bound", p.chatTimeout)
	}

	// Nil-guarded setter convention: must not panic.
	var nilProxy *ProxyHandler
	nilProxy.SetChatProxyTimeout(time.Minute)
}
