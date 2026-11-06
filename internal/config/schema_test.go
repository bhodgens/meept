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
	// Summarize-mode defaults (transcript-large-corpus leaf 02): opt-in,
	// model resolution is daemon-side (summarizer_model -> small_model),
	// so the tool-side default model stays empty.
	if c.Transcript.SummarizeEnabled {
		t.Fatal("summarize_enabled must default to false")
	}
	if c.Transcript.SummarizeModel != "" {
		t.Fatalf("summarize_model: %q, want empty", c.Transcript.SummarizeModel)
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

// TestDefaultConfig_SkillsRanking pins the branch-04 leaf-02 opt-in default:
// skills.ranking ships OFF. Absent config key => Enabled=false => the skill
// listing stays alphabetical (leaf-01 behavior, byte-identical). The
// zero TimeoutSeconds means "built-in default" — the wiring applies 2s;
// the schema itself never carries an implicit-on value.
func TestDefaultConfig_SkillsRanking(t *testing.T) {
	c := DefaultConfig()
	if c.Skills.Ranking.Enabled {
		t.Fatal("skills.ranking default must be disabled (opt-in defaults invariant)")
	}
	if c.Skills.Ranking.BaseURL != "" || c.Skills.Ranking.Model != "" {
		t.Fatalf("ranking endpoint fields must default empty: %+v", c.Skills.Ranking)
	}
	if c.Skills.Ranking.TimeoutSeconds != 0 {
		t.Fatalf("timeout default must be 0 (built-in applied at wiring), got %d", c.Skills.Ranking.TimeoutSeconds)
	}
}
