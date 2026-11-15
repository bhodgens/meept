// Command clipchild is the voice_clips suite's hermetic child stand-in
// (gate leaf 00T). Like the voice-lifecycle fakechild it speaks C1 over
// the REAL daemon text socket and C2 over the per-call audio pairs and
// exposes a control socket the suite drives/observes — with THREE
// clip-era additions the shared fakechild cannot make (sibling suites
// depend on it unchanged):
//
//  1. EVERY daemon->child frame is mirrored to the control socket with
//     its RAW line verbatim (keys lines mirrored with key VALUES
//     redacted), so the suite can assert the exact C3 wire shapes the
//     daemon planner emits (clip_render clips arrays and say clip_id
//     never survive a re-encode through the sibling's stripped frame
//     struct).
//  2. clip_render handling: for each {key,text} not present in the REAL
//     clipstore (internal/voice/clipstore, Contract C2 paths under the
//     daemon's MEEPT_HOME), synthesize through the SAME providers.NewTTS
//     adapter the real child constructs from the keys line, store via
//     Put, and answer one C3 clip_ready frame (ok:true/false).
//  3. say with clip_id + store hit: stream the stored PCM to the .out
//     socket with NO provider call; miss/absent: today's live Speak
//     path (the fake TTS endpoint can be configured slow, which is how
//     the suite proves cached filler latency).
//
// CONTRACT PROVENANCE: render/playback CODE here is a suite-local driver
// over the real clipstore package + real provider adapter; the REAL
// child-side semantics are unit-pinned in internal/voicechild
// (clip_playback_test.go + leaf 03's own suite). What THIS binary proves
// e2e is the DAEMON side: keys -> clip_render emission, pool->file set
// equality, identity-change regeneration, say frames carrying clip_id,
// and farewell-before-end ordering.
//
// Barge-in/STT/turn-confirm machinery is deliberately absent — the
// voice_clips scenarios never exercise it, and a missing feature here
// must not be mistaken for a daemon bug.
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

	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voicechild"
	"github.com/caimlas/meept/internal/voicechild/providers"
	"github.com/caimlas/meept/internal/voiceframe"
)

// ctrlMsg is one control-socket message (either direction). Raw carries
// the verbatim mirrored line (daemon->child frames only).
type ctrlMsg struct {
	Ctrl      string `json:"ctrl"`
	CallID    string `json:"call_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Last      *bool  `json:"last,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Direction string `json:"direction,omitempty"`
	Raw       string `json:"raw,omitempty"`
}

// keysWire mirrors the daemon keys line fields (same JSON keys; values of
// stt_key/tts_key are consumed here and REDACTED before mirroring).
type keysWire struct {
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

// clipRenderWire mirrors the C3 clip_render line: {"event":
// "clip_render","call_id":"...","clips":[{"key":...,"text":...}]}.
type clipRenderWire struct {
	Event  string `json:"event"`
	CallID string `json:"call_id"`
	Clips  []struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	} `json:"clips"`
}

type callState struct {
	tts      voicechild.TTSProvider
	ident    keysWire // selectors only (keys zeroed after construction)
	out      net.Conn
	sayQ     chan voiceframe.Frame
	gen      uint64 // bumped by clear: playback of a superseded gen dies
	closed   bool
	inParked bool // .in reader dialed once
}

type child struct {
	mu          sync.Mutex
	ctrlWriteMu sync.Mutex            // serializes whole-line ctrl writes
	conns       map[net.Conn]struct{} // control conns
	text        net.Conn

	audioDir string
	store    *clipstore.Store

	calls map[string]*callState
}

func (c *child) toCtrl(m ctrlMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	c.mu.Lock()
	conns := make([]net.Conn, 0, len(c.conns))
	for conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	// Serialize whole-line writes: concurrent mirrors (text reader +
	// playback worker + heartbeat) must never interleave JSON lines.
	c.ctrlWriteMu.Lock()
	defer c.ctrlWriteMu.Unlock()
	for _, conn := range conns {
		if _, werr := conn.Write(append(b, '\n')); werr != nil {
			noteErr("ctrl write:", werr) // observer lane; harness discards stderr
		}
	}
}

func (c *child) logf(ccid, format string, args ...any) {
	c.toCtrl(ctrlMsg{Ctrl: "log", CallID: ccid, Text: fmt.Sprintf(format, args...)})
}

