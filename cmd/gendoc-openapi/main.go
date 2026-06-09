// gendoc-openapi generates OpenAPI 3.0 specifications from Go HTTP handlers and service types.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SchemaInfo holds schema (model) information
type SchemaInfo struct {
	Name        string
	Description string
	Properties  []PropertyInfo
	Required    []string
}

// PropertyInfo holds schema property information
type PropertyInfo struct {
	Name        string
	Type        string
	Format      string
	Description string
	Enum        []string
	Nullable    bool
}

// EndpointInfo holds parsed endpoint information
type EndpointInfo struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Tags        []string
	Handler     string
}

// APIInfo holds the parsed API specification
type APIInfo struct {
	Title       string
	Description string
	Version     string
	Endpoints   []EndpointInfo
	Schemas     []SchemaInfo
	Tags        []string
}

func main() {
	outputPath := flag.String("output", "docs/reference/http-api/openapi.yaml", "Output file path for OpenAPI spec")
	handlersPath := flag.String("handlers", "internal/comm/http", "Path to HTTP handlers package")
	servicesPath := flag.String("services", "internal/services", "Path to services package")
	flag.Parse()

	apiInfo := &APIInfo{
		Title:       "Meept HTTP API",
		Description: "REST API for Meept daemon - exposes chat, memory, task queue, skills, and self-improvement capabilities",
		Version:     "0.2.0",
	}

	// Parse schemas from services
	schemas, err := parseServices(*servicesPath)
	if err != nil {
		log.Printf("Warning: Failed to parse services: %v", err)
	}
	apiInfo.Schemas = schemas

	// Parse endpoints from handlers
	endpoints, tags, err := parseHandlers(*handlersPath)
	if err != nil {
		log.Printf("Warning: Failed to parse handlers: %v", err)
	}
	apiInfo.Endpoints = endpoints
	apiInfo.Tags = uniqueStrings(tags)

	// Generate OpenAPI YAML
	yamlContent := generateOpenAPI(apiInfo)

	// Write output
	if err := os.WriteFile(*outputPath, []byte(yamlContent), 0o644); err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}

	fmt.Printf("Generated OpenAPI spec with %d endpoints and %d schemas to %s\n", len(endpoints), len(schemas), *outputPath)
}

// parseServices extracts schema definitions from service files
func parseServices(servicesPath string) ([]SchemaInfo, error) {
	var schemas []SchemaInfo

	err := filepath.WalkDir(servicesPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		fileSchemas, err := parseServiceFile(path)
		if err != nil {
			log.Printf("Warning: %v", err)
		}
		schemas = append(schemas, fileSchemas...)
		return nil
	})

	return schemas, err
}

// parseServiceFile parses a service file for struct definitions
func parseServiceFile(filename string) ([]SchemaInfo, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var schemas []SchemaInfo

	ast.Inspect(node, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		schemaInfo := parseSchema(typeSpec, structType)
		if schemaInfo.Name != "" {
			schemas = append(schemas, schemaInfo)
		}

		return true
	})

	return schemas, nil
}

// parseSchema extracts schema info from a struct
func parseSchema(typeSpec *ast.TypeSpec, structType *ast.StructType) SchemaInfo {
	schema := SchemaInfo{
		Name: typeSpec.Name.Name,
	}

	if typeSpec.Doc != nil {
		for _, comment := range typeSpec.Doc.List {
			text := strings.TrimPrefix(comment.Text, "//")
			text = strings.TrimSpace(text)
			if text != "" {
				schema.Description = text
				break
			}
		}
	}

	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		prop := PropertyInfo{
			Name: field.Names[0].Name,
		}

		prop.Type = exprToString(field.Type)
		prop.Nullable = isNullableType(field.Type)

		if field.Tag != nil {
			tag := field.Tag.Value
			if jsonTag := extractJSONTag(tag); jsonTag != "" {
				prop.Name = jsonTag
			}
		}

		if field.Doc != nil && len(field.Doc.List) > 0 {
			prop.Description = strings.TrimSpace(strings.TrimPrefix(field.Doc.List[0].Text, "//"))
		}

		if field.Tag != nil {
			tag := field.Tag.Value
			if !strings.Contains(tag, "omitempty") {
				schema.Required = append(schema.Required, prop.Name)
			}
		}

		schema.Properties = append(schema.Properties, prop)
	}

	return schema
}

// parseHandlers parses handler files for endpoint registrations
func parseHandlers(handlersPath string) ([]EndpointInfo, []string, error) {
	var endpoints []EndpointInfo
	var tags []string

	files, err := filepath.Glob(filepath.Join(handlersPath, "*.go"))
	if err != nil {
		return nil, nil, err
	}

	for _, file := range files {
		fileEndpoints, fileTags, err := parseHandlerFile(file)
		if err != nil {
			log.Printf("Warning: %v", err)
			continue
		}
		endpoints = append(endpoints, fileEndpoints...)
		tags = append(tags, fileTags...)
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Path < endpoints[j].Path
	})

	return endpoints, tags, nil
}

