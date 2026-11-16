package daemon

// Voice-persona fix pins: the voice turn runner MUST stamp the bound
// employee's definition (persona prompt + constitution constraints) onto
// the session loop before every turn. Regression context: a live call's
// reply came back as a 40-line markdown analysis although the companion
// employee's prompt forbids markdown - loopTurnRunner cloned the daemon
// template loop and never applied the employee definition, so the turn
// ran the daemon default persona (SOUL.md + DefaultConstitution).
//
// The tests capture the system prompt the loop actually sends: a stub
// chatter records messages[0] (the conversation's system prompt slot,
// Conversation.buildMessageList), so the pin is END-TO-END through
// RunOnce - not a config-field introspection.

import (
	"context"
	"path/filepath"
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

// personaChatter records the FIRST message (the system prompt slot) of
// every Chat call and answers with a fixed spoken-style line.
type personaChatter struct {
	mu      sync.Mutex
	systems []string
	reply   string
}

func (c *personaChatter) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(msgs) > 0 {
		c.systems = append(c.systems, msgs[0].Content)
	}
	return &llm.Response{Content: c.reply}, nil
}

func (c *personaChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *personaChatter) Config() *llm.ModelConfig { return &llm.ModelConfig{ModelID: "persona-stub"} }

// lastSystem returns the most recently recorded system prompt.
func (c *personaChatter) lastSystem() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.systems) == 0 {
		return ""
	}
	return c.systems[len(c.systems)-1]
}

// fakeEmployeeResolver resolves canned employee definitions; unknown ids
// return employee.ErrEmployeeNotFound (the manager's contract).
type fakeEmployeeResolver struct {
	byID map[string]*employee.Employee
}

func (f *fakeEmployeeResolver) GetEmployee(_ context.Context, id string) (*employee.Employee, error) {
	if def, ok := f.byID[id]; ok {
		return def, nil
	}
	return nil, employee.ErrEmployeeNotFound
}

// companionDefinition mirrors companion-employee.json5's load shape: a
// spoken-word prompt forbidding markdown plus a constitution with charter.
func companionDefinition() *employee.Employee {
	def := &employee.Employee{}
	def.ID = "companion"
	def.Prompt = "You are Companion. Speak for the ear: short sentences, plain words. Never use markdown, bullet lists, or code blocks."
	def.Constitution = employee.Constitution{
		Purpose:      "Give the operator a natural spoken conversation partner at the desk",
		Role:         "Desk Companion",
		Charter:      "Converse over the local voice leg. Speak plainly and briefly - your output is spoken aloud.",
		AutonomyTier: employee.Tier1Reactive,
		ApprovedAt:   time.Now(),
	}
	return def
}

// personaTurnRunnerWithCapture builds a loopTurnRunner whose template
// loop chats through the returned recorder: every RunOnce system prompt
// lands in the recorder (ConfigSnapshot clones the template's chatter
// pointer onto the session loop, so clone turns record through the same
// stub).
func personaTurnRunnerWithCapture(t *testing.T, resolver EmployeeDefinitionResolver) (*loopTurnRunner, *personaChatter) {
	t.Helper()
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	chatter := &personaChatter{reply: "spoken reply"}
	template := agent.NewAgentLoop("template-voice-persona", t.TempDir(),
		agent.WithLLMChatter(chatter))
	mgr := agent.NewManager(agent.ManagerConfig{SessionStore: sessions})
	r := &loopTurnRunner{
		mgr:        mgr,
		template:   template,
		workingDir: t.TempDir(),
		sessions:   sessions,
		employees:  resolver,
		logger:     logger,
	}
	return r, chatter
}

func TestVoiceTurnUsesBoundEmployeePersona(t *testing.T) {
	resolver := &fakeEmployeeResolver{byID: map[string]*employee.Employee{
		"companion": companionDefinition(),
	}}
	r, chatter := personaTurnRunnerWithCapture(t, resolver)

	sessions := session.NewMemoryStore(testLogger(t))
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	reply, err := r.Run(context.Background(), voice.SessionHandle{
		ID:             sess.ID,
		ConversationID: sess.ConversationID,
		EmployeeID:     "companion",
	}, "analyze this repository structure for me in detail")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply == "" {
		t.Fatal("empty reply")
	}

	sys := chatter.lastSystem()
	if !strings.Contains(sys, "You are Companion") {
		t.Errorf("voice turn system prompt missing the employee persona prompt; got %.200q", sys)
	}
	if !strings.Contains(sys, "Never use markdown") {
		t.Errorf("voice turn system prompt missing the employee's markdown prohibition; got %.200q", sys)
	}
	if !strings.Contains(sys, "Desk Companion") {
		t.Errorf("voice turn system prompt missing the constitution role (SynthesizedPrompt); got %.200q", sys)
	}
}