// call returns (lazily creating) the call's state and arms its playback
// worker exactly once, at creation, under c.mu.
func (c *child) call(ccid string) *callState {
	c.mu.Lock()
	st, ok := c.calls[ccid]
	if !ok {
		st = &callState{sayQ: make(chan voiceframe.Frame, 32)}
		c.calls[ccid] = st
		c.mu.Unlock()
		go c.playback(ccid)
		return st
	}
	c.mu.Unlock()
	return st
}

// sendFrame writes one child->daemon C1 line.
func (c *child) sendFrame(f any) error {
	c.mu.Lock()
	conn := c.text
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("clipchild: no text connection")
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(b, '\n'))
	return err
}

// redactKeysLine strips stt_key/tts_key VALUES for the mirror (the
// sibling's posture: key material never leaves the child).
func redactKeysLine(line string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return line
	}
	if s, ok := m["stt_key"].(string); ok && s != "" {
		m["stt_key"] = "[REDACTED]"
	}
	if s, ok := m["tts_key"].(string); ok && s != "" {
		m["tts_key"] = "[REDACTED]"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return line
	}
	return string(b)
}

// handleLine processes one raw daemon->child line: mirror it, then act
// on the clip-relevant events.
func (c *child) handleLine(line string) {
	var probe struct {
		CallID string `json:"call_id"`
		Event  string `json:"event"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		c.toCtrl(ctrlMsg{Ctrl: "log", Text: "clipchild rx unparseable: " + line})
		return
	}
	mirrorRaw := line
	switch probe.Event {
	case "keys":
		mirrorRaw = redactKeysLine(line)
		var k keysWire
		if err := json.Unmarshal([]byte(line), &k); err == nil {
			c.handleKeys(k)
		}
	case "clip_render":
		var r clipRenderWire
		if err := json.Unmarshal([]byte(line), &r); err == nil {
			go c.handleClipRender(r)
		}
	case "say":
		var f voiceframe.Frame
		if err := json.Unmarshal([]byte(line), &f); err == nil {
			st := c.call(f.CallID)
			st.sayQ <- f
		}
	case "clear":
		st := c.call(probe.CallID)
		c.mu.Lock()
		st.gen++
		c.mu.Unlock()
	case "end":
		c.mu.Lock()
		if st, ok := c.calls[probe.CallID]; ok {
			st.closed = true
		}
		c.mu.Unlock()
	case "open":
		// Answer exactly like the real child (the local-leg topology is
		// unused by this suite but the open frame must not go unhandled).
		noteErr("open setup send:", c.sendFrame(map[string]any{
			"call_id": probe.CallID, "event": "setup",
			"codec": "L16", "sample_rate": 16000,
		}))
	}
	c.toCtrl(ctrlMsg{Ctrl: "frame", CallID: probe.CallID, Raw: mirrorRaw})
}

// handleKeys constructs the call's REAL TTS adapter from the keys line
// (same C3 dispatch cmd/meept-voice installs) and stores the identity
// selectors for render meta.
func (c *child) handleKeys(k keysWire) {
	st := c.call(k.CallID)
	tts, err := providers.NewTTS(voicechild.ProviderConfig{
		Vendor:     k.TTSVendor,
		Model:      k.TTSModel,
		BaseURL:    k.TTSBaseURL,
		Voice:      k.TTSVoice,
		SampleRate: 16000, // this suite streams the fake endpoint at the negotiated rate
	}, k.TTSKey)
	if err != nil {
		c.logf(k.CallID, "clipchild tts construct failed: %v", err)
		return
	}
	k.STTKey, k.TTSKey = "", ""
	c.mu.Lock()
	st.tts = tts
	st.ident = k
	c.mu.Unlock()
	c.logf(k.CallID, "clipchild tts constructed vendor=%s model=%s voice=%s", k.TTSVendor, k.TTSModel, k.TTSVoice)
}

// ensureOut dials the call's .out socket once (lazy: the daemon listens
// from media start onward).
func (c *child) ensureOut(ccid string) net.Conn {
	st := c.call(ccid)
	c.mu.Lock()
	if st.out != nil {
		out := st.out
		c.mu.Unlock()
		return out
	}
	c.mu.Unlock()
	out, err := net.Dial("unix", filepath.Join(c.audioDir, ccid+".out"))
	if err != nil {
		c.logf(ccid, "clipchild dial .out: %v", err)
		return nil
	}
	c.mu.Lock()
	st.out = out
	c.mu.Unlock()
	return out
}

// playback drains the say queue in wire order: clip_id + store hit
// streams stored PCM with ZERO provider calls; otherwise today's live
// Speak.
func (c *child) playback(ccid string) {
	st := c.call(ccid)
	for f := range st.sayQ {
		c.mu.Lock()
		gen := st.gen
		c.mu.Unlock()
		out := c.ensureOut(ccid)
		if out == nil {
			c.logf(ccid, "clipchild say dropped (no audio out) text=%q", f.Text)
			continue
		}
		var pcm io.ReadCloser
		source := "live"
		started := time.Now()
		if f.ClipID != "" && c.store.Has(f.ClipID) {
			rc, _, err := c.store.Get(f.ClipID)
			if err == nil {
				pcm = rc
				source = "clip"
			}
		}
		if pcm == nil {
			c.mu.Lock()
			tts := st.tts
			c.mu.Unlock()
			if tts == nil {
				c.logf(ccid, "clipchild say: no tts adapter, text=%q", f.Text)
				continue
			}
			r, err := tts.Speak(context.Background(), f.Text)
			if err != nil {
				c.logf(ccid, "clipchild live speak failed: %v", err)
				continue
			}
			pcm = r
		}
		buf := make([]byte, 640)
		total := 0
		for {
			c.mu.Lock()
			curGen := st.gen
			c.mu.Unlock()
			if curGen != gen {
				noteErr("barge-in abort close:", pcm.Close())
				c.logf(ccid, "clipchild playback aborted clip=%s", f.ClipID)
				break
			}
			n, rerr := pcm.Read(buf)
			if n > 0 {
				if _, werr := out.Write(buf[:n]); werr != nil {
					break
				}
				total += n
			}
			if rerr != nil {
				noteErr("drain close:", pcm.Close())
				break
			}
		}
		c.logf(ccid, "clipchild say-played source=%s key=%q bytes=%d ms=%d",
			source, f.ClipID, total, time.Since(started).Milliseconds())
	}
}

// handleClipRender renders misses through the call's adapter and answers
// clip_ready per spec (C3).
func (c *child) handleClipRender(r clipRenderWire) {
	st := c.call(r.CallID)
	for _, spec := range r.Clips {
		if c.store.Has(spec.Key) {
			c.logf(r.CallID, "clipchild render-skip key=%s", spec.Key)
			continue // existing key: no synth, no re-emit (leaf-03 parity; CP-4 ambiguity noted in 00T)
		}
		c.mu.Lock()
		tts := st.tts
		ident := st.ident
		c.mu.Unlock()
		ok := false
		if tts != nil {
			pcm, err := tts.Speak(context.Background(), spec.Text)
			if err != nil {
				c.logf(r.CallID, "clipchild render speak failed key=%s: %v", spec.Key, err)
			} else {
				buf, rerr := io.ReadAll(pcm)
				noteErr("read sink close:", pcm.Close())
				rerr = firstErr(rerr, c.store.Put(spec.Key, clipstore.Meta{
					Key:        spec.Key,
					Text:       spec.Text,
					Vendor:     ident.TTSVendor,
					Model:      ident.TTSModel,
					Voice:      ident.TTSVoice,
					SampleRate: 16000,
					Bytes:      int64(len(buf)),
					CreatedAt:  time.Now().UTC(),
				}, strings.NewReader(string(buf))))
				if rerr == nil {
					ok = true
					c.logf(r.CallID, "clipchild rendered key=%s bytes=%d", spec.Key, len(buf))
				} else {
					c.logf(r.CallID, "clipchild render store failed key=%s: %v", spec.Key, rerr)
				}
			}
		}
		// C3: {"event":"clip_ready","call_id":"<ccid>","key":"<64 hex>","ok":true}
		if err := c.sendFrame(map[string]any{
			"event": "clip_ready", "call_id": r.CallID, "key": spec.Key, "ok": ok,
		}); err != nil {
			c.logf(r.CallID, "clipchild clip_ready send: %v", err)
		}
	}
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// noteErr logs a benign transport/close error to stderr. The harness
// discards this binary's stderr, so the call changes neither frame timing
// nor frame content — it exists so no error is silently ignored.
func noteErr(op string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "clipchild:", op, err)
	}
}

// handleCtrl acts on one suite command (subset of the sibling fakechild's
// protocol: setup/prompt/dial-audio).
func (c *child) handleCtrl(raw []byte) {
	var m ctrlMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	switch m.Ctrl {
	case "setup":
		c.logf(m.CallID, "clipchild ctrl setup received")
		noteErr("ctrl setup send:", c.sendFrame(map[string]any{
			"call_id": m.CallID, "event": "setup",
			"from": m.From, "to": m.To, "direction": m.Direction,
			"codec": "L16", "sample_rate": 16000,
		}))
	case "prompt":
		f := map[string]any{"call_id": m.CallID, "event": "prompt", "text": m.Text}
		if m.Last != nil && *m.Last {
			f["last"] = true
		}
		noteErr("ctrl prompt send:", c.sendFrame(f))
	case "dial-audio":
		c.ensureOut(m.CallID)
		// Keep the .in read side parked (the splice writes inbound audio;
		// this suite never feeds a real STT).
		st := c.call(m.CallID)
		c.mu.Lock()
		already := st.inParked || st.closed
		st.inParked = true
		c.mu.Unlock()
		if !already {
			go func() {
				in, err := net.Dial("unix", filepath.Join(c.audioDir, m.CallID+".in"))
				if err != nil {
					return
				}
				buf := make([]byte, 4096)
				for {
					if _, err := in.Read(buf); err != nil {
						return
					}
				}
			}()
		}
	}
}

func main() {
	textSocket := flag.String("text-socket", "", "daemon text socket (C1)")
	audioDir := flag.String("audio-dir", "", "per-call audio pair dir (C2)")
	control := flag.String("control", "", "control socket path (suite drives)")
	flag.Parse()
	if *textSocket == "" || *audioDir == "" {
		fmt.Fprintln(os.Stderr, "clipchild: -text-socket and -audio-dir are required")
		os.Exit(2)
	}
	if *control == "" {
		*control = filepath.Join(filepath.Dir(*audioDir), "ctrl.sock")
	}

	token := ""
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	token = strings.TrimSpace(line)
	if token == "" {
		fmt.Fprintln(os.Stderr, "clipchild: empty start token on stdin")
		os.Exit(3)
	}

	c := &child{
		conns:    map[net.Conn]struct{}{},
		audioDir: *audioDir,
		store:    clipstore.NewStore(), // $MEEPT_HOME/voice_clips (C2)
		calls:    map[string]*callState{},
	}

	conn, err := net.Dial("unix", *textSocket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipchild: dial text socket: %v\n", err)
		os.Exit(4)
	}
	if _, err := fmt.Fprintf(conn, "%s\n", token); err != nil {
		fmt.Fprintf(os.Stderr, "clipchild: token proof: %v\n", err)
		os.Exit(6)
	}
	c.mu.Lock()
	c.text = conn
	c.mu.Unlock()

	ln, err := net.Listen("unix", *control)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipchild: listen control: %v\n", err)
		os.Exit(5)
	}
	go func() {
		for {
			ctrlConn, err := ln.Accept()
			if err != nil {
				return
			}
			c.mu.Lock()
			c.conns[ctrlConn] = struct{}{}
			c.mu.Unlock()
			go func() {
				sc := bufio.NewScanner(ctrlConn)
				sc.Buffer(make([]byte, 0, 16*1024), 1024*1024)
				for sc.Scan() {
					c.handleCtrl(sc.Bytes())
				}
				c.mu.Lock()
				delete(c.conns, ctrlConn)
				c.mu.Unlock()
				noteErr("ctrl conn close:", ctrlConn.Close())
			}()
		}
	}()

	textErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			c.handleLine(sc.Text())
		}
		textErr <- io.EOF
	}()

	c.toCtrl(ctrlMsg{Ctrl: "log", Text: "clipchild ready: text connected + ctrl listening"})

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	select {
	case <-sigs:
	case <-textErr:
	case <-time.After(24 * time.Hour):
	}
	if cerr := ln.Close(); cerr != nil {
		noteErr("shutdown listen close:", cerr)
	}
	if cerr := conn.Close(); cerr != nil {
		noteErr("shutdown text close:", cerr)
	}
}
