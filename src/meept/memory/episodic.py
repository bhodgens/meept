"""Episodic memory subsystem -- conversation and interaction history.

Attempts to use ``memu`` for vector-backed episodic storage.  When ``memu``
is not installed the module falls back to a fully-functional SQLite + FTS5
implementation so the rest of meept keeps working without external
dependencies.
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
# Probe for memu availability
# ---------------------------------------------------------------------------

_MEMU_AVAILABLE: bool
try:
    import memu  # type: ignore[import-untyped]

    _MEMU_AVAILABLE = True
    logger.debug("memu library detected -- using memu for episodic memory.")
except ImportError:
    _MEMU_AVAILABLE = False
    logger.debug("memu not installed -- episodic memory will use SQLite/FTS5 fallback.")


# ---------------------------------------------------------------------------
# SQLite FTS5 fallback
# ---------------------------------------------------------------------------

_CREATE_TABLE_SQL = """\
CREATE TABLE IF NOT EXISTS episodic_memories (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'conversation',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL,
    embedding_text TEXT NOT NULL DEFAULT ''
);
"""

_CREATE_FTS_SQL = """\
CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts
USING fts5(content, category, embedding_text, content='episodic_memories', content_rowid='rowid');
"""

# Triggers that keep the FTS index in sync with the content table.
_TRIGGER_INSERT = """\
CREATE TRIGGER IF NOT EXISTS episodic_fts_ai AFTER INSERT ON episodic_memories BEGIN
    INSERT INTO episodic_fts(rowid, content, category, embedding_text)
    VALUES (new.rowid, new.content, new.category, new.embedding_text);
END;
"""

_TRIGGER_DELETE = """\
CREATE TRIGGER IF NOT EXISTS episodic_fts_ad AFTER DELETE ON episodic_memories BEGIN
    INSERT INTO episodic_fts(episodic_fts, rowid, content, category, embedding_text)
    VALUES ('delete', old.rowid, old.content, old.category, old.embedding_text);
END;
"""

_TRIGGER_UPDATE = """\
CREATE TRIGGER IF NOT EXISTS episodic_fts_au AFTER UPDATE ON episodic_memories BEGIN
    INSERT INTO episodic_fts(episodic_fts, rowid, content, category, embedding_text)
    VALUES ('delete', old.rowid, old.content, old.category, old.embedding_text);
    INSERT INTO episodic_fts(rowid, content, category, embedding_text)
    VALUES (new.rowid, new.content, new.category, new.embedding_text);
