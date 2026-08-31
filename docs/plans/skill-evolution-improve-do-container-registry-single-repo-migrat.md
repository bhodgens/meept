# Plan: Skill evolution: improve do-container-registry-single-repo-migration

## Meta

- plan_id: plan-20260831224614-0036
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is 0.11 with 17/19 negative responses, indicating the current instructions are ineffective. Likely issues: no clear prerequisite checks, ambiguous migration steps, and missing error handling for common failures like credential issues or non-existent source repos.

Candidate content:
# Container Registry Single Repo Migration

## Purpose
Migrate a single repository's container images from one container registry to another (e.g., Docker Hub → GHCR, ECR → GHCR, or between regions).

## Prerequisites
Before starting, verify:
1. **Source credentials** — You have access to the source registry (login token, password, or OIDC).
2. **Destination credentials** — You have write access to the destination registry.
3. **Source image exists** — Confirm the image and tag exist in the source registry before attempting migration.
4. **Docker CLI installed** — Ensure `docker` is available and logged into both registries.

## Step-by-Step Instructions

### Step 1: Authenticate to Both Registries
```bash
# Source registry
docker login <source-registry> -u <username> -p <password-or-token>

# Destination registry
docker login <dest-registry> -u <username> -p <password-or-token>
```

### Step 2: Pull the Image
```bash
docker pull <source-registry>/<repo>:<tag>
```
If the tag is missing or pull fails, verify the image name and tag in the source registry first.

### Step 3: Tag for Destination
```bash
docker tag <source-registry>/<repo>:<tag> <dest-registry>/<repo>:<tag>
```

### Step 4: Push to Destination
```bash
docker push <dest-registry>/<repo>:<tag>
```

### Step 5: Verify
```bash
docker manifest inspect <dest-registry>/<repo>:<tag>
```

## Common Errors & Fixes
| Error | Cause | Fix |
|---|---|---|
| `unauthorized` | Wrong credentials | Re-authenticate with correct token |
| `manifest unknown` | Image or tag doesn't exist | List source tags with `docker run --rm mplatform/mquery <source>:<tag>` or check registry UI |
| `connection refused` | Registry unreachable | Check network/firewall, verify registry URL |
| `resource temporarily unavailable` | Rate limiting | Add `--quiet` and retry with backoff |

## Important Notes
- This skill handles **single repo only**. For multi-repo migrations, use batch migration scripts.
- If the source and destination are the same registry but different paths, treat as a rename operation (pull + re-tag + push).
- Always verify the destination image before deleting or updating the source.

## Notes