func TestVoiceTurnWithoutBindingKeepsTemplatePersona(t *testing.T) {
	// Nil resolver = wiring degraded (no employee framework): the turn
	// must still run, on the template persona.
	r, chatter := personaTurnRunnerWithCapture(t, nil)

	sessions := session.NewMemoryStore(testLogger(t))
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := r.Run(context.Background(), voice.SessionHandle{
		ID:             sess.ID,
		ConversationID: sess.ConversationID,
	}, "hello"); err != nil {
		t.Fatalf("Run with nil resolver: %v", err)
	}
	sys := chatter.lastSystem()
	if strings.Contains(sys, "You are Companion") {
		t.Error("unbound turn must NOT carry the employee persona")
	}
}

func TestVoiceTurnUnknownEmployeeDegradesCleanly(t *testing.T) {
	// An employee id with no definition: the call continues on the
	// template persona (never dropped) - the never-park posture.
	resolver := &fakeEmployeeResolver{byID: map[string]*employee.Employee{}}
	r, chatter := personaTurnRunnerWithCapture(t, resolver)

	sessions := session.NewMemoryStore(testLogger(t))
	sess, err := sessions.Create("voice call")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := r.Run(context.Background(), voice.SessionHandle{
		ID:             sess.ID,
		ConversationID: sess.ConversationID,
		EmployeeID:     "ghost",
	}, "hello"); err != nil {
		t.Fatalf("Run with unknown employee must degrade, not fail: %v", err)
	}
	if strings.Contains(chatter.lastSystem(), "You are Companion") {
		t.Error("unknown-employee turn must not carry an unresolvable persona")
	}
}

func TestVoiceBindingCarriesEmployeeIDOnHandle(t *testing.T) {
	// The binding must propagate the call's bound employee id onto the
	// SessionHandle it hands the turn runner - that id is what selects
	// the persona. Runs the REAL Binding over a fake TurnRunner.
	logger := testLogger(t)
	sessions := session.NewMemoryStore(logger)
	store, err := voice.NewCallStore(filepath.Join(t.TempDir(), "calls.db"), logger)
	if err != nil {
		t.Fatalf("call store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	captured := make(chan voice.SessionHandle, 4)
	binding, err := voice.NewBinding(voice.BindingDeps{
		Calls:    store,
		Sessions: fakeVoiceSessions{store: sessions},
		Turns: captureRunner{run: func(_ context.Context, sess voice.SessionHandle, _ string) (string, error) {
			captured <- sess
			return "ok", nil
		}},
		Progress: noopProgressPublisher{},
		Logger:   logger,
	})
	if err != nil {
		t.Fatalf("NewBinding: %v", err)
	}
	if _, err := binding.OnCallStart(context.Background(), "cc-p", "local:desk", "companion", "local", voice.DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}
	if _, err := binding.RunTurn(context.Background(), "cc-p", "hello"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	select {
	case sess := <-captured:
		if sess.EmployeeID != "companion" {
			t.Errorf("SessionHandle.EmployeeID = %q; want companion (the persona selector)", sess.EmployeeID)
		}
	default:
		t.Fatal("turn runner was never invoked")
	}
}

// --- small fakes ---

// noopProgressPublisher satisfies voice.ProgressPublisher (drops events).
type noopProgressPublisher struct{}

func (noopProgressPublisher) PublishProgress(voice.ProgressEvent) {}

type fakeVoiceSessions struct{ store *session.MemoryStore }

func (f fakeVoiceSessions) Create(name string) (voice.SessionHandle, error) {
	s, err := f.store.Create(name)
	if err != nil {
		return voice.SessionHandle{}, err
	}
	return voice.SessionHandle{ID: s.ID, ConversationID: s.ConversationID}, nil
}

func (f fakeVoiceSessions) List() ([]voice.NamedSession, error) {
	all, err := f.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]voice.NamedSession, 0, len(all))
	for _, s := range all {
		if s == nil {
			continue
		}
		out = append(out, voice.NamedSession{
			ID: s.ID, Name: s.Name, ConversationID: s.ConversationID, EmployeeID: s.EmployeeID,
		})
	}
	return out, nil
}

func (f fakeVoiceSessions) UpdateName(sessionID, name string) error {
	return f.store.UpdateName(sessionID, name)
}

func (f fakeVoiceSessions) SetEmployeeID(sessionID, employeeID string) error {
	return f.store.SetEmployeeID(sessionID, employeeID)
}

type captureRunner struct {
	run func(context.Context, voice.SessionHandle, string) (string, error)
}

func (c captureRunner) Run(ctx context.Context, sess voice.SessionHandle, prompt string) (string, error) {
	return c.run(ctx, sess, prompt)
}
