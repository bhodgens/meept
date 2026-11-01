package slack

import (
	"fmt"
	"strings"
)

// Action value grammar pinned by C3: "perm:<requestID>:<verb>" where verb
// is allow | deny | always (the relay verbs). The visible button label is
// decoration; the VALUE is the contract.
const (
	actionPrefix = "perm:"
	VerbAllow    = "allow"
	VerbDeny     = "deny"
	VerbAlways   = "always"
)

// ParseActionValue splits a block_actions value into (requestID, verb, ok).
// Any value not matching the exact grammar (including empty segments or a
// verb outside the relay verb set) returns ok=false - the caller answers
// with a generic ephemeral error and never feeds the value to the relay.
func ParseActionValue(v string) (requestID, verb string, ok bool) {
	if !strings.HasPrefix(v, actionPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(v, actionPrefix)
	id, verb, found := strings.Cut(rest, ":")
	if !found || id == "" || verb == "" {
		return "", "", false
	}
	switch verb {
	case VerbAllow, VerbDeny, VerbAlways:
		return id, verb, true
	default:
		return "", "", false
	}
}

// ActionValue renders the pinned grammar (inverse of ParseActionValue).
func ActionValue(requestID, verb string) string {
	return actionPrefix + requestID + ":" + verb
}

// resolveCard is the Block Kit payload for a pending permission request.
// The exact visual shape is this leaf's choice; the pinned element is the
// action VALUE grammar.
type resolveCard struct {
	Channel string  `json:"channel"`
	Text    string  `json:"text"`
	Blocks  []block `json:"blocks"`
	// TS is the message timestamp for chat.update bodies ("" on post).
	TS string `json:"ts,omitempty"`
}

type block struct {
	Type     string       `json:"type"`
	Text     *textObj     `json:"text,omitempty"`
	Elements []actionElem `json:"elements,omitempty"`
	Fields   []textObj    `json:"fields,omitempty"`
	Accessor *actionElem  `json:"accessory,omitempty"`
}

type textObj struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type actionElem struct {
	Type  string  `json:"type"`
	Text  textObj `json:"text"`
	Value string  `json:"value,omitempty"`
	Style string  `json:"style,omitempty"`
}

// renderRequestCard builds the chat.postMessage request body for a pending
// permission. requestID rides ONLY inside the action values.
func renderRequestCard(channel, requestID, summary, agentID string) resolveCard {
	title := fmt.Sprintf("permission requested (agent %s)", agentID)
	primary := textObj{Type: "mrkdwn", Text: fmt.Sprintf("*%s*\n%s", title, escapeSlackText(summary))}

	buttons := []actionElem{
		{Type: "button", Text: textObj{Type: "plain_text", Text: "allow"}, Value: ActionValue(requestID, VerbAllow), Style: "primary"},
		{Type: "button", Text: textObj{Type: "plain_text", Text: "deny"}, Value: ActionValue(requestID, VerbDeny), Style: "danger"},
		{Type: "button", Text: textObj{Type: "plain_text", Text: "always"}, Value: ActionValue(requestID, VerbAlways)},
	}

	return resolveCard{
		Channel: channel,
		Text:    title,
		Blocks: []block{
			{Type: "section", Text: &primary},
			{Type: "actions", Elements: buttons},
		},
	}
}

// renderResolvedCard builds the chat.update body that flips the card to its
// terminal state (research A.3: chat.update needs channel + ts of the
// original message; both ride in the returned card).
func renderResolvedCard(channel, ts, decision, approver string) resolveCard {
	text := fmt.Sprintf("permission request %s (by %s)", decision, approver)
	return resolveCard{
		Channel: channel,
		Text:    text,
		Blocks: []block{
			{Type: "section", Text: &textObj{Type: "mrkdwn", Text: "*" + text + "*"}},
		},
		TS: ts,
	}
}
