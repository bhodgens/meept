// Package mutation provides mutation testing harness for Go tests.
//
// Mutation testing intentionally introduces bugs into code to verify that
// the test suite catches them. If a test passes with mutated code, the
// test is insufficient.
//
// Usage:
//
//	func TestSomething(t *testing.T) {
//	    mutation.RunMutationTest(t, func() {
//	        // Apply mutation here
//	    }, func() error {
//	        // Run the test being validated
//	        return runTest()
//	    })
//	}
package mutation

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// MutatorType identifies the type of mutation applied.
type MutatorType string

const (
	// MutatorSwapConstraintNode swaps the HasField.Node to wrong capture
	MutatorSwapConstraintNode MutatorType = "swap_constraint_node"
	// MutatorInvertCondition inverts if ok { } to if !ok { }
	MutatorInvertCondition MutatorType = "invert_condition"
	// MutatorZeroReturn changes return value to zero value
	MutatorZeroReturn MutatorType = "zero_return"
	// MutatorSkipNilCheck removes nil guard checks
	MutatorSkipNilCheck MutatorType = "skip_nil_check"
	// MutatorOffByOne changes loop bounds by 1
	MutatorOffByOne MutatorType = "off_by_one"
)

// MutationResult describes the outcome of a mutation test.
type MutationResult struct {
	MutatorType  MutatorType
	FilePath     string
	LineNumber   int
	Description  string
	TestPassed   bool // if true, test passed with mutation = BAD (insufficient test)
	ExpectedFail bool // if true, test was expected to fail
	Error        error
}

// RunMutationTest runs a mutation test.
//
// mutateFn applies the mutation (e.g., modifies a value, changes a condition)
// testFn runs the test being validated and returns an error if it fails.
//
// The mutation test PASSES (test suite is good) if:
//   - Original test passes (sanity check)
//   - Mutated test fails (test catches the bug)
//
// The mutation test FAILS (test suite is insufficient) if:
//   - Mutated test still passes (test didn't catch the bug)
func RunMutationTest(t *testing.T, mutateFn func(), testFn func() error) {
	t.Helper()

	// Step 1: Run original test - must pass
	origErr := testFn()
	if origErr != nil {
		t.Fatalf("mutation test: original test must pass, got error: %v", origErr)
	}

	// Step 2: Apply mutation
	mutateFn()

	// Step 3: Run test with mutation - must fail
	mutatedErr := testFn()
	if mutatedErr == nil {
		t.Errorf("mutation test FAILED: test passed with mutated code (insufficient test coverage)")
	} else {
		t.Logf("mutation test PASSED: test correctly failed with mutation: %v", mutatedErr)
	}
}

// FileMutator provides file-level mutation operations.
type FileMutator struct {
	fset *token.FileSet
}

// NewFileMutator creates a new file mutator.
func NewFileMutator() *FileMutator {
	return &FileMutator{
		fset: token.NewFileSet(),
	}
}

// MutateInCondition inverts a boolean condition in an if statement.
// Returns the mutated source line and true if mutation was applied.
func (m *FileMutator) MutateInCondition(source string, lineNum int) (string, bool) {
	lines := strings.Split(source, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return source, false
	}

	line := lines[lineNum-1]
	mutated := line

	// Pattern: if <expr> { -> if !(<expr>) {
	ifPattern := regexp.MustCompile(`if\s+(\S+)\s*\{`)
	if ifPattern.MatchString(line) {
		matches := ifPattern.FindStringSubmatch(line)
		if len(matches) == 2 {
			expr := matches[1]
			// Don't double-negate
			if strings.HasPrefix(expr, "!") {
				// Remove double negation: if !x { -> if x {
				mutated = ifPattern.ReplaceAllString(line, "if ("+expr[1:]+") {")
			} else {
				mutated = ifPattern.ReplaceAllString(line, "if !("+expr+") {")
			}
			lines[lineNum-1] = mutated
			return strings.Join(lines, "\n"), true
		}
	}

	return source, false
}

