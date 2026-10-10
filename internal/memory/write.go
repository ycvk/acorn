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

type WriteInput struct {
	ID       string   `json:"id,omitempty" jsonschema_description:"Existing thought ID to revise or resolve; omit for a new thought or keep."`
	Revision int64    `json:"revision,omitempty"`
	State    string   `json:"state,omitempty" jsonschema_description:"Thought state: open, resolved or released."`
	Reason   string   `json:"reason,omitempty"`
	Content  string   `json:"content" jsonschema_description:"The fact or thought to preserve."`
	SourceID string   `json:"source_id" jsonschema_description:"Source ID supplied in the current context; keep requires the current owner's message."`
	Quote    string   `json:"quote" jsonschema_description:"Exact supporting substring of the source."`
	Scope    string   `json:"scope,omitempty" jsonschema_description:"Conditions under which this applies."`
	Entities []string `json:"entities,omitempty"`
	Parents  []string `json:"parents,omitempty" jsonschema_description:"Related memory IDs for an agent thought."`
}

type CorrectInput struct {
	ID        string   `json:"id"`
	Revision  int64    `json:"revision"`
	Content   string   `json:"content"`
	SourceID  string   `json:"source_id" jsonschema_description:"The current owner message requesting this correction."`
	Quote     string   `json:"quote" jsonschema_description:"Exact words of the correction."`
	Scope     string   `json:"scope,omitempty"`
	Entities  []string `json:"entities,omitempty" jsonschema_description:"Entities supported by the corrected claim and source."`
	ValidFrom string   `json:"valid_from,omitempty" jsonschema_description:"When this becomes true, RFC3339; defaults to now."`
}

func (e *Engine) currentOwner(ctx context.Context, runID, id string) (core.MemorySource, error) {
	source, err := e.cfg.Store.LoadMemorySource(ctx, id)
	if err != nil {
		return source, err
	}
	if runID == "" || source.RunID != runID || source.Speaker != "owner" {
		return source, errors.New("memory change requires the current owner's request source")
	}
	return source, nil
}

func (e *Engine) Keep(ctx context.Context, runID string, in WriteInput) (core.MemoryRecord, error) {
	if _, err := e.currentOwner(ctx, runID, in.SourceID); err != nil {
		return core.MemoryRecord{}, err
	}
	if in.ID != "" || in.Revision != 0 || in.State != "" {
		return core.MemoryRecord{}, errors.New("keep creates direct facts; use memory_correct for revisions")
	}
	if len(in.Parents) > 0 {
		return core.MemoryRecord{}, errors.New("keep takes direct owner evidence; parents belong to thoughts")
	}
	exclusion, err := e.cfg.Store.MemoryExclusions(ctx)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	records, err := e.cfg.Store.CommitMemory(ctx, core.MemoryMutation{Epoch: exclusion.Epoch, Now: e.cfg.Clock(), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: in.Content, Scope: in.Scope, Entities: in.Entities, Pinned: true, Evidence: []core.MemoryEvidence{{SourceID: in.SourceID, Quote: in.Quote, Relation: "supports"}}}, Reason: "owner explicitly asked to remember"}}})
	if err != nil {
		return core.MemoryRecord{}, err
	}
	return records[0], nil
}

