package validator

import (
	"context"
	"regexp"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// Real fixture sentences: detection depends on unicode script ranges, so
// the German and CJK fixtures are genuine native-script text, never
// transliterations.
const (
	englishFixture = "The quick brown fox jumps over the lazy dog. The results of the study show " +
		"that the test was successful, and the team has been able to confirm the first of the two " +
		"hypotheses about how people use the new system in their work."

	germanFixture = "Die Ergebnisse der Untersuchung sind eindeutig und zeigen, dass die " +
		"verschiedenen Verfahren zurzeit nicht auf einem gemeinsamen Stand sind. Die Wissenschaftler " +
		"haben deshalb beschlossen, dass die Studien über mehrere Jahre fortgesetzt werden sollen, " +
		"damit die Unterschiede zwischen den Gruppen besser verstanden werden können."

	chineseFixture = "研究人员今天宣布，他们已经完成了关于机器学习模型在新数据集上的性能评估工作。这项研究表明，" +
		"新的训练方法显著提高了系统在多项任务中的准确率，同时大幅减少了所需的计算资源。团队计划在未来几个月内发布更多详细信息。"

	japaneseFixture = "研究チームは本日、新しい機械学習モデルの性能評価が完了したと発表しました。この研究によれば、" +
		"新しい訓練方法は複数のタスクでの精度を大幅に向上させ、必要な計算資源を大幅に削減しました。"
)

func TestLanguageFilter_Name(t *testing.T) {
	if got := NewLanguageFilter("").Name(); got != "language_en" {
		t.Fatalf("default Name = %q, want language_en", got)
	}
	if got := NewLanguageFilter("de").Name(); got != "language_de" {
		t.Fatalf("Name = %q, want language_de", got)
	}
}

func TestLanguageFilter_Process(t *testing.T) {
	f := NewLanguageFilter("en")
	ctx := context.Background()
	step := &task.TaskStep{}

	langFailPattern := regexp.MustCompile(`^lang=(\S+) confidence=(0\.\d+|1\.00) expected=en$`)

	tests := []struct {
		name    string
		output  string
		want    FilterOutcome
		lang    string // expected captured lang when want == FilterFail
		matchRe *regexp.Regexp
	}{
		{"english passes", englishFixture, FilterPass, "", nil},
		{"empty passes", "", FilterPass, "", nil},
		{"whitespace passes", "  \n ", FilterPass, "", nil},
		{"german fails", germanFixture, FilterFail, "de", langFailPattern},
		{"chinese fails", chineseFixture, FilterFail, "zh", langFailPattern},
		{"japanese fails", japaneseFixture, FilterFail, "ja", langFailPattern},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := f.Process(ctx, step, tc.output)
			if !res.Valid() {
				t.Fatalf("result invalid: %+v", res)
			}
			if res.Outcome != tc.want {
				t.Fatalf("Outcome = %v, want %v (reason=%q)", res.Outcome, tc.want, res.Reason)
			}
			if tc.want == FilterFail {
				m := tc.matchRe.FindStringSubmatch(res.Reason)
				if m == nil {
					t.Fatalf("Reason %q does not match %v", res.Reason, tc.matchRe)
				}
				if m[1] != tc.lang {
					t.Fatalf("detected lang = %q, want %q", m[1], tc.lang)
				}
			}
			// Fail-only filter: NEVER rewrites, so a pass never carries output.
			if res.Output != "" {
				t.Fatalf("language filter must never rewrite, got output %q", res.Output)
			}
		})
	}
}

func TestLanguageFilter_MixedMostlyEnglishPasses(t *testing.T) {
	f := NewLanguageFilter("en")
	ctx := context.Background()
	step := &task.TaskStep{}

	// >50% English: a couple of borrowed German words do not flip the
	// verdict (the 0.5 floor encodes that by design).
	mixed := englishFixture + " The authors also discuss Zeitgeist and Doppelganger in passing."
	if res := f.Process(ctx, step, mixed); res.Outcome != FilterPass {
		t.Fatalf("mixed mostly-English Outcome = %v (reason=%q), want pass", res.Outcome, res.Reason)
	}
}

func TestLanguageFilter_Idempotent(t *testing.T) {
	f := NewLanguageFilter("en")
	ctx := context.Background()
	step := &task.TaskStep{}

	// Pure function of the input: two runs over the same (and over the
	// filter's own, never-rewritten) output give identical verdicts.
	for _, output := range []string{englishFixture, germanFixture, chineseFixture, ""} {
		first := f.Process(ctx, step, output)
		second := f.Process(ctx, step, output)
		if first != second {
			t.Fatalf("not idempotent for %q: %+v vs %+v", output, first, second)
		}
	}
}

func TestLanguageFilter_ExpectedOtherLanguage(t *testing.T) {
	// A German-expecting filter passes German and fails English.
	f := NewLanguageFilter("de")
	ctx := context.Background()
	step := &task.TaskStep{}
	if res := f.Process(ctx, step, germanFixture); res.Outcome != FilterPass {
		t.Fatalf("german under expected=de: Outcome = %v (reason=%q), want pass", res.Outcome, res.Reason)
	}
	res := f.Process(ctx, step, englishFixture)
	if res.Outcome != FilterFail || res.Reason != "lang=en confidence=0.70 expected=de" {
		t.Fatalf("english under expected=de: %+v, want fail lang=en", res)
	}
}
