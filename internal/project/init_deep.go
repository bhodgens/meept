package project

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/code/ast"
)

// DeepInitOptions controls the behavior of deep project initialization.
type DeepInitOptions struct {
	// RootDir is the project root to scan (default: current directory)
	RootDir string
	// MaxDepth limits how deep the directory tree is scanned (0 = unlimited)
	MaxDepth int
	// MinFileCount is the minimum files in a directory to get its own AGENTS.md
	MinFileCount int
	// MaxFileCount caps files scanned per directory for performance
	MaxFileCount int
	// IncludeTests controls whether test files are included in symbol extraction
	IncludeTests bool
	// SymbolDepth limits AST extraction depth (0 = unlimited)
	SymbolDepth int
	// ExcludePatterns are path globs to skip (e.g., "vendor", ".git")
	ExcludePatterns []string
	// OutputFormat controls verbosity: "concise" (default) or "detailed"
	OutputFormat string
}

// DefaultDeepInitOptions returns sensible defaults.
func DefaultDeepInitOptions() DeepInitOptions {
	return DeepInitOptions{
		MaxDepth:        6,
		MinFileCount:    3,
		MaxFileCount:    50,
		IncludeTests:    false,
		SymbolDepth:     2,
		ExcludePatterns: []string{".git", "vendor", "node_modules", "dist", "build", ".worktrees", "archive"},
		OutputFormat:    "concise",
	}
}

// DeepInitResult summarizes what was generated.
type DeepInitResult struct {
	RootDir     string        `json:"root_dir"`
	AgentsFiles []AgentsFile  `json:"agents_files"`
	Duration    time.Duration `json:"duration"`
	Errors      []string      `json:"errors,omitempty"`
}

// AgentsFile describes a single generated AGENTS.md.
type AgentsFile struct {
	Path        string `json:"path"`
	Level       string `json:"level"` // "root", "domain", "component"
	FileCount   int    `json:"file_count"`
	SymbolCount int    `json:"symbol_count"`
}

// DeepInitializer handles scanning and AGENTS.md generation.
type DeepInitializer struct {
	parser  *ast.ParserManager
	extract *ast.SymbolExtractor
	opts    DeepInitOptions
	logger  *slog.Logger
}

// NewDeepInitializer creates a new initializer with default parser config.
func NewDeepInitializer(opts DeepInitOptions, logger *slog.Logger) *DeepInitializer {
	if logger == nil {
		logger = slog.Default()
	}
	pm := ast.NewParserManager(ast.DefaultParserConfig())
	return &DeepInitializer{
		parser:  pm,
		extract: ast.NewSymbolExtractor(pm),
		opts:    opts,
		logger:  logger,
	}
}

// Run performs the deep initialization.
func (di *DeepInitializer) Run(ctx context.Context) (*DeepInitResult, error) {
	start := time.Now()
	root := di.opts.RootDir
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
		root = wd
	}

	result := &DeepInitResult{
		RootDir: root,
	}

	// Scan directory tree
	dirs, err := di.scanTree(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("scan tree: %w", err)
	}

	// Generate AGENTS.md for each qualifying directory
	for _, dir := range dirs {
		if err := di.generateAgentsFile(ctx, dir, result); err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("generate %s: %v", dir.Path, err))
		}
	}

	result.Duration = time.Since(start)
	di.logger.Info("deep init complete",
		"root", root,
		"files_generated", len(result.AgentsFiles),
		"duration", result.Duration,
	)
	return result, nil
}

// dirInfo holds scan results for one directory.
type dirInfo struct {
	Path       string
	Depth      int
	Files      []string // source code files only
	Symbols    []ast.Symbol
	ChildPaths []string // immediate subdirectories with source files
	ParentPath string
}

