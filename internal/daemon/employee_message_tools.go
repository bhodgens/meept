// Package daemon — employee_message_tools.go wires the loop-economics
// leaf-10 inter-agent messaging stack: the employee MessageStore is
// adapted to the tool-layer agentMessageStore (import-cycle constraint:
// internal/tools/builtin cannot import internal/employee), and
// send_agent_message / inbox tools are registered with roster
// reachability from the employee Manager.
package daemon

import (
	"context"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// employeeMessageStoreAdapter adapts *employee.MessageStore to
// builtin.agentMessageStore.
type employeeMessageStoreAdapter struct {
	store *employee.MessageStore
}

func (a *employeeMessageStoreAdapter) Enqueue(msg *builtin.AgentMessage) error {
	m := &employee.AgentMessage{ID: msg.ID, From: msg.From, To: msg.To, Body: msg.Body}
	if err := a.store.Enqueue(m); err != nil {
		return err
	}
	msg.ID = m.ID
	msg.State = m.State
	msg.CreatedAt = m.CreatedAt
	return nil
}

func (a *employeeMessageStoreAdapter) DrainInbox(to string, limit int) ([]*builtin.AgentMessage, error) {
	msgs, err := a.store.DrainInbox(to, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*builtin.AgentMessage, len(msgs))
	for i := range msgs {
		out[i] = &builtin.AgentMessage{
			ID: msgs[i].ID, From: msgs[i].From, To: msgs[i].To,
			Body: msgs[i].Body, State: msgs[i].State,
			CreatedAt: msgs[i].CreatedAt, DeliveredAt: msgs[i].DeliveredAt,
		}
	}
	return out, nil
}

func (a *employeeMessageStoreAdapter) MarkRead(ids []string) error {
	return a.store.MarkRead(ids)
}

var _ builtin.AgentMessager = (*employeeMessageStoreAdapter)(nil)

// adapterMessageDrainer adapts the builtin-facing adapter to
// agent.MessageDrainer. The two interfaces both declare DrainInbox but with
// different return types, so one concrete type cannot satisfy both; the tiny
// wrapper bridges the store to the loop's turn-start injection seam.
type adapterMessageDrainer struct {
	adapter *employeeMessageStoreAdapter
}

func (d *adapterMessageDrainer) DrainInbox(to string, limit int) ([]agent.IncomingAgentMessage, error) {
	msgs, err := d.adapter.DrainInbox(to, limit)
	if err != nil {
		return nil, err
	}
	out := make([]agent.IncomingAgentMessage, len(msgs))
	for i := range msgs {
		out[i] = agent.IncomingAgentMessage{
			ID: msgs[i].ID, From: msgs[i].From, To: msgs[i].To,
			Body: msgs[i].Body, CreatedAt: msgs[i].CreatedAt,
		}
	}
	return out, nil
}

var _ agent.MessageDrainer = (*adapterMessageDrainer)(nil)

// newMessageDrainer returns an agent.MessageDrainer backed by the adapter.
// Returns nil for a nil adapter (no typed-nil boxing).
func newMessageDrainer(a *employeeMessageStoreAdapter) agent.MessageDrainer {
	if a == nil {
		return nil
	}
	return &adapterMessageDrainer{adapter: a}
}

// WireEmployeeMessaging registers send_agent_message and inbox on the
// given tool registry using the employee message store and manager. A
// nil store or registry skips registration (messaging unavailable).
//
// senderOf resolves the calling agent's ID at tool execution time; it is
// also used to drain each loop's inbox at turn start (the daemon sets
// AgentLoop.SetMessageDrainer with a drainer bound to this adapter).
func WireEmployeeMessaging(
	msgStore *employee.MessageStore,
	mgr *employee.Manager,
	senderOf func(ctx context.Context) string,
) *employeeMessageStoreAdapter {
	if msgStore == nil {
		return nil
	}
	adapter := &employeeMessageStoreAdapter{store: msgStore}
	if mgr != nil {
		mgr.SetEmployeeMessager(adapter)
	}
	return adapter
}

// WireEmployeeMessagingForComponents performs the leaf-10 messaging
// wiring against a constructed Components: registers send_agent_message,
// inbox, and roster reachability on the platform tools. Safe to call
// when the employee stack is disabled (no-op).
func WireEmployeeMessagingForComponents(c *Components) {
	if c == nil || c.ToolRegistry == nil {
		return
	}
	msgStore := c.EmployeeMessageStore
	senderOf := func(ctx context.Context) string {
		// The calling loop's agent id when the executor stamps it into
		// ctx (tools.AgentIDFromContext); "orchestrator" for contexts
		// without an agent identity (RPC-driven callers).
		if id := tools.AgentIDFromContext(ctx); id != "" {
			return id
		}
		return "orchestrator"
	}
	adapter := WireEmployeeMessaging(msgStore, c.EmployeeManager, senderOf)

	// Bot ids are message targets too. Wire the production drainer on the
	// shared template loop so every turn-start prompt build (employee, bot,
	// and plain agent sessions) drains its queued inbox. Uses a concrete
	// wrapper behind an explicit nil check to avoid typed-nil boxing.
	if d := newMessageDrainer(adapter); d != nil && c.AgentLoop != nil {
		c.AgentLoop.SetMessageDrainer(d)
	}

	// Roster reachability for platform_agents.
	for _, t := range c.ToolRegistry.List() {
		if pa, ok := t.(*builtin.PlatformAgentsTool); ok && c.EmployeeManager != nil {
			pa.SetReachability(func(agentID string) (bool, time.Time, bool) {
				reachable, lastSeen := c.EmployeeManager.Reachability(agentID)
				return reachable, lastSeen, true
			})
		}
	}
	if adapter == nil {
		return
	}
	targetExists := func(id string) bool {
		if c.AgentRegistry != nil {
			if _, ok := c.AgentRegistry.GetSpec(id); ok {
				return true
			}
		}
		if c.EmployeeManager != nil {
			emp, err := c.EmployeeManager.GetEmployee(context.Background(), id)
			if err == nil && emp != nil {
				return true
			}
		}
		if c.BotManager != nil {
			bot, err := c.BotManager.GetBot(context.Background(), id)
			if err == nil && bot != nil {
				return true
			}
		}
		return false
	}
	listTargets := func() []string {
		seen := make(map[string]struct{})
		var ids []string
		add := func(id string) {
			if id == "" {
				return
			}
			if _, ok := seen[id]; ok {
				return
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if c.EmployeeManager != nil {
			if emps, err := c.EmployeeManager.ListEmployees(context.Background(), ""); err == nil {
				for _, e := range emps {
					add(e.ID)
				}
			}
		}
		if c.BotManager != nil {
			if bots, err := c.BotManager.ListBots(context.Background()); err == nil {
				for _, b := range bots {
					add(b.ID)
				}
			}
		}
		return ids
	}
	c.ToolRegistry.Register(builtin.NewSendAgentMessageTool(adapter, targetExists, listTargets, senderOf))
	c.ToolRegistry.Register(builtin.NewInboxTool(adapter, senderOf, nil, "meept-daemon"))
}
