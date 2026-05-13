package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// GendocTag represents a parsed gendoc tag
type GendocTag struct {
	Key   string
	Value string
}

// StructInfo holds information about a struct with gendoc tags
type StructInfo struct {
	Name        string
	Description string
	Section     string
	Example     string
	Fields      []FieldInfo
}

// FieldInfo holds information about a struct field
type FieldInfo struct {
	Name        string
	Type        string
	Description string
	Default     string
	Tag         string
}

// TemplateData holds data for the markdown template
type TemplateData struct {
	SectionName string
	Description string
	Example     string
	Fields      []FieldInfo
}

func main() {
	outputDir := flag.String("output", "docs/reference/generated/", "Output directory for generated docs")
	pkgPath := flag.String("pkg", "github.com/caimlas/meept/internal/config", "Go package to parse")
	flag.Parse()

	// Parse the specified package
	structs, err := parsePackage(*pkgPath)
	if err != nil {
		log.Fatalf("Failed to parse package %s: %v", *pkgPath, err)
	}

	// Generate markdown files
	if err := generateDocs(structs, *outputDir); err != nil {
		log.Fatalf("Failed to generate docs: %v", err)
	}

	fmt.Printf("Generated %d documentation files in %s\n", len(structs), *outputDir)
}

// parsePackage parses a Go package and extracts structs with gendoc tags
func parsePackage(pkgPath string) ([]StructInfo, error) {
	// Convert package path to filesystem path
	fsPath := strings.Replace(pkgPath, "github.com/caimlas/meept", ".", 1)
	fsPath = filepath.Join(fsPath, "*.go")

	files, err := filepath.Glob(fsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find Go files: %w", err)
	}

	var structs []StructInfo

	for _, file := range files {
		fileStructs, err := parseFile(file)
		if err != nil {
			log.Printf("Warning: Failed to parse %s: %v", file, err)
			continue
		}
		structs = append(structs, fileStructs...)
	}

	return structs, nil
}

// parseFile parses a single Go file and extracts structs with gendoc tags
func parseFile(filename string) ([]StructInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", filename, err)
	}

	var structs []StructInfo

	ast.Inspect(node, func(n ast.Node) bool {
		if typeSpec, ok := n.(*ast.TypeSpec); ok {
			if structType, ok := typeSpec.Type.(*ast.StructType); ok {
				structInfo := parseStruct(typeSpec, structType, node.Comments)
				if structInfo.Section != "" {
					structs = append(structs, structInfo)
				}
			}
		}
		return true
	})

	return structs, nil
}

// parseStruct extracts information from a struct definition
func parseStruct(typeSpec *ast.TypeSpec, structType *ast.StructType, comments []*ast.CommentGroup) StructInfo {
	structInfo := StructInfo{
		Name: typeSpec.Name.Name,
	}

	// Find the comment group that immediately precedes this type spec
	var docComment *ast.CommentGroup
	for _, commentGroup := range comments {
		if commentGroup.End() < typeSpec.Pos() && (docComment == nil || commentGroup.End() > docComment.End()) {
			docComment = commentGroup
		}
	}

	// Parse gendoc tags from the associated comment
	if docComment != nil {
		for _, comment := range docComment.List {
			if strings.Contains(comment.Text, "//gendoc:") {
				tags := parseGendocTags(comment.Text)
				for _, tag := range tags {
					switch tag.Key {
					case "section":
						structInfo.Section = tag.Value
					case "desc":
						structInfo.Description = tag.Value
					case "example":
						structInfo.Example = tag.Value
					}
				}
			}
		}
	}

	// Extract field information
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue // Skip embedded fields
		}

		fieldName := field.Names[0].Name
		fieldType := exprToString(field.Type)
		fieldDesc := ""
		fieldDefault := ""
		fieldTag := ""

		// Extract description from field comment
		if field.Doc != nil && len(field.Doc.List) > 0 {
			fieldDesc = strings.TrimSpace(strings.TrimPrefix(field.Doc.List[0].Text, "//"))
		}

		// Extract tag
		if field.Tag != nil {
			fieldTag = strings.Trim(field.Tag.Value, "`")
		}

		fieldInfo := FieldInfo{
			Name:        fieldName,
			Type:        fieldType,
			Description: fieldDesc,
			Default:     fieldDefault,
			Tag:         fieldTag,
		}

		structInfo.Fields = append(structInfo.Fields, fieldInfo)
	}

	return structInfo
}

// parseGendocTags extracts gendoc tags from a comment line
func parseGendocTags(comment string) []GendocTag {
	var tags []GendocTag

	lines := strings.SplitSeq(comment, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "//gendoc:"); ok {
			parts := strings.SplitN(after, " ", 2)
			if len(parts) == 2 {
				tags = append(tags, GendocTag{
					Key:   strings.TrimSpace(parts[0]),
					Value: strings.TrimSpace(parts[1]),
				})
			}
		}
	}

	return tags
}

// exprToString converts an AST expression to a string representation
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.StructType:
		return "struct{...}"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// generateDocs creates markdown files for each struct
func generateDocs(structs []StructInfo, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil { //nolint:gosec // generated docs directory is public
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	tmpl, err := template.New("config").Parse(markdownTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	for _, structInfo := range structs {
		if structInfo.Section == "" {
			continue
		}

		filename := filepath.Join(outputDir, structInfo.Section+".md")
		file, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", filename, err)
		}
		defer file.Close()

		data := TemplateData{
			SectionName: structInfo.Section,
			Description: structInfo.Description,
			Example:     structInfo.Example,
			Fields:      structInfo.Fields,
		}

		if err := tmpl.Execute(file, data); err != nil {
			return fmt.Errorf("failed to execute template for %s: %w", structInfo.Section, err)
		}
	}

	return nil
}

// markdownTemplate is the template for generating config reference documentation
const markdownTemplate = "# {{.SectionName}}\n\n{{.Description}}\n\n{{if .Example}}\n## Example\n\n```toml\n{{.Example}}\n```\n{{end}}\n\n## Fields\n\n| Field | Type | Description | Default |\n|-------|------|-------------|---------|\n{{range .Fields}}| {{.Name}} | {{.Type}} | {{.Description}} | {{.Default}} |\n{{end}}\n"
