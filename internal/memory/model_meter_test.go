package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type compactionFixture struct {
	einomodel.AgenticModel
	fail bool
}

func (m *compactionFixture) Generate(_ context.Context, _ []*schema.AgenticMessage, _ ...einomodel.Option) (*schema.AgenticMessage, error) {
	if m.fail {
		return nil, errors.New("provider failure")
	}
	reply := &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: "summary"}}}}
	reply.ResponseMeta = &schema.AgenticResponseMeta{TokenUsage: &schema.TokenUsage{PromptTokens: 21, CompletionTokens: 8}}
	return reply, nil
}
func TestCompactionAttemptsShareRunUsage(t *testing.T) {
	engine, db := testEngine(t, &protocolModel{}, 0)
	ctx := WithRun(context.Background(), "owner-run", false)
	model := &compactionFixture{}
	meter := engine.MeterCompaction(model)
	if _, err := meter.Generate(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("history")}); err != nil {
		t.Fatal(err)
	}
	used, err := db.MemoryUsageSince(ctx, time.Time{}, "owner")
	if err != nil || used != 29 {
		t.Fatalf("reported usage %d %v", used, err)
	}
	model.fail = true
	if _, err := meter.Generate(ctx, []*schema.AgenticMessage{schema.UserAgenticMessage("history")}); err == nil {
		t.Fatal("provider error hidden")
	}
	failed, err := db.MemoryUsageSince(ctx, time.Time{}, "owner")
	if err != nil || failed <= used+4096 {
		t.Fatalf("failed attempt lost reservation %d %v", failed, err)
	}
}
