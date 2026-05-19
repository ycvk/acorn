package providerusage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	CallSiteRuntime    = "runtime"
	CallSitePlan       = "plan"
	CallSiteAct        = "act"
	CallSiteObserve    = "observe"
	CallSiteAssistant  = "assistant"
	CallSiteCompaction = "compaction"
)

type contextKey struct{}

func WithCallSite(ctx context.Context, callSite string) context.Context {
	trimmed := strings.TrimSpace(callSite)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, trimmed)
}

func CallSiteFromContext(ctx context.Context) string {
	if ctx == nil {
		return CallSiteRuntime
	}
	value, ok := ctx.Value(contextKey{}).(string)
	if !ok {
		return CallSiteRuntime
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return CallSiteRuntime
	}
	return value
}

type Record struct {
	UsageID          string
	RunID            string
	SessionID        string
	CallSite         string
	ProviderName     string
	ModelName        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	CreatedAt        time.Time
}

type RunMetadata struct {
	RunID           string
	SessionID       string
	ProviderName    string
	ModelName       string
	InitialSequence uint64
}

type Recorder interface {
	AppendProviderUsage(context.Context, Record) error
}

func WrapModel(model einomodel.BaseChatModel, recorder Recorder, metadata RunMetadata) (einomodel.BaseChatModel, error) {
	if model == nil {
		return nil, errors.New("provider usage model is nil")
	}
	if recorder == nil {
		return nil, errors.New("provider usage recorder is nil")
	}
	metadata = normalizeRunMetadata(metadata)
	if metadata.RunID == "" {
		return nil, errors.New("provider usage run_id is required")
	}
	if metadata.ProviderName == "" {
		return nil, errors.New("provider usage provider_name is required")
	}
	if metadata.ModelName == "" {
		return nil, errors.New("provider usage model_name is required")
	}
	m := &recordingModel{
		inner:    model,
		recorder: recorder,
		metadata: metadata,
	}
	m.sequence.Store(metadata.InitialSequence)
	return m, nil
}

type recordingModel struct {
	inner    einomodel.BaseChatModel
	recorder Recorder
	metadata RunMetadata
	sequence atomic.Uint64
}

func (m *recordingModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	msg, err := m.inner.Generate(ctx, input, opts...)
	if err != nil {
		return msg, err
	}
	if err := m.recordUsage(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (m *recordingModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	stream, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, errors.New("provider usage inner stream is nil")
	}

	reader, writer := schema.Pipe[*schema.Message](0)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				writer.Send(nil, fmt.Errorf("provider usage stream panic: %v", r))
			}
			stream.Close()
			writer.Close()
		}()
		frames := make([]*schema.Message, 0, 4)
		for {
			frame, recvErr := stream.Recv()
			if recvErr != nil {
				if recvErr == io.EOF {
					break
				}
				writer.Send(nil, recvErr)
				return
			}
			frames = append(frames, frame)
			if closed := writer.Send(frame, nil); closed {
				return
			}
		}
		if len(frames) == 0 {
			return
		}
		finalMessage, concatErr := schema.ConcatMessages(frames)
		if concatErr != nil {
			writer.Send(nil, fmt.Errorf("concat provider usage stream: %w", concatErr))
			return
		}
		if recordErr := m.recordUsage(ctx, finalMessage); recordErr != nil {
			writer.Send(nil, recordErr)
		}
	}()
	return reader, nil
}

func (m *recordingModel) recordUsage(ctx context.Context, msg *schema.Message) error {
	if m == nil || msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return nil
	}
	usage := msg.ResponseMeta.Usage
	record := Record{
		UsageID:          m.nextUsageID(),
		RunID:            m.metadata.RunID,
		SessionID:        m.metadata.SessionID,
		CallSite:         CallSiteFromContext(ctx),
		ProviderName:     m.metadata.ProviderName,
		ModelName:        m.metadata.ModelName,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		CachedTokens:     usage.PromptTokenDetails.CachedTokens,
		ReasoningTokens:  usage.CompletionTokensDetails.ReasoningTokens,
		CreatedAt:        time.Now().UTC(),
	}
	if err := normalizeRecord(record); err != nil {
		return err
	}
	if err := m.recorder.AppendProviderUsage(ctx, record); err != nil {
		return fmt.Errorf("record provider usage %s: %w", record.UsageID, err)
	}
	return nil
}

func (m *recordingModel) nextUsageID() string {
	seq := m.sequence.Add(1)
	return fmt.Sprintf("provider_usage:%s:%06d", m.metadata.RunID, seq)
}

func normalizeRunMetadata(metadata RunMetadata) RunMetadata {
	return RunMetadata{
		RunID:           strings.TrimSpace(metadata.RunID),
		SessionID:       strings.TrimSpace(metadata.SessionID),
		ProviderName:    strings.TrimSpace(metadata.ProviderName),
		ModelName:       strings.TrimSpace(metadata.ModelName),
		InitialSequence: metadata.InitialSequence,
	}
}

func NormalizeRecord(record Record) (Record, error) {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UsageID = strings.TrimSpace(record.UsageID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.CallSite = strings.TrimSpace(record.CallSite)
	record.ProviderName = strings.TrimSpace(record.ProviderName)
	record.ModelName = strings.TrimSpace(record.ModelName)
	return record, normalizeRecord(record)
}

func normalizeRecord(record Record) error {
	if record.UsageID == "" {
		return errors.New("provider usage id is required")
	}
	if record.RunID == "" {
		return errors.New("provider usage run_id is required")
	}
	if record.CallSite == "" {
		return errors.New("provider usage call_site is required")
	}
	if record.ProviderName == "" {
		return errors.New("provider usage provider_name is required")
	}
	if record.ModelName == "" {
		return errors.New("provider usage model_name is required")
	}
	return nil
}
