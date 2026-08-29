// Fake ACP agent for internal/acp tests. Speaks newline JSON-RPC on stdio.
//
//	-mode echo|slow|die|badhandshake|permission
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func write(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent encode: %v\n", err)
	}
}

func reply(id *int64, result any) {
	if id == nil {
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return
	}
	write(msg{JSONRPC: "2.0", ID: id, Result: raw})
}

func main() {
	mode := flag.String("mode", "echo", "echo|slow|die|badhandshake|permission")
	flag.Parse()

	dec := json.NewDecoder(os.Stdin)
	sessionID := "sess-fake"
	for {
		var m msg
		if err := dec.Decode(&m); err != nil {
			return
		}
		switch m.Method {
		case "initialize":
			if *mode == "slow" {
				time.Sleep(5 * time.Second)
			}
			if *mode == "badhandshake" {
				write(msg{
					JSONRPC: "2.0",
					ID:      m.ID,
					Error:   &rpcErr{Code: -32000, Message: "bad handshake"},
				})
				continue
			}
			if *mode == "die" {
				os.Exit(1)
			}
			reply(m.ID, map[string]any{
				"protocolVersion": 1,
				"agentInfo":       map[string]string{"name": "fakeagent"},
			})
		case "session/new":
			reply(m.ID, map[string]any{"sessionId": sessionID})
		case "session/prompt":
			if *mode == "slow" {
				time.Sleep(5 * time.Second)
			}
			if *mode == "permission" {
				permID := int64(99001)
				write(msg{
					JSONRPC: "2.0",
					ID:      &permID,
					Method:  "session/requestPermission",
					Params:  json.RawMessage(`{"sessionId":"sess-fake"}`),
				})
				var replyMsg msg
				if err := dec.Decode(&replyMsg); err != nil {
					return
				}
			}
			text := promptText(m.Params)
			chunk, err := json.Marshal(map[string]any{
				"sessionId": sessionID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]string{"type": "text", "text": text},
				},
			})
			if err == nil {
				write(msg{
					JSONRPC: "2.0",
					Method:  "session/update",
					Params:  chunk,
				})
			}
			reply(m.ID, map[string]any{"stopReason": "end_turn"})
		case "session/cancel":
			// notification; no reply
		default:
			if m.ID != nil && m.Method == "" {
				continue
			}
		}
	}
}

func promptText(params json.RawMessage) string {
	var p struct {
		Prompt []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"prompt"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	out := ""
	for _, b := range p.Prompt {
		out += b.Text
	}
	if out == "" {
		return "ok"
	}
	return out
}
