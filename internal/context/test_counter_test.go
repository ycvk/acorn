package context

import (
	"testing"
)

func testTokenCounter(t *testing.T) TokenCounter {
	t.Helper()
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatalf("NewTokenCounter: %v", err)
	}
	return counter
}
