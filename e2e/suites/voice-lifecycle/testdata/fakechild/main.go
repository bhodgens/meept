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
	"context"
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

	"github.com/caimlas/meept/internal/voicechild"
	"github.com/caimlas/meept/internal/voicechild/providers"
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

// realProviders is overridable at build time (-ldflags "-X
// main.realProviders=true"): the voice-tierA e2e scenario builds this
// binary with the REAL provider adapters wired, so the C1 keys line the
// daemon sends constructs actual providers.NewSTT/NewTTS adapters. The
// default build stays factory-free (pure C1/C2 protocol fake).
var realProviders = "false"

func realMode() bool { return realProviders == "true" }

type child struct {
	textSocket string
	audioDir   string

	mu       sync.Mutex
	conn     net.Conn // the text-socket connection
	ctrls    map[net.Conn]struct{}
	dropped  bool // a write failed: the transport is dead
	audioOut map[string]net.Conn
	sttRate  map[string]int
	// realProviders mode only: the C3 TTS factory (the same
	// providers.NewTTS dispatch cmd/meept-voice installs) and the
	// adapters constructed from each call's keys line. In-memory only.
	ttsFactory voicechild.TTSFactory
	tts        map[string]voicechild.TTSProvider

	// speaking/latched mirror the pipeline's playback state for the
	// bargein control: speaking is set when a say frame is mirrored for
	// the call, latched is one interrupt per barge-in until the next say.
	speaking map[string]bool
	latched  map[string]bool
}

// realFactories mirrors cmd/meept-voice's config-driven C3 wiring: the
// factory dispatches on the ProviderConfig the keys line carried. Used
// only when the binary was built with realProviders=true.
func realFactories() voicechild.TTSFactory {
	return func(cfg voicechild.ProviderConfig, key string) (voicechild.TTSProvider, error) {
		return providers.NewTTS(cfg, key)
	}
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
	c.mu.Lock()
	c.audioOut[ccid] = outConn
	c.mu.Unlock()
	// Drain .in (the splice writes inbound audio; the fake STT ignores it
	// unless the suite speaks real ones - see speak-real).
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

// speakReal synthesizes text through the call's REAL TTS adapter (the one
// constructed from the daemon's keys line) and writes the resulting PCM
// to the C2 .out socket - the same surface the real pipeline's playback()
// uses. Errors are reported over the control socket as log frames.
func (c *child) speakReal(ccid, text string) {
	c.mu.Lock()
	tts := c.tts[ccid]
	out := c.audioOut[ccid]
	rate := c.sttRate[ccid]
	c.mu.Unlock()
	if tts == nil {
		c.toCtrl(frame{CallID: ccid, Event: "log", Text: "fakechild speak-real: no tts adapter constructed"})
		return
	}
	if out == nil {
		c.toCtrl(frame{CallID: ccid, Event: "log", Text: "fakechild speak-real: audio pair not dialed"})
		return
	}
	pcm, err := tts.Speak(context.Background(), text)
	if err != nil {
		c.toCtrl(frame{CallID: ccid, Event: "log", Text: "fakechild speak-real speak: " + err.Error()})
		return
	}
	defer func() { _ = pcm.Close() }() //nolint:errcheck // test sink drain
	buf := make([]byte, 4096)
	total := 0
	for {
		n, rerr := pcm.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				c.toCtrl(frame{CallID: ccid, Event: "log", Text: "fakechild speak-real write: " + werr.Error()})
				return
			}
			total += n
		}
		if rerr != nil {
			break
		}
	}
	c.toCtrl(frame{CallID: ccid, Event: "log", Text: fmt.Sprintf("fakechild speak-real wrote %d bytes at rate %d", total, rate)})
}

// answerOpen mirrors the real child's open contract (internal/voicechild
// open.go handleOpen): dial the C2 pair for the frame's call and answer
// with the setup frame the daemon's router arms the talk loop on. This
// makes the fake child behave like meept-voice on BOTH legs: the phone
// suites keep driving setup manually (an explicit setup for a live ccid
// is an idempotent no-op daemon-side), and the local leg now completes
// its open->setup->keys chain hermetically.
func (c *child) answerOpen(f frame) {
	if err := c.dialAudio(f.CallID); err != nil {
		c.toCtrl(frame{CallID: f.CallID, Event: "log", Text: "fakechild open dial-audio failed: " + err.Error()})
	}
	setup := frame{
		CallID:     f.CallID,
		Event:      "setup",
		From:       f.From,
		To:         f.To,
		Direction:  f.Direction,
		Codec:      f.Codec,
		SampleRate: f.SampleRate,
	}
	if setup.Codec == "" {
		setup.Codec = "L16"
		setup.SampleRate = 16000
	}
	if err := c.sendFrame(setup); err != nil {
		c.toCtrl(frame{CallID: f.CallID, Event: "log", Text: "fakechild setup answer failed: " + err.Error()})
	}
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
	case "speak-real":
		// voice-tierA leaf 07: synthesize through the call's REAL TTS
		// adapter (constructed from the daemon's keys line) and push the
		// PCM onto the C2 .out socket. No-op without a constructed
		// adapter (reported as a log frame, never a panic).
		go c.speakReal(m.CallID, m.Text)
	case "bargein":
		// voice-tierA leaf 07: replay the leaf-06 transcript-branch frame
		// sequence - interrupt BEFORE the partial's prompt (C5 order),
		// exactly what the real pipeline's consumeTranscripts emits when
		// a STT partial lands while playback is live. The latch mirrors
		// noteBargeIn: one interrupt per barge-in until the next say
		// re-arms playback. The suite's bargein scenario drives this
		// control instead of a raw prompt so the daemon observes the
		// same frames the real child produces.
		c.mu.Lock()
		speaking := c.speaking[m.CallID]
		latched := c.latched[m.CallID]
		c.mu.Unlock()
		if speaking && !latched {
			intFrame := frame{CallID: m.CallID, Event: "interrupt"}
			if err := c.sendFrame(intFrame); err != nil {
				c.toCtrl(frame{CallID: m.CallID, Event: "log", Text: "fakechild bargein interrupt send failed: " + err.Error()})
				return
			}
			// Mirror the emitted interrupt to the control clients: the
			// suite's observable that the C5-order frame actually left
			// the child toward the daemon.
			c.toCtrl(intFrame)
			c.mu.Lock()
			c.latched[m.CallID] = true
			c.mu.Unlock()
		}
		last := false
		f := frame{CallID: m.CallID, Event: "prompt", Text: m.Text, Last: &last}
		if err := c.sendFrame(f); err != nil {
			c.toCtrl(frame{CallID: m.CallID, Event: "log", Text: "fakechild bargein prompt send failed: " + err.Error()})
		}
	}
}

