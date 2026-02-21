#!/usr/bin/env python3
"""
Meept Memvid Service

A FastAPI service that wraps the memvid library to provide HTTP access
to memvid's video-based memory storage for the meept daemon.
"""

import argparse
import asyncio
import hashlib
import logging
import os
import sys
import time
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import uvicorn
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger("meept-memvid")

# Try to import memvid
try:
    from memvid import MemvidEncoder, MemvidRetriever
    MEMVID_AVAILABLE = True
except ImportError:
    logger.warning("memvid not installed, running in mock mode")
    MEMVID_AVAILABLE = False


# --- Models ---

class Memory(BaseModel):
    """A stored memory item."""
    id: str
    content: str
    zone: str
    metadata: dict[str, Any] = Field(default_factory=dict)
    created_at: datetime


class MemoryResult(BaseModel):
    """A memory returned from search with relevance score."""
    memory: Memory
    relevance_score: float


class StoreRequest(BaseModel):
    """Request to store a memory."""
    content: str
    zone: str = "default"
    metadata: dict[str, Any] = Field(default_factory=dict)


class StoreResponse(BaseModel):
    """Response from storing a memory."""
    id: str
    success: bool
    error: str | None = None


class SearchRequest(BaseModel):
    """Request to search memories."""
    query: str
    zone: str = ""
    limit: int = 10


class SearchResponse(BaseModel):
    """Response from searching memories."""
    results: list[MemoryResult]
    total: int
    error: str | None = None


class GetRequest(BaseModel):
    """Request to get memories by IDs."""
    ids: list[str]
    zone: str = ""


class GetResponse(BaseModel):
    """Response from getting memories."""
    memories: list[Memory]
    error: str | None = None


class DeleteRequest(BaseModel):
    """Request to delete a memory."""
    id: str
    zone: str = ""


class DeleteResponse(BaseModel):
    """Response from deleting a memory."""
    success: bool
    error: str | None = None


class HealthResponse(BaseModel):
    """Health check response."""
    status: str
    zones: int
    memories: int
    disk_usage_bytes: int


# --- Storage Backend ---

