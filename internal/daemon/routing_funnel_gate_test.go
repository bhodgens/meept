package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/robfig/cron/v3"
)

// gateRecordingChatter is the loop double for C2/C3/C4. It records every
// message the turn actually sent and returns a fixed usage total.
// employeeBoundAgentLoopExecutor calls clone.RunOnce and drops systemPrompt,
// so the prefix never appears here.
type gateRecordingChatter struct {
	mu       sync.Mutex
	contents []string
	reply    string
	usage    llm.TokenUsage
}

func (c *gateRecordingChatter) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range msgs {
		c.contents = append(c.contents, m.Content)
	}
	return &llm.Response{Content: c.reply, Usage: c.usage}, nil
}

func (c *gateRecordingChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *gateRecordingChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "routing-funnel-stub"}
}

func (c *gateRecordingChatter) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.contents, "\n")
}

func newBoundExecutor(t *testing.T, employeeID string, chatter *gateRecordingChatter, hook func(*agent.AgentLoop)) *employeeBoundAgentLoopExecutor {
	t.Helper()
	template := agent.NewAgentLoop("routing-funnel-template", t.TempDir(), agent.WithLLMChatter(chatter))
	if template == nil {
		t.Fatal("NewAgentLoop returned nil")
	}
	return &employeeBoundAgentLoopExecutor{
		inner: &agentLoopBotExecutorAdapter{
			agentLoop: template,
			logger:    slog.New(slog.DiscardHandler),
		},
		employeeID:   employeeID,
		constitution: employee.Constitution{Purpose: "answer"},
		cloneHook:    hook,
	}
}

// TestExecuteBotPassesSystemPrefix pins C2: ExecuteBot must pass systemPrompt
// into the per-turn clone before RunOnce. The recording double is the chatter
// the clone actually calls. RED today because the adapter drops systemPrompt.
func TestExecuteBotPassesSystemPrefix(t *testing.T) {
	const prefix = "ROUTING-FUNNEL-C2-PREFIX"
	chatter := &gateRecordingChatter{reply: "ok"}
	bound := newBoundExecutor(t, "secretary", chatter, nil)

	_, _, err := bound.ExecuteBot(context.Background(), prefix, "do the thing")
	if !strings.Contains(chatter.joined(), prefix) {
		t.Fatalf("recording double did not receive system prefix %q (ExecuteBot dropped systemPrompt before RunOnce)", prefix)
	}
	if err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
}

// TestExecuteBotReturnsLastTurnTokens pins C3: ExecuteBot's tokensUsed is the
// turn's provider usage, not a hardcoded 0. The stub reports usage 17.
func TestExecuteBotReturnsLastTurnTokens(t *testing.T) {
	chatter := &gateRecordingChatter{
		reply: "ok",
		usage: llm.TokenUsage{PromptTokens: 8, CompletionTokens: 9, TotalTokens: 17},
	}
	bound := newBoundExecutor(t, "secretary", chatter, nil)

	_, tokens, err := bound.ExecuteBot(context.Background(), "system", "do the thing")
	if tokens != 17 {
		t.Fatalf("ExecuteBot tokensUsed = %d, want 17", tokens)
	}
	if err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
}

// TestEmployeeTurnUsesStandingSession pins C4: the conversation id passed
// into RunOnce is "emp-sess-" plus the employee id. The clone is built with
// that same id (employeeTurnClone) and RunOnce is called with it; cloneHook
// snapshots SessionID before RunOnce. RED today because the adapter calls
// id.Generate("bot-exec").
func TestEmployeeTurnUsesStandingSession(t *testing.T) {
	const employeeID = "coder-7"
	want := "emp-sess-" + employeeID
	var (
		hooked bool
		gotID  string
	)
	chatter := &gateRecordingChatter{reply: "ok"}
	bound := newBoundExecutor(t, employeeID, chatter, func(cl *agent.AgentLoop) {
		hooked = true
		gotID = cl.SessionID()
	})

	_, _, err := bound.ExecuteBot(context.Background(), "system", "do the thing")
	if !hooked {
		t.Fatal("cloneHook was not called; cannot observe the RunOnce conversation id")
	}
	if gotID != want {
		t.Fatalf("conversation id passed into RunOnce = %q, want %q", gotID, want)
	}
	if err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
}

// cronSourceReflector records the trigger source Assess renders into the
// user prompt ("**source:** <source>"). Manager.Trigger is the only path
// that reaches it.
type cronSourceReflector struct {
	mu      sync.Mutex
	sources []string
}

