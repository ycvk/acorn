package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// ForgetForOwner excludes a memory the owner chose to forget in the app. The
// request is recorded as the owner's source and excluded with the memory.
func (e *Engine) ForgetForOwner(ctx context.Context, id string) (core.MemoryExclusion, error) {
	read, err := e.cfg.Store.ReadMemory(ctx, id)
	if err != nil {
		return core.MemoryExclusion{}, err
	}
	request, err := e.ownerRequest(ctx, "Forget: "+read.Record.Content)
	if err != nil {
		return core.MemoryExclusion{}, err
	}
	return e.cfg.Store.ForgetMemory(ctx, core.MemoryForget{
		RecordIDs:       []string{id},
		RequestSourceID: request.ID,
		Reason:          "owner forgot it in the app",
		Now:             e.cfg.Clock(),
	})
}

// ForgetThreadForOwner forgets the conversation the owner deletes: every
// message and tool result of the thread, on a request recorded as the owner's
// source. A thread with nothing to forget returns an empty exclusion.
func (e *Engine) ForgetThreadForOwner(ctx context.Context, threadID, title string) (core.MemoryExclusion, error) {
	sources, err := e.cfg.Store.ThreadMemorySourceIDs(ctx, threadID)
	if err != nil {
		return core.MemoryExclusion{}, err
	}
	if len(sources) == 0 {
		return core.MemoryExclusion{}, nil
	}
	request, err := e.ownerRequest(ctx, "Delete conversation: "+title)
	if err != nil {
		return core.MemoryExclusion{}, err
	}
	return e.cfg.Store.ForgetMemory(ctx, core.MemoryForget{
		SourceIDs:       sources,
		RequestSourceID: request.ID,
		Reason:          "owner deleted the conversation",
		Now:             e.cfg.Clock(),
	})
}

// CorrectForOwner supersedes a fact with the owner's own wording from the
// app; the wording is the new fact's evidence. Scope and entities carry over.
func (e *Engine) CorrectForOwner(ctx context.Context, id string, revision int64, content string) (core.MemoryRecord, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return core.MemoryRecord{}, errors.New("correction content is required")
	}
	read, err := e.cfg.Store.ReadMemory(ctx, id)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	if read.Record.Kind != "fact" {
		return core.MemoryRecord{}, fmt.Errorf("%w: %s is a %s", core.ErrMemoryNotFact, id, read.Record.Kind)
	}
	if read.Record.Revision != revision {
		return core.MemoryRecord{}, core.ErrMemoryConflict
	}
	request, err := e.ownerRequest(ctx, content)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	return e.correct(ctx, CorrectInput{
		ID:       id,
		Revision: revision,
		Content:  content,
		SourceID: request.ID,
		Quote:    content,
		Scope:    read.Record.Scope,
		Entities: read.Record.Entities,
	})
}

func (e *Engine) ownerRequest(ctx context.Context, body string) (core.MemorySource, error) {
	now := e.cfg.Clock()
	sum := sha256.Sum256([]byte(now.Format(time.RFC3339Nano) + "\x00" + body))
	id := "request:" + hex.EncodeToString(sum[:16])
	return e.cfg.Store.RegisterMemorySource(ctx, core.MemorySource{
		ID: id, Kind: core.MemoryRequestSource, ObjectID: id, Version: "1", Speaker: "owner", Body: body, RecordedAt: now,
	})
}
