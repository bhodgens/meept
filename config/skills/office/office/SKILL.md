---
name: office
description: Create, edit, read, and validate Office files - pick the right format skill by file extension (.docx, .pptx, .xlsx), then use its tools on the file. Use when a task mentions a Word, PowerPoint, or Excel file.
tags:
  - office
  - document
  - presentation
  - spreadsheet
risk_level: low
examples:
  - "write the meeting notes into a docx"
  - "build a slide deck summarizing this report"
  - "put the survey results into a spreadsheet"
---

# Office files

Route any Office task to the format skill that matches the file extension. This
skill is a routing index only - it picks a skill and lets that skill's workflow
drive the work. Do not re-derive the per-format steps here.

## Routing

Read the target file's extension and load the matching skill by name:

| Extension | Skill to load | Backing tool |
|-----------|---------------|--------------|
| `.docx`   | `office-docx` | `office_docx`   |
| `.pptx`   | `office-pptx` | `office_pptx`   |
| `.xlsx`   | `office-xlsx` | `office_workbook` |

Rules:

- Match on the actual file extension. If the extension is missing or unknown,
  ask the user rather than guessing a format.
- Load the format skill BEFORE calling any tool. The format skill carries the
  tool contract and the requires-tools gate; do not bypass it.
- A format skill is a workflow, not a rendering engine. Formatting, layout, and
  visual fidelity are the format skill's concern, never the router's.

## Deliver

After the format skill's workflow succeeds, report the final absolute path from
the tool result. Keep files in the session working directory unless the user
asks otherwise.
