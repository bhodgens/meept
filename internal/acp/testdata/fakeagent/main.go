// Fake ACP agent for internal/acp tests. Speaks newline JSON-RPC on stdio.
//
//	-mode echo|slow|die|badhandshake|permission|permission-always|selfapprove|argv
//
// argv: session/prompt replies with the fakeagent's own os.Args tail
// ("argv=<a1>,<a2>,...") so tests can assert the spawn argv (persona-lane
// CommandArgs concatenation).
//
// selfapprove (audit M5 e2e): after emitting session/requestPermission the
// agent dials the DAEMON's own Unix RPC socket (-socket) and tries to answer
// its own pending request — the real-world self-approval attack. The daemon's
// approver-identity gate must reject it (the agent process is daemon-spawned,
// so its kernel-verified peer PID is a daemon descendant). The probe results
// are echoed into the prompt reply text:
//
//	selfapprove:list=<enabled>|resolve=<error message or OK>
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
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
	// persona-lane argv tests pass extra undefined flags (e.g. -p borg)
	// after the catalog command; ContinueOnError keeps the process alive
	// so the argv echo can read os.Args directly.
	fs := flag.NewFlagSet("fakeagent", flag.ContinueOnError)
	mode := fs.String("mode", "echo", "echo|slow|die|badhandshake|permission|permission-always|selfapprove|argv")
	socket := fs.String("socket", "", "daemon unix RPC socket (selfapprove mode)")
	resultFile := fs.String("result-file", "", "write the selfapprove probe result here (selfapprove mode)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent flag parse: %v\n", err)
	}

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
			if *mode == "selfapprove" {
				// Audit M5 attack simulation: register a pending human
				// request, then try to answer it OURSELVES over the
				// daemon's own RPC socket. The gate must reject both the
				// list (pending ids invisible) and the resolve.
				permID := int64(99003)
				write(msg{
					JSONRPC: "2.0",
					ID:      &permID,
					Method:  "session/requestPermission",
					Params:  json.RawMessage(`{"sessionId":"sess-fake","toolCall":{"title":"rm -rf scratch"}}`),
				})
				listResult, resolveResult := selfApproveProbe(*socket, permID)
				replyEcho = "selfapprove:list=" + listResult + "|resolve=" + resolveResult
				// Persist the probe result to a file the e2e test polls:
				// the echo also rides the prompt reply, but the file is
				// the deterministic channel (reply text depends on the
				// relay timeout arriving first).
				if *resultFile != "" {
					if wErr := os.WriteFile(*resultFile, []byte(replyEcho+"\n"), 0o600); wErr != nil {
						fmt.Fprintf(os.Stderr, "fakeagent: write result file: %v\n", wErr)
					}
				}
				// Drain the permission reply frame (meept sends one when
				// the wait ends — timeout or human answer).
				var replyMsg msg
				if err := dec.Decode(&replyMsg); err != nil {
					return
				}
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
			if *mode == "argv" {
				// echo the spawn argv tail for CommandArgs assertions
				text = "argv=" + strings.Join(os.Args[1:], ",")
			}
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

// selfApproveProbe dials the daemon's own Unix RPC socket (the agent is a
// daemon descendant, so its kernel-verified peer PID is what the approver
// gate walks) and attempts the self-approval attack:
//  1. acp.permissions.list — the gate must hide the pending id (disabled/
//     empty envelope), so listResult is "hidden" when enabled=false or no
//     pending rows are visible, else "LEAKED".
//  2. acp.permissions.resolve on the just-registered request — the gate must
//     reject with the self-approval error, so resolveResult is the error
//     message on rejection, or "OK" if the daemon WRONGLY let it through.
//
// permID is the agent's own requestPermission id; the resolve targets the
// pending relay id discovered from the list (empty list => the id is not
// discoverable, so the resolve probes with the perm id and still must fail).
func selfApproveProbe(socket string, permID int64) (listResult, resolveResult string) {
	if socket == "" {
		return "no-socket", "no-socket"
	}
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return "dial-failed:" + err.Error(), "dial-failed"
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return "deadline-failed:" + err.Error(), "deadline-failed"
	}
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	// 1. list
	pendingID, listErr := rpcProbe(w, r, 1, "acp.permissions.list", `{}`)
	if listErr != "" {
		return "list-error:" + listErr, "skipped"
	}
	if pendingID == "" {
		listResult = "hidden"
	} else {
		listResult = "LEAKED:" + pendingID
	}

	// 2. resolve — try the discovered pending id, else our own perm id.
	target := pendingID
	if target == "" {
		target = strconv.FormatInt(permID, 10)
	}
	params := `{"id":"` + target + `","decision":"allow"}`
	_, resolveErr := rpcProbe(w, r, 2, "acp.permissions.resolve", params)
	if resolveErr != "" {
		// Rejection is the SECURE outcome. Surface a stable marker plus
		// the message so the test can assert on "self-approval".
		resolveResult = "REJECTED:" + sanitize(resolveErr)
	} else {
		resolveResult = "OK-SELF-APPROVED"
	}
	return listResult, resolveResult
}

// rpcProbe sends one length-prefixed JSON-RPC frame and reads the reply.
// It returns (firstPendingID, errorMessage): for list, firstPendingID is
// the first row's id (or "" when disabled/empty); errorMessage is the
// JSON-RPC error message when the call was rejected.
func rpcProbe(w *bufio.Writer, r *bufio.Reader, id int, method, params string) (string, string) {
	req := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"` + method + `","params":` + params + `}`
	frame := strconv.Itoa(len(req)) + "\n" + req
	if _, err := w.WriteString(frame); err != nil {
		return "", "write:" + err.Error()
	}
	if err := w.Flush(); err != nil {
		return "", "flush:" + err.Error()
	}
	lenLine, err := r.ReadString('\n')
	if err != nil {
		return "", "readlen:" + err.Error()
	}
	n, err := strconv.Atoi(strings.TrimSpace(lenLine))
	if err != nil || n <= 0 || n > 10*1024*1024 {
		return "", "badlen:" + lenLine
	}
	buf := make([]byte, n)
	if _, err := readFull(r, buf); err != nil {
		return "", "readbody:" + err.Error()
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf, &resp); err != nil {
		return "", "decode:" + err.Error()
	}
	if resp.Error != nil {
		return "", resp.Error.Message
	}
	// Extract the first pending id, if any (list result shape).
	var list struct {
		Enabled bool `json:"enabled"`
		Pending []struct {
			ID string `json:"id"`
		} `json:"pending"`
	}
	if err := json.Unmarshal(resp.Result, &list); err == nil && list.Enabled && len(list.Pending) > 0 {
		return list.Pending[0].ID, ""
	}
	return "", ""
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// sanitize collapses a message to a single line and trims it so it survives
// the space-delimited echo in the prompt reply.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "/")
	if len(s) > 120 {
		s = s[:120]
	}
	return strings.TrimSpace(s)
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
