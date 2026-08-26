package daemon

import (
	"os"
	"testing"
)

// fakeProc is a test double for a process found by the ps scan.
type fakeProc struct {
	pid      int
	ppid     int
	envTag   bool   // MEEPT_DAEMON_CHILD=1 visible in ps output
	startStr string // recorded parent start marker
}

func TestOrphanSweep(t *testing.T) {
	cases := []struct {
		name       string
		procs      []fakeProc
		wantKilled []int
	}{
		{
			name: "own children kept",
			procs: []fakeProc{
				{pid: 100, ppid: os.Getpid(), envTag: true, startStr: "1000"},
			},
			wantKilled: nil,
		},
		{
			name: "recent-tag re-parented kept",
			procs: []fakeProc{
				{pid: 101, ppid: 1, envTag: true, startStr: "99999999999"},
			},
			wantKilled: nil,
		},
		{
			name: "old-tag re-parented killed",
			procs: []fakeProc{
				{pid: 102, ppid: 1, envTag: true, startStr: "1000"},
			},
			wantKilled: []int{102},
		},
		{
			name: "non-meept process never touched",
			procs: []fakeProc{
				{pid: 103, ppid: 1, envTag: false, startStr: "1000"},
				{pid: 104, ppid: 7, envTag: false, startStr: "1000"},
			},
			wantKilled: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var procs []*fakeProc
			for i := range tc.procs {
				p := &tc.procs[i]
				procs = append(procs, p)
			}
			scanner := func() ([]ProcInfo, error) {
				out := make([]ProcInfo, 0, len(procs))
				for _, p := range procs {
					out = append(out, ProcInfo{PID: p.pid, PPID: p.ppid, EnvTag: p.envTag, StartMarker: p.startStr})
				}
				return out, nil
			}
			killed := map[int]bool{}
			signaler := func(pid int, sig Signal) error {
				if sig == SigKill {
					killed[pid] = true
				}
				return nil
			}
			SweepOrphans(scanner, signaler, "5000000000", 0) // zero wait in tests
			for _, want := range tc.wantKilled {
				if !killed[want] {
					t.Errorf("expected pid %d to be killed", want)
				}
			}
			for pid := range killed {
				found := false
				for _, w := range tc.wantKilled {
					if w == pid {
						found = true
					}
				}
				if !found {
					t.Errorf("unexpected kill of pid %d", pid)
				}
			}
		})
	}
}

func TestParsePsLine(t *testing.T) {
	// macOS style: pid ppid command with embedded env tag
	info, ok := parsePsLine("  512     1 /bin/sh -c MEEPT_DAEMON_CHILD=1 llama-server")
	if !ok || info.PID != 512 || info.PPID != 1 || !info.EnvTag {
		t.Fatalf("unexpected parse result: %+v ok=%v", info, ok)
	}
	// no tag
	info, ok = parsePsLine("  513     1 /usr/sbin/cron")
	if ok && info.EnvTag {
		t.Fatalf("expected non-meept line without tag, got %+v ok=%v", info, ok)
	}
	// garbage
	if _, ok := parsePsLine("not a ps line"); ok {
		t.Fatal("expected garbage line to be skipped")
	}
}

func TestRealScannerSmoke(t *testing.T) {
	// Smoke: scanner runs and returns without error on this platform.
	if _, err := ScanChildProcs(); err != nil {
		t.Skipf("ps scanning unavailable here: %v", err)
	}
}
