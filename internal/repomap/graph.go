// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/multi"
)

// Edge weight multipliers for adjusting graph edge weights based on various heuristics.
const (
	// UserMentionMultiplier is applied when an identifier is explicitly mentioned in the chat.
	UserMentionMultiplier = 10.0
	// ChatFileMultiplier is applied when the file is actively being discussed in conversation.
	ChatFileMultiplier = 50.0
	// CompoundIdentifierBonus is applied for snake_case/camelCase identifiers with length >= 8.
	CompoundIdentifierBonus = 10.0
	// PrivateIdentifierPenalty is applied to identifiers starting with underscore.
	PrivateIdentifierPenalty = 0.1
	// GenericIdentifierPenalty is applied to identifiers defined in more than 5 files.
	GenericIdentifierPenalty = 0.1
)

// RepoGraph wraps gonum's MultiDiGraph with repo-specific functionality.
type RepoGraph struct {
	g     *multi.DirectedGraph
	nodes map[string]graph.Node // file path → node
	edges map[string]float64    // edge key (from→to) → weight
	tags  []Tag                 // underlying tags for lookups
}

// nodeID is used to generate unique node IDs atomically across concurrent BuildGraph calls.
var nodeID int64

// NewRepoGraph creates a new empty RepoGraph.
func NewRepoGraph() *RepoGraph {
	return &RepoGraph{
		g:     multi.NewDirectedGraph(),
		nodes: make(map[string]graph.Node),
		edges: make(map[string]float64),
		tags:  nil,
	}
}

// getOrCreateNode returns an existing node for a file path, or creates a new one.
func (g *RepoGraph) getOrCreateNode(filePath string) graph.Node {
	if node, ok := g.nodes[filePath]; ok {
		return node
	}

	// Atomic increment-and-fetch prevents duplicate IDs under concurrent repomap builds.
	id := atomic.AddInt64(&nodeID, 1) - 1
	node := &fileNode{id: id, filePath: filePath}
	g.nodes[filePath] = node
	g.g.AddNode(node)
	return node
}

// addEdge adds a weighted edge between two nodes (files).
// Edge direction: referencing file → defining file
func (g *RepoGraph) addEdge(fromFile, toFile string, weight float64) {
	fromNode := g.getOrCreateNode(fromFile)
	toNode := g.getOrCreateNode(toFile)

	// Skip self-loops
	if fromFile == toFile {
		return
	}

	edgeKey := fmt.Sprintf("%s→%s", fromFile, toFile)

	// Check if edge already exists - keep the higher weight
	if existingWeight, ok := g.edges[edgeKey]; ok {
		if existingWeight >= weight {
			return // Keep existing higher weight edge
		}
	}

	// Store the weight in our edges map
	g.edges[edgeKey] = weight

	// Create line in gonum multi graph using SetLine
	line := newWeightedLine(fromNode, toNode, weight)
	g.g.SetLine(line)
}

// Graph returns the underlying gonum directed graph.
func (g *RepoGraph) Graph() *multi.DirectedGraph {
	return g.g
}

// Nodes returns all nodes in the graph.
func (g *RepoGraph) Nodes() []graph.Node {
	return graph.NodesOf(g.g.Nodes())
}

// EdgeWeight returns the weight of an edge between two files.
func (g *RepoGraph) EdgeWeight(fromFile, toFile string) float64 {
	edgeKey := fmt.Sprintf("%s→%s", fromFile, toFile)
	return g.edges[edgeKey]
}

// fileNode represents a node in the dependency graph (backing a file).
type fileNode struct {
	id       int64
	filePath string
}

// ID implements graph.Node.
func (n *fileNode) ID() int64 {
	return n.id
}

// weightedLine represents a weighted line (edge) in the multi graph.
// It implements both graph.Line and multi.WeightedLine interfaces.
type weightedLine struct {
	from   graph.Node
	to     graph.Node
	weight float64
	IDVal  int64
}

// newWeightedLine creates a new weighted line with a unique ID.
func newWeightedLine(from, to graph.Node, weight float64) *weightedLine {
	// Pack two int64 node IDs into a single unique ID using integer arithmetic.
	// This requires node IDs to stay well below 1e9 (1,000,000,000) to avoid overflow.
	return &weightedLine{
		from:   from,
		to:     to,
		weight: weight,
		IDVal:  from.ID()*int64(1e9) + to.ID(), // Generate unique ID
	}
}

// From implements graph.Line.
func (e *weightedLine) From() graph.Node {
	return e.from
}

// To implements graph.Line.
func (e *weightedLine) To() graph.Node {
	return e.to
}

// Weight implements multi.WeightedLine.
func (e *weightedLine) Weight() float64 {
	return e.weight
}

// ID implements multi.Line.
func (e *weightedLine) ID() int64 {
	return e.IDVal
}

// ReversedLine implements graph.Line.
func (e *weightedLine) ReversedLine() graph.Line {
	return &weightedLine{
		from:   e.to,
		to:     e.from,
		weight: e.weight,
		IDVal:  e.to.ID()*1e9 + e.from.ID(),
	}
}

