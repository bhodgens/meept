package builtin

// office_docx builtin tool (contract C11.2, .hermes/contracts/06-office-skills.md;
// leaf 06-office-skills/01-docx-skill).
//
// Backed by github.com/gomutex/godocx v0.1.5 (MIT — the research-gated pick in
// docs/plans/20261102-dsh-convergence/06-office-skills/library-picks.md; API
// signatures verified against the module source). The library is the ONLY
// research-dependent seam for create/edit; the JSON contract is library-
// agnostic.
//
// Note: godocx's docx.Table complex type is unexported with no text accessor,
// so verification extracts w:t text directly from word/document.xml in the
// saved zip — covers paragraphs AND table cells, and proves the bytes on disk
// (not the in-memory model).
//
// Actions (C11.2): create | read | edit | validate over .docx files inside
// the session working directory. Edits APPEND blocks and never rebuild the
// document. Saves to a new file unless in_place is requested. Every write
// reopens the saved file and verifies the requested text is present.

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	godocx "github.com/gomutex/godocx"
	"github.com/gomutex/godocx/docx"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// OfficeDocxTool creates / reads / edits / validates Word .docx documents.
type OfficeDocxTool struct {
	tools.ToolDefaults
}

// NewOfficeDocxTool constructs the tool (no deps; the fence rides the context).
func NewOfficeDocxTool() *OfficeDocxTool { return &OfficeDocxTool{} }

func (t *OfficeDocxTool) Name() string     { return "office_docx" }
func (t *OfficeDocxTool) Category() string { return "filesystem" }

func (t *OfficeDocxTool) Description() string {
	return "Create, read, edit, or validate Word .docx documents with headings, paragraphs, and tables. Paths stay inside the session working directory. Edits append blocks and preserve existing content; saves go to a new file unless in_place is set."
}

// IsReadOnly: read and validate never mutate (C11.2).
func (t *OfficeDocxTool) IsReadOnly(input map[string]any) bool {
	action, _ := input["action"].(string)
	return action == "read" || action == "validate"
}

func (t *OfficeDocxTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        schemaTypeString,
				Description: "Document path (.docx). Resolved against the session working directory; escapes are refused.",
			},
			"action": {
				Type:        schemaTypeString,
				Description: `One of "create" (new document from blocks), "read" (extract text), "edit" (append blocks to an existing document), "validate" (reopen and verify expected text is present).`,
			},
			"blocks": {
				Type:        schemaTypeArray,
				Description: `Document blocks for create/edit: {"type":"heading","text":str,"level":"1".."6"} | {"type":"paragraph","text":str,"style":str(opt)} | {"type":"table","rows":[[str]]}.`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeObject,
					Description: "One block: heading, paragraph, or table.",
				},
			},
			"in_place": {
				Type:        schemaTypeBoolean,
				Description: "create/edit only: overwrite the file at \"path\" instead of writing \"<name>.edited.docx\" next to it (default false).",
			},
			"expect_text": {
				Type:        schemaTypeArray,
				Description: "validate only: strings that must appear in the reopened document's text; all must be present for valid=true.",
				Items: &llm.ParameterProperty{
					Type:        schemaTypeString,
					Description: "One expected text fragment.",
				},
			},
		},
		Required: []string{"path", "action"},
	}
}

// Execute implements tools.Tool (C11.2 contract).
func (t *OfficeDocxTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("office_docx: no path specified")
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))

	resolved, err := t.resolveFenced(ctx, path)
	if err != nil {
		return nil, err
	}

	switch action {
	case "create":
		return t.doCreate(resolved, args)
	case "read":
		return t.doRead(resolved)
	case "edit":
		return t.doEdit(resolved, args)
	case "validate":
		return t.doValidate(resolved, args)
	default:
		return nil, fmt.Errorf("office_docx: unknown action %q (want create, read, edit, or validate)", action)
	}
}

