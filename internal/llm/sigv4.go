package llm

// Minimal AWS Signature Version 4 request signing (stdlib only — no AWS SDK
// dependency). Implements the header-based (Authorization) signing flow
// documented at https://docs.aws.amazon.com/IAM/latest/UserGuide/create-signed-request.html
// for POST JSON payloads with an empty query string, which is all the Bedrock
// runtime invoke endpoints need.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// awsSigV4Algorithm is the signing algorithm identifier AWS expects in
	// the Authorization header and credential scope.
	awsSigV4Algorithm = "AWS4-HMAC-SHA256"
	// awsSigV4Terminator closes the credential scope.
	awsSigV4Terminator = "aws4_request"
	// bedrockServiceName is the SigV4 service token for Amazon Bedrock. The
	// bedrock-runtime REST endpoints sign under "bedrock" even though the
	// host is bedrock-runtime.<region>.amazonaws.com.
	bedrockServiceName = "bedrock"
)

// awsCredentials carries the resolved IAM identity for one signing operation.
// SessionToken is optional (required only for temporary STS credentials).
type awsCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// signSigV4 mutates httpReq in place, adding the x-amz-date,
// x-amz-content-sha256, (optional) x-amz-security-token, and Authorization
// headers that authenticate it against host for service in region at t.
//
// Signed headers are the AWS-standard quartet content-type;host;x-amz-*;*
// computed from the headers actually present on the request at signing time —
// callers must set Content-Type (and any other payload-affecting headers)
// BEFORE calling signSigV4. The body hash pins the exact payload bytes, so
// nothing may mutate the request (URL, headers, or body) afterwards.
func signSigV4(httpReq *http.Request, body []byte, creds awsCredentials, region, service string, t time.Time) error {
	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return fmt.Errorf("sigv4: missing AWS credentials (access key or secret key empty)")
	}

	amzDate := t.UTC().Format("20060102T150405Z")
	dateStamp := t.UTC().Format("20060102")

	// Payload hash: x-amz-content-sha256 pins the body bytes.
	bodyHash := sigv4SHA256Hex(body)

	httpReq.Header.Set("x-amz-date", amzDate)
	httpReq.Header.Set("x-amz-content-sha256", bodyHash)
	if creds.SessionToken != "" {
		httpReq.Header.Set("x-amz-security-token", creds.SessionToken)
	}

	// Canonical headers: lowercase names, trimmed values, sorted by name.
	// Host comes from the URL (Go does not populate req.Host for
	// client-constructed requests); every x-amz-* header we just set is
	// included, matching AWS SDK signing behavior.
	signed := map[string]string{
		"content-type":         httpReq.Header.Get("Content-Type"),
		"host":                 httpReq.URL.Host,
		"x-amz-content-sha256": bodyHash,
		"x-amz-date":           amzDate,
	}
	if creds.SessionToken != "" {
		signed["x-amz-security-token"] = creds.SessionToken
	}
	names := make([]string, 0, len(signed))
	for name := range signed {
		names = append(names, name)
	}
	sort.Strings(names)

	var canonicalHeaders bytes.Buffer
	for _, name := range names {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.TrimSpace(signed[name]))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		httpReq.Method,
		canonicalURI(httpReq),
		"", // canonical query string: Bedrock invoke endpoints take no query
		canonicalHeaders.String(),
		signedHeaders,
		bodyHash,
	}, "\n")

	credentialScope := strings.Join([]string{dateStamp, region, service, awsSigV4Terminator}, "/")
	stringToSign := strings.Join([]string{
		awsSigV4Algorithm,
		amzDate,
		credentialScope,
		sigv4SHA256Hex([]byte(canonicalRequest)),
	}, "\n")

	key := sigv4HMACSHA256(
		sigv4HMACSHA256(
			sigv4HMACSHA256(
				sigv4HMACSHA256([]byte("AWS4"+creds.SecretAccessKey), []byte(dateStamp)),
				[]byte(region)),
			[]byte(service)),
		[]byte(awsSigV4Terminator))
	signature := hex.EncodeToString(sigv4HMACSHA256(key, []byte(stringToSign)))

	httpReq.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		awsSigV4Algorithm, creds.AccessKeyID, credentialScope, signedHeaders, signature))
	return nil
}

// canonicalURI returns the single-URI-encoded request path. Go's URL.EscapedPath
// already percent-encodes each path segment exactly once per RFC 3986 (leaving
// RFC 3986 pchars like ':' intact), which matches SigV4's canonical form.
func canonicalURI(httpReq *http.Request) string {
	p := httpReq.URL.EscapedPath()
	if p == "" {
		return "/"
	}
	return p
}

func sigv4SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sigv4HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
