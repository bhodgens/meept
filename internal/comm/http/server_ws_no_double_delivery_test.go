package http

import (
	"os"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
)

// The bus delivers to ALL matching subscriptions. An exact pattern plus a
// wildcard that both match one topic ("employee.result" + "employee.*",
// "agent.turn.state" + "agent.*.*") produce TWO WS frames per event.
// Pin that WithWebSocket skips the redundant exact duplicates while still
// relaying the topics through the covering wildcard.
func TestWSRelayTopicPatternsNoDoubleDelivery(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	if !strings.Contains(string(src), `topic == "employee.result"`) ||
		!strings.Contains(string(src), `topic == "agent.turn.state"`) {
		t.Fatal("WithWebSocket does not skip the redundant exact subscriptions")
	}

	// The covering wildcards are present in the topic list.
	for _, wildcard := range []string{"employee.*", "agent.*.*"} {
		if !strings.Contains(string(src), `"`+wildcard+`"`) {
			t.Fatalf("covering wildcard %q missing from WS topic list", wildcard)
		}
	}
	// One subscription per topic: each of these is matched by exactly one
	// of {employee.*, agent.*.*} after the exact entries are dropped.
	for _, topic := range []string{"employee.result", "agent.turn.state"} {
		count := 0
		for _, p := range []string{"employee.*", "agent.*.*"} {
			if bus.MatchWildcard(p, topic) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("topic %q covered by %d wildcards, want 1", topic, count)
		}
	}
}
