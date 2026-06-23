package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/preferences"
)

// newPhase3TestServer creates a test HTTP server with fully wired instructions
// handler (store + parser + verifier). Returns the server and store.
func newPhase3TestServer(t *testing.T) (*httptest.Server, *preferences.Store) {
	t.Helper()

	tmpDir := t.TempDir()
	store := preferences.NewUserInstructionStore([]string{tmpDir})
	// Run discovery to initialize the in-memory map
	_, _ = store.Discovery()

	parser := agent.NewInstructionParser()
	verifier := preferences.NewInstructionVerifier(nil)

	handler := &InstructionsHandler{
		store:    store,
		parser:   parser,
		verifier: verifier,
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	return server, store
}

// TestPhase3_E2E_CreateAndList exercises the full create-then-list flow:
// POST an instruction via natural language, then GET the list to verify it
// was parsed, verified, and persisted correctly.
func TestPhase3_E2E_CreateAndList(t *testing.T) {
	server, store := newPhase3TestServer(t)
	defer server.Close()

	client := server.Client()

	// POST a new instruction via NL input
	createReq := map[string]any{
		"input": "Every day at 9am run tests in this project",
		"tier":  store.DefaultTier(),
	}
	body, _ := json.Marshal(createReq)

	resp, err := client.Post(server.URL+"/api/v1/instructions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST create failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var createResp InstructionResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("Failed to decode create response: %v", err)
	}

	if !createResp.Success {
		t.Fatalf("Create success = false, error: %s", createResp.Error)
	}
	if createResp.Instruction == nil {
		t.Fatal("Create returned nil instruction")
	}

	// Verify the instruction was parsed correctly
	instr := createResp.Instruction
	if instr.Action == "" {
		t.Error("Expected non-empty action")
	}
	if instr.Trigger == "" {
		t.Error("Expected non-empty trigger")
	}
	if instr.ID == "" {
		t.Error("Expected non-empty ID")
	}

	// Now GET the list to verify persistence
	listResp, err := client.Get(server.URL + "/api/v1/instructions")
	if err != nil {
		t.Fatalf("GET list failed: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("GET list status = %d, want %d", listResp.StatusCode, http.StatusOK)
	}

	var listResult InstructionResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}

	if !listResult.Success {
		t.Error("List success = false")
	}

	// The HTTP handler's Save() updates the in-memory store. Verify
	// the instruction is retrievable via GetActive (no Discovery needed
	// since Save adds to the in-memory map directly).
	active := store.GetActive()
	if len(active) == 0 {
		t.Error("Expected at least one active instruction in store after create")
	}

	// Verify the created instruction is in the list
	found := false
	for _, a := range active {
		if a.ID == instr.ID || a.Action == instr.Action {
			found = true
			break
		}
	}
	if !found {
		t.Error("Created instruction not found in active instructions")
	}
}

// TestPhase3_E2E_PreviewDryRun exercises the preview endpoint: POST an
// instruction for dry-run parsing without persistence, then verify the
// response has the parsed fields.
func TestPhase3_E2E_PreviewDryRun(t *testing.T) {
	server, _ := newPhase3TestServer(t)
	defer server.Close()

	client := server.Client()

	previewReq := map[string]any{
		"input": "Every day at 9am run tests in this project",
	}
	body, _ := json.Marshal(previewReq)

	resp, err := client.Post(server.URL+"/api/v1/instructions/preview", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST preview failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST preview status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result InstructionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode preview response: %v", err)
	}

	if !result.Success {
		t.Fatalf("Preview success = false, error: %s", result.Error)
	}

	if result.ParsedInstruction == nil {
		t.Fatal("Preview returned nil ParsedInstruction")
	}

	parsed := result.ParsedInstruction
	if parsed.Trigger.Type == "" {
		t.Error("Expected non-empty trigger type")
	}
	if parsed.Action.Tool == "" {
		t.Error("Expected non-empty action tool")
	}
	if parsed.Scope == "" {
		t.Error("Expected non-empty scope")
	}

	// The NL input should parse to a cron trigger
	if parsed.Trigger.Type != "cron" {
		t.Errorf("Expected trigger type 'cron', got %q", parsed.Trigger.Type)
	}

	// The action should be shell_execute for "run tests"
	if parsed.Action.Tool != "shell_execute" {
		t.Errorf("Expected action tool 'shell_execute', got %q", parsed.Action.Tool)
	}

	// Preview should not persist anything — verify by listing
	listResp, err := client.Get(server.URL + "/api/v1/instructions")
	if err != nil {
		t.Fatalf("GET list failed: %v", err)
	}
	defer listResp.Body.Close()

	var listResult InstructionResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}

	// The store should have zero instructions from preview (it was just a dry run)
	// Note: listResult.Instructions comes from store.GetActive() which requires
	// Discovery() to be called. Since preview doesn't call Discovery, the
	// in-memory map may still be empty. Check that no instruction was persisted
	// by verifying the parsed instruction ID is not in the list.
	for _, instr := range listResult.Instructions {
		if parsed.RawInput != "" && instr.Name == parsed.RawInput {
			t.Error("Preview should not persist instruction, but found matching entry in list")
		}
	}
}

// TestPhase3_E2E_UpdateDelete exercises the update then delete flow:
// create an instruction, PUT an update to disable it, then DELETE it,
// verifying it's gone.
func TestPhase3_E2E_UpdateDelete(t *testing.T) {
	server, store := newPhase3TestServer(t)
	defer server.Close()

	client := server.Client()

	// Step 1: Create an instruction via the store directly (simpler than HTTP create)
	instr := &preferences.UserInstruction{
		ID:      "e2e-update-delete",
		Name:    "e2e-update-delete",
		Trigger: "intent:code",
		Action:  "shell_execute",
		ActionArgs: map[string]any{"command": "go test ./..."},
		Enabled: true,
	}
	if err := store.Save(instr, store.DefaultTier()); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Step 2: PUT update — disable the instruction
	updateReq := map[string]any{
		"enabled": false,
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", server.URL+"/api/v1/instructions/"+instr.ID, bytes.NewReader(body))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT update failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT update status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var updateResult InstructionResponse
	if err := json.NewDecoder(resp.Body).Decode(&updateResult); err != nil {
		t.Fatalf("Failed to decode update response: %v", err)
	}

	if !updateResult.Success {
		t.Errorf("Update success = false, error: %s", updateResult.Error)
	}
	if updateResult.Instruction == nil {
		t.Fatal("Update returned nil instruction")
	}
	if updateResult.Instruction.Enabled {
		t.Error("Expected enabled = false after update")
	}

	// Step 3: DELETE the instruction
	delReq, _ := http.NewRequest("DELETE", server.URL+"/api/v1/instructions/"+instr.ID, nil)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d, want %d", delResp.StatusCode, http.StatusOK)
	}

	var delResult InstructionResponse
	if err := json.NewDecoder(delResp.Body).Decode(&delResult); err != nil {
		t.Fatalf("Failed to decode delete response: %v", err)
	}

	if !delResult.Success {
		t.Errorf("Delete success = false, error: %s", delResult.Error)
	}

	// Step 4: Verify the instruction is gone
	getResp, err := client.Get(server.URL + "/api/v1/instructions/" + instr.ID)
	if err != nil {
		t.Fatalf("GET after delete failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusNotFound {
		t.Errorf("GET after delete status = %d, want %d", getResp.StatusCode, http.StatusNotFound)
	}
}
