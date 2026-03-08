package embeddings

import (
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	vector := []float64{1.0, 2.0, 3.0}
	similarity := CosineSimilarity(vector, vector)
	if math.Abs(similarity-1.0) > 1e-6 {
		t.Errorf("identical vectors should have similarity 1.0, got %f", similarity)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	vectorA := []float64{1.0, 0.0}
	vectorB := []float64{0.0, 1.0}
	similarity := CosineSimilarity(vectorA, vectorB)
	if math.Abs(similarity) > 1e-6 {
		t.Errorf("orthogonal vectors should have similarity 0.0, got %f", similarity)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	vectorA := []float64{1.0, 0.0}
	vectorB := []float64{-1.0, 0.0}
	similarity := CosineSimilarity(vectorA, vectorB)
	if math.Abs(similarity+1.0) > 1e-6 {
		t.Errorf("opposite vectors should have similarity -1.0, got %f", similarity)
	}
}

func TestCosineSimilarityMismatchedLength(t *testing.T) {
	vectorA := []float64{1.0, 2.0}
	vectorB := []float64{1.0, 2.0, 3.0}
	similarity := CosineSimilarity(vectorA, vectorB)
	if similarity != 0 {
		t.Errorf("mismatched length should return 0, got %f", similarity)
	}
}

func TestCosineSimilarityEmpty(t *testing.T) {
	similarity := CosineSimilarity([]float64{}, []float64{})
	if similarity != 0 {
		t.Errorf("empty vectors should return 0, got %f", similarity)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	vectorA := []float64{0.0, 0.0}
	vectorB := []float64{1.0, 2.0}
	similarity := CosineSimilarity(vectorA, vectorB)
	if similarity != 0 {
		t.Errorf("zero vector should return 0, got %f", similarity)
	}
}
