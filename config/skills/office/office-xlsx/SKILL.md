---
name: office-xlsx
description: Create, read, edit, and validate Excel workbooks (.xlsx) - sheets, headers, typed cells, and formulas - through the office_workbook tool.
tags:
  - office
  - xlsx
  - spreadsheet
requires-tools:
  - office_workbook
risk_level: low
examples:
  - "create a workbook with the survey results"
  - "read report.xlsx and summarize the sheets"
  - "add a totals formula to the budget sheet"
---

# Excel workbooks (.xlsx)

Use the `office_workbook` tool for every Excel operation. All paths resolve
inside the session working directory; escapes are refused. Save to a new
workbook unless the user requests an in-place edit.

## Create

`action="create"` with `sheets`: an array of
`{name, headers: [str], rows: [[cell]], highlight_rows: [int]}`. Cells are
string, number, boolean, or null. Numbers stay numbers - never stringify
numeric cells. The tool reopens the saved file and returns verified sheet
names and cell counts; report those, not what you sent.

## Read

`action="read"` returns every sheet with typed cell values. Formula cells
come back as `{formula, evaluated: false}` plus an optional `cached_value`:

- A formula is NEVER evaluated by this tool. `evaluated: false` means the
  computed result does not exist here.
- `cached_value` may be absent or stale (written by whatever app last saved
  the file). Never report it as a newly calculated result.
- When the user needs computed values, say that recalculation requires
  opening the workbook in Excel/LibreOffice; do not substitute constants
  for requested formulas.

## Edit

`action="edit"` with `cells`: an array of `{sheet, cell, value}` for plain
values or `{sheet, cell, formula}` for formulas (include the leading `=`).
Edits are targeted - untouched cells, sheets, and formulas survive. The tool
reopens the file and verifies each touched cell; check the `verified` list
before reporting success.

## Validate

`action="validate"` reopens the workbook and reports verified sheet names,
row/cell counts, and formula-cell counts. Run it after any create/edit the
user cares about, and after receiving a workbook you did not create, before
trusting its contents.

## Deliver

Report the final absolute path from the tool result. Keep the workbook in
the session working directory; do not copy it elsewhere unless asked.
