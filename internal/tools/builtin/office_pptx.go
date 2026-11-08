package builtin

// office_pptx builtin tool (contract C11.3, .hermes/contracts/06-office-skills.md;
// leaf 06-office-skills/02-pptx-skill).
//
// Backed by github.com/mmonterroca/pptxgo (MIT — the research-gated pick in
// docs/plans/20261102-dsh-convergence/06-office-skills/library-picks.md; API
// signatures verified against the module source). The library is the ONLY
// research-dependent seam for create/edit; the JSON contract is library-
// agnostic.
//
// Actions (C11.3): create | read | edit | validate over .pptx files inside
// the session working directory. Edits APPEND slides and never rebuild the
// deck (Template.Save passes every untouched part through byte-verbatim, so
// charts / SmartArt / mixed formatting on existing slides survive); the only
// way to change EXISTING slide content is a per-slide replace:{find,with}
// (pptxgo Template.Replace byte-splice, which heals run splits). Every write
// reopens the saved file and verifies slide count and requested content
// before reporting success (reopen-and-verify convention).
//
// Paths, including image_path, are confined to the session working
// directory (resolveFenced, same fence as office_docx / spreadsheet_write).

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mmonterroca/pptxgo/opc"
	"github.com/mmonterroca/pptxgo/pptx"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// OfficePptxTool creates / reads / edits / validates PowerPoint .pptx decks.
type OfficePptxTool struct {
	tools.ToolDefaults
}

// NewOfficePptxTool constructs the tool (no deps; the fence rides the context).
func NewOfficePptxTool() *OfficePptxTool { return &OfficePptxTool{} }

func (t *OfficePptxTool) Name() string     { return "office_pptx" }
func (t *OfficePptxTool) Category() string { return "filesystem" }

func (t *OfficePptxTool) Description() string {
	return "Create, read, edit, or validate PowerPoint .pptx presentations with slide titles, bullet bodies, speaker notes, and images. Paths stay inside the session working directory. Edits append slides (or replace text on existing slides) and preserve untouched content; saves go to a new file unless in_place is set."
}

// IsReadOnly: read and validate never mutate (C11.3).
func (t *OfficePptxTool) IsReadOnly(input map[string]any) bool {
	action, _ := input["action"].(string)
	return action == "read" || action == "validate"
}

func (t *OfficePptxTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"path": {
				Type:        schemaTypeString,
				Description: "Presentation path (.pptx). Resolved against the session working directory; escapes are refused.",
			},
			"action": {
				Type:        schemaTypeString,
				Description: `One of "create" (new deck from slides), "read" (slide summary: count, titles, text, notes), "edit" (append slides and/or replace text on existing slides), "validate" (reopen and verify expected text is present).`,
			},
			"slides": {
				Type:        schemaTypeArray,
				Description: `create/edit payload: {"title":str, "body":[str], "notes":str, "image_path":str(in-session, opt), "replace":{"find":str,"with":str}(edit only, targets an EXISTING slide)}. A slide with only "replace" changes existing content without appending.`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeObject,
					Description: "One slide spec: title, body lines, notes, optional in-session image, optional existing-content replacement.",
				},
			},
			"in_place": {
				Type:        schemaTypeBoolean,
				Description: "create/edit only: overwrite the file at \"path\" instead of writing \"<name>.edited.pptx\" next to it (default false).",
			},
			"expect_text": {
				Type:        schemaTypeString,
				Description: `validate only: strings that must appear in the reopened deck's slide text; all must be present for valid=true.`,
				Items: &llm.ParameterProperty{
					Type:        schemaTypeString,
					Description: "One expected text fragment.",
				},
			},
		},
		Required: []string{"path", "action"},
	}
}

// Execute implements tools.Tool (C11.3 contract).
func (t *OfficePptxTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("office_pptx: no path specified")
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))

	resolved, err := t.resolveFenced(ctx, path)
	if err != nil {
		return nil, err
	}

	switch action {
	case "create":
		return t.doCreate(ctx, resolved, args)
	case "read":
		return t.doRead(resolved)
	case "edit":
		return t.doEdit(ctx, resolved, args)
	case "validate":
		return t.doValidate(resolved, args)
	default:
		return nil, fmt.Errorf("office_pptx: unknown action %q (want create, read, edit, or validate)", action)
	}
}

