package daemon

// 04-replies-consent/00T §K3 — reply/consent glue gate (TEST-ONLY file).
// Drives the ARMED comm/http messaging webhook end-to-end exactly the way
// messaging_arming_test.go does (armingRoutes/postMessaging/canned-
// MessagingBody reused, NOT edited). Extends rather than modifies the
// 03-arming gate; every local helper here is rpl*-prefixed for collision
// safety in the shared daemon test package.
//
// =========================================================================
// PINNED FUTURE SHAPES — compile-RED until leaf 04/02 lands. Names and
// signatures frozen; the impl leaf adds them, nothing else may rename.
//
// (1) internal/daemon/messaging_arming.go — messagingArmingDeps gains:
//
//	ReplyHook func(voicetelnyx.Message, sms.ReplyAction) error
//
//	OnInbound semantics (inserted at the existing
//	"-- SEAM (leaf 04/01)" marker, AFTER store.AppendEvent +
//	InboundTopic publish, per the seam comment's placement rule):
//	  - ClassifyReply(msg.Text) runs on every NEWLY-APPLIED inbound
//    (dup event id returns before the seam — AppendEvent false acks
//    silently with no hook call, existing redelivery pin unchanged).
//	  - ReplyHook != nil => called SYNCHRONOUSLY with (msg, action)
//    for EVERY action, including ReplyOther (the hook switches
//    internally; nil hook => no reply handling, which keeps every
//    existing 03-arming test green untouched).
//	  - A ReplyHook error is LOGGED and SWALLOWED: OnInbound still
//    returns nil. §K3 pin — the classifier/glue can never drop a
//    durably-stored row or turn it into a redelivery storm. Proved by
//    TestReplyGlueHookErrorNeverDropsStoreOrPublish below.
//
// (2) internal/daemon consent hook constructor (leaf 04/02 builds the
// production hook from these primitives and hands it to the wiring):
//
//	type consentReplyDeps struct {
//	    Contacts     *contact.SQLiteStore      // opt-out toggles (D5)
//	    Sender       *telnyx.Client            // DIRECT SendSMS2 path (§D
//	                                          // note below), httptest-
//	                                          // base URL in tests
//	    Bus          *bus.MessageBus           // YES ack fan-out
//	    HelpThrottle *voicetelnyx.NonceStore   // 24h/contact (master §7:
//	                                          // NewNonceStore(24h, n)
//	                                          // reuse; CheckFresh = send)
//	    Confirms     *sms.PendingConfirms      // §K2 registry (#132)
//	    StopReply    string                    // cfg stop_reply (empty =>
//	                                          // sms-pkg default wording)
//	    HelpReply    string                    // cfg help_reply (EMPTY =>
//	                                          // HELP stays SILENT, the
//	                                          // auto-reply only arms when
//	                                          // the operator sets text)
//	    Logger       *slog.Logger
//	}
//	func newConsentReplyHook(d consentReplyDeps) func(voicetelnyx.Message, sms.ReplyAction) error
//
// Behavioral contract asserted below:
//   - ReplyStop   -> Contacts: record exists => MessagingOptOut SET via
//     read-modify-Put (second-wins upsert, contact/store.go:221-258);
//     unknown number => NO record created, but the courtesy confirmation
//     STILL sends (02-glue task 1). Confirmation dedupe: a second STOP
//     from a contact that is already opted out sends NOTHING new.
//   - ReplyStart  -> record exists => MessagingOptOut CLEARED + start
//     confirmation sent (cfg.StopReply text reused — STOP/START share
//     the one config key per master §7; no separate start_reply exists).
//   - STOP/START confirmations send EVEN to the just-set opt-out — this
//     is the TCPA courtesy-confirmation exception (02-glue task 2): the
//     send goes through the DIRECT telnyx.Client.SendSMS2 path, never
//     the contact channel that consults D5 opt-out. The e2e pin is
//     observable: opt-out is SET in the store AND the confirmation was
//     captured, in the same event.
//   - ReplyHelp  -> cfg.HelpReply non-empty: send iff
//     HelpThrottle.CheckFresh(msg.From) is true (1/contact/24h); a
//     second HELP inside the window is silent. cfg empty: always silent.
//   - ReplyYes   -> Confirms.Resolve(msg.From): true => publish on
//     sms.StateTopic a StateChanged with ProviderMessageID = pending.Ref
//     and Status = "ack" (the "StateTopic-shaped ack (kind ack)" pin —
//     v1 reuses the two string slots rather than a new payload type;
//     #132 consumes ref-id + "ack"). Second YES: registry one-shot =>
//     false => NO second ack, NO send. Unregistered YES: silent, no
//     send, no ack.
//   - ReplyOther -> no side effects.
//
// Test numbers: armingPhone() from the reused 03-gate file (digit
// concatenation, redaction-proof); reply-side suffixes start at 400 to
// stay disjoint from the 03-gate's 1xx-3xx ranges. Sends are captured on
// an httptest server standing in for api.telnyx.com (the Client's inject
// able baseURL, messaging.go:134).
// =========================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	commhttp "github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/sms"
	"github.com/caimlas/meept/internal/telnyx"
	voicetelnyx "github.com/caimlas/meept/internal/voice/telnyx"
)