// resolveFenced applies the spreadsheet_write fence verbatim (C11.2:
// refuse out-of-session paths; no process-cwd or temp-dir fallback).
func (t *OfficeDocxTool) resolveFenced(ctx context.Context, path string) (string, error) {
	sessionDir := tools.WorkingDirFromContext(ctx)
	if sessionDir == "" {
		return "", fmt.Errorf("office_docx: no session working directory in context: cannot resolve path %q safely", path)
	}
	absSession, err := filepath.Abs(sessionDir)
	if err != nil {
		return "", fmt.Errorf("office_docx: cannot resolve session working directory: %w", err)
	}
	resolved, err := resolvePath(ctx, path)
	if err != nil {
		return "", fmt.Errorf("office_docx: invalid path %q: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	if resolved != absSession && !strings.HasPrefix(resolved, absSession+string(filepath.Separator)) {
		return "", fmt.Errorf("office_docx: path %q resolves to %s, which is outside the session working directory %s: refusing", path, resolved, absSession)
	}
	return resolved, nil
}

// docxBlock is one parsed content block.
type docxBlock struct {
	Kind  string // heading | paragraph | table
	Text  string
	Level uint
	Style string
	Rows  [][]string
}

func parseDocxBlocks(raw []any) ([]docxBlock, error) {
	blocks := make([]docxBlock, 0, len(raw))
	for i, b := range raw {
		obj, ok := b.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("office_docx: blocks[%d] is %T, want an object with \"type\"", i, b)
		}
		kind, _ := obj["type"].(string)
		blk := docxBlock{Kind: strings.ToLower(strings.TrimSpace(kind))}
		switch blk.Kind {
		case "heading":
			blk.Text, _ = obj["text"].(string)
			if blk.Text == "" {
				return nil, fmt.Errorf("office_docx: blocks[%d]: heading requires non-empty \"text\"", i)
			}
			var lvl uint = 1
			switch lv := obj["level"].(type) {
			case string:
				if lv != "" {
					if _, err := fmt.Sscanf(lv, "%d", &lvl); err != nil {
						return nil, fmt.Errorf("office_docx: blocks[%d]: heading level %q is not a number 0-6", i, lv)
					}
				}
			case float64:
				lvl = uint(lv)
			}
			if lvl > 6 {
				return nil, fmt.Errorf("office_docx: blocks[%d]: heading level %d out of range 0-6", i, lvl)
			}
			blk.Level = lvl
		case "paragraph":
			blk.Text, _ = obj["text"].(string)
			blk.Style, _ = obj["style"].(string)
		case "table":
			rawRows, ok := obj["rows"].([]any)
			if !ok || len(rawRows) == 0 {
				return nil, fmt.Errorf("office_docx: blocks[%d]: table requires a non-empty \"rows\" array of string arrays", i)
			}
			for r, rr := range rawRows {
				cells, ok := rr.([]any)
				if !ok {
					return nil, fmt.Errorf("office_docx: blocks[%d].rows[%d] is %T, want an array of strings", i, r, rr)
				}
				row := make([]string, 0, len(cells))
				for c, cell := range cells {
					s, ok := cell.(string)
					if !ok {
						return nil, fmt.Errorf("office_docx: blocks[%d].rows[%d][%d] is %T, want a string", i, r, c, cell)
					}
					row = append(row, s)
				}
				blk.Rows = append(blk.Rows, row)
			}
		default:
			return nil, fmt.Errorf("office_docx: blocks[%d]: unknown type %q (want heading, paragraph, or table)", i, kind)
		}
		blocks = append(blocks, blk)
	}
	return blocks, nil
}

// applyDocxBlocks appends blocks to an open document (never rebuilds it).
func applyDocxBlocks(rd *docx.RootDoc, blocks []docxBlock) error {
	for i, blk := range blocks {
		switch blk.Kind {
		case "heading":
			if _, err := rd.AddHeading(blk.Text, blk.Level); err != nil {
				return fmt.Errorf("office_docx: blocks[%d]: add heading: %w", i, err)
			}
		case "paragraph":
			p := rd.AddParagraph(blk.Text)
			if blk.Style != "" {
				p.Style(blk.Style)
			}
		case "table":
			tbl := rd.AddTable()
			tbl.Style("TableGrid")
			for _, row := range blk.Rows {
				tr := tbl.AddRow()
				for _, cell := range row {
					tr.AddCell().AddParagraph(cell)
				}
			}
		}
	}
	return nil
}

// docxOutputPath picks the save target. in_place always writes to path.
// create defaults to path itself (the given path IS the new file); edit
// defaults to a NEW file next to it so the original survives (C11.2).
func docxOutputPath(path string, args map[string]any, action string) string {
	if inPlace, _ := args["in_place"].(bool); inPlace || action == "create" {
		return path
	}
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + ".edited" + ext
}

