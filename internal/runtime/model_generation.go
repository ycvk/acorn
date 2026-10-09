package runtime

import (
	"context"
	"errors"
	"io"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// streamingGenerationModel also streams calls made by the summarizer. Anthropic
// requires streaming for requests with large output budgets.
type streamingGenerationModel struct{ einomodel.AgenticModel }

func (m *streamingGenerationModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...einomodel.Option) (*schema.AgenticMessage, error) {
	stream, err := m.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	var frames []*schema.AgenticMessage
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return schema.ConcatAgenticMessages(frames)
}
