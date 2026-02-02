"""Tests for the ClawSkill installer."""

from __future__ import annotations

import io
import zipfile
from pathlib import Path

import httpx
import pytest

from meept.clawskills.client import ClawHubClient
from meept.clawskills.installer import (
    ArchiveValidationError,
    ClawSkillInstaller,
    validate_archive,
)
from meept.clawskills.models import LockFile, OriginMetadata
from tests.test_clawskills.conftest import (
    MockTransport,
    make_bad_zip_bad_extension,
    make_bad_zip_executable,
    make_bad_zip_forbidden_file,
    make_bad_zip_path_traversal,
    make_skill_zip,
)


# ---------------------------------------------------------------------------
# Archive validation
# ---------------------------------------------------------------------------


class TestValidateArchive:
    def test_valid_archive(self) -> None:
        data = make_skill_zip("myskill")
        with zipfile.ZipFile(io.BytesIO(data)) as zf:
            paths = validate_archive(zf)
        assert any("SKILL.md" in p for p in paths)

    def test_path_traversal_rejected(self) -> None:
        data = make_bad_zip_path_traversal()
        with zipfile.ZipFile(io.BytesIO(data)) as zf:
            with pytest.raises(ArchiveValidationError, match="Path traversal"):
                validate_archive(zf)

    def test_forbidden_file_rejected(self) -> None:
        data = make_bad_zip_forbidden_file()
        with zipfile.ZipFile(io.BytesIO(data)) as zf:
            with pytest.raises(ArchiveValidationError, match="Forbidden file"):
                validate_archive(zf)

    def test_executable_rejected(self) -> None:
        data = make_bad_zip_executable()
        with zipfile.ZipFile(io.BytesIO(data)) as zf:
            with pytest.raises(ArchiveValidationError, match="Executable"):
                validate_archive(zf)

    def test_bad_extension_rejected(self) -> None:
        data = make_bad_zip_bad_extension()
        with zipfile.ZipFile(io.BytesIO(data)) as zf:
            with pytest.raises(ArchiveValidationError, match="Disallowed extension"):
                validate_archive(zf)

    def test_large_file_rejected(self) -> None:
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as zf:
            # Create a file larger than 200KB.
            zf.writestr("skill/huge.md", "x" * (201 * 1024))
        with zipfile.ZipFile(io.BytesIO(buf.getvalue())) as zf:
            with pytest.raises(ArchiveValidationError, match="too large"):
                validate_archive(zf)


# ---------------------------------------------------------------------------
# Installer
# ---------------------------------------------------------------------------


@pytest.fixture()
async def installer(install_dir: Path, mock_transport: MockTransport) -> ClawSkillInstaller:
    client = ClawHubClient(base_url="https://clawhub.ai")
    client._client = httpx.AsyncClient(
        transport=mock_transport,
        base_url="https://clawhub.ai",
    )
    inst = ClawSkillInstaller(base_dir=install_dir, client=client)
    yield inst
    await client.close()


class TestClawSkillInstaller:
    @pytest.mark.asyncio
    async def test_install_creates_files(
        self, installer: ClawSkillInstaller, install_dir: Path,
    ) -> None:
        origin = await installer.install("gifgrep", version="1.2.0")
        assert origin.slug == "gifgrep"
        assert origin.version == "1.2.0"
        assert (install_dir / "gifgrep" / "SKILL.md").is_file()
        assert (install_dir / "gifgrep" / ".origin.json").is_file()

    @pytest.mark.asyncio
    async def test_install_updates_lock(
        self, installer: ClawSkillInstaller, install_dir: Path,
    ) -> None:
        await installer.install("gifgrep", version="1.2.0")
        lock = LockFile.load(install_dir / ".lock.json")
        entry = lock.get("gifgrep")
        assert entry is not None
        assert entry.version == "1.2.0"
        assert entry.sha256 != ""

    @pytest.mark.asyncio
    async def test_install_resolves_version(
        self, installer: ClawSkillInstaller,
    ) -> None:
        origin = await installer.install("gifgrep")
        # MockTransport resolves to 1.2.0.
        assert origin.version == "1.2.0"

    @pytest.mark.asyncio
    async def test_remove(
        self, installer: ClawSkillInstaller, install_dir: Path,
    ) -> None:
        await installer.install("gifgrep", version="1.2.0")
        installer.remove("gifgrep")
        assert not (install_dir / "gifgrep").exists()
        lock = LockFile.load(install_dir / ".lock.json")
        assert lock.get("gifgrep") is None

    @pytest.mark.asyncio
    async def test_list_installed(
        self, installer: ClawSkillInstaller,
    ) -> None:
        await installer.install("gifgrep", version="1.2.0")
        entries = installer.list_installed()
        assert len(entries) == 1
        assert entries[0].slug == "gifgrep"

    @pytest.mark.asyncio
    async def test_get_origin(
        self, installer: ClawSkillInstaller,
    ) -> None:
        await installer.install("gifgrep", version="1.2.0")
        origin = installer.get_origin("gifgrep")
        assert origin is not None
        assert origin.slug == "gifgrep"

    @pytest.mark.asyncio
    async def test_get_origin_missing(
        self, installer: ClawSkillInstaller,
    ) -> None:
        assert installer.get_origin("nonexistent") is None

    @pytest.mark.asyncio
    async def test_install_bad_archive_rejected(
        self, install_dir: Path,
    ) -> None:
        bad_data = make_bad_zip_bad_extension()
        transport = MockTransport(zip_data=bad_data)
        client = ClawHubClient(base_url="https://clawhub.ai")
        client._client = httpx.AsyncClient(
            transport=transport, base_url="https://clawhub.ai",
        )
        inst = ClawSkillInstaller(base_dir=install_dir, client=client)
        try:
            with pytest.raises(ArchiveValidationError):
                await inst.install("evil", version="1.0.0")
        finally:
            await client.close()
