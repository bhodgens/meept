package llm

// Tests for the AWS Bedrock path: stdlib SigV4 signing (sigv4.go), the
// Bedrock request-shape adapters (bedrock.go), and the end-to-end signed
// invoke flow through AnthropicClient.
//
// Signatures are fixed to a constant clock and asserted byte-for-byte
// against an independently computed value (the same algorithm implemented
// per the AWS SigV4 spec), plus structural checks on the headers AWS
// requires on bedrock-runtime invoke calls.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sigv4TestClock is the fixed signing time all vector assertions use.
var sigv4TestClock = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// expectedSigV4Authorization recomputes the Authorization header per the AWS
// SigV4 spec, independently of signSigV4's code path (same algorithm, written
// straight from the spec steps: canonical request -> string to sign ->
// derived key -> HMAC).
func expectedSigV4Authorization(method, canonicalURI, host, contentType string, body []byte, creds awsCredentials, region, service string, t time.Time) string {
	amzDate := t.UTC().Format("20060102T150405Z")
	dateStamp := t.UTC().Format("20060102")

	payloadHashBytes := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payloadHashBytes[:])

	headers := map[string]string{
		"content-type":         contentType,
		"host":                 host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           amzDate,
	}
	if creds.SessionToken != "" {
		headers["x-amz-security-token"] = creds.SessionToken
	}
	names := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"}
	if creds.SessionToken != "" {
		names = append(names, "x-amz-security-token")
	}
	// names are already sorted by construction.

	var canonicalHeaders strings.Builder
	for _, n := range names {
		canonicalHeaders.WriteString(n)
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(headers[n]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		"",
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	crHash := sha256.Sum256([]byte(canonicalRequest))
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(crHash[:])

	mac := func(key, data []byte) []byte {
		m := hmac.New(sha256.New, key)
		m.Write(data)
		return m.Sum(nil)
	}
	kDate := mac([]byte("AWS4"+creds.SecretAccessKey), []byte(dateStamp))
	kRegion := mac(kDate, []byte(region))
	kService := mac(kRegion, []byte(service))
	kSigning := mac(kService, []byte("aws4_request"))
	sig := mac(kSigning, []byte(stringToSign))

	return "AWS4-HMAC-SHA256 Credential=" + creds.AccessKeyID + "/" + scope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + hex.EncodeToString(sig)
}

func TestSignSigV4_AuthorizationHeaderMatchesSpecVector(t *testing.T) {
	body := []byte(`{"hello":"bedrock"}`)
	creds := awsCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "wJalrXUtnFEMI"} //nolint:gosec // fake SigV4 test-vector constant, not a credential
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-west-2.amazonaws.com/model/anthropic.claude-sonnet-4-6-v2:0/invoke", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if err := signSigV4(req, body, creds, "us-west-2", bedrockServiceName, sigv4TestClock); err != nil {
		t.Fatalf("signSigV4: %v", err)
	}

	want := expectedSigV4Authorization(
		http.MethodPost,
		"/model/anthropic.claude-sonnet-4-6-v2:0/invoke",
		"bedrock-runtime.us-west-2.amazonaws.com",
		"application/json",
		body, creds, "us-west-2", bedrockServiceName, sigv4TestClock)

	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization mismatch:\n got: %s\nwant: %s", got, want)
	}
	if got := req.Header.Get("x-amz-date"); got != "20260903T120000Z" {
		t.Errorf("x-amz-date = %q, want 20260903T120000Z", got)
	}
	sum := sha256.Sum256(body)
	if got := req.Header.Get("x-amz-content-sha256"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("x-amz-content-sha256 does not pin the body bytes")
	}
}

func TestSignSigV4_SessionTokenIsSignedAndSent(t *testing.T) {
	body := []byte(`{}`)
	creds := awsCredentials{AccessKeyID: "AKID", SecretAccessKey: "SECRET", SessionToken: "STS-TOKEN-123"} //nolint:gosec // fake SigV4 test-vector constant, not a credential
	req, _ := http.NewRequest(http.MethodPost, "https://bedrock-runtime.eu-central-1.amazonaws.com/model/m/invoke", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if err := signSigV4(req, body, creds, "eu-central-1", bedrockServiceName, sigv4TestClock); err != nil {
		t.Fatalf("signSigV4: %v", err)
	}

	if got := req.Header.Get("x-amz-security-token"); got != "STS-TOKEN-123" {
		t.Errorf("x-amz-security-token = %q, want STS-TOKEN-123", got)
	}
	want := expectedSigV4Authorization(
		http.MethodPost, "/model/m/invoke",
		"bedrock-runtime.eu-central-1.amazonaws.com", "application/json",
		body, creds, "eu-central-1", bedrockServiceName, sigv4TestClock)
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization mismatch with session token:\n got: %s\nwant: %s", got, want)
	}
}

