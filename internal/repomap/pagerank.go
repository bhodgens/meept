// Package repomap provides repository mapping with graph-based symbol ranking.
// It extracts symbol definitions and references via tree-sitter, builds a dependency
// graph, and applies Personalized PageRank to identify the most relevant symbols
// for the current conversation.
package repomap

import (
	"fmt"
	"math"
	"sort"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/multi"
)

// PageRankConfig holds PageRank parameters.
type PageRankConfig struct {
	// Damping is the PageRank damping factor (probability of following a link).
	// Default: 0.85
	Damping float64
	// MaxIterations is the maximum number of PageRank iterations.
	// Default: 100
	MaxIterations int
	// ConvergenceTol is the convergence tolerance for PageRank.
	// Default: 1e-6
	ConvergenceTol float64
	// Personalization is a map of file paths to bias weights for Personalized PageRank.
	Personalization map[string]float64
}

// DefaultPageRankConfig returns a PageRankConfig with default values.
func DefaultPageRankConfig() PageRankConfig {
	return PageRankConfig{
		Damping:       0.85,
		MaxIterations: 100,
		ConvergenceTol: 1e-6,
		Personalization: map[string]float64{
			// Default personalization weights can be set here
		},
	}
}

// ComputeRank applies Personalized PageRank to the given graph and returns
// ranked tags sorted by their PageRank score in descending order.
func ComputeRank(g *RepoGraph, config PageRankConfig) RankedTags {
	if g == nil || len(g.Nodes()) == 0 {
		return nil
	}

	// Use defaults if not specified
	if config.Damping == 0 {
		config.Damping = 0.85
	}
	if config.MaxIterations == 0 {
		config.MaxIterations = 100
	}
	if config.ConvergenceTol == 0 {
		config.ConvergenceTol = 1e-6
	}
	if config.Personalization == nil {
		config.Personalization = make(map[string]float64)
	}

	// Build personalization vector from config
	persMap := buildPersonalizationMap(g, config.Personalization)

	// Run Personalized PageRank
	pagerank := personalizedPageRank(g.Graph(), config.Damping, config.MaxIterations, config.ConvergenceTol, persMap)

	// Redistribute rank across definitions
	rankedDefs := redistributeRank(g, pagerank)

	// Sort by rank (descending)
	sort.Sort(RankedTags(rankedDefs))

	return rankedDefs
}

// personalizedPageRank computes Personalized PageRank on the graph.
// The personalization parameter biases the random walk toward certain nodes.
func personalizedPageRank(g *multi.DirectedGraph, damping float64, maxIter int, tol float64, personalization map[int64]float64) map[int64]float64 {
	nodes := graph.NodesOf(g.Nodes())
	n := len(nodes)
	if n == 0 {
		return nil
	}

	// Create node ID to index mapping
	idToIdx := make(map[int64]int)
	idxToID := make([]int64, n)
	for i, node := range nodes {
		idToIdx[node.ID()] = i
		idxToID[i] = node.ID()
	}

	// Initialize ranks - either from personalization or uniform
	ranks := make([]float64, n)
	teleport := make([]float64, n)

	// Normalize personalization weights
	sum := 0.0
	for _, w := range personalization {
		sum += w
	}

	if sum > 0 {
		for nodeID, w := range personalization {
			if idx, ok := idToIdx[nodeID]; ok {
				teleport[idx] = w / sum
			}
		}
	} else {
		// Uniform teleport if no personalization
		teleportProb := 1.0 / float64(n)
		for i := range teleport {
			teleport[i] = teleportProb
		}
	}

	// Initialize with teleport distribution (uniform or personalized)
	for i := range ranks {
		ranks[i] = teleport[i]
	}

	// Precompute outgoing edges for each node
	// Use graph.From() which returns nodes, then get Edge between them
	outDegree := make([]int, n)
	edgeWeights := make([]map[int]float64, n) // edgeWeights[i][j] = weight from i to j

	for i, node := range nodes {
		outDegree[i] = 0
		edgeWeights[i] = make(map[int]float64)

		// Use graph.From to get destination nodes
		destinations := g.From(node.ID())
		var destNodes []graph.Node
		for destinations.Next() {
			destNodes = append(destNodes, destinations.Node())
		}

		var totalWeight float64
		for _, destNode := range destNodes {
			edge := g.Edge(node.ID(), destNode.ID())
			if edge == nil {
				continue
			}

			var weight float64
			if wl, ok := edge.(interface{ Weight() float64 }); ok {
				weight = wl.Weight()
			} else {
				weight = 1.0
			}

			if weight > 0 {
				toIdx := idToIdx[destNode.ID()]
				edgeWeights[i][toIdx] = weight
				totalWeight += weight
			}
			outDegree[i]++
		}

		// Normalize weights
		if totalWeight > 0 {
			for toIdx := range edgeWeights[i] {
				edgeWeights[i][toIdx] /= totalWeight
			}
		}
	}

	// Identify sink nodes (nodes with no outgoing edges)
	isSink := make([]bool, n)
	for i, od := range outDegree {
		isSink[i] = (od == 0)
	}

	// Handle sinks: spread rank equally to all nodes
	sinkSum := 0.0
	if n > 0 {
		for i := range isSink {
			if isSink[i] {
				sinkSum += ranks[i]
			}
		}
	}
	sinkTeleport := make([]float64, n)
	if n > 0 {
		sinkTeleportProb := sinkSum / float64(n)
		for i := range sinkTeleport {
			sinkTeleport[i] = sinkTeleportProb
		}
	}

	// Power iteration
	newRanks := make([]float64, n)
	alpha := damping

	for iter := 0; iter < maxIter; iter++ {
		// Calculate sink node contribution
		sinkContribution := 0.0
		for i := range isSink {
			if isSink[i] {
				sinkContribution += ranks[i]
			}
		}
		sinkSpread := sinkContribution / float64(n)

		// Calculate new ranks
		for i := range nodes {
			// Base: teleport probability
			newRanks[i] = (1-alpha)*teleport[i] + alpha*sinkSpread

			// Add contributions from incoming edges
			// Need to find nodes that point to this node
			for j := 0; j < n; j++ {
				if weight, ok := edgeWeights[j][i]; ok {
					if outDegree[j] > 0 {
						newRanks[i] += alpha * ranks[j] * weight
					}
				}
			}
		}

		// Check convergence using L1 norm
		diff := 0.0
		for i := range ranks {
			diff += math.Abs(newRanks[i] - ranks[i])
		}

		// Copy new ranks
		copy(ranks, newRanks)

		if diff < tol {
			break
		}
	}

	// Convert back to map keyed by node ID
	result := make(map[int64]float64, n)
	for i, nodeID := range idxToID {
		result[nodeID] = ranks[i]
	}

	return result
}

