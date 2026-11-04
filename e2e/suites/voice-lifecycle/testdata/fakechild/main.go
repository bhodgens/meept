// Command fakechild is the hermetic meept-voice stand-in for the voice
// e2e suites (leaf 15): it replaces the real jargo child so CI never loads
// ONNX or speaks to a vendor. It speaks contract C1 over the REAL daemon
// text socket and contract C2 over the REAL per-call audio pairs, and
// exposes a small control socket the test suite drives and observes:
//
//	suite -> child (control): {"ctrl":"setup"|"prompt"|"interrupt"|"dial-audio",...}
//	child -> suite (control): {"ctrl":"frame","frame":{...C1 frame...}}
//
// The child reads its start token from stdin exactly like the real binary
// (the daemon pipes it in), connects the text socket, mirrors every
// daemon->child frame to the control socket, and exits on SIGTERM/SIGINT
// or when the daemon closes the text connection.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// frame mirrors the C1 text-socket frame shape (internal/voiceframe.Frame).
// Field names match the wire keys exactly.
type frame struct {
	CallID     string `json:"call_id"`
	Event      string `json:"event"`
	Text       string `json:"text,omitempty"`
	Last       *bool  `json:"last,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Codec      string `json:"codec,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// ctrlMsg is one control-socket message (either direction).
type ctrlMsg struct {
	Ctrl       string `json:"ctrl"` // frame|setup|prompt|interrupt|dial-audio|log
	CallID     string `json:"call_id,omitempty"`
	Text       string `json:"text,omitempty"`
	Last       *bool  `json:"last,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Codec      string `json:"codec,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Note       string `json:"note,omitempty"`
	Frame      *frame `json:"frame,omitempty"`
}

type child struct {
	textSocket string
	audioDir   string

	mu      sync.Mutex
	conn    net.Conn // the text-socket connection
	ctrls   map[net.Conn]struct{}
	dropped bool // a write failed: the transport is dead
}

func (c *child) sendLine(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(append(b, '\n')) //nolint:errcheck // control mirror is best-effort
}

// toCtrl mirrors one daemon frame to every connected control client.
func (c *child) toCtrl(f frame) {
	c.mu.Lock()
	conns := make([]net.Conn, 0, len(c.ctrls))
	for conn := range c.ctrls {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		c.sendLine(conn, ctrlMsg{Ctrl: "frame", Frame: &f})
	}
}

// sendFrame writes one C1 frame line to the daemon text socket.
func (c *child) sendFrame(f frame) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("fake child: no text connection")
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(b, '\n'))
	return err
}

// dialAudio connects the per-call C2 pair for ccid and keeps both ends
// open (the splice polls .out until the child dials; a silent open socket
// is a healthy idle call).
func (c *child) dialAudio(ccid string) error {
	inPath := filepath.Join(c.audioDir, ccid+".in")
	outPath := filepath.Join(c.audioDir, ccid+".out")
	var dialer net.Dialer
	inConn, err := dialer.Dial("unix", inPath)
	if err != nil {
		return fmt.Errorf("dial %s: %w", inPath, err)
	}
	outConn, err := dialer.Dial("unix", outPath)
	if err != nil {
		_ = inConn.Close() //nolint:errcheck // error path
		return fmt.Errorf("dial %s: %w", outPath, err)
	}
	// Drain .in (the splice writes inbound audio; the fake STT ignores it).
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := inConn.Read(buf); err != nil {
				return
			}
		}
	}()
	// Hold .out open, writing nothing: ReadFromChild blocks until teardown
	// closes the socket.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := outConn.Read(buf); err != nil {
				return
			}
		}
	}()
	return nil
}

