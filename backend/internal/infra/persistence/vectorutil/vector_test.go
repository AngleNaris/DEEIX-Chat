package vectorutil

import (
	"strings"
	"testing"
)

func TestAlignForStoragePadsWithoutChangingValues(t *testing.T) {
	input := []float32{1, -2, 3.5}
	result, err := AlignForStorage(input)
	if err != nil {
		t.Fatalf("AlignForStorage() error = %v", err)
	}
	if len(result) != StorageDimensions {
		t.Fatalf("aligned dimensions = %d, want %d", len(result), StorageDimensions)
	}
	for index, value := range input {
		if result[index] != value {
			t.Fatalf("aligned value[%d] = %v, want %v", index, result[index], value)
		}
	}
	if result[len(input)] != 0 || result[len(result)-1] != 0 {
		t.Fatal("expected padded dimensions to be zero")
	}
}

func TestAlignForStorageRejectsOversizedInput(t *testing.T) {
	input := make([]float32, StorageDimensions+1)
	if _, err := AlignForStorage(input); err == nil {
		t.Fatal("expected oversized vector to be rejected")
	}
}

func TestPostgresLiteralUsesPhysicalDimensions(t *testing.T) {
	literal, err := PostgresLiteral([]float32{1, 2})
	if err != nil {
		t.Fatalf("PostgresLiteral() error = %v", err)
	}
	if strings.Count(literal, ",") != StorageDimensions-1 {
		t.Fatalf("literal dimensions = %d, want %d", strings.Count(literal, ",")+1, StorageDimensions)
	}
	if !strings.HasPrefix(literal, "[1,2,0,") || !strings.HasSuffix(literal, ",0]") {
		t.Fatalf("unexpected literal boundary: %.16s ... %s", literal, literal[len(literal)-8:])
	}
}

func TestCandidateLimitNeverDropsRequestedResults(t *testing.T) {
	for _, topK := range []int{1, 100, 1001} {
		if limit := CandidateLimit(topK); limit < topK {
			t.Fatalf("CandidateLimit(%d) = %d", topK, limit)
		}
	}
}
