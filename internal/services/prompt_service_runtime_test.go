package services

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// assertPrompt fetches name and asserts the resolved tier and raw content.
func assertPrompt(t *testing.T, svc *PromptService, name string, tier PromptTier, content string) {
	t.Helper()
	got, err := svc.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	if got.Tier != tier {
		t.Errorf("Get(%q).Tier = %s, want %s", name, got.Tier, tier)
	}
	if got.Content != content {
		t.Errorf("Get(%q).Content = %q, want %q", name, got.Content, content)
	}
}

// TestPromptService_ProjectTierFollowsRuntimeChange proves the project tier is
// not frozen at construction: SetProjectDir swaps it and the next List/Get
// observes the new project's overrides (the runtime project-switch contract).
func TestPromptService_ProjectTierFollowsRuntimeChange(t *testing.T) {
	tmp := t.TempDir()
	projA := filepath.Join(tmp, "proj-a")
	projB := filepath.Join(tmp, "proj-b")
	mustWriteFile(t, filepath.Join(projA, "planner", "interview.md"), "A")
	mustWriteFile(t, filepath.Join(projB, "planner", "interview.md"), "B")

	svc := NewPromptService(projA, filepath.Join(tmp, "user"), filepath.Join(tmp, "system"), filepath.Join(tmp, "bundled"))

	if got := svc.ProjectDir(); got != projA {
		t.Fatalf("ProjectDir = %q, want %q", got, projA)
	}
	assertPrompt(t, svc, "interview", TierProject, "A")

	// Simulated project switch: same call the daemon bridge makes on project.set.
	svc.SetProjectDir(projB)
	if got := svc.ProjectDir(); got != projB {
		t.Fatalf("ProjectDir after switch = %q, want %q", got, projB)
	}
	assertPrompt(t, svc, "interview", TierProject, "B")

	// List must agree with Get on the switched tier.
	entries, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Name == "planner/interview.md" {
			found = true
			if e.Tier != TierProject {
				t.Errorf("list tier = %s, want project", e.Tier)
			}
			if want := filepath.Join(projB, "planner", "interview.md"); e.SourcePath != want {
				t.Errorf("list source = %q, want %q", e.SourcePath, want)
			}
		}
	}
	if !found {
		t.Errorf("planner/interview.md missing from List after switch: %+v", entries)
	}

	// Clearing the project tier drops it (no project active → no project tier).
	svc.SetProjectDir("")
	if got := svc.ProjectDir(); got != "" {
		t.Errorf("ProjectDir after clear = %q, want empty", got)
	}
	if _, err := svc.Get("interview"); err == nil {
		t.Error("Get succeeded after clearing the project tier, want not-found")
	}
}

// TestPromptService_FixedTiersUnaffectedByProjectChange proves user, system,
// and bundled tiers are stable across project-tier changes and that clearing
// the project tier falls back to the lower tiers unchanged.
func TestPromptService_FixedTiersUnaffectedByProjectChange(t *testing.T) {
	tmp := t.TempDir()
	projA := filepath.Join(tmp, "proj-a")
	projB := filepath.Join(tmp, "proj-b")
	userDir := filepath.Join(tmp, "user")
	systemDir := filepath.Join(tmp, "system")
	bundledDir := filepath.Join(tmp, "bundled")

	mustWriteFile(t, filepath.Join(projA, "planner", "shared.md"), "A")
	mustWriteFile(t, filepath.Join(projB, "planner", "shared.md"), "B")
	mustWriteFile(t, filepath.Join(userDir, "planner", "user.md"), "USER")
	mustWriteFile(t, filepath.Join(systemDir, "planner", "system.md"), "SYS")
	mustWriteFile(t, filepath.Join(bundledDir, "planner", "shared.md"), "BUNDLED")

	svc := NewPromptService(projA, userDir, systemDir, bundledDir)

	checkFixed := func(stage string) {
		t.Helper()
		assertPrompt(t, svc, "user", TierUser, "USER")
		assertPrompt(t, svc, "system", TierSystem, "SYS")
	}

	checkFixed("before switch")
	assertPrompt(t, svc, "shared", TierProject, "A")

	svc.SetProjectDir(projB)
	checkFixed("after switch")
	assertPrompt(t, svc, "shared", TierProject, "B")

	// With no project tier, the bundled template shows through with its own
	// label — the fixed tiers did not absorb the project override.
	svc.SetProjectDir("")
	checkFixed("after clear")
	assertPrompt(t, svc, "shared", TierBundled, "BUNDLED")
}

// TestPromptService_ConcurrentLookupsDuringProjectChange exercises List/Get
// concurrently with SetProjectDir. Run with -race: it fails if the project
// tier is read and written without synchronization.
func TestPromptService_ConcurrentLookupsDuringProjectChange(t *testing.T) {
	tmp := t.TempDir()
	projA := filepath.Join(tmp, "proj-a")
	projB := filepath.Join(tmp, "proj-b")
	mustWriteFile(t, filepath.Join(projA, "planner", "interview.md"), "A")
	mustWriteFile(t, filepath.Join(projB, "planner", "interview.md"), "B")

	svc := NewPromptService(projA, "", "", "")

	var failures atomic.Int64
	stop := make(chan struct{})
	var toggleWG, readerWG sync.WaitGroup

	// Toggler: keeps swapping the project tier between the two projects.
	toggleWG.Add(1)
	go func() {
		defer toggleWG.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				svc.SetProjectDir(projA)
			} else {
				svc.SetProjectDir(projB)
			}
		}
	}()

	// Readers: every lookup must see a coherent project tier (A or B), never a
	// torn, mislabeled, or missing one.
	const readers = 8
	const iterations = 300
	for r := 0; r < readers; r++ {
		readerWG.Add(1)
		go func() {
			defer readerWG.Done()
			for j := 0; j < iterations; j++ {
				entries, err := svc.List()
				if err != nil {
					failures.Add(1)
				} else {
					var seen bool
					for _, e := range entries {
						if e.Name != "planner/interview.md" {
							continue
						}
						seen = true
						if e.Tier != TierProject {
							failures.Add(1)
						}
					}
					if !seen {
						failures.Add(1)
					}
				}
				detail, err := svc.Get("interview")
				if err != nil {
					failures.Add(1)
					continue
				}
				if detail.Tier != TierProject || (detail.Content != "A" && detail.Content != "B") {
					failures.Add(1)
				}
			}
		}()
	}

	readerWG.Wait()
	close(stop)
	toggleWG.Wait()

	if n := failures.Load(); n != 0 {
		t.Errorf("%d concurrent lookups observed an incoherent project tier", n)
	}
}
