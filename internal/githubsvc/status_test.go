package githubsvc

import (
	"testing"

	"github.com/google/go-github/v93/github"
)

// NOTE: gate test for contract 3 (SummarizePR). Intentionally RED until
// leaf 01 lands status.go — SummarizePR and the go-github v93 import do not
// exist yet.

func TestSummarizePR_AllGreen(t *testing.T) {
	pr := &github.PullRequest{
		Number:    github.Int(7),
		State:     github.String("open"),
		Draft:     github.Bool(false),
		Mergeable: github.Bool(true),
		Head:      &github.PullRequestBranch{SHA: github.String("abc123")},
	}
	checks := []*github.CheckRun{
		{Name: github.String("lint"), Status: github.String("completed"), Conclusion: github.String("success")},
		{Name: github.String("tests"), Status: github.String("completed"), Conclusion: github.String("success")},
	}
	workflows := []*github.WorkflowRun{
		{Name: github.String("ci"), Status: github.String("completed"), Conclusion: github.String("success")},
	}

	got := SummarizePR(pr, checks, workflows)

	if got.Number != 7 {
		t.Errorf("Number = %d, want 7", got.Number)
	}
	if !got.AllGreen {
		t.Errorf("AllGreen = false, want true when every check and workflow concluded success")
	}
	if got.Head != "abc123" {
		t.Errorf("Head = %q, want %q", got.Head, "abc123")
	}
	if got.State != "open" {
		t.Errorf("State = %q, want %q", got.State, "open")
	}
}

func TestSummarizePR_NotGreenWhenCheckFails(t *testing.T) {
	pr := &github.PullRequest{
		Number: github.Int(7),
		State:  github.String("open"),
		Head:   &github.PullRequestBranch{SHA: github.String("abc123")},
	}
	checks := []*github.CheckRun{
		{Name: github.String("lint"), Status: github.String("completed"), Conclusion: github.String("success")},
		{Name: github.String("build"), Status: github.String("completed"), Conclusion: github.String("failure")},
	}
	workflows := []*github.WorkflowRun{
		{Name: github.String("ci"), Status: github.String("completed"), Conclusion: github.String("success")},
	}

	got := SummarizePR(pr, checks, workflows)

	if got.AllGreen {
		t.Errorf("AllGreen = true, want false when a check concluded failure")
	}
}

func TestSummarizePR_NotGreenWhenWorkflowCancelled(t *testing.T) {
	pr := &github.PullRequest{
		Number: github.Int(7),
		State:  github.String("open"),
		Head:   &github.PullRequestBranch{SHA: github.String("abc123")},
	}
	checks := []*github.CheckRun{
		{Name: github.String("lint"), Status: github.String("completed"), Conclusion: github.String("success")},
	}
	workflows := []*github.WorkflowRun{
		{Name: github.String("ci"), Status: github.String("completed"), Conclusion: github.String("cancelled")},
	}

	got := SummarizePR(pr, checks, workflows)

	if got.AllGreen {
		t.Errorf("AllGreen = true, want false when a workflow concluded cancelled")
	}
}
