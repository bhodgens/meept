package llm

// AWS Bedrock runtime support for the Anthropic-compatible transport.
//
// The Bedrock Anthropic endpoint (bedrock-runtime.<region>.amazonaws.com)
// differs from the direct Anthropic API in three ways, all handled here:
//
//  1. Authentication is AWS SigV4 (see sigv4.go) instead of x-api-key.
//  2. The request body carries "anthropic_version": "bedrock-2023-05-31"
//     in-band (there is no anthropic-version HTTP header).
//  3. invoke-with-response-stream returns AWS event-stream binary framing
//     (vnd.amazon.eventstream) rather than text/event-stream. The decoder
//     below unwraps that framing and re-emits the inner Anthropic SSE
//     payloads so the existing parseStreamingResponse pipeline works
//     unchanged.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// bedrockAnthropicVersion is the in-body anthropic_version Bedrock expects
// for Anthropic models (the wire equivalent of the direct API's
// anthropic-version header).
const bedrockAnthropicVersion = "bedrock-2023-05-31"

// bedrockAWSCredentials resolves IAM credentials from the standard AWS
// environment variables, mirroring how other meept credential paths read
// config: static keys, with an optional STS session token.
func bedrockAWSCredentials() awsCredentials {
	return awsCredentials{
		AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
	}
}

// bedrockRegion resolves the SigV4 signing region. Priority:
//  1. The region embedded in the BaseURL host
//     (bedrock-runtime.<region>.amazonaws.com).
//  2. AWS_REGION / AWS_DEFAULT_REGION.
//
// Bedrock has no global endpoint, so an undeterminable region is an error.
func bedrockRegion(baseURL string) (string, error) {
	host := baseURL
	if u, err := extractHost(baseURL); err == nil {
		host = u
	}
	if m := bedrockHostRegionPattern.FindStringSubmatch(host); m != nil {
		return m[1], nil
	}
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r, nil
	}
	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r, nil
	}
	return "", fmt.Errorf("bedrock: cannot determine AWS region: BaseURL host %q carries no region and neither AWS_REGION nor AWS_DEFAULT_REGION is set", host)
}

var bedrockHostRegionPattern = regexp.MustCompile(`bedrock(?:-runtime)?(?:-fips)?\.([a-z0-9-]+)\.`)

// extractHost returns the host portion of a URL-ish string without
// importing net/url twice in the hot path; callers pass cfg.BaseURL.
func extractHost(rawURL string) (string, error) {
	u, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	return u.URL.Host, nil
}

// applyBedrockSigV4 signs httpReq for a Bedrock runtime invoke call.
// It resolves credentials and region, folds any configured extra headers
// into the signed header set (non-reserved names only), and signs the
// request in place. Callers must have set Content-Type before calling.
func (c *AnthropicClient) applyBedrockSigV4(httpReq *http.Request, body []byte) error {
	creds := bedrockAWSCredentials()
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return &ClientError{Message: "bedrock: AWS credentials not configured — set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY (and optionally AWS_SESSION_TOKEN)"}
	}
	region, err := bedrockRegion(c.config.BaseURL)
	if err != nil {
		return &ClientError{Message: err.Error()}
	}

	// Fold configured extra headers into the signature. Reserved headers
	// that SigV4 derives itself are excluded (host, x-amz-*), as is
	// authorization. Extra headers are signed here AND re-applied after
	// auth by applyAnthropicExtraHeaders; the same-value re-set leaves the
	// signature valid.
	for k, v := range c.config.ExtraHeaders {
		if v == "" {
			continue
		}
		lk := strings.ToLower(k)
		if lk == "host" || lk == "authorization" || strings.HasPrefix(lk, "x-amz-") || lk == "content-type" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	return signSigV4(httpReq, body, creds, region, bedrockServiceName, time.Now())
}

// hasBedrockEventStreamBody reports whether an HTTP response body carrying
// the AWS event-stream content type should be unwrapped before SSE parsing.
func hasBedrockEventStreamBody(h http.Header) bool {
	ct := strings.ToLower(h.Get("Content-Type"))
	return strings.HasPrefix(ct, "application/vnd.amazon.eventstream") ||
		strings.HasPrefix(ct, "application/vnd.amazon.eventstream-json")
}

