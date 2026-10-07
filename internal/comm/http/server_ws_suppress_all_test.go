package http

// Bughunt H3 — the channel-less unsubscribe frame.
//
// A client sends {type: "unsubscribe", data: {channel: "all"}} with no
// session_id. The handler used to `delete(h.sessionSubs[wc])`, which does NOT
// suppress delivery — ShouldSend treats an absent filter set as BROADCAST mode
// — so the connection immediately started receiving every session's events again,
// including the ones it had explicitly unsubscribed from. That is the exact
// failure the per-(channel, session) suppression ledger exists to prevent.
//
// The fix records a connection-wide opt-out FLAG and keeps the grants, so a
// later re-subscribe re-arms without the client re-enrolling every session.

import "testing"

func TestWSChannelFilter_ConnectionWideUnsubscribeSuppressesAndKeepsGrants(t *testing.T) {
	hub := NewWebSocketHub(nil)

	const sessA = "session-h3-a"
	const sessB = "session-h3-b"

	// The client subscribes both sessions across both channels, exactly as the
	// Flutter client does (chat + progress for one session).
	hub.SubscribeSession(nilWSConn, sessA, wsChannelChat)
	hub.SubscribeSession(nilWSConn, sessA, wsChannelProgress)
	hub.SubscribeSession(nilWSConn, sessB, wsChannelChat)

	// Baseline: delivery works.
	for _, tc := range []struct{ event, session string }{
		{"chat_message", sessA},
		{"agent_progress", sessA},
		{"chat_message", sessB},
	} {
		if !hub.ShouldSend(nilWSConn, tc.event, tc.session) {
			t.Fatalf("precondition: %s/%s should be delivered", tc.event, tc.session)
		}
	}

	// The channel-level frame with no session id: a connection-wide opt-out.
	hub.SuppressAll(nilWSConn)

	// Nothing is delivered on ANY channel, for ANY session.
	for _, tc := range []struct{ event, session string }{
		{"chat_message", sessA},
		{"agent_progress", sessA},
		{"chat_message", sessB},
		{"turn.terminal", sessA},
	} {
		if hub.ShouldSend(nilWSConn, tc.event, tc.session) {
			t.Errorf("%s/%s delivered after a connection-wide unsubscribe", tc.event, tc.session)
		}
	}

	// The GRANTS SURVIVE. This is the half that distinguishes the fix from the
	// old delete: the entry is still present, so a re-subscribe re-arms
	// delivery rather than leaving the connection muted forever.
	hub.sessMu.RLock()
	subs := hub.sessionSubs[nilWSConn]
	hub.sessMu.RUnlock()
	if subs == nil {
		t.Fatal("the connection's filter entry was deleted; delivery would revert to broadcast mode")
	}
	if len(subs.sessions) != 2 {
		t.Errorf("granted sessions = %d, want 2 (the opt-out must not discard grants)", len(subs.sessions))
	}

	// Re-subscribing clears the connection-wide opt-out and restores delivery.
	// The flag is connection-wide, so lifting it restores exactly the GRANT SET
	// that was preserved — which is the point of keeping the grants rather than
	// deleting them: the client's own subscriptions come back, and the opt-outs
	// it had recorded per (channel, session) still hold.
	hub.SubscribeSession(nilWSConn, sessA, wsChannelChat)
	if !hub.ShouldSend(nilWSConn, "chat_message", sessA) {
		t.Error("a re-subscribe must re-arm delivery after a connection-wide unsubscribe")
	}
	if !hub.ShouldSend(nilWSConn, "chat_message", sessB) {
		t.Error("sessB was granted and never unsubscribed; lifting the flag must restore it too")
	}

	// A session the client never granted stays muted after the re-subscribe:
	// restoring the grant set is not the same as returning to broadcast mode.
	if hub.ShouldSend(nilWSConn, "chat_message", "never-subscribed") {
		t.Error("a re-subscribe returned the connection to broadcast mode for an ungranted session")
	}
}

// TestWSChannelFilter_ConnectionWideOptOutStillFailsOpenWhenNoEntry proves the
// flag does not change broadcast mode for a connection that never subscribed:
// an absent entry is still broadcast, because ShouldSend is a pure filter query
// and the session-less bypass belongs at the relay call sites.
func TestWSChannelFilter_ConnectionWideOptOutStillFailsOpenWhenNoEntry(t *testing.T) {
	hub := NewWebSocketHub(nil)

	if !hub.ShouldSend(nilWSConn, "chat_message", "any-session") {
		t.Error("a connection with no filters must stay in broadcast mode")
	}
}

// TestWSChannelFilter_ConnectionWideOptOutSuppressesPreviouslyUnsubscribed is
// the regression the old delete actually caused: the client had EXPLICITLY
// unsubscribed sessA's chat, the deletion returned the whole connection to
// broadcast, and sessA's chat started arriving again. This pins that the
// connection-wide opt-out keeps it muted.
func TestWSChannelFilter_ConnectionWideOptOutSuppressesPreviouslyUnsubscribed(t *testing.T) {
	hub := NewWebSocketHub(nil)

	hub.SubscribeSession(nilWSConn, "s1", wsChannelChat)
	hub.SubscribeSession(nilWSConn, "s1", wsChannelProgress)
	hub.UnsubscribeSession(nilWSConn, "s1", wsChannelChat)

	// Explicit chat unsubscribe holds.
	if hub.ShouldSend(nilWSConn, "chat_message", "s1") {
		t.Fatal("precondition: the explicit chat unsubscribe did not mute chat")
	}

	// A channel-level unsubscribe with no session arrives (the old code path
	// that deleted the whole entry).
	hub.SuppressAll(nilWSConn)

	if hub.ShouldSend(nilWSConn, "chat_message", "s1") {
		t.Error("H3: chat re-delivered for s1 after a connection-wide unsubscribe; " +
			"the client had explicitly opted out and must stay opted out")
	}
	if hub.ShouldSend(nilWSConn, "agent_progress", "s1") {
		t.Error("progress re-delivered after a connection-wide unsubscribe")
	}
}
