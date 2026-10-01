package validator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Pins for the data-driven language word tables: a maintainer adds a
// Latin-script language (e.g. French) by dropping <code>.txt into
// $MEEPT_HOME/validator/lang — no Go changes. Detection must then name
// that language with a usable confidence, and the expected-language
// filter must PASS output in the configured language.

func TestLoadLanguageWordTables_French(t *testing.T) {
	dir := t.TempDir()
	// Minimal but realistic French function-word list (>20 words so it
	// clears the load floor).
	fr := `# french function words
le la les un une des du de au aux et ou mais donc or ni car je tu il elle nous vous ils elles
ce cet cette ces mon ton son ma ta sa mes tes ses notre votre leurs qui que quoi dont où est
sont était étaient a ai as ont avons avez sera seront être avoir fait faire plus moins très
`
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(fr), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadLanguageWordTables(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { _ = LoadLanguageWordTables(filepath.Join(t.TempDir())) }) // reset for other tests

	code, conf := detectedLanguage("le chat est sur la table et il est très content de la voir")
	if code != "fr" || conf < 0.5 {
		t.Fatalf("detectedLanguage(french prose) = (%q, %.2f), want (fr, >=0.50)", code, conf)
	}

	// The French-expecting filter passes French output...
	f := NewLanguageFilter("fr")
	if r := f.Process(context.TODO(), nil, "le chat est sur la table et il est très content"); r.Outcome != FilterPass {
		t.Fatalf("french output vs language_fr: %+v, want pass", r)
	}
	// ...and still fails English output.
	if r := f.Process(context.TODO(), nil, "the cat is on the table and it is very happy to see it"); r.Outcome != FilterFail {
		t.Fatalf("english output vs language_fr: %+v, want fail", r)
	}
}

func TestLoadLanguageWordTables_MissingDirIsNormal(t *testing.T) {
	if err := LoadLanguageWordTables(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatalf("missing dir should be a no-op, got %v", err)
	}
	if lookupExtraLangSet("fr") != nil {
		t.Fatalf("fr table present after a failed load; want nil")
	}
}

func TestLoadLanguageWordTables_TooSmallRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xx.txt"), []byte("le la les"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := LoadLanguageWordTables(dir)
	if err == nil {
		t.Fatalf("3-word table accepted; want an error naming the 20-word floor")
	}
	if lookupExtraLangSet("xx") != nil {
		t.Fatalf("rejected table still registered")
	}
}

func TestLoadLanguageWordTables_BuiltinsNotShadowable(t *testing.T) {
	dir := t.TempDir()
	// A user en.txt/de.txt must NOT replace the code-defined tables.
	if err := os.WriteFile(filepath.Join(dir, "en.txt"), []byte("zzz yyy xxx www qqq"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadLanguageWordTables(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { _ = LoadLanguageWordTables(filepath.Join(t.TempDir())) })
	code, _ := detectedLanguage("the cat is on the table and it is very happy to see it now")
	if code != "en" {
		t.Fatalf("user en.txt shadowed the builtin table: detected %q, want en", code)
	}
}
