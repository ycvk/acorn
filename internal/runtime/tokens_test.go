package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

func TestCountMessagesBillsImagesAtAFixedEstimate(t *testing.T) {
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	text := schema.UserAgenticMessage("看看这张图")
	plain, err := counter.CountMessages(ctx, []adk.AgenticMessage{text}, nil)
	if err != nil {
		t.Fatal(err)
	}
	withImage := schema.UserAgenticMessage("看看这张图")
	withImage.ContentBlocks = append(withImage.ContentBlocks, schema.NewContentBlock(&schema.UserInputImage{Base64Data: strings.Repeat("A", 1<<20), MIMEType: "image/png"}))
	got, err := counter.CountMessages(ctx, []adk.AgenticMessage{withImage}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain+core.ImageInputTokens {
		t.Fatalf("tokens = %d, want %d text + %d image", got, plain, core.ImageInputTokens)
	}
}
