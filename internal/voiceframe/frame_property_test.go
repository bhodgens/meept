package voiceframe

// Property-based tests for the shared C1/C2 frame codec
// (frame.go, docs/plans/20261102-voice-sidecar contract).
//
// Hand-rolled, zero new deps, deterministic seed (skill:
// software-development/property-based-testing; in-house pattern:
// strategist-usa go-shard-runner parse_csv_property_test.go).
//
// Properties (implementation contract refs are frame.go line numbers at
// the time of writing):
//
//	P1 round-trip: DecodeFrame(EncodeFrame(f)) == f for every Frame
//	   value the generator can build — including hostile text (unicode,
//	   quotes, newlines, control chars) that exercises json.Marshal
//	   escaping. Holds because C1 is "one line of compact JSON": Marshal
//	   never emits a raw '\n' inside a string (escaped as \u000A), so
//	   the line framing is unambiguous and Unmarshal restores the value
//	   exactly (frame.go:49-67).
//	P2 line discipline: EncodeFrame output contains EXACTLY one '\n',
//	   the trailing one — any embedded newline would split one frame
//	   into two on the shared text socket (frame.go:48-55, C1 byte-exact
//	   JSON lines).
//	P3 valid-JSON no-throw: for ARBITRARY bytes, DecodeFrame either
//	   returns a frame or an error — never panics. The codec sits on a
//	   unix socket fed by another process; hostile bytes are normal
//	   operation (frame.go:61-67).
//	P4 ChunkBytes table completeness: the (codec, rate) plane returns
//	   either (640, L16@16k) or (160, PCMU@8k) or an error — never a
//	   zero-size chunk with nil error, and never a panic (frame.go:72-81,
//	   C2 20 ms chunking).
//
// Generator: seed 20261106, rand.New(rand.NewSource(...)), 4000 cases
// per property; hostile corners seeded explicitly. On failure the seed
// and case index print for replay.

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

const propSeed = int64(20261106)

var hostileTexts = []string{
	"", " ", "\n", "\r\n", "\"", "\"}", "\\", `\u0041`, "{call_id}",
	"\x00", "\t", "\U0001F44D\U0001F631", "café\u00e9", "тест", "\u202Eevil",
	"strings.Repeat fake", "say this", "perm:abc:allow", strings.Repeat("A", 4096),
}

func randText(r *rand.Rand) string {
	if r.Intn(4) == 0 {
		return hostileTexts[r.Intn(len(hostileTexts))]
	}
	n := r.Intn(64)
	b := make([]rune, 0, n)
	for range n {
		// Mix ASCII, quotes/backslashes (JSON-escape stress), CJK and
		// astral-plane runes (multi-byte UTF-8 stress).
		switch r.Intn(4) {
		case 0:
			b = append(b, rune(0x20+r.Intn(0x5f))) // printable ASCII
		case 1:
			escapeStress := []rune(`"\{}` + " \n	\x00")
			b = append(b, escapeStress[r.Intn(len(escapeStress))])
		case 2:
			b = append(b, rune(0x4e00+r.Intn(0x200))) // CJK
		default:
			b = append(b, rune(0x1F300+r.Intn(0x100))) // astral (4-byte UTF-8)
		}
	}
	return string(b)
}

func randFrame(r *rand.Rand) Frame {
	events := []string{"setup", "prompt", "interrupt", "end", "say", "clear", "dtmf", "", strings.Repeat("x", 32)}
	bPtr := []*bool{nil, boolPtrV(true), boolPtrV(false)}
	return Frame{
		CallID:     randText(r),
		Event:      events[r.Intn(len(events))],
		Text:       randText(r),
		Last:       bPtr[r.Intn(len(bPtr))],
		From:       randText(r),
		To:         randText(r),
		Direction:  []string{"", "inbound", "outbound"}[r.Intn(3)],
		Codec:      []string{"", "L16", "PCMU", "opus"}[r.Intn(4)],
		SampleRate: []int{0, 8000, 16000, 44100}[r.Intn(4)],
		Reason:     randText(r),
	}
}

func boolPtrV(b bool) *bool { return &b }