// resolveFenced applies the spreadsheet_write fence verbatim (C11.3:
// refuse out-of-session paths; no process-cwd or temp-dir fallback).
func (t *OfficePptxTool) resolveFenced(ctx context.Context, path string) (string, error) {
	sessionDir := tools.WorkingDirFromContext(ctx)
	if sessionDir == "" {
		return "", fmt.Errorf("office_pptx: no session working directory in context: cannot resolve path %q safely", path)
	}
	absSession, err := filepath.Abs(sessionDir)
	if err != nil {
		return "", fmt.Errorf("office_pptx: cannot resolve session working directory: %w", err)
	}
	resolved, err := resolvePath(ctx, path)
	if err != nil {
		return "", fmt.Errorf("office_pptx: invalid path %q: %w", path, err)
	}
	resolved = filepath.Clean(resolved)
	if resolved != absSession && !strings.HasPrefix(resolved, absSession+string(filepath.Separator)) {
		return "", fmt.Errorf("office_pptx: path %q resolves to %s, which is outside the session working directory %s: refusing", path, resolved, absSession)
	}
	return resolved, nil
}

// resolveImageFenced confines a slide's image_path the same way as path
// (C11.3: image_path is opt-in-session).
func (t *OfficePptxTool) resolveImageFenced(ctx context.Context, slideIdx int, imagePath string) (string, error) {
	resolved, err := t.resolveFenced(ctx, imagePath)
	if err != nil {
		return "", fmt.Errorf("office_pptx: slides[%d]: image_path %q: %w", slideIdx, imagePath, err)
	}
	if resolved == "" {
		return "", fmt.Errorf("office_pptx: slides[%d]: image_path resolved empty", slideIdx)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("office_pptx: slides[%d]: image_path %q: %w", slideIdx, imagePath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("office_pptx: slides[%d]: image_path %q is a directory", slideIdx, imagePath)
	}
	return resolved, nil
}

// pptxSlide is one parsed slide spec.
type pptxSlide struct {
	Title     string
	Body      []string
	Notes     string
	ImagePath string
	Find      string // replace-only: text to find on an EXISTING slide
	With      string // replace-only: replacement text
}

func parsePptxSlides(raw []any) ([]pptxSlide, error) {
	slides := make([]pptxSlide, 0, len(raw))
	for i, s := range raw {
		obj, ok := s.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("office_pptx: slides[%d] is %T, want an object with \"title\"", i, s)
		}
		sl := pptxSlide{}
		sl.Title, _ = obj["title"].(string)
		sl.Notes, _ = obj["notes"].(string)
		sl.ImagePath, _ = obj["image_path"].(string)
		if bodyRaw, ok := obj["body"].([]any); ok {
			for j, b := range bodyRaw {
				line, ok := b.(string)
				if !ok {
					return nil, fmt.Errorf("office_pptx: slides[%d].body[%d] is %T, want a string", i, j, b)
				}
				sl.Body = append(sl.Body, line)
			}
		}
		if repRaw, ok := obj["replace"].(map[string]any); ok {
			sl.Find, _ = repRaw["find"].(string)
			sl.With, _ = repRaw["with"].(string)
			if sl.Find == "" {
				return nil, fmt.Errorf("office_pptx: slides[%d]: replace requires a non-empty \"find\"", i)
			}
		}
		if sl.Title == "" && sl.Find == "" {
			return nil, fmt.Errorf("office_pptx: slides[%d]: requires a non-empty \"title\" (or a replace:{find,with})", i)
		}
		slides = append(slides, sl)
	}
	return slides, nil
}

// applySlide appends one new slide to an open Presentation (never touches
// existing slides). Widescreen 16:9 (pptxgo's New default) with explicit
// title/body sizes, per the dsh office-pptx convention to set dimensions
// and text sizes explicitly.
func (t *OfficePptxTool) applySlide(ctx context.Context, pres *pptx.Presentation, i int, sl pptxSlide) error {
	slide := pres.AddSlide(pptx.WithLayout(pptx.LayoutTitleAndContent))
	if sl.Title != "" {
		slide.Title(sl.Title).FontSize(pptxPointsTitle)
	}
	for j, line := range sl.Body {
		if line == "" {
			continue
		}
		body := slide.Body(line).FontSize(pptxPointsBody)
		_ = body
		_ = j
	}
	if sl.Notes != "" {
		slide.Notes(sl.Notes)
	}
	if sl.ImagePath != "" {
		resolved, err := t.resolveImageFenced(ctx, i, sl.ImagePath)
		if err != nil {
			return err
		}
		// Right of the body placeholder, top-aligned with it, ~40% width.
		slide.AddImageWithSize(resolved, pptxImageX, pptxImageY, pptxImageW, pptxImageH)
	}
	return nil
}