// buildPersonalizationMap creates the personalization map from file paths to node IDs.
func buildPersonalizationMap(g *RepoGraph, personalization map[string]float64) map[int64]float64 {
	pers := make(map[int64]float64)

	// Apply explicit personalization weights from config
	for filePath, weight := range personalization {
		if node, ok := g.nodes[filePath]; ok {
			pers[node.ID()] = weight
		}
	}

	return pers
}

// AddChatFileBias adds bias to nodes for files actively being discussed in the chat.
func AddChatFileBias(g *RepoGraph, chatFiles []string, bias float64) map[string]float64 {
	pers := make(map[string]float64)

	for _, file := range chatFiles {
		if _, ok := g.nodes[file]; ok {
			pers[file] = bias
		}
	}

	return pers
}

// AddMentionedIdentifierBias adds bias to nodes for files that match mentioned identifiers.
func AddMentionedIdentifierBias(g *RepoGraph, mentionedIdentifiers []string, bias float64) map[string]float64 {
	pers := make(map[string]float64)

	for _, ident := range mentionedIdentifiers {
		for file := range g.nodes {
			if matchesPathComponents(file, ident) {
				pers[file] += bias
			}
		}
	}

	return pers
}

// matchesPathComponents checks if any path component matches the identifier.
// This helps identify files related to a mentioned identifier.
func matchesPathComponents(filePath, identifier string) bool {
	// Check if the identifier appears in any part of the path
	if len(identifier) < 2 {
		return false
	}

	// Only do case-insensitive match if identifier has lowercase (likely not an acronym)
	hasLowercase := false
	for _, c := range identifier {
		if c >= 'a' && c <= 'z' {
			hasLowercase = true
			break
		}
	}

	if hasLowercase {
		// Try exact match first
		if containsPathComponent(filePath, identifier) {
			return true
		}
		// Try without extension
		if containsPathComponentWithoutExt(filePath, identifier) != "" {
			return true
		}
	}

	return false
}

// containsPathComponent checks if a path component exactly matches the identifier.
func containsPathComponent(path, ident string) bool {
	// Check each path component
	components := splitPath(path)
	for _, comp := range components {
		if comp == ident {
			return true
		}
	}
	return false
}

// containsPathComponentWithoutExt checks if path component matches identifier without extension.
func containsPathComponentWithoutExt(path, ident string) string {
	components := splitPath(path)
	for _, comp := range components {
		// Strip extension and compare
		for i := len(comp) - 1; i >= 0; i-- {
			if comp[i] == '.' {
				comp = comp[:i]
				break
			}
		}
		if comp == ident {
			return comp
		}
	}
	return ""
}