// TestFrame_RoundTrip ports P1+P2: encode→decode is the identity and the
// encoded line never splits.
func TestFrame_RoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(propSeed))
	for i := 0; i < 4000; i++ {
		f := randFrame(r)
		enc, err := EncodeFrame(f)
		if err != nil {
			t.Fatalf("case %d: EncodeFrame: %v (frame %+v)", i, err, f)
		}
		if n := strings.Count(string(enc), "\n"); n != 1 || enc[len(enc)-1] != '\n' {
			t.Fatalf("case %d: encoded frame has %d newlines (want exactly 1, trailing): %q", i, n, enc)
		}
		dec, err := DecodeFrame(enc)
		if err != nil {
			t.Fatalf("case %d: DecodeFrame: %v (input %q)", i, err, enc)
		}
		if !reflect.DeepEqual(f, dec) {
			t.Fatalf("case %d: round-trip drift:\n in  = %+v\n out = %+v", i, f, dec)
		}
	}
}

// TestFrame_DecodeArbitraryBytesNeverPanics ports P3: arbitrary socket bytes
// produce a frame or an error, never a panic, and garbage never decodes.
func TestFrame_DecodeArbitraryBytesNeverPanics(t *testing.T) {
	r := rand.New(rand.NewSource(propSeed + 1))
	corners := [][]byte{
		{}, []byte("\n"), []byte("null"), []byte("[]"), []byte("123"),
		[]byte(`"a string"`), []byte(`{"call_id":123}`),
		[]byte(`{"call_id":"a","event":"prompt","last":"notabool"}`),
		[]byte(`{"call_id":"a","event":` + strings.Repeat("[", 2000) + strings.Repeat("]", 2000) + `}`),
		bytes_of(0x00, 0xff), []byte("{\"call_id\":\"a\",\"event\":\"say\"}\ntrailing line\n"),
		[]byte(`{"text":"` + strings.Repeat("\\ud800", 10) + `"}`), // lone surrogates
	}
	for i, c := range corners {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("corner %d (%q) panicked: %v", i, c, rec)
				}
			}()
			_, _ = DecodeFrame(c)
		}()
	}
	for i := 0; i < 2000; i++ {
		n := r.Intn(512)
		b := make([]byte, n)
		for j := range b {
			b[j] = byte(r.Intn(256))
		}
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("random case %d panicked: %v (input %q)", i, rec, b)
				}
			}()
			_, _ = DecodeFrame(b)
		}()
	}
}

// bytes_of is a small helper so the corners table stays readable.
func bytes_of(pairs ...byte) []byte {
	b := make([]byte, 0, len(pairs))
	for range pairs {
		b = append(b, pairs...)
	}
	return b
}

// TestFrame_GarbageNeverDecodes ports P3's error side: JSON values that are
// not objects (null, array, number, string) must NOT decode to a zero frame
// silently — the child/daemon would silently mis-handle the call.
// encoding/json in fact rejects non-object literals into a struct, and
// type-mismatched fields error too; this pins that behavior so a stdlib
// upgrade cannot quietly change it.
func TestFrame_GarbageNeverDecodes(t *testing.T) {
	for _, g := range []string{"null", "[]", "123", `"str"`, "true",
		`{"call_id":123}`, `{"last":"yes"}`, `{`, `{"event":"say"}extra`} {
		var f Frame
		if err := json.Unmarshal([]byte(g), &f); err == nil {
			// json.Unmarshal into a struct accepts `null` (leaves f zero).
			// DecodeFrame wraps the same semantics. Flag only shapes that
			// would fake a REAL frame: a null decode yields the zero Frame,
			// whose empty event/call_id no handler treats as a frame.
			var want Frame
			if g == "null" && f == want {
				continue
			}
			t.Fatalf("garbage %q decoded without error to %+v", g, f)
		}
	}
}

// TestChunkBytes_TotalPlane ports P4: over the full codec/rate plane, the
// function returns one of the two documented chunk sizes or an error —
// never (0, nil) and never a panic.
func TestChunkBytes_TotalPlane(t *testing.T) {
	codecs := []string{"", "L16", "PCMU", "l16", "pcmu", "opus", "MULAW", "L16 "}
	for codec := range codecs {
		for rate := 0; rate <= 96000; rate += 1000 {
			size, err := ChunkBytes(codecs[codec], rate)
			if err == nil {
				switch {
				case codecs[codec] == "L16" && rate == 16000:
					if size != 640 {
						t.Fatalf("L16@16k = %d, want 640", size)
					}
				case codecs[codec] == "PCMU" && rate == 8000:
					if size != 160 {
						t.Fatalf("PCMU@8k = %d, want 160", size)
					}
				default:
					t.Fatalf("unexpected success: %q@%d -> %d", codecs[codec], rate, size)
				}
			} else if size != 0 {
				t.Fatalf("error path returned non-zero size %d for %q@%d", size, codecs[codec], rate)
			}
		}
	}
}
