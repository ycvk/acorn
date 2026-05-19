package contextplane

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func testTokenCounter(t *testing.T) *CompressionTokenCounter {
	t.Helper()
	counter, err := NewCompressionTokenCounter(config.ContextPolicy{TokenEncoding: "o200k_base"})
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	return counter
}