// bedrockEventStreamAdapter wraps a Bedrock event-stream body and re-emits
// each decoded :message-type/event payload as a line pair that the existing
// SSE scanner understands ("event: X" + "data: {json}").
type bedrockEventStreamAdapter struct {
	src     io.Reader
	r       *bufio.Reader
	pending []byte // buffered SSE-shaped bytes not yet consumed
	eof     bool
}

func newBedrockEventStreamAdapter(r io.Reader) *bedrockEventStreamAdapter {
	return &bedrockEventStreamAdapter{src: r, r: bufio.NewReaderSize(r, 64*1024)}
}

// Read implements io.Reader over the decoded SSE-shaped byte stream.
func (a *bedrockEventStreamAdapter) Read(p []byte) (int, error) {
	for len(a.pending) == 0 {
		if a.eof {
			return 0, io.EOF
		}
		env, err := readBedrockEventFrame(a.r)
		if errors.Is(err, io.EOF) {
			a.eof = true
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		payload, isEvent, err := decodeBedrockEventPayload(env)
		if err != nil {
			return 0, &ClientError{Message: "bedrock: malformed event-stream frame", Cause: err}
		}
		if !isEvent {
			continue // :message-type error/exception frames surface via payload-less skip; terminal error handled by parser
		}
		a.pending = payload
	}
	n := copy(p, a.pending)
	a.pending = a.pending[n:]
	return n, nil
}

// bedrockEventFrame is one decoded AWS event-stream message.
type bedrockEventFrame struct {
	headers map[string][]byte
	payload []byte
}

var (
	bedrockHeaderMessageType = []byte(":message-type")
	bedrockHeaderEventType   = []byte(":event-type")
	// bedrockHeaderExceptionType names the failure on :message-type
	// exception frames (per the AWS event-stream spec).
	bedrockHeaderExceptionType = []byte(":exception-type")
)

func readBedrockEventFrame(r *bufio.Reader) (*bedrockEventFrame, error) {
	// Preamble per AWS event-stream encoding: total length (4, big-endian),
	// headers length (4), prelude CRC (4); then headers, payload, message CRC.
	head := make([]byte, 12)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err // includes clean io.EOF
	}
	total := binary.BigEndian.Uint32(head[0:4])
	hdrLen := binary.BigEndian.Uint32(head[4:8])
	const preludeLen = 12
	const messageCRCLen = 4
	// Minimum frame: prelude + empty headers + empty payload + message CRC.
	if total < preludeLen+messageCRCLen || uint64(hdrLen) > uint64(total-preludeLen-messageCRCLen) {
		return nil, fmt.Errorf("invalid frame lengths (total=%d headers=%d)", total, hdrLen)
	}
	// Remaining bytes after the prelude: headers + payload + message CRC.
	// All are consumed so the next frame starts aligned.
	body := make([]byte, total-preludeLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("truncated frame body: %w", err)
	}
	hdrs := parseBedrockHeaders(body[:hdrLen])
	payload := body[hdrLen : len(body)-messageCRCLen]
	return &bedrockEventFrame{headers: hdrs, payload: payload}, nil
}

// parseBedrockHeaders decodes the header block of one event-stream frame.
// Unsupported header value types are skipped (the two headers this decoder
// needs — :message-type and :event-type — are 7-bit strings).
func parseBedrockHeaders(b []byte) map[string][]byte {
	hdrs := make(map[string][]byte)
	for len(b) > 0 {
		nameLen := int(b[0])
		b = b[1:]
		if nameLen == 0 || nameLen > len(b) {
			return hdrs // malformed; keep what we have
		}
		name := b[:nameLen]
		b = b[nameLen:]
		if len(b) == 0 {
			return hdrs
		}
		vt := b[0] // header value type
		b = b[1:]
		var val []byte
		switch vt {
		case 0x07: // 7-bit length string
			if len(b) == 0 {
				return hdrs
			}
			vl := int(b[0])
			b = b[1:]
			if vl > len(b) {
				return hdrs
			}
			val, b = b[:vl], b[vl:]
		case 0x04: // 16-bit signed int
			if len(b) < 2 {
				return hdrs
			}
			b = b[2:]
		case 0x05: // 64-bit signed int
			if len(b) < 8 {
				return hdrs
			}
			b = b[8:]
		case 0x06: // 64-bit float
			if len(b) < 8 {
				return hdrs
			}
			b = b[8:]
		case 0x00: // true
		case 0x01: // false
		case 0x02: // byte
			if len(b) < 1 {
				return hdrs
			}
			b = b[1:]
		case 0x03: // 16-bit short
			if len(b) < 2 {
				return hdrs
			}
			b = b[2:]
		default:
			return hdrs // unknown type; stop parsing headers
		}
		hdrs[string(name)] = val
	}
	return hdrs
}

