package debug

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// FindPIDByName returns the process ID of the first process whose command name
// matches the given name. On macOS and Linux it parses the output of `ps`.
// Returns os.ErrNotExist if no matching process is found.
func FindPIDByName(name string) (int, error) {
	if name == "" {
		return 0, fmt.Errorf("process name must not be empty")
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		return findPIDUnix(name)
	default:
		return 0, fmt.Errorf("pid lookup by name is not supported on %s", runtime.GOOS)
	}
}

// findPIDUnix uses `ps` to find a PID by process name on macOS and Linux.
func findPIDUnix(name string) (int, error) {
	// Use `ps -x -o pid,comm` (macOS) or `ps -x -o pid,comm 2>/dev/null || ps -eo pid,comm` (Linux).
	// The `-x` flag includes processes without a controlling terminal (macOS).
	cmd := exec.Command("ps", "-x", "-o", "pid=,comm=")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try without -x for Linux compatibility.
		cmd = exec.Command("ps", "-eo", "pid=,comm=")
		output, err = cmd.Output()
		if err != nil {
			return 0, fmt.Errorf("failed to list processes: %w", err)
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}

		pidStr := strings.TrimSpace(parts[0])
		comm := strings.TrimSpace(parts[1])

		// Strip leading path to get the basename (e.g. "/usr/bin/python3" -> "python3").
		if slash := strings.LastIndex(comm, "/"); slash >= 0 {
			comm = comm[slash+1:]
		}

		if comm == name {
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			return pid, nil
		}
	}

	return 0, os.ErrNotExist
}

// FindProcessBinary returns the absolute path of the executable for the given PID.
// On macOS and Linux it reads /proc/<pid>/exe or uses `ps -o comm`.
// Returns os.ErrNotExist if the process is not found.
func FindProcessBinary(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}

	switch runtime.GOOS {
	case "linux":
		return findProcessBinaryLinux(pid)
	case "darwin":
		return findProcessBinaryDarwin(pid)
	default:
		return "", fmt.Errorf("process binary lookup is not supported on %s", runtime.GOOS)
	}
}

func findProcessBinaryLinux(pid int) (string, error) {
	exeLink := fmt.Sprintf("/proc/%d/exe", pid)
	// readlink resolves the symlink.
	target, err := os.Readlink(exeLink)
	if err != nil {
		if os.IsNotExist(err) {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("failed to resolve process binary: %w", err)
	}
	return target, nil
}

func findProcessBinaryDarwin(pid int) (string, error) {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
	output, err := cmd.Output()
	if err != nil {
		return "", os.ErrNotExist
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", os.ErrNotExist
	}
	return path, nil
}

// DetectAdapterForProcess attempts to detect the best DAP adapter for a
// running process by examining its binary path. It checks the file extension
// of the binary and the working directory for project markers.
func DetectAdapterForProcess(pid int) (*AdapterConfig, error) {
	binary, err := FindProcessBinary(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to locate process binary: %w", err)
	}

	// Try extension-based detection first.
	ext := strings.ToLower(unknownExt(binary))
	if ext != "" {
		for i := range defaultAdapters {
			a := &defaultAdapters[i]
			for _, ft := range a.FileTypes {
				if ft == ext {
					return a, nil
				}
			}
		}
	}

	// Extension-based detection failed. Try heuristic: check for known binary names.
	base := baseName(binary)
	nameToAdapter := map[string]string{
		"dlv":      "dlv",
		"python":   "debugpy",
		"python3":  "debugpy",
		"python3.8": "debugpy",
		"python3.9": "debugpy",
		"python3.10": "debugpy",
		"python3.11": "debugpy",
		"python3.12": "debugpy",
		"python3.13": "debugpy",
		"go":       "dlv",
		"node":     "codelldb",
	}

	if adapterName, ok := nameToAdapter[base]; ok {
		return FindAdapterByName(adapterName), nil
	}

	return nil, fmt.Errorf("no DAP adapter found for process binary %q (pid %d)", binary, pid)
}

// unknownExt tries to detect a "language extension" from a compiled binary path.
// For compiled binaries (no extension), it returns "". For interpreted scripts
// or other files with extensions, it returns the extension.
func unknownExt(path string) string {
	// If the path has a known extension, return it directly.
	extIdx := strings.LastIndex(path, ".")
	if extIdx < 0 {
		return ""
	}
	ext := path[extIdx:] // includes the dot, e.g. ".go"

	// Check if it's a common shebang-based extension (even if compiled).
	shebangExts := map[string]bool{
		".py": true, ".rb": true, ".js": true, ".ts": true,
		".go": true, ".rs": true, ".c": true, ".cpp": true,
		".cc": true, ".cxx": true, ".h": true, ".hpp": true,
		".m": true, ".mm": true, ".swift": true,
	}
	if shebangExts[ext] {
		return ext
	}
	return ""
}

// baseName returns the last component of a path, without directory prefix.
func baseName(path string) string {
	if slash := strings.LastIndex(path, "/"); slash >= 0 {
		return path[slash+1:]
	}
	return path
}
