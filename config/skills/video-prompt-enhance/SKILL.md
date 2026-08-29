---
name: video-prompt-enhance
description: Expand a short video brief into clip-capped, identity-locked prompts. Use when video must be generated or a T2V prompt must be improved.
tags:
  - video
  - prompting
requires:
  - reasoning
risk_level: low
---

# Video Prompt Enhance

Turn a short brief into clip-ready video prompts.

This skill is written for a small model. Keep output short. Confirm the live model name before you lock field labels.

## Caps

- Default clip length: 8-12 s. Hard cap: 15 s unless the user named a longer model.
- One beat per clip. Chain clips for longer pieces.
- Dialogue budget: about 2.5 words per second.

## Extract first

| Field | What to lock |
|-------|----------------|
| Action | One beat. Who does what. |
| Duration | Seconds for this clip |
| Identity | Locked character block, copied verbatim on later clips |
| Camera | Shot size, move (static, pan, dolly, orbit), cut count |
| Setting | Place, time, weather |
| Sound | Diegetic only in the scene field. Audience music is separate or N/A. |
| Dialogue | Speaker id, language, exact line, on-screen vs voiceover |
| Aspect | 16:9, 9:16, 1:1 |

## Write the prompt

One fenced block per clip:

```
clip: 1
duration_s: 10
aspect: 16:9
prompt:
[scene action in present tense. Include locked identity block verbatim. End on a clear last frame.]
sound:
[ambient + action + spoken lines. No non-diegetic score unless asked.]
dialogue:
[optional. Keep under the word budget.]
```

If the user named MiniMax H3, use that family's field labels instead of this generic pack. Do not invent vendor syntax.

## Consistency

- Repeat locked identity text word-for-word in every clip that shows the person.
- Regen whole clips. Do not splice mid-line.
- Search invented names before they ship.

## Do not

- Do not write one prompt for a 60 s piece.
- Do not paraphrase the locked identity block.
- Do not bake CRT or grain into the source prompt.
- Do not put music the characters can hear in a non-diegetic music field.