// MutateReturnToNil changes a return statement to return nil/zero value.
func (m *FileMutator) MutateReturnToNil(source string, lineNum int) (string, bool) {
	lines := strings.Split(source, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return source, false
	}

	line := lines[lineNum-1]

	// Pattern: return <expr>, nil -> return nil, nil
	// Pattern: return <expr> -> return nil
	returnPattern := regexp.MustCompile(`return\s+(\S+)(,\s*(nil|error))?`)
	if returnPattern.MatchString(line) {
		matches := returnPattern.FindStringSubmatch(line)
		if len(matches) >= 2 {
			if matches[3] != "" {
				// Has error return: return value, err -> return nil, err
				mutated := returnPattern.ReplaceAllString(line, "return nil, "+matches[3])
				lines[lineNum-1] = mutated
				return strings.Join(lines, "\n"), true
			} else {
				// Simple return: return value -> return nil
				mutated := returnPattern.ReplaceAllString(line, "return nil")
				lines[lineNum-1] = mutated
				return strings.Join(lines, "\n"), true
			}
		}
	}

	return source, false
}

// ASTMutator provides AST-level mutation operations.
type ASTMutator struct{}

// NewASTMutator creates a new AST mutator.
func NewASTMutator() *ASTMutator {
	return &ASTMutator{}
}

// MutationVisitor implements ast.Visitor for tracking mutations.
type MutationVisitor struct {
	Mutations  []string
	TargetNode ast.Node
	Mutated    bool
}

func (v *MutationVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}

	if v.TargetNode != nil && node == v.TargetNode {
		v.Mutated = true
		v.Mutations = append(v.Mutations, fmt.Sprintf("mutated node at %T", node))
	}

	return v
}

// GenerateMutationReport creates a mutation test report.
type MutationReport struct {
	TotalMutations    int
	KilledMutations   int
	SurvivedMutations int
	MutationScore     float64
	Results           []MutationResult
}

// AddResult adds a mutation result to the report.
func (r *MutationReport) AddResult(res MutationResult) {
	r.TotalMutations++
	if res.TestPassed {
		r.SurvivedMutations++ // Bad: test didn't catch mutation
	} else {
		r.KilledMutations++ // Good: test caught mutation
	}
	r.Results = append(r.Results, res)
	r.MutationScore = float64(r.KilledMutations) / float64(r.TotalMutations) * 100
}

// String returns a human-readable report.
func (r *MutationReport) String() string {
	var sb strings.Builder
	sb.WriteString("Mutation Testing Report\n")
	sb.WriteString("=======================\n")
	sb.WriteString(fmt.Sprintf("Total mutations: %d\n", r.TotalMutations))
	sb.WriteString(fmt.Sprintf("Killed (caught): %d\n", r.KilledMutations))
	sb.WriteString(fmt.Sprintf("Survived (missed): %d\n", r.SurvivedMutations))
	sb.WriteString(fmt.Sprintf("Mutation score: %.1f%%\n", r.MutationScore))
	sb.WriteString("\n")

	if len(r.Results) > 0 {
		sb.WriteString("Details:\n")
		for _, res := range r.Results {
			status := "KILLED"
			if res.TestPassed {
				status = "SURVIVED (test insufficient)"
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s at %s:%d - %s\n",
				status, res.MutatorType, res.FilePath, res.LineNumber, res.Description))
		}
	}

	return sb.String()
}

// RunFileMutations runs mutation testing on a Go source file.
// Returns a report of which mutations were caught by existing tests.
func RunFileMutations(t *testing.T, filePath string, testFn func() error) *MutationReport {
	t.Helper()

	report := &MutationReport{}

	// Parse the file
	source, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", filePath, err)
	}

	_, err = parser.ParseFile(token.NewFileSet(), filePath, source, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse file %s: %v", filePath, err)
	}

	mutator := NewFileMutator()
	lineCount := len(strings.Split(string(source), "\n"))

	// Try mutations on each line
	for i := 1; i <= lineCount; i++ {
		// Try condition inversion
		mutatedSource, ok := mutator.MutateInCondition(string(source), i)
		if ok {
			result := MutationResult{
				MutatorType:  MutatorInvertCondition,
				FilePath:     filePath,
				LineNumber:   i,
				Description:  "Inverted condition on line " + fmt.Sprint(i),
				ExpectedFail: true,
			}

			// Write mutated source to temp file
			tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("mutation_%d.go", i))
			os.WriteFile(tmpFile, []byte(mutatedSource), 0644)
			defer os.Remove(tmpFile)

			// Run test
			testErr := testFn()
			result.TestPassed = (testErr == nil)

			report.AddResult(result)
		}
	}

	return report
}
