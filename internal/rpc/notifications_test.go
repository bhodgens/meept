package rpc

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeDNDController is a test double for the DNDController interface.
type fakeDNDController struct {
	dnd bool
}

func (f *fakeDNDController) SetDoNotDisturb(dnd bool) { f.dnd = dnd }
func (f *fakeDNDController) IsDoNotDisturb() bool     { return f.dnd }

func TestNotificationsSetDND(t *testing.T) {
	emitter := &fakeDNDController{}
	h := NewNotificationsHandler(emitter)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterNotificationsHandlers(srv)

	// Enable.
	res, err := srv.CallMethod(context.Background(), "notifications.set_dnd", json.RawMessage(`{"enabled":true}`))
	if err != nil {
		t.Fatalf("set_dnd true: %v", err)
	}
	if !emitter.dnd {
		t.Error("emitter.dnd = false, want true after set")
	}
	m, _ := res.(map[string]any)
	if m["enabled"] != true {
		t.Errorf("result enabled = %v, want true", m["enabled"])
	}

	// Disable.
	res, err = srv.CallMethod(context.Background(), "notifications.set_dnd", json.RawMessage(`{"enabled":false}`))
	if err != nil {
		t.Fatalf("set_dnd false: %v", err)
	}
	if emitter.dnd {
		t.Error("emitter.dnd = true, want false after set")
	}
	m, _ = res.(map[string]any)
	if m["enabled"] != false {
		t.Errorf("result enabled = %v, want false", m["enabled"])
	}

	// Invalid params.
	if _, err := srv.CallMethod(context.Background(), "notifications.set_dnd", json.RawMessage(`{"enabled":"yes"}`)); err == nil {
		t.Error("expected error for invalid params")
	}
}

func TestNotificationsGetDND(t *testing.T) {
	emitter := &fakeDNDController{dnd: false}
	h := NewNotificationsHandler(emitter)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterNotificationsHandlers(srv)

	res, err := srv.CallMethod(context.Background(), "notifications.get_dnd", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("get_dnd: %v", err)
	}
	m, _ := res.(map[string]any)
	if m["enabled"] != false {
		t.Errorf("get_dnd enabled = %v, want false", m["enabled"])
	}

	emitter.SetDoNotDisturb(true)
	res, err = srv.CallMethod(context.Background(), "notifications.get_dnd", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("get_dnd: %v", err)
	}
	m, _ = res.(map[string]any)
	if m["enabled"] != true {
		t.Errorf("get_dnd enabled = %v, want true", m["enabled"])
	}
}

func TestNotificationsNilEmitter(t *testing.T) {
	h := NewNotificationsHandler(nil)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterNotificationsHandlers(srv)

	if _, err := srv.CallMethod(context.Background(), "notifications.get_dnd", json.RawMessage("{}")); err == nil {
		t.Error("expected error with nil emitter on get_dnd")
	}
	if _, err := srv.CallMethod(context.Background(), "notifications.set_dnd", json.RawMessage(`{"enabled":true}`)); err == nil {
		t.Error("expected error with nil emitter on set_dnd")
	}
}
