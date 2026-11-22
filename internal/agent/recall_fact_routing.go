package agent

// Fact->memory routing (03-context/02 T3, F-ORCH-24; gate T5
// recall_fact_test.go).
//
// A fact question about the CONVERSATION ("what was the codeword I told
// you earlier in this session?") is answerable from the scoped memory
// lane (buildMemoryContext -> sessionScopeOptions -> Manager.Search).
// The live 8B classified it quickplan @0.9 and the turn decomposed into
// task_get plumbing -- a task row interrogating the task store instead
// of answering the user's question. isFactRecallQuestion detects the
// shape so classifyIntent can override a task-creating verdict with the
// recall lane (no task creation; the chat loop answers from the scoped
// memory context, and RecallAnswer falls through when no prior task
// exists).
//
// The detector is deliberately NARROW: an interrogative plus a
// personal past-conversation cue, with no leading execution imperative
// and no work artifact. "can you fix the bug I told you about?" stays
// work (artifact mention); "implement what I told you" stays work (not
// interrogative). Only conversation-fact questions reroute.

import "strings"

// factRecallConversationCues are the personal past-conversation markers
// that distinguish "recall a fact from THIS conversation" from generic
// questions. Lowercase substring match.
var factRecallConversationCues = []string{
	"told you",
	"you told me",
	"i said",
	"i mentioned",
	"you said",
	"we discussed",
	"i gave you",
	"do you remember",
	"earlier in this session",
	"earlier in this conversation",
}

// isFactRecallQuestion reports whether input is a fact-shaped question
// about the conversation itself (answerable from memory, never new
// work). A recall-intent verdict is left alone: RecallAnswer already
// answers it from the session digest / scoped memory. Only a
// TASK-CREATING verdict on this shape is the F-ORCH-24 defect.
func isFactRecallQuestion(input string) bool {
	if !isInterrogative(input) {
		return false
	}
	if hasLeadingImperativeVerb(input) {
		return false
	}
	if referencesPriorWork(input) {
		return false
	}
	lower := strings.ToLower(input)
	for _, cue := range factRecallConversationCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}
