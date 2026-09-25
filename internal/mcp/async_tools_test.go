package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRPCSeq serializes the /tmp socket filenames across tests.
var fakeRPCSeq int32

// fakeRPCServer answers JSON-RPC lines on a Unix socket, dispatching by
// method. It lets the tool implementations be exercised against a scripted
// daemon without the real meept-daemon.
type fakeRPC struct {
	listener net.Listener
	// handler is invoked with the decoded JSON-RPC request and returns the
	// "result" value (marshaled). Returning an error answers with a JSON-RPC
	// error object — matching transport.Client.Call's error contract.
	handler func(method string, params map[string]any) (any, error)
}

func startFakeRPC(t *testing.T, handler func(string, map[string]any) (any, error)) string {
	t.Helper()
	// macOS limits unix socket paths to 104 bytes; t.TempDir() under the
	// per-test cache dir can exceed that, so bind a short /tmp path instead.
	sock := fmt.Sprintf("/tmp/meept-mcp-test-%d-%d.sock", os.Getpid(), atomic.AddInt32(&fakeRPCSeq, 1))
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeRPC{listener: l, handler: handler}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = l.Close() })
	return sock
}

func (f *fakeRPC) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	readFrame := func() ([]byte, error) {
		lengthLine, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
		if err != nil {
			return nil, err
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		return payload, nil
	}
	writeFrame := func(payload []byte) error {
		_, err := fmt.Fprintf(conn, "%d\n", len(payload))
		if err != nil {
			return err
		}
		_, err = conn.Write(payload)
		return err
	}
	for {
		raw, err := readFrame()
		if err != nil {
			return
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return
		}
		var params map[string]any
		_ = json.Unmarshal(req.Params, &params)
		result, herr := f.handler(req.Method, params)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if herr != nil {
			resp["error"] = map[string]any{"code": -32000, "message": herr.Error()}
		} else {
			resp["result"] = result
		}
		line, _ := json.Marshal(resp)
		if err := writeFrame(line); err != nil {
			return
		}
	}
}

// callTool drives a tools/call through a real Server against the fake RPC
// and returns the parsed JSON-RPC response.
func callTool(t *testing.T, sock, name string, args map[string]any) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 9, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	input := bytes.NewBuffer(append(payload, '\n'))
	output := &bytes.Buffer{}
	srv := NewServer(input, output, nil)
	if err := srv.ConnectRPC(sock); err != nil {
		t.Fatalf("ConnectRPC: %v", err)
	}
	defer srv.CloseRPC()
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	var resp struct {
		Result json.RawMessage  `json:"result"`
		Error  *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", output.String(), err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", string(*resp.Error))
	}
	var inner struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &inner); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if inner.IsError {
		t.Fatalf("tool returned isError: %s", inner.Content[0].Text)
	}
	if len(inner.Content) == 0 {
		t.Fatal("no content in tool result")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(inner.Content[0].Text), &out); err != nil {
		t.Fatalf("tool content is not a JSON object: %v (%q)", err, inner.Content[0].Text)
	}
	return out
}

func TestToolChatSubmitAck(t *testing.T) {
	var gotMethod string
	var gotParams map[string]any
	sock := startFakeRPC(t, func(method string, params map[string]any) (any, error) {
		gotMethod = method
		gotParams = params
		return map[string]any{
			"turn_id": "turn-abc", "conversation_id": "conv-abc",
			"session_id": "sess-1", "accepted": true,
			"note": "accepted; result arrives via turn.terminal",
		}, nil
	})

	out := callTool(t, sock, "meept_chat_submit", map[string]any{
		"session_id": "sess-1", "message": "hello", "source_client": "meept-bench-e2e",
	})

	if gotMethod != "chat.submit" {
		t.Errorf("RPC method = %q, want chat.submit", gotMethod)
	}
	if gotParams["message"] != "hello" || gotParams["session_id"] != "sess-1" {
		t.Errorf("submit params = %v", gotParams)
	}
	if gotParams["conversation_id"] != "sess-1" {
		t.Errorf("conversation_id should echo session_id, got %v", gotParams["conversation_id"])
	}
	if out["turn_id"] != "turn-abc" || out["accepted"] != true {
		t.Errorf("ack result = %v", out)
	}
}

func TestToolChatSubmitRejected(t *testing.T) {
	sock := startFakeRPC(t, func(string, map[string]any) (any, error) {
		return map[string]any{"turn_id": "", "accepted": false, "note": "message is required"}, nil
	})
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 9, "method": "tools/call",
		"params": map[string]any{"name": "meept_chat_submit",
			"arguments": map[string]any{"session_id": "s", "message": "x"}},
	})
	input := bytes.NewBuffer(append(payload, '\n'))
	output := &bytes.Buffer{}
	srv := NewServer(input, output, nil)
	if err := srv.ConnectRPC(sock); err != nil {
		t.Fatalf("ConnectRPC: %v", err)
	}
	defer srv.CloseRPC()
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var inner struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &inner); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !inner.IsError {
		t.Fatalf("expected isError for a rejected submit, got %s", inner.Content[0].Text)
	}
}

