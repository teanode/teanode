// Package embeddings provides utility functions for vector embeddings.
package embeddings

import (
	"math"
)

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 if either vector is zero-length or they differ in length.
func CosineSimilarity(vectorA, vectorB []float32) float64 {
	if len(vectorA) != len(vectorB) || len(vectorA) == 0 {
		return 0
	}
	var dotProduct, normA, normB float64
	for index := range vectorA {
		dotProduct += float64(vectorA[index]) * float64(vectorB[index])
		normA += float64(vectorA[index]) * float64(vectorA[index])
		normB += float64(vectorB[index]) * float64(vectorB[index])
	}
	denominator := math.Sqrt(normA) * math.Sqrt(normB)
	if denominator == 0 {
		return 0
	}
	return dotProduct / denominator
}
