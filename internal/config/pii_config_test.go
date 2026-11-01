package config

import (
	"reflect"
	"testing"
)

// T3 (pii leaf 02): zero-value PiiConfig is disabled - the opt-in default.
// Flipping any default here is a product decision (opt-in-defaults rule).
func TestPiiConfigDefaults(t *testing.T) {
	var c PiiConfig
	if c.Enabled {
		t.Fatal("PiiConfig.Enabled zero value = true; must default false (opt-in)")
	}
	if c.SecretRestore {
		t.Fatal("PiiConfig.SecretRestore zero value = true; must default false")
	}
	if c.RestoreWindow != 0 {
		t.Fatalf("PiiConfig.RestoreWindow zero value = %v; must default 0 (session lifetime)", c.RestoreWindow)
	}
	if len(c.Classes) != 0 {
		t.Fatalf("PiiConfig.Classes zero value = %v; empty means all default classes", c.Classes)
	}

	// SecurityConfig carries the section and survives a zero-value embed.
	var sc SecurityConfig
	if sc.Pii.Enabled || sc.Pii.SecretRestore {
		t.Fatal("SecurityConfig.Pii zero value must be fully disabled")
	}
}

// T3: the struct tags match contract C4-B exactly (json + toml).
func TestPiiConfigTags(t *testing.T) {
	c := PiiConfig{}
	typ := fieldTags(c)
	want := map[string]string{
		"Enabled":       "enabled",
		"Classes":       "classes",
		"RestoreWindow": "restore_window",
		"SecretRestore": "secret_restore",
	}
	for f, tag := range want {
		if typ[f] != tag {
			t.Fatalf("field %s json tag = %q, want %q", f, typ[f], tag)
		}
	}

	// SecurityConfig embeds the section under "pii".
	sc := SecurityConfig{}
	if fieldTags(sc)["Pii"] != "pii" {
		t.Fatalf("SecurityConfig.Pii json tag = %q, want %q", fieldTags(sc)["Pii"], "pii")
	}
}

// fieldTags maps exported field names to their json tag via reflection.
func fieldTags(v any) map[string]string {
	out := map[string]string{}
	rt := reflect.TypeOf(v)
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		out[f.Name] = f.Tag.Get("json")
	}
	return out
}
