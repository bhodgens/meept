---
name: image-identify
description: Identify subject, objects, readable text, style, and source clues in an image. Use when the user asks what an image is or to describe a photo.
tags:
  - vision
  - identification
requires:
  - reasoning
risk_level: low
---

# Image Identify

Report what an image shows. Separate observation from guess.

## If you can see the pixels

1. Subject in one sentence.
2. Objects, people count, pose, setting.
3. Readable text, transcribed exactly.
4. Medium and style (photo, screenshot, illustration, 3d render, likely generator family).
5. Distinctive marks (logo, UI chrome, watermark, unique clothing).
6. Source guess only if a mark or cited search supports it.

## If you cannot see the pixels

Say that in the first line. Then use only:

- filename and path
- user caption
- EXIF or sidecar if present
- web search of distinctive on-screen text the user already quoted

Do not invent objects.

## Output

```
subject: ...
objects: ...
text: ...
style: ...
source_guess: ... (high|medium|low)
notes: ...
```

Every claim that is not a direct observation gets a confidence tag.

## Do not

- Do not name private people.
- Do not claim a brand or artwork without a visible mark or a cited hit.
- Do not reverse-search faces.
- Do not generate a replacement image.
