package toolset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
)

type ArtifactWriteInput struct {
	Kind     string `json:"kind" jsonschema:"description=Artifact kind: text, markdown, json, diff, log, test_report, or binary."`
	Title    string `json:"title,omitempty" jsonschema:"description=Short human-readable title for the artifact."`
	MIMEType string `json:"mime_type,omitempty" jsonschema:"description=Optional MIME type such as text/plain or application/json."`
	Content  string `json:"content" jsonschema:"description=Artifact content to persist as run evidence."`
}

type ArtifactReadInput struct {
	ArtifactID string `json:"artifact_id" jsonschema:"description=Opaque artifact id returned by artifact_write or artifact_list."`
	Offset     int64  `json:"offset" jsonschema:"description=Zero-based byte offset to start reading from."`
	Limit      int64  `json:"limit" jsonschema:"description=Maximum bytes to read. Must be greater than zero."`
}

type ArtifactListInput struct {
	RunID     string `json:"run_id,omitempty" jsonschema:"description=Optional run id. Mutually exclusive with session_id. Defaults to the current run when available."`
	SessionID string `json:"session_id,omitempty" jsonschema:"description=Optional session id. Mutually exclusive with run_id. Used when listing session-level artifacts."`
}

type ArtifactSummary struct {
	ArtifactID          string `json:"artifact_id"`
	RunID               string `json:"run_id"`
	SessionID           string `json:"session_id,omitempty"`
	SourceToolResultRef string `json:"source_tool_result_ref,omitempty"`
	Kind                string `json:"kind"`
	Title               string `json:"title,omitempty"`
	MIMEType            string `json:"mime_type,omitempty"`
	SizeBytes           int64  `json:"size_bytes"`
	SHA256              string `json:"sha256"`
	CreatedAt           string `json:"created_at"`
}

type ArtifactWriteOutput struct {
	ArtifactSummary
}

type ArtifactReadOutput struct {
	ArtifactSummary
	Offset  int64  `json:"offset"`
	Bytes   int    `json:"bytes"`
	EOF     bool   `json:"eof"`
	Content string `json:"content"`
}

type ArtifactListOutput struct {
	RunID     string            `json:"run_id,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	Items     []ArtifactSummary `json:"items"`
}

func buildArtifactTools(service ArtifactService, bridge domain.ToolCallContextBridge) ([]einotool.BaseTool, error) {
	if service == nil {
		return nil, errors.New("artifact service is required")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required")
	}
	writeTool, err := buildArtifactWriteTool(service, bridge)
	if err != nil {
		return nil, err
	}
	readTool, err := buildArtifactReadTool(service)
	if err != nil {
		return nil, err
	}
	listTool, err := buildArtifactListTool(service, bridge)
	if err != nil {
		return nil, err
	}
	return []einotool.BaseTool{writeTool, readTool, listTool}, nil
}

func buildArtifactWriteTool(service ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("artifact_write", "Persist run-scoped artifact content and return an opaque artifact id.", func(ctx context.Context, input ArtifactWriteInput, emit tools.ToolProgressEmitter) (ArtifactWriteOutput, error) {
		runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
		if runID == "" {
			return ArtifactWriteOutput{}, errors.New("artifact_write requires current run context")
		}
		callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
		if callID == "" {
			return ArtifactWriteOutput{}, errors.New("artifact_write requires current tool call context")
		}
		sourceRef := "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID)
		record, err := service.Write(ctx, store.ArtifactWriteRequest{
			RunID:               runID,
			SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
			SourceToolResultRef: sourceRef,
			Kind:                store.ArtifactKind(strings.TrimSpace(input.Kind)),
			Title:               input.Title,
			MIMEType:            input.MIMEType,
			Content:             []byte(input.Content),
		})
		if err != nil {
			return ArtifactWriteOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("wrote artifact %s (%d bytes)", record.ArtifactID, record.SizeBytes)); err != nil {
			return ArtifactWriteOutput{}, err
		}
		return ArtifactWriteOutput{ArtifactSummary: artifactSummaryFromRecord(record)}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build artifact_write tool: %w", err)
	}
	return tool, nil
}

func buildArtifactReadTool(service ArtifactService) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("artifact_read", "Read an explicit byte range from a persisted artifact.", func(ctx context.Context, input ArtifactReadInput, emit tools.ToolProgressEmitter) (ArtifactReadOutput, error) {
		result, err := service.ReadRange(ctx, store.ArtifactReadRangeRequest{
			ArtifactID: input.ArtifactID,
			Offset:     input.Offset,
			Limit:      input.Limit,
		})
		if err != nil {
			return ArtifactReadOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("read artifact %s offset %d bytes %d", result.Record.ArtifactID, result.Offset, len(result.Content))); err != nil {
			return ArtifactReadOutput{}, err
		}
		return ArtifactReadOutput{
			ArtifactSummary: artifactSummaryFromRecord(result.Record),
			Offset:          result.Offset,
			Bytes:           len(result.Content),
			EOF:             result.EOF,
			Content:         string(result.Content),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build artifact_read tool: %w", err)
	}
	return tool, nil
}

func buildArtifactListTool(service ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("artifact_list", "List artifacts for a run or session.", func(ctx context.Context, input ArtifactListInput, emit tools.ToolProgressEmitter) (ArtifactListOutput, error) {
		runID := strings.TrimSpace(input.RunID)
		sessionID := strings.TrimSpace(input.SessionID)
		if runID != "" && sessionID != "" {
			return ArtifactListOutput{}, errors.New("artifact_list accepts run_id or session_id, not both")
		}
		if runID == "" && sessionID == "" {
			runID = strings.TrimSpace(bridge.CurrentRunID(ctx))
			if runID == "" {
				sessionID = strings.TrimSpace(bridge.CurrentSessionID(ctx))
			}
		}
		var (
			records []store.ArtifactRecord
			err     error
		)
		if runID != "" {
			records, err = service.ListByRun(ctx, runID)
		} else if sessionID != "" {
			records, err = service.ListBySession(ctx, sessionID)
		} else {
			return ArtifactListOutput{}, errors.New("artifact_list requires run_id, session_id, or current run/session context")
		}
		if err != nil {
			return ArtifactListOutput{}, err
		}
		items := make([]ArtifactSummary, 0, len(records))
		for _, record := range records {
			items = append(items, artifactSummaryFromRecord(record))
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("listed %d artifacts", len(items))); err != nil {
			return ArtifactListOutput{}, err
		}
		return ArtifactListOutput{RunID: runID, SessionID: sessionID, Items: items}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build artifact_list tool: %w", err)
	}
	return tool, nil
}

func artifactSummaryFromRecord(record store.ArtifactRecord) ArtifactSummary {
	return ArtifactSummary{
		ArtifactID:          record.ArtifactID,
		RunID:               record.RunID,
		SessionID:           record.SessionID,
		SourceToolResultRef: record.SourceToolResultRef,
		Kind:                string(record.Kind),
		Title:               record.Title,
		MIMEType:            record.MIMEType,
		SizeBytes:           record.SizeBytes,
		SHA256:              record.SHA256,
		CreatedAt:           record.CreatedAt.Format("2006-01-02T15:04:05.000000000Z07:00"),
	}
}
