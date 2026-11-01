package toolprov

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/tools"
)

// stubProvider is a minimal Provider implementation for gate tests.
type stubProvider struct {
	name  string
	tools []tools.Tool
	class RiskClass
}

func (p *stubProvider) Name() string               { return p.name }
func (p *stubProvider) Discover() []tools.Tool     { return p.tools }
func (p *stubProvider) PermissionClass() RiskClass { return p.class }
func (p *stubProvider) Broker() *secrets.Broker    { return nil }

// stubSeamTool is a minimal tools.Tool contributed by a provider.
type stubSeamTool struct {
	name string
}

func (t *stubSeamTool) Name() string        { return t.name }
func (t *stubSeamTool) Description() string { return "stub tool " + t.name }

func (t *stubSeamTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{},
	}
}

func (t *stubSeamTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"result": "ok"}, nil
}

func (t *stubSeamTool) IsReadOnly(map[string]any) bool        { return true }
func (t *stubSeamTool) IsConcurrencySafe(map[string]any) bool { return true }

// clearFactories resets the package-level factory map between tests and
// restores it afterwards, so init-populated state cannot leak between
// tests.
func clearFactories(t *testing.T) {
	t.Helper()
	saved := factories
	factories = make(map[string]func() Provider)
	t.Cleanup(func() { factories = saved })
}

