// Package vectorutil defines the physical vector representation shared by
// PostgreSQL and SQLite persistence implementations.
package vectorutil

import (
	"fmt"
	"strconv"
	"strings"
)

// StorageDimensions is the fixed physical vector width used by every vector
// store. Configured embedding dimensions up to this limit are zero-padded on
// persistence; zero-padding preserves cosine similarity.
const (
	StorageDimensions = 4096
	// IndexDimensions fits pgvector's halfvec HNSW limit while retaining all but
	// the final 96 components of a maximum-width vector for candidate recall.
	IndexDimensions = 4000
)

// CandidateLimit returns a bounded ANN candidate set for exact full-vector reranking.
func CandidateLimit(topK int) int {
	const (
		minimum    = 100
		maximum    = 1000
		multiplier = 10
	)
	limit := topK * multiplier
	if limit < minimum {
		return minimum
	}
	if limit > maximum {
		if topK > maximum {
			return topK
		}
		return maximum
	}
	return limit
}

// AlignForStorage returns a vector with the physical storage width.
func AlignForStorage(input []float32) ([]float32, error) {
	if len(input) == 0 || len(input) == StorageDimensions {
		return input, nil
	}
	if len(input) > StorageDimensions {
		return nil, fmt.Errorf("embedding dimensions %d exceed supported maximum %d", len(input), StorageDimensions)
	}
	result := make([]float32, StorageDimensions)
	copy(result, input)
	return result, nil
}

// PostgresLiteral serializes a vector after aligning it to the physical width.
func PostgresLiteral(input []float32) (string, error) {
	aligned, err := AlignForStorage(input)
	if err != nil {
		return "", err
	}
	if len(aligned) == 0 {
		return "[]", nil
	}
	var builder strings.Builder
	builder.Grow(len(aligned) * 4)
	builder.WriteByte('[')
	for index, value := range aligned {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'f', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String(), nil
}
