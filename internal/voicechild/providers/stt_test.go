package providers

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/gojargo/jargo/service/wsutil"
)

// dgFakeServer is an httptest WS vendor fake speaking Deepgram's live
// transcription protocol: it records binary audio frames and answers with
// configurable Results JSON.
type dgFakeServer struct {
	srv      *httptest.Server
	mu       countGuard
	gotAuth  string
	gotQuery string
	results  []string // text messages to emit, one per binary frame received
}

// countGuard is a tiny mutex+int pair (named so the fake reads clearly).
type countGuard struct {
	n int
}

// dgResultsJSON builds one Results message.
func dgResultsJSON(text string, isFinal bool) string {
	final := "false"
	if isFinal {
		final = "true"
	}
	return `{"type":"Results","channel":{"alternatives":[{"transcript":"` + text + `"}]},"is_final":` + final + `}`
}

// newDGFake starts the fake vendor; the returned base host (host:port) is
// what the adapter dials (ws scheme for the httptest server).
func newDGFake(t *testing.T, results []string) *dgFakeServer {
	t.Helper()
	f := &dgFakeServer{results: results}
	upgrader := func(w http.ResponseWriter, r *http.Request) {
		if f.gotAuth == "" {
			f.gotAuth = r.Header.Get("Authorization")
			f.gotQuery = r.URL.RawQuery
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "") // test teardown
		ctx := r.Context()
		emitted := 0
		for {
			mt, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if mt != websocket.MessageBinary {
				continue
			}
			f.mu.n++
			if emitted < len(f.results) {
				if werr := conn.Write(ctx, websocket.MessageText, []byte(f.results[emitted])); werr != nil {
					return
				}
				emitted++
			}
		}
	}
	f.srv = httptest.NewServer(http.HandlerFunc(upgrader))
	t.Cleanup(f.srv.Close)
	return f
}

// newTestSTT builds a DeepgramSTT wired to the fake vendor.
func newTestSTT(t *testing.T, results []string) (*DeepgramSTT, *dgFakeServer) {
	t.Helper()
	f := newDGFake(t, results)
	s := &DeepgramSTT{
		cfg:    ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000},
		apiKey: "test-key",
	}
	host := strings.TrimPrefix(f.srv.URL, "http://")
	s.setSTTDialer(func(ctx context.Context, path string, header wsHeader) (wsSession, error) {
		conn, err := dialFakeWS(ctx, "ws://"+host+path, header)
		if err != nil {
			return nil, err
		}
		return conn, nil
	})
	return s, f
}

// pcmS16LE builds n samples of a deterministic non-zero s16le pattern.
func pcmS16LE(n int) []byte {
	b := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(1000+i))
	}
	return b
}

