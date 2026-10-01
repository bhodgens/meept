// Package gclplugin exposes the mutexio analyzer as a golangci-lint
// module plugin (see https://golangci-lint.run/docs/plugins/module-plugins/).
//
// Registering mutexio inside golangci-lint lets it honor the ~200
// //nolint:mutexio directives natively and removes the
// "Found unknown linters in //nolint directives: mutexio" warning
// (golangci-lint#1450 — no allow-list knob for custom nolint names).
//
// The analyzer still runs standalone via `make analyzers` and the
// pre-commit hook; the golangci registration is an additional surface,
// not a replacement.
package gclplugin

import (
	"github.com/caimlas/meept/tools/analyzers/mutexio/mutexio"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("mutexio", New)
}

// plugin implements register.LinterPlugin for the mutexio analyzer.
type plugin struct{}

// New builds the plugin instance. mutexio has no settings; any config
// block supplied in linters.settings.custom.mutexio.settings is ignored.
func New(_ any) (register.LinterPlugin, error) {
	return &plugin{}, nil
}

// BuildAnalyzers returns the mutexio analyzer.
func (p *plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{mutexio.Analyzer}, nil
}

// GetLoadMode declares types-info: the analyzer resolves selector types
// (pass.TypesInfo) to classify I/O calls under mutex.
func (p *plugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
