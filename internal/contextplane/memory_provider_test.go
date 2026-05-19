package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
)

func buildMemoryMessageForTest(ctx context.Context, counter TokenCounter, budget LayeredMemoryBudget, sessionSummarySection, checkpointSection string, prepared *memorymodule.PrepareResult) (*schema.Message, error) {
	packet, err := buildMemoryContextPacket(ctx, counter, budget, sessionSummarySection, checkpointSection, prepared)
	if err != nil {
		return nil, err
	}
	return buildMemoryMessageFromPacket(packet), nil
}

func TestBuildMemoryContextPacketHonorsTotalBudget(t *testing.T) {
	counter := testTokenCounter(t)
	packet, err := buildMemoryContextPacket(
		context.Background(),
		counter,
		LayeredMemoryBudget{L2InitialTokens: 42},
		"",
		"<working-checkpoint>\ncheckpoint keeps session continuity tight and specific.\n</working-checkpoint>",
		&memorymodule.PrepareResult{Entries: []memorymodule.Entry{
			{Ref: "fact:1", Kind: "fact", Content: "first memory entry with a fairly long detail"},
			{Ref: "fact:2", Kind: "fact", Content: "second memory entry with another fairly long detail"},
			{Ref: "fact:3", Kind: "fact", Content: "third memory entry that should not always fit"},
		}},
	)
	if err != nil {
		t.Fatalf("buildMemoryContextPacket: %v", err)
	}
	if packet == nil {
		t.Fatal("expected non-nil packet")
	}
	content := strings.Join(memoryPacketSectionContent(packet), "\n")
	tokens, err := counter.CountText(context.Background(), content)
	if err != nil {
		t.Fatalf("CountText: %v", err)
	}
	if tokens > 42 {
		t.Fatalf("tokens = %d, want <= 42", tokens)
	}
	if !strings.Contains(content, "fact:1") {
		t.Fatalf("expected first memory entry to fit: %q", content)
	}
	if strings.Contains(content, "fact:3") {
		t.Fatalf("expected later memory entry to be omitted under tight budget: %q", content)
	}
}

func TestBuildMemoryContextPacketKeepsCheckpointWithPreparedMemory(t *testing.T) {
	packet, err := buildMemoryContextPacket(
		context.Background(),
		testTokenCounter(t),
		LayeredMemoryBudget{L2InitialTokens: 30},
		"",
		"<working-checkpoint>\nshort checkpoint\n</working-checkpoint>",
		&memorymodule.PrepareResult{Entries: []memorymodule.Entry{{Ref: "fact:1", Kind: "fact", Content: "targeted recall"}}},
	)
	if err != nil {
		t.Fatalf("buildMemoryContextPacket: %v", err)
	}
	if packet == nil {
		t.Fatal("expected non-nil packet")
	}
	content, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 30}, "", "<working-checkpoint>\nshort checkpoint\n</working-checkpoint>", &memorymodule.PrepareResult{Entries: []memorymodule.Entry{{Ref: "fact:1", Kind: "fact", Content: "targeted recall"}}})
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}
	if content == nil {
		t.Fatal("expected non-nil message")
	}
	if !strings.Contains(content.Content, "<working-checkpoint>") {
		t.Fatalf("checkpoint content missing: %q", content.Content)
	}
	if !strings.Contains(content.Content, "## Memory Entries") {
		t.Fatalf("memory entries content missing: %q", content.Content)
	}
}

func TestFitPreparedMemoryToBudgetOmitsWholeEntries(t *testing.T) {
	content, attachedRefs, err := fitPreparedMemoryToBudget(context.Background(), testTokenCounter(t), &memorymodule.PrepareResult{Entries: []memorymodule.Entry{
		{Ref: "fact:1", Kind: "fact", Content: "one"},
		{Ref: "fact:2", Kind: "fact", Content: "two two two two two two two"},
	}}, 15)
	if err != nil {
		t.Fatalf("fitPreparedMemoryToBudget: %v", err)
	}
	if strings.TrimSpace(content) == "" {
		t.Fatal("expected prepared memory content")
	}
	if !strings.Contains(content, "fact:1") {
		t.Fatalf("expected first fact content to survive: %q", content)
	}
	if strings.Contains(content, "fact:2") {
		t.Fatalf("expected second fact content to be omitted: %q", content)
	}
	if strings.Join(attachedRefs, ",") != "fact:1" {
		t.Fatalf("attached refs = %#v, want fact:1", attachedRefs)
	}
}

func memoryPacketSectionContent(packet *memoryContextPacket) []string {
	if packet == nil {
		return nil
	}
	sections := make([]string, 0, len(packet.Sections))
	for _, section := range packet.Sections {
		sections = append(sections, section.Content)
	}
	return sections
}