// T2: PCM streamed in -> finals surface with text.
func TestSTTStreamsFinals(t *testing.T) {
	s, f := newTestSTT(t, []string{
		dgResultsJSON("hel", false),
		dgResultsJSON("hello world", true),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pcm := make(chan []byte)
	out, err := s.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	pcm <- pcmS16LE(160)
	pcm <- pcmS16LE(160)

	// Interim then final, in order.
	select {
	case tr := <-out:
		if tr.Text != "hel" || tr.Final {
			t.Fatalf("first transcript wrong: %+v", tr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for interim transcript")
	}
	select {
	case tr := <-out:
		if tr.Text != "hello world" || !tr.Final {
			t.Fatalf("final transcript wrong: %+v", tr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for final transcript")
	}

	if f.gotAuth != "Token test-key" {
		t.Fatalf("Authorization header wrong: %q", f.gotAuth)
	}
	if !strings.Contains(f.gotQuery, "model=nova-3") ||
		!strings.Contains(f.gotQuery, "sample_rate=16000") ||
		!strings.Contains(f.gotQuery, "encoding=linear16") {
		t.Fatalf("query wrong: %s", f.gotQuery)
	}
	cancel()
}

// T2: one WS drop mid-call reconnects exactly once and continues
// streaming on the new session. The first fake server closes its
// connection right after serving one result (a real vendor drop); the
// adapter's single in-call retry lands on the second server.
func TestSTTReconnectsOnce(t *testing.T) {
	var dialMu sync.Mutex
	dials := 0
	f2 := newDGFake(t, []string{dgResultsJSON("partial then final", true)})
	host2 := strings.TrimPrefix(f2.srv.URL, "http://")

	// First server: serve one result, then drop the connection.
	upgrader1 := func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		for {
			mt, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if mt != websocket.MessageBinary {
				continue
			}
			dialMu.Lock()
			dials++
			first := dials
			dialMu.Unlock()
			conn.Write(ctx, websocket.MessageText, []byte(dgResultsJSON("par", false))) // best-effort vendor frame; write errors are the drop path under test
			if first == 1 {
				// Abrupt vendor drop after the interim.
				conn.Close(websocket.StatusAbnormalClosure, "drop") // best-effort test teardown
				return
			}
		}
	}
	f1 := httptest.NewServer(http.HandlerFunc(upgrader1))
	t.Cleanup(f1.Close)
	host1 := strings.TrimPrefix(f1.URL, "http://")

	s := &DeepgramSTT{
		cfg:    ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000},
		apiKey: "test-key",
	}
	s.setSTTDialer(func(ctx context.Context, path string, header wsHeader) (wsSession, error) {
		dialMu.Lock()
		n := dials + 1 // dial in flight
		dialMu.Unlock()
		host := host1
		if n > 1 {
			host = host2
		}
		conn, err := dialFakeWS(ctx, "ws://"+host+path, header)
		if err != nil {
			return nil, err
		}
		return conn, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcm := make(chan []byte)
	out, err := s.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	pcm <- pcmS16LE(160) // triggers interim from session 1, then its drop
	// Speech continues after the drop: this chunk reaches session 2 (the
	// reconnect). Audio written into the dropped session's TCP buffer
	// before the drop was detected is lost - the same trade jargo's
	// wsservice documents ("a dropped session costs the audio spoken
	// during the gap").
	go func() {
		time.Sleep(50 * time.Millisecond)
		pcm <- pcmS16LE(160)
	}()

	gotInterim := false
	deadline := time.After(5 * time.Second)
	for {
		select {
		case tr, ok := <-out:
			if !ok {
				t.Fatalf("stream closed before the final transcript")
			}
			if tr.Final {
				if tr.Text != "partial then final" {
					t.Fatalf("final text wrong: %+v", tr)
				}
				dialMu.Lock()
				n := dials
				dialMu.Unlock()
				if n != 1 {
					t.Fatalf("first-session frames = %d, want 1", n)
				}
				return
			}
			gotInterim = true
		case <-deadline:
			dialMu.Lock()
			n := dials
			dialMu.Unlock()
			t.Fatalf("timed out (interim=%v, first-session frames=%d)", gotInterim, n)
		}
	}
}

// T2 negative: a second drop (one retry already spent) ends the stream
// with a closed channel - no infinite retry loop.
func TestSTTSecondDropEndsStream(t *testing.T) {
	var dialMu sync.Mutex
	dials := 0

	// Both fake servers accept, count the dial, and drop immediately
	// (no results ever flow).
	dropping := func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		dialMu.Lock()
		dials++
		dialMu.Unlock()
		conn.Close(websocket.StatusAbnormalClosure, "drop") // best-effort test teardown; close status is the signal, not the error
	}
	f1 := httptest.NewServer(http.HandlerFunc(dropping))
	f2 := httptest.NewServer(http.HandlerFunc(dropping))
	t.Cleanup(f1.Close)
	t.Cleanup(f2.Close)
	hosts := []string{strings.TrimPrefix(f1.URL, "http://"), strings.TrimPrefix(f2.URL, "http://")}

	s := &DeepgramSTT{
		cfg:    ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000},
		apiKey: "test-key",
	}
	s.setSTTDialer(func(ctx context.Context, path string, header wsHeader) (wsSession, error) {
		dialMu.Lock()
		idx := dials
		dialMu.Unlock()
		if idx >= len(hosts) {
			t.Errorf("more than %d dials: reconnect policy violated", len(hosts))
			return nil, context.DeadlineExceeded
		}
		return dialFakeWS(ctx, "ws://"+hosts[idx]+path, header)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcm := make(chan []byte)
	out, err := s.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Close the chunk stream: the pump's Finalize write fails on every
	// dropped session, so both sessions error out; the single retry is
	// spent and the stream must end (closed channel), not loop forever.
	close(pcm)

	select {
	case _, ok := <-out:
		if ok {
			t.Fatalf("expected closed transcript channel")
		}
	case <-time.After(5 * time.Second):
		dialMu.Lock()
		n := dials
		dialMu.Unlock()
		t.Fatalf("stream did not end after the single retry was spent (dials=%d)", n)
	}
	dialMu.Lock()
	n := dials
	dialMu.Unlock()
	if n != 2 {
		t.Fatalf("dials = %d, want exactly 2 (initial + one retry)", n)
	}
}

// dialFakeWS dials the fake vendor over plain ws (the httptest server is
// HTTP, not TLS), reusing jargo's wsutil for handshake classification and
// the bounded close.
func dialFakeWS(ctx context.Context, url string, header wsHeader) (wsSession, error) {
	conn, err := wsutil.Dial(ctx, url, header.http(), wsutil.DefaultReadLimit)
	if err != nil {
		return nil, err
	}
	return &wsutilSession{Conn: conn}, nil
}