// decodeBedrockEventPayload converts one frame into SSE-shaped bytes
// ("event: <type>\ndata: <json>\n\n").
//
// Bedrock's InvokeModelWithResponseStream wraps each Anthropic event as the
// event-stream union form {"<event-type>": {"bytes": "<base64 json>"}} (the
// base64 decode is what the AWS SDKs do for the `blob` union member). A
// passthrough is kept for payload shapes that already carry the event JSON
// at the top level.
func decodeBedrockEventPayload(env *bedrockEventFrame) ([]byte, bool, error) {
	mt := string(env.headers[string(bedrockHeaderMessageType)])
	switch mt {
	case "event":
		// fall through to decoding below
	case "exception", "error":
		// Exception frames name the failure via :exception-type.
		name := string(env.headers[string(bedrockHeaderExceptionType)])
		if name == "" {
			name = string(env.headers[string(bedrockHeaderEventType)])
		}
		return nil, false, fmt.Errorf("bedrock: stream exception %s: %s", name, string(env.payload))
	default:
		return nil, false, nil // :message-type container/other — skip
	}
	et := string(env.headers[string(bedrockHeaderEventType)])

	var union map[string]json.RawMessage
	if err := json.Unmarshal(env.payload, &union); err != nil {
		return nil, false, fmt.Errorf("payload json: %w", err)
	}

	// Preferred shape: {"<event-type>": {"bytes": "<base64>"}}.
	if member, ok := union[et]; ok {
		var blob struct {
			Bytes string `json:"bytes"`
		}
		if err := json.Unmarshal(member, &blob); err == nil && blob.Bytes != "" {
			decoded, derr := base64Decode(blob.Bytes)
			if derr != nil {
				return nil, false, fmt.Errorf("event %s bytes base64: %w", et, derr)
			}
			return sseFrameBytes(et, decoded), true, nil
		}
	}

	// Passthrough: the union's single member is already the event JSON, or
	// a member named after the event type holds the JSON directly. The
	// unnamed fallback prefers the lexicographically SMALLEST member name so
	// multi-member payloads decode deterministically (Go map iteration order
	// is randomized; picking "whichever iterates first" flaked ~1-in-8 on
	// multi-key payloads).
	if len(union) > 0 {
		if member, ok := union[et]; ok && looksLikeJSON(member) {
			return sseFrameBytes(et, member), true, nil
		}
		firstName := ""
		var firstMember json.RawMessage
		for name, member := range union {
			if !looksLikeJSON(member) {
				continue
			}
			if firstName == "" || name < firstName {
				firstName, firstMember = name, member
			}
		}
		if firstName != "" {
			return sseFrameBytes(et, firstMember), true, nil
		}
	}
	return nil, false, fmt.Errorf("bedrock: event %s payload has no decodable member", et)
}

// sseFrameBytes renders one decoded event as SSE-shaped bytes the existing
// scanner consumes: "event: <type>\ndata: <json>\n\n".
func sseFrameBytes(eventType string, data []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(eventType)
	buf.WriteString("\ndata: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}

// looksLikeJSON reports whether b starts like a JSON value. The scanner
// downstream tolerates junk events, but rejecting non-JSON here surfaces
// framing mistakes early with a useful error.
func looksLikeJSON(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		case '{', '[', '"', 't', 'f', 'n', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			return true
		default:
			return false
		}
	}
	return false
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
