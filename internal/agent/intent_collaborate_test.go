package agent

import "testing"

func TestIntentCollaborate_Category(t *testing.T) {
	if IntentCollaborate.Category() != CategoryDefer {
		t.Errorf("category = %q, want defer", IntentCollaborate.Category())
	}
}

func TestIntentCollaborate_DefaultAgent(t *testing.T) {
	if IntentCollaborate.DefaultAgent() != "analyst" {
		t.Errorf("default agent = %q, want analyst", IntentCollaborate.DefaultAgent())
	}
}

func TestIntentCollaborate_ShouldDispatchAsync(t *testing.T) {
	if !IntentCollaborate.ShouldDispatchAsync(false) {
		t.Error("ShouldDispatchAsync should be true")
	}
}

func TestIntentCollaborate_ShouldCreateTask(t *testing.T) {
	if !IntentCollaborate.ShouldCreateTask() {
		t.Error("ShouldCreateTask should be true")
	}
}

func TestIntentCollaborate_IsValid(t *testing.T) {
	if !IsValidIntentType("collaborate") {
		t.Error("'collaborate' should be a valid intent type")
	}
}

func TestIntentCollaborate_Keywords(t *testing.T) {
	kw := IntentCollaborate.Keywords()
	if len(kw) == 0 {
		t.Error("keywords should not be empty")
	}
	hasCollab := false
	for _, k := range kw {
		if k == "collaborate" {
			hasCollab = true
			break
		}
	}
	if !hasCollab {
		t.Error("'collaborate' should be in keywords")
	}
}
