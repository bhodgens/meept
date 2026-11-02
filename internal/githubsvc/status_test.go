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
		Number:    new(7),
		State:     new("open"),
		Draft:     new(false),
		Mergeable: new(true),
		Head:      &github.PullRequestBranch{SHA: new("abc123")},
	}
	checks := []*github.CheckRun{
		{Name: new("lint"), Status: new("completed"), Conclusion: new("success")},
		{Name: new("tests"), Status: new("completed"), Conclusion: new("success")},
	}
	workflows := []*github.WorkflowRun{
		{Name: new("ci"), Status: new("completed"), Conclusion: new("success")},
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
		Number: new(7),
		State:  new("open"),
		Head:   &github.PullRequestBranch{SHA: new("abc123")},
	}
	checks := []*github.CheckRun{
		{Name: new("lint"), Status: new("completed"), Conclusion: new("success")},
		{Name: new("build"), Status: new("completed"), Conclusion: new("failure")},
	}
	workflows := []*github.WorkflowRun{
		{Name: new("ci"), Status: new("completed"), Conclusion: new("success")},
	}

	got := SummarizePR(pr, checks, workflows)

	if got.AllGreen {
		t.Errorf("AllGreen = true, want false when a check concluded failure")
	}
}

func TestSummarizePR_NotGreenWhenWorkflowCancelled(t *testing.T) {
	pr := &github.PullRequest{
		Number: new(7),
		State:  new("open"),
		Head:   &github.PullRequestBranch{SHA: new("abc123")},
	}
	checks := []*github.CheckRun{
		{Name: new("lint"), Status: new("completed"), Conclusion: new("success")},
	}
	workflows := []*github.WorkflowRun{
		{Name: new("ci"), Status: new("completed"), Conclusion: new("cancelled")},
	}

	got := SummarizePR(pr, checks, workflows)

	if got.AllGreen {
		t.Errorf("AllGreen = true, want false when a workflow concluded cancelled")
	}
}