// --- helpers (rpl*-prefixed; no collisions with the 03 gate) ------------

type rplSend struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// rplCapture is the telnyx-API stand-in: records every POST /v2/messages
// body and answers the accepted-message envelope SendSMS2 expects.
type rplCapture struct {
	srv   *httptest.Server
	mu    sync.Mutex
	sends []rplSend
	fails bool // when true, answer 500 so SendSMS2 errors
}

func (c *rplCapture) record() []rplSend {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]rplSend, len(c.sends))
	copy(out, c.sends)
	return out
}

func newRplCapture(t *testing.T) *rplCapture {
	t.Helper()
	c := &rplCapture{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var s rplSend
		_ = json.Unmarshal(body, &s)
		c.mu.Lock()
		fail := c.fails
		if !fail {
			c.sends = append(c.sends, s)
		}
		c.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errors":[{"code":"500","description":"boom"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"glue-msg-1","status":"queued"}}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// (The former setFails(bool) accessor was dropped as dead code — the
// pre-commit unused-code gate (staticcheck U1000) does not honor
// nolint. c.fails stays live on the serve path at :143; a future 500-
// path case arms it directly on the fixture struct.)

// openRplContacts opens the real contact store (the a8186602 messaging_
// opt_out guard column is created by contact.NewStore's migrate).
func openRplContacts(t *testing.T) *contact.SQLiteStore {
	t.Helper()
	st, err := contact.NewStore(filepath.Join(t.TempDir(), "contacts-rpl.db"))
	if err != nil {
		t.Fatalf("contact.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// rplGlue bundles one armed end-to-end route + all observable seams.
type rplGlue struct {
	routes   *commhttp.MessagingRoutes
	contacts *contact.SQLiteStore
	capture  *rplCapture
	confirms *sms.PendingConfirms
	inbound  chan sms.InboundMessage
	states   chan sms.StateChanged
	store    *sms.Store
}

// newRplGlue arms buildMessagingDeps with the production consent hook.
// Empty helpReply opts into the silence pin; helpThrottleTTL is the
// NonceStore window.
func newRplGlue(t *testing.T, stopReply, helpReply string) *rplGlue {
	t.Helper()
	st, _ := openArmingStore(t)
	b := newArmingBus(t)
	contacts := openRplContacts(t)
	capture := newRplCapture(t)
	// M14 fail-closed: the consent glue replies to each test's inbound
	// `from` number; allowlist the full 401-408 range the suite uses.
	sender, err := telnyx.NewClient(capture.srv.URL,
		telnyx.WithAPIKey("gate-token"),
		telnyx.WithFromNumber(armingPhone(900)),
		telnyx.WithAllowedNumbers([]string{
			armingPhone(401), armingPhone(402), armingPhone(403), armingPhone(404),
			armingPhone(405), armingPhone(406), armingPhone(407), armingPhone(408),
		}))
	if err != nil {
		t.Fatalf("telnyx.NewClient: %v", err)
	}
	confirms := sms.NewPendingConfirms(nil, 30*time.Minute)
	hook := newConsentReplyHook(consentReplyDeps{
		Contacts:     contacts,
		Sender:       sender,
		Bus:          b,
		HelpThrottle: voicetelnyx.NewNonceStore(24*time.Hour, 1000),
		Confirms:     confirms,
		StopReply:    stopReply,
		HelpReply:    helpReply,
		Logger:       testLogger(t),
	})
	d := buildMessagingDeps(messagingArmingDeps{
		Store:     st,
		Nonce:     voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:       b,
		Logger:    testLogger(t),
		ReplyHook: hook,
	})
	if d.OnInbound == nil {
		t.Fatal("precondition: armed (non-nil Store) OnInbound must be set")
	}
	inbound := make(chan sms.InboundMessage, 8)
	sub := bus.SubscribeT(b, "reply-glue-in", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	t.Cleanup(func() { b.Unsubscribe(sub) })
	states := make(chan sms.StateChanged, 8)
	sub2 := bus.SubscribeT(b, "reply-glue-st", sms.StateTopic, func(p sms.StateChanged) { states <- p })
	t.Cleanup(func() { b.Unsubscribe(sub2) })

	return &rplGlue{
		routes:   armingRoutes(t, commhttp.MessagingWebhookDeps{OnInbound: d.OnInbound, OnFinalized: d.OnFinalized}),
		contacts: contacts,
		capture:  capture,
		confirms: confirms,
		inbound:  inbound,
		states:   states,
		store:    st,
	}
}

// postInbound signs + posts a message.received with a unique event id and
// awaits the InboundTopic fan-out so side effects are observed AFTER the
// synchronous glue leg completed.
func (g *rplGlue) postInbound(t *testing.T, eventID, from, to, text string) {
	t.Helper()
	rec := postMessaging(t, g.routes,
		cannedMessagingBody("message.received", eventID, "prov-"+eventID, from, to, text, ""),
		true)
	if !is2xx(rec.Code) {
		t.Fatalf("inbound %q status = %d; want 2xx", text, rec.Code)
	}
	select {
	case p := <-g.inbound:
		if p.EventID != eventID || p.From != from || p.Text != text {
			t.Fatalf("InboundTopic payload = %+v; want %s/%s/%q", p, eventID, from, text)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("InboundTopic fired nothing for %s", eventID)
	}
}

func (g *rplGlue) optOut(t *testing.T, e164 string) bool {
	t.Helper()
	rec, err := g.contacts.GetByPhone(context.Background(), e164)
	if err != nil {
		if errors.Is(err, contact.ErrUnknownContact) {
			return false
		}
		t.Fatalf("GetByPhone(%s): %v", e164, err)
	}
	return rec.MessagingOptOut
}

func (g *rplGlue) sendCount(t *testing.T) int {
	t.Helper()
	return len(g.capture.record())
}

// --- §K3 STOP -------------------------------------------------------------

// TestReplyGlueStopOptsOutAndConfirms pins the STOP leg end-to-end through
// the armed webhook: contact opt-out SET (real contact store, guard column
// from a8186602), courtesy confirmation captured on the telnyx stand-in
// EVEN to the just-set opt-out (direct-send path, 02-glue task 2), and
// dedupe: a second STOP adds NO new confirmation send (contact already
// opted out) and keeps the row set.
func TestReplyGlueStopOptsOutAndConfirms(t *testing.T) {
	g := newRplGlue(t, "You have been unsubscribed.", "")
	from, to := armingPhone(401), armingPhone(900)

	if err := g.contacts.Put(context.Background(), contact.Record{
		ID: "rpl-stop-1", DisplayName: "Stopper", Phone: from,
	}); err != nil {
		t.Fatalf("seed contact: %v", err)
	}

	g.postInbound(t, "evt-stop-1", from, to, "STOP")

	if !g.optOut(t, from) {
		t.Fatal("contact MessagingOptOut not set after armed STOP")
	}
	sends := g.capture.record()
	if len(sends) != 1 {
		t.Fatalf("confirmation sends after STOP = %d (%+v); want exactly 1", len(sends), sends)
	}
	if sends[0].To != from || sends[0].Text != "You have been unsubscribed." {
		t.Fatalf("STOP confirmation = %+v; want to=%s text=cfg stop_reply", sends[0], from)
	}

	// Dedupe: second STOP (fresh event id -> passes nonce + AppendEvent)
	// must NOT re-confirm an already-opted contact.
	g.postInbound(t, "evt-stop-2", from, to, "stop")
	if g.sendCount(t) != 1 {
		t.Fatalf("sends after second STOP = %d; want 1 (STOP confirmation dedupes)", g.sendCount(t))
	}
	if !g.optOut(t, from) {
		t.Fatal("second STOP flipped opt-out back; must stay set")
	}
}

// TestReplyGlueStopUnknownNumberConfirmsNoRecord pins the polite-unknown
// posture (02-glue task 1): STOP from an unseeded number sends the
// confirmation but creates NO contact record (no ghost rows from junk
// inbound traffic).
func TestReplyGlueStopUnknownNumberConfirmsNoRecord(t *testing.T) {
	g := newRplGlue(t, "Unsubscribed.", "")
	from := armingPhone(402)

	g.postInbound(t, "evt-stop-unk-1", from, armingPhone(900), "unsubscribe")

	if g.sendCount(t) != 1 {
		t.Fatalf("unknown-number STOP sends = %d; want 1 (courtesy confirmation still goes out)", g.sendCount(t))
	}
	if _, err := g.contacts.GetByPhone(context.Background(), from); !errors.Is(err, contact.ErrUnknownContact) {
		t.Fatalf("GetByPhone after unknown STOP err = %v; want ErrUnknownContact (no record created)", err)
	}
}

// TestReplyGlueStopDefaultWording pins that an EMPTY cfg stop_reply still
// confirms — wording defaults live in the sms pkg (config gate
// messaging_texting_test.go:42), ported from strategist-usa
// go-shard-runner/internal/workers/incoming_message.go:386 ("You have
// been opted out. Reply START to resubscribe.") — so the STOP default
// text references the START resubscribe path.
func TestReplyGlueStopDefaultWording(t *testing.T) {
	g := newRplGlue(t, "", "")
	from := armingPhone(403)

	g.postInbound(t, "evt-stop-def-1", from, armingPhone(900), "STOP")

	sends := g.capture.record()
	if len(sends) != 1 {
		t.Fatalf("default-wording STOP sends = %d; want 1", len(sends))
	}
	if sends[0].Text == "" {
		t.Fatal("default STOP confirmation empty; sms-pkg default wording must apply")
	}
	if !containsFold(sends[0].Text, "START") {
		t.Fatalf("default STOP confirmation = %q; ported wording (:386) must mention START to resubscribe", sends[0].Text)
	}
}

// --- §K3 START ------------------------------------------------------------

// TestReplyGlueStartClearsOptOut pins the symmetric START leg (master §7:
// "STOP reply sets it, START reply clears it" — contact/types.go:36-38):
// opted-out contact + armed START -> flag cleared + confirmation sent.
func TestReplyGlueStartClearsOptOut(t *testing.T) {
	g := newRplGlue(t, "You have been unsubscribed.", "")
	from := armingPhone(404)

	if err := g.contacts.Put(context.Background(), contact.Record{
		ID: "rpl-start-1", DisplayName: "Returner", Phone: from, MessagingOptOut: true,
	}); err != nil {
		t.Fatalf("seed opted-out contact: %v", err)
	}

	g.postInbound(t, "evt-start-1", from, armingPhone(900), "START")

	if g.optOut(t, from) {
		t.Fatal("contact still opted out after armed START")
	}
	if g.sendCount(t) != 1 {
		t.Fatalf("START confirmation sends = %d; want 1", g.sendCount(t))
	}
}

// --- §K3 HELP -------------------------------------------------------------

// TestReplyGlueHelpThrottledOnePer24h pins the HELP leg: cfg.HelpReply set
// -> exactly one auto-reply per contact per window (NonceStore 24h reuse,
// master §7); second HELP inside the window SILENT. Window cannot be
// advanced (real-time NonceStore) — the pin is the fresh-deny, which is
// the throttled posture's observable half.
func TestReplyGlueHelpThrottledOnePer24h(t *testing.T) {
	g := newRplGlue(t, "", "Reply STOP to unsubscribe. Reply START to subscribe.")
	from := armingPhone(405)

	g.postInbound(t, "evt-help-1", from, armingPhone(900), "HELP")
	if g.sendCount(t) != 1 {
		t.Fatalf("first HELP sends = %d; want 1 auto-reply", g.sendCount(t))
	}

	g.postInbound(t, "evt-help-2", from, armingPhone(900), "help")
	if g.sendCount(t) != 1 {
		t.Fatalf("sends after second HELP in window = %d; want 1 (throttled 1/contact/24h)", g.sendCount(t))
	}

	// Different contact is NOT throttled by the first one's window.
	other := armingPhone(406)
	g.postInbound(t, "evt-help-3", other, armingPhone(900), "HELP")
	if g.sendCount(t) != 2 {
		t.Fatalf("HELP from a different number sends = %d; want 2 (throttle is per-contact)", g.sendCount(t))
	}
}

// TestReplyGlueHelpSilentWithoutCfg pins the silent-config posture:
// cfg.HelpReply empty -> HELP is stored/published but NEVER answered
// (auto-reply arms only on operator-set text).
func TestReplyGlueHelpSilentWithoutCfg(t *testing.T) {
	g := newRplGlue(t, "", "")
	g.postInbound(t, "evt-help-off-1", armingPhone(407), armingPhone(900), "HELP")
	if g.sendCount(t) != 0 {
		t.Fatalf("HELP with empty cfg.HelpReply sends = %d; want 0 (silent)", g.sendCount(t))
	}
}

// --- §K3 YES --------------------------------------------------------------

// TestReplyGlueYesResolvesPendingAndAcks pins the #132 dispatch-ack leg:
// YES from a number with a registered pending -> Confirms.Resolve true +
// StateTopic-shaped ack (ProviderMessageID = pending ref, Status = "ack")
// + NO SMS send. Second YES: registry one-shot -> no double-resolve, no
// second ack.
func TestReplyGlueYesResolvesPendingAndAcks(t *testing.T) {
	g := newRplGlue(t, "", "")
	from := armingPhone(408)
	g.confirms.Register("dispatch-132", from, "coffee at 3?")

	g.postInbound(t, "evt-yes-1", from, armingPhone(900), "YES")

	select {
	case p := <-g.states:
		if p.ProviderMessageID != "dispatch-132" || p.Status != "ack" {
			t.Fatalf("ack on StateTopic = %+v; want ProviderMessageID=dispatch-132 Status=ack", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("YES with a registered pending fired no StateTopic ack")
	}
	if g.sendCount(t) != 0 {
		t.Fatalf("YES sends = %d; want 0 (ack is a bus event, not an SMS)", g.sendCount(t))
	}

	// Drain check + second YES must not double-resolve.
	g.postInbound(t, "evt-yes-2", from, armingPhone(900), "yes")
	select {
	case p := <-g.states:
		t.Fatalf("second YES published a second ack %+v; want none (one-shot registry)", p)
	case <-time.After(300 * time.Millisecond):
	}
	if _, ok := g.confirms.Resolve(from); ok {
		t.Fatal("pending still resolvable after the glue consumed it")
	}
}

// TestReplyGlueYesWithoutPendingSilent pins the miss posture: YES from an
// unregistered number -> no ack, no send, still 2xx + stored.
func TestReplyGlueYesWithoutPendingSilent(t *testing.T) {
	g := newRplGlue(t, "", "")
	g.postInbound(t, "evt-yes-miss-1", armingPhone(409), armingPhone(900), "yes")

	select {
	case p := <-g.states:
		t.Fatalf("unregistered YES published ack %+v; want none", p)
	case <-time.After(300 * time.Millisecond):
	}
	if g.sendCount(t) != 0 {
		t.Fatalf("unregistered YES sends = %d; want 0", g.sendCount(t))
	}
	rows, err := g.store.RecentEvents(context.Background(), armingPhone(409), time.Time{}, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("store rows for YES miss = %+v err=%v; want exactly 1 (inbound always stored)", rows, err)
	}
}

// --- §K3 classifier-failure integrity --------------------------------------

// TestReplyGlueHookErrorNeverDropsStoreOrPublish is the §K3 poison pin: a
// ReplyHook that returns an error on EVERY action must not change the
// handler posture (2xx ack), the durable AppendEvent row, or the
// InboundTopic fan-out — reply handling is strictly downstream of storage
// and can never become a redelivery storm (messaging_arming.go seam
// comment). The poison also proves the seam fires on ReplyOther-class
// text after the store+publish leg (the hook call ordering is what makes
// this test's 'rows==1 && publish==1' load-bearing).
func TestReplyGlueHookErrorNeverDropsStoreOrPublish(t *testing.T) {
	st, _ := openArmingStore(t)
	b := newArmingBus(t)

	var calls int
	var mu sync.Mutex
	poison := func(voicetelnyx.Message, sms.ReplyAction) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("poisoned classifier seam")
	}

	d := buildMessagingDeps(messagingArmingDeps{
		Store:     st,
		Nonce:     voicetelnyx.NewNonceStore(6*time.Minute, 10000),
		Bus:       b,
		Logger:    testLogger(t),
		ReplyHook: poison,
	})
	routes := armingRoutes(t, commhttp.MessagingWebhookDeps{
		OnInbound:   d.OnInbound,
		OnFinalized: d.OnFinalized,
	})

	inbound := make(chan sms.InboundMessage, 4)
	sub := bus.SubscribeT(b, "poison-gate", sms.InboundTopic, func(p sms.InboundMessage) { inbound <- p })
	defer b.Unsubscribe(sub)

	from := armingPhone(410)
	for _, text := range []string{"STOP", "help", "yes", "just prose"} {
		rec := postMessaging(t, routes,
			cannedMessagingBody("message.received", "evt-poison-"+text, "prov-poison", from, armingPhone(900), text, ""),
			true)
		if !is2xx(rec.Code) {
			t.Fatalf("poisoned hook %q: status = %d; want 2xx (error swallowed, §K3)", text, rec.Code)
		}
		select {
		case <-inbound:
		case <-time.After(2 * time.Second):
			t.Fatalf("poisoned hook %q: InboundTopic dropped the fan-out", text)
		}
	}

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 4 {
		t.Fatalf("ReplyHook calls = %d; want 4 (fires for every action incl. Other after store+publish)", got)
	}
	rows, err := st.RecentEvents(context.Background(), from, time.Time{}, 10)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("store rows = %d; want 4 (poisoned hook never drops the durable row)", len(rows))
	}
}

// containsFold is the case-insensitive substring check the SMS reply
// fixtures need. The hand-rolled ASCII case map this used to carry was
// flagged by scripts/audit-utf8-byte-arithmetic.py — strings.ToUpper is
// Unicode-correct and one call.
func containsFold(hay, needle string) bool {
	return strings.Contains(strings.ToUpper(hay), strings.ToUpper(needle))
}