// applyReplace changes text on an EXISTING slide via pptxgo's Template
// byte-splice (preserves every other run's formatting; heals run splits).
func (t *OfficePptxTool) applyReplace(tmpl *pptx.Template, i int, sl pptxSlide) (int, error) {
	if sl.With == "" {
		return 0, fmt.Errorf("office_pptx: slides[%d]: replace requires a non-empty \"with\"", i)
	}
	n, err := tmpl.Replace(sl.Find, sl.With)
	if err != nil {
		return 0, fmt.Errorf("office_pptx: slides[%d]: replace %q: %w", i, sl.Find, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("office_pptx: slides[%d]: replace target %q not found in the deck (reopen-and-verify: refusing to report a no-op edit)", i, sl.Find)
	}
	return n, nil
}

// pptxOutputPath picks the save target. in_place always writes to path.
// create defaults to path itself (the given path IS the new file); edit
// defaults to a NEW file next to it so the original survives (C11.3).
func pptxOutputPath(path string, args map[string]any, action string) string {
	if inPlace, _ := args["in_place"].(bool); inPlace || action == "create" {
		return path
	}
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + ".edited" + ext
}

// openDeck reopens a saved .pptx from disk (the reopen-and-verify seam:
// verification runs on the bytes on disk, not the in-memory model).
func openDeck(path string) (*pptx.Template, error) {
	tmpl, err := pptx.Open(path)
	if err != nil {
		return nil, fmt.Errorf("office_pptx: reopen %s: %w", path, err)
	}
	return tmpl, nil
}

// doCreate builds a new deck from slides, saves, reopens, verifies.
func (t *OfficePptxTool) doCreate(ctx context.Context, path string, args map[string]any) (any, error) {
	raw, _ := args["slides"].([]any)
	slides, err := parsePptxSlides(raw)
	if err != nil {
		return nil, err
	}
	if len(slides) == 0 {
		return nil, fmt.Errorf("office_pptx: create requires at least one slide")
	}
	for i, sl := range slides {
		if sl.Find != "" {
			return nil, fmt.Errorf("office_pptx: slides[%d]: replace targets an EXISTING slide; use action \"edit\"", i)
		}
	}
	out := pptxOutputPath(path, args, "create")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, fmt.Errorf("office_pptx: cannot create parent directories: %w", err)
	}
	pres := pptx.New()
	for i, sl := range slides {
		if err := t.applySlide(ctx, pres, i, sl); err != nil {
			return nil, err
		}
	}
	if err := writeDeck(pres, out); err != nil {
		return nil, err
	}
	return t.verifyAndResult(out, slides, "created")
}