// TestProviderRegisterDiscover covers F1 (T1/T2 of 01-seam.md): init-time
// factory registration, duplicate-name protection, sorted Names, lazy
// Factory lookup, and the RegisterAll walk into a registry. RED until
// 01-seam.md declares Register/Factory/Names/RegisterAll and implements
// their behavior.
func TestProviderRegisterDiscover(t *testing.T) {
	t.Run("Register then Factory returns construction thunk without invoking", func(t *testing.T) {
		clearFactories(t)

		constructed := false
		err := Register("alpha", func() Provider {
			constructed = true
			return &stubProvider{name: "alpha", class: RiskReadOnly}
		})
		if err != nil {
			t.Fatalf("Register(alpha) returned error: %v", err)
		}

		f, ok := Factory("alpha")
		if !ok {
			t.Fatal("Factory(alpha) not found after Register")
		}
		if constructed {
			t.Fatal("Factory lookup constructed the provider; construction must be lazy")
		}
		p := f()
		if !constructed {
			t.Fatal("invoking the factory thunk did not construct the provider")
		}
		if p.Name() != "alpha" {
			t.Errorf("provider Name() = %q, want %q", p.Name(), "alpha")
		}
	})

	t.Run("duplicate name Register errors and first factory is untouched", func(t *testing.T) {
		clearFactories(t)

		first := &stubProvider{name: "alpha", class: RiskReadOnly}
		if err := Register("alpha", func() Provider { return first }); err != nil {
			t.Fatalf("first Register(alpha) returned error: %v", err)
		}

		err := Register("alpha", func() Provider { return &stubProvider{name: "alpha"} })
		if err == nil {
			t.Fatal("second Register(alpha) succeeded; duplicate provider names must error, never silently overwrite")
		}
		if !strings.Contains(err.Error(), "alpha") {
			t.Errorf("duplicate error %q does not name the clashing provider", err.Error())
		}

		f, ok := Factory("alpha")
		if !ok {
			t.Fatal("Factory(alpha) missing after duplicate rejection")
		}
		if got := f(); got != Provider(first) {
			t.Error("duplicate Register replaced the first factory; the first must stay untouched")
		}
	})

	t.Run("Names returns sorted registered names", func(t *testing.T) {
		clearFactories(t)

		for _, name := range []string{"zeta", "alpha", "mu"} {
			n := name
			if err := Register(n, func() Provider { return &stubProvider{name: n} }); err != nil {
				t.Fatalf("Register(%s) returned error: %v", n, err)
			}
		}

		got := Names()
		want := []string{"alpha", "mu", "zeta"}
		if len(got) != len(want) {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Names() = %v, want sorted %v", got, want)
			}
		}
	})

	t.Run("RegisterAll registers Discover tools and returns the count", func(t *testing.T) {
		clearFactories(t)

		if err := Register("alpha", func() Provider {
			return &stubProvider{
				name:  "alpha",
				class: RiskMutating,
				tools: []tools.Tool{
					&stubSeamTool{name: "alpha_one"},
					&stubSeamTool{name: "alpha_two"},
				},
			}
		}); err != nil {
			t.Fatalf("Register(alpha) returned error: %v", err)
		}
		if err := Register("beta", func() Provider {
			return &stubProvider{
				name:  "beta",
				class: RiskReadOnly,
				tools: []tools.Tool{&stubSeamTool{name: "beta_one"}},
			}
		}); err != nil {
			t.Fatalf("Register(beta) returned error: %v", err)
		}

		reg := tools.NewRegistry(nil)
		n, err := RegisterAll(reg, []string{"alpha", "beta"})
		if err != nil {
			t.Fatalf("RegisterAll returned error: %v", err)
		}
		if n != 3 {
			t.Errorf("RegisterAll count = %d, want 3 (2 alpha tools + 1 beta tool)", n)
		}
		for _, name := range []string{"alpha_one", "alpha_two", "beta_one"} {
			if reg.Get(name) == nil {
				t.Errorf("tool %q missing from registry after RegisterAll", name)
			}
		}
	})

	t.Run("RegisterAll unknown name errors listing known names", func(t *testing.T) {
		clearFactories(t)

		if err := Register("alpha", func() Provider {
			return &stubProvider{
				name:  "alpha",
				tools: []tools.Tool{&stubSeamTool{name: "alpha_one"}},
			}
		}); err != nil {
			t.Fatalf("Register(alpha) returned error: %v", err)
		}

		reg := tools.NewRegistry(nil)
		_, err := RegisterAll(reg, []string{"alpha", "missing"})
		if err == nil {
			t.Fatal("RegisterAll with unknown provider name succeeded; it must error")
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Errorf("error %q does not name the unknown provider", err.Error())
		}
		if !strings.Contains(err.Error(), "alpha") {
			t.Errorf("error %q does not list known name %q", err.Error(), "alpha")
		}
	})

	t.Run("RegisterAll surfaces duplicate tool names as an error", func(t *testing.T) {
		clearFactories(t)

		if err := Register("alpha", func() Provider {
			return &stubProvider{
				name: "alpha",
				tools: []tools.Tool{
					&stubSeamTool{name: "dup_tool"},
					&stubSeamTool{name: "dup_tool"},
				},
			}
		}); err != nil {
			t.Fatalf("Register(alpha) returned error: %v", err)
		}

		// F1 duplicate guard: two tools sharing a name inside one provider's
		// Discover set must error, never silently overwrite. Registry.Register
		// replaces silently today, so the seam must add duplicate detection.
		reg := tools.NewRegistry(nil)
		if _, err := RegisterAll(reg, []string{"alpha"}); err == nil {
			t.Fatal("RegisterAll accepted two Discover tools with the same name; duplicate tool names must error")
		}
	})

	t.Run("Discover sets land in the registry with names unchanged", func(t *testing.T) {
		clearFactories(t)

		if err := Register("alpha", func() Provider {
			return &stubProvider{
				name:  "alpha",
				tools: []tools.Tool{&stubSeamTool{name: "alpha_one"}},
			}
		}); err != nil {
			t.Fatalf("Register(alpha) returned error: %v", err)
		}

		reg := tools.NewRegistry(nil)
		if _, err := RegisterAll(reg, []string{"alpha"}); err != nil {
			t.Fatalf("RegisterAll returned error: %v", err)
		}
		got := reg.Get("alpha_one")
		if got == nil {
			t.Fatal("alpha_one missing from registry after provider walk")
		}
		if fmt.Sprint(got.Name()) != "alpha_one" {
			t.Errorf("registered tool Name() = %q, want alpha_one", got.Name())
		}
	})
}
