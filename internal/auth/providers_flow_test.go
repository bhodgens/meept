package auth

import "testing"

func TestFlowKindDefaults(t *testing.T) {
	t.Parallel()
	valid := map[FlowKind]bool{
		FlowDeviceRFC8628: true,
		FlowDeviceCodex:   true,
		FlowPKCEPaste:     true,
		"":                true, // empty means FlowDeviceRFC8628
	}
	for id, cfg := range OAuthProviders {
		if !valid[cfg.Flow] {
			t.Errorf("provider %s: invalid Flow %q", id, cfg.Flow)
		}
	}
}

func TestFlowKindConstants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  FlowKind
		want string
	}{
		{"FlowDeviceRFC8628", FlowDeviceRFC8628, "device_rfc8628"},
		{"FlowDeviceCodex", FlowDeviceCodex, "device_codex"},
		{"FlowPKCEPaste", FlowPKCEPaste, "pkce_paste"},
	}
	for _, tc := range cases {
		if string(tc.got) != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