// keysLine mirrors the daemon's C1 keys wire shape (the child decodes the
// raw line; the fake mirrors exactly the fields the real decoder reads).
type keysLine struct {
	CallID     string `json:"call_id"`
	Event      string `json:"event"`
	STTKey     string `json:"stt_key"`
	TTSKey     string `json:"tts_key"`
	STTVendor  string `json:"stt_vendor"`
	STTModel   string `json:"stt_model"`
	STTBaseURL string `json:"stt_base_url"`
	TTSVendor  string `json:"tts_vendor"`
	TTSModel   string `json:"tts_model"`
	TTSBaseURL string `json:"tts_base_url"`
	TTSVoice   string `json:"tts_voice"`
}

// handleRawLine processes one raw daemon->child line. The shared codec
// decodes every frame; keys lines (event "keys", NOT in the shared Frame
// shape) are consumed here: realProviders mode constructs the REAL
// providers adapters from the frame exactly as cmd/meept-voice's
// config-driven factories do (C3), and every mode reports the negotiated
// sample rate back to the suite over the control socket.
func (c *child) handleRawLine(line []byte) {
	var probe frame
	if err := json.Unmarshal(line, &probe); err != nil || probe.Event != "keys" {
		return
	}
	var k keysLine
	if err := json.Unmarshal(line, &k); err != nil {
		return
	}
	c.mu.Lock()
	rate := 16000
	if r, ok := c.sttRate[k.CallID]; ok && r != 0 {
		rate = r
	}
	ttsFactory := c.ttsFactory
	c.mu.Unlock()
	c.toCtrl(frame{CallID: k.CallID, Event: "log", Text: fmt.Sprintf("fakechild keys received stt_vendor=%s tts_vendor=%s rate=%d", k.STTVendor, k.TTSVendor, rate)})

	if !realMode() || ttsFactory == nil {
		return
	}
	// C3 dispatch: the SAME config-driven factories cmd/meept-voice
	// installs (NewTTS on the ProviderConfig the keys line carried).
	tts, terr := ttsFactory(voicechild.ProviderConfig{
		Vendor:     k.TTSVendor,
		Model:      k.TTSModel,
		BaseURL:    k.TTSBaseURL,
		Voice:      k.TTSVoice,
		SampleRate: rate,
	}, k.TTSKey)
	if terr != nil {
		c.toCtrl(frame{CallID: k.CallID, Event: "log", Text: "fakechild tts construct failed: " + terr.Error()})
		return
	}
	c.mu.Lock()
	c.tts[k.CallID] = tts
	c.mu.Unlock()
	c.toCtrl(frame{CallID: k.CallID, Event: "log", Text: "fakechild tts constructed"})
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

	c := &child{
		textSocket: *textSocket,
		audioDir:   *audioDir,
		ctrls:      map[net.Conn]struct{}{},
		audioOut:   map[string]net.Conn{},
		sttRate:    map[string]int{},
		tts:        map[string]voicechild.TTSProvider{},
		speaking:   map[string]bool{},
		latched:    map[string]bool{},
	}
	if realMode() {
		c.ttsFactory = realFactories()
	}

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

	// Mirror every daemon->child frame to the control clients. The raw
	// line ALSO feeds handleRawLine first: keys lines are decoded there
	// (rate capture + realProviders adapter construction) before the
	// shared-codec mirror drops them as unknown events. An OPEN frame is
	// answered with setup exactly like the real child (internal/voicechild
	// open.go handleOpen): the local-leg daemon sends open and waits for
	// the setup answer to arm its talk loop.
	textErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			raw := sc.Bytes()
			line := make([]byte, len(raw))
			copy(line, raw)
			c.handleRawLine(line)
			var f frame
			if err := json.Unmarshal(line, &f); err != nil {
				continue
			}
			if f.Event == "open" {
				c.answerOpen(f)
				continue
			}
			if f.Event == "keys" {
				continue // never mirrored over the control socket: key material
			}
			if f.Event == "say" {
				// Playback state mirror: a say (re)arms the barge-in
				// latch, exactly like the real pipeline's playback().
				c.mu.Lock()
				c.speaking[f.CallID] = true
				c.latched[f.CallID] = false
				c.mu.Unlock()
			}
			if f.Event == "clear" {
				c.mu.Lock()
				c.speaking[f.CallID] = false
				c.mu.Unlock()
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
