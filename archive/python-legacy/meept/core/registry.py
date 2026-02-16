"""Component registry with singleton semantics and async-aware factories."""

from __future__ import annotations

import asyncio
import logging
from typing import Any, Callable

log = logging.getLogger(__name__)

# Type alias for factories -- may be sync or async callables.
Factory = Callable[..., Any]


class Registry:
    """Central service-locator / dependency-injection container.

    Each component is identified by a unique string *name*.  A *factory* is
    registered for that name and is invoked at most once (singleton) when the
    component is first requested via :meth:`get_or_create`.

    Factories may be plain callables **or** async callables -- the registry
    will ``await`` the result when appropriate.
    """

    def __init__(self) -> None:
        self._factories: dict[str, Factory] = {}
        self._instances: dict[str, Any] = {}
        self._lock = asyncio.Lock()

    # ------------------------------------------------------------------
    # Registration
    # ------------------------------------------------------------------

    def register(self, name: str, factory: Factory) -> None:
        """Associate *name* with a callable *factory*.

        If the name is already registered the old factory is silently
        replaced, but any previously-created singleton instance is *not*
        discarded (use :meth:`remove` first if you need a fresh instance).
        """
        if name in self._factories:
            log.debug("registry: overwriting factory for %r", name)
        self._factories[name] = factory

    def register_instance(self, name: str, instance: Any) -> None:
        """Directly store a pre-built *instance* under *name*.

        This bypasses the factory mechanism entirely and is useful for
        objects that are constructed externally (e.g. the registry itself,
        or the event loop).
        """
        self._instances[name] = instance
        log.debug("registry: stored instance for %r", name)

    # ------------------------------------------------------------------
    # Retrieval
    # ------------------------------------------------------------------

    def get(self, name: str) -> Any | None:
        """Return the singleton instance for *name*, or ``None``.

        This method never invokes the factory -- it only returns an
        instance that has already been created.
        """
        return self._instances.get(name)

    async def get_or_create(self, name: str) -> Any:
        """Return the singleton for *name*, creating it on first access.

        The registered factory is called exactly once.  If the factory is a
        coroutine function its result is awaited.

        Raises
        ------
        KeyError
            If no factory has been registered for *name*.
        """
        # Fast path -- already instantiated.
        instance = self._instances.get(name)
        if instance is not None:
            return instance

        async with self._lock:
            # Double-check after acquiring the lock.
            instance = self._instances.get(name)
            if instance is not None:
                return instance

            factory = self._factories.get(name)
            if factory is None:
                raise KeyError(f"No factory registered for component {name!r}")

            log.debug("registry: creating component %r", name)
            result = factory()
            if asyncio.iscoroutine(result):
                result = await result
            self._instances[name] = result
            return result

    # ------------------------------------------------------------------
    # Housekeeping
    # ------------------------------------------------------------------

    def remove(self, name: str) -> None:
        """Remove both the factory and singleton for *name* (if present)."""
        self._factories.pop(name, None)
        self._instances.pop(name, None)

    def has(self, name: str) -> bool:
        """Return ``True`` if a factory or instance is registered for *name*."""
        return name in self._factories or name in self._instances

    @property
    def names(self) -> list[str]:
        """Return sorted list of all registered component names."""
        return sorted(set(self._factories) | set(self._instances))

    def __contains__(self, name: str) -> bool:
        return self.has(name)

    def __repr__(self) -> str:
        return f"<Registry components={self.names!r}>"
