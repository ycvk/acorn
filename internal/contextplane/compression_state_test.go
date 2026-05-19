package contextplane

import "testing"

func TestNewCompressionState(t *testing.T) {
	s := NewCompressionState()
	if s.CompressionCount != 0 {
		t.Errorf("CompressionCount = %d, want 0", s.CompressionCount)
	}
	if s.LastSummary != "" {
		t.Errorf("LastSummary = %q, want empty", s.LastSummary)
	}
}

func TestRecordCompressionStoresLatestSummary(t *testing.T) {
	s := NewCompressionState()
	s.RecordCompression("summary one")
	s.RecordCompression("summary two")

	if s.LastSummary != "summary two" {
		t.Fatalf("LastSummary = %q, want latest summary", s.LastSummary)
	}
	if s.CompressionCount != 2 {
		t.Fatalf("CompressionCount = %d, want 2", s.CompressionCount)
	}
}
