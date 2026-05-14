// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// EntityCreateTool creates a knowledge graph entity/node.
// In Meept's architecture, entities are represented by memory IDs,
// so this tool ensures a node exists in the knowledge graph.
type EntityCreateTool struct {
	graph *memory.KnowledgeGraph
}

// NewEntityCreateTool creates a new entity create tool.
func NewEntityCreateTool(graph *memory.KnowledgeGraph) *EntityCreateTool {
	return &EntityCreateTool{graph: graph}
}

func (t *EntityCreateTool) Name() string { return "entity_create" }

func (t *EntityCreateTool) Description() string {
	return "Create a knowledge graph entity (node) representing a concept, person, or relationship. Entities are automatically created when storing memories, but this tool can explicitly ensure an entity exists before linking it."
}

func (t *EntityCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropEntityID: {
				Type:        schemaTypeString,
				Description: "Unique identifier for the entity (will be used as memory_id in the graph).",
			},
			"entity_type": {
				Type:        schemaTypeString,
				Description: "Type of entity (e.g., 'person', 'concept', 'task', 'decision', 'project'). Stored as metadata.",
			},
			"properties": {
				Type:        "object",
				Description: "Additional properties to store with the entity (e.g., {'name': 'Alice', 'role': 'developer'}).",
			},
		},
		Required: []string{"entity_id", "entity_type"},
	}
}

// EntityCreateResult contains the result of entity creation.
type EntityCreateResult struct {
	Success    bool   `json:"success"`
	EntityID   string `json:"entity_id"`
	EntityType string `json:"entity_type"`
	Created    bool   `json:"created"`
	Message    string `json:"message"`
}

func (t *EntityCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return nil, fmt.Errorf("knowledge graph not configured")
	}

	entityID, _ := args["entity_id"].(string)
	if entityID == "" {
		return nil, fmt.Errorf("entity_id is required")
	}

	entityType, _ := args["entity_type"].(string)
	if entityType == "" {
		return nil, fmt.Errorf("entity_type is required")
	}

	// Extract properties as metadata
	var metadata map[string]any
	if props, ok := args["properties"].(map[string]any); ok {
		metadata = props
	} else {
		metadata = make(map[string]any)
	}
	metadata["entity_type"] = entityType

	// In Meept's architecture, entities are memory IDs.
	// Ensure the node exists in the PageRank table.
	err := t.graph.EnsureNode(ctx, entityID)
	if err != nil {
		return EntityCreateResult{
			Success:    false,
			EntityID:   entityID,
			EntityType: entityType,
			Created:    false,
			Message:    fmt.Sprintf("failed to create entity: %v", err),
		}, nil
	}

	return EntityCreateResult{
		Success:    true,
		EntityID:   entityID,
		EntityType: entityType,
		Created:    true,
		Message:    "entity created successfully",
	}, nil
}

// EntityLinkTool links two entities with a relation in the knowledge graph.
type EntityLinkTool struct {
	graph *memory.KnowledgeGraph
}

// NewEntityLinkTool creates a new entity link tool.
func NewEntityLinkTool(graph *memory.KnowledgeGraph) *EntityLinkTool {
	return &EntityLinkTool{graph: graph}
}

func (t *EntityLinkTool) Name() string { return "entity_link" }

func (t *EntityLinkTool) Description() string {
	return "Create a relationship (edge) between two entities in the knowledge graph. This establishes how concepts, people, or memories are connected."
}

func (t *EntityLinkTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"source_id": {
				Type:        schemaTypeString,
				Description: "ID of the source entity (the entity the relationship originates from).",
			},
			"target_id": {
				Type:        schemaTypeString,
				Description: "ID of the target entity (the entity the relationship points to).",
			},
			"relation_type": {
				Type:        schemaTypeString,
				Description: "Type of relationship: 'reference' (one references another), 'similar' (semantic similarity), 'temporal' (same time/session), 'co_accessed' (accessed together), 'causal' (one led to another).",
				Enum:        []string{schemaPropReference, "similar", "temporal", "co_accessed", "causal"},
			},
			"weight": {
				Type:        schemaTypeNumber,
				Description: "Strength of the relationship from 0.0 to 1.0 (default 0.5). Higher values indicate stronger connections.",
			},
			"metadata": {
				Type:        "object",
				Description: "Optional metadata to attach to the edge (e.g., context about why this relationship exists).",
			},
		},
		Required: []string{"source_id", "target_id", "relation_type"},
	}
}

