# Image offload: model-facing media/image input surface is absent

## Problem

The 2026-11-02 dsh-vs-meept convergence audit closed this item as a **GitHub
issue, not an implementation leaf** (user decision D2/D9): images and media
uploaded by a user cannot reach a model in a user-shaped way. Meept's chat
path is text-first; a pasted/attached image has no pipeline into a
multimodal-capable model prompt, and there is no model-facing tool to load or
reference an image asset from the session. The engine has no image/vision
input surface today, so "image offload" (handing an image to the model) is
impossible for the user.

## Why it is deferred to a GitHub issue

The items 11/12 of the audit closeout were explicitly scoped OUT of the
implementation tree by user decision. This issue captures the product gap and
acceptance target without prescribing an engine — it is a backlog item for a
future session.

## Expected behavior

- A user can include an image in a chat turn and the model receives it as an
  input (text-plus-image message), not a bare filename or an OOM blob.
- Where the configured model is not multimodal, the system degrades honestly:
  a clear "model does not support image input" message, never a silent drop or
  a garbled prompt.
- A model-facing capability (tool and/or attached-image message block) lets
  the session reference an image already on disk (e.g. a screenshot or asset
  in the workspace).

## Acceptance criteria

- [ ] Attaching an image to a chat turn produces a model-visible image message
      for a multimodal-capable provider.
- [ ] A non-multimodal provider returns a detectable, user-shaped refusal
      (no silent corruption).
- [ ] A read tool exposes an on-disk image path to the model as an image
      input.
- [ ] No engine overreach: this issue scopes the surface only; engine/model
      routing stays as-is.

## Notes / grounding

- Present-absent anchored against the meept repo at the audit date: no image
  message block and no image-input tool exist in the chat path (audit
  runtime-safety.md and skills-schedule-storage.md found no model-facing image
  surface). This issue is a pure backlog item.
