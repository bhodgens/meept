package agent

import "testing"

// TestNewIntentTypesRoster verifies Plan 2's four new intent types have the
// expected string values.
func TestNewIntentTypesRoster(t *testing.T) {
	cases := []struct {
		got, want IntentType
	}{
		{IntentWrite, IntentType("write")},
		{IntentArchitect, IntentType("architect")},
		{IntentSkeptic, IntentType("skeptic")},
		{IntentLibrarian, IntentType("librarian")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// TestNewIntentCategoriesRoster verifies the four new intents defer to
// executors.
func TestNewIntentCategoriesRoster(t *testing.T) {
	for _, intent := range []IntentType{IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian} {
		if intent.Category() != CategoryDefer {
			t.Errorf("intent %q: got category %q, want %q", intent, intent.Category(), CategoryDefer)
		}
	}
}

// TestNewIntentDefaultAgentsRoster verifies routing from each new intent to
// its specialist agent.
func TestNewIntentDefaultAgentsRoster(t *testing.T) {
	cases := []struct {
		intent IntentType
		agent  string
	}{
		{IntentWrite, "writer"},
		{IntentArchitect, "architect"},
		{IntentSkeptic, "skeptic"},
		{IntentLibrarian, "librarian"},
	}
	for _, c := range cases {
		if got := c.intent.DefaultAgent(); got != c.agent {
			t.Errorf("intent %q: DefaultAgent got %q, want %q", c.intent, got, c.agent)
		}
	}
}

// TestNewIntentKeywordsRoster verifies each new intent surfaces trigger
// phrases for the classifier.
func TestNewIntentKeywordsRoster(t *testing.T) {
	for _, intent := range []IntentType{IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian} {
		if len(intent.Keywords()) == 0 {
			t.Errorf("intent %q should have keywords", intent)
		}
	}
}

// TestNewIntentValidRoster verifies the four new intents are valid types.
func TestNewIntentValidRoster(t *testing.T) {
	for _, intent := range []IntentType{IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian} {
		if !IsValidIntentType(string(intent)) {
			t.Errorf("intent %q should be valid", intent)
		}
	}
}

// TestNewIntentDispatchAsyncRoster verifies ShouldDispatchAsync returns true
// for the four new intents.
func TestNewIntentDispatchAsyncRoster(t *testing.T) {
	for _, intent := range []IntentType{IntentWrite, IntentArchitect, IntentSkeptic, IntentLibrarian} {
		if !intent.ShouldDispatchAsync(false) {
			t.Errorf("intent %q: ShouldDispatchAsync should be true", intent)
		}
	}
}