// handleCtrl acts on one suite command.
func (c *child) handleCtrl(raw []byte) {
	var m ctrlMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	switch m.Ctrl {
	case "setup":
		f := frame{
			CallID:     m.CallID,
			Event:      "setup",
			From:       m.From,
			To:         m.To,
			Direction:  m.Direction,
			Codec:      m.Codec,
			SampleRate: m.SampleRate,
		}
		if f.Codec == "" {
			f.Codec = "L16"
			f.SampleRate = 16000
		}
		if err := c.sendFrame(f); err != nil {
			c.toCtrl(frame{CallID: m.CallID, Event: "log", Text: "setup send failed: " + err.Error()})
		}
	case "prompt":
		last := m.Last != nil && *m.Last
		f := frame{CallID: m.CallID, Event: "prompt", Text: m.Text}
		if last {
			t := true
			f.Last = &t
		}
		if err := c.sendFrame(f); err != nil {
			c.toCtrl(frame{CallID: m.CallID, Event: "log", Text: "prompt send failed: " + err.Error()})
		}
	case "interrupt":
		if err := c.sendFrame(frame{CallID: m.CallID, Event: "interrupt"}); err != nil {
			c.toCtrl(frame{CallID: m.CallID, Event: "log", Text: "interrupt send failed: " + err.Error()})
		}
	case "dial-audio":
		if err := c.dialAudio(m.CallID); err != nil {
			c.toCtrl(frame{CallID: m.CallID, Event: "log", Text: "dial-audio failed: " + err.Error()})
		}
	}
}

func main() {
	textSocket := flag.String("text-socket", "", "daemon text socket path (C1)")
	audioDir := flag.String("audio-dir", "", "per-call audio pair directory (C2)")
	control := flag.String("control", "", "control socket path the suite drives")
	flag.Parse()
	if *textSocket == "" || *audioDir == "" {
		fmt.Fprintln(os.Stderr, "fakechild: -text-socket and -audio-dir are required")
		os.Exit(2)
	}
	// The daemon's spawn argv passes only --text-socket/--audio-dir (the
	// daemon owns the argv, contract C1/C2). The control path derives from
	// the audio dir deterministically: the sibling of the sock dir, so a
	// suite precomputes it from $MEEPT_HOME without touching the daemon's
	// spawn contract.
	if *control == "" {
		*control = filepath.Join(filepath.Dir(*audioDir), "ctrl.sock")
	}

	// Start token via stdin ONLY (never argv/env), like the real child.
	token := ""
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	token = strings.TrimSpace(line)
	if token == "" {
		fmt.Fprintln(os.Stderr, "fakechild: empty start token on stdin")
		os.Exit(3)
	}
	_ = token // consumed per contract; the fake never forwards key material

	c := &child{textSocket: *textSocket, audioDir: *audioDir, ctrls: map[net.Conn]struct{}{}}

	conn, err := net.Dial("unix", *textSocket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakechild: dial text socket: %v\n", err)
		os.Exit(4)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Control listener: the suite dials here to drive and observe the call.
	ln, err := net.Listen("unix", *control)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakechild: listen control: %v\n", err)
		os.Exit(5)
	}
	go func() {
		for {
			ctrlConn, err := ln.Accept()
			if err != nil {
				return
			}
			c.mu.Lock()
			c.ctrls[ctrlConn] = struct{}{}
			c.mu.Unlock()
			go func() {
				sc := bufio.NewScanner(ctrlConn)
				sc.Buffer(make([]byte, 0, 16*1024), 1024*1024)
				for sc.Scan() {
					c.handleCtrl(sc.Bytes())
				}
				c.mu.Lock()
				delete(c.ctrls, ctrlConn)
				c.mu.Unlock()
				_ = ctrlConn.Close() //nolint:errcheck // cleanup
			}()
		}
	}()

	// Mirror every daemon->child frame to the control clients.
	textErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var f frame
			if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
				continue
			}
			c.toCtrl(f)
		}
		textErr <- io.EOF
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sigs:
	case err := <-textErr:
		_ = err // daemon closed the connection: exit cleanly
	case <-time.After(24 * time.Hour):
	}
	_ = ln.Close()   //nolint:errcheck // shutdown
	_ = conn.Close() //nolint:errcheck // shutdown
}
