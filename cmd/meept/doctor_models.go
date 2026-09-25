package main

// doctor_models.go: the `meept doctor` local-model check.
//
// Report-only (doctor convention): for every lifecycle-backed endpoint in
// models.json5 it stats the declared model path and reports present/missing.
// A missing path is a WARN, never a failure — a cloud-only machine or one
// that has not run `make deps-models` yet is a legal state, and doctor must
// not turn it into an error.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// checkModelsDoctor stats every lifecycle model_path in the loaded models
// config. One check line per endpoint (prefix "models:"), or a single ok
// line when no lifecycle endpoints are configured.
func checkModelsDoctor() []doctorCheck {
	cfg, err := llm.LoadProvidersConfigDefault()
	if err != nil {
		return []doctorCheck{{
			name:   "models",
			ok:     true,
			warn:   true,
			detail: "models config unreadable (" + err.Error() + ")",
		}}
	}

	// Deterministic order: sort provider/model keys.
	provIDs := make([]string, 0, len(cfg.Providers))
	for id := range cfg.Providers {
		provIDs = append(provIDs, id)
	}
	sort.Strings(provIDs)

	var checks []doctorCheck
	seen := map[string]bool{} // same path can back several models
	missing := 0
	for _, pid := range provIDs {
		p := cfg.Providers[pid]
		if p.Lifecycle == nil {
			continue
		}
		ep := p.Lifecycle
		paths := map[string]string{}
		for k, v := range ep.ModelPaths {
			if v != "" {
				paths[k] = v
			}
		}
		if len(paths) == 0 && ep.ModelPath != "" {
			paths["default"] = ep.ModelPath
		}
		// Stable per-endpoint ordering.
		keys := make([]string, 0, len(paths))
		for k := range paths {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			raw := paths[k]
			// Raw config values still carry ${MEEPT_MODELS_DIR:-...};
			// expand with the same rules the daemon uses at load.
			path := expandModelsPathForDoctor(raw)
			if seen[path] {
				continue
			}
			seen[path] = true
			_, statErr := os.Stat(path)
			state := "present"
			if statErr != nil {
				state = "missing"
				missing++
			}
			checks = append(checks, doctorCheck{
				name:  "models:" + pid,
				ok:    statErr == nil,
				warn:  statErr != nil,
				detail: fmt.Sprintf("%s: %s (run 'make deps-models' to fetch)",
					state, displayPath(path)),
			})
		}
	}
	if len(checks) == 0 {
		return []doctorCheck{{
			name:   "models",
			ok:     true,
			detail: "no local lifecycle endpoints configured",
		}}
	}
	return checks
}

// expandModelsPathForDoctor expands ${VAR} and ${VAR:-default} in a config
// path the way internal/llm.expandEnvVars does at load time. Mirrored here
// (not imported) because the daemon-side resolver needs boot wiring; doctor
// only ever needs plain env + default semantics for path variables.
func expandModelsPathForDoctor(s string) string {
	out := s
	for {
		open := strings.Index(out, "${")
		if open < 0 {
			break
		}
		close := strings.Index(out[open:], "}")
		if close < 0 {
			break
		}
		close += open
		inner := out[open+2 : close]
		name, def := inner, ""
		hasDef := false
		if i := strings.Index(inner, ":-"); i >= 0 {
			name, def, hasDef = inner[:i], inner[i+2:], true
		}
		// Skip the runtime placeholder ($MODEL_PATH is expanded at spawn).
		if name == "MODEL_PATH" {
			break
		}
		if val, ok := os.LookupEnv(name); ok && val != "" {
			out = out[:open] + val + out[close+1:]
			continue
		}
		if hasDef {
			def = strings.ReplaceAll(def, "$HOME", homeDirForModels())
			out = out[:open] + def + out[close+1:]
			continue
		}
		out = out[:open] + out[close+1:]
	}
	return out
}

func homeDirForModels() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// displayPath shortens $HOME to ~ for the report line.
func displayPath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~" + p[len(h):]
	}
	return p
}
