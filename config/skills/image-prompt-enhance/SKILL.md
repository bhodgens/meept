---
name: image-prompt-enhance
description: Expand a short image brief into generator-ready prose. Use when an image must be generated or a prompt must be improved for Flux, SD, ComfyUI, or Midjourney.
tags:
  - image
  - prompting
requires:
  - reasoning
risk_level: low
---

# Image Prompt Enhance

Turn a short brief into a specific prompt a text-to-image model can follow.

This skill is written for a small model. Keep output short. Do not explain prompting theory.

## Extract first

Fill every blank. If a field is missing, invent a concrete default and mark it `[filled]`.

| Field | What to lock |
|-------|----------------|
| Subject | Who or what, count, pose, clothing, age band |
| Setting | Place, time of day, weather, foreground/background |
| Lighting | Key light, fill, color temperature |
| Camera | Shot size, angle, lens mm, depth of field |
| Style | Medium + one reference family (photo, oil, comic, 3d render) |
| Color | 2-4 named colors |
| Aspect | 1:1, 16:9, 9:16, 4:5 |
| Negatives | Only real failure modes for this subject |

## Write the prompt

Output three blocks only:

```
prompt:
[one paragraph, 40-90 words. Subject first. Then setting, lighting, camera, style, color. Concrete nouns. No "beautiful", "stunning", "masterpiece", "8k", "trending on artstation".]

negative:
[comma list. Skip if the backend is Midjourney; use --no instead.]

midjourney:
[subject, style, mood, lighting, composition --ar W:H --stylize 100]
```

Drop the `midjourney:` block unless the user named Midjourney.

## Backend notes

- **Flux / SD / ComfyUI**: prose paragraph. Natural language. No weight syntax unless the user asked.
- **Midjourney**: comma phrases, subject first, parameters last.
- **GPT-Image / Nano Banana**: prose is fine; keep text-in-image quotes exact and short.

## Consistency

On a re-render, copy locked identity phrases word-for-word. Change only the field the user asked to change.

## Do not

- Do not add film grain, CRT, or scanlines unless asked.
- Do not name living private people.
- Do not emit more than one prompt variant unless asked.