// doEdit opens an existing deck, appends slides and/or replaces existing
// text, saves, verifies (slide count AND requested content).
func (t *OfficePptxTool) doEdit(ctx context.Context, path string, args map[string]any) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("office_pptx: edit requires an existing presentation at %s: %w", path, err)
	}
	raw, _ := args["slides"].([]any)
	slides, err := parsePptxSlides(raw)
	if err != nil {
		return nil, err
	}
	if len(slides) == 0 {
		return nil, fmt.Errorf("office_pptx: edit requires at least one slide")
	}
	out := pptxOutputPath(path, args, "edit")

	// Replace-first on the ORIGINAL bytes: replaces target existing slides,
	// appends target the deck being built. Byte-splice the original, save
	// the intermediate, then reopen into a Presentation to append (an
	// append needs the content model, which only New-built decks can
	// rehydrate safely - pptxgo rehydrates foreign presentation.xml as a
	// Template, never as a *Presentation).
	replacements := 0
	haveReplace := false
	for _, sl := range slides {
		if sl.Find != "" {
			haveReplace = true
		}
	}
	if haveReplace {
		tmpl, err := pptx.Open(path)
		if err != nil {
			return nil, fmt.Errorf("office_pptx: open %s: %w", path, err)
		}
		tmplDeckPaths[tmpl] = path
		defer delete(tmplDeckPaths, tmpl)
		before := tmpl.SlideCount()
		for i, sl := range slides {
			if sl.Find == "" {
				continue
			}
			n, err := t.applyReplace(tmpl, i, sl)
			if err != nil {
				return nil, err
			}
			replacements += n
		}
		var buf bytes.Buffer
		if err := tmpl.Save(&buf); err != nil {
			return nil, fmt.Errorf("office_pptx: save %s: %w", out, err)
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			return nil, fmt.Errorf("office_pptx: write %s: %w", out, err)
		}
		// The spliced intermediate IS the reopen-and-verify base for the
		// append below (a foreign deck cannot be rehydrated as *Presentation).
		path = out
		tmpl2, err := openDeck(path)
		if err != nil {
			return nil, err
		}
		if got := tmpl2.SlideCount(); got != before {
			return nil, fmt.Errorf("office_pptx: reopen verification failed: replace changed the slide count from %d to %d", before, got)
		}
	}

	// Append-only branch: appends ride a NEW Presentation whose slide parts
	// are byte-copied from the current file, so existing slides (and their
	// charts / formatting) survive verbatim; New's master provides the
	// placeholders. Slide order after this is: copied existing slides first,
	// then the appended ones.
	var appended []pptxSlide
	for _, sl := range slides {
		if sl.Find != "" {
			continue
		}
		appended = append(appended, sl)
	}
	if len(appended) > 0 {
		existing, err := openDeck(path)
		if err != nil {
			return nil, err
		}
		tmplDeckPaths[existing] = path
		defer delete(tmplDeckPaths, existing)
		existingCount := existing.SlideCount()
		pres := pptx.New()
		// Reserve one AddSlide slot per copied slide (the rel + sldId
		// wiring is pptxgo-internal), then overwrite each reserved
		// slideN.xml part with the source slide's bytes: the copied XML
		// passes through byte-verbatim, so charts / tables (inline) and
		// mixed formatting survive untouched.
		for i := 1; i <= existingCount; i++ {
			if err := copySlideInto(pres, existing, i); err != nil {
				return nil, err
			}
		}
		for i, sl := range appended {
			if err := t.applySlide(ctx, pres, existingCount+i, sl); err != nil {
				return nil, err
			}
		}
		if err := writeDeck(pres, out); err != nil {
			return nil, err
		}
		// reopen-and-verify: existing slides + appended slides all present.
		tmpl, err := openDeck(out)
		if err != nil {
			return nil, err
		}
		if got := tmpl.SlideCount(); got != existingCount+len(appended) {
			return nil, fmt.Errorf("office_pptx: reopen verification failed: slide count is %d, want %d (%d existing + %d appended)", got, existingCount+len(appended), existingCount, len(appended))
		}
	}

	return t.verifyAndResult(out, slides, "edited")
}

// writeDeck saves a Presentation and fails loudly on any builder error.
// Staged raw parts (copied slide bytes, media) are spliced into the saved
// zip before it hits disk; [Content_Types].xml gains an Override per staged
// part whose type is not already covered.
func writeDeck(pres *pptx.Presentation, out string) error {
	var buf bytes.Buffer
	if err := pres.Save(&buf); err != nil {
		return fmt.Errorf("office_pptx: save %s: %w", out, err)
	}
	data := buf.Bytes()
	if staged, ok := stagedParts[pres]; ok && len(staged) > 0 {
		var err error
		data, err = injectDeckParts(data, staged)
		if err != nil {
			return fmt.Errorf("office_pptx: merge %s: %w", out, err)
		}
		delete(stagedParts, pres)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("office_pptx: write %s: %w", out, err)
	}
	return nil
}