// docxText extracts every w:t text node from word/document.xml inside the
// docx zip. Runs on the SAVED bytes: covers paragraphs and table cells alike.
func docxText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("office_docx: reopen %s: %w", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("office_docx: read document.xml: %w", err)
		}
		defer rc.Close()
		var sb strings.Builder
		dec := xml.NewDecoder(rc)
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if !ok || se.Name.Local != "t" {
				continue
			}
			var txt string
			if err := dec.DecodeElement(&txt, &se); err == nil {
				sb.WriteString(txt)
				sb.WriteString("\n")
			}
		}
		return sb.String(), nil
	}
	return "", fmt.Errorf("office_docx: %s has no word/document.xml", path)
}

// doCreate builds a new document from blocks, saves, reopens, verifies.
func (t *OfficeDocxTool) doCreate(path string, args map[string]any) (any, error) {
	raw, _ := args["blocks"].([]any)
	blocks, err := parseDocxBlocks(raw)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("office_docx: create requires at least one block")
	}
	out := docxOutputPath(path, args, "create")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, fmt.Errorf("office_docx: cannot create parent directories: %w", err)
	}
	rd, err := godocx.NewDocument()
	if err != nil {
		return nil, fmt.Errorf("office_docx: new document: %w", err)
	}
	defer rd.Close()
	if err := applyDocxBlocks(rd, blocks); err != nil {
		return nil, err
	}
	if err := rd.SaveTo(out); err != nil {
		return nil, fmt.Errorf("office_docx: save %s: %w", out, err)
	}
	return t.verifyAndResult(out, blocks, "created")
}

// doEdit opens an existing document, APPENDS blocks, saves, verifies.
func (t *OfficeDocxTool) doEdit(path string, args map[string]any) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("office_docx: edit requires an existing document at %s: %w", path, err)
	}
	raw, _ := args["blocks"].([]any)
	blocks, err := parseDocxBlocks(raw)
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("office_docx: edit requires at least one block")
	}
	out := docxOutputPath(path, args, "edit")
	rd, err := godocx.OpenDocument(path)
	if err != nil {
		return nil, fmt.Errorf("office_docx: open %s: %w", path, err)
	}
	defer rd.Close()
	if err := applyDocxBlocks(rd, blocks); err != nil {
		return nil, err
	}
	if err := rd.SaveTo(out); err != nil {
		return nil, fmt.Errorf("office_docx: save %s: %w", out, err)
	}
	return t.verifyAndResult(out, blocks, "edited")
}

// verifyAndResult reopens the saved file and checks every block's text
// survived (reopen-and-verify convention).
func (t *OfficeDocxTool) verifyAndResult(out string, blocks []docxBlock, action string) (any, error) {
	text, err := docxText(out)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, blk := range blocks {
		if blk.Kind == "table" {
			for _, row := range blk.Rows {
				for _, cell := range row {
					if cell != "" && !strings.Contains(text, cell) {
						missing = append(missing, cell)
					}
				}
			}
			continue
		}
		if blk.Text != "" && !strings.Contains(text, blk.Text) {
			missing = append(missing, blk.Text)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("office_docx: saved %s but reopened verification found missing content: %v", out, missing)
	}
	info, _ := os.Stat(out)
	var size int64
	if info != nil {
		size = info.Size()
	}
	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":     out,
			"action":   action,
			"blocks":   len(blocks),
			"bytes":    size,
			"verified": true,
		},
		Evidence: []models.Evidence{
			models.NewEvidence(
				models.EvidenceFileExists,
				out,
				fmt.Sprintf("action=%s,blocks=%d", action, len(blocks)),
				"office_docx",
			),
		},
	}, nil
}

// doRead extracts the document text.
func (t *OfficeDocxTool) doRead(path string) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("office_docx: read: %w", err)
	}
	text, err := docxText(path)
	if err != nil {
		return nil, err
	}
	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":     path,
			"action":   "read",
			"text":     strings.TrimSpace(text),
			"verified": true,
		},
	}, nil
}

// doValidate reopens and checks every expect_text fragment is present.
func (t *OfficeDocxTool) doValidate(path string, args map[string]any) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("office_docx: validate: %w", err)
	}
	text, err := docxText(path)
	if err != nil {
		return nil, err
	}
	raw, _ := args["expect_text"].([]any)
	var missing []string
	for _, e := range raw {
		s, ok := e.(string)
		if !ok || s == "" {
			continue
		}
		if !strings.Contains(text, s) {
			missing = append(missing, s)
		}
	}
	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":     path,
			"action":   "validate",
			"valid":    len(missing) == 0,
			"missing":  missing,
			"verified": true,
		},
	}, nil
}
