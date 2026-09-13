package repomap

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	meeptcfg "github.com/caimlas/meept/internal/config"
)

// TestGeneratorCacheDirHonorsMeeptHome asserts an empty CacheDir (the
// generator default) resolves under MEEPT_HOME instead of the operator home.
func TestGeneratorCacheDirHonorsMeeptHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(meeptcfg.EnvMeeptHome, tmp)

	cfg := DefaultRepoMapConfig()
	cfg.CacheDir = ""

	gen, err := NewRepoMapGenerator(cfg, slog.Default(), []string{})
	if err != nil {
		t.Fatalf("NewRepoMapGenerator: %v", err)
	}
	want := filepath.Join(tmp, "repomap_cache")
	if got := gen.config.CacheDir; got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}

// TestGeneratorCacheDirRedirectsShippedDefault asserts the shipped
// "~/.meept/repomap_cache" config default is redirected under MEEPT_HOME --
// this is the path the daemon actually passes in (DefaultRepoMapConfig).
func TestGeneratorCacheDirRedirectsShippedDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(meeptcfg.EnvMeeptHome, tmp)

	cfg := DefaultRepoMapConfig()
	if !strings.HasPrefix(cfg.CacheDir, "~/.meept") {
		t.Fatalf("precondition: shipped CacheDir = %q, want a ~/.meept prefix", cfg.CacheDir)
	}

	gen, err := NewRepoMapGenerator(cfg, slog.Default(), []string{})
	if err != nil {
		t.Fatalf("NewRepoMapGenerator: %v", err)
	}
	want := filepath.Join(tmp, "repomap_cache")
	if got := gen.config.CacheDir; got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}

// TestGeneratorCacheDirDefaultWithoutMeeptHome asserts the historical default
// is unchanged when MEEPT_HOME is unset. HOME is redirected to a temp dir so
// the test cannot touch the operator's ~/.meept.
func TestGeneratorCacheDirDefaultWithoutMeeptHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv(meeptcfg.EnvMeeptHome, "")

	cfg := DefaultRepoMapConfig()
	cfg.CacheDir = ""
	gen, err := NewRepoMapGenerator(cfg, slog.Default(), []string{})
	if err != nil {
		t.Fatalf("NewRepoMapGenerator: %v", err)
	}
	want := filepath.Join(tmpHome, ".meept", "repomap_cache")
	if got := gen.config.CacheDir; got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
	if !strings.HasPrefix(filepath.Clean(gen.config.CacheDir), filepath.Clean(tmpHome)) {
		t.Errorf("CacheDir %q escaped the temp home %q", gen.config.CacheDir, tmpHome)
	}

	// A non-meept ~ cache dir keeps expanding to the real (temp) home.
	cfg2 := DefaultRepoMapConfig()
	cfg2.CacheDir = "~/custom-repomap-cache"
	gen2, err := NewRepoMapGenerator(cfg2, slog.Default(), []string{})
	if err != nil {
		t.Fatalf("NewRepoMapGenerator: %v", err)
	}
	if want2 := filepath.Join(tmpHome, "custom-repomap-cache"); gen2.config.CacheDir != want2 {
		t.Errorf("CacheDir = %q, want %q", gen2.config.CacheDir, want2)
	}
}
