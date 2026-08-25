package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestStartDeviceFlow_FormEncoded(t *testing.T) {
	var gotContentType string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceCode:      "dc_form",
			UserCode:        "ABCD-1234",
			VerificationURI: "https://example.com/device",
			ExpiresIn:       900,
			Interval:        5,
		})
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID:    "cid",
		DeviceEP:    srv.URL,
		Scopes:      []string{"openid", "email"},
		FormEncoded: true,
	}

	result, err := StartDeviceFlow(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartDeviceFlow: %v", err)
	}
	if result == nil || result.DeviceCode != "dc_form" {
		t.Fatalf("device code = %+v, want dc_form", result)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", gotContentType)
	}
	vals, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("url.ParseQuery(%q): %v", gotBody, err)
	}
	if got := vals.Get("client_id"); got != "cid" {
		t.Errorf("client_id = %q, want cid", got)
	}
	if got := vals.Get("scope"); got != "openid email" {
		t.Errorf("scope = %q, want \"openid email\"", got)
	}
}

func TestStartDeviceFlow_JSONDefault(t *testing.T) {
	var gotContentType string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = string(body)
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceCode:      "dc_json",
			UserCode:        "ABCD-1234",
			VerificationURI: "https://example.com/device",
			ExpiresIn:       900,
			Interval:        5,
		})
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "cid",
		DeviceEP: srv.URL,
		Scopes:   []string{"models:read"},
	}

	if _, err := StartDeviceFlow(context.Background(), cfg); err != nil {
		t.Fatalf("StartDeviceFlow: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if !strings.Contains(gotBody, `"client_id":"cid"`) {
		t.Errorf("body = %q, want JSON body containing client_id", gotBody)
	}
}
