package theme

import "testing"

func TestParseEmbeddedTokens(t *testing.T) {
	tokens, err := Parse(TokensJSON5)
	if err != nil {
		t.Fatalf("embedded tokens.json5 failed to parse: %v", err)
	}
	for _, v := range FrozenVariants {
		for _, r := range FrozenRoles {
			hex := tokens.Hex(v, r)
			if hex == "" {
				t.Errorf("variant %q missing role %q", v, r)
			}
		}
	}
}

func TestParseRejectsBadStructure(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"missing variant", `{"cyberpunk":{"primary":"#FF6600"}}`},
		{"unknown role", func() string {
			s := `{`
			for i, v := range FrozenVariants {
				if i > 0 {
					s += `,`
				}
				s += `"` + v + `":{"primary":"#FF6600","bogus_role":"#123456"`
				for _, r := range FrozenRoles[1:] {
					s += `,"` + r + `":"#123456"`
				}
				s += `}`
			}
			return s + `}`
		}()},
		{"bad hex", func() string {
			s := `{`
			for i, v := range FrozenVariants {
				if i > 0 {
					s += `,`
				}
				s += `"` + v + `":{`
				for j, r := range FrozenRoles {
					if j > 0 {
						s += `,`
					}
					val := "#123456"
					if r == "primary" && v == "cyberpunk" {
						val = "orange"
					}
					s += `"` + r + `":"` + val + `"`
				}
				s += `}`
			}
			return s + `}`
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.json)); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// TestCyberpunkMatchesTodayGUI pins the no-visual-diff guarantee: the shared
// roles that existed in the Flutter palette keep their exact pre-theming hex.
func TestCyberpunkMatchesTodayGUI(t *testing.T) {
	tokens, err := Parse(TokensJSON5)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"background":  "#000000",
		"surface":     "#1A1A1A",
		"surfaceAlt":  "#2A2A2A",
		"border":      "#333333",
		"error":       "#FF3366", // redAlert
		"warning":     "#FFCC00", // yellowWarning
		"info":        "#3399FF", // blueInfo
		"secondary":   "#00FFAA", // greenSuccess
		"success":     "#00FFAA",
		"textPrimary": "#E0E0E0", // veryLightGray
		"textMuted":   "#6B7280", // TUI ColorMuted
	}
	for role, hex := range want {
		got := tokens.Hex("cyberpunk", role)
		if got != hex {
			t.Errorf("cyberpunk.%s = %s, want %s (GUI parity)", role, got, hex)
		}
	}
}
