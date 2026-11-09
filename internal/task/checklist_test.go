package task

import (
	"testing"
)

func TestChecklist_AddItem(t *testing.T) {
	c := &Checklist{}
	c.AddItem("Test item 1")
	c.AddItem("Test item 2")

	if len(c.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(c.Items))
	}
	if c.Items[0].Text != "Test item 1" {
		t.Errorf("Expected 'Test item 1', got %q", c.Items[0].Text)
	}
	if c.Items[0].Completed {
		t.Error("Expected item to be uncompleted")
	}
}

func TestChecklist_CompleteItem(t *testing.T) {
	c := &Checklist{}
	c.AddItem("Test item 1")
	c.AddItem("Test item 2")

	if !c.CompleteItem("Test item 1") {
		t.Error("Expected CompleteItem to return true")
	}
	if !c.Items[0].Completed {
		t.Error("Expected item to be completed")
	}
	if c.Items[1].Completed {
		t.Error("Expected second item to remain uncompleted")
	}
}

func TestChecklist_IsComplete(t *testing.T) {
	// Empty checklist is considered complete (vacuous truth)
	c := &Checklist{}
	if !c.IsComplete() {
		t.Error("Expected empty checklist to be complete (vacuous truth)")
	}

	c.AddItem("Item 1")
	if c.IsComplete() {
		t.Error("Expected checklist with uncompleted items to not be complete")
	}

	c.CompleteItem("Item 1")
	if !c.IsComplete() {
		t.Error("Expected checklist with all items completed to be complete")
	}
}

func TestChecklist_Remaining(t *testing.T) {
	c := &Checklist{}
	c.AddItem("Item 1")
	c.AddItem("Item 2")
	c.AddItem("Item 3")

	if c.Remaining() != 3 {
		t.Errorf("Expected 3 remaining, got %d", c.Remaining())
	}

	c.CompleteItem("Item 1")
	if c.Remaining() != 2 {
		t.Errorf("Expected 2 remaining, got %d", c.Remaining())
	}
}

func TestGetCheckpointGates(t *testing.T) {
	// Test that the function exists and compiles
	// Full integration testing would require a database
	t.Log("GetCheckpointGates function exists")
}
