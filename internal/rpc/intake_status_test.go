package rpc

import (
	"context"
	"testing"
)

func TestIntakeStatusHandler_DisabledDefault(t *testing.T) {
	h := NewIntakeStatusHandler(nil, nil)
	res, err := h.handleIntakeStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := res.(IntakeStatusView)
	if !ok {
		t.Fatalf("response type = %T, want IntakeStatusView", res)
	}
	if v.Enabled || v.PollerScheduled {
		t.Errorf("disabled default must be enabled=false poller_scheduled=false, got %+v", v)
	}
	if v.Note == "" {
		t.Error("disabled default must carry an explanatory note")
	}
}

func TestIntakeStatusHandler_EnabledEchoesConfig(t *testing.T) {
	cfg := IntakeStatusView{
		Enabled:         true,
		WatchRepo:       "bhodgens/meept",
		PollInterval:    "10m",
		RequiredLabels:  []string{"factory"},
		DoneLabel:       "factory:done",
		DefaultEmployee: "eng-1",
		GateCommand:     "go test -p 2 ./...",
		ProjectNumber:   7,
	}
	h := NewIntakeStatusHandler(func() IntakeStatusView { return cfg }, func() bool { return true })
	res, err := h.handleIntakeStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v := res.(IntakeStatusView)
	if v.WatchRepo != cfg.WatchRepo || v.PollInterval != cfg.PollInterval ||
		v.DoneLabel != cfg.DoneLabel || v.DefaultEmployee != cfg.DefaultEmployee ||
		v.GateCommand != cfg.GateCommand || v.ProjectNumber != cfg.ProjectNumber ||
		len(v.RequiredLabels) != 1 || v.RequiredLabels[0] != "factory" {
		t.Errorf("enabled config fields not echoed: %+v", v)
	}
	if !v.PollerScheduled {
		t.Error("poller_scheduled must follow the probe when enabled")
	}
}

func TestIntakeStatusHandler_EnabledButPollerMissing(t *testing.T) {
	h := NewIntakeStatusHandler(
		func() IntakeStatusView { return IntakeStatusView{Enabled: true} },
		func() bool { return false },
	)
	res, _ := h.handleIntakeStatus(context.Background(), nil)
	v := res.(IntakeStatusView)
	if !v.Enabled || v.PollerScheduled {
		t.Errorf("enabled-but-unscheduled must be enabled=true poller_scheduled=false, got %+v", v)
	}
}

func TestIntakeStatusHandler_NilPollerProbeIsFalse(t *testing.T) {
	h := NewIntakeStatusHandler(func() IntakeStatusView { return IntakeStatusView{Enabled: true} }, nil)
	res, _ := h.handleIntakeStatus(context.Background(), nil)
	if v := res.(IntakeStatusView); v.PollerScheduled {
		t.Error("a nil probe must report poller_scheduled=false, never panic")
	}
}

func TestIntakeStatusHandler_SetterNilGuards(t *testing.T) {
	h := NewIntakeStatusHandler(nil, nil)
	h.SetConfigFunc(nil)
	h.SetPollerScheduledFunc(nil)
	if _, err := h.handleIntakeStatus(context.Background(), nil); err != nil {
		t.Fatalf("nil-guarded setters must leave the handler callable: %v", err)
	}
}