func (r *cronSourceReflector) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range msgs {
		const mark = "**source:** "
		if i := strings.Index(m.Content, mark); i >= 0 {
			rest := m.Content[i+len(mark):]
			line, _, _ := strings.Cut(rest, "\n")
			r.sources = append(r.sources, strings.TrimSpace(line))
		}
	}
	return &llm.Response{Content: `{"candidates":[]}`}, nil
}

func (r *cronSourceReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return r.Chat(ctx, msgs, opts...)
}

func (r *cronSourceReflector) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "cron-source-stub"}
}

func (r *cronSourceReflector) source() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sources) == 0 {
		return ""
	}
	return r.sources[len(r.sources)-1]
}

// TestCronTriggerCallsManager pins C6: an enabled cron trigger calls
// employee.Manager.Trigger with source "cron".
//
// Missing callback: bot.Manager.startBot only calls router.Register, and
// EventActionRouter.Register ignores every trigger that is not an enabled
// bus_event. There is no cron registration callback and no scheduler job
// to fire. Leaf 05 owns adding that callback. This test still registers
// the row, starts the bot, and invokes any cron callback it can see.
func TestCronTriggerCallsManager(t *testing.T) {
	const botID = "nightly"
	ctx := context.Background()

	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store close: %v", err)
		}
	})

	msgBus := bus.New(nil, nil)
	t.Cleanup(func() { msgBus.Close() })
	router := bot.NewEventActionRouter(msgBus, nil)
	botMgr := bot.NewManager(store, router)

	rec := &cronSourceReflector{}
	loop := employee.NewGoalLoop(botID, &employee.Constitution{Purpose: "nightly sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(rec)
	empMgr := employee.NewManager(botMgr)
	empMgr.RegisterGoalLoop(botID, loop)

	def := bot.BotDefinition{
		ID:      botID,
		Name:    "nightly",
		Prompt:  "summarize the day",
		Enabled: true,
		Triggers: []bot.BotTrigger{{
			Type:           bot.TriggerTypeCron,
			Schedule:       "0 4 * * *",
			PromptTemplate: "run the nightly sweep",
			Enabled:        true,
		}},
	}
	if err := botMgr.CreateBot(ctx, def); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := botMgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if !fireCronRegistrationCallback(botMgr, router, empMgr) {
		t.Log("missing callback: no cron registration callback on bot.Manager, EventActionRouter, or employee.Manager; startBot does not schedule cron triggers")
	}

	if got := rec.source(); got != "cron" {
		t.Fatalf("Trigger source = %q, want %q", got, "cron")
	}
}

// fireCronRegistrationCallback invokes an exported cron callback or a
// *cron.Cron job if one is visible. Unexported fields of another package
// are not readable from here. Returns false when nothing was fired.
func fireCronRegistrationCallback(targets ...any) bool {
	fired := false
	for _, target := range targets {
		if target == nil {
			continue
		}
		v := reflect.ValueOf(target)
		for i := 0; i < v.NumMethod(); i++ {
			m := v.Type().Method(i)
			if !strings.Contains(strings.ToLower(m.Name), "cron") {
				continue
			}
			if invokeZeroOrContext(v.Method(i)) {
				fired = true
			}
		}
		ev := v
		if ev.Kind() == reflect.Pointer {
			if ev.IsNil() {
				continue
			}
			ev = ev.Elem()
		}
		if ev.Kind() != reflect.Struct {
			continue
		}
		for i := 0; i < ev.NumField(); i++ {
			ft := ev.Type().Field(i)
			f := ev.Field(i)
			if !f.CanInterface() {
				continue
			}
			name := strings.ToLower(ft.Name)
			if strings.Contains(name, "cron") || f.Type() == reflect.TypeOf((*cron.Cron)(nil)) {
				if fireCronValue(f) {
					fired = true
				}
			}
		}
	}
	return fired
}

func invokeZeroOrContext(m reflect.Value) bool {
	mt := m.Type()
	if mt.Kind() != reflect.Func {
		return false
	}
	switch mt.NumIn() {
	case 0:
		m.Call(nil)
		return true
	case 1:
		if mt.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem() {
			m.Call([]reflect.Value{reflect.ValueOf(context.Background())})
			return true
		}
	}
	return false
}

func fireCronValue(f reflect.Value) bool {
	if !f.IsValid() || !f.CanInterface() {
		return false
	}
	if invokeZeroOrContext(f) {
		return true
	}
	c, ok := f.Interface().(*cron.Cron)
	if !ok || c == nil {
		return false
	}
	entries := c.Entries()
	for _, entry := range entries {
		if entry.Job != nil {
			entry.Job.Run()
		}
	}
	return len(entries) > 0
}
