"""Task memory subsystem -- domain-specific technical knowledge.

Attempts to use ``memvid`` for video-encoded vector memory.  When ``memvid``
is not installed the module falls back to a fully-functional SQLite + FTS5
implementation so domain search keeps working.
"""

from __future__ import annotations

import json
import logging
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import aiosqlite

from meept.models.memory_types import MemoryItem, MemoryResult, MemoryType

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Probe for memvid availability
# ---------------------------------------------------------------------------

_MEMVID_AVAILABLE: bool
try:
    import memvid  # type: ignore[import-untyped]

    _MEMVID_AVAILABLE = True
    logger.debug("memvid library detected -- using memvid for task memory.")
except ImportError:
    _MEMVID_AVAILABLE = False
    logger.debug("memvid not installed -- task memory will use SQLite/FTS5 fallback.")


# ---------------------------------------------------------------------------
# SQLite FTS5 schema
# ---------------------------------------------------------------------------

_CREATE_TABLE_SQL = """\
CREATE TABLE IF NOT EXISTS task_memories (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    domain        TEXT NOT NULL DEFAULT 'general',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL
);
"""

_CREATE_FTS_SQL = """\
CREATE VIRTUAL TABLE IF NOT EXISTS task_fts
USING fts5(content, domain, content='task_memories', content_rowid='rowid');
"""

_TRIGGER_INSERT = """\
CREATE TRIGGER IF NOT EXISTS task_fts_ai AFTER INSERT ON task_memories BEGIN
    INSERT INTO task_fts(rowid, content, domain)
    VALUES (new.rowid, new.content, new.domain);
END;
"""

_TRIGGER_DELETE = """\
CREATE TRIGGER IF NOT EXISTS task_fts_ad AFTER DELETE ON task_memories BEGIN
    INSERT INTO task_fts(task_fts, rowid, content, domain)
    VALUES ('delete', old.rowid, old.content, old.domain);
END;
"""

_TRIGGER_UPDATE = """\
CREATE TRIGGER IF NOT EXISTS task_fts_au AFTER UPDATE ON task_memories BEGIN
    INSERT INTO task_fts(task_fts, rowid, content, domain)
    VALUES ('delete', old.rowid, old.content, old.domain);
    INSERT INTO task_fts(rowid, content, domain)
    VALUES (new.rowid, new.content, new.domain);
END;
"""


# ---------------------------------------------------------------------------
# Public class
# ---------------------------------------------------------------------------


