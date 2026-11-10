# 02 — CLI `.md` Dispatch in `agents create / validate / update`

## Dispatch Instruction

Do NOT commit. Do NOT run git add. Modify the CLI commands, run the 00T e2e gate (`make e2e-fast-area AREA=employee-markdown`), report results. The orchestrator reviews and commits.

## Objective

Make `meept agents create <def.md>`, `meept agents update <id> <def.md>`, and `meept agents validate <def.md>` accept an `EMPLOYEE.md` file (`.md` / `.markdown` extension) by dispatching through the new `internal/employeemd.Parse`. The `.json5`/`.json` path is unchanged.

## Files

- Modify: `cmd/meept/agents_cmd.go` (create + update + readDefinitionFile dispatch)
- Modify: `cmd/meept/agents_validate.go`
- Import added: `github.com/caimlas/meept/internal/employeemd`

## Context (read these)

- `cmd/meept/agents_cmd.go:219` `readDefinitionFile(path, v any)` reads + parses JSON5 into `v`. Keep it for the json5/json path.
- `agents create` (line 472) calls `readDefinitionFile(defPath, &def)` with `def map[string]any`, then `client.Call("agents.create", def)`.
- `agents update` (line 544) calls `readDefinitionFile(defPath, &def)` then sets `def["id"]=agentID` then `client.Call("agents.update", def)`.
- `cmd/meept/agents_validate.go` calls `readDefinitionFile(defPath, &def)` with a typed struct `{ID string; Constitution map[string]any}`, marshal-unmarshal the constitution, and `c.Validate(def.ID)`.

## Design (exact)

Add one helper in `cmd/meept` (new file is fine, or inline in agents_cmd.go):

```go
// isMarkdownDef reports whether path denotes an EMPLOYEE.md definition.
func isMarkdownDef(path string) bool {
    ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
    return ext == "md" || ext == "markdown"
}

// readEmployeeDefinition reads a definition file into a map[string]any,
// dispatching on extension: markdown goes through employeemd.Parse, all
// other extensions (json5/json) go through readDefinitionFile.
func readEmployeeDefinition(path string) (map[string]any, error) {
    if isMarkdownDef(path) {
        data, err := os.ReadFile(path)
        if err != nil {
            return nil, fmt.Errorf("failed to read definition file: %w", err)
        }
        m, err := employeemd.Parse(data)
        if err != nil {
            return nil, fmt.Errorf("invalid EMPLOYEE.md: %w", err)
        }
        return m, nil
    }
    var def map[string]any
    if err := readDefinitionFile(path, &def); err != nil {
        return nil, err
    }
    return def, nil
}
```

Then change each command to use `readEmployeeDefinition` instead of `readDefinitionFile`:

- `agents create`: `def, err := readEmployeeDefinition(defPath)`; keep the rest identical.
- `agents update`: `def, err := readEmployeeDefinition(defPath)`; `def["id"] = agentID`; keep the rest.
- `agents validate`: `def, err := readEmployeeDefinition(defPath)`; then read `def["id"]` and `def["constitution"]` as `map[string]any` out of the returned map (instead of the typed struct). Preserve the exact constitution decode + `c.Validate` block from the current file — only the file-reading front changes to support markdown.

Update command `Use:`/`Long`/`Short` strings: replace "definition file" / "definition.json5" mentions with "definition file (.json5 or .md)". Keep the prose accurate but minimal.

## Self-Verification

- [] `gofmt -l cmd/meept/` clean.
- `go build ./cmd/meept/` succeeds.
- The 00T gate: `make e2e-fast-area AREA=employee-markdown` goes GREEN (validate + create + update on the md fixture all pass).

## Review Checklist

- JSON5/json path is byte-identical to before (only `.md`/`.markdown` routed to the new parser).
- validate still performs exact constitution decode + `Constitution.Validate`; no behavior change for `.json5`.
- No new flags; extension-based dispatch as specified.
- Error messages name the failing file and are user-actionable.
