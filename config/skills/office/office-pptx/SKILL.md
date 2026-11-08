---
name: office-pptx
description: Create, read, edit, and validate PowerPoint presentations (.pptx) - slides with titles, bullet bodies, notes, and images - through the office_pptx tool.
tags:
  - office
  - pptx
  - presentation
requires-tools:
  - office_pptx
risk_level: low
examples:
  - "build a 3-slide deck summarizing this report as a pptx"
  - "read quarterly.pptx and list the slide titles"
  - "add a slide about the budget to deck.pptx"
---

# PowerPoint presentations (.pptx)

Use the `office_pptx` tool for every .pptx operation. All paths — including
`image_path` — must resolve inside the session working directory; escapes are
refused. The tool edits content, not appearance: it cannot verify text fit,
contrast, or rendering fidelity, so do not claim visual quality the result
does not show.

## Create

`action="create"` with `slides`: an array of
`{title, body:[str], notes, image_path}` — `title` is required per slide,
`body` is the bullet lines, `notes` the speaker notes, `image_path` an
optional image file already inside the session working directory. Slides get
the standard 16:9 dimensions and explicit title/body text sizes. The deck is
saved at `path`, then reopened and verified; report the returned `path`,
`slides`, and `verified`, not what you sent.

## Read

`action="read"` returns the slide count, each slide's title, text, and
notes. Formatting, charts, and SmartArt are not represented; do not claim
structure the text does not show.

## Edit

`action="edit"` with `slides` APPENDS new slides to an existing deck. It
never rebuilds or reorders existing slides — charts, SmartArt, animations,
and mixed formatting on untouched slides pass through unchanged. Each entry
may carry `replace: {find, with}` to change text inside an EXISTING slide
(the only supported way to alter existing content without rebuilding it).
By default the result goes to a NEW file `<name>.edited.pptx` next to the
original; pass `in_place: true` to overwrite. After the save the tool
reopens the file, re-verifies the slide count, and fails loudly if any
`replace` target was missing or any appended content did not survive.

## Validate

`action="validate"` with `expect_text`: an array of strings that must appear
somewhere in the deck's slide text. The result reports `valid` plus any
`missing` fragments. Run it before trusting a deck you did not create, and
after edits the user cares about.

## Deliver

Report the final absolute path from the tool result. Keep decks in the
session working directory unless the user asks otherwise.
