package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caimlas/meept/internal/preferences"
)

// TestInstructionsHandlerCRUD tests the CRUD operations for instructions.
func TestInstructionsHandlerCRUD(t *testing.T) {
	store := preferences.NewUserInstructionStore(preferences.DefaultTiers)

	handler := &InstructionsHandler{
		store: store,
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	// First, manually create an instruction via the store
	instr := &preferences.UserInstruction{
		ID:      "test-instr-1",
		Trigger: "intent:code",
		Action:  "shell",
		Enabled: true,
	}
	store.Save(instr, "project")

	t.Run("List instructions", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/api/v1/instructions")
		if err != nil {
			t.Fatalf("Failed to list instructions: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result InstructionResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true")
		}
		if len(result.Instructions) == 0 {
			t.Error("Expected at least one instruction")
		}
	})

	t.Run("Get instruction by ID", func(t *testing.T) {
		resp, err := client.Get(server.URL + "/api/v1/instructions/test-instr-1")
		if err != nil {
			t.Fatalf("Failed to get instruction: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result InstructionResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true")
		}
		if result.Instruction == nil {
			t.Fatal("Expected instruction in response")
		}
		if result.Instruction.ID != "test-instr-1" {
			t.Errorf("Expected ID test-instr-1, got %s", result.Instruction.ID)
		}
	})
}

// TestInstructionsHandlerDelete tests instruction deletion.
func TestInstructionsHandlerDelete(t *testing.T) {
	store := preferences.NewUserInstructionStore(preferences.DefaultTiers)

	handler := &InstructionsHandler{
		store: store,
	}

	// Create an instruction first
	instr := &preferences.UserInstruction{
		ID:      "test-instr-delete",
		Trigger: "intent:code",
		Action:  "shell",
		Enabled: true,
	}
	store.Save(instr, "project")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	// Delete the instruction
	req, _ := http.NewRequest("DELETE", server.URL+"/api/v1/instructions/test-instr-delete", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify it's deleted
	getReq, _ := http.NewRequest("GET", server.URL+"/api/v1/instructions/test-instr-delete", nil)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 after delete, got %d", getResp.StatusCode)
	}
}

// TestInstructionsHandlerUpdate tests instruction updates.
func TestInstructionsHandlerUpdate(t *testing.T) {
	store := preferences.NewUserInstructionStore(preferences.DefaultTiers)

	handler := &InstructionsHandler{
		store: store,
	}

	// Create an instruction first
	instr := &preferences.UserInstruction{
		ID:      "test-instr-update",
		Trigger: "intent:code",
		Action:  "shell",
		Enabled: true,
	}
	store.Save(instr, "project")

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	t.Run("Update enabled flag", func(t *testing.T) {
		payload := map[string]any{
			"enabled": false,
		}
		body, _ := json.Marshal(payload)

		req, _ := http.NewRequest("PUT", server.URL+"/api/v1/instructions/test-instr-update", bytes.NewReader(body))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		var result InstructionResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success: %s", result.Error)
		}
		if result.Instruction == nil {
			t.Fatal("Expected instruction in response")
		}
		if result.Instruction.Enabled {
			t.Error("Expected enabled=false")
		}
	})
}

// TestInstructionsHandlerRegisterRoutes verifies routes are registered.
func TestInstructionsHandlerRegisterRoutes(t *testing.T) {
	store := preferences.NewUserInstructionStore(preferences.DefaultTiers)
	handler := &InstructionsHandler{
		store: store,
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Routes registered successfully if we got here without panic
	t.Log("Routes registered successfully")
}
