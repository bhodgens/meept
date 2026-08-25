// Package secrets implements the secret broker: declared secrets are loaded
// once at daemon startup from environment variables or files, and children
// (shell commands, MCP server subprocesses) receive ONLY placeholder strings
// of the form MEEPT_SECRET:<name>. Real values stay in broker memory and are
// resolved exclusively inside this package (the egress proxy, leaf 04).
//
// Plaintext never appears in child environments, logs, or bus payloads: every
// exported accessor returns placeholders; resolve() is unexported so no code
// outside package secrets can obtain a real value.
package secrets

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
)

// PlaceholderPrefix marks values that are secret placeholders rather than
// real data. Must stay identical to internal/runtime's SecretPlaceholderPrefix
// (envpolicy.go), which lets placeholder-form env values bypass deny-globs.
// Declared here literally to avoid importing internal/runtime from this
// package; a test pins both constants equal.
const PlaceholderPrefix = "MEEPT_SECRET:"

// Placeholder returns the token children receive for the named secret:
// "MEEPT_SECRET:<name>". It never contains real secret material.
func Placeholder(name string) string {
	return PlaceholderPrefix + name
}

// isPlaceholder reports whether value carries the placeholder prefix.
func isPlaceholder(value string) bool {
	return strings.HasPrefix(value, PlaceholderPrefix)
}

// Source declares where one named secret is loaded from.
type Source struct {
	Kind   string   `json:"kind"   toml:"kind"`   // "env" | "file"
	Name   string   `json:"name"   toml:"name"`   // env var name / file path
	Hosts  []string `json:"hosts"  toml:"hosts"`  // host suffixes proxy may inject toward
	Header string   `json:"header" toml:"header"` // e.g. Authorization
	Format string   `json:"format" toml:"format"` // e.g. "Bearer {}" — {} replaced by value
}

// Config maps secret name -> its source declaration.
type Config map[string]Source

// Broker holds eagerly-loaded secret values in memory only. All accessors
// return placeholders or metadata; the real value is reachable solely via
// the unexported resolve(), consumed within package secrets by the egress
// proxy. Values are never persisted and never logged.
type Broker struct {
	sources Config
	values  map[string]string // name -> real value (memory only)
}

// NewBroker loads every source eagerly and returns a ready broker. Missing
// env vars/files and unknown kinds produce ONE aggregated error naming every
// failure; no partial broker is returned on failure.
//
// The logger receives names/paths only — never resolved values.
func NewBroker(cfg Config, logger *slog.Logger) (*Broker, error) {
	if logger == nil {
		logger = slog.Default()
	}

	values := make(map[string]string, len(cfg))
	failed := make([]string, 0, len(cfg))

	for _, name := range sortedNames(cfg) {
		src := cfg[name]
		val, err := loadSource(src)
		if err != nil {
			failed = append(failed, name)
			continue
		}
		values[name] = val
	}

	if len(failed) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d secrets failed:", len(failed))
		for i, name := range failed {
			if i > 0 {
				sb.WriteString(",")
			}
			// Format per plan contract: "<name> (<kind> <location>)" —
			// e.g. 'a (env MISSING_VAR), b (file /nope)'. Location only;
			// never any value content.
			src := cfg[name]
			fmt.Fprintf(&sb, " %s (%s %s)", name, src.Kind, src.Name)
		}
		return nil, fmt.Errorf("%s", sb.String())
	}

	for _, name := range sortedNames(cfg) {
		logger.Debug("secret loaded", "name", name, "kind", cfg[name].Kind)
	}

	return &Broker{sources: cfg, values: values}, nil
}

// loadSource loads one source per its Kind. Returns errors naming the kind
// and location (env var name or file path) but never any value content.
func loadSource(src Source) (string, error) {
	switch src.Kind {
	case "env":
		val, ok := os.LookupEnv(src.Name)
		if !ok {
			return "", fmt.Errorf("env %s not set", src.Name)
		}
		return val, nil
	case "file":
		data, err := os.ReadFile(src.Name)
		if err != nil {
			return "", fmt.Errorf("file %s: %w", src.Name, err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	default:
		return "", fmt.Errorf("unknown kind %q (want \"env\" or \"file\")", src.Kind)
	}
}

// sortedNames returns config keys sorted for deterministic iteration.
func sortedNames(cfg Config) []string {
	names := make([]string, 0, len(cfg))
	for name := range cfg {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Names returns all known secret names, sorted.
func (b *Broker) Names() []string {
	return sortedNames(b.sources)
}

// Source returns the declaration for name.
func (b *Broker) Source(name string) (Source, bool) {
	src, ok := b.sources[name]
	return src, ok
}

// ChildValue returns the PLACEHOLDER string for name (never the real value),
// suitable for placing into a child process environment or command args.
// Unknown names produce an error.
func (b *Broker) ChildValue(name string) (string, error) {
	if _, ok := b.sources[name]; !ok {
		return "", fmt.Errorf("unknown secret %q", name)
	}
	return Placeholder(name), nil
}

// ChildValues maps every known secret to its placeholder. Useful when
// building a full child env map.
func (b *Broker) ChildValues() map[string]string {
	out := make(map[string]string, len(b.sources))
	for name := range b.sources {
		out[name] = Placeholder(name)
	}
	return out
}

// IsPlaceholder reports whether value has the secret-placeholder form.
// Exported so sibling packages (runtime wiring, tool executors) share one
// definition of "this string is a placeholder".
func IsPlaceholder(value string) bool {
	return isPlaceholder(value)
}

// resolve returns the REAL value for name. Unexported by contract: only code
// inside package secrets (the egress proxy, leaf 04) may call it. Never log
// the result.
func (b *Broker) resolve(name string) (string, error) {
	val, ok := b.values[name]
	if !ok {
		return "", fmt.Errorf("unknown secret %q", name)
	}
	return val, nil
}