// EntityLinkResult contains the result of entity linking.
type EntityLinkResult struct {
	Success      bool    `json:"success"`
	SourceID     string  `json:"source_id"`
	TargetID     string  `json:"target_id"`
	RelationType string  `json:"relation_type"`
	Weight       float64 `json:"weight"`
	Message      string  `json:"message"`
}

func (t *EntityLinkTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return nil, fmt.Errorf("knowledge graph not configured")
	}

	sourceID, _ := args["source_id"].(string)
	if sourceID == "" {
		return nil, fmt.Errorf("source_id is required")
	}

	targetID, _ := args["target_id"].(string)
	if targetID == "" {
		return nil, fmt.Errorf("target_id is required")
	}

	relationTypeStr, _ := args["relation_type"].(string)
	if relationTypeStr == "" {
		relationTypeStr = schemaPropReference
	}

	// Map string to EdgeType
	var edgeType memory.EdgeType
	switch relationTypeStr {
	case schemaPropReference:
		edgeType = memory.EdgeTypeReference
	case "similar":
		edgeType = memory.EdgeTypeSimilar
	case "temporal":
		edgeType = memory.EdgeTypeTemporal
	case "co_accessed":
		edgeType = memory.EdgeTypeCoAccessed
	case "causal":
		edgeType = memory.EdgeTypeCausal
	default:
		return nil, fmt.Errorf("invalid relation_type: %s", relationTypeStr)
	}

	// Parse weight
	weight := 0.5
	if w, ok := args["weight"].(float64); ok && w > 0 && w <= 1 {
		weight = w
	}

	// Extract metadata
	var metadata map[string]any
	if meta, ok := args["metadata"].(map[string]any); ok {
		metadata = meta
	} else {
		metadata = make(map[string]any)
	}

	// Ensure both nodes exist
	if err := t.graph.EnsureNode(ctx, sourceID); err != nil {
		return nil, fmt.Errorf("failed to ensure source node: %w", err)
	}
	if err := t.graph.EnsureNode(ctx, targetID); err != nil {
		return nil, fmt.Errorf("failed to ensure target node: %w", err)
	}

	// Create the edge
	edge := memory.MemoryEdge{
		SourceID:   sourceID,
		TargetID:   targetID,
		EdgeType:   edgeType,
		Weight:     weight,
		Confidence: 1.0,
		Metadata:   metadata,
	}

	if err := t.graph.AddEdge(ctx, edge); err != nil {
		return EntityLinkResult{
			Success:      false,
			SourceID:     sourceID,
			TargetID:     targetID,
			RelationType: relationTypeStr,
			Weight:       weight,
			Message:      fmt.Sprintf("failed to create link: %v", err),
		}, nil
	}

	return EntityLinkResult{
		Success:      true,
		SourceID:     sourceID,
		TargetID:     targetID,
		RelationType: relationTypeStr,
		Weight:       weight,
		Message:      "entities linked successfully",
	}, nil
}

// EntityQueryTool queries the knowledge graph for related entities.
type EntityQueryTool struct {
	graph *memory.KnowledgeGraph
}

// NewEntityQueryTool creates a new entity query tool.
func NewEntityQueryTool(graph *memory.KnowledgeGraph) *EntityQueryTool {
	return &EntityQueryTool{graph: graph}
}

func (t *EntityQueryTool) Name() string { return "entity_query" }

func (t *EntityQueryTool) Description() string {
	return "Query the knowledge graph to find related entities. Returns connected memories/entities with their relationship types and PageRank importance scores."
}