// splitPath splits a file path into its components.
func splitPath(path string) []string {
	var components []string
	var current []rune

	for _, c := range path {
		if c == '/' || c == '\\' {
			if len(current) > 0 {
				components = append(components, string(current))
				current = nil
			}
		} else {
			current = append(current, c)
		}
	}

	if len(current) > 0 {
		components = append(components, string(current))
	}

	return components
}

// redistributeRank takes the raw PageRank scores (which are per-node/file) and
// redistributes them across the actual tags (definitions) in each file.
func redistributeRank(g *RepoGraph, pagerank map[int64]float64) RankedTags {
	// Build a map of file to its rank score
	fileRank := make(map[string]float64)
	for filePath, node := range g.nodes {
		fileRank[filePath] = pagerank[node.ID()]
	}

	// Group definitions by file
	defsByFile := make(map[string][]Tag)
	defs := FilterDefinitions(g.tags)
	for _, def := range defs {
		defsByFile[def.RelFname] = append(defsByFile[def.RelFname], def)
	}

	// Redistribute rank to each definition in a file
	var ranked RankedTags

	// Distribute file rank equally among its definitions
	for filePath, defs := range defsByFile {
		fileScore := fileRank[filePath]
		if len(defs) == 0 {
			continue
		}

		// Distribute score evenly among definitions in this file
		defScore := fileScore / float64(len(defs))

		for _, def := range defs {
			ranked = append(ranked, RankedTag{
				Tag:   def,
				Score: defScore,
			})
		}
	}

	// Scale scores to normalize - PageRank values can be very small
	// Scale so that the highest score is 1.0
	if len(ranked) > 0 {
		maxScore := ranked[0].Score
		if maxScore > 0 {
			for i := range ranked {
				ranked[i].Score = ranked[i].Score / maxScore
			}
		}
	}

	return ranked
}

// TopTags returns the top N tags from a ranked list.
func TopTags(ranked RankedTags, n int) RankedTags {
	if n >= len(ranked) {
		return ranked
	}
	return ranked[:n]
}

// FilterByScore returns only tags with a score above the threshold.
func FilterByScore(ranked RankedTags, threshold float64) RankedTags {
	var filtered RankedTags
	for _, rt := range ranked {
		if rt.Score >= threshold {
			filtered = append(filtered, rt)
		}
	}
	return filtered
}

// GetTagsByFile groups ranked tags by their file path.
func GetTagsByFile(ranked RankedTags) map[string]RankedTags {
	result := make(map[string]RankedTags)
	for _, rt := range ranked {
		result[rt.RelFname] = append(result[rt.RelFname], rt)
	}

	// Sort each file's tags by score
	for file := range result {
		sort.Sort(RankedTags(result[file]))
	}

	return result
}

// ScoreDistribution returns statistics about the score distribution.
func ScoreDistribution(ranked RankedTags) (min, max, mean, median float64) {
	if len(ranked) == 0 {
		return 0, 0, 0, 0
	}

	min = ranked[len(ranked)-1].Score
	max = ranked[0].Score

	sum := 0.0
	for _, rt := range ranked {
		sum += rt.Score
	}
	mean = sum / float64(len(ranked))

	// Median
	mid := len(ranked) / 2
	if len(ranked)%2 == 0 {
		median = (ranked[mid-1].Score + ranked[mid].Score) / 2
	} else {
		median = ranked[mid].Score
	}

	return min, max, mean, median
}

// String returns a string representation of RankedTags (useful for debugging).
func (r RankedTags) String() string {
	if len(r) == 0 {
		return "RankedTags{}"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("RankedTags(%d):", len(r)))

	// Show top 10
	maxShow := 10
	if len(r) < maxShow {
		maxShow = len(r)
	}

	for i := 0; i < maxShow; i++ {
		rt := r[i]
		lines = append(lines, fmt.Sprintf("  %.4f %s:%d %s (%s)",
			rt.Score, rt.RelFname, rt.Line, rt.Name, rt.Kind))
	}

	if len(r) > maxShow {
		lines = append(lines, fmt.Sprintf("  ... and %d more", len(r)-maxShow))
	}

	// Score distribution
	min, max, mean, median := ScoreDistribution(r)
	lines = append(lines, fmt.Sprintf("  Distribution: min=%.4f, max=%.4f, mean=%.4f, median=%.4f",
		min, max, mean, median))

	return lines[0] + "\n" + joinLines(lines[1:]...)
}

