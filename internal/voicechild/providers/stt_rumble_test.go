package providers

// F1 gate: the Rumble adapter buffers one voiced PCM window, posts it to a
// warm vLLM transcription server as base64 audio, and emits exactly one
// Final transcript. The fake server proves the keyless request shape.
// RED until leaf 01 lands newRumbleSTT.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

func TestRumbleSTTStream(t *testing.T) {
	var sawFile, sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/audio/transcriptions") {
			t.Errorf("request path = %q, want /v1/audio/transcriptions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, ok := body["file"]; ok {
			sawFile = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"text":"hello there"}`)
	}))
	defer srv.Close()

	stt, err := newRumbleSTT(ProviderConfig{
		Vendor:     "rumble",
		Model:      "Reson8/Rumble-2-Flash",
		BaseURL:    srv.URL,
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("newRumbleSTT: %v", err)
	}
	var _ voicechild.STTProvider = stt

	pcm := make(chan []byte)
	go func() {
		for range 40 {
			pcm <- make([]byte, 640)
		}
		close(pcm)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	select {
	case tr, ok := <-out:
		if !ok {
			t.Fatal("stream closed with no transcript")
		}
		if tr.Text != "hello there" || !tr.Final {
			t.Fatalf("transcript = %+v, want {hello there true}", tr)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for transcript")
	}
	if !sawFile {
		t.Error("request omitted base64 audio field 'file'")
	}
	if sawAuth {
		t.Error("keyless Rumble adapter sent an Authorization header")
	}
}

// ---- F1/F3 gate additions (leaf 00T). TestRumbleSTTStream above is untouched. ----

// audioURLFromBody extracts messages[0].content[0].audio_url.url from a
// chat.completions request body (contract C3). ok=false when the shape is
// absent (today's buggy {"model","file"} body).
func audioURLFromBody(body map[string]any) (string, bool) {
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return "", false
	}
	m, ok := msgs[0].(map[string]any)
	if !ok {
		return "", false
	}
	content, ok := m["content"].([]any)
	if !ok || len(content) == 0 {
		return "", false
	}
	part, ok := content[0].(map[string]any)
	if !ok {
		return "", false
	}
	audio, ok := part["audio_url"].(map[string]any)
	if !ok {
		return "", false
	}
	u, ok := audio["url"].(string)
	return u, ok
}

// feedRumblePCM sends a few voiced chunks then closes pcm, so the adapter
// flushes exactly one window on close.
func feedRumblePCM(pcm chan<- []byte) {
	for range 3 {
		pcm <- []byte{1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0}
	}
	close(pcm)
}

// TestRumbleRequestShape (F1/C3): the Rumble adapter must POST a
// chat.completions request whose messages[0].content[0].audio_url.url is a
// WAV data URI, send no Authorization header, and read the transcript from
// choices[0].message.content. RED today: NewSTT posts {"model","file"} to
// /v1/audio/transcriptions and parses a flat {"text"} response.
func TestRumbleRequestShape(t *testing.T) {
	type captured struct {
		path string
		auth string
		body map[string]any
	}
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := captured{path: r.URL.Path, auth: r.Header.Get("Authorization")}
		if err := json.NewDecoder(r.Body).Decode(&c.body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"hello there"}}]}`)
	}))
	defer srv.Close()

	stt, err := NewSTT(ProviderConfig{
		Vendor: "rumble", Model: "Reson8/Rumble-2-Flash",
		BaseURL: srv.URL, SampleRate: 16000,
	}, "")
	if err != nil {
		t.Fatalf("NewSTT: %v", err)
	}
	pcm := make(chan []byte)
	go feedRumblePCM(pcm)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var texts []voicechild.Transcript
	for tr := range out {
		texts = append(texts, tr)
	}

	// Assert the wire shape FIRST so the RED output names the wrong
	// path/body (the transport bug), not just the empty transcript.
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("server saw no transcription request")
	}
	for i, c := range got {
		if !strings.HasSuffix(c.path, "/v1/chat/completions") {
			t.Errorf("request %d path = %q, want suffix /v1/chat/completions", i, c.path)
		}
		if c.auth != "" {
			t.Errorf("request %d carried Authorization header (Rumble is keyless)", i)
		}
		uri, ok := audioURLFromBody(c.body)
		if !ok || !strings.HasPrefix(uri, "data:audio/wav;base64,") {
			t.Errorf("request %d messages[0].content[0].audio_url.url = %q (present=%v), want data:audio/wav;base64,...", i, uri, ok)
		}
	}
	if len(texts) != 1 {
		t.Fatalf("transcript count = %d, want exactly 1: %+v", len(texts), texts)
	}
	if texts[0].Text != "hello there" || !texts[0].Final {
		t.Fatalf("transcript = %+v, want {hello there true}", texts[0])
	}
}