func TestSignSigV4_MissingCredentialsRejected(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/x", nil)
	err := signSigV4(req, nil, awsCredentials{SecretAccessKey: "only-secret"}, "us-east-1", bedrockServiceName, sigv4TestClock)
	if err == nil {
		t.Fatal("expected error for missing access key")
	}
	if !strings.Contains(err.Error(), "credentials") {
		t.Errorf("error should mention credentials, got: %v", err)
	}
}

func TestBedrockRegionResolution(t *testing.T) {
	t.Run("from host", func(t *testing.T) {
		region, err := bedrockRegion("https://bedrock-runtime.us-east-1.amazonaws.com")
		if err != nil || region != "us-east-1" {
			t.Errorf("region=%q err=%v, want us-east-1/nil", region, err)
		}
	})
	t.Run("fips host", func(t *testing.T) {
		region, err := bedrockRegion("https://bedrock-runtime-fips.us-gov-west-1.amazonaws.com")
		if err != nil || region != "us-gov-west-1" {
			t.Errorf("region=%q err=%v, want us-gov-west-1/nil", region, err)
		}
	})
	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("AWS_REGION", "eu-west-1")
		region, err := bedrockRegion("https://custom-bedrock-proxy.internal:8443")
		if err != nil || region != "eu-west-1" {
			t.Errorf("region=%q err=%v, want eu-west-1/nil", region, err)
		}
	})
	t.Run("undeterminable", func(t *testing.T) {
		t.Setenv("AWS_REGION", "")
		t.Setenv("AWS_DEFAULT_REGION", "")
		_, err := bedrockRegion("https://example.internal")
		if err == nil {
			t.Fatal("expected error when region is undeterminable")
		}
	})
}

// bedrockSignedInvokeServer asserts every request an AnthropicClient makes
// against a bedrock-runtime-style endpoint is fully signed and shaped, then
// serves a minimal valid InvokeModel response.
type bedrockSignedInvokeServer struct {
	t       *testing.T
	headers http.Header
	path    string
	body    []byte
	srv     *httptest.Server
}

func newBedrockSignedInvokeServer(t *testing.T) *bedrockSignedInvokeServer {
	t.Helper()
	bs := &bedrockSignedInvokeServer{t: t}
	bs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bs.headers = r.Header.Clone()
		bs.path = r.URL.Path
		bs.body, _ = io.ReadAll(r.Body)

		resp := map[string]any{
			"id":          "msg_brb",
			"type":        "message",
			"role":        "assistant",
			"model":       "anthropic.claude-sonnet-4-6-v2:0",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "aws hello"}},
			"usage":       map[string]any{"input_tokens": 3, "output_tokens": 4},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	t.Cleanup(bs.srv.Close)
	return bs
}

