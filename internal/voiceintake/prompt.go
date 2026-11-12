package voiceintake

// Guided-intake prompt blocks (leaf 05 T4): the one-question-at-a-time
// procedure and the read-back builder, ported GENERically from the
// uptractor coordinator intake (phone/core/internal/agent/intake.go —
// propose_record's NEEDS <field> single-question contract and the
// verbatim server-built readback). No Uptractor vocabulary: fields are
// whatever the IntakeSpec declares (the secretary ships message-ish
// kinds), never jobs or work orders.

import (
	"fmt"
	"strings"
)

// IntakeField declares one collectable field: its canonical name, the
// spoken question asked when it is missing, and whether the record can
// file without it.
type IntakeField struct {
	Name     string
	Question string
	Required bool
}

// IntakeSpec declares one intake record kind: the kind vocabulary word
// and its fields. IntakeSpec.Kind stays a plain string by design (master
// note: the dispatch-board tree extends kinds without touching this pkg).
type IntakeSpec struct {
	Kind   string
	Fields []IntakeField
}

// MissingField returns the FIRST required field missing from collected,
// with its one question — the one-question-at-a-time contract. "Missing"
// means absent from the map or blank/whitespace-only. ok=false when no
// required field is missing (the record is ready for read-back).
func (s IntakeSpec) MissingField(collected map[string]string) (field string, question string, ok bool) {
	for _, f := range s.Fields {
		if !f.Required {
			continue
		}
		if strings.TrimSpace(collected[f.Name]) == "" {
			return f.Name, f.Question, true
		}
	}
	return "", "", false
}

// NextQuestion renders the model-facing instruction for the next missing
// field, intake.go's "NEEDS <field>. Ask the caller only this" shape:
// the agent asks EXACTLY that one question, never a bundle.
func (s IntakeSpec) NextQuestion(collected map[string]string) string {
	field, question, ok := s.MissingField(collected)
	if !ok {
		return ""
	}
	return fmt.Sprintf("NEEDS %s. Ask the caller only this, in one short question: %s", field, question)
}

// ReadBack assembles the verbatim read-back from the collected fields in
// spec order — the words the agent says word-for-word before asking for
// confirmation. Only filled fields render; a completely empty record
// yields an empty string (the caller validates before reaching here).
func (s IntakeSpec) ReadBack(collected map[string]string) string {
	var parts []string
	for _, f := range s.Fields {
		if v := strings.TrimSpace(collected[f.Name]); v != "" {
			parts = append(parts, f.Name+": "+v)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// GuidedIntakePrompt is the employee-prompt extension the wiring appends
// when the secretary's guided-intake gate is on (leaf 05 T3). Ported from
// IntakeSystemPrompt, generalized: identify the caller, one question at a
// time driven by the tool's NEEDS line, verbatim read-back, save only
// after the caller confirms, ambiguous subject lists are asked about —
// never picked. intake_save is the ONLY write path; nothing is "saved"
// until it returns saved.
const GuidedIntakePrompt = "Guided intake procedure: First learn who is calling and what they need. " +
	"Collect the needed details ONE question at a time — the intake_save tool tells you exactly which field to ask next; ask only that. " +
	"When a name you hear could match more than one known entry, never pick one yourself: say the options and ask the caller which they mean. " +
	"When nothing is missing, say the read-back to the caller word for word, then wait: they may say confirm, or tell you what to change. " +
	"Never say anything is saved until intake_save returns saved — and call it only AFTER the caller confirms the read-back. " +
	"If they correct something, collect the corrected field again and read back once more. " +
	"Keep every reply to one or two short spoken sentences with no lists."
