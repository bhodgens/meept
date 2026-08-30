package agents

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parseGateMD parses AGENT.md text and returns the gate metadata (nil when
// absent).
func parseGateMD(t *testing.T, text string) *GateMetadata {
	t.Helper()
	def, err := ParseAgentText(text)
	if err != nil {
		t.Fatalf("ParseAgentText: %v", err)
	}
	return def.Gate
}

func TestGateFrontmatterParsing(t *testing.T) {
	tests := []struct {
		name string
		fm   string
		want *GateMetadata
	}{
		{
			name: "no gate block",
			fm:   "id: coder\nname: Code Specialist",
			want: nil,
		},
		{
			name: "full gate block",
			fm:   "id: coder\nname: Code Specialist\ngate:\n  command: \"go test ./...\"\n  timeout_seconds: 300\n  skip_when_unchanged: true",
			want: &GateMetadata{
				Command:           "go test ./...",
				TimeoutSeconds:    300,
				SkipWhenUnchanged: true,
			},
		},
		{
			name: "command only",
			fm:   "id: coder\nname: Code Specialist\ngate:\n  command: make check",
			want: &GateMetadata{
				Command:           "make check",
				TimeoutSeconds:    0, // defaulted only at NormalizeGateDefaults
				SkipWhenUnchanged: true,
			},
		},
		{
			name: "explicit skip_when_unchanged false is preserved",
			fm:   "id: coder\nname: Code Specialist\ngate:\n  command: make check\n  skip_when_unchanged: false",
			want: &GateMetadata{
				Command:           "make check",
				TimeoutSeconds:    0,
				SkipWhenUnchanged: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGateMD(t, "---\n"+tt.fm+"\n---\n\n# Body\n\nBody text.\n")
			if tt.want == nil {
				if got != nil {
					t.Fatalf("Gate = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("Gate = nil, want %+v", tt.want)
			}
			if got.Command != tt.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tt.want.Command)
			}
			if got.TimeoutSeconds != tt.want.TimeoutSeconds {
				t.Errorf("TimeoutSeconds = %d, want %d", got.TimeoutSeconds, tt.want.TimeoutSeconds)
			}
			if got.SkipWhenUnchanged != tt.want.SkipWhenUnchanged {
				t.Errorf("SkipWhenUnchanged = %v, want %v", got.SkipWhenUnchanged, tt.want.SkipWhenUnchanged)
			}
		})
	}
}

func TestGateYAMLRoundtrip(t *testing.T) {
	orig := GateMetadata{
		Command:           "go test ./...",
		TimeoutSeconds:    300,
		SkipWhenUnchanged: true,
	}
	data, err := yaml.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var parsed GateMetadata
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Compare exported fields; the unexported skipExplicit marker is parse
	// provenance and legitimately differs (marshal emits the key, so re-parse
	// sees it as explicit).
	if parsed.Command != orig.Command || parsed.TimeoutSeconds != orig.TimeoutSeconds || parsed.SkipWhenUnchanged != orig.SkipWhenUnchanged {
		t.Fatalf("roundtrip mismatch:\n got %+v\nwant %+v", parsed, orig)
	}
}

func TestGateNormalizeDefaults(t *testing.T) {
	tests := []struct {
		name        string
		in          GateMetadata
		wantTimeout int
		wantSkip    bool
	}{
		{name: "all unset", in: GateMetadata{Command: "true"}, wantTimeout: 300, wantSkip: true},
		{name: "explicit timeout", in: GateMetadata{Command: "true", TimeoutSeconds: 42}, wantTimeout: 42, wantSkip: true},
		{name: "negative timeout", in: GateMetadata{Command: "true", TimeoutSeconds: -5}, wantTimeout: 300, wantSkip: true},
		{
			// Parsed frontmatter with an explicit false must survive
			// normalization (skipExplicit set by UnmarshalYAML).
			name:        "parsed explicit false skip preserved",
			in:          mustParseGate(t, "id: coder\ngate:\n  command: make check\n  skip_when_unchanged: false"),
			wantTimeout: 300,
			wantSkip:    false,
		},
		{
			name:        "parsed absent skip defaults true",
			in:          mustParseGate(t, "id: coder\ngate:\n  command: make check"),
			wantTimeout: 300,
			wantSkip:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.in
			g.NormalizeGateDefaults()
			if g.TimeoutSeconds != tt.wantTimeout {
				t.Errorf("TimeoutSeconds = %d, want %d", g.TimeoutSeconds, tt.wantTimeout)
			}
			if g.SkipWhenUnchanged != tt.wantSkip {
				t.Errorf("SkipWhenUnchanged = %v, want %v", g.SkipWhenUnchanged, tt.wantSkip)
			}
		})
	}
}

// mustParseGate parses frontmatter text into a GateMetadata via the real
// parser (exercising UnmarshalYAML).
func mustParseGate(t *testing.T, fm string) GateMetadata {
	t.Helper()
	g := parseGateMD(t, "---\n"+fm+"\n---\n\nBody.\n")
	if g == nil {
		t.Fatal("gate block missing")
	}
	return *g
}

func TestGateBundledAgentMDHasGate(t *testing.T) {
	// The bundled coder/debugger AGENT.md files ship a gate block. This test
	// runs against the real files when the repo layout is present.
	for _, agentID := range []string{"coder", "debugger"} {
		path := "../../config/agents/" + agentID + "/AGENT.md"
		def, err := ParseAgentFile(path)
		if err != nil {
			t.Logf("bundled %s AGENT.md not readable from test env: %v", agentID, err)
			continue
		}
		if def.Gate == nil {
			t.Errorf("%s AGENT.md has no gate block", agentID)
			continue
		}
		if def.Gate.Command == "" {
			t.Errorf("%s gate.command is empty", agentID)
		}
		if def.Gate.TimeoutSeconds <= 0 {
			t.Errorf("%s gate.timeout_seconds unset (%d)", agentID, def.Gate.TimeoutSeconds)
		}
	}
	if t.Failed() {
		t.Fail()
	}
}

func TestGateMarshalOmitsEmptyCommand(t *testing.T) {
	data, err := yaml.Marshal(GateMetadata{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "command") {
		t.Errorf("empty GateMetadata should omit command, got:\n%s", data)
	}
}
