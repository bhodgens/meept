# memory

Memory subsystem configuration including backend selection, consolidation, episodic/task/personality memory types, embeddings, security, caching, limits, expiration, and versioning.


## Example

```toml
[memory] backend = "memvid"
```


## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| Backend | MemoryBackend | Backend specifies the storage backend: "memvid" (default) or "sqlite" |  |
| DataDir | string |  |  |
| ConsolidationIntervalHours | int |  |  |
| Episodic | EpisodicConfig |  |  |
| Task | TaskMemoryConfig |  |  |
| Personality | PersonalityConfig |  |  |
| Embeddings | EmbeddingConfig |  |  |
| Security | MemorySecurityConfig | Security holds memory security settings |  |
| Caching | MemoryCachingConfig | Caching holds memory prefix caching settings |  |
| Limits | MemoryLimitsConfig | Limits holds character limit settings for memory categories |  |
| Expiration | MemoryExpirationConfig | Expiration holds memory expiration settings |  |
| Versioning | MemoryVersioningConfig | Versioning holds versioned memory settings |  |
| ProjectOverrides | map[string]MemoryLimitsConfig | ProjectOverrides allows per-project character limit overrides |  |