// setKwargsField sets one C2 field on cfg through reflection so this gate
// compiles before the fields exist (they land in leaf 01).
func setKwargsField(t *testing.T, cfg *ProviderConfig, name string, val any) {
	t.Helper()
	f := reflect.ValueOf(cfg).Elem().FieldByName(name)
	if !f.IsValid() || !f.CanSet() {
		t.Fatalf("cannot set ProviderConfig.%s", name)
	}
	switch v := val.(type) {
	case string:
		f.SetString(v)
	case *bool:
		if v == nil {
			f.Set(reflect.Zero(f.Type()))
			return
		}
		p := reflect.New(f.Type().Elem())
		p.Elem().SetBool(*v)
		f.Set(p)
	default:
		t.Fatalf("unsupported kwargs value type %T", val)
	}
}

// runRumbleRequest drives one Rumble stream against a fresh fake
// chat.completions server and returns the captured request body + auth.
func runRumbleRequest(t *testing.T, cfg ProviderConfig) (map[string]any, string) {
	t.Helper()
	var mu sync.Mutex
	var body map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		body, auth = b, r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()
	cfg.Vendor = "rumble"
	cfg.BaseURL = srv.URL
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 16000
	}
	stt, err := NewSTT(cfg, "")
	if err != nil {
		t.Fatalf("NewSTT: %v", err)
	}
	pcm := make(chan []byte)
	go feedRumblePCM(pcm)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := stt.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range out { // drain so the handler completes
	}
	mu.Lock()
	defer mu.Unlock()
	return body, auth
}

// TestRumbleTemplateKwargs (F3/C2/C3): ProviderConfig's chat_template_*
// fields must reach the request body's chat_template_kwargs with exact
// values; an all-nil/empty config must omit the key entirely. RED today:
// the C2 fields do not exist (named by the reflection preflight) and the
// transport has no kwargs.
func TestRumbleTemplateKwargs(t *testing.T) {
	ct := reflect.TypeFor[ProviderConfig]()
	type fieldSpec struct {
		name string
		want reflect.Kind
		ptr  bool
	}
	spec := []fieldSpec{
		{"ChatTemplateLanguage", reflect.String, false},
		{"ChatTemplateDigits", reflect.Bool, true},
		{"ChatTemplateFiller", reflect.Bool, true},
		{"ChatTemplateVerbatim", reflect.Bool, true},
	}
	for _, s := range spec {
		f, ok := ct.FieldByName(s.name)
		if !ok {
			t.Errorf("missing symbol ProviderConfig.%s (contract C2 F3)", s.name)
			continue
		}
		got := f.Type
		if s.ptr {
			if got.Kind() != reflect.Ptr || got.Elem().Kind() != s.want {
				t.Errorf("ProviderConfig.%s is %s, want *%s", s.name, got, s.want)
			}
		} else if got.Kind() != s.want {
			t.Errorf("ProviderConfig.%s is %s, want %s", s.name, got, s.want)
		}
	}
	if t.Failed() {
		t.Fatalf("C2 kwargs fields missing - cannot gate request body")
	}

	falseVal, trueVal := false, true
	cfg := ProviderConfig{Vendor: "rumble", Model: "Reson8/Rumble-2-Flash"}
	setKwargsField(t, &cfg, "ChatTemplateLanguage", "English")
	setKwargsField(t, &cfg, "ChatTemplateDigits", &falseVal)
	setKwargsField(t, &cfg, "ChatTemplateFiller", &trueVal)
	setKwargsField(t, &cfg, "ChatTemplateVerbatim", &falseVal)

	body, auth := runRumbleRequest(t, cfg)
	if auth != "" {
		t.Errorf("Rumble request carried Authorization header")
	}
	kw, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("request body has no chat_template_kwargs: %v", body)
	}
	want := map[string]any{"language": "English", "digits": false, "filler": true, "verbatim": false}
	for k, w := range want {
		if got, ok := kw[k]; !ok || got != w {
			t.Errorf("chat_template_kwargs[%q] = %v (present=%v), want %v (body: %v)", k, got, ok, w, body)
		}
	}

	// All-nil/empty row: the key must be omitted entirely.
	body2, _ := runRumbleRequest(t, ProviderConfig{Vendor: "rumble", Model: "Reson8/Rumble-2-Flash"})
	if _, ok := body2["chat_template_kwargs"]; ok {
		t.Errorf("nil kwargs row still sent chat_template_kwargs: %v", body2)
	}
}
