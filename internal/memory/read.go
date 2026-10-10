package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type ReadInput struct {
	ID        string `json:"id"`
	Cursor    string `json:"cursor,omitempty" jsonschema_description:"The next_cursor returned by the previous page."`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

type ReadPage struct {
	Record       core.MemoryRecord   `json:"record"`
	Versions     []core.MemoryRecord `json:"versions,omitempty"`
	Links        []core.MemoryLink   `json:"links,omitempty"`
	Source       *core.MemorySource  `json:"source,omitempty"`
	SourceOffset int                 `json:"source_offset,omitempty"`
	NextCursor   string              `json:"next_cursor,omitempty"`
}

func (e *Engine) Read(ctx context.Context, in ReadInput) (ReadPage, error) {
	read, err := e.cfg.Store.ReadMemory(ctx, in.ID)
	if err != nil {
		return ReadPage{}, err
	}

	budget := in.MaxTokens
	if budget <= 0 {
		budget = e.cfg.ContextTokens
	}
	budget = min(budget, e.cfg.ContextTokens)
	phase, index, offset := "v", 0, 0
	if in.Cursor != "" {
		parts := strings.Split(in.Cursor, ":")
		if len(parts) != 4 {
			return ReadPage{}, errors.New("invalid memory_read cursor")
		}
		revision, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || revision != read.Record.Revision {
			return ReadPage{}, core.ErrMemoryConflict
		}
		phase = parts[1]
		index, err = strconv.Atoi(parts[2])
		if err != nil || index < 0 {
			return ReadPage{}, errors.New("invalid memory_read index")
		}
		offset, err = strconv.Atoi(parts[3])
		if err != nil || offset < 0 || (phase != "v" && phase != "s") {
			return ReadPage{}, errors.New("invalid memory_read cursor")
		}
	}
	cursor := func(phase string, index, offset int) string {
		return fmt.Sprintf("%d:%s:%d:%d", read.Record.Revision, phase, index, offset)
	}
	out := ReadPage{Record: read.Record, Links: read.Links}
	if phase == "v" {
		if index > len(read.Versions) || offset != 0 {
			return out, errors.New("invalid revision cursor")
		}
		for index < len(read.Versions) {
			candidate := out
			candidate.Versions = append(append([]core.MemoryRecord(nil), out.Versions...), read.Versions[len(read.Versions)-1-index])
			candidate.NextCursor = ""
			if index+1 < len(read.Versions) {
				candidate.NextCursor = cursor("v", index+1, 0)
			} else if len(read.Sources) > 0 {
				candidate.NextCursor = cursor("s", 0, 0)
			}
			n, err := e.jsonTokens(ctx, candidate)
			if err != nil {
				return out, err
			}
			if n > budget {
				if len(out.Versions) == 0 {
					return out, errors.New("memory_read max_tokens cannot hold this revision and its evidence")
				}
				return out, nil
			}
			out = candidate
			index++
		}
		if len(out.Versions) > 0 {
			return out, nil
		}
		index = 0
	}
	if len(read.Sources) == 0 {
		n, err := e.jsonTokens(ctx, out)
		if err != nil {
			return out, err
		}
		if n > budget {
			return out, errors.New("memory_read max_tokens cannot hold record metadata")
		}
		return out, nil
	}
	if index >= len(read.Sources) {
		return out, errors.New("memory_read cursor is beyond the source range")
	}
	source := read.Sources[index]
	runes := []rune(source.Content)
	if offset > len(runes) {
		return out, errors.New("memory_read cursor is beyond the source content")
	}
	source.Content = ""
	out.Source = &source
	out.SourceOffset = offset
	fit := func(end int) (bool, error) {
		source.Content = string(runes[offset:end])
		out.NextCursor = ""
		if end < len(runes) {
			out.NextCursor = cursor("s", index, end)
		} else if index+1 < len(read.Sources) {
			out.NextCursor = cursor("s", index+1, 0)
		}
		n, err := e.jsonTokens(ctx, out)
		return n <= budget, err
	}
	low, high := offset, len(runes)
	for low < high {
		mid := low + (high-low+1)/2
		ok, err := fit(mid)
		if err != nil {
			return out, err
		}
		if ok {
			low = mid
		} else {
			high = mid - 1
		}
	}
	ok, err := fit(low)
	if err != nil {
		return out, err
	}
	if !ok || (low == offset && offset < len(runes)) {
		return out, errors.New("memory_read max_tokens cannot hold source metadata and content")
	}
	return out, nil
}

func (e *Engine) jsonTokens(ctx context.Context, v any) (int, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return e.cfg.Count(ctx, string(data))
}

type ConcernInput struct {
	Action    string   `json:"action" jsonschema:"enum=list,enum=create,enum=active,enum=waiting,enum=resolved,enum=released"`
	ID        string   `json:"id,omitempty"`
	Revision  int64    `json:"revision,omitempty"`
	Title     string   `json:"title,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	SourceID  string   `json:"source_id,omitempty"`
	RecordIDs []string `json:"record_ids,omitempty"`
	ReviewAt  string   `json:"review_at,omitempty"`
}

func (e *Engine) Concern(ctx context.Context, runID string, in ConcernInput) ([]core.MemoryConcern, error) {
	if in.Action == "list" {
		return e.cfg.Store.ListConcerns(ctx, false)
	}
	source, err := e.cfg.Store.LoadMemorySource(ctx, in.SourceID)
	if err != nil {
		return nil, err
	}
	if source.RunID != runID || runID == "" {
		return nil, errors.New("concern update requires a source from the current run")
	}
	if in.Action == "resolved" && source.Speaker != "owner" && source.Speaker != "tool" {
		return nil, errors.New("resolving a concern requires owner confirmation or execution evidence")
	}
	concern := core.MemoryConcern{ID: in.ID, Title: in.Title, State: in.Action, Reason: in.Reason, SourceID: in.SourceID, RecordIDs: in.RecordIDs, UpdatedAt: e.cfg.Clock()}
	if in.Action == "create" {
		if in.ID != "" || in.Revision != 0 {
			return nil, errors.New("new concern must omit id and revision")
		}
		concern.State = "active"
	}
	if in.ReviewAt != "" {
		concern.ReviewAt, err = time.Parse(time.RFC3339, in.ReviewAt)
		if err != nil {
			return nil, fmt.Errorf("concern review_at: %w", err)
		}
	}
	saved, err := e.cfg.Store.SaveConcern(ctx, concern, in.Revision)
	if err != nil {
		return nil, err
	}
	return []core.MemoryConcern{saved}, nil
}
