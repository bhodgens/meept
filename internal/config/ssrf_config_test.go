package config

import (
	"strings"
	"testing"
)

func TestDefaultConfig_SSRFEnabledByDefault(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Security.SSRF.Enabled {
		t.Error("security.ssrf.enabled default = false, want true")
	}
	if cfg.Security.SSRF.MaxRedirects != 5 {
		t.Errorf("security.ssrf.max_redirects default = %d, want 5", cfg.Security.SSRF.MaxRedirects)
	}
}

func TestSecurityConfigValidate_SSRF(t *testing.T) {
	base := func() SecurityConfig {
		cfg := DefaultConfig()
		return cfg.Security
	}

	valid := base()
	valid.SSRF.AllowedCIDRs = []string{"10.0.0.0/8", ""}
	valid.SSRF.BlockedCIDRs = []string{"192.168.0.0/16"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid SSRF config rejected: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*SecurityConfig)
		wantSub string
	}{
		{
			"invalid allowed CIDR",
			func(c *SecurityConfig) { c.SSRF.AllowedCIDRs = []string{"not-a-cidr"} },
			"invalid CIDR",
		},
		{
			"invalid blocked CIDR",
			func(c *SecurityConfig) { c.SSRF.BlockedCIDRs = []string{"10.0.0.0/99"} },
			"invalid CIDR",
		},
		{
			"negative max redirects",
			func(c *SecurityConfig) { c.SSRF.MaxRedirects = -3 },
			"max_redirects",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(&c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate() accepted %+v", c.SSRF)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}
