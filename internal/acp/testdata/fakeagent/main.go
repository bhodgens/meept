// Fake ACP agent for internal/acp tests. Speaks newline JSON-RPC on stdio.
//
//	-mode echo|slow|die|badhandshake|permission|permission-always
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
	mode := flag.String("mode", "echo", "echo|slow|die|badhandshake|permission|permission-always")
	flag.Parse()

	dec := json.NewDecoder(os.Stdin)
	sessionID := "sess-fake"
	replyEcho := ""
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
			if *mode == "earlyperm" {
				// Item-5 race pin: emit session/requestPermission BEFORE
				// replying to session/new, so the permission goroutine
				// reads s.sessionID concurrently with Start's write.
				permID := int64(99002)
				write(msg{
					JSONRPC: "2.0",
					ID:      &permID,
					Method:  "session/requestPermission",
					Params:  json.RawMessage(`{"sessionId":"sess-fake"}`),
				})
			}
			reply(m.ID, map[string]any{"sessionId": sessionID})
			if *mode == "earlyperm" {
				// Consume the permission reply frame whenever it lands.
				var replyMsg msg
				if err := dec.Decode(&replyMsg); err != nil {
					return
				}
			}
		case "session/prompt":
			if *mode == "slow" {
				time.Sleep(5 * time.Second)
			}
			if *mode == "permission" || *mode == "permission-always" {
				permID := int64(99001)
				params := json.RawMessage(`{"sessionId":"sess-fake"}`)
				if *mode == "permission-always" {
					// acp-always-allow e2e: offer options WITH an
					// allow_always entry, exactly like a capability-
					// declaring ACP agent.
					params = json.RawMessage(`{"sessionId":"sess-fake",` +
						`"toolCall":{"title":"rm -rf scratch"},` +
						`"options":[` +
						`{"optionId":"reject-once","name":"Reject","kind":"reject_once"},` +
						`{"optionId":"allow-once","name":"Allow once","kind":"allow_once"},` +
						`{"optionId":"allow-always","name":"Allow always","kind":"allow_always"}]}`)
				}
				write(msg{
					JSONRPC: "2.0",
					ID:      &permID,
					Method:  "session/requestPermission",
					Params:  params,
				})
				var replyMsg msg
				if err := dec.Decode(&replyMsg); err != nil {
					return
				}
				if *mode == "permission-always" {
					// Echo the received reply outcome into the prompt
					// reply so the harness can assert what meept sent
					// the AGENT (selected/<optionId> echo proof).
					replyEcho = replyOutcome(replyMsg.Result)
				}
			}
			text := promptText(m.Params)
			if replyEcho != "" {
				text += " perm:" + replyEcho
			}
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

// replyOutcome renders the outcome of a requestPermission reply frame as
// "<outcome>/<optionId>" (or "<outcome>" with no optionId, or "error" for
// an error frame) — the acp-always-allow permission-always mode echoes it
// back so the harness can assert exactly what the client selected.
func replyOutcome(result json.RawMessage) string {
	var r struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if len(result) == 0 || json.Unmarshal(result, &r) != nil || r.Outcome.Outcome == "" {
		return "error"
	}
	if r.Outcome.OptionID == "" {
		return r.Outcome.Outcome
	}
	return r.Outcome.Outcome + "/" + r.Outcome.OptionID
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
