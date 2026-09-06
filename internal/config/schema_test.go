package config

import "testing"

// TestDefaultConfig_SkillsWikiState verifies the frozen defaults for the
// [skills.wiki] and [skills.state] sections (05-config-wiring.md §Contract 5):
// wiki defaults enabled (the store is inert until wired), state defaults
// disabled (opt-in by config; no default flips in this leaf).
// TestTranscriptToolConfigDefaults verifies the frozen defaults for the
// top-level [transcript] section (skill-authoring-and-media-ingest leaf 05,
// Contract 5): disabled by default (external Python dependency, opt-in like
// [browser]), python3, youtube-transcript-api, 60s.
func TestTranscriptToolConfigDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.Transcript.Enabled {
		t.Fatal("transcript default must be disabled")
	}
	if c.Transcript.PythonPath != "python3" {
		t.Fatalf("python_path: %q", c.Transcript.PythonPath)
	}
	if c.Transcript.ModuleName != "youtube-transcript-api" {
		t.Fatalf("module_name: %q", c.Transcript.ModuleName)
	}
	if c.Transcript.TimeoutSeconds != 60 {
		t.Fatalf("timeout_seconds: %d", c.Transcript.TimeoutSeconds)
	}
	if c.Transcript.FallbackOutputDir != "~/.meept/media" {
		t.Fatalf("fallback_output_dir: %q", c.Transcript.FallbackOutputDir)
	}
}

func TestDefaultConfig_SkillsWikiState(t *testing.T) {
	c := DefaultConfig()
	if !c.Skills.Wiki.Enabled {
		t.Fatal("wiki default must be enabled")
	}
	if c.Skills.Wiki.Dir != "~/.meept/wiki" {
		t.Fatalf("dir: %q", c.Skills.Wiki.Dir)
	}
	if c.Skills.State.Enabled {
		t.Fatal("state default must be disabled")
	}
	if c.Skills.State.MaxStateChars != 2000 {
		t.Fatalf("max chars: %d", c.Skills.State.MaxStateChars)
	}
}
