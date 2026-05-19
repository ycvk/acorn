package contextplane

// CompressionState tracks compression history within a single run so later
// compressions can update the latest sanitized summary incrementally.
type CompressionState struct {
	LastSummary      string
	CompressionCount int
}

// NewCompressionState creates a zero-value CompressionState.
func NewCompressionState() *CompressionState {
	return &CompressionState{}
}

// RecordCompression updates state after a successful compression.
func (s *CompressionState) RecordCompression(summary string) {
	s.LastSummary = summary
	s.CompressionCount++
}
