package daemon

// Voice-turn clock stamp pins (live defect 2026-11-11: "what time is it?"
// answered "I can't check the current time" — the voice turn prompt carried
// no clock). The loopTurnRunner seam must prepend a short current-date/time
// line to the spoken prompt; the persisted history must keep the raw spoken
// text; and an echoed stamp must trip the spoken-reply guard.
//
// The stamp text is captured END-TO-END: a stub chatter records the full
// message list of every Chat call, so the pin asserts on the user message
// the loop actually sends — not a config-field introspection.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/voice"
)

// clockStampMarker is the prefix of the injected scaffolding line.
const clockStampMarker = "[Context: current date/time is "

// clockChatter records every message of every Chat call and answers with a
// fixed speakable line.
type clockChatter struct {
	mu       sync.Mutex
	messages [][]llm.ChatMessage
	reply    string
}

func (c *clockChatter) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msgs)
	return &llm.Response{Content: c.reply}, nil
}

func (c *clockChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *clockChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{ModelID: "clock-stub"} }

// lastUserMessages returns the user-role contents of the most recent Chat call.
func (c *clockChatter) lastUserMessages() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) == 0 {
		return nil
	}
	var out []string
	for _, m := range c.messages[len(c.messages)-1] {
		if m.Role == llm.RoleUser {
			out = append(out, m.Content)
		}
	}
	return out
}

func clockStampRunner(t *testing.T, reply string) (*loopTurnRunner, *clockChatter, *session.MemoryStore) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	chatter := &clockChatter{reply: reply}
	template := agent.NewAgentLoop("template-voice-clock", t.TempDir(),
		agent.WithLLMChatter(chatter))
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	r := &loopTurnRunner{
		mgr:        mgr,
		template:   template,
		workingDir: t.TempDir(),
		sessions:   sessions,
		logger:     logger,
	}
	return r, chatter, sessions
}

func newVoiceSession(t *testing.T, sessions *session.MemoryStore) voice.SessionHandle {
	t.Helper()
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return voice.SessionHandle{ID: sess.ID, ConversationID: sess.ConversationID}
}

// TestVoiceTurnPromptCarriesClockStamp: the user message that reaches the
// loop must start with the date/time stamp and still carry the raw spoken
// prompt after it.
func TestVoiceTurnPromptCarriesClockStamp(t *testing.T) {
	r, chatter, sessions := clockStampRunner(t, "it's just past two o'clock")
	sess := newVoiceSession(t, sessions)

	const spoken = "what time is it"
	before := time.Now()
	if _, err := r.Run(context.Background(), sess, spoken); err != nil {
		t.Fatalf("Run: %v", err)
	}
	after := time.Now()

	var stamped string
	for _, m := range chatter.lastUserMessages() {
		if strings.Contains(m, clockStampMarker) {
			stamped = m
		}
	}
	if stamped == "" {
		t.Fatalf("voice turn prompt lost the clock stamp; user messages: %#v", chatter.lastUserMessages())
	}
	if !strings.HasPrefix(stamped, clockStampMarker) {
		t.Errorf("stamp must lead the prompt, got %.120q", stamped)
	}
	if !strings.HasSuffix(stamped, spoken) {
		t.Errorf("raw spoken prompt must follow the stamp intact, got %.120q", stamped)
	}

	// The stamp's date must be parseable and current.
	inner := strings.TrimPrefix(stamped, clockStampMarker)
	inner, rest, ok := strings.Cut(inner, "] ")
	if !ok {
		t.Fatalf("unterminated stamp: %.120q", stamped)
	}
	if rest != spoken {
		t.Errorf("text after stamp must be the raw prompt, got %.120q", rest)
	}
	stampTime, err := time.Parse(voiceTurnTimeLayout, inner)
	if err != nil {
		t.Fatalf("stamp date %q is not parseable with the pinned layout: %v", inner, err)
	}
	if stampTime.Before(before.Add(-time.Minute)) || stampTime.After(after.Add(time.Minute)) {
		t.Errorf("stamp time %v is not the current time (turn ran %v..%v)", stampTime, before, after)
	}
}

