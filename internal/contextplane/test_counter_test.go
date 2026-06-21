package contextplane

import (
	"testing"
)

func testTokenCounter(t *testing.T) *CompressionTokenCounter {
	t.Helper()
	counter, err := NewCompressionTokenCounter()
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	return counter
}