// parseHandlerFile extracts endpoint registrations from a handler file
func parseHandlerFile(filename string) ([]EndpointInfo, []string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, err
	}

	var endpoints []EndpointInfo
	var tags []string

	// Pattern: mux.HandleFunc("GET /path", handlerName)
	muxPattern := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) ([^"]+)", ([a-zA-Z0-9_.]+)\)`)

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "mux.HandleFunc") && !strings.Contains(line, "fmt.Sprintf") {
			matches := muxPattern.FindStringSubmatch(line)
			if len(matches) >= 4 {
				method := matches[1]
				path := matches[2]
				handler := matches[3]

				endpoint := EndpointInfo{
					Method:  method,
					Path:    path,
					Summary: handlerNameToSummary(handler),
					Tags:    []string{pathToTag(path)},
					Handler: handler,
				}

				endpoints = append(endpoints, endpoint)
				tags = append(tags, endpoint.Tags...)
			}
		}
	}

	return endpoints, tags, nil
}

// generateOpenAPI generates the OpenAPI 3.0 YAML specification
func generateOpenAPI(info *APIInfo) string {
	var sb strings.Builder

	sb.WriteString("openapi: 3.0.3\n")
	sb.WriteString("info:\n")
	sb.WriteString(fmt.Sprintf("  title: %s\n", info.Title))
	sb.WriteString(fmt.Sprintf("  description: %s\n", info.Description))
	sb.WriteString(fmt.Sprintf("  version: %s\n", info.Version))
	sb.WriteString("\n")
	sb.WriteString("servers:\n")
	sb.WriteString("  - url: http://localhost:8081\n")
	sb.WriteString("    description: Local development server\n")
	sb.WriteString("\n")

	// Tags
	if len(info.Tags) > 0 {
		sb.WriteString("tags:\n")
		for _, tag := range info.Tags {
			sb.WriteString(fmt.Sprintf("  - name: %s\n", tag))
		}
		sb.WriteString("\n")
	}

	// Security schemes
	sb.WriteString("components:\n")
	sb.WriteString("  securitySchemes:\n")
	sb.WriteString("    ApiKeyAuth:\n")
	sb.WriteString("      type: apiKey\n")
	sb.WriteString("      in: header\n")
	sb.WriteString("      name: Authorization\n")
	sb.WriteString("      description: \"API key authentication. Use: Authorization: Bearer YOUR_API_KEY\"\n")
	sb.WriteString("\n")

	// Schemas
	if len(info.Schemas) > 0 {
		sb.WriteString("  schemas:\n")
		for _, schema := range info.Schemas {
			sb.WriteString(fmt.Sprintf("    %s:\n", schema.Name))
			if schema.Description != "" {
				sb.WriteString(fmt.Sprintf("      description: %s\n", schema.Description))
			}
			sb.WriteString("      type: object\n")
			if len(schema.Properties) > 0 {
				sb.WriteString("      properties:\n")
				for _, prop := range schema.Properties {
					sb.WriteString(fmt.Sprintf("        %s:\n", prop.Name))
					if prop.Description != "" {
						sb.WriteString(fmt.Sprintf("          description: %s\n", prop.Description))
					}
					sb.WriteString(fmt.Sprintf("          type: %s\n", mapGoTypeToOpenAPI(prop.Type)))
					if prop.Nullable {
						sb.WriteString("          nullable: true\n")
					}
				}
			}
			if len(schema.Required) > 0 {
				sb.WriteString("      required:\n")
				for _, req := range schema.Required {
					sb.WriteString(fmt.Sprintf("        - %s\n", req))
				}
			}
		}
		sb.WriteString("\n")
	}

	// Paths
	sb.WriteString("paths:\n")
	for _, endpoint := range info.Endpoints {
		pathKey := "/" + strings.TrimPrefix(endpoint.Path, "/")
		sb.WriteString(fmt.Sprintf("  %s:\n", pathKey))
		sb.WriteString(fmt.Sprintf("    %s:\n", strings.ToLower(endpoint.Method)))
		sb.WriteString(fmt.Sprintf("      summary: %s\n", endpoint.Summary))
		if endpoint.Description != "" {
			sb.WriteString(fmt.Sprintf("      description: %s\n", endpoint.Description))
		}
		if len(endpoint.Tags) > 0 {
			sb.WriteString("      tags:\n")
			for _, tag := range endpoint.Tags {
				sb.WriteString(fmt.Sprintf("        - %s\n", tag))
			}
		}
		sb.WriteString("      security:\n")
		sb.WriteString("        - ApiKeyAuth: []\n")
		sb.WriteString("      responses:\n")
		sb.WriteString("        '200':\n")
		sb.WriteString("          description: Success\n")
		sb.WriteString("        '400':\n")
		sb.WriteString("          description: Bad request\n")
		sb.WriteString("        '401':\n")
		sb.WriteString("          description: Unauthorized\n")
		sb.WriteString("        '500':\n")
		sb.WriteString("          description: Internal server error\n")
	}

	return sb.String()
}

// Helper functions

func handlerNameToSummary(name string) string {
	name = strings.TrimPrefix(name, "handle")
	name = strings.TrimSuffix(name, "Handler")
	return strings.ReplaceAll(name, "_", " ")
}

func pathToTag(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && parts[0] != "api" && parts[0] != "v1" {
		return parts[0]
	}
	if len(parts) > 1 {
		return parts[1]
	}
	return "default"
}

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

func extractJSONTag(tag string) string {
	jsonTag := strings.Split(tag, "json:\"")
	if len(jsonTag) < 2 {
		return ""
	}
	return strings.Split(jsonTag[1], "\"")[0]
}

func isNullableType(expr ast.Expr) bool {
	_, isPtr := expr.(*ast.StarExpr)
	_, isSlice := expr.(*ast.ArrayType)
	_, isMap := expr.(*ast.MapType)
	return isPtr || isSlice || isMap
}

func mapGoTypeToOpenAPI(goType string) string {
	switch {
	case strings.Contains(goType, "string"):
		return "string"
	case strings.Contains(goType, "int"), strings.Contains(goType, "uint"):
		return "integer"
	case strings.Contains(goType, "float"):
		return "number"
	case strings.Contains(goType, "bool"):
		return "boolean"
	case strings.Contains(goType, "[]"), strings.Contains(goType, "map"):
		return "array"
	case strings.Contains(goType, "time.Time"):
		return "string"
	default:
		return "object"
	}
}

func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
