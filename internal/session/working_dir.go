package session

// WorkingDirSource names the session field that supplied an effective
// working directory. Diagnostic only: resolution never depends on the
// source, but the daemon logs it so an unbound turn is debuggable from
// the daemon log alone.
type WorkingDirSource string

const (
	// WorkingDirFromWorktree: the session's provisioned git worktree.
	WorkingDirFromWorktree WorkingDirSource = "worktree_path"
	// WorkingDirFromProject: the project bound to the session.
	WorkingDirFromProject WorkingDirSource = "project_path"
	// WorkingDirFromDetection: the client-side CWD recorded in the
	// session's detection context (the interactive client's process CWD,
	// sent with session.create / `meept chat --cwd`).
	WorkingDirFromDetection WorkingDirSource = "detection_context_cwd"
	// WorkingDirFromDefault: the configured last resort
	// (daemon.default_working_dir), consulted after everything else.
	WorkingDirFromDefault WorkingDirSource = "daemon_default"
	// WorkingDirFromNone: nothing is bound to the session.
	WorkingDirFromNone WorkingDirSource = "none"
)

// ResolveWorkingDir returns the effective working directory bound to a
// session, together with the field it came from. This is THE precedence
// for the whole repo (AGENTS.md, "Step jobs run in the session's
// directory"):
//
//	WorktreePath > ProjectPath > DetectionContext.CWD
//
// Project scoping is PER-SESSION: only the session's OWN binding is
// consulted. There is no global "active project" fallback — a session
// created without a project and without a client CWD resolves nothing
// here, and callers then apply the configured daemon default
// (daemon.default_working_dir) or fail actionably.
//
// The daemon process's own working directory is never consulted: it is
// wherever the daemon was launched from (often the meept repo itself) and
// is never the user's project directory. When nothing is bound the caller
// gets ("", WorkingDirFromNone) and must fail actionably
// (tools.ErrNoWorkingDir) instead of guessing a directory.
func ResolveWorkingDir(sess *Session) (string, WorkingDirSource) {
	if sess == nil {
		return "", WorkingDirFromNone
	}
	if sess.WorktreePath != "" {
		return sess.WorktreePath, WorkingDirFromWorktree
	}
	if sess.ProjectPath != "" {
		return sess.ProjectPath, WorkingDirFromProject
	}
	if sess.DetectionContext != nil && sess.DetectionContext.CWD != "" {
		return sess.DetectionContext.CWD, WorkingDirFromDetection
	}
	return "", WorkingDirFromNone
}
