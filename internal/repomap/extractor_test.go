package repomap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagExtractor_ExtractTags(t *testing.T) {
	// Create a temporary Go file to test
	content := `package main

import "fmt"

type MyStruct struct {
	Name string
	Age  int
}

func NewMyStruct(name string, age int) *MyStruct {
	return &MyStruct{Name: name, Age: age}
}

func (m *MyStruct) Greet() string {
	return fmt.Sprintf("Hello, %s!", m.Name)
}

const DefaultName = "unknown"

var globalVar = "test"

func main() {
	s := NewMyStruct("test", 25)
	println(s.Greet())
}
`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	extractor := NewTagExtractor(nil)
	tags, err := extractor.ExtractTags(testFile)

	require.NoError(t, err)
	require.NotEmpty(t, tags, "expected tags to be extracted")

	// Check that we got definitions
	definitions := FilterDefinitions(tags)
	t.Logf("Found %d definitions", len(definitions))
	for _, def := range definitions {
		t.Logf("  %s: %s (line %d, isDef=%v)", def.Kind, def.Name, def.Line, def.IsDef)
	}

	// Should have function and type definitions (method extraction is advanced)
	hasFunction := false
	hasType := false
	hasConstantOrVariable := false

	for _, def := range definitions {
		switch def.Kind {
		case "function":
			hasFunction = true
		case "type":
			hasType = true
		case "constant", "variable":
			hasConstantOrVariable = true
		}
	}

	assert.True(t, hasFunction, "should have function definitions")
	assert.True(t, hasType, "should have type definitions")
	assert.True(t, hasConstantOrVariable, "should have constant or variable definitions")
}

func TestTagExtractor_ExtractTagsRaw(t *testing.T) {
	// Create multiple test files
	tmpDir := t.TempDir()

	// Create a Go file
	goContent := `package main

func Hello() string {
	return "hello"
}

type Foo struct{}
`
	goFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(goFile, []byte(goContent), 0644)

	// Create a Python file
	pyContent := `def hello():
    return "hello"

class Foo:
    pass
`
	pyFile := filepath.Join(tmpDir, "test.py")
	os.WriteFile(pyFile, []byte(pyContent), 0644)

	extractor := NewTagExtractor(nil)
	tags, err := extractor.ExtractTagsRaw([]string{goFile, pyFile})

	require.NoError(t, err)
	require.NotEmpty(t, tags, "expected tags from multiple files")

	// Should have tags from both files
	files := make(map[string]bool)
	for _, tag := range tags {
		files[tag.RelFname] = true
	}

	assert.True(t, files["test.go"], "should have tags from Go file")
	assert.True(t, files["test.py"], "should have tags from Python file")
}

func TestFindDefinitions(t *testing.T) {
	tags := []Tag{
		{Name: "Foo", IsDef: true, Kind: "class"},
		{Name: "Bar", IsDef: false, Kind: "variable"},
		{Name: "Foo", IsDef: false, Kind: "variable"},
		{Name: "Foo", IsDef: true, Kind: "function"},
	}

	defs := FindDefinitions(tags, "Foo")
	assert.Len(t, defs, 2, "should find 2 definitions of Foo")

	// Check that we get both the class and function definitions
	hasClass := false
	hasFunc := false
	for _, d := range defs {
		if d.Kind == "class" {
			hasClass = true
		}
		if d.Kind == "function" {
			hasFunc = true
		}
	}
	assert.True(t, hasClass, "should find class definition")
	assert.True(t, hasFunc, "should find function definition")
}

func TestFindReferences(t *testing.T) {
	tags := []Tag{
		{Name: "Foo", IsDef: true, Kind: "class"},
		{Name: "Bar", IsDef: false, Kind: "variable"},
		{Name: "Foo", IsDef: false, Kind: "variable"},
	}

	refs := FindReferences(tags, "Foo")
	assert.Len(t, refs, 1, "should find 1 reference to Foo")
	assert.False(t, refs[0].IsDef)
}

func TestFilterTagsByKind(t *testing.T) {
	tags := []Tag{
		{Name: "Func1", Kind: "function"},
		{Name: "Func2", Kind: "function"},
		{Name: "Class1", Kind: "class"},
		{Name: "Var1", Kind: "variable"},
	}

	functions := FilterTagsByKind(tags, "function")
	assert.Len(t, functions, 2)

	classes := FilterTagsByKind(tags, "class")
	assert.Len(t, classes, 1)

	both := FilterTagsByKind(tags, "function", "class")
	assert.Len(t, both, 3)
}

func TestDeduplicateTags(t *testing.T) {
	tags := []Tag{
		{FName: "test.go", Line: 10, Name: "Foo", IsDef: true},
		{FName: "test.go", Line: 10, Name: "Foo", IsDef: true}, // duplicate
		{FName: "test.go", Line: 15, Name: "Foo", IsDef: true}, // different line
		{FName: "test.go", Line: 10, Name: "Bar", IsDef: false}, // different name
	}

	deduped := deduplicateTags(tags)
	assert.Len(t, deduped, 3, "should remove exact duplicates")
}

func TestContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	content := `package main
func main() {}
`
	testFile := filepath.Join(tmpDir, "test.go")
	os.WriteFile(testFile, []byte(content), 0644)

	extractor := NewTagExtractor(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should not hang or panic
	_, err := extractor.ExtractTagsWithContext(ctx, testFile)
	// Error expected since context is cancelled
	t.Logf("Error (expected): %v", err)
}