func (t *EntityQueryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropEntityID: {
				Type:        schemaTypeString,
				Description: "ID of the entity to query for related entities.",
			},
			"relation_type": {
				Type:        schemaTypeString,
				Description: "Optional: filter by specific relation type ('reference', 'similar', 'temporal', 'co_accessed', 'causal'). If not specified, returns all relations.",
				Enum:        []string{schemaPropReference, "similar", "temporal", "co_accessed", "causal", ""},
			},
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum number of related entities to return (default 20, max 100).",
			},
			"include_pagerank": {
				Type:        schemaTypeBoolean,
				Description: "Whether to include PageRank scores (default true).",
			},
		},
		Required: []string{"entity_id"},
	}
}

// EntityQueryResult contains the result of entity query.
type EntityQueryResult struct {
	Success  bool            `json:"success"`
	EntityID string          `json:"entity_id"`
	Count    int             `json:"count"`
	Related  []RelatedEntity `json:"related"`
	Message  string          `json:"message,omitempty"`
}

// RelatedEntity represents a connected entity in the graph.
type RelatedEntity struct {
	ID           string  `json:"id"`
	RelationType string  `json:"relation_type"`
	Direction    string  `json:"direction"` // "outgoing" or "incoming"
	Weight       float64 `json:"weight"`
	Confidence   float64 `json:"confidence"`
	PageRank     float64 `json:"page_rank,omitempty"`
	CommunityID  string  `json:"community_id,omitempty"`
}

func (t *EntityQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return nil, fmt.Errorf("knowledge graph not configured")
	}

	entityID, _ := args["entity_id"].(string)
	if entityID == "" {
		return nil, fmt.Errorf("entity_id is required")
	}

	// Parse limit
	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 100)
	}

	// Parse relation type filter
	relationTypeFilter := ""
	if rt, ok := args["relation_type"].(string); ok && rt != "" {
		relationTypeFilter = rt
	}

	// Parse include_pagerank
	includePageRank := true
	if ipr, ok := args["include_pagerank"].(bool); ok {
		includePageRank = ipr
	}

	// Get edges for the entity
	edges, err := t.graph.GetEdges(ctx, entityID)
	if err != nil {
		return EntityQueryResult{
			Success:  false,
			EntityID: entityID,
			Count:    0,
			Related:  nil,
			Message:  fmt.Sprintf("failed to query graph: %v", err),
		}, nil
	}

	// Build results
	related := make([]RelatedEntity, 0, len(edges))
	for _, edge := range edges {
		// Filter by relation type if specified
		if relationTypeFilter != "" && string(edge.EdgeType) != relationTypeFilter {
			continue
		}

		// Determine direction and related ID
		var direction, relatedID string
		if edge.SourceID == entityID {
			direction = "outgoing"
			relatedID = edge.TargetID
		} else {
			direction = "incoming"
			relatedID = edge.SourceID
		}

		re := RelatedEntity{
			ID:           relatedID,
			RelationType: string(edge.EdgeType),
			Direction:    direction,
			Weight:       edge.Weight,
			Confidence:   edge.Confidence,
		}

		// Optionally fetch PageRank
		if includePageRank {
			if pr, err := t.graph.GetPageRank(ctx, relatedID); err == nil {
				re.PageRank = pr
			}
			// Fetch community
			if comm, err := t.graph.GetCommunity(ctx, relatedID); err == nil {
				re.CommunityID = comm
			}
		}

		related = append(related, re)

		// Stop if we've reached the limit
		if len(related) >= limit {
			break
		}
	}

	return EntityQueryResult{
		Success:  true,
		EntityID: entityID,
		Count:    len(related),
		Related:  related,
		Message:  fmt.Sprintf("found %d related entities", len(related)),
	}, nil
}

// GraphStatsTool retrieves statistics about the knowledge graph.
type GraphStatsTool struct {
	graph *memory.KnowledgeGraph
}

// NewGraphStatsTool creates a new graph stats tool.
func NewGraphStatsTool(graph *memory.KnowledgeGraph) *GraphStatsTool {
	return &GraphStatsTool{graph: graph}
}

func (t *GraphStatsTool) Name() string { return "graph_stats" }

func (t *GraphStatsTool) Description() string {
	return "Get statistics about the knowledge graph including node count, edge count, average degree, and community information. Useful for understanding the structure and connectivity of stored memories."
}

