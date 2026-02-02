"""Configuration loader for meept.

Reads ``meept.toml`` (and companion Markdown files) from the meept config
directory -- by default ``~/.meept/``.
"""

from __future__ import annotations

import logging
import os
import re
import sys
from pathlib import Path
from typing import Any

from meept.models.config_schema import MeeptSettings

log = logging.getLogger(__name__)

# Python 3.11+ ships ``tomllib`` in the stdlib.
if sys.version_info >= (3, 11):
    import tomllib
else:
    try:
        import tomli as tomllib  # type: ignore[no-redef]
    except ImportError as exc:
        raise ImportError(
            "meept requires Python 3.11+ (stdlib tomllib) or the 'tomli' package"
        ) from exc

_ENV_VAR_RE = re.compile(r"\$\{([^}]+)\}")

# Default config directory
_DEFAULT_CONFIG_DIR = Path("~/.meept").expanduser()


class MeeptConfig:
    """Loads, validates, and exposes the meept configuration.

    Parameters
    ----------
    config_path:
        Explicit path to ``meept.toml``.  When *None* the loader falls back
        to ``~/.meept/meept.toml``.
    """

    def __init__(self, config_path: str | Path | None = None) -> None:
        if config_path is not None:
            self._toml_path = Path(config_path).expanduser().resolve()
        else:
            self._toml_path = _DEFAULT_CONFIG_DIR / "meept.toml"

        self._config_dir = self._toml_path.parent

        # Parsed settings
        self.settings: MeeptSettings = MeeptSettings()

        # Companion documents loaded verbatim
        self.constitution: str = ""
        self.restrictions: str = ""
        self.purpose: str = ""

        # Perform initial load
        self._load()

    # ------------------------------------------------------------------
    # Public helpers
    # ------------------------------------------------------------------

    def reload(self) -> None:
        """Re-read every config source from disk.

        This is safe to call at runtime (e.g. in response to a
        ``CONFIG_RELOAD`` bus message).
        """
        self._load()
        log.info("config: reloaded from %s", self._toml_path)

    @property
    def config_dir(self) -> Path:
        """Return the resolved config directory path."""
        return self._config_dir

    @property
    def data_dir(self) -> Path:
        """Convenience accessor for ``settings.daemon.data_dir`` (expanded)."""
        return Path(self.settings.daemon.data_dir).expanduser().resolve()

    @property
    def models_config_path(self) -> Path:
        """Path to the ``models.json5`` configuration file.

        Looks for the file alongside ``meept.toml``, falling back to the
        shipped default in the config directory.
        """
        # Check next to meept.toml first.
        candidate = self._config_dir / "models.json5"
        if candidate.exists():
            return candidate

        # Fall back to user data dir.
        user_candidate = Path("~/.meept/models.json5").expanduser()
        if user_candidate.exists():
            return user_candidate

        # Return the default path (may not exist yet -- providers.py handles that).
        return candidate

    # ------------------------------------------------------------------
    # Internals
    # ------------------------------------------------------------------

    def _load(self) -> None:
        raw = self._load_toml()
        expanded = _expand_env_vars(raw)
        expanded = _expand_paths(expanded)
        self.settings = MeeptSettings.model_validate(expanded)
        self._load_documents()

    def _load_toml(self) -> dict[str, Any]:
        if not self._toml_path.exists():
            log.warning("config: %s not found -- using defaults", self._toml_path)
            return {}
        with self._toml_path.open("rb") as fp:
            data: dict[str, Any] = tomllib.load(fp)
        log.debug("config: parsed %s", self._toml_path)
        return data

    def _load_documents(self) -> None:
        """Load constitution.md, restrictions.md, purpose.md if present."""
        self.constitution = _read_text(self._config_dir / "constitution.md")
        self.restrictions = _read_text(self._config_dir / "restrictions.md")
        self.purpose = _read_text(self._config_dir / "purpose.md")


# ------------------------------------------------------------------
# Module-level helpers
# ------------------------------------------------------------------


def _read_text(path: Path) -> str:
    """Return file contents or empty string if missing."""
    try:
        return path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return ""


def _expand_env_vars(obj: Any) -> Any:
    """Recursively replace ``${VAR}`` references in string values.

    If the environment variable is not set the placeholder is left as-is
    and a debug message is logged.
    """
    if isinstance(obj, str):
        def _replacer(match: re.Match[str]) -> str:
            var = match.group(1)
            value = os.environ.get(var)
            if value is None:
                log.debug("config: env var %r not set -- keeping placeholder", var)
                return match.group(0)
            return value
        return _ENV_VAR_RE.sub(_replacer, obj)
    if isinstance(obj, dict):
        return {k: _expand_env_vars(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [_expand_env_vars(item) for item in obj]
    return obj


def _expand_paths(obj: Any) -> Any:
    """Expand leading ``~`` in string values that look like filesystem paths.

    A value is treated as a path candidate if it starts with ``~/`` or ``~``.
    """
    if isinstance(obj, str) and obj.startswith("~"):
        return str(Path(obj).expanduser())
    if isinstance(obj, dict):
        return {k: _expand_paths(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [_expand_paths(item) for item in obj]
    return obj
