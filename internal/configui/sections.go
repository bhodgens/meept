// internal/configui/sections.go
package configui

// BuildSectionFields creates the fields for a given section key path.
// Sections are defined in separate files (sections_*.go) but this function
// dispatches to them.
func BuildSectionFields(keyPath string) []Field {
	switch keyPath {
	case "daemon":
		return buildDaemonFields()
	case "scheduler":
		return buildSchedulerFields()
	// All other sections will be added in Phase 3
	default:
		return []Field{
			NewTextField("_stub", "(section not yet implemented)", ""),
		}
	}
}
