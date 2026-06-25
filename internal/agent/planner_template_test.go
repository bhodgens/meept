package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlannerTemplateLoader_RenderFromBundledTier(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), "---\nname: planner.decompose\n---\nMax={{.MaxSteps}} input={{.Input}}")

	l := newPlannerTemplateLoader(tmp)
	got, err := l.render("planner/decompose.md", map[string]any{
		"MaxSteps": 8,
		"Input":    "hello",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "Max=8 input=hello"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPlannerTemplateLoader_TierPrecedence(t *testing.T) {
	project := t.TempDir()
	user := t.TempDir()
	writeFile(t, filepath.Join(user, "planner", "decompose.md"), "USER")
	writeFile(t, filepath.Join(project, "planner", "decompose.md"), "PROJECT")

	l := newPlannerTemplateLoader(project, user)
	got, err := l.render("planner/decompose.md", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != "PROJECT" {
		t.Errorf("precedence: got %q want PROJECT", got)
	}
}

func TestPlannerTemplateLoader_FallbackWhenMissing(t *testing.T) {
	l := newPlannerTemplateLoader(t.TempDir())
	l.fallbacks["planner/decompose.md"] = "FALLBACK {{.Input}}"

	got, err := l.render("planner/decompose.md", map[string]any{"Input": "x"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "FALLBACK x"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPlannerTemplateLoader_ErrorWhenMissingAndNoFallback(t *testing.T) {
	l := newPlannerTemplateLoader(t.TempDir())
	_, err := l.render("planner/nonexistent.md", nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestPlannerTemplateLoader_StripsYAMLFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	body := "---\nname: planner.decompose\ndescription: x\n---\nHELLO {{.Input}}"
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), body)

	l := newPlannerTemplateLoader(tmp)
	got, _ := l.render("planner/decompose.md", map[string]any{"Input": "world"})
	if strings.Contains(got, "name: planner.decompose") {
		t.Errorf("frontmatter leaked into body: %q", got)
	}
	if !strings.Contains(got, "HELLO world") {
		t.Errorf("body not rendered: %q", got)
	}
}

func TestPlannerTemplateLoader_StripsYAMLFrontmatterCRLF(t *testing.T) {
	tmp := t.TempDir()
	body := "---\r\nname: planner.decompose\r\ndescription: x\r\n---\r\nHELLO {{.Input}}"
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), body)

	l := newPlannerTemplateLoader(tmp)
	got, err := l.render("planner/decompose.md", map[string]any{"Input": "world"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(got, "name: planner.decompose") {
		t.Errorf("frontmatter leaked into body: %q", got)
	}
	if !strings.HasPrefix(got, "HELLO world") {
		t.Errorf("body has stray bytes or did not render: %q", got)
	}
}

func TestPlannerTemplateLoader_MalformedTemplateErrors(t *testing.T) {
	tmp := t.TempDir()
	writeFile(t, filepath.Join(tmp, "planner", "decompose.md"), "{{ .Broken")
	l := newPlannerTemplateLoader(tmp)
	_, err := l.render("planner/decompose.md", nil)
	if err == nil {
		t.Fatal("want parse error, got nil")
	}
}

func TestPlannerTemplateLoader_LegacyFallbacksExecutable(t *testing.T) {
	// The legacy fallback consts must remain valid text/templates so they
	// can serve as drop-in replacements when no bundled markdown file is
	// found on disk. Task 3 wires them into StrategicPlanner; this test
	// guards against drift breaking the fallback path silently.
	t.Run("decompose", func(t *testing.T) {
		l := newPlannerTemplateLoader(t.TempDir())
		l.fallbacks["planner/decompose.md"] = defaultDecomposeFallback()
		got, err := l.render("planner/decompose.md", map[string]any{
			"MaxSteps":       6,
			"ContextSection": "CTX",
			"Input":          "do thing",
		})
		if err != nil {
			t.Fatalf("render decompose fallback: %v", err)
		}
		if !strings.Contains(got, "AT MOST 6 steps") {
			t.Errorf("MaxSteps not interpolated: %q", got)
		}
		if !strings.Contains(got, "do thing") {
			t.Errorf("Input not interpolated: %q", got)
		}
		if !strings.Contains(got, "CTX") {
			t.Errorf("ContextSection not interpolated: %q", got)
		}
	})
	t.Run("interview", func(t *testing.T) {
		l := newPlannerTemplateLoader(t.TempDir())
		l.fallbacks["planner/interview.md"] = defaultInterviewFallback()
		got, err := l.render("planner/interview.md", map[string]any{
			"Request":      "build x",
			"Goal":         "ship",
			"Ambiguity":    "0.4",
			"Scope":        "v1",
			"Category":     "feat",
			"Confidence":   "0.8",
			"Ambiguities":  "none",
		})
		if err != nil {
			t.Fatalf("render interview fallback: %v", err)
		}
		if !strings.Contains(got, "build x") {
			t.Errorf("Request not interpolated: %q", got)
		}
		if !strings.Contains(got, "ship") {
			t.Errorf("Goal not interpolated: %q", got)
		}
	})
	t.Run("decompose_spec", func(t *testing.T) {
		l := newPlannerTemplateLoader(t.TempDir())
		l.fallbacks["planner/decompose_spec.md"] = defaultDecomposeSpecFallback()
		got, err := l.render("planner/decompose_spec.md", map[string]any{
			"MaxStepsPerPhase": 8,
			"MaxPhases":        5,
			"ContextSection":   "CTX-SPEC",
			"Input":            "build feature X",
		})
		if err != nil {
			t.Fatalf("render decompose_spec fallback: %v", err)
		}
		if !strings.Contains(got, "1 and 8 steps") {
			t.Errorf("MaxStepsPerPhase not interpolated: %q", got)
		}
		if !strings.Contains(got, "Maximum 5 phases") {
			t.Errorf("MaxPhases not interpolated: %q", got)
		}
		if !strings.Contains(got, "CTX-SPEC") {
			t.Errorf("ContextSection not interpolated: %q", got)
		}
		if !strings.Contains(got, "build feature X") {
			t.Errorf("Input not interpolated: %q", got)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
