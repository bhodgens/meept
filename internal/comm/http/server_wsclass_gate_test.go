package http

import (
	"os"
	"path/filepath"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/comm/wsclass"
)

// reflectTypeKey derives the "pkgpath.TypeName" identity of a payload
// value — the same key space discoverWSClassifiedImplementers uses for
// types found by the type checker (named.Obj().Pkg().Path() + name equals
// reflect's PkgPath + Name for named struct types).
func reflectTypeKey(v wsclass.WSClassified) string {
	typ := reflect.TypeOf(v)
	return typ.PkgPath() + "." + typ.Name()
}

// TestEveryWSClassifiedHasDecoderEntry guards the typed-decoder gate
// (audit LOW finding F-E). typedPayloadDecoders is topic-keyed: a typed
// bus.Topic[T] whose payload implements wsclass.WSClassified but has NO
// entry in the map silently regresses to the legacy topic-prefix
// classification — the marker is never consulted.
//
// The test works in two layers:
//
//  1. A static expectation table (payload type -> topic name), and the
//     assertion that each expected topic has a decoder entry. Adding a
//     new WSClassified payload type without a decoder entry FAILS here
//     via layer 2, and the error message routes the author to the fix.
//  2. A type-checker sweep (golang.org/x/tools/go/packages) that
//     discovers every named type in the module implementing
//     wsclass.WSClassified — so a NEW implementer anywhere in the repo
//     is caught even though this table has never heard of it.
func TestEveryWSClassifiedHasDecoderEntry(t *testing.T) {
	// The expectation table: every known WSClassified payload type maps
	// to exactly one typed topic name keying typedPayloadDecoders.
	expectations := map[string]string{
		reflectTypeKey(agent.TurnTerminalEvent{}): agent.TopicTurnTerminal.Name,
	}

	// Layer 2: discover implementers module-wide via the type checker.
	discovered := discoverWSClassifiedImplementers(t)
	if _, ok := discovered[reflectTypeKey(agent.TurnTerminalEvent{})]; !ok {
		t.Fatal("discovery did not find agent.TurnTerminalEvent as a wsclass.WSClassified implementer — the sweep itself is broken")
	}

	for typeName := range discovered {
		topic, ok := expectations[typeName]
		if !ok {
			t.Errorf("payload type %s implements wsclass.WSClassified but is not registered in this test: add a typedPayloadDecoders entry in server_wsclass.go (decode fn + topic) and a row in this test's expectations table — a new WS-visible typed topic without a decoder entry silently regresses to legacy prefix classification", typeName)
			continue
		}
		if _, ok := typedPayloadDecoders[topic]; !ok {
			t.Errorf("WSClassified payload %s (topic %q) has NO typedPayloadDecoders entry: new WS-visible typed topics silently regress to legacy prefix classification", typeName, topic)
		}
	}
	if t.Failed() {
		return
	}

	// Converse: every decoder entry must be backed by a registered
	// expectation (catches stale entries too).
	for topic := range typedPayloadDecoders {
		registered := false
		for _, want := range expectations {
			if want == topic {
				registered = true
				break
			}
		}
		if !registered {
			t.Errorf("typedPayloadDecoders entry %q is not backed by any WSClassified expectation in this test; register its payload type (or remove the stale entry)", topic)
		}
	}
}

// discoverWSClassifiedImplementers type-checks the module and returns
// every named struct type implementing wsclass.WSClassified, keyed
// "pkgpath.TypeName" (identical key space to reflectTypeKey).
func discoverWSClassifiedImplementers(t *testing.T) map[string]struct{} {
	t.Helper()

	// Load the module from its root (tests run in the package dir).
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found from %s: %v", root, err)
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedDeps | packages.NeedImports,
		Dir:  root,
		// Tests included so a test-only implementer is caught too.
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	var errs strings.Builder
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			// Ill-typed files in unrelated packages (build-tag churn,
			// etc.) must not break the sweep; log them for diagnosis.
			errs.WriteString(p.PkgPath + ": " + e.Msg + "\n")
		}
	})
	if errs.Len() > 0 {
		t.Logf("packages.Load reported errors (continuing — the implementer scan works off whatever type-checked):\n%s", errs.String())
	}

	// Resolve the wsclass.WSClassified interface from the loaded package.
	var iface *types.Interface
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if iface != nil || p.PkgPath != "github.com/caimlas/meept/internal/comm/wsclass" {
			return
		}
		obj := p.Types.Scope().Lookup("WSClassified")
		if obj == nil {
			return
		}
		if named, ok := obj.Type().(*types.Named); ok {
			if underlying, ok := named.Underlying().(*types.Interface); ok {
				iface = underlying
			}
		}
	})
	if iface == nil {
		t.Fatal("wsclass.WSClassified not found in loaded packages")
	}

	found := map[string]struct{}{}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.Types == nil || p.Types.Scope() == nil {
			return
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok || tn.IsAlias() {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			if _, isIface := named.Underlying().(*types.Interface); isIface {
				continue
			}
			if types.Implements(named, iface) {
				found[p.PkgPath+"."+name] = struct{}{}
			}
		}
	})
	return found
}
