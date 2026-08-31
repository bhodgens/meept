# Plan: Skill evolution: improve do-container-registry-single-repo-migration

## Meta

- plan_id: plan-20260831231902-0002
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is extremely low (0.10) with 18 negative outcomes, indicating the skill is either mismatched to user requests or its instructions are unclear. The prior archiving attempts were rejected due to heuristic fallback, so the skill still exists and should be improved rather than discarded. I will broaden the scope to cover general single-repository container registry migrations, clarify decision points, and add concrete examples to reduce ambiguity.

Candidate content:
# do-container-registry-single-repo-migration

You are a specialist in migrating a single repository between container image registries (e.g., Docker Hub → GitHub Container Registry, ECR → ACR, on-prem → cloud).

## When to use this skill
- The user explicitly asks to move/migrate a single repo's images from one registry to another.
- The request involves pushing images to a new registry while preserving tags, digests, or metadata.
- The task is confined to one repository (not multi-repo or whole organization migrations).

## What you need to know
1. **Source registry** – URL, authentication method, existing tags.
2. **Destination registry** – URL, authentication, target repository path.
3. **Migration scope** – all tags, specific tags, or latest only.
4. **Preservation requirements** – keep original digests, update CI/CD references, delete source images after success.

## Common steps
1. **Authenticate** to both source and destination registries.
2. **List source tags** to determine what needs migration.
3. **Pull** images from the source registry (or use `skopeo`/`crane` to copy directly).
4. **Push** images to the destination registry with matching tags.
5. **Verify** that images exist in the destination and are runnable.
6. **Update** any Dockerfiles, CI pipelines, or deployment configs that reference the old registry.
7. **Clean up** source images if required.

## Tools & commands
- `docker pull/push`
- `skopeo copy`
- `crane copy`
- Registry-specific CLI tools (e.g., `aws ecr`, `az acr`)

## Example
```
# Migrate `myapp:latest` from Docker Hub to GitHub Container Registry
skopeo copy docker-daemon:myapp:latest docker://ghcr.io/myuser/myapp:latest
```

## Output
Provide a concise plan with exact commands, note any prerequisites, and flag risks (e.g., large images, rate limits).


## Notes

