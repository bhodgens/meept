package context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSkillMD writes a SKILL.md file into a temp skill directory named by
// slug and returns the file path.
func writeSkillMD(t *testing.T, slug, frontmatter string) string {
	t.Helper()
	skillDir := filepath.Join(t.TempDir(), slug)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	path := filepath.Join(skillDir, "SKILL.md")
	content := frontmatter + "\nBody content.\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestParseSkillFile_RequiresTools(t *testing.T) {
	t.Run("multi-entry list parsed in order", func(t *testing.T) {
		fm := "---\nname: web-shot\ndescription: Take screenshots\nrequires-tools:\n  - web_fetch\n  - cua-driver.capture\n---\n"
		path := writeSkillMD(t, "web-shot", fm)

		skill, err := ParseSkillFile(path)
		require.NoError(t, err)
		require.NotNil(t, skill)
		assert.Equal(t, []string{"web_fetch", "cua-driver.capture"}, skill.RequiresTools)
	})

	t.Run("absent yields nil", func(t *testing.T) {
		fm := "---\nname: plain-skill\ndescription: A plain skill\n---\n"
		path := writeSkillMD(t, "plain-skill", fm)

		skill, err := ParseSkillFile(path)
		require.NoError(t, err)
		require.NotNil(t, skill)
		assert.Empty(t, skill.RequiresTools)
		assert.Nil(t, skill.RequiresTools)
	})

	t.Run("malformed scalar fails yaml unmarshal", func(t *testing.T) {
		// requires-tools given as a single scalar string. yaml.v3 cannot
		// decode a !!str into []string, so ParseSkillFile returns a
		// frontmatter parse error (fail-loud) rather than gracefully
		// string-wrapping. This asserts yaml's actual behavior.
		fm := "---\nname: broken-skill\ndescription: Broken frontmatter\nrequires-tools: web_fetch\n---\n"
		path := writeSkillMD(t, "broken-skill", fm)

		skill, err := ParseSkillFile(path)
		require.Error(t, err)
		assert.Nil(t, skill)
		assert.Contains(t, err.Error(), "failed to parse frontmatter fields")
	})
}
