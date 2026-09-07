---
name: learn-from-video
description: "Turn a YouTube video (lecture, tutorial, demo) into a reusable skill: fetch the transcript, extract the generalizable procedure, and persist it as a new or updated skill."
tags: [media, learning, skills]
requires-tools:
  - transcript_fetch
---

# learn from video

## when to use
User pastes a YouTube URL (or names a video) and wants the knowledge
captured as a durable skill — not just summarized.

## workflow
1. Call `transcript_fetch` with the URL. If it errors, surface the
   install guidance verbatim; do not improvise a fallback without
   asking. For learn workflows add `output_path=<path>` (relative
   paths land in the session working dir) so the full transcript is
   written to disk and the tool returns only a bounded preview.
2. Read the transcript file with `file_read` in ~2k-char slices
   (about 50 lines; use offset/limit). Keep each request under ~2k
   chars so a whole slice survives even when the agent's tool-result
   budget is at its floor. After each slice, write structured notes:
   steps, decision rules, tool/API names, numbers, and the WHY. Keep
   notes short enough to hold the whole video's notes in mind at
   synthesis (~2-4k chars).
2.5. If the user asks to "describe the shape" / summarize the video
   rather than learn it, call `transcript_fetch` with `summarize=true`
   instead and work from the digest; page the file only for verbatim
   detail.
3. Distill what generalizes from your notes: steps, decision rules,
   failure modes, tool/API names, and the WHY behind choices. Discard
   one-off specifics (names, prices, dates) unless the user asked for
   them.
4. Draft the skill: frontmatter (kebab-case name derived from the
   topic; one-line description; tags), body with: when to use,
   prerequisites, numbered procedure, decision rules, verification
   steps, pitfalls. Keep it under ~200 lines.
5. SHOW THE DRAFT to the user and ask to confirm before writing.
6. On confirm: `skills_create` for a new skill; `skills_patch` (replace
   mode) to extend an existing skill. Report the created/patched path.

## decision rules
- The video teaches a PROCEDURE -> skill. It only reports NEWS ->
  offer a summary instead.
- Full transcript needed verbatim -> output_path + `file_read` paging.
  Gist only -> `summarize=true`.
- Existing skill covers the topic -> propose `skills_patch` with the
  exact old_string; never blind-rewrite.
- Ambiguous or conflicting steps in the transcript -> ask the user;
  never guess silently.
- Secrets, tokens, or credentials appearing in the transcript -> never
  copy them into the skill.

## verification
- After writing, run the skills list/get path (skills.get) to confirm
  the skill is discoverable, and report the name the user can invoke.
