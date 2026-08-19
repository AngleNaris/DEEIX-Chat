// Package embeddingutil defines stable identifiers shared by embedding producers and consumers.
package embeddingutil

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// ModelSignature identifies one embedding vector space by normalized model name and output dimensions.
func ModelSignature(model string, outputDimensions int) string {
	raw := strings.TrimSpace(model) + "@" + strconv.Itoa(outputDimensions)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:4]) + "@" + strconv.Itoa(outputDimensions)
}