class MemvidBackend:
    """Backend that uses memvid for storage."""

    def __init__(self, data_dir: Path):
        self.data_dir = data_dir
        self.data_dir.mkdir(parents=True, exist_ok=True)
        self.zones: dict[str, tuple[MemvidEncoder, MemvidRetriever | None]] = {}
        self._lock = asyncio.Lock()
        self._memory_index: dict[str, dict[str, Any]] = {}  # id -> {content, zone, metadata, created_at}
        self._load_existing_zones()

    def _load_existing_zones(self):
        """Load existing .mv2 files on startup."""
        for mv2_file in self.data_dir.glob("*.mv2"):
            zone_name = mv2_file.stem
            try:
                retriever = MemvidRetriever()
                retriever.load_index(str(mv2_file), str(mv2_file.with_suffix(".json")))
                # Create encoder for writes
                encoder = MemvidEncoder()
                self.zones[zone_name] = (encoder, retriever)
                logger.info(f"Loaded existing zone: {zone_name}")
            except Exception as e:
                logger.error(f"Failed to load zone {zone_name}: {e}")

    def _get_zone_path(self, zone: str) -> tuple[Path, Path]:
        """Get .mv2 and .json paths for a zone."""
        safe_name = zone.replace(":", "_").replace("/", "_")
        return (
            self.data_dir / f"{safe_name}.mv2",
            self.data_dir / f"{safe_name}.json",
        )

    async def _ensure_zone(self, zone: str) -> tuple[MemvidEncoder, MemvidRetriever | None]:
        """Ensure a zone exists, creating if needed."""
        async with self._lock:
            if zone not in self.zones:
                mv2_path, json_path = self._get_zone_path(zone)
                encoder = MemvidEncoder()
                retriever = None
                if mv2_path.exists():
                    retriever = MemvidRetriever()
                    retriever.load_index(str(mv2_path), str(json_path))
                self.zones[zone] = (encoder, retriever)
                logger.info(f"Created zone: {zone}")
            return self.zones[zone]

    def _generate_id(self, content: str, zone: str) -> str:
        """Generate a unique ID for a memory."""
        data = f"{content}{zone}{time.time_ns()}"
        return hashlib.sha256(data.encode()).hexdigest()[:16]

    async def store(self, content: str, zone: str, metadata: dict[str, Any]) -> str:
        """Store content in a zone."""
        encoder, retriever = await self._ensure_zone(zone)

        # Generate ID
        memory_id = self._generate_id(content, zone)

        # Add to encoder
        encoder.add_text(content)

        # Store in index
        self._memory_index[memory_id] = {
            "content": content,
            "zone": zone,
            "metadata": metadata,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }

        # Build/rebuild the video for this zone
        mv2_path, json_path = self._get_zone_path(zone)
        encoder.build_video(str(mv2_path), str(json_path))

        # Reload retriever
        retriever = MemvidRetriever()
        retriever.load_index(str(mv2_path), str(json_path))
        async with self._lock:
            self.zones[zone] = (encoder, retriever)

        logger.info(f"Stored memory {memory_id} in zone {zone}")
        return memory_id

    async def search(self, query: str, zone: str, limit: int) -> list[MemoryResult]:
        """Search for memories."""
        results = []

        zones_to_search = [zone] if zone else list(self.zones.keys())

        for z in zones_to_search:
            if z not in self.zones:
                continue

            _, retriever = self.zones[z]
            if retriever is None:
                continue

            try:
                # Search using memvid retriever
                matches = retriever.search(query, top_k=limit)
                for text, score in matches:
                    # Find memory ID by content
                    memory_id = None
                    for mid, data in self._memory_index.items():
                        if data["content"] == text and data["zone"] == z:
                            memory_id = mid
                            break

                    if memory_id is None:
                        # Create a temporary ID for memories not in index
                        memory_id = self._generate_id(text, z)

                    memory = Memory(
                        id=memory_id,
                        content=text,
                        zone=z,
                        metadata=self._memory_index.get(memory_id, {}).get("metadata", {}),
                        created_at=datetime.fromisoformat(
                            self._memory_index.get(memory_id, {}).get(
                                "created_at", datetime.now(timezone.utc).isoformat()
                            )
                        ),
                    )
                    results.append(MemoryResult(memory=memory, relevance_score=score))
            except Exception as e:
                logger.error(f"Search failed in zone {z}: {e}")

        # Sort by relevance and limit
        results.sort(key=lambda r: r.relevance_score, reverse=True)
        return results[:limit]

    async def get_by_ids(self, ids: list[str], zone: str) -> list[Memory]:
        """Get memories by their IDs."""
        memories = []
        for memory_id in ids:
            if memory_id in self._memory_index:
                data = self._memory_index[memory_id]
                if zone and data["zone"] != zone:
                    continue
                memories.append(Memory(
                    id=memory_id,
                    content=data["content"],
                    zone=data["zone"],
                    metadata=data.get("metadata", {}),
                    created_at=datetime.fromisoformat(data["created_at"]),
                ))
        return memories

    async def delete(self, memory_id: str, zone: str) -> bool:
        """Delete a memory by ID."""
        if memory_id in self._memory_index:
            data = self._memory_index[memory_id]
            if zone and data["zone"] != zone:
                return False
            del self._memory_index[memory_id]
            logger.info(f"Deleted memory {memory_id}")
            return True
        return False

    def get_stats(self) -> tuple[int, int, int]:
        """Get storage statistics."""
        total_size = sum(
            f.stat().st_size
            for f in self.data_dir.glob("*")
            if f.is_file()
        )
        return len(self.zones), len(self._memory_index), total_size


class MockBackend:
    """Mock backend for when memvid is not available."""

    def __init__(self, data_dir: Path):
        self.data_dir = data_dir
        self.data_dir.mkdir(parents=True, exist_ok=True)
        self._memories: dict[str, dict[str, Any]] = {}
        self._zones: set[str] = set()

    def _generate_id(self, content: str, zone: str) -> str:
        data = f"{content}{zone}{time.time_ns()}"
        return hashlib.sha256(data.encode()).hexdigest()[:16]

    async def store(self, content: str, zone: str, metadata: dict[str, Any]) -> str:
        memory_id = self._generate_id(content, zone)
        self._memories[memory_id] = {
            "content": content,
            "zone": zone,
            "metadata": metadata,
            "created_at": datetime.now(timezone.utc).isoformat(),
        }
        self._zones.add(zone)
        logger.info(f"[MOCK] Stored memory {memory_id} in zone {zone}")
        return memory_id

    async def search(self, query: str, zone: str, limit: int) -> list[MemoryResult]:
        results = []
        query_lower = query.lower()

        for memory_id, data in self._memories.items():
            if zone and data["zone"] != zone:
                continue

            # Simple substring matching for mock
            content_lower = data["content"].lower()
            if query_lower in content_lower:
                score = len(query_lower) / len(content_lower) if content_lower else 0
                memory = Memory(
                    id=memory_id,
                    content=data["content"],
                    zone=data["zone"],
                    metadata=data.get("metadata", {}),
                    created_at=datetime.fromisoformat(data["created_at"]),
                )
                results.append(MemoryResult(memory=memory, relevance_score=score))

        results.sort(key=lambda r: r.relevance_score, reverse=True)
        return results[:limit]

    async def get_by_ids(self, ids: list[str], zone: str) -> list[Memory]:
        memories = []
        for memory_id in ids:
            if memory_id in self._memories:
                data = self._memories[memory_id]
                if zone and data["zone"] != zone:
                    continue
                memories.append(Memory(
                    id=memory_id,
                    content=data["content"],
                    zone=data["zone"],
                    metadata=data.get("metadata", {}),
                    created_at=datetime.fromisoformat(data["created_at"]),
                ))
        return memories

    async def delete(self, memory_id: str, zone: str) -> bool:
        if memory_id in self._memories:
            data = self._memories[memory_id]
            if zone and data["zone"] != zone:
                return False
            del self._memories[memory_id]
            return True
        return False

    def get_stats(self) -> tuple[int, int, int]:
        return len(self._zones), len(self._memories), 0