func (di *DeepInitializer) scanTree(ctx context.Context, root string) ([]*dirInfo, error) {
	exclude := make(map[string]bool)
	for _, p := range di.opts.ExcludePatterns {
		exclude[p] = true
	}

	dirMap := make(map[string]*dirInfo)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}
		if d.IsDir() {
			name := filepath.Base(path)
			if exclude[name] {
				return filepath.SkipDir
			}
			// Check depth
			if di.opts.MaxDepth > 0 {
				rel, _ := filepath.Rel(root, path)
				depth := strings.Count(rel, string(os.PathSeparator))
				if depth > di.opts.MaxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// File: track in parent directory
		dir := filepath.Dir(path)
		name := d.Name()
		if !di.isSourceFile(name) {
			return nil
		}
		if !di.opts.IncludeTests && isTestFile(name) {
			return nil
		}

		if dirMap[dir] == nil {
			dirMap[dir] = &dirInfo{Path: dir}
		}
		info := dirMap[dir]
		if len(info.Files) < di.opts.MaxFileCount {
			info.Files = append(info.Files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Compute depth, parent/child relationships, and extract symbols
	var result []*dirInfo
	for path, info := range dirMap {
		if len(info.Files) < di.opts.MinFileCount {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		info.Depth = strings.Count(rel, string(os.PathSeparator))
		if info.Depth < 0 {
			info.Depth = 0
		}
		info.ParentPath = filepath.Dir(path)

		// Extract symbols
		for _, file := range info.Files {
			syms, err := di.extract.ExtractFromFileWithFilter(ctx, file, di.symbolFilter())
			if err != nil {
				di.logger.Debug("symbol extraction failed", "file", file, "error", err)
				continue
			}
			info.Symbols = append(info.Symbols, syms...)
		}

		result = append(result, info)
	}

	// Populate child relationships
	for _, info := range result {
		for _, other := range result {
			if other.ParentPath == info.Path {
				info.ChildPaths = append(info.ChildPaths, other.Path)
			}
		}
	}

	// Sort by depth (shallow first)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Depth < result[j].Depth
	})

	return result, nil
}

func (di *DeepInitializer) isSourceFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".rs", ".java", ".rb", ".c", ".cpp", ".h", ".hpp":
		return true
	}
	return false
}

func isTestFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, "_test.py")
}

func (di *DeepInitializer) symbolFilter() ast.SymbolFilter {
	depth := di.opts.SymbolDepth
	if depth == 0 {
		depth = 2
	}
	return ast.SymbolFilter{
		MaxDepth: depth,
		Kinds: []ast.SymbolKind{
			ast.SymbolKindFunction,
			ast.SymbolKindMethod,
			ast.SymbolKindStruct,
			ast.SymbolKindInterface,
			ast.SymbolKindClass,
			ast.SymbolKindEnum,
			ast.SymbolKindModule,
		},
		IncludePrivate: true,
	}
}

func (di *DeepInitializer) generateAgentsFile(ctx context.Context, dir *dirInfo, result *DeepInitResult) error {
	level := di.levelFor(dir)
	content := di.buildAgentsContent(dir, level)
	path := filepath.Join(dir.Path, "AGENTS.md")

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	result.AgentsFiles = append(result.AgentsFiles, AgentsFile{
		Path:        path,
		Level:       level,
		FileCount:   len(dir.Files),
		SymbolCount: len(dir.Symbols),
	})
	return nil
}

func (di *DeepInitializer) levelFor(dir *dirInfo) string {
	switch {
	case dir.Depth == 0:
		return "root"
	case len(dir.ChildPaths) > 0:
		return "domain"
	default:
		return "component"
	}
}

