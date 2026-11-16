package daemon

import (
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
)

// turnstate_wiring.go arms the per-session turn-state publisher for every
// ACP session the daemon creates (inter-agent-coordination leaf 04-01, C8
// wiring half). The seam lives on acp.Manager (SetOnSessionCreated, fired
// once per freshly started session from GetOrCreatePersona); this file
// supplies the daemon-side callback that maps the acp string tuple onto
// agent.TurnStateEvent and publishes it on agent.TopicTurnState
// ("agent.turn.state") via agent.publishTurnState. The daemon may import
// internal/agent (no cycle: agent never imports daemon).
//
// Every emission is nil-guarded: a nil bus is a no-op (publishTurnState
// returns immediately), so an unwired daemon stays byte-identical with the
// seam installed but silent. The callback runs on the ACP session's turn
// goroutine and does no I/O beyond the bus publish.

// armTurnStatePublisher installs the turn-state publisher on one freshly
// created ACP session. busRef may be nil — the publisher still installs and
// every emission degrades to a no-op until a bus is present.
func armTurnStatePublisher(sess *acp.Session, busRef *bus.MessageBus) {
	if sess == nil {
		return
	}
	sess.SetTurnStatePublisher(func(agentID, state, stopReason, phase string) {
		ev := agent.TurnStateEvent{
			AgentID:    agentID,
			State:      agent.AgentTurnState(state),
			StopReason: agent.StopReason(stopReason),
			Phase:      phase,
			Timestamp:  time.Now().UTC(),
		}
		// agent.publishTurnState is unexported; this is its documented
		// transport for bus-holding publishers (turnstate.go): the typed
		// event straight onto TopicTurnState via bus.PublishT. The nil
		// guard matches publishTurnState's nil-bus no-op exactly.
		if busRef != nil {
			bus.PublishT(busRef, agent.TopicTurnState, "daemon", ev)
		}
	})
}

// installTurnStatePublisher returns the acp.Manager post-creation hook for
// the given bus. busRef may be nil; the hook is still returned so the
// manager's seam stays armed and emissions stay silent no-ops (pinned
// posture for the nil-bus wiring test).
func installTurnStatePublisher(busRef *bus.MessageBus) func(*acp.Session) {
	return func(sess *acp.Session) {
		armTurnStatePublisher(sess, busRef)
	}
}
