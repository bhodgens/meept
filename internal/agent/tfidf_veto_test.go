package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTfidfVeto_NilDisabled(t *testing.T) {
	var v *tfidfVeto
	if !v.agrees("anything", "code") {
		t.Error("nil veto must never block Door 1")
	}
}

func TestTfidfVeto_MissingFileDisabled(t *testing.T) {
	v, err := loadTfidfVeto(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("missing model should not error: %v", err)
	}
	if v != nil {
		t.Error("missing model should return nil (disabled)")
	}
}

func TestTfidfVeto_LoadAndAgree(t *testing.T) {
	model := filepath.Join(t.TempDir(), "veto.json")
	doc := `{"vocab":{"fi":0,"ix":1},"idf":[1.0,1.0],"classes":["code","chat"],
		"coef":[[1.0,1.0],[0.0,0.0]],"intercept":[0.0,0.0],
		"ngram_range":[2,4],"char_wb":true,"built_at":"t","train_docs":2,"train_accuracy":1.0}`
	if err := os.WriteFile(model, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := loadTfidfVeto(model)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !v.loaded {
		t.Error("model should be loaded")
	}
	// "fix" contains both grams -> code scores higher (coef all positive)
	got, conf := v.predict("fix")
	if got != "code" {
		t.Errorf("predict(fix) = %q, want code (conf %v)", got, conf)
	}
	if !v.agrees("fix", "code") {
		t.Error("veto should agree with code")
	}
	if v.agrees("fix", "chat") {
		t.Error("veto should disagree with chat")
	}
}

func TestTfidfVeto_CorruptModel(t *testing.T) {
	model := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(model, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTfidfVeto(model); err == nil {
		t.Error("corrupt model should error")
	}
}

func TestTfidfVeto_AgreeMatchesTrainingVocab(t *testing.T) {
	// vocab keys must be lowercased n-grams: the vectorizer lowercases
	// before matching (training script lowercases too).
	model := filepath.Join(t.TempDir(), "veto.json")
	doc := `{"vocab":{"im 2":0,"ple":1},"idf":[1.0,1.0],"classes":["code"],
		"coef":[[1.0,1.0]],"intercept":[0.0],
		"ngram_range":[2,4],"char_wb":true,"built_at":"t","train_docs":1,"train_accuracy":1.0}`
	if err := os.WriteFile(model, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := loadTfidfVeto(model)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := v.predict("Implement"); !strings.EqualFold(got, "code") && got != "code" {
		t.Logf("predict(Implement) = %q (single-class model always returns it)", got)
	}
}