func (e *Engine) Think(ctx context.Context, runID, sessionID string, in WriteInput) (core.MemoryRecord, error) {
	if strings.TrimSpace(in.Content) == "" || runID == "" {
		return core.MemoryRecord{}, errors.New("think requires content and run")
	}
	origin, err := e.cfg.Store.LoadMemorySource(ctx, in.SourceID)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	if origin.RunID != runID {
		return core.MemoryRecord{}, errors.New("thought origin must belong to this run")
	}
	if strings.TrimSpace(in.Quote) == "" || !strings.Contains(origin.Content, in.Quote) {
		return core.MemoryRecord{}, errors.New("thought origin quote is absent from source")
	}
	exclusion, err := e.cfg.Store.MemoryExclusions(ctx)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	if in.ID != "" {
		old, err := e.cfg.Store.ReadMemory(ctx, in.ID)
		if err != nil {
			return core.MemoryRecord{}, err
		}
		if old.Record.Kind != "thought" || in.Revision <= 0 || in.Reason == "" {
			return core.MemoryRecord{}, errors.New("think updates require a thought ID, revision and reason")
		}
		evidence := append([]core.MemoryEvidence(nil), old.Record.Evidence...)
		evidence = append(evidence, core.MemoryEvidence{SourceID: origin.ID, Quote: in.Quote, Relation: "supports"})
		records, err := e.cfg.Store.CommitMemory(ctx, core.MemoryMutation{Epoch: exclusion.Epoch, Now: e.cfg.Clock(), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{ID: in.ID, Kind: "thought", Content: in.Content, State: in.State, Scope: in.Scope, Entities: in.Entities, Parents: in.Parents, Evidence: evidence}, ExpectedRevision: in.Revision, Reason: in.Reason}}})
		if err != nil {
			return core.MemoryRecord{}, err
		}
		return records[0], nil
	}
	sum := sha256.Sum256([]byte(runID + "\x00" + in.Content))
	id := "thought:" + hex.EncodeToString(sum[:16])
	source, err := e.cfg.Store.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "thought", ObjectID: id, Version: "1", Speaker: "assistant", RunID: runID, SessionID: sessionID, Body: in.Content, RecordedAt: e.cfg.Clock()})
	if err != nil {
		return core.MemoryRecord{}, err
	}
	records, err := e.cfg.Store.CommitMemory(ctx, core.MemoryMutation{Epoch: exclusion.Epoch, Now: e.cfg.Clock(), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "thought", Content: in.Content, Scope: in.Scope, Entities: in.Entities, Parents: in.Parents, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: in.Content, Relation: "supports"}, {SourceID: origin.ID, Quote: in.Quote, Relation: "supports"}}}, Reason: "agent thought with originating evidence"}}})
	if err != nil {
		return core.MemoryRecord{}, err
	}
	return records[0], nil
}

func (e *Engine) Correct(ctx context.Context, runID string, in CorrectInput) (core.MemoryRecord, error) {
	if _, err := e.currentOwner(ctx, runID, in.SourceID); err != nil {
		return core.MemoryRecord{}, err
	}
	old, err := e.cfg.Store.ReadMemory(ctx, in.ID)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	if old.Record.Kind != "fact" {
		return core.MemoryRecord{}, errors.New("memory_correct revises a fact; withdraw an inference by its id with memory_forget")
	}
	if old.Record.Revision != in.Revision {
		return core.MemoryRecord{}, core.ErrMemoryConflict
	}
	now := e.cfg.Clock()
	from := now
	if in.ValidFrom != "" {
		from, err = time.Parse(time.RFC3339, in.ValidFrom)
		if err != nil {
			return core.MemoryRecord{}, fmt.Errorf("valid_from: %w", err)
		}
	}
	exclusion, err := e.cfg.Store.MemoryExclusions(ctx)
	if err != nil {
		return core.MemoryRecord{}, err
	}
	records, err := e.cfg.Store.CommitMemory(ctx, core.MemoryMutation{Epoch: exclusion.Epoch, Now: now, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: in.Content, Scope: in.Scope, Entities: in.Entities, ValidFrom: from.Format(time.RFC3339Nano), Evidence: []core.MemoryEvidence{{SourceID: in.SourceID, Quote: in.Quote, Relation: "supports"}}}, Supersedes: in.ID, SupersedesRevision: in.Revision, Reason: "owner correction"}}})
	if err != nil {
		return core.MemoryRecord{}, err
	}
	return records[0], nil
}

// Forget commits exclusions before the runtime cancels old model input. The
// tool acknowledges only after its injected visibility barrier has completed.
func (e *Engine) Forget(ctx context.Context, runID string, request core.MemoryForget) (core.MemoryExclusion, error) {
	if _, err := e.currentOwner(ctx, runID, request.RequestSourceID); err != nil {
		return core.MemoryExclusion{}, err
	}
	request.Now = e.cfg.Clock()
	return e.cfg.Store.ForgetMemory(ctx, request)
}
