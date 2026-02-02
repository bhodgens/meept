"""ClawSkill installer -- download, validate, extract, and manage installed skills.

Handles the full install lifecycle:
1. Resolve version via :class:`ClawHubClient`.
2. Download ZIP archive (streaming, SHA-256 during download).
3. Validate archive contents (security-critical).
4. Extract to ``~/.meept/clawskills/{slug}/``.
5. Parse ``SKILL.md`` via existing parser.
6. Write ``.origin.json`` provenance metadata.
7. Update ``.lock.json``.
"""

from __future__ import annotations

import io
import logging
import shutil
import zipfile
from datetime import datetime, timezone
from pathlib import Path

from meept.clawskills.client import ClawHubClient
from meept.clawskills.models import LockFile, LockFileEntry, OriginMetadata
from meept.skills.parser import parse_skill_file

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Archive validation
# ---------------------------------------------------------------------------

_ALLOWED_EXTENSIONS: frozenset[str] = frozenset({
    ".md", ".txt", ".yaml", ".yml", ".json", ".toml",
})

_FORBIDDEN_FILENAMES: frozenset[str] = frozenset({
    ".env",
    "credentials.json",
    "secrets.yaml",
    "secrets.yml",
    ".git",
})

_MAX_FILE_SIZE = 200 * 1024  # 200 KB per file


class ArchiveValidationError(Exception):
    """Raised when a downloaded archive fails security validation."""


def validate_archive(zf: zipfile.ZipFile) -> list[str]:
    """Validate every entry in the ZIP archive.

    Returns a list of safe relative paths.
    Raises :class:`ArchiveValidationError` on the first violation.
    """
    safe_paths: list[str] = []

    for info in zf.infolist():
        name = info.filename

        # Skip directory entries.
        if info.is_dir():
            continue

        # Path traversal -- check individual path components, not just
        # substring match, and reject absolute paths on all platforms.
        if ".." in Path(name).parts:
            raise ArchiveValidationError(
                f"Path traversal detected: {name!r}"
            )
        if name.startswith("/") or (len(name) > 1 and name[1] == ":"):
            raise ArchiveValidationError(
                f"Absolute path in archive: {name!r}"
            )

        # Forbidden filenames.
        basename = Path(name).name
        if basename in _FORBIDDEN_FILENAMES:
            raise ArchiveValidationError(
                f"Forbidden file in archive: {name!r}"
            )

        # Extension whitelist.
        suffix = Path(name).suffix.lower()
        if suffix not in _ALLOWED_EXTENSIONS:
            raise ArchiveValidationError(
                f"Disallowed extension {suffix!r} in archive: {name!r}"
            )

        # Executable bits (Unix external_attr stores permissions in upper 16 bits).
        unix_mode = (info.external_attr >> 16) & 0o777
        if unix_mode & 0o111:
            raise ArchiveValidationError(
                f"Executable permission detected on: {name!r}"
            )

        # Individual file size.
        if info.file_size > _MAX_FILE_SIZE:
            raise ArchiveValidationError(
                f"File too large ({info.file_size} bytes): {name!r}"
            )

        safe_paths.append(name)

    return safe_paths


# ---------------------------------------------------------------------------
# Installer
# ---------------------------------------------------------------------------


