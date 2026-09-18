package agent

import (
	"testing"
)

func TestRoutingRepairGitStatusNoun(t *testing.T) {
	// git-status-control acceptance failure (2026-09-18, scratch rig):
	// LLM scored verdict=git @0.95 on a 'run git status' request; the
	// agreement veto killed it because 'status' is a git SUBCOMMAND noun,
	// not an action verb -> quickplan fallback. 'git <noun>' evidence must
	// satisfy the veto exactly like a git verb.
	cases := []string{
		"Run git status --porcelain --untracked-files=all and save its output to routing-status.txt.",
		"show me git diff of the last commit",
		"check git log for the last 5 commits",
	}
	for _, c := range cases {
		if !inputContainsGitVerb(c) {
			t.Errorf("git subcommand noun not recognized as git evidence: %q", c)
		}
	}
	// Controls: non-git usage of these words must NOT match.
	nonGit := []string{
		"what is my status update for the week",
		"diff two text files for me",
		"log these hours in the timesheet",
	}
	for _, c := range nonGit {
		if inputContainsGitVerb(c) {
			t.Errorf("non-git noun usage matched: %q", c)
		}
	}
}
