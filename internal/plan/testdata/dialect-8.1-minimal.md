# Plan: Add user avatar upload

## Meta

- task_id: t-20260906-avatar
- version: 1
- status: draft
- updated: 2026-09-06

## Goal

Users can upload a profile avatar that persists across sessions.

## Decisions

- Decision: Store avatars on the local filesystem — Rationale: single-node deployment, no object store available.

## Open Questions

## Phases

### Phase 1: Avatar storage

Give uploads a durable home and validation rules.

**Produces:**

- `avatar-store` (file) — on-disk avatar directory plus write and validate helpers

**Consumes:** none

**Steps:**

1. Create the avatar directory layout and permissions [code]
2. Implement size and type validation helpers [code] (needs: Phase1.S1)

### Phase 2: Upload endpoint

Wire the HTTP upload path to the store.

**Produces:**

- `avatar-upload-endpoint` (interface) — POST /users/me/avatar handler

**Consumes:**

- `avatar-store` (file) — validated storage for uploaded images

**Steps:**

1. Add the upload handler backed by avatar-store [code] (needs: avatar-store)

## Notes

Max upload size is 2 MiB; JPEG and PNG only.