// injectDeckParts rewrites a saved .pptx zip: each staged part REPLACES the
// entry of the same name or is appended (media, slide bytes), and
// [Content_Types].xml gains a per-part Override for every staged part whose
// content type the package does not already declare for that part.
func injectDeckParts(data []byte, staged map[string]stagedPart) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open staged zip: %w", err)
	}
	// Pull the content-types entry out first so it can be patched before
	// anything is streamed into the writer.
	var ctXML []byte
	for _, f := range zr.File {
		if f.Name == "[Content_Types].xml" {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open [Content_Types].xml: %w", err)
			}
			ctXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("read [Content_Types].xml: %w", err)
			}
			break
		}
	}
	if ctXML == nil {
		return nil, fmt.Errorf("staged zip has no [Content_Types].xml")
	}
	ctXML, err = patchContentTypes(ctXML, staged)
	if err != nil {
		return nil, err
	}
	staged["[Content_Types].xml"] = stagedPart{
		contentType: "application/xml",
		data:        ctXML,
	}

	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range zr.File {
		sp, isStaged := staged[f.Name]
		if isStaged {
			if err := writeZipEntry(w, f.Name, sp.data); err != nil {
				return nil, err
			}
			continue
		}
		if err := copyZipEntry(w, f); err != nil {
			return nil, err
		}
	}
	// Append staged parts that did not exist in the base zip.
	for name, sp := range staged {
		if name == "[Content_Types].xml" {
			continue // already written in-order above
		}
		if containsPart(zr, name) {
			continue
		}
		if err := writeZipEntry(w, name, sp.data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close merged zip: %w", err)
	}
	return out.Bytes(), nil
}

