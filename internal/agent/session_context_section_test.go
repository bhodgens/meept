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

// Context-usage rule (e2e A5, gh #37): the system-prompt session context
// must tell the model to answer session-work questions FROM the context —
// a standing rule, present whenever any session context exists.
func TestSessionContextSection_CarriesContextUsageRule(t *testing.T) {
	l := &AgentLoop{workingDir: "/tmp/project-x"}
	section := l.buildSessionContextSection()
	if !strings.Contains(section, "answer specifically from the context above") {
		t.Errorf("session context missing the usage rule:\n%s", section)
	}
}

// Run oCbPZZ (2026-09-11): when both a working directory and a client CWD
// exist, the session context must NOT print the client CWD — the model
// wrote hello.txt into the client's shell directory instead of the session
// project dir. The working directory is the only directory file tools
// should see.
func TestSessionContextSection_ClientCWDSuppressedWhenWorkingDirSet(t *testing.T) {
	l := &AgentLoop{
		workingDir: "/tmp/wd/project",
		detectionContext: &DetectionContext{
			CWD: "/tmp/wd",
		},
	}
	section := l.buildSessionContextSection()
	if !strings.Contains(section, "Working directory: /tmp/wd/project") {
		t.Errorf("working directory missing:\n%s", section)
	}
	if strings.Contains(section, "Client CWD") {
		t.Errorf("client CWD leaked into prompt with working dir set:\n%s", section)
	}
	if !strings.Contains(section, "All file tools operate inside the working directory") {
		t.Errorf("authoritative-directory statement missing:\n%s", section)
	}
}

// Without a working directory, Client CWD keeps its diagnostic value.
func TestSessionContextSection_ClientCWDKeptWithoutWorkingDir(t *testing.T) {
	l := &AgentLoop{
		detectionContext: &DetectionContext{CWD: "/tmp/wd"},
	}
	section := l.buildSessionContextSection()
	if !strings.Contains(section, "Client CWD: /tmp/wd") {
		t.Errorf("client CWD missing with no working dir:\n%s", section)
	}
}