func (t *GraphStatsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

// GraphStatsResult contains knowledge graph statistics.
type GraphStatsResult struct {
	Success        bool    `json:"success"`
	NodeCount      int     `json:"node_count"`
	EdgeCount      int     `json:"edge_count"`
	AvgDegree      float64 `json:"avg_degree"`
	CommunityCount int     `json:"community_count"`
	LastUpdated    string  `json:"last_updated,omitempty"`
	Message        string  `json:"message,omitempty"`
}

func (t *GraphStatsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return GraphStatsResult{
			Success: false,
			Message: "knowledge graph not configured",
		}, nil
	}

	stats, err := t.graph.GetStats(ctx)
	if err != nil {
		return GraphStatsResult{
			Success: false,
			Message: fmt.Sprintf("failed to get graph stats: %v", err),
		}, nil
	}

	result := GraphStatsResult{
		Success:        true,
		NodeCount:      stats.NodeCount,
		EdgeCount:      stats.EdgeCount,
		AvgDegree:      stats.AvgDegree,
		CommunityCount: stats.CommunityCount,
	}
	if !stats.LastUpdated.IsZero() {
		result.LastUpdated = stats.LastUpdated.Format("2006-01-02T15:04:05Z07:00")
	}

	return result, nil
}

// ComputePageRankTool recomputes PageRank scores for all nodes in the graph.
type ComputePageRankTool struct {
	graph *memory.KnowledgeGraph
}

// NewComputePageRankTool creates a new compute PageRank tool.
func NewComputePageRankTool(graph *memory.KnowledgeGraph) *ComputePageRankTool {
	return &ComputePageRankTool{graph: graph}
}

func (t *ComputePageRankTool) Name() string { return "compute_pagerank" }

func (t *ComputePageRankTool) Description() string {
	return "Recompute PageRank scores for all entities in the knowledge graph. PageRank measures importance based on connectivity. Run this after adding many edges to update importance scores."
}

func (t *ComputePageRankTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

// ComputePageRankResult contains the result of PageRank computation.
type ComputePageRankResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (t *ComputePageRankTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return ComputePageRankResult{
			Success: false,
			Message: "knowledge graph not configured",
		}, nil
	}

	if err := t.graph.ComputePageRank(ctx); err != nil {
		return ComputePageRankResult{
			Success: false,
			Message: fmt.Sprintf("failed to compute PageRank: %v", err),
		}, nil
	}

	return ComputePageRankResult{
		Success: true,
		Message: "PageRank computed successfully",
	}, nil
}

// DetectCommunitiesTool runs community detection on the knowledge graph.
type DetectCommunitiesTool struct {
	graph *memory.KnowledgeGraph
}

// NewDetectCommunitiesTool creates a new detect communities tool.
func NewDetectCommunitiesTool(graph *memory.KnowledgeGraph) *DetectCommunitiesTool {
	return &DetectCommunitiesTool{graph: graph}
}

func (t *DetectCommunitiesTool) Name() string { return "detect_communities" }

func (t *DetectCommunitiesTool) Description() string {
	return "Run community detection on the knowledge graph to find clusters of related entities. Communities represent groups of tightly connected memories/concepts."
}

func (t *DetectCommunitiesTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"return_mapping": {
				Type:        schemaTypeBoolean,
				Description: "If true, returns the full entity_id -> community_id mapping (can be large). Default false.",
			},
		},
		Required: []string{},
	}
}

// DetectCommunitiesResult contains the result of community detection.
type DetectCommunitiesResult struct {
	Success        bool              `json:"success"`
	CommunityCount int               `json:"community_count"`
	Mapping        map[string]string `json:"mapping,omitempty"`
	Message        string            `json:"message,omitempty"`
}

