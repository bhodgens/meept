"""Plugin discovery and loading for third-party tools.

Plugins live in directories under a configurable plugin root. Each plugin
directory must contain a ``meept.plugin.json`` manifest and a Python module
that exposes a ``register(registry)`` callable.
"""

from __future__ import annotations

import importlib
import importlib.util
import json
import logging
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from meept.tools.interface import ToolRegistry

log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Plugin metadata
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class PluginInfo:
    """Parsed metadata from a plugin's ``meept.plugin.json``.

    Parameters
    ----------
    name:
        Plugin display name.
    version:
        Semver version string.
    description:
        Short human-readable summary.
    path:
        Filesystem path to the plugin directory.
    module_name:
        Dotted Python module name (or filename) to import.
    """

    name: str
    version: str
    description: str
    path: Path
    module_name: str


# ---------------------------------------------------------------------------
# Loader
# ---------------------------------------------------------------------------


class PluginLoader:
    """Discovers and loads meept plugins from a directory.

    Parameters
    ----------
    plugin_dir:
        Root directory to scan for plugin sub-directories.
    """

    MANIFEST_FILE = "meept.plugin.json"

    def __init__(self, plugin_dir: Path) -> None:
        self._plugin_dir = plugin_dir

    def discover(self) -> list[PluginInfo]:
        """Scan the plugin directory for valid plugin manifests.

        Returns a list of :class:`PluginInfo` for every directory that
        contains a valid ``meept.plugin.json``.  Directories without a
        manifest or with malformed JSON are silently skipped (with a
        warning logged).
        """
        plugins: list[PluginInfo] = []

        if not self._plugin_dir.is_dir():
            log.debug("Plugin directory does not exist: %s", self._plugin_dir)
            return plugins

        for child in sorted(self._plugin_dir.iterdir()):
            if not child.is_dir():
                continue

            manifest_path = child / self.MANIFEST_FILE
            if not manifest_path.is_file():
                continue

            try:
                manifest = self._parse_manifest(manifest_path, child)
                plugins.append(manifest)
                log.debug("Discovered plugin: %s (%s)", manifest.name, manifest.version)
            except Exception:
                log.warning("Failed to parse plugin manifest: %s", manifest_path, exc_info=True)

        return plugins

    def load(self, plugin_info: PluginInfo, registry: ToolRegistry) -> None:
        """Import a plugin module and call its ``register`` function.

        The plugin module is expected to expose a callable
        ``register(registry: ToolRegistry) -> None`` at module level.

        Parameters
        ----------
        plugin_info:
            Metadata describing the plugin to load.
        registry:
            The tool registry to pass to the plugin's ``register`` function.

        Raises
        ------
        ImportError
            If the module cannot be imported.
        AttributeError
            If the module does not expose a ``register`` callable.
        """
        module = self._import_plugin_module(plugin_info)

        register_fn = getattr(module, "register", None)
        if register_fn is None or not callable(register_fn):
            raise AttributeError(
                f"Plugin {plugin_info.name!r} module {plugin_info.module_name!r} "
                f"does not expose a callable 'register'"
            )

        log.info("Loading plugin: %s v%s", plugin_info.name, plugin_info.version)
        register_fn(registry)

    def load_all(self, registry: ToolRegistry) -> None:
        """Discover all plugins and load them into *registry*.

        Plugins that fail to load are logged as warnings but do not
        prevent other plugins from loading.
        """
        plugins = self.discover()
        for plugin_info in plugins:
            try:
                self.load(plugin_info, registry)
            except Exception:
                log.warning(
                    "Failed to load plugin %s: skipping",
                    plugin_info.name,
                    exc_info=True,
                )

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _parse_manifest(manifest_path: Path, plugin_dir: Path) -> PluginInfo:
        """Parse a ``meept.plugin.json`` file into a :class:`PluginInfo`."""
        with manifest_path.open("r", encoding="utf-8") as fh:
            data: dict[str, Any] = json.load(fh)

        name = data.get("name")
        version = data.get("version", "0.0.0")
        description = data.get("description", "")
        module_name = data.get("module", "plugin")

        if not name:
            raise ValueError(f"Plugin manifest missing 'name': {manifest_path}")

        return PluginInfo(
            name=name,
            version=version,
            description=description,
            path=plugin_dir,
            module_name=module_name,
        )

    @staticmethod
    def _import_plugin_module(plugin_info: PluginInfo) -> Any:
        """Import the plugin's Python module.

        If *module_name* looks like a dotted path it is imported directly.
        Otherwise it is treated as a filename relative to the plugin
        directory.
        """
        module_name = plugin_info.module_name

        # If the module name has no dots and the file exists on disk,
        # load it from the filesystem path.
        module_file = plugin_info.path / f"{module_name}.py"
        if module_file.is_file():
            spec = importlib.util.spec_from_file_location(
                f"meept_plugin_{plugin_info.name}",
                module_file,
            )
            if spec is None or spec.loader is None:
                raise ImportError(f"Cannot create module spec for {module_file}")
            mod = importlib.util.module_from_spec(spec)
            sys.modules[spec.name] = mod
            spec.loader.exec_module(mod)  # type: ignore[union-attr]
            return mod

        # Fall back to standard import (e.g. for installed packages).
        return importlib.import_module(module_name)
