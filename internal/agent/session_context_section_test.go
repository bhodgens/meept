package agent

import (
	"strings"
	"testing"
)

// Pins for the path-hygiene line in buildSessionContextSection (e2e run 11,
// 2026-09-11 0hqmgO): with a working directory bound, the system prompt must
// state it AND instruct the model to use relative paths — the run-11 coder
// wrote hello.txt successfully, then "verified" with file_read /hello.txt
// (an invented absolute root path), got security-blocked, and failed its own
// task despite the artifact existing.

func TestSessionContextSection_CarriesWorkingDirAndPathRule(t *testing.T) {
	l := &AgentLoop{workingDir: "/tmp/project-x"}
	section := l.buildSessionContextSection()
	if !strings.Contains(section, "Working directory: /tmp/project-x") {
		t.Errorf("session context missing working directory:\n%s", section)
	}
	if !strings.Contains(section, "relative to the working directory") {
		t.Errorf("session context missing the relative-path rule:\n%s", section)
	}
	if !strings.Contains(section, "never invent absolute paths") {
		t.Errorf("session context missing the no-invented-absolute-paths rule:\n%s", section)
	}
}

func TestSessionContextSection_EmptyWithoutWorkingDir(t *testing.T) {
	l := &AgentLoop{}
	section := l.buildSessionContextSection()
	if section != "" {
		t.Errorf("no session/project/cwd set; want empty section, got:\n%s", section)
	}
}