END;
"""


# ---------------------------------------------------------------------------
# Public class
# ---------------------------------------------------------------------------


class EpisodicMemory:
    """Stores and retrieves episodic (conversation) memories.

    If the ``memu`` library is installed it is used for vector-similarity
    search.  Otherwise an SQLite database with FTS5 full-text search provides
    equivalent functionality with keyword matching instead of embeddings.
    """

    def __init__(self) -> None:
        self._db: aiosqlite.Connection | None = None
        self._data_dir: Path | None = None
        self._use_memu: bool = False
        self._memu_store: Any = None
        self._llm_client: Any = None
        self._initialized: bool = False

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def initialize(self, data_dir: Path, llm_client: Any = None) -> None:
        """Set up the episodic memory backend.

        Parameters
        ----------
        data_dir:
            Directory in which database files are stored.
        llm_client:
            Optional :class:`~meept.llm.client.LLMClient` used for smart
            retrieval when memu is not available.
        """
        self._data_dir = data_dir
        self._llm_client = llm_client
        data_dir.mkdir(parents=True, exist_ok=True)

        if _MEMU_AVAILABLE:
            try:
                self._memu_store = memu.MemoryStore(str(data_dir / "episodic_memu"))
                self._use_memu = True
                logger.info("Episodic memory initialised with memu backend.")
            except Exception:
                logger.warning(
                    "Failed to initialise memu store -- falling back to SQLite.",
                    exc_info=True,
                )
                self._use_memu = False

        # Always open the SQLite database -- it serves as fallback *and* as
        # the canonical metadata / category / archive store even when memu is
        # the primary search backend.
        db_path = data_dir / "episodic.db"
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
            "Episodic memory ready (backend=%s, db=%s).",
            "memu" if self._use_memu else "sqlite/fts5",
            db_path,
        )

    # ------------------------------------------------------------------
    # Store
    # ------------------------------------------------------------------

    async def store(
        self,
        content: str,
        category: str = "conversation",
        metadata: dict | None = None,
    ) -> str:
        """Persist a new episodic memory.

        Returns the unique id of the stored item.
        """
        self._ensure_initialized()
        assert self._db is not None

        item_id = uuid.uuid4().hex
        now_iso = datetime.now(timezone.utc).isoformat()
        meta_json = json.dumps(metadata or {})
        # embedding_text combines content + category for better FTS coverage.
        embedding_text = f"{category}: {content}"

        await self._db.execute(
            """
            INSERT INTO episodic_memories (id, content, category, metadata_json, created_at, embedding_text)
            VALUES (?, ?, ?, ?, ?, ?)
            """,
            (item_id, content, category, meta_json, now_iso, embedding_text),
        )
        try:
            await self._db.commit()
        except Exception:
            logger.error("Failed to commit episodic memory %s", item_id, exc_info=True)
            raise

        # Also index in memu when available.
        if self._use_memu and self._memu_store is not None:
            try:
                self._memu_store.add(
                    item_id,
                    content,
                    metadata={"category": category, **(metadata or {})},
                )
            except Exception:
                logger.warning("memu indexing failed for %s", item_id, exc_info=True)

        logger.debug("Stored episodic memory %s (category=%s).", item_id, category)
        return item_id

    # ------------------------------------------------------------------
    # Search
    # ------------------------------------------------------------------

    async def search(self, query: str, limit: int = 10) -> list[MemoryResult]:
        """Search episodic memories by relevance.

        Uses memu vector search when available, otherwise falls back to FTS5.
        """
        self._ensure_initialized()

        if self._use_memu and self._memu_store is not None:
            return await self._search_memu(query, limit)
        return await self._search_fts(query, limit)

    async def get_recent(self, limit: int = 20) -> list[MemoryResult]:
        """Retrieve the most recent episodic memories by creation time."""
        self._ensure_initialized()
        assert self._db is not None

        rows = await self._fetch_all(
            """
            SELECT id, content, category, metadata_json, created_at
            FROM episodic_memories
            ORDER BY created_at DESC
            LIMIT ?
            """,
            (limit,),
        )
        return [self._row_to_result(r, relevance=0.0) for r in rows]

    async def get_by_category(
        self, category: str, limit: int = 20
    ) -> list[MemoryResult]:
        """Retrieve memories filtered to a specific category."""
        self._ensure_initialized()
        assert self._db is not None

        rows = await self._fetch_all(
            """
            SELECT id, content, category, metadata_json, created_at
            FROM episodic_memories
            WHERE category = ?
            ORDER BY created_at DESC
            LIMIT ?
            """,
            (category, limit),
        )
        return [self._row_to_result(r, relevance=0.0) for r in rows]

    # ------------------------------------------------------------------
    # Stats helper (used by consolidation)
    # ------------------------------------------------------------------

    async def count(self) -> int:
        """Return total number of episodic memories."""
        self._ensure_initialized()
        assert self._db is not None
        async with self._db.execute("SELECT COUNT(*) FROM episodic_memories") as cur:
            row = await cur.fetchone()
            return row[0] if row else 0

    async def get_oldest_timestamp(self) -> datetime | None:
        """Return the created_at of the oldest memory, or ``None``."""
        self._ensure_initialized()
        assert self._db is not None
        async with self._db.execute(
            "SELECT MIN(created_at) FROM episodic_memories"
        ) as cur:
            row = await cur.fetchone()
            if row and row[0]:
                return datetime.fromisoformat(row[0])
        return None

    async def get_newest_timestamp(self) -> datetime | None:
        """Return the created_at of the newest memory, or ``None``."""
        self._ensure_initialized()
        assert self._db is not None
        async with self._db.execute(
            "SELECT MAX(created_at) FROM episodic_memories"
        ) as cur:
            row = await cur.fetchone()
            if row and row[0]:
                return datetime.fromisoformat(row[0])
        return None

    async def get_old_memories(
        self, older_than: datetime, limit: int = 200
    ) -> list[MemoryResult]:
        """Return memories created before *older_than* (for consolidation)."""
        self._ensure_initialized()
        assert self._db is not None

        rows = await self._fetch_all(
            """
            SELECT id, content, category, metadata_json, created_at
            FROM episodic_memories
            WHERE created_at < ?
            ORDER BY created_at ASC
            LIMIT ?
            """,
            (older_than.isoformat(), limit),
        )
        return [self._row_to_result(r, relevance=0.0) for r in rows]

    async def delete_by_ids(self, ids: list[str]) -> int:
        """Delete memories by their ids. Returns count of deleted rows."""
        self._ensure_initialized()
        assert self._db is not None

        if not ids:
            return 0

        placeholders = ",".join("?" for _ in ids)
        async with self._db.execute(
            f"DELETE FROM episodic_memories WHERE id IN ({placeholders})",  # noqa: S608
            ids,
        ) as cur:
            deleted = cur.rowcount
        try:
            await self._db.commit()
        except Exception:
            logger.error("Failed to commit deletion of %d episodic memories", len(ids), exc_info=True)
            raise

        # Also remove from memu index.
        if self._use_memu and self._memu_store is not None:
            for item_id in ids:
                try:
                    self._memu_store.remove(item_id)
                except Exception:
                    pass

        return deleted

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------

    async def close(self) -> None:
        """Release resources."""
        if self._db is not None:
            await self._db.close()
            self._db = None
        if self._use_memu and self._memu_store is not None:
            try:
                self._memu_store.close()
            except Exception:
                pass
            self._memu_store = None
        self._initialized = False
        logger.info("Episodic memory closed.")

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _ensure_initialized(self) -> None:
        if not self._initialized:
            raise RuntimeError(
                "EpisodicMemory has not been initialised. "
                "Call ``await initialize(data_dir)`` first."
            )

    async def _fetch_all(
        self, sql: str, params: tuple = ()
    ) -> list[tuple]:
        """Execute a query and return all rows."""
        assert self._db is not None
        async with self._db.execute(sql, params) as cursor:
            return await cursor.fetchall()

    def _row_to_result(
        self, row: tuple, relevance: float = 0.0
    ) -> MemoryResult:
        """Convert a raw SQLite row to a ``MemoryResult``."""
        item_id, content, category, meta_json, created_at_str = row[:5]
        try:
            metadata = json.loads(meta_json)
        except (json.JSONDecodeError, TypeError):
            metadata = {}
        created_at = datetime.fromisoformat(created_at_str)

        item = MemoryItem(
            id=item_id,
            content=content,
            memory_type=MemoryType.EPISODIC,
            category=category,
            metadata=metadata,
            created_at=created_at,
        )
        return MemoryResult(item=item, relevance_score=relevance, source="episodic")

    async def _search_fts(self, query: str, limit: int) -> list[MemoryResult]:
        """Full-text search using SQLite FTS5."""
        assert self._db is not None

        # FTS5 match query -- escape double-quotes in the user query to avoid
        # syntax errors, and wrap each token in quotes for a safe prefix search.
        safe_query = _sanitise_fts_query(query)
        if not safe_query:
            # If the query sanitises to nothing, fall back to recent memories.
            return await self.get_recent(limit)

        rows = await self._fetch_all(
            """
            SELECT
                m.id, m.content, m.category, m.metadata_json, m.created_at,
                rank
            FROM episodic_fts f
            JOIN episodic_memories m ON m.rowid = f.rowid
            WHERE episodic_fts MATCH ?
            ORDER BY rank
            LIMIT ?
            """,
            (safe_query, limit),
        )

        results: list[MemoryResult] = []
        for r in rows:
            # FTS5 rank is negative (lower = better); normalise to [0, 1].
            raw_rank = r[5] if len(r) > 5 else 0.0
            score = _normalise_fts_rank(raw_rank)
            results.append(self._row_to_result(r, relevance=score))
        return results

    async def _search_memu(self, query: str, limit: int) -> list[MemoryResult]:
        """Vector-similarity search via memu."""
        assert self._memu_store is not None
        assert self._db is not None

        try:
            hits = self._memu_store.search(query, top_k=limit)
        except Exception:
            logger.warning("memu search failed -- falling back to FTS.", exc_info=True)
            return await self._search_fts(query, limit)

        results: list[MemoryResult] = []
        for hit in hits:
            # memu returns objects with .id, .score, .metadata
            hit_id = getattr(hit, "id", None) or (hit.get("id") if isinstance(hit, dict) else None)
            hit_score = getattr(hit, "score", 0.0) if not isinstance(hit, dict) else hit.get("score", 0.0)

            if hit_id is None:
                continue

            # Fetch full row from SQLite for metadata.
            rows = await self._fetch_all(
                """
                SELECT id, content, category, metadata_json, created_at
                FROM episodic_memories
                WHERE id = ?
                """,
                (str(hit_id),),
            )
            if rows:
                results.append(self._row_to_result(rows[0], relevance=float(hit_score)))
        return results


# ---------------------------------------------------------------------------
# Module-level helpers
# ---------------------------------------------------------------------------


def _sanitise_fts_query(raw: str) -> str:
    """Turn a user-provided string into a safe FTS5 MATCH expression.

    Each whitespace-separated token is double-quoted so special FTS5
    characters (``*``, ``:``, ``(``, etc.) are treated as literals.
    Tokens are joined with implicit AND.
    """
    tokens = raw.split()
    safe_tokens: list[str] = []
    for t in tokens:
        # Remove characters that break FTS5 even inside double-quotes.
        cleaned = t.replace('"', "").strip()
        if cleaned:
            safe_tokens.append(f'"{cleaned}"')
    return " ".join(safe_tokens)


def _normalise_fts_rank(rank: float) -> float:
    """Map an FTS5 ``rank`` value to ``[0.0, 1.0]``.

    FTS5 rank values are negative (more negative = better match).  We map
    via ``score = 1 / (1 + abs(rank))`` so that more-negative ranks produce
    higher scores (closer to 1.0).
    """
    if rank >= 0:
        return 0.0
    return 1.0 / (1.0 + abs(rank))