class TaskMemory:
    """Stores and retrieves domain-specific task/technical memories.

    When ``memvid`` is available it is used for vector-encoded storage.
    Otherwise an SQLite database with FTS5 full-text search is used as a
    drop-in replacement.
    """

    def __init__(self) -> None:
        self._db: aiosqlite.Connection | None = None
        self._data_dir: Path | None = None
        self._domains: list[str] = ["general"]
        self._use_memvid: bool = False
        self._memvid_stores: dict[str, Any] = {}
        self._initialized: bool = False

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def initialize(
        self,
        data_dir: Path,
        domains: list[str] | None = None,
    ) -> None:
        """Set up the task memory backend.

        Parameters
        ----------
        data_dir:
            Directory in which database files are stored.
        domains:
            List of knowledge domains to track (e.g. ``["general", "code",
            "commands"]``).  Defaults to ``["general"]``.
        """
        self._data_dir = data_dir
        self._domains = domains or ["general"]
        data_dir.mkdir(parents=True, exist_ok=True)

        if _MEMVID_AVAILABLE:
            try:
                self._init_memvid(data_dir)
                self._use_memvid = True
                logger.info("Task memory initialised with memvid backend.")
            except Exception:
                logger.warning(
                    "Failed to initialise memvid -- falling back to SQLite.",
                    exc_info=True,
                )
                self._use_memvid = False

        # Always set up SQLite as canonical store / fallback.
        db_path = data_dir / "task.db"
        self._db = await aiosqlite.connect(str(db_path))
        await self._db.execute("PRAGMA journal_mode=WAL;")
        await self._db.execute("PRAGMA foreign_keys=ON;")
        await self._db.execute(_CREATE_TABLE_SQL)
        await self._db.execute(_CREATE_FTS_SQL)
        await self._db.execute(_TRIGGER_INSERT)
        await self._db.execute(_TRIGGER_DELETE)
        await self._db.execute(_TRIGGER_UPDATE)
        await self._db.commit()

        self._initialized = True
        logger.info(
            "Task memory ready (backend=%s, domains=%s, db=%s).",
            "memvid" if self._use_memvid else "sqlite/fts5",
            self._domains,
            db_path,
        )

    # ------------------------------------------------------------------
    # Store
    # ------------------------------------------------------------------

    async def store(
        self,
        content: str,
        domain: str = "general",
        metadata: dict | None = None,
    ) -> str:
        """Persist a new task memory.

        Returns the unique id of the stored item.
        """
        self._ensure_initialized()
        assert self._db is not None

        item_id = uuid.uuid4().hex
        now_iso = datetime.now(timezone.utc).isoformat()
        meta_json = json.dumps(metadata or {})

        await self._db.execute(
            """
            INSERT INTO task_memories (id, content, domain, metadata_json, created_at)
            VALUES (?, ?, ?, ?, ?)
            """,
            (item_id, content, domain, meta_json, now_iso),
        )
        await self._db.commit()

        # Also index in memvid when available.
        if self._use_memvid:
            store = self._memvid_stores.get(domain)
            if store is not None:
                try:
                    store.add(content, metadata={"id": item_id, **(metadata or {})})
                except Exception:
                    logger.warning(
                        "memvid indexing failed for %s in domain %s",
                        item_id,
                        domain,
                        exc_info=True,
                    )

        logger.debug("Stored task memory %s (domain=%s).", item_id, domain)
        return item_id

    # ------------------------------------------------------------------
    # Search
    # ------------------------------------------------------------------

    async def search(
        self,
        query: str,
        domain: str | None = None,
        limit: int = 10,
    ) -> list[MemoryResult]:
        """Search task memories, optionally scoped to a domain.

        Uses memvid when available, otherwise falls back to FTS5.
        """
        self._ensure_initialized()

        if self._use_memvid:
            return await self._search_memvid(query, domain, limit)
        return await self._search_fts(query, domain, limit)

    # ------------------------------------------------------------------
    # Stats helpers
    # ------------------------------------------------------------------

    async def count(self) -> int:
        """Return total number of task memories."""
        self._ensure_initialized()
        assert self._db is not None
        async with self._db.execute("SELECT COUNT(*) FROM task_memories") as cur:
            row = await cur.fetchone()
            return row[0] if row else 0

    async def get_oldest_timestamp(self) -> datetime | None:
        """Return the created_at of the oldest task memory."""
        self._ensure_initialized()
        assert self._db is not None
        async with self._db.execute(
            "SELECT MIN(created_at) FROM task_memories"
        ) as cur:
            row = await cur.fetchone()
            if row and row[0]:
                return datetime.fromisoformat(row[0])
        return None

    async def get_newest_timestamp(self) -> datetime | None:
        """Return the created_at of the newest task memory."""
        self._ensure_initialized()
        assert self._db is not None
        async with self._db.execute(
            "SELECT MAX(created_at) FROM task_memories"
        ) as cur:
            row = await cur.fetchone()
            if row and row[0]:
                return datetime.fromisoformat(row[0])
        return None

    async def find_duplicates(self, threshold_chars: int = 50) -> list[list[str]]:
        """Find groups of memories with identical content.

        Returns a list of id-lists where each inner list contains two or more
        memory ids sharing the exact same content.  Only groups where the
        content length exceeds *threshold_chars* are returned (to avoid
        flagging trivially short entries).
        """
        self._ensure_initialized()
        assert self._db is not None

        async with self._db.execute(
            """
            SELECT GROUP_CONCAT(id, ','), content, COUNT(*) as cnt
            FROM task_memories
            WHERE LENGTH(content) > ?
            GROUP BY content
            HAVING cnt > 1
            """,
            (threshold_chars,),
        ) as cur:
            rows = await cur.fetchall()

        groups: list[list[str]] = []
        for row in rows:
            ids_str = row[0]
            groups.append(ids_str.split(","))
        return groups

    async def delete_by_ids(self, ids: list[str]) -> int:
        """Delete task memories by their ids. Returns count of deleted rows."""
        self._ensure_initialized()
        assert self._db is not None

        if not ids:
            return 0

        placeholders = ",".join("?" for _ in ids)
        async with self._db.execute(
            f"DELETE FROM task_memories WHERE id IN ({placeholders})",  # noqa: S608
            ids,
        ) as cur:
            deleted = cur.rowcount
        await self._db.commit()
        return deleted

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------

    async def close(self) -> None:
        """Release resources."""
        if self._db is not None:
            await self._db.close()
            self._db = None

        for name, store in self._memvid_stores.items():
            try:
                store.close()
            except Exception:
                logger.debug("Error closing memvid store %s", name, exc_info=True)
        self._memvid_stores.clear()
        self._initialized = False
        logger.info("Task memory closed.")

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _ensure_initialized(self) -> None:
        if not self._initialized:
            raise RuntimeError(
                "TaskMemory has not been initialised. "
                "Call ``await initialize(data_dir)`` first."
            )

    def _init_memvid(self, data_dir: Path) -> None:
        """Create a memvid store per domain."""
        for domain in self._domains:
            store_path = data_dir / f"task_memvid_{domain}"
            store_path.mkdir(parents=True, exist_ok=True)
            self._memvid_stores[domain] = memvid.MemvidEncoder(str(store_path))

    async def _fetch_all(self, sql: str, params: tuple = ()) -> list[tuple]:
        assert self._db is not None
        async with self._db.execute(sql, params) as cursor:
            return await cursor.fetchall()

    def _row_to_result(
        self, row: tuple, relevance: float = 0.0, domain: str = "general"
    ) -> MemoryResult:
        """Convert a raw SQLite row to a ``MemoryResult``."""
        item_id, content, row_domain, meta_json, created_at_str = row[:5]
        try:
            metadata = json.loads(meta_json)
        except (json.JSONDecodeError, TypeError):
            metadata = {}
        created_at = datetime.fromisoformat(created_at_str)

        item = MemoryItem(
            id=item_id,
            content=content,
            memory_type=MemoryType.TASK,
            category=row_domain,
            metadata=metadata,
            created_at=created_at,
        )
        return MemoryResult(
            item=item,
            relevance_score=relevance,
            source=f"task:{row_domain}",
        )

    async def _search_fts(
        self, query: str, domain: str | None, limit: int
    ) -> list[MemoryResult]:
        """Full-text search using SQLite FTS5."""
        assert self._db is not None

        safe_query = _sanitise_fts_query(query)
        if not safe_query:
            # Return most recent if query is empty after sanitisation.
            return await self._get_recent(domain, limit)

        if domain is not None:
            rows = await self._fetch_all(
                """
                SELECT
                    m.id, m.content, m.domain, m.metadata_json, m.created_at,
                    f.rank
                FROM task_fts f
                JOIN task_memories m ON m.rowid = f.rowid
                WHERE task_fts MATCH ? AND m.domain = ?
                ORDER BY f.rank
                LIMIT ?
                """,
                (safe_query, domain, limit),
            )
        else:
            rows = await self._fetch_all(
                """
                SELECT
                    m.id, m.content, m.domain, m.metadata_json, m.created_at,
                    f.rank
                FROM task_fts f
                JOIN task_memories m ON m.rowid = f.rowid
                WHERE task_fts MATCH ?
                ORDER BY f.rank
                LIMIT ?
                """,
                (safe_query, limit),
            )

        results: list[MemoryResult] = []
        for r in rows:
            raw_rank = r[5] if len(r) > 5 else 0.0
            score = _normalise_fts_rank(raw_rank)
            results.append(self._row_to_result(r, relevance=score))
        return results

    async def _search_memvid(
        self, query: str, domain: str | None, limit: int
    ) -> list[MemoryResult]:
        """Vector search via memvid stores."""
        assert self._db is not None

        domains_to_search = [domain] if domain else list(self._memvid_stores.keys())
        all_hits: list[tuple[str, float, str]] = []  # (id, score, domain)

        for d in domains_to_search:
            store = self._memvid_stores.get(d)
            if store is None:
                continue
            try:
                hits = store.search(query, top_k=limit)
                for hit in hits:
                    hit_id = (
                        hit.get("metadata", {}).get("id")
                        if isinstance(hit, dict)
                        else getattr(hit, "metadata", {}).get("id")
                    )
                    hit_score = (
                        hit.get("score", 0.0)
                        if isinstance(hit, dict)
                        else getattr(hit, "score", 0.0)
                    )
                    if hit_id:
                        all_hits.append((hit_id, float(hit_score), d))
            except Exception:
                logger.warning("memvid search failed for domain %s", d, exc_info=True)

        if not all_hits:
            # Fall back to FTS if memvid returned nothing.
            return await self._search_fts(query, domain, limit)

        # Sort by score descending and take top results.
        all_hits.sort(key=lambda h: h[1], reverse=True)
        all_hits = all_hits[:limit]

        results: list[MemoryResult] = []
        for hit_id, hit_score, hit_domain in all_hits:
            rows = await self._fetch_all(
                """
                SELECT id, content, domain, metadata_json, created_at
                FROM task_memories
                WHERE id = ?
                """,
                (hit_id,),
            )
            if rows:
                results.append(
                    self._row_to_result(rows[0], relevance=hit_score, domain=hit_domain)
                )
        return results

    async def _get_recent(
        self, domain: str | None, limit: int
    ) -> list[MemoryResult]:
        """Return most recent task memories, optionally filtered by domain."""
        assert self._db is not None

        if domain is not None:
            rows = await self._fetch_all(
                """
                SELECT id, content, domain, metadata_json, created_at
                FROM task_memories
                WHERE domain = ?
                ORDER BY created_at DESC
                LIMIT ?
                """,
                (domain, limit),
            )
        else:
            rows = await self._fetch_all(
                """
                SELECT id, content, domain, metadata_json, created_at
                FROM task_memories
                ORDER BY created_at DESC
                LIMIT ?
                """,
                (limit,),
            )
        return [self._row_to_result(r) for r in rows]


# ---------------------------------------------------------------------------
# Module-level helpers
# ---------------------------------------------------------------------------


def _sanitise_fts_query(raw: str) -> str:
    """Turn a user-provided string into a safe FTS5 MATCH expression."""
    tokens = raw.split()
    safe_tokens: list[str] = []
    for t in tokens:
        cleaned = t.replace('"', "").strip()
        if cleaned:
            safe_tokens.append(f'"{cleaned}"')
    return " ".join(safe_tokens)


def _normalise_fts_rank(rank: float) -> float:
    """Map an FTS5 rank (negative, lower = better) to ``[0, 1]``."""
    if rank >= 0:
        return 0.0
    return 1.0 / (1.0 - rank)
