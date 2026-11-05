---
name: office-docx
description: Create, read, edit, and validate Word documents (.docx) - headings, paragraphs, and tables - through the office_docx tool.
tags:
  - office
  - docx
  - document
requires-tools:
  - office_docx
risk_level: low
examples:
  - "write the meeting notes into a docx"
  - "read report.docx and summarize it"
  - "append a conclusion section to proposal.docx"
---

# Word documents (.docx)

Use the `office_docx` tool for every Word operation. All paths resolve
inside the session working directory; escapes are refused.

## Create

`action="create"` with `blocks`: an array of
`{type:"heading", text, level:"1".."6"}`, `{type:"paragraph", text, style?}`,
or `{type:"table", rows:[[str]]}`. The document is saved at `path`, then
reopened and verified; report the `path` and `verified` from the result, not
what you sent.

## Read

`action="read"` returns the document text (paragraph and table cell text,
one node per line). Formatting is not represented; do not claim structure
the text does not show.

## Edit

`action="edit"` APPENDS blocks to an existing document. It never rewrites or
reorders earlier content. By default the result goes to a NEW file
`<name>.edited.docx` next to the original; pass `in_place: true` to
overwrite the original instead. After the save the tool reopens the file
and fails loudly if any appended text is missing.

## Validate

`action="validate"` with `expect_text`: an array of strings that must appear
in the document. The result reports `valid` plus any `missing` fragments.
Run it before trusting a document you did not create, and after edits the
user cares about.

## Deliver

Report the final absolute path from the tool result. Keep documents in the
session working directory unless the user asks otherwise.
