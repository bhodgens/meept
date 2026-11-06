package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bughunt config-tools F-4 pins: the `origin: user` frontmatter stamp is
// now ENFORCED, not write-only. A non-user funnel overwriting a
// user-stamped skill is refused with ErrUserClobber and the candidate
// lands as SKILL.md.new for a human merge.

const userStampedSkill = "---\nname: prot\ndescription: d\norigin: user\n---\n\nuser body\n"

func writeRaw(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestWriteSkillWithOrigin_UserClobberGuard(t *testing.T) {
	otherOrigins := []Origin{OriginAgent, OriginEvolver, OriginReview, OriginUnknown}
	for _, o := range otherOrigins {
		t.Run("refuse_"+string(o), func(t *testing.T) {
			dir := t.TempDir()
			w := NewWriter(dir, nil)
			writeRaw(t, dir, "prot", userStampedSkill)

			cand := "---\nname: prot\ndescription: d\n---\n\nmachine body\n"
			err := w.WriteSkillWithOrigin("prot", cand, o)
			if !errors.Is(err, ErrUserClobber) {
				t.Fatalf("guard for origin %q: got %v, want ErrUserClobber", o, err)
			}
			// Original bytes intact.
			data, _ := os.ReadFile(filepath.Join(dir, "prot", "SKILL.md"))
			if string(data) != userStampedSkill {
				t.Fatalf("protected file altered:\n%s", data)
			}
			// Candidate preserved in the sidecar.
			side, serr := os.ReadFile(filepath.Join(dir, "prot", "SKILL.md.new"))
			if serr != nil {
				t.Fatalf("sidecar missing: %v", serr)
			}
			if !strings.Contains(string(side), "machine body") {
				t.Fatalf("sidecar lost candidate: %s", side)
			}
		})
	}

	t.Run("allow_user_restamp", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir, nil)
		writeRaw(t, dir, "prot", userStampedSkill)
		// A user-funnel write IS the explicit re-authorization.
		if err := w.WriteSkillWithOrigin("prot", userStampedSkill, OriginUser); err != nil {
			t.Fatalf("user write must be allowed: %v", err)
		}
	})

	t.Run("legacy_unstamped_still_overwritable", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir, nil)
		legacy := "---\nname: prot\ndescription: d\n---\n\nlegacy body\n"
		writeRaw(t, dir, "prot", legacy)
		if err := w.WriteSkillWithOrigin("prot", "new\n", OriginEvolver); err != nil {
			t.Fatalf("legacy (no origin key) must stay overwritable: %v", err)
		}
	})

	t.Run("agent_origin_not_protected", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir, nil)
		writeRaw(t, dir, "prot", "---\nname: prot\ndescription: d\norigin: agent\n---\n\na body\n")
		if err := w.WriteSkillWithOrigin("prot", "b\n", OriginEvolver); err != nil {
			t.Fatalf("origin: agent is not the user class; evolver refine must pass: %v", err)
		}
	})

	t.Run("nested_origin_key_ignored", func(t *testing.T) {
		dir := t.TempDir()
		w := NewWriter(dir, nil)
		nested := "---\nname: prot\ndescription: d\nmeta:\n  origin: user\n---\n\nb\n"
		writeRaw(t, dir, "prot", nested)
		if err := w.WriteSkillWithOrigin("prot", "x\n", OriginEvolver); err != nil {
			t.Fatalf("indented nested origin must not count as the top-level key: %v", err)
		}
	})
}

func TestSkillFileOrigin(t *testing.T) {
	cases := []struct {
		in   string
		want Origin
	}{
		{userStampedSkill, OriginUser},
		{"---\norigin: evolver\n---\n\n", OriginEvolver},
		{"---\nname: x\n---\n\n", OriginUnknown},
		{"no frontmatter", OriginUnknown},
		{"---\nname: x\norigin:\n---\n", OriginUnknown},
		{"---\nname: x\norigin: bogus\n---\n", OriginUnknown},
		{"---\nname: x\norigin: review\n---\n", OriginReview},
	}
	for _, c := range cases {
		if got := skillFileOrigin([]byte(c.in)); got != c.want {
			t.Errorf("skillFileOrigin(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
