package ast

import (
	"context"
	"fmt"
)

// BlockSpan represents the line range of a syntactic block.
// StartLine and EndLine are 1-based and inclusive.
type BlockSpan struct {
	StartLine int    // 1-based
	EndLine   int    // 1-based, inclusive
	NodeType  string // e.g., "function_declaration"
}

// blockNodeTypes maps languages to AST node types that constitute "blocks"
// for the purpose of replace block / delete block operations.
// The order within each slice is significant: more specific types should
// appear before more general ones (e.g., method before function).
var blockNodeTypes = map[Language][]string{
	LangGo: {
		"function_declaration",
		"method_declaration",
		"func_literal",
		"type_declaration",
		"interface_type",
		"struct_type",
	},
	LangPython: {
		"function_definition",
		"class_definition",
	},
	LangTypeScript: {
		"function_declaration",
		"class_declaration",
		"method_definition",
		"arrow_function",
		"function_expression",
	},
	LangJavaScript: {
		"function_declaration",
		"class_declaration",
		"method_definition",
		"arrow_function",
		"function_expression",
	},
	LangRust: {
		"function_item",
		"impl_item",
		"struct_item",
		"enum_item",
	},
	LangJava: {
		"method_declaration",
		"class_declaration",
		"constructor_declaration",
	},
	LangC: {
		"function_definition",
		"struct_specifier",
	},
	LangCpp: {
		"function_definition",
		"class_specifier",
		"struct_specifier",
	},
	LangRuby: {
		"method",
		"class",
		"module",
	},
}

// FindBlockSpan locates the syntactic block that contains or starts at
// the given 1-based line number. It returns the inclusive line range of
// the smallest matching block, or an error if no block is found.
func (pm *ParserManager) FindBlockSpan(ctx context.Context, filePath string, lineNum int) (*BlockSpan, error) {
	lang := DetectLanguage(filePath)
	if lang == LangUnknown {
		return nil, fmt.Errorf("could not detect language for: %s", filePath)
	}

	result, err := pm.ParseFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	targetLine := lineNum - 1 // convert to 0-based for comparison

	// Walk our converted AST to find the smallest block containing the line
	var found *BlockSpan
	var walk func(Node)
	walk = func(node Node) {
		if found != nil {
			return // already found a (deeper) match
		}

		isBlock := false
		for _, bt := range blockNodeTypes[lang] {
			if node.Type == bt {
				isBlock = true
				break
			}
		}

		if isBlock {
			start := node.Range.StartLine // 0-based
			end := node.Range.EndLine     // 0-based, inclusive in our Range type

			if start <= targetLine && end >= targetLine {
				// Found a block containing the target line.
				// Continue into children to find a more specific (smaller) block.
				for _, child := range node.Children {
					walk(child)
					if found != nil {
						return
					}
				}
				// No child match — this is the smallest block.
				found = &BlockSpan{
					StartLine: start + 1, // 1-based
					EndLine:   end + 1,   // 1-based
					NodeType:  node.Type,
				}
				return
			}
		}

		// Not a matching block, recurse into children
		for _, child := range node.Children {
			walk(child)
			if found != nil {
				return
			}
		}
	}

	walk(result.RootNode)

	if found == nil {
		// As a fallback, try to find a block that *starts* near the target line
		// (the user may have provided a line inside the signature)
		var nearest *BlockSpan
		var nearestDist int
		var walkNearest func(Node)
		walkNearest = func(node Node) {
			isBlock := false
			for _, bt := range blockNodeTypes[lang] {
				if node.Type == bt {
					isBlock = true
					break
				}
			}
			if isBlock {
				start := node.Range.StartLine
				end := node.Range.EndLine
				dist := abs(start - targetLine)
				if nearest == nil || dist < nearestDist {
					nearest = &BlockSpan{
						StartLine: start + 1,
						EndLine:   end + 1,
						NodeType:  node.Type,
					}
					nearestDist = dist
				}
			}
			for _, child := range node.Children {
				walkNearest(child)
			}
		}
		walkNearest(result.RootNode)

		if nearest != nil && nearestDist <= 5 {
			found = nearest
		}
	}

	if found == nil {
		return nil, fmt.Errorf("no syntactic block found at line %d in %s", lineNum, filePath)
	}

	return found, nil
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