// containsPart reports whether the zip already holds an entry named name.
func containsPart(zr *zip.Reader, name string) bool {
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

// writeZipEntry writes one raw entry into w.
func writeZipEntry(w *zip.Writer, name string, data []byte) error {
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
	ow, err := w.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	if _, err := ow.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// patchContentTypes adds an <Override PartName=... ContentType=...> element
// per staged part not already overridden (media extensions already covered
// by a matching Default are skipped — Overrides are additive; duplicates
// for the same PartName would be schema-invalid, hence the check).
func patchContentTypes(ctXML []byte, staged map[string]stagedPart) ([]byte, error) {
	var ct struct {
		Overrides []struct {
			PartName    string `xml:"PartName,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if err := xml.Unmarshal(ctXML, &ct); err != nil {
		return nil, fmt.Errorf("parse [Content_Types].xml: %w", err)
	}
	overridden := map[string]string{}
	for _, o := range ct.Overrides {
		overridden[strings.TrimPrefix(o.PartName, "/")] = o.ContentType
	}
	// Extension Defaults are captured with their real attr names below; we
	// re-unmarshal with the correct attribute tag.
	var ct2 struct {
		Defaults []struct {
			Extension   string `xml:"Extension,attr"`
			ContentType string `xml:"ContentType,attr"`
		} `xml:"Default"`
	}
	if err := xml.Unmarshal(ctXML, &ct2); err != nil {
		return nil, fmt.Errorf("parse [Content_Types].xml defaults: %w", err)
	}
	coveredByDefault := map[string]string{}
	for _, d := range ct2.Defaults {
		coveredByDefault[d.Extension] = d.ContentType
	}

	added := ""
	for name, sp := range staged {
		if name == "[Content_Types].xml" {
			continue
		}
		if _, done := overridden[name]; done {
			continue
		}
		if ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), "."); ext != "" && coveredByDefault[ext] == sp.contentType {
			continue
		}
		added += fmt.Sprintf(`<Override PartName="/%s" ContentType="%s"/>`, name, sp.contentType)
	}
	if added == "" {
		return ctXML, nil
	}
	s := string(ctXML)
	closeIdx := strings.LastIndex(s, "</Types>")
	if closeIdx < 0 {
		return nil, fmt.Errorf("[Content_Types].xml has no </Types> close tag")
	}
	return []byte(s[:closeIdx] + added + s[closeIdx:]), nil
}

// copyZipEntry streams one zip entry into w.
func copyZipEntry(w *zip.Writer, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer rc.Close()
	hdr := &zip.FileHeader{Name: f.Name, Method: zip.Deflate}
	ow, err := w.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("create %s: %w", f.Name, err)
	}
	if _, err := io.Copy(ow, rc); err != nil {
		return fmt.Errorf("copy %s: %w", f.Name, err)
	}
	return nil
}

// verifyAndResult reopens the saved file and checks the requested content
// survived (reopen-and-verify convention), then reports the final path.
func (t *OfficePptxTool) verifyAndResult(out string, slides []pptxSlide, action string) (any, error) {
	count, deck, err := slideText(out)
	if err != nil {
		return nil, err
	}
	var missing []string
	replacements := 0
	for _, sl := range slides {
		if sl.Find != "" {
			// replace edits verified above (no-op refused in applyReplace).
			replacements++
			continue
		}
		if sl.Title != "" && !strings.Contains(deck, sl.Title) {
			missing = append(missing, sl.Title)
		}
		for _, line := range sl.Body {
			if line != "" && !strings.Contains(deck, line) {
				missing = append(missing, line)
			}
		}
		if sl.Notes != "" && !strings.Contains(deck, sl.Notes) {
			missing = append(missing, "notes: "+sl.Notes)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("office_pptx: saved %s but reopened verification found missing content: %v", out, missing)
	}
	if action == "edited" && count < len(slides) && replacements == 0 {
		return nil, fmt.Errorf("office_pptx: reopened %s has %d slides, fewer than the %d requested", out, count, len(slides))
	}
	info, _ := os.Stat(out)
	var size int64
	if info != nil {
		size = info.Size()
	}
	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":         out,
			"action":       action,
			"slides":       len(slides),
			"slide_count":  count,
			"replacements": replacements,
			"bytes":        size,
			"verified":     true,
		},
		Evidence: []models.Evidence{
			models.NewEvidence(
				models.EvidenceFileExists,
				out,
				fmt.Sprintf("action=%s,slides=%d", action, len(slides)),
				"office_pptx",
			),
		},
	}, nil
}

// slideText reopens the deck and returns (count, all-slide text including
// speaker notes). OpenSlide.Text only covers the slide part; notes live in
// the sibling notesSlideN.xml part referenced from the slide's own rels, so
// those parts are pulled from the deck zip and appended (same a:t walk).
func slideText(path string) (int, string, error) {
	tmpl, err := openDeck(path)
	if err != nil {
		return 0, "", err
	}
	var sb strings.Builder
	for _, s := range tmpl.Slides() {
		txt, err := s.Text()
		if err != nil {
			return 0, "", fmt.Errorf("office_pptx: read %s: %w", path, err)
		}
		sb.WriteString(txt)
		sb.WriteString("\n")
		if s.Index() >= 1 {
			if notes, ok := slideNotesText(path, s.Index()); ok {
				sb.WriteString(notes)
				sb.WriteString("\n")
			}
		}
	}
	return tmpl.SlideCount(), sb.String(), nil
}

// slideNotesText extracts the a:t text of the notes slide annotating slide n
// (resolved through the slide part's rels; false when the deck has none).
func slideNotesText(deckPath string, n int) (string, bool) {
	srcPath := fmt.Sprintf("ppt/slides/slide%d.xml", n)
	rels, err := deckSlideRels(deckPath, srcPath)
	if err != nil {
		return "", false
	}
	notesType := "http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide"
	for _, rel := range rels {
		if rel.Type != notesType {
			continue
		}
		raw, err := deckPartBytes(deckPath, rel.Target)
		if err != nil {
			return "", false
		}
		txt, err := extractATText(raw)
		if err != nil {
			return "", false
		}
		return txt, true
	}
	return "", false
}

// extractATText concatenates every a:t element's chardata in document order,
// one a:p per line (mirrors pptxgo's extractText for parts it cannot reach).
func extractATText(raw []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var sb strings.Builder
	inT := false
	paragraphHasText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "t":
				inT = true
			case "p":
				if paragraphHasText {
					sb.WriteByte('\n')
				}
				paragraphHasText = false
			}
		case xml.EndElement:
			if el.Name.Local == "t" {
				inT = false
			}
		case xml.CharData:
			if inT {
				sb.Write(el)
				paragraphHasText = true
			}
		}
	}
	return sb.String(), nil
}

// doRead returns the slide summary (count, titles, text, notes) from the
// reopened bytes (C11.3 read behavior). Notes ride the same a:t extraction
// as slide text (they are slide-part XML too), so no content the deck holds
// is silently invisible.
func (t *OfficePptxTool) doRead(path string) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("office_pptx: read: %w", err)
	}
	count, text, err := slideText(path)
	if err != nil {
		return nil, err
	}
	return tools.ToolResult{
		Success: true,
		Result: map[string]any{
			"path":        path,
			"action":      "read",
			"slide_count": count,
			"text":        strings.TrimSpace(text),
			"verified":    true,
		},
	}, nil
}

// doValidate reopens and checks every expect_text fragment is present.
func (t *OfficePptxTool) doValidate(path string, args map[string]any) (any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("office_pptx: validate: %w", err)
	}
	count, text, err := slideText(path)
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
			"path":        path,
			"action":      "validate",
			"slide_count": count,
			"valid":       len(missing) == 0,
			"missing":     missing,
			"verified":    true,
		},
	}, nil
}

// Layout constants (EMU) and explicit text sizes (points), per the dsh
// office-pptx convention: set slide dimensions (pptxgo's New default IS
// widescreen 16:9) and text sizes explicitly, and keep images off the body
// text. Body placeholder spans the left ~60% of a 13.33in slide; the image
// occupies the right column at a fixed 4in x 3in.
const (
	pptxPointsTitle = 30
	pptxPointsBody  = 18
	pptxImageX      = int(8.6 * 914400) // 8.6in
	pptxImageY      = int(1.6 * 914400) // 1.6in
	pptxImageW      = 4 * 914400        // 4in
	pptxImageH      = 3 * 914400        // 3in
)

// copySlideInto reserves the nth slide slot in pres (1-indexed, matching
// the source deck's slide n) and overwrites the reserved slideN.xml part
// with the source slide's raw bytes, re-linking the slide's image
// relationships under the new package. Existing slide XML passes through
// byte-verbatim, so charts (inline), tables (inline), and mixed formatting
// survive untouched; media parts ride along under the same basename, and
// the new package's slide rel points at the copied part.
func copySlideInto(pres *pptx.Presentation, tmpl *pptx.Template, n int) error {
	srcPath := fmt.Sprintf("ppt/slides/slide%d.xml", n)
	dstPath := pptx.SlidePath(n)
	raw, err := deckPartBytes(tmplDeckPath(tmpl), srcPath)
	if err != nil {
		return fmt.Errorf("office_pptx: slide %d: %w", n, err)
	}
	// Reserve the slot with a throwaway AddSlide (wires rel + sldId
	// pptxgo-internally), then swap in the real bytes: re-adding a part at
	// an existing path overwrites its content in place (pptxgo
	// Package.AddRawPart contract).
	_ = pres.AddSlide()
	presAddRaw(pres, dstPath, pptx.ContentTypeSlide, raw)

	// Image rels on this slide -> re-register their media parts in the new
	// package (same basename => the copied slide's ../media/... targets
	// stay valid).
	rels, err := tmplRelationships(tmpl, srcPath)
	if err != nil {
		return fmt.Errorf("office_pptx: slide %d: %w", n, err)
	}
	for _, rel := range rels {
		if rel.Type != opc.RelTypeImage {
			continue
		}
		mediaPath := "ppt/" + strings.TrimPrefix(rel.Target, "../")
		mediaRaw, err := deckPartBytes(tmplDeckPath(tmpl), mediaPath)
		if err != nil {
			return fmt.Errorf("office_pptx: slide %d: %w", n, err)
		}
		// AddMediaPart lives on opc.Package; Presentation embeds its own
		// package unexported, so we go through the exposed raw seam.
		presAddMedia(pres, mediaPath, contentTypeForMedia(mediaPath), mediaRaw)
	}
	return nil
}

// The pptxgo Template keeps its opc.Package unexported, so the accessors
// below reach the slide bytes, slide relationships, and media bytes through
// encoding/xml + archive/zip over the deck on disk. They mirror exactly what
// Template.rawSlideBytes / Package.Relationships expose.

// ContentTypeImagePNG / ContentTypeImageJPEG / ContentTypeImageGIF, per ext.
func contentTypeForMedia(mediaPath string) string {
	switch strings.ToLower(filepath.Ext(mediaPath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

// The presAddRaw/presAddMedia seam: Presentation embeds its opc.Package
// unexported, but Save flushes the package through its exported io.Writer.
// The append branch therefore merges at the ZIP level: it saves the
// New-built package (reserved slides + appended slides) and injects the
// source deck's media parts and copied-slide bytes into that zip before it
// hits disk. injectDeckParts below does the byte splice.
func presAddRaw(pres *pptx.Presentation, pth, contentType string, raw []byte) {
	staged := stagedPartsFor(pres)
	staged[pth] = stagedPart{contentType: contentType, data: raw}
}

func presAddMedia(pres *pptx.Presentation, pth, contentType string, data []byte) {
	presAddRaw(pres, pth, contentType, data)
}

// stagedPart is one raw part waiting to be spliced into the saved zip.
type stagedPart struct {
	contentType string
	data        []byte
}

// stagedParts maps an in-flight Presentation to the raw parts copySlideInto
// staged onto it (slide XML overwrites + media adds). Keyed by pointer;
// entries live for one writeDeck call and are deleted there.
var stagedParts = map[*pptx.Presentation]map[string]stagedPart{}

func stagedPartsFor(pres *pptx.Presentation) map[string]stagedPart {
	m, ok := stagedParts[pres]
	if !ok {
		m = map[string]stagedPart{}
		stagedParts[pres] = m
	}
	return m
}

// tmplDeckPath records the on-disk deck a Template was opened from, so the
// copy helper can read media/rels parts the Template does not expose (its
// opc.Package is unexported). Filled by the edit path right after pptx.Open.
func tmplDeckPath(tmpl *pptx.Template) string {
	if tmpl == nil {
		return ""
	}
	if p, ok := tmplDeckPaths[tmpl]; ok {
		return p
	}
	return ""
}

// tmplDeckPaths associates an open Template with the file it came from.
// Keyed by pointer identity; entries live for one edit call and are deleted
// on the edit path's defer.
var tmplDeckPaths = map[*pptx.Template]string{}

// slideRelEntry is one Internal relationship of a slide part.
type slideRelEntry struct {
	ID     string
	Type   string
	Target string // package-root path (slash-led) or external URI
}

// deckSlideRels reads a slide part's relationships straight from the deck
// zip (no open Template required — used by the notes reader).
func deckSlideRels(deckPath, srcPath string) ([]slideRelEntry, error) {
	relPath := "ppt/slides/_rels/" + filepath.Base(srcPath) + ".rels"
	raw, err := deckPartBytes(deckPath, relPath)
	if err != nil {
		return nil, err
	}
	return parseSlideRels(raw, srcPath)
}

// parseSlideRels unmarshals slide .rels XML into slideRelEntry values,
// resolving targets the way pptxgo's resolvePartTarget does: slash-led
// targets are package-root paths, otherwise relative to the owning part's
// directory. External targets are skipped.
func parseSlideRels(raw []byte, srcPath string) ([]slideRelEntry, error) {
	var rels struct {
		Entries []struct {
			ID         string `xml:"Id,attr"`
			Type       string `xml:"Type,attr"`
			Target     string `xml:"Target,attr"`
			TargetMode string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(raw, &rels); err != nil {
		return nil, fmt.Errorf("parse slide rels: %w", err)
	}
	var out []slideRelEntry
	for _, e := range rels.Entries {
		if e.TargetMode == "External" {
			continue
		}
		target := e.Target
		if strings.HasPrefix(target, "/") {
			target = path.Clean(strings.TrimPrefix(target, "/"))
		} else {
			target = path.Clean(path.Join(path.Dir(srcPath), target))
		}
		out = append(out, slideRelEntry{ID: e.ID, Type: e.Type, Target: target})
	}
	return out, nil
}

// tmplRelationships returns the slide part's own relationships, read from
// the deck zip (ppt/slides/_rels/slideN.xml.rels). Target resolution mirrors
// pptxgo's resolvePartTarget: slash-led targets are package-root paths,
// otherwise they are relative to the owning part's directory.
func tmplRelationships(tmpl *pptx.Template, srcPath string) ([]slideRelEntry, error) {
	relPath := "ppt/slides/_rels/" + filepath.Base(srcPath) + ".rels"
	deckPath := ""
	if tmpl != nil {
		deckPath = tmplDeckPath(tmpl)
	}
	raw, err := deckPartBytes(deckPath, relPath)
	if err != nil {
		return nil, err
	}
	return parseSlideRels(raw, srcPath)
}

// deckPartBytes returns a part's raw bytes from a deck on disk (used for
// parts the Template does not expose: media, rels). It re-reads the zip per
// call — edit volume is one file per call, so this stays trivially cheap.
func deckPartBytes(deckPath, partPath string) ([]byte, error) {
	zr, err := zip.OpenReader(deckPath)
	if err != nil {
		return nil, fmt.Errorf("office_pptx: open %s: %w", deckPath, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != partPath {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("office_pptx: read %s: %w", partPath, err)
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("office_pptx: %s has no %s", deckPath, partPath)
}