class ClawSkillInstaller:
    """Manages the installation, update, and removal of clawskills.

    Parameters
    ----------
    base_dir:
        Root directory for installed clawskills (e.g. ``~/.meept/clawskills``).
    client:
        A :class:`ClawHubClient` instance for API calls.
    """

    def __init__(self, base_dir: Path, client: ClawHubClient) -> None:
        self.base_dir = base_dir
        self.client = client
        self._lock_path = base_dir / ".lock.json"

    def _load_lock(self) -> LockFile:
        return LockFile.load(self._lock_path)

    def _save_lock(self, lock: LockFile) -> None:
        lock.save(self._lock_path)

    async def install(
        self, slug: str, version: str | None = None,
    ) -> OriginMetadata:
        """Install a skill from ClawHub.

        Parameters
        ----------
        slug:
            The skill slug (e.g. ``"gifgrep"``).
        version:
            Specific version to install.  Resolves to latest if ``None``.

        Returns
        -------
        OriginMetadata
            Provenance metadata for the installed skill.
        """
        # 1. Resolve version.
        if version is None:
            resolved = await self.client.resolve_version(slug)
            version = resolved.get("version", resolved.get("latest", ""))
            if not version:
                raise ValueError(f"Could not resolve version for {slug!r}")

        log.info("Installing clawskill %s@%s", slug, version)

        # 2. Download ZIP.
        result = await self.client.download(slug, version)

        # 3. Validate archive.
        buf = io.BytesIO(result.data)
        with zipfile.ZipFile(buf, "r") as zf:
            safe_paths = validate_archive(zf)

            # 4. Extract to install_dir/{slug}/.
            skill_dir = self.base_dir / slug
            if skill_dir.exists():
                shutil.rmtree(skill_dir)
            skill_dir.mkdir(parents=True, exist_ok=True)

            for entry_path in safe_paths:
                # Flatten any top-level directory prefix (common in ZIPs).
                parts = Path(entry_path).parts
                if len(parts) > 1:
                    rel = Path(*parts[1:])
                else:
                    rel = Path(parts[0])

                dest = skill_dir / rel

                # Final safety check: resolved path must stay inside skill_dir.
                try:
                    dest.resolve().relative_to(skill_dir.resolve())
                except ValueError:
                    raise ArchiveValidationError(
                        f"Path escapes install directory: {entry_path!r}"
                    )

                dest.parent.mkdir(parents=True, exist_ok=True)
                dest.write_bytes(zf.read(entry_path))

        # 5. Verify SKILL.md exists.
        skill_md = skill_dir / "SKILL.md"
        if not skill_md.is_file():
            shutil.rmtree(skill_dir, ignore_errors=True)
            raise FileNotFoundError(
                f"Archive for {slug!r} does not contain SKILL.md"
            )

        parsed = parse_skill_file(skill_md)
        if parsed is None:
            shutil.rmtree(skill_dir, ignore_errors=True)
            raise ValueError(f"SKILL.md in {slug!r} is invalid or has no name")

        # 6. Write .origin.json.
        now = datetime.now(timezone.utc).isoformat()
        source_url = f"{self.client.base_url}/api/v1/download?slug={slug}&version={version}"
        origin = OriginMetadata(
            slug=slug,
            version=version,
            sha256=result.sha256,
            installed_at=now,
            source_url=source_url,
            files=safe_paths,
        )
        origin.save(skill_dir / ".origin.json")

        # 7. Update lock file.
        lock = self._load_lock()
        lock.add(LockFileEntry(
            slug=slug,
            version=version,
            sha256=result.sha256,
            installed_at=now,
            files=safe_paths,
        ))
        self._save_lock(lock)

        log.info("Installed clawskill %s@%s (%d bytes, sha256=%s)",
                 slug, version, result.size, result.sha256[:16])
        return origin

    async def update(self, slug: str) -> OriginMetadata | None:
        """Update a single installed skill to the latest version.

        Returns the new :class:`OriginMetadata` if updated, or ``None`` if
        already up to date.
        """
        lock = self._load_lock()
        entry = lock.get(slug)
        if entry is None:
            raise ValueError(f"Clawskill {slug!r} is not installed")

        resolved = await self.client.resolve_version(slug)
        latest = resolved.get("version", resolved.get("latest", ""))
        if latest and latest == entry.version:
            log.info("Clawskill %s is already at %s", slug, latest)
            return None

        return await self.install(slug, version=latest or None)

    async def update_all(self) -> list[str]:
        """Update all installed clawskills.  Returns slugs that were updated."""
        lock = self._load_lock()
        updated: list[str] = []
        for slug in list(lock.entries.keys()):
            try:
                result = await self.update(slug)
                if result is not None:
                    updated.append(slug)
            except Exception:
                log.warning("Failed to update clawskill %s", slug, exc_info=True)
        return updated

    def remove(self, slug: str) -> None:
        """Remove an installed clawskill."""
        skill_dir = self.base_dir / slug
        if skill_dir.exists():
            shutil.rmtree(skill_dir)

        lock = self._load_lock()
        lock.remove(slug)
        self._save_lock(lock)
        log.info("Removed clawskill %s", slug)

    def list_installed(self) -> list[LockFileEntry]:
        """Return all installed clawskills from the lock file."""
        lock = self._load_lock()
        return list(lock.entries.values())

    def get_origin(self, slug: str) -> OriginMetadata | None:
        """Load the .origin.json for an installed skill."""
        origin_path = self.base_dir / slug / ".origin.json"
        if not origin_path.is_file():
            return None
        try:
            return OriginMetadata.load(origin_path)
        except Exception:
            log.warning("Failed to load .origin.json for %s", slug, exc_info=True)
            return None
