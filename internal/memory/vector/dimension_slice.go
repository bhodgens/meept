package vector

import (
	"context"
	"fmt"
)

// Supported target dimensions for Matryoshka dimension slicing.
const (
	Dim768 = 768
	Dim512 = 512
	Dim256 = 256
	Dim128 = 128
)

// SliceEmbedding truncates a full-dimensional embedding to a target dimension.
// Implements Matryoshka Representation Learning: simply takes the first N
// dimensions (the most informative components in truncated SVD order).
//
// For example: 768 -> 512 keeps embedding[0:512].
func SliceEmbedding(embedding []float32, targetDim int) ([]float32, error) {
	if targetDim <= 0 {
		return nil, fmt.Errorf("target dimension must be positive: %d", targetDim)
	}
	if targetDim > len(embedding) {
		return nil, fmt.Errorf("target dimension %d exceeds source dimension %d",
			targetDim, len(embedding))
	}
	return embedding[:targetDim], nil
}

// ValidateDimension checks that a target dimension is valid for a given source.
func ValidateDimension(source, target int) error {
	if source <= 0 {
		return fmt.Errorf("source dimension must be positive: %d", source)
	}
	if target <= 0 {
		return fmt.Errorf("target dimension must be positive: %d", target)
	}
	if target > source {
		return fmt.Errorf("target dimension %d exceeds source dimension %d",
			target, source)
	}
	if target == source {
		return nil // no slicing needed
	}
	return nil
}

// EmbeddingWithDimension generates an embedding via the provider and slices it
// to the target dimension. Returns an error if the provider's output dimension
// does not match the expected source size.
//
// This is a convenience wrapper: call GenerateEmbedding, then SliceEmbedding.
func EmbeddingWithDimension(ctx context.Context, provider Provider, text string, targetDim int) ([]float32, error) {
	if err := ValidateDimension(provider.Dimension(), targetDim); err != nil {
		return nil, fmt.Errorf("dimension validation: %w", err)
	}

	embedding, err := provider.GenerateEmbedding(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("generate embedding: %w", err)
	}

	return SliceEmbedding(embedding, targetDim)
}

// SuggestedDimension returns the dimension that best fits the source embedding
// for storage. If source matches a known target, returns it; otherwise returns
// source (no slicing).
func SuggestedDimension(source int) int {
	switch {
	case source >= Dim768:
		return Dim512
	case source >= Dim512:
		return Dim256
	case source >= Dim256:
		return Dim128
	default:
		return source
	}
}