func (t *DetectCommunitiesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return DetectCommunitiesResult{
			Success: false,
			Message: "knowledge graph not configured",
		}, nil
	}

	communities, err := t.graph.DetectCommunities(ctx)
	if err != nil {
		return DetectCommunitiesResult{
			Success: false,
			Message: fmt.Sprintf("failed to detect communities: %v", err),
		}, nil
	}

	communityCount := len(uniqueValues(communities))
	result := DetectCommunitiesResult{
		Success:        true,
		CommunityCount: communityCount,
		Message:        fmt.Sprintf("detected %d communities", communityCount),
	}

	// Only return mapping if requested
	if returnMapping, ok := args["return_mapping"].(bool); ok && returnMapping {
		result.Mapping = communities
	}

	return result, nil
}

// uniqueValues returns the unique values from a map.
func uniqueValues(m map[string]string) []string {
	seen := make(map[string]bool)
	for _, v := range m {
		seen[v] = true
	}
	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	return result
}

// GetCommunitySiblingsTool finds other entities in the same community.
type GetCommunitySiblingsTool struct {
	graph *memory.KnowledgeGraph
}

// NewGetCommunitySiblingsTool creates a new community siblings tool.
func NewGetCommunitySiblingsTool(graph *memory.KnowledgeGraph) *GetCommunitySiblingsTool {
	return &GetCommunitySiblingsTool{graph: graph}
}

func (t *GetCommunitySiblingsTool) Name() string { return "community_siblings" }

func (t *GetCommunitySiblingsTool) Description() string {
	return "Find other entities that belong to the same community as the given entity. Useful for discovering related memories that are clustered together by the graph structure."
}

func (t *GetCommunitySiblingsTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropEntityID: {
				Type:        schemaTypeString,
				Description: "ID of the entity to find community siblings for.",
			},
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum number of siblings to return (default 20, max 100).",
			},
		},
		Required: []string{"entity_id"},
	}
}

// CommunitySiblingsResult contains community siblings query result.
type CommunitySiblingsResult struct {
	Success      bool     `json:"success"`
	EntityID     string   `json:"entity_id"`
	CommunityID  string   `json:"community_id"`
	SiblingCount int      `json:"sibling_count"`
	Siblings     []string `json:"siblings"`
	Message      string   `json:"message,omitempty"`
}

func (t *GetCommunitySiblingsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.graph == nil {
		return nil, fmt.Errorf("knowledge graph not configured")
	}

	entityID, _ := args["entity_id"].(string)
	if entityID == "" {
		return nil, fmt.Errorf("entity_id is required")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 100)
	}

	// Get the entity's community
	communityID, err := t.graph.GetCommunity(ctx, entityID)
	if err != nil {
		return CommunitySiblingsResult{
			Success:      false,
			EntityID:     entityID,
			SiblingCount: 0,
			Siblings:     nil,
			Message:      fmt.Sprintf("failed to get community: %v", err),
		}, nil
	}

	if communityID == "" {
		return CommunitySiblingsResult{
			Success:      true,
			EntityID:     entityID,
			CommunityID:  "",
			SiblingCount: 0,
			Siblings:     []string{},
			Message:      "entity not assigned to any community",
		}, nil
	}

	// Get siblings
	siblings, err := t.graph.GetCommunitySiblings(ctx, entityID, limit)
	if err != nil {
		return CommunitySiblingsResult{
			Success:      false,
			EntityID:     entityID,
			CommunityID:  communityID,
			SiblingCount: 0,
			Siblings:     nil,
			Message:      fmt.Sprintf("failed to get siblings: %v", err),
		}, nil
	}

	return CommunitySiblingsResult{
		Success:      true,
		EntityID:     entityID,
		CommunityID:  communityID,
		SiblingCount: len(siblings),
		Siblings:     siblings,
		Message:      fmt.Sprintf("found %d entities in community %s", len(siblings), communityID),
	}, nil
}

// Ensure all tools implement the Tool interface
var (
	_ tools.Tool = (*EntityCreateTool)(nil)
	_ tools.Tool = (*EntityLinkTool)(nil)
	_ tools.Tool = (*EntityQueryTool)(nil)
	_ tools.Tool = (*GraphStatsTool)(nil)
	_ tools.Tool = (*ComputePageRankTool)(nil)
	_ tools.Tool = (*DetectCommunitiesTool)(nil)
	_ tools.Tool = (*GetCommunitySiblingsTool)(nil)
)
