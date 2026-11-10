package vector

// ShardsForTest exposes the shard map for in-package fixtures (hybrid
// two-shard join test). Test-only: never call from production code.
func (m *ShardManager) ShardsForTest() map[ShardType]*VectorShard {
	return m.shards
}
