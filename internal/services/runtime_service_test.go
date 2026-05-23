package services

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestRuntimeService_Status_NilManager(t *testing.T) {
	svc := NewRuntimeService(nil)
	_, err := svc.Status(context.Background())
	if err == nil {
		t.Fatal("expected error with nil manager")
	}
}

func TestRuntimeService_StatusForProvider_NilManager(t *testing.T) {
	svc := NewRuntimeService(nil)
	_, err := svc.StatusForProvider(context.Background(), "local")
	if err == nil {
		t.Fatal("expected error with nil manager")
	}
}

func TestRuntimeService_Start_NilManager(t *testing.T) {
	svc := NewRuntimeService(nil)
	err := svc.StartProvider(context.Background(), "local")
	if err == nil {
		t.Fatal("expected error with nil manager")
	}
}

func TestRuntimeService_Status_Empty(t *testing.T) {
	mgr := llm.NewRuntimeManager(nil)
	svc := NewRuntimeService(mgr)

	resp, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Runtimes) != 0 {
		t.Errorf("expected 0 runtimes, got %d", len(resp.Runtimes))
	}
}
