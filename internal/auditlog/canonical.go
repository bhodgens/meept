// Package auditlog provides the tamper-evident hash-chained audit trail:
// canonical JSON serialization, per-record SHA-256 hashing, prev-hash
// chaining over an append-only SQLite table, and full-chain verification.
//
// Canonicalization is an in-house implementation (sorted keys, no
// insignificant whitespace, UTF-8, standard Go number formatting). It is
// self-consistent within meept but is NOT a full RFC 8785 (JCS)
// implementation — float formatting and HTML-escaping policy differ.
// See docs/plans/tamper-evident-audit-log/OPEN-QUESTIONS.md Q4.
package auditlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// CanonicalJSON renders v as canonical JSON: recursively sorted object keys
// (byte-wise), no insignificant whitespace, UTF-8, HTML characters NOT
// escaped. Supported: map[string]any, []any, string, bool, nil, integer
// types, float64, json.Number. Everything else is an error — callers must
// pre-normalize (e.g. time.Time → string via RFC3339Nano).
func CanonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := appendCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func appendCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		return appendJSONString(buf, x)
	case float64:
		s, err := canonicalFloat(x)
		if err != nil {
			return err
		}
		buf.WriteString(s)
	case json.Number:
		if _, err := x.Float64(); err != nil {
			return fmt.Errorf("canonical json.Number %q: %w", x, err)
		}
		buf.WriteString(x.String())
	case int:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int8:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case uint:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		buf.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(x, 10))
	case []any:
		buf.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := appendCanonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := appendJSONString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := appendCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonical: unsupported type %T", v)
	}
	return nil
}

func appendJSONString(buf *bytes.Buffer, s string) error {
	var sb bytes.Buffer
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("canonical string: %w", err)
	}
	out := sb.Bytes()
	buf.Write(bytes.TrimSuffix(out, []byte("\n"))) // Encoder appends newline
	return nil
}

func canonicalFloat(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("canonical float: non-finite %v", f)
	}
	if f == 0 {
		return "0", nil // normalize -0
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	// 'g' may emit exponents (1e+06); keep — deterministic per Go spec.
	return s, nil
}