func TestBedrockInvoke_IsSignedEndToEnd(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "")

	bs := newBedrockSignedInvokeServer(t)
	cfg := &ModelConfig{
		ProviderID: ProviderIDBedrock,
		BaseURL:    bs.srv.URL, // host has no region -> env fallback would fail; set it below
		ModelID:    "anthropic.claude-sonnet-4-6-v2:0",
		APIKey:     "ignored-for-bedrock",
	}
	// Region derives from the host via bedrockHostRegionPattern; the test
	// server host has none, so pin it through AWS_REGION.
	t.Setenv("AWS_REGION", "us-east-2")

	client := NewAnthropicClient(cfg)
	resp, err := client.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("empty response content")
	}

	// Auth headers: SigV4 present, Anthropic-native headers absent.
	if auth := bs.headers.Get("Authorization"); auth == "" {
		t.Fatal("request was not signed: no Authorization header")
	} else {
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
			t.Errorf("Authorization scheme = %q", auth)
		}
		if !strings.Contains(auth, "/us-east-2/bedrock/aws4_request") {
			t.Errorf("credential scope missing region/service: %q", auth)
		}
		if !strings.Contains(auth, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date") {
			t.Errorf("SignedHeaders missing required quartet: %q", auth)
		}
	}
	if bs.headers.Get("x-amz-date") == "" {
		t.Error("missing x-amz-date header")
	}
	if bs.headers.Get("x-api-key") != "" {
		t.Error("bedrock request must not carry x-api-key")
	}
	if bs.headers.Get("anthropic-version") != "" {
		t.Error("bedrock request must not carry anthropic-version header")
	}

	// URL shape: /model/{id}/invoke.
	if bs.path != "/model/anthropic.claude-sonnet-4-6-v2:0/invoke" {
		t.Errorf("invoke path = %q", bs.path)
	}

	// Body shape: anthropic_version travels in-band.
	var sent struct {
		AnthropicVersion string `json:"anthropic_version"`
		Model            string `json:"model"`
	}
	if err := json.Unmarshal(bs.body, &sent); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if sent.AnthropicVersion != bedrockAnthropicVersion {
		t.Errorf("anthropic_version = %q, want %q", sent.AnthropicVersion, bedrockAnthropicVersion)
	}
	if sent.Model != "anthropic.claude-sonnet-4-6-v2:0" {
		t.Errorf("model = %q", sent.Model)
	}
}

func TestBedrockMissingCredentialsFailFast(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "us-east-1")

	bs := newBedrockSignedInvokeServer(t)
	cfg := &ModelConfig{
		ProviderID: ProviderIDBedrock,
		BaseURL:    bs.srv.URL,
		ModelID:    "anthropic.claude-sonnet-4-6-v2:0",
	}
	client := NewAnthropicClient(cfg)
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected credential error, got success")
	}
	if !strings.Contains(err.Error(), "AWS credentials") {
		t.Errorf("error should point at AWS credentials, got: %v", err)
	}
}

