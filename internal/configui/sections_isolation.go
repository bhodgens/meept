// internal/configui/sections_isolation.go
package configui

func buildIsolationFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Isolation
	return []Field{
		NewTextField("base_dir", "base dir", s.BaseDir),
		NewToggleField("auto_git_init", "auto git init", s.AutoGitInit),
		NewToggleField("auto_test", "auto test", s.AutoTest),
	}
}
