package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// chatResponseForSlotTests is the minimal ChatResponse body the slot
// priority tests return from the httptest server.
func chatResponseForSlotTests(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	resp := ChatResponse{
		ID:    "chatcmpl-slot",
		Model: "test-model",
		Choices: []Choice{
			{
				Message:      ResponseMessage{Role: "assistant", Content: json.RawMessage(`"ok"`)},
				FinishReason: "stop",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// TestClientSlotPriorityInteractiveJumpsQueue drives the public Chat API
// with MaxConcurrency=1: a background request holds the slot inside the
// handler; an interactive and a second background request queue at the
// gate. When the slot frees, the INTERACTIVE waiter must be granted
// first — observable as the interactive Chat completing before the
// background one (leaf Task 3). Priority never reaches the wire: the
// handler needs no request identity.
func TestClientSlotPriorityInteractiveJumpsQueue(t *testing.T) {
	// The first handler entry (bg1, the only request that can be in
	// flight) holds until the test frees it.
	var once sync.Once
	releaseBg := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { <-releaseBg })
		chatResponseForSlotTests(t, w)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:        server.URL,
		ModelID:        "test-model",
		MaxConcurrency: 1,
	})

	// 1. Background request takes the slot and holds it inside the
	//    handler.
	bg1Done := make(chan error, 1)
	bg1Entered := make(chan struct{})
	go func() {
		_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
			WithPriority(false))
		bg1Done <- err
	}()
	// Wait until bg1 is parked inside the handler (slot definitively
	// held). The handler blocks on releaseBg, so Chat cannot return.
	go func() {
		// Poll the client-side waiter state: bg1 has acquired the slot
		// once its HTTP call is in flight; detect via a short settle
		// delay after Chat started (deterministic: only one request can
		// exist pre-acquire and bg1 started first).
		for client.concurrencyGate == nil || client.concurrencyGate.heldCount() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		close(bg1Entered)
	}()
	select {
	case <-bg1Entered:
	case <-time.After(5 * time.Second):
		t.Fatal("bg1 never acquired the slot")
	}

	// 2. Background waiter enqueued FIRST...
	bg2Done := make(chan error, 1)
	go func() {
		_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
			WithPriority(false))
		bg2Done <- err
	}()
	// ...ensure it is parked on the gate...
	for client.concurrencyGate.waiterCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	// ...then enqueue the interactive waiter.
	intDone := make(chan error, 1)
	go func() {
		_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
			WithPriority(true))
		intDone <- err
	}()

	// 3. Free the slot. The gate must grant the INTERACTIVE waiter:
	//     its Chat completes (handler responds immediately for it)
	//     while bg2 stays parked at the gate.
	close(releaseBg)

	select {
	case err := <-intDone:
		if err != nil {
			t.Fatalf("interactive Chat: %v", err)
		}
		select {
		case err := <-bg2Done:
			t.Fatalf("background bg2 completed before interactive; err=%v", err)
		default:
		}
	case err := <-bg2Done:
		t.Fatalf("background bg2 was granted before the interactive waiter; err=%v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("no waiter granted after slot release")
	}

	// 4. Everyone drains successfully.
	for name, ch := range map[string]chan error{"bg1": bg1Done, "bg2": bg2Done} {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("%s Chat: %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s never completed", name)
		}
	}
}

// TestClientSlotPriorityDefaultBackground pins the no-regression rule:
// priority-less callers (WithPriority never called) behave exactly as
// today — background lane, cap enforced, all requests succeed.
func TestClientSlotPriorityDefaultBackground(t *testing.T) {
	var mu sync.Mutex
	var maxConcurrent, current int

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		current++
		if current > maxConcurrent {
			maxConcurrent = current
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			current--
			mu.Unlock()
		}()
		time.Sleep(30 * time.Millisecond)
		chatResponseForSlotTests(t, w)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:        server.URL,
		ModelID:        "test-model",
		MaxConcurrency: 2,
	})

	const n = 6
	errs := make(chan error, n)
	for range n {
		go func() {
			_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
			errs <- err
		}()
	}
	for range n {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Chat timed out")
		}
	}
	mu.Lock()
	mc := maxConcurrent
	mu.Unlock()
	if mc > 2 {
		t.Fatalf("maxConcurrent = %d, want <= 2", mc)
	}
	if mc < 2 {
		t.Fatalf("maxConcurrent = %d, want >= 2 (limit utilized)", mc)
	}
}

// TestClientSlotPriorityCtxCancelWhileQueued proves ctx-cancel at the
// client boundary dequeues the waiter cleanly: a queued Chat whose
// context is cancelled returns an error and never blocks later grants.
func TestClientSlotPriorityCtxCancelWhileQueued(t *testing.T) {
	var once sync.Once
	releaseBg := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { <-releaseBg })
		chatResponseForSlotTests(t, w)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := NewClient(&ModelConfig{
		BaseURL:        server.URL,
		ModelID:        "test-model",
		MaxConcurrency: 1,
	})

	bg1Done := make(chan error, 1)
	go func() {
		_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
		bg1Done <- err
	}()
	for client.concurrencyGate == nil || client.concurrencyGate.heldCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	queuedDone := make(chan error, 1)
	go func() {
		_, err := client.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi"}})
		queuedDone <- err
	}()
	for client.concurrencyGate.waiterCount() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-queuedDone:
		if err == nil {
			t.Fatal("cancelled queued Chat returned nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled queued Chat never returned")
	}
	if n := client.concurrencyGate.waiterCount(); n != 0 {
		t.Fatalf("waiters after cancel = %d, want 0", n)
	}

	close(releaseBg)
	select {
	case err := <-bg1Done:
		if err != nil {
			t.Fatalf("bg1 Chat: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("bg1 never completed")
	}
}