func TestBedrockEventStreamAdapter_DecodesFramingToSSE(t *testing.T) {
	// Use the union form {"<event-type>": {"bytes": "<base64>"}} — the shape
	// InvokeModelWithResponseStream actually emits. (A bare-payload fixture
	// here flaked: the union-decode path correctly base64-unwraps, and the
	// passthrough fallback for top-level event JSON is order-dependent over
	// a map, so the passthrough branch is not deterministic for multi-key
	// payloads. RoundTrip test below covers the real wire shape end-to-end.)
	inner := []byte(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}`)
	payload := []byte(`{"chunk":{"bytes":` + strconv.Quote(base64.StdEncoding.EncodeToString(inner)) + `}}`)
	framed := bedrockTestFrame(t, ":message-type", "event", ":event-type", "chunk", payload)

	adapter := newBedrockEventStreamAdapter(bytes.NewReader(framed))
	out, err := io.ReadAll(adapter)
	if err != nil {
		t.Fatalf("read adapter: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "event: chunk") {
		t.Errorf("adapter output missing event line: %q", got)
	}
	if !strings.Contains(got, `"content_block_delta"`) {
		t.Errorf("adapter output missing payload json: %q", got)
	}

	// The re-emitted bytes must survive the real SSE scanner's event shape.
	if !strings.HasPrefix(got, "event: chunk\ndata: ") {
		t.Errorf("adapter output is not SSE-shaped: %q", got)
	}
}

func TestBedrockEventStreamAdapter_NonEventFrames(t *testing.T) {
	payload := []byte(`{"ok":true}`)

	t.Run("skips unknown message types", func(t *testing.T) {
		frames := bytes.Join([][]byte{
			bedrockTestFrame(t, ":message-type", "container", "", "", payload),
			bedrockTestFrame(t, ":message-type", "event", ":event-type", "chunk", payload),
		}, nil)

		adapter := newBedrockEventStreamAdapter(bytes.NewReader(frames))
		out, err := io.ReadAll(adapter)
		if err != nil {
			t.Fatalf("read adapter: %v", err)
		}
		if n := bytes.Count(out, []byte("event: ")); n != 1 {
			t.Errorf("expected exactly 1 event line after skipping container frame, got %d in %q", n, out)
		}
	})

	t.Run("surfaces exception frames as errors", func(t *testing.T) {
		frames := bytes.Join([][]byte{
			bedrockTestFrame(t, ":message-type", "event", ":event-type", "chunk", payload),
			bedrockTestFrame(t, ":message-type", "exception", ":exception-type", "ThrottlingException", payload),
		}, nil)

		adapter := newBedrockEventStreamAdapter(bytes.NewReader(frames))
		_, err := io.ReadAll(adapter)
		if err == nil {
			t.Fatal("expected error from exception frame")
		}
		if !strings.Contains(err.Error(), "ThrottlingException") {
			t.Errorf("error should name the exception type, got: %v", err)
		}
	})
}

// bedrockTestFrame encodes one AWS event-stream message with two 7-bit-string
// headers and the given payload (no CRC validation — the decoder skips CRCs).
func bedrockTestFrame(t *testing.T, name1, val1, name2, val2 string, payload []byte) []byte {
	t.Helper()
	headers := buildBedrockTestHeaders(name1, val1, name2, val2)

	total := uint32(12 + len(headers) + len(payload) + 4) //nolint:gosec // G115: fixture sizes are tiny and bounded
	buf := &bytes.Buffer{}
	write := func(b []byte) {
		if _, err := buf.Write(b); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	u32 := make([]byte, 4)
	for _, v := range []uint32{total, uint32(len(headers)), 0 /*prelude CRC, unchecked*/} { //nolint:gosec // G115: fixture sizes are tiny and bounded
		binaryBigEndianPut(u32, v)
		write(u32)
	}
	write(headers)
	write(payload)
	binaryBigEndianPut(u32, 0 /*message CRC, unchecked*/)
	write(u32)
	return buf.Bytes()
}

func buildBedrockTestHeaders(pairs ...string) []byte {
	buf := &bytes.Buffer{}
	for i := 0; i < len(pairs); i += 2 {
		name, val := pairs[i], pairs[i+1] //nolint:gosec // G602: i+1 is always in range for pair-wise iteration
		buf.WriteByte(byte(len(name)))    //nolint:gosec // G115: header names are short constants
		buf.WriteString(name)
		buf.WriteByte(0x07)           // header value type: 7-bit length string
		buf.WriteByte(byte(len(val))) //nolint:gosec // G115: header values are short constants
		buf.WriteString(val)
	}
	return buf.Bytes()
}

func binaryBigEndianPut(dst []byte, v uint32) {
	dst[0] = byte(v >> 24) //nolint:gosec // G115: uint32->byte shifts are lossless by definition
	dst[1] = byte(v >> 16) //nolint:gosec // G115
	dst[2] = byte(v >> 8)  //nolint:gosec // G115
	dst[3] = byte(v)       //nolint:gosec // G115
}

// TestBedrockStreamRoundTripThroughRealParser feeds a full Bedrock
// event-stream transcript (binary framing) through the adapter and the real
// parseStreamingResponse pipeline, asserting the decoded Response — the
// end-to-end proof that streaming Bedrock responses decode correctly.
func TestBedrockStreamRoundTripThroughRealParser(t *testing.T) {
	events := []struct{ et, json string }{
		{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","usage":{"input_tokens":7,"output_tokens":1}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"bedrock says hi"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":5}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	frames := make([][]byte, 0, len(events))
	for _, ev := range events {
		frames = append(frames, bedrockTestFrame(t,
			":message-type", "event", ":event-type", ev.et,
			[]byte(fmt.Sprintf(`{%q:{"bytes":%q}}`, ev.et, base64.StdEncoding.EncodeToString([]byte(ev.json))))))
	}

	adapter := newBedrockEventStreamAdapter(bytes.NewReader(bytes.Join(frames, nil)))
	resp, err := (&AnthropicClient{config: &ModelConfig{ProviderID: ProviderIDBedrock, ModelID: "anthropic.claude-sonnet-4-6-v2:0"}, logger: slog.Default()}).
		parseStreamingResponse(adapter, nil)
	if err != nil {
		t.Fatalf("parseStreamingResponse: %v", err)
	}
	if resp.Content != "bedrock says hi" {
		t.Errorf("content = %q, want %q", resp.Content, "bedrock says hi")
	}
	if resp.Usage.PromptTokens != 7 {
		t.Errorf("input tokens = %d, want 7", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("output tokens = %d, want 5", resp.Usage.CompletionTokens)
	}
}