func joinLines(lines ...string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

// CombinedPersonalization merges multiple personalization maps into one.
// When multiple biases apply to the same node, they are added together.
func CombinedPersonalization(biases ...map[string]float64) map[string]float64 {
	combined := make(map[string]float64)

	for _, bias := range biases {
		for nodeID, weight := range bias {
			combined[nodeID] += weight
		}
	}

	return combined
}

// NormalizePersonalization ensures the personalization vector sums to 1.
func NormalizePersonalization(pers map[string]float64) map[string]float64 {
	if len(pers) == 0 {
		return pers
	}

	sum := 0.0
	for _, w := range pers {
		sum += w
	}

	if sum == 0 {
		return pers
	}

	normalized := make(map[string]float64, len(pers))
	for nodeID, w := range pers {
		normalized[nodeID] = w / sum
	}

	return normalized
}

// InversePageRank computes inverse PageRank - nodes that are least important.
// This can be useful for identifying core/essential files that everything depends on.
func InversePageRank(g *RepoGraph, config PageRankConfig) RankedTags {
	ranked := ComputeRank(g, config)

	// Invert scores
	for i := range ranked {
		if ranked[i].Score > 0 {
			ranked[i].Score = 1.0 / ranked[i].Score
		}
	}

	// Re-sort (now ascending - least important first becomes highest inverse score)
	sort.Sort(sort.Reverse(RankedTags(ranked)))

	return ranked
}

// PageRankWithTeleportation computes PageRank with explicit teleportation
// to a set of sink nodes. This ensures the random walker can "teleport"
// to important pages instead of getting stuck in sink nodes.
func PageRankWithTeleportation(g *RepoGraph, config PageRankConfig, teleportTo []string) RankedTags {
	// Add teleportation bias to specified files
	teleportBias := make(map[string]float64)
	for _, file := range teleportTo {
		teleportBias[file] = 1.0
	}

	// Merge with existing personalization. Initialize the map if the caller
	// passed a zero-value PageRankConfig (Personalization == nil) — writing
	// to a nil map panics in Go.
	if config.Personalization == nil {
		config.Personalization = make(map[string]float64)
	}
	for file, weight := range teleportBias {
		config.Personalization[file] += weight
	}

	return ComputeRank(g, config)
}

// ComputeRankDetailed returns more detailed ranking information.
func ComputeRankDetailed(g *RepoGraph, config PageRankConfig) (RankedTags, map[string]float64, error) {
	if g == nil || len(g.Nodes()) == 0 {
		return nil, nil, nil
	}

	// Use defaults if not specified
	if config.Damping == 0 {
		config.Damping = 0.85
	}
	if config.MaxIterations == 0 {
		config.MaxIterations = 100
	}
	if config.ConvergenceTol == 0 {
		config.ConvergenceTol = 1e-6
	}
	if config.Personalization == nil {
		config.Personalization = make(map[string]float64)
	}

	// Build personalization vector from config
	persMap := buildPersonalizationMap(g, config.Personalization)

	// Run Personalized PageRank
	pagerank := personalizedPageRank(g.Graph(), config.Damping, config.MaxIterations, config.ConvergenceTol, persMap)

	// Convert node IDs to file paths for easier debugging
	fileScores := make(map[string]float64)
	for filePath, node := range g.nodes {
		fileScores[filePath] = pagerank[node.ID()]
	}

	// Redistribute rank across definitions
	rankedDefs := redistributeRank(g, pagerank)

	// Sort by rank (descending)
	sort.Sort(RankedTags(rankedDefs))

	return rankedDefs, fileScores, nil
}

// EccentricityCentrality computes the reciprocal of the graph's largest
// eccentricity (distance to farthest node). This identifies the most
// "central" nodes in terms of reachability.
func EccentricityCentrality(g *RepoGraph) map[string]float64 {
	if g == nil || len(g.Nodes()) == 0 {
		return nil
	}

	result := make(map[string]float64)
	nodes := g.Nodes()

	if len(nodes) == 0 {
		return result
	}

	// Get the underlying gonum graph
	gonumGraph := g.Graph()

	// Simple approach: spread one unit from each node and measure total reception
	// More central nodes receive more "flow"
	flow := make(map[int64]float64)

	for _, fromNode := range nodes {
		fromID := fromNode.ID()
		// Simulate spreading from this node
		visited := make(map[int64]bool)
		queue := []graph.Node{fromNode}
		visited[fromID] = true

		// BFS to spread influence
		steps := 0
		maxSteps := 5 // Limit propagation depth

		for len(queue) > 0 && steps < maxSteps {
			current := queue[0]
			queue = queue[1:]

			// Receive flow at this node
			flow[current.ID()] += 1.0 / math.Pow(2, float64(steps))

			// Visit neighbors
			destinations := gonumGraph.From(current.ID())
			for destinations.Next() {
				toNode := destinations.Node()
				toID := toNode.ID()
				if !visited[toID] {
					visited[toID] = true
					queue = append(queue, toNode)
				}
			}

			steps++
		}
	}

	// Normalize and convert to file paths
	maxFlow := 0.0
	for _, f := range flow {
		if f > maxFlow {
			maxFlow = f
		}
	}

	if maxFlow > 0 {
		for filePath, node := range g.nodes {
			result[filePath] = flow[node.ID()] / maxFlow
		}
	}

	return result
}