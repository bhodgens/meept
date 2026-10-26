package githubsvc

import "github.com/google/go-github/v93/github"

// CheckRun is a plain-JSON mirror of the check-conclusion facts the
// github-pr-ci tools surface to the agent.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// WorkflowRun mirrors a GitHub Actions workflow run's conclusion facts.
type WorkflowRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// PRStatus is the summarized view of a PR's CI health returned by the
// github-pr-ci tools.
type PRStatus struct {
	Number    int           `json:"number"`
	State     string        `json:"state"`
	Draft     bool          `json:"draft"`
	Mergeable string        `json:"mergeable"`
	Head      string        `json:"head"`
	Checks    []CheckRun    `json:"checks"`
	Workflows []WorkflowRun `json:"workflows"`
	AllGreen  bool          `json:"all_green"`
}

// SummarizePR flattens a go-github PullRequest, its check runs, and its
// workflow runs into a PRStatus. AllGreen is true only when every check
// AND every workflow concluded "success"; a missing or non-success
// conclusion (including empty) means not green.
func SummarizePR(pr *github.PullRequest, checks []*github.CheckRun, workflows []*github.WorkflowRun) PRStatus {
	if pr == nil {
		return PRStatus{}
	}
	flatChecks := make([]CheckRun, 0, len(checks))
	allGreen := true
	for _, c := range checks {
		if c == nil {
			continue
		}
		flatChecks = append(flatChecks, CheckRun{
			Name:       c.GetName(),
			Status:     c.GetStatus(),
			Conclusion: c.GetConclusion(),
		})
		if c.GetConclusion() != "success" {
			allGreen = false
		}
	}
	flatWorkflows := make([]WorkflowRun, 0, len(workflows))
	for _, w := range workflows {
		if w == nil {
			continue
		}
		flatWorkflows = append(flatWorkflows, WorkflowRun{
			Name:       w.GetName(),
			Status:     w.GetStatus(),
			Conclusion: w.GetConclusion(),
		})
		if w.GetConclusion() != "success" {
			allGreen = false
		}
	}
	// go-github v93: Mergeable is *bool ("whether the PR can be merged at
	// all"), so the string view is derived from that boolean.
	mergeable := "no"
	if pr.GetMergeable() {
		mergeable = "yes"
	}
	return PRStatus{
		Number:    pr.GetNumber(),
		State:     pr.GetState(),
		Draft:     pr.GetDraft(),
		Mergeable: mergeable,
		Head:      pr.GetHead().GetSHA(),
		Checks:    flatChecks,
		Workflows: flatWorkflows,
		AllGreen:  allGreen,
	}
}