// BuildGraph constructs the dependency graph from tags.
// It creates nodes for each file and adds weighted edges based on references.
func BuildGraph(tags []Tag, chatFiles []string, mentionedIdentifiers []string) *RepoGraph {
	g := NewRepoGraph()
	g.tags = tags

	// Build a map of definitions by name for quick lookup
	definitionsByName := buildDefinitionIndex(tags)

	// Step 1: Create nodes for each file that has tags
	filesWithTags := make(map[string]bool)
	for _, tag := range tags {
		filesWithTags[tag.RelFname] = true
	}

	for file := range filesWithTags {
		g.getOrCreateNode(file)
	}

	// Step 2: Add weighted edges for references
	// Edge direction: referencing file → defining file
	references := FilterReferences(tags)
	for _, ref := range references {
		// Find definitions matching this reference
		defs := definitionsByName[ref.Name]
		for _, def := range defs {
			if def.RelFname == ref.RelFname {
				continue // Skip same-file references for now
			}
			weight := calculateEdgeWeight(ref, def, chatFiles, mentionedIdentifiers, tags)
			g.addEdge(ref.RelFname, def.RelFname, weight)
		}
	}

	return g
}

// buildDefinitionIndex creates a map of definition name to list of definition tags.
func buildDefinitionIndex(tags []Tag) map[string][]Tag {
	index := make(map[string][]Tag)
	defs := FilterDefinitions(tags)
	for _, def := range defs {
		index[def.Name] = append(index[def.Name], def)
	}
	return index
}

// calculateEdgeWeight applies all weight heuristics.
// It calculates the edge weight based on the reference, definition, and context.
func calculateEdgeWeight(ref, def Tag, chatFiles, mentionedIdentifiers []string, allTags []Tag) float64 {
	weight := 1.0

	// Check if identifier is mentioned in the conversation
	if contains(mentionedIdentifiers, ref.Name) {
		weight *= UserMentionMultiplier
	}

	// Check if reference file is in chat (actively being discussed)
	if contains(chatFiles, ref.RelFname) {
		weight *= ChatFileMultiplier
	}

	// Compound identifier bonus: snake_case/camelCase with length >= 8
	if isCompoundIdentifier(ref.Name) && len(ref.Name) >= 8 {
		weight *= CompoundIdentifierBonus
	}

	// Private identifier penalty: starts with underscore
	if strings.HasPrefix(ref.Name, "_") {
		weight *= PrivateIdentifierPenalty
	}

	// Generic name penalty: defined in many files (>5)
	defCount := countDefinitionsForName(allTags, ref.Name)
	if defCount > 5 {
		weight *= GenericIdentifierPenalty
	}

	// Frequency scaling: sqrt(count) to prevent domination by common identifiers
	frequency := float64(countReferencesForName(allTags, ref.Name))
	if frequency > 1 {
		weight = weight * math.Sqrt(frequency)
	}

	return weight
}

// contains checks if a string slice contains a specific value.
func contains(slice []string, value string) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}
	return false
}

// isCompoundIdentifier checks if an identifier uses compound naming (snake_case or camelCase).
func isCompoundIdentifier(name string) bool {
	// Check for snake_case (contains underscore)
	if strings.Contains(name, "_") {
		return true
	}

	// Check for camelCase (has uppercase letters after lowercase)
	hasUpperAfterLower := false
	for i := 1; i < len(name); i++ {
		if name[i] >= 'A' && name[i] <= 'Z' {
			if name[i-1] >= 'a' && name[i-1] <= 'z' {
				hasUpperAfterLower = true
				break
			}
		}
	}
	return hasUpperAfterLower
}

// countDefinitionsForName counts how many files define a given identifier.
func countDefinitionsForName(tags []Tag, name string) int {
	count := 0
	seen := make(map[string]bool)
	defs := FilterDefinitions(tags)
	for _, def := range defs {
		if def.Name == name {
			if !seen[def.RelFname] {
				seen[def.RelFname] = true
				count++
			}
		}
	}
	return count
}

// countReferencesForName counts total references to a given identifier.
func countReferencesForName(tags []Tag, name string) int {
	count := 0
	refs := FilterReferences(tags)
	for _, ref := range refs {
		if ref.Name == name {
			count++
		}
	}
	return count
}

// RankedTag represents a tag with its computed PageRank score.
type RankedTag struct {
	Tag
	Score float64
}

// RankedTags is a slice of RankedTag sorted by score (descending).
type RankedTags []RankedTag

// Len implements sort.Interface.
func (r RankedTags) Len() int {
	return len(r)
}

// Less implements sort.Interface.
func (r RankedTags) Less(i, j int) bool {
	return r[i].Score > r[j].Score // Descending order
}

// Swap implements sort.Interface.
func (r RankedTags) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

// unexportedRankedTags is an internal version that allows sorting without allocating new slices.
type unexportedRankedTags []RankedTag

func (r unexportedRankedTags) Len() int           { return len(r) }
func (r unexportedRankedTags) Less(i, j int) bool { return r[i].Score > r[j].Score }
func (r unexportedRankedTags) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }

// SortRankedTags sorts a slice of RankedTag by score in descending order.
func SortRankedTags(tags []RankedTag) {
	sort.Sort(unexportedRankedTags(tags))
}