# --- Application ---

# Global backend instance
backend: MemvidBackend | MockBackend | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan manager."""
    global backend

    data_dir = Path(os.environ.get("MEEPT_MEMVID_DATA_DIR", "~/.meept/memvid")).expanduser()

    if MEMVID_AVAILABLE:
        backend = MemvidBackend(data_dir)
        logger.info(f"Initialized memvid backend at {data_dir}")
    else:
        backend = MockBackend(data_dir)
        logger.warning(f"Initialized MOCK backend at {data_dir} (memvid not available)")

    yield

    logger.info("Shutting down memvid service")


app = FastAPI(
    title="Meept Memvid Service",
    description="HTTP API for memvid memory storage",
    version="0.1.0",
    lifespan=lifespan,
)


@app.get("/health", response_model=HealthResponse)
async def health():
    """Health check endpoint."""
    if backend is None:
        raise HTTPException(status_code=503, detail="Backend not initialized")

    zones, memories, disk_usage = backend.get_stats()
    return HealthResponse(
        status="ok",
        zones=zones,
        memories=memories,
        disk_usage_bytes=disk_usage,
    )


@app.post("/store", response_model=StoreResponse)
async def store(request: StoreRequest):
    """Store a memory."""
    if backend is None:
        return StoreResponse(id="", success=False, error="Backend not initialized")

    try:
        memory_id = await backend.store(
            content=request.content,
            zone=request.zone,
            metadata=request.metadata,
        )
        return StoreResponse(id=memory_id, success=True)
    except Exception as e:
        logger.error(f"Store failed: {e}")
        return StoreResponse(id="", success=False, error=str(e))


@app.post("/search", response_model=SearchResponse)
async def search(request: SearchRequest):
    """Search memories."""
    if backend is None:
        return SearchResponse(results=[], total=0, error="Backend not initialized")

    try:
        results = await backend.search(
            query=request.query,
            zone=request.zone,
            limit=request.limit,
        )
        return SearchResponse(results=results, total=len(results))
    except Exception as e:
        logger.error(f"Search failed: {e}")
        return SearchResponse(results=[], total=0, error=str(e))


@app.post("/get", response_model=GetResponse)
async def get_memories(request: GetRequest):
    """Get memories by IDs."""
    if backend is None:
        return GetResponse(memories=[], error="Backend not initialized")

    try:
        memories = await backend.get_by_ids(
            ids=request.ids,
            zone=request.zone,
        )
        return GetResponse(memories=memories)
    except Exception as e:
        logger.error(f"Get failed: {e}")
        return GetResponse(memories=[], error=str(e))


@app.post("/delete", response_model=DeleteResponse)
async def delete_memory(request: DeleteRequest):
    """Delete a memory."""
    if backend is None:
        return DeleteResponse(success=False, error="Backend not initialized")

    try:
        success = await backend.delete(
            memory_id=request.id,
            zone=request.zone,
        )
        return DeleteResponse(success=success)
    except Exception as e:
        logger.error(f"Delete failed: {e}")
        return DeleteResponse(success=False, error=str(e))


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Meept Memvid Service")
    parser.add_argument(
        "--host",
        default=os.environ.get("MEEPT_MEMVID_HOST", "127.0.0.1"),
        help="Host to bind to",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=int(os.environ.get("MEEPT_MEMVID_PORT", "8765")),
        help="Port to bind to",
    )
    parser.add_argument(
        "--data-dir",
        default=os.environ.get("MEEPT_MEMVID_DATA_DIR", "~/.meept/memvid"),
        help="Data directory for memvid files",
    )
    parser.add_argument(
        "--log-level",
        default="INFO",
        choices=["DEBUG", "INFO", "WARNING", "ERROR"],
        help="Log level",
    )

    args = parser.parse_args()

    # Set environment variable for data dir
    os.environ["MEEPT_MEMVID_DATA_DIR"] = args.data_dir

    # Configure logging
    logging.getLogger().setLevel(getattr(logging, args.log_level))

    logger.info(f"Starting Meept Memvid Service on {args.host}:{args.port}")
    logger.info(f"Data directory: {args.data_dir}")
    logger.info(f"Memvid available: {MEMVID_AVAILABLE}")

    uvicorn.run(
        app,
        host=args.host,
        port=args.port,
        log_level=args.log_level.lower(),
    )


if __name__ == "__main__":
    main()