// TestVoiceTurnHistoryStoresRawPrompt: persistExchange must save the user's
// spoken words, not the model-facing scaffolding line.
func TestVoiceTurnHistoryStoresRawPrompt(t *testing.T) {
	r, _, sessions := clockStampRunner(t, "half past one")
	sess := newVoiceSession(t, sessions)

	if _, err := r.Run(context.Background(), sess, "what time is it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs, err := sessions.GetMessages(sess.ID, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	var user string
	for _, m := range msgs {
		if m.Role == "user" {
			user = m.Content
		}
	}
	if user == "" {
		t.Fatalf("exchange not persisted: %+v", msgs)
	}
	if strings.Contains(user, clockStampMarker) {
		t.Errorf("clock stamp leaked into persisted history: %.120q", user)
	}
	if user != "what time is it" {
		t.Errorf("persisted user message = %.120q, want the raw spoken prompt", user)
	}
}

// TestVoiceTurnClockStampEchoTripsGuard: the stamp is bracketed user-role
// scaffolding — if the model ever echoes it back as the answer, the spoken
// guard must catch it (same posture as the [system: nudge token).
func TestVoiceTurnClockStampEchoTripsGuard(t *testing.T) {
	echoed := voiceTurnContextStamp(time.Now()) + "the time is shown above"
	if _, trips := agent.SpokenReplyViolation(echoed); !trips {
		t.Errorf("an echoed clock stamp must trip the spoken guard; got %.120q", echoed)
	}
	// A clean spoken answer about the time must NOT trip.
	if _, trips := agent.SpokenReplyViolation("it's 12:20 in the afternoon, Mountain time"); trips {
		t.Error("plain spoken time answer must not trip the guard")
	}
}

// TestVoiceTurnContextStampRoundTripsTimeParse: the layout the daemon
// formats with must parse back with time.Parse for every zone Go renders —
// including NUMERIC zone names (FixedZone("+0530")) and LONG IANA names
// (a LoadLocation-shaped FixedZone), which the pre-C-7 layout ("... MST
// (UTC-07:00)") failed because Go's MST element only matches three-letter
// or UTC+-hh zone text.
func TestVoiceTurnContextStampRoundTripsTimeParse(t *testing.T) {
	for _, now := range []time.Time{
		time.Date(2026, 11, 11, 12, 20, 0, 0, time.Local),
		time.Date(2026, 11, 10, 0, 24, 0, 0, time.FixedZone("MST", -7*3600)),
		time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
		// C-7: numeric abbreviation (India-style half-hour offset).
		time.Date(2026, 11, 10, 0, 24, 0, 0, time.FixedZone("+0530", 5*3600+1800)),
		// C-7: long IANA-style zone name (Go renders these verbatim
		// for FixedZone-named locations).
		time.Date(2026, 11, 10, 0, 24, 0, 0, time.FixedZone("America/Denver", -7*3600)),
	} {
		stamp := voiceTurnContextStamp(now)
		inner, _, ok := strings.Cut(strings.TrimPrefix(stamp, clockStampMarker), "] ")
		if !ok {
			t.Fatalf("stamp %q missing closing bracket", stamp)
		}
		parsed, err := time.Parse(voiceTurnTimeLayout, inner)
		if err != nil {
			t.Errorf("stamp %q does not round-trip time.Parse: %v", stamp, err)
			continue
		}
		// The numeric offset must survive the round-trip (the model does
		// timezone arithmetic on it — C-7 keeps the actionable part).
		_, off := parsed.Zone()
		_, wantOff := now.Zone()
		if off != wantOff {
			t.Errorf("stamp %q round-tripped to offset %d, want %d", stamp, off, wantOff)
		}
	}
}

// stampedEmployeeResolver is a minimal EmployeeDefinitionResolver stub that
// answers with one employee carrying the given timezone.
type stampedEmployeeResolver struct {
	tz string
}

func (r stampedEmployeeResolver) GetEmployee(_ context.Context, _ string) (*employee.Employee, error) {
	e := &employee.Employee{}
	e.Timezone = r.tz
	return e, nil
}

// TestVoiceTurnStampUsesEmployeeTimezone pins C-8: the stamp clock is
// resolved in the BOUND EMPLOYEE's timezone (the same zone the "# Moment"
// awareness block renders), falling back scheduler-tz -> time.Local —
// never silently the daemon's clock when the employee carries a zone.
func TestVoiceTurnStampUsesEmployeeTimezone(t *testing.T) {
	// Employee tz: America/Denver = UTC-7; at 2026-11-11 19:00 UTC the
	// employee wall clock is 12:00 (-0700), New York is 14:00 (-0500).
	r, chatter, sessions := clockStampRunner(t, "noon here")
	r.employees = stampedEmployeeResolver{tz: "America/Denver"}
	sess := newVoiceSession(t, sessions)

	utcNow := time.Date(2026, 11, 11, 19, 0, 30, 0, time.UTC)
	stamp := voiceTurnContextStamp(utcNow.In(r.resolveVoiceTurnLocation(context.Background(), "emp-tz")))
	if !strings.Contains(stamp, "(UTC-07:00)") {
		t.Errorf("stamp with America/Denver employee = %q, want the -07:00 employee offset", stamp)
	}

	// End-to-end: a Run with the same resolver renders the employee's
	// offset in the model-facing prompt (now-injected clock, so only the
	// OFFSET can be pinned deterministically).
	if _, err := r.Run(context.Background(), sess, "what time is it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var stamped string
	for _, m := range chatter.lastUserMessages() {
		if strings.Contains(m, clockStampMarker) {
			stamped = m
		}
	}
	if stamped == "" {
		t.Fatalf("voice turn prompt lost the clock stamp: %#v", chatter.lastUserMessages())
	}
	if !strings.Contains(stamped, "(UTC-07:00)") {
		t.Errorf("Run stamp = %.160q, want the employee's (UTC-07:00) offset", stamped)
	}
}

// TestVoiceTurnStampFallsBackToSchedulerTZ pins the middle of the C-8
// fallback chain: an employee with NO timezone falls to the runner's
// schedulerTZ; both empty falls to time.Local.
func TestVoiceTurnStampFallsBackToSchedulerTZ(t *testing.T) {
	r, _, _ := clockStampRunner(t, "x")
	r.employees = stampedEmployeeResolver{tz: ""} // no def zone
	r.schedulerTZ = "Asia/Kolkata"                // UTC+5:30
	loc := r.resolveVoiceTurnLocation(context.Background(), "emp")
	if _, off := time.Now().In(loc).Zone(); off != 5*3600+1800 {
		t.Errorf("fallback location offset = %d, want the scheduler tz +19800", off)
	}
	// Bad scheduler tz + no def zone -> time.Local.
	r.schedulerTZ = "Not/AZone"
	if loc := r.resolveVoiceTurnLocation(context.Background(), "emp"); loc != time.Local {
		t.Errorf("bad scheduler tz must fall to time.Local, got %v", loc)
	}
	// Nil resolver -> time.Local (pre-C-8 posture for anonymous sessions).
	r2, _, _ := clockStampRunner(t, "x")
	if loc := r2.resolveVoiceTurnLocation(context.Background(), ""); loc != time.Local {
		t.Errorf("nil resolver/empty employee must fall to time.Local, got %v", loc)
	}
}