// buildAgentsContent creates the markdown content for an AGENTS.md file.
func (di *DeepInitializer) buildAgentsContent(dir *dirInfo, level string) string {
	var b strings.Builder
	rel, _ := filepath.Rel(di.opts.RootDir, dir.Path)
	if rel == "." {
		rel = filepath.Base(di.opts.RootDir)
	}

	b.WriteString("# Agent Context: ")
	b.WriteString(rel)
	b.WriteString("\n\n")

	// Architecture conventions based on level
	switch level {
	case "root":
		b.WriteString("## Architecture\n\n")
		b.WriteString("This is the project root. Key conventions:\n\n")
		b.WriteString(di.rootConventions(dir))
		b.WriteString("\n")
	case "domain":
		b.WriteString("## Domain\n\n")
		b.WriteString(fmt.Sprintf("Domain: `%s`. Contains %d files, %d symbols.\n\n",
			rel, len(dir.Files), len(dir.Symbols)))
		b.WriteString(di.domainConventions(dir))
		b.WriteString("\n")
	case "component":
		b.WriteString("## Component\n\n")
		b.WriteString(fmt.Sprintf("Component: `%s`. Contains %d files, %d symbols.\n\n",
			rel, len(dir.Files), len(dir.Symbols)))
		b.WriteString(di.componentConventions(dir))
		b.WriteString("\n")
	}

	// Symbols section (concise shorthand notation)
	if len(dir.Symbols) > 0 {
		b.WriteString("## Symbols\n\n")
		di.writeSymbolsShorthand(&b, dir.Symbols)
		b.WriteString("\n")
	}

	// Subdirectories
	if len(dir.ChildPaths) > 0 {
		b.WriteString("## Subdirectories\n\n")
		for _, child := range dir.ChildPaths {
			childRel, _ := filepath.Rel(di.opts.RootDir, child)
			b.WriteString("- `")
			b.WriteString(childRel)
			b.WriteString("`\n")
		}
		b.WriteString("\n")
	}

	// Token conservation note
	b.WriteString("## Notes\n\n")
	b.WriteString("- Use shorthand notation when referencing symbols above.\n")
	b.WriteString("- Prefer relative paths from this directory.\n")
	b.WriteString("- Generated by `meept init`.\n")

	return b.String()
}

func (di *DeepInitializer) rootConventions(dir *dirInfo) string {
	var b strings.Builder
	// Detect patterns from files
	hasGo := di.hasLang(dir, ".go")
	hasPy := di.hasLang(dir, ".py")
	hasJS := di.hasLang(dir, ".js", ".ts", ".tsx")
	hasRS := di.hasLang(dir, ".rs")

	if hasGo {
		b.WriteString("- Go project: use standard layout (cmd/, internal/, pkg/).\n")
		b.WriteString("- Interface definitions in internal/; Public APIs in pkg/.\n")
	}
	if hasPy {
		b.WriteString("- Python project: use package-per-module layout.\n")
	}
	if hasJS {
		b.WriteString("- Node/TS project: src/ for source, tests adjacent or in __tests__/.\n")
	}
	if hasRS {
		b.WriteString("- Rust project: src/lib.rs or src/main.rs as entry.\n")
	}
	b.WriteString(fmt.Sprintf("- Total source files in project: %d.\n", len(dir.Files)))
	return b.String()
}

func (di *DeepInitializer) domainConventions(dir *dirInfo) string {
	var b strings.Builder
	pkg := filepath.Base(dir.Path)
	b.WriteString(fmt.Sprintf("- Package/module name: `%s`.\n", pkg))
	b.WriteString("- Domain-specific types and interfaces live here.\n")
	b.WriteString("- Prefer internal functions; export only when cross-domain.\n")
	return b.String()
}

func (di *DeepInitializer) componentConventions(dir *dirInfo) string {
	var b strings.Builder
	pkg := filepath.Base(dir.Path)
	b.WriteString(fmt.Sprintf("- Component: `%s`.\n", pkg))
	b.WriteString("- Focused, single-responsibility component.\n")
	b.WriteString("- Minimal public surface area.\n")
	return b.String()
}

func (di *DeepInitializer) hasLang(dir *dirInfo, exts ...string) bool {
	for _, f := range dir.Files {
		ext := strings.ToLower(filepath.Ext(f))
		for _, e := range exts {
			if ext == e {
				return true
			}
		}
	}
	return false
}