func TestToolSubscribe(t *testing.T) {
	var gotTopics any
	sock := startFakeRPC(t, func(method string, params map[string]any) (any, error) {
		if method != "bus.subscribe" {
			t.Errorf("method = %s, want bus.subscribe", method)
		}
		gotTopics = params["topics"]
		return map[string]any{"subscription_id": "sub-42", "topics": params["topics"]}, nil
	})

	out := callTool(t, sock, "meept_subscribe", map[string]any{})

	if out["subscription_id"] != "sub-42" {
		t.Errorf("subscription_id = %v, want sub-42", out["subscription_id"])
	}
	// The subscription MUST include turn.terminal — without it the async
	// turn driver never sees the terminal event.
	topics, _ := gotTopics.([]any)
	found := false
	for _, tp := range topics {
		if s, ok := tp.(string); ok && s == "turn.terminal" {
			found = true
		}
	}
	if !found {
		t.Errorf("bus.subscribe topics %v missing turn.terminal", gotTopics)
	}
}

func TestToolUnsubscribe(t *testing.T) {
	var gotSub string
	sock := startFakeRPC(t, func(method string, params map[string]any) (any, error) {
		if method != "bus.unsubscribe" {
			t.Errorf("method = %s, want bus.unsubscribe", method)
		}
		gotSub, _ = params["subscription_id"].(string)
		return map[string]any{"ok": true}, nil
	})

	out := callTool(t, sock, "meept_unsubscribe", map[string]any{"subscription_id": "sub-7"})
	if gotSub != "sub-7" {
		t.Errorf("forwarded subscription_id = %q, want sub-7", gotSub)
	}
	if out["unsubscribed"] != "sub-7" {
		t.Errorf("result = %v", out)
	}
}

func TestToolWaitTurnTerminal(t *testing.T) {
	// First bus.poll returns an unrelated event, then the terminal event.
	polls := 0
	sock := startFakeRPC(t, func(method string, params map[string]any) (any, error) {
		if method != "bus.poll" {
			t.Errorf("method = %s, want bus.poll", method)
		}
		polls++
		if polls == 1 {
			return map[string]any{"events": []map[string]any{
				{"topic": "agent.progress", "timestamp": "2026-09-25T12:00:00Z",
					"payload": map[string]any{"note": "noise"}},
			}}, nil
		}
		return map[string]any{"events": []map[string]any{
			{"topic": "turn.terminal", "timestamp": "2026-09-25T12:00:01Z",
				"payload": map[string]any{
					"turn_id": "turn-abc", "session_id": "sess-1",
					"conversation_id": "conv-abc", "status": "completed",
					"reply": "I created hello.txt at /tmp/x/hello.txt",
				}},
		}}, nil
	})

	out := callTool(t, sock, "meept_wait_turn", map[string]any{
		"turn_id": "turn-abc", "subscription_id": "sub-1", "timeout_ms": 5000,
	})

	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed", out["status"])
	}
	reply, _ := out["response"].(string)
	if reply == "" {
		t.Fatalf("no reply in result: %v", out)
	}
	if polls != 2 {
		t.Errorf("polls = %d, want 2", polls)
	}
}

func TestToolWaitTurnIgnoresOtherTurns(t *testing.T) {
	// A turn.terminal event for ANOTHER turn must not satisfy the wait.
	polls := 0
	sock := startFakeRPC(t, func(string, map[string]any) (any, error) {
		polls++
		return map[string]any{"events": []map[string]any{
			{"topic": "turn.terminal", "timestamp": "2026-09-25T12:00:01Z",
				"payload": map[string]any{
					"turn_id": "turn-OTHER", "status": "completed", "reply": "not mine",
				}},
		}}, nil
	})

	start := time.Now()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 9, "method": "tools/call",
		"params": map[string]any{"name": "meept_wait_turn",
			"arguments": map[string]any{"turn_id": "turn-mine",
				"subscription_id": "sub-1", "timeout_ms": 700}},
	})
	input := bytes.NewBuffer(append(payload, '\n'))
	output := &bytes.Buffer{}
	srv := NewServer(input, output, nil)
	if err := srv.ConnectRPC(sock); err != nil {
		t.Fatalf("ConnectRPC: %v", err)
	}
	defer srv.CloseRPC()
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var inner struct {
		Content []struct{ Text string } `json:"content"`
		IsError bool                    `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &inner); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !inner.IsError {
		t.Fatalf("expected isError timeout, got %s", inner.Content[0].Text)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("wait ran %v, timeout_ms was not honored", elapsed)
	}
}

func TestToolWaitTurnFailedCarriesError(t *testing.T) {
	sock := startFakeRPC(t, func(string, map[string]any) (any, error) {
		return map[string]any{"events": []map[string]any{
			{"topic": "turn.terminal", "timestamp": "2026-09-25T12:00:01Z",
				"payload": map[string]any{
					"turn_id": "turn-abc", "status": "failed",
					"error": "provider quota exhausted",
				}},
		}}, nil
	})

	out := callTool(t, sock, "meept_wait_turn", map[string]any{
		"turn_id": "turn-abc", "subscription_id": "sub-1", "timeout_ms": 5000,
	})
	if out["status"] != "failed" {
		t.Errorf("status = %v, want failed", out["status"])
	}
	resp, _ := out["response"].(string)
	if resp == "" {
		t.Fatalf("expected user-facing failure text, got %v", out)
	}
}
