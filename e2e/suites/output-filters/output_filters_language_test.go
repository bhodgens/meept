//go:build e2e

// Configurable-output-language e2e coverage (expected_language +
// data-driven word tables). Three contracts the unit tests cannot pin:
//
//  1. expected_language=zh — a Chinese-script step result PASSES the
//     language filter on a default chain (the script detector is
//     authoritative per-rune; zero extra config).
//  2. expected_language=fr with a user word table at
//     $MEEPT_HOME/validator/lang/fr.txt — French prose passes and the
//     SAME prose fails when the config expects English (the table is
//     what makes French detectable; without it Latin-script French is
//     indistinguishable from English by hit-rate).
//  3. The word-table file is loaded at daemon boot from the sandboxed
//     MEEPT_HOME — the daemon wiring seam, not just the loader function.
package outputfilters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// chineseResult is unambiguously Chinese: dense Han-script prose, no
// Latin words, so per-rune detection settles it at confidence 1.0.
const chineseResult = "分析已完成，结果已写入指定文件，所有数据均已核对无误，可以开始下一步工作了。"

// frenchResult is dense French function words — ~0.7 hit-rate against
// the seeded table, comfortably above the 0.5 detection floor even if
// the filter sees a longer variant of the text.
const frenchResult = "Le fichier est dans la table avec les donnees du client et il a ete fait " +
	"par notre equipe pour vous avec plus de soins et de tres bons resultats dans la verification."

// seedLangTable writes a French function-word table into the sandboxed
// MEEPT_HOME before the daemon boots.
func seedLangTable(s *harness.Stack) error {
	dir := filepath.Join(s.MeeptHome, "validator", "lang")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// >20 words: articles, pronouns, common verbs, prepositions.
	fr := `# french function words
le la les un une des du de au aux et ou mais donc or ni car je tu il elle nous vous ils elles
ce cet cette ces mon ton son ma ta sa mes tes ses notre votre leurs qui que quoi dont est sont
etait etaient a ai as ont avons avez sera seront etre avoir fait faire plus moins tres dans
sur avec pour par
`
	return os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(fr), 0o644)
}

// waitForAnyTaskTerminal waits for at least one task row and returns its id
// once it reaches a terminal state.
func waitForAnyTaskTerminal(t *testing.T, s *harness.Stack) string {
	t.Helper()
	var taskID string
	harness.WaitFor(t, 20*time.Second, "task row", func() bool {
		tasks := harness.Tasks(t, s.TasksDBPath())
		if len(tasks) > 0 {
			taskID = tasks[len(tasks)-1].ID
			return true
		}
		return false
	})
	deadline := time.Now().Add(150 * time.Second)
	var state string
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID {
				state = row.State
			}
		}
		if state == "completed" || state == "failed" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if state != "completed" && state != "failed" {
		t.Fatalf("task %s stuck non-terminal (state %q)", taskID, state)
	}
	return taskID
}

// stepResults returns every step result string for the task.
func stepResults(t *testing.T, s *harness.Stack, taskID string) []string {
	t.Helper()
	var out []string
	for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
		if st.Result != "" {
			out = append(out, st.Result)
		}
	}
	return out
}

// TestExpectedLanguageChinesePassesDefaultChain: expected_language=zh and a
// Chinese step result — the task completes and the Chinese text survives a
// step result (the language filter did NOT reject it).
func TestExpectedLanguageChinesePassesDefaultChain(t *testing.T) {
	s := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"daemon.output_filters.expected_language": "zh",
		}),
	)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "langzh", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "lang-zh.txt")
	s.Fake.SetPostToolText(chineseResult)
	s.Fake.EnqueueFileWrite("call-langzh", artifact, "分析结果")

	s.SubmitChatHTTP(t, sessionID, "Create a file named lang-zh.txt containing the analysis")

	taskID := waitForAnyTaskTerminal(t, s)

	found := false
	for _, r := range stepResults(t, s, taskID) {
		if strings.Contains(r, "分析已完成") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected_language=zh: Chinese result rejected or rewritten; steps:\n%s",
			harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), taskID)))
	}
}

// TestExpectedLanguageFrenchWithUserWordTable: with a seeded fr.txt, the
// SAME French prose is REJECTED when the chain expects English (detection
// names it "fr" at rate >= 0.5) and PASSES when expected_language=fr.
// Both stacks share the word table; the config is the only difference —
// which is exactly the M10 contract: expected_language decides, data
// makes the language detectable at all.
func TestExpectedLanguageFrenchWithUserWordTable(t *testing.T) {
	// Arm A: default English expectation + the table — French prose is
	// named "fr" and must NOT survive untouched as the shipped answer.
	sEn := harness.Start(t, harness.WithPreBootHook(seedLangTable))
	sEn.RegisterProject(t, "e2e-project")
	sessionEn := sEn.CreateSession(t, "langen", sEn.ProjectDir)

	artifactEn := filepath.Join(sEn.ProjectDir, "lang-en.txt")
	sEn.Fake.SetPostToolText(frenchResult)
	sEn.Fake.EnqueueFileWrite("call-lang-en", artifactEn, "analyse")

	sEn.SubmitChatHTTP(t, sessionEn, "Create a file named lang-en.txt containing analyse")

	taskEn := waitForAnyTaskTerminal(t, sEn)
	// The honest arm-A signal is the store's filter accounting: the step
	// that carried the French prose must carry filter_error naming the fr
	// detection, and the task must NOT be completed (rejection exhausted
	// the retry budget). The raw prose still appears inside the stored
	// result JSON — rejection does not launder the record.
	var sawFilterError bool
	for _, st := range harness.Steps(t, sEn.TasksDBPath(), taskEn) {
		if strings.Contains(st.FilterError, "lang=fr") {
			sawFilterError = true
		}
	}
	taskFailed := false
	for _, row := range harness.Tasks(t, sEn.TasksDBPath()) {
		if row.ID == taskEn && row.State == "failed" {
			taskFailed = true
		}
	}
	if !sawFilterError || !taskFailed {
		t.Fatalf("english-expecting chain (with fr.txt) did not reject the French step: filter_error_seen=%v task_failed=%v",
			sawFilterError, taskFailed)
	}

	// Arm B: expected_language=fr + the SAME table — the SAME prose
	// passes and survives a step result.
	sFr := harness.Start(t,
		harness.WithConfigOverlay(map[string]any{
			"daemon.output_filters.expected_language": "fr",
		}),
		harness.WithPreBootHook(seedLangTable),
	)
	sFr.RegisterProject(t, "e2e-project")
	sessionFr := sFr.CreateSession(t, "langfr", sFr.ProjectDir)

	artifactFr := filepath.Join(sFr.ProjectDir, "lang-fr.txt")
	sFr.Fake.SetPostToolText(frenchResult)
	sFr.Fake.EnqueueFileWrite("call-lang-fr", artifactFr, "analyse")

	sFr.SubmitChatHTTP(t, sessionFr, "Create a file named lang-fr.txt containing analyse")

	taskFr := waitForAnyTaskTerminal(t, sFr)
	found := false
	for _, r := range stepResults(t, sFr, taskFr) {
		if strings.Contains(r, "Le fichier est dans la table") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected_language=fr with fr.txt: French result rejected or rewritten; steps:\n%s",
			harness.FormatSteps(harness.Steps(t, sFr.TasksDBPath(), taskFr)))
	}
}