// writeSymbolsShorthand emits concise symbol documentation using
// token-conserving shorthand notation.
//
// Format for each symbol:
//   - Kind Name: brief signature
//
// E.g.:
//   - Fn ProcessRequest(ctx, req): (*Response, error)
//   - Type Config struct { Timeout int }
//   - Ifac Chatter: Chat(ctx, []ChatMessage) (*Response, error)
func (di *DeepInitializer) writeSymbolsShorthand(b *strings.Builder, symbols []ast.Symbol) {
	// Group by kind
	byKind := make(map[string][]ast.Symbol)
	for _, s := range symbols {
		byKind[s.Kind.String()] = append(byKind[s.Kind.String()], s)
	}

	// Order: types, interfaces, functions, methods, others
	order := []string{"struct", "interface", "function", "method", "class", "enum"}
	written := 0
	maxSymbols := 30 // cap for token conservation

	for _, kind := range order {
		items := byKind[kind]
		if len(items) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", kind))
		for _, s := range items {
			if written >= maxSymbols {
				b.WriteString(fmt.Sprintf("_... and %d more ..._\n", len(symbols)-written))
				return
			}
			shorthand := di.shorthandFor(s)
			b.WriteString("- ")
			b.WriteString(shorthand)
			b.WriteString("\n")
			written++
		}
		b.WriteString("\n")
	}
}

// shorthandFor returns a compact string for a symbol.
func (di *DeepInitializer) shorthandFor(s ast.Symbol) string {
	var b strings.Builder
	switch s.Kind {
	case ast.SymbolKindFunction:
		b.WriteString("Fn ")
		b.WriteString(s.Name)
		if s.Signature != "" {
			b.WriteString(": ")
			b.WriteString(di.truncateSignature(s.Signature, 60))
		}
	case ast.SymbolKindMethod:
		b.WriteString("Meth ")
		b.WriteString(s.Name)
		if s.Signature != "" {
			b.WriteString(": ")
			b.WriteString(di.truncateSignature(s.Signature, 60))
		}
	case ast.SymbolKindStruct:
		b.WriteString("Type ")
		b.WriteString(s.Name)
		if s.Signature != "" {
			b.WriteString(" ")
			b.WriteString(di.truncateSignature(s.Signature, 60))
		}
	case ast.SymbolKindInterface:
		b.WriteString("Ifac ")
		b.WriteString(s.Name)
		if s.Signature != "" {
			b.WriteString(" ")
			b.WriteString(di.truncateSignature(s.Signature, 60))
		}
	case ast.SymbolKindClass:
		b.WriteString("Cls ")
		b.WriteString(s.Name)
	default:
		b.WriteString(s.Kind.String()[:4])
		b.WriteString(" ")
		b.WriteString(s.Name)
	}
	return b.String()
}

func (di *DeepInitializer) truncateSignature(sig string, maxLen int) string {
	if len(sig) <= maxLen {
		return sig
	}
	return sig[:maxLen-3] + "..."
}

// LoadAgentsForContext loads AGENTS.md files relevant to the current working
// context (file path). It returns the content of the closest AGENTS.md plus
// the root AGENTS.md, from most specific to least specific.
func LoadAgentsForContext(projectRoot, workingFile string) ([]string, error) {
	var results []string

	// Find the closest AGENTS.md by walking up from workingFile
	dir := filepath.Dir(workingFile)
	for {
		agentsPath := filepath.Join(dir, "AGENTS.md")
		if data, err := os.ReadFile(agentsPath); err == nil {
			results = append(results, string(data))
		}

		parent := filepath.Dir(dir)
		if parent == dir || dir == projectRoot {
			// Also load root AGENTS.md if not already loaded
			rootAgents := filepath.Join(projectRoot, "AGENTS.md")
			if _, err := os.Stat(rootAgents); err == nil {
				if data, err := os.ReadFile(rootAgents); err == nil {
					// Avoid duplicate if workingFile is directly in root
					if len(results) == 0 || filepath.Dir(workingFile) != projectRoot {
						results = append(results, string(data))
					}
				}
			}
			break
		}
		dir = parent
	}

	return results, nil
